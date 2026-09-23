package bench

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/handsomefox/dnsbench/internal/dnsclient"
	"github.com/handsomefox/dnsbench/internal/dnstest"
)

func TestStats_IsValid(t *testing.T) {
	tests := []struct {
		name  string
		stats Stats
		want  bool
	}{
		{
			name: "Valid stats",
			stats: Stats{
				Min:    1.0,
				Max:    10.0,
				Mean:   5.0,
				Count:  100,
				Errors: 0,
				Total:  100,
			},
			want: true,
		},
		{
			name: "Zero count",
			stats: Stats{
				Min:    1.0,
				Max:    10.0,
				Mean:   5.0,
				Count:  0,
				Errors: 0,
				Total:  100,
			},
			want: false,
		},
		{
			name: "NaN mean",
			stats: Stats{
				Min:    1.0,
				Max:    10.0,
				Mean:   math.NaN(),
				Count:  100,
				Errors: 0,
				Total:  100,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.stats.IsValid(); got != tt.want {
				t.Errorf("Stats.IsValid() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStats_SuccessRate(t *testing.T) {
	tests := []struct {
		name  string
		stats Stats
		want  float64
	}{
		{
			name: "All successful",
			stats: Stats{
				Count: 100,
				Total: 100,
			},
			want: 1.0,
		},
		{
			name: "50% success rate",
			stats: Stats{
				Count: 50,
				Total: 100,
			},
			want: 0.5,
		},
		{
			name: "Zero total",
			stats: Stats{
				Count: 0,
				Total: 0,
			},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.stats.SuccessRate(); got != tt.want {
				t.Errorf("Stats.SuccessRate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCalculateStats(t *testing.T) {
	tests := []struct {
		name      string
		latencies []float64
		errors    int
		total     int
		want      Stats
	}{
		{
			name:      "Empty latencies",
			latencies: []float64{},
			errors:    5,
			total:     10,
			want: Stats{
				Min:    math.NaN(),
				Max:    math.NaN(),
				Mean:   math.NaN(),
				Count:  0,
				Errors: 5,
				Total:  10,
			},
		},
		{
			name:      "Normal distribution",
			latencies: []float64{1.0, 2.0, 3.0, 4.0, 5.0},
			errors:    2,
			total:     7,
			want: Stats{
				Min:    1.0,
				Max:    5.0,
				Mean:   3.0,
				Median: 3.0,
				P95:    4.8,
				Count:  5,
				Errors: 2,
				Total:  7,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateStats(tt.latencies, tt.errors, tt.total)

			// Special handling for NaN comparisons
			if math.IsNaN(got.Min) != math.IsNaN(tt.want.Min) ||
				(!math.IsNaN(got.Min) && got.Min != tt.want.Min) {
				t.Errorf("calculateStats() Min = %v, want %v", got.Min, tt.want.Min)
			}
			if math.IsNaN(got.Max) != math.IsNaN(tt.want.Max) ||
				(!math.IsNaN(got.Max) && got.Max != tt.want.Max) {
				t.Errorf("calculateStats() Max = %v, want %v", got.Max, tt.want.Max)
			}
			if math.IsNaN(got.Mean) != math.IsNaN(tt.want.Mean) ||
				(!math.IsNaN(got.Mean) && got.Mean != tt.want.Mean) {
				t.Errorf("calculateStats() Mean = %v, want %v", got.Mean, tt.want.Mean)
			}
			for _, f := range []struct {
				name      string
				got, want float64
			}{{"Median", got.Median, tt.want.Median}, {"P95", got.P95, tt.want.P95}} {
				if tt.want.Count == 0 {
					if !math.IsNaN(f.got) {
						t.Errorf("calculateStats() %s = %v, want NaN", f.name, f.got)
					}
				} else if math.Abs(f.got-f.want) > 1e-9 {
					t.Errorf("calculateStats() %s = %v, want %v", f.name, f.got, f.want)
				}
			}
			if got.Count != tt.want.Count {
				t.Errorf("calculateStats() Count = %v, want %v", got.Count, tt.want.Count)
			}
			if got.Errors != tt.want.Errors {
				t.Errorf("calculateStats() Errors = %v, want %v", got.Errors, tt.want.Errors)
			}
			if got.Total != tt.want.Total {
				t.Errorf("calculateStats() Total = %v, want %v", got.Total, tt.want.Total)
			}
		})
	}
}

func TestRunBenchmark_ValidatesInput(t *testing.T) {
	ctx := context.Background()
	cfg := Options{Repeats: 1}

	if _, err := Run(ctx, cfg, nil, []string{"example.com"}, NoopReporter{}); err == nil {
		t.Fatalf("expected error for missing servers")
	}

	if _, err := Run(ctx, cfg, []dnsclient.Server{{Name: "a", Addr: "1.1.1.1"}}, nil, NoopReporter{}); err == nil {
		t.Fatalf("expected error for missing domains")
	}
}

func TestStats_MarshalJSON(t *testing.T) {
	failed := Stats{Min: math.NaN(), Max: math.NaN(), Mean: math.NaN(), Errors: 3, Total: 3}
	ok := Stats{Min: 1.5, Max: 4, Mean: 2.25, Median: 2.25, P95: 3.8, Count: 2, Total: 2}

	tests := []struct {
		name string
		v    any
		want string
	}{
		{
			name: "NaN latencies become null",
			v:    failed,
			want: `{"min":null,"max":null,"mean":null,"median":null,"p95":null,"count":0,"errors":3,"total":3,"retried":0,"blocked":0}`,
		},
		{
			name: "finite latencies stay numbers",
			v:    ok,
			want: `{"min":1.5,"max":4,"mean":2.25,"median":2.25,"p95":3.8,"count":2,"errors":0,"total":2,"retried":0,"blocked":0}`,
		},
		{
			// The SSE reporter stores Stats by value inside a map.
			name: "inside a map",
			v:    map[string]any{"stats": failed},
			want: `{"stats":{"min":null,"max":null,"mean":null,"median":null,"p95":null,"count":0,"errors":3,"total":3,"retried":0,"blocked":0}}`,
		},
		{
			name: "inside a result",
			v:    Result{Server: dnsclient.Server{Name: "a", Addr: "192.0.2.1"}, Stats: failed},
			want: `{"server":{"name":"a","addr":"192.0.2.1"},"stats":{"min":null,"max":null,"mean":null,"median":null,"p95":null,"count":0,"errors":3,"total":3,"retried":0,"blocked":0}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.v)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("json.Marshal() = %s, want %s", got, tt.want)
			}
		})
	}
}

type countingReporter struct {
	NoopReporter
	failed atomic.Int32
	err    atomic.Value
}

func (r *countingReporter) OnQueryResult(_ dnsclient.Server, result QueryResult) {
	if result.Err != nil {
		r.failed.Add(1)
		r.err.Store(result.Err.Error())
	}
}

// A resolver the host has no route to must fail at once, not after ten
// attempts with backoff. The zoned link-local address names an interface
// that does not exist, so the UDP connect fails on any host.
func TestRunBenchmark_UnreachableResolverFailsFast(t *testing.T) {
	cfg := Options{Repeats: 3, Timeout: time.Second, Concurrency: 2}
	servers := []dnsclient.Server{{Name: "nowhere", Addr: "fe80::1%nosuchif0"}}
	domains := []string{"example.com", "example.org"}
	reporter := &countingReporter{}

	start := time.Now()
	results, err := Run(t.Context(), cfg, servers, domains, reporter)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if took := time.Since(start); took > time.Second {
		t.Errorf("Run() took %v, want under a second", took)
	}

	stats := results[0].Stats
	if stats.Count != 0 || stats.Errors != 6 || stats.Total != 6 {
		t.Errorf("stats = %+v, want 0 successes and 6 errors of 6", stats)
	}
	if got := reporter.failed.Load(); got != 6 {
		t.Errorf("reporter saw %d failed lookups, want 6", got)
	}
	if msg, ok := reporter.err.Load().(string); !ok || !strings.Contains(msg, "no route to resolver") {
		t.Errorf("reported error = %q, want it to mention the missing route", msg)
	}
}

// viaDoT points every server of a run at the fake DoT server f.
func viaDoT(f *dnstest.DoT) func(dnsclient.Server, int) *dnsclient.Resolver {
	return func(server dnsclient.Server, concurrency int) *dnsclient.Resolver {
		server.TLSName = "dns.test"
		return dnsclient.New(server, concurrency, dnsclient.WithHostPort(f.HostPort), dnsclient.WithRootCAs(f.Roots))
	}
}

// Each resolver gets every domain Repeats times, plus WarmupRuns warmup
// lookups of each domain before its first measured lookup.
func TestRunBenchmark_CountsLookupsAndWarmups(t *testing.T) {
	f := dnstest.StartDoT(t, false)

	cfg := Options{Repeats: 2, Warmup: 1, Timeout: 2 * time.Second, Concurrency: 4, NewResolver: viaDoT(f)}
	servers := []dnsclient.Server{{Name: "a", Addr: "127.0.0.1"}, {Name: "b", Addr: "127.0.0.2"}}
	domains := []string{"one.example", "two.example", "three.example"}

	results, err := Run(t.Context(), cfg, servers, domains, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	for i, r := range results {
		if r.Server != servers[i] {
			t.Errorf("result %d is for %s, want the servers' order", i, r.Server.Name)
		}
		if r.Stats.Count != 6 || r.Stats.Total != 6 {
			t.Errorf("%s: stats = %+v, want 6 answers of 6", r.Server.Name, r.Stats)
		}
	}
	// Two servers × (3 domains × 2 repeats + 3 domains × 1 warmup).
	if got := f.Queries.Load(); got != 18 {
		t.Errorf("server saw %d queries, want 18", got)
	}
}

// cancelAfter cancels a run after its first n lookups.
type cancelAfter struct {
	NoopReporter
	n      atomic.Int32
	cancel context.CancelFunc
}

func (r *cancelAfter) OnQueryResult(_ dnsclient.Server, _ QueryResult) {
	if r.n.Add(-1) == 0 {
		r.cancel()
	}
}

// Stopping a run must not count the lookups it cut short as failures.
func TestRunBenchmark_CancelDoesNotCountFailures(t *testing.T) {
	f := dnstest.StartDoT(t, false)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reporter := &cancelAfter{cancel: cancel}
	reporter.n.Store(3)

	cfg := Options{Repeats: 20, Timeout: 2 * time.Second, Concurrency: 2, NewResolver: viaDoT(f)}
	servers := []dnsclient.Server{{Name: "a", Addr: "127.0.0.1"}, {Name: "b", Addr: "127.0.0.2"}}
	results, err := Run(ctx, cfg, servers, []string{"one.example", "two.example"}, reporter)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	for _, r := range results {
		if r.Stats.Errors != 0 {
			t.Errorf("%s: %d failed lookups after a cancel, want 0", r.Server.Name, r.Stats.Errors)
		}
		if r.Stats.Total >= 40 {
			t.Errorf("%s: %d lookups ran, want the run cut short", r.Server.Name, r.Stats.Total)
		}
	}
}

// viaHostPort points every server of a run at hostPort over plain DNS.
func viaHostPort(hostPort string) func(dnsclient.Server, int) *dnsclient.Resolver {
	return func(server dnsclient.Server, concurrency int) *dnsclient.Resolver {
		return dnsclient.New(server, concurrency, dnsclient.WithHostPort(hostPort))
	}
}

// A resolver that passes the precheck but never answers must not cost
// every lookup its full timeout. The run gives up on it after giveUpAfter
// failed lookups and fails the rest at once.
func TestRunBenchmark_GivesUpOnSilentResolver(t *testing.T) {
	addr, queries := dnstest.StartSilent(t)

	cfg := Options{Repeats: 10, Timeout: 100 * time.Millisecond, Retries: 0, Concurrency: 2, NewResolver: viaHostPort(addr)}
	domains := []string{"one.example", "two.example", "three.example", "four.example"}
	reporter := &countingReporter{}

	start := time.Now()
	results, err := Run(t.Context(), cfg, []dnsclient.Server{{Name: "silent", Addr: "127.0.0.1"}}, domains, reporter)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	// Forty lookups at 100 ms each, two at a time, would take two seconds.
	if took := time.Since(start); took > time.Second {
		t.Errorf("Run() took %v, want it to give up well before a second", took)
	}
	if stats := results[0].Stats; stats.Errors != 40 || stats.Total != 40 {
		t.Errorf("stats = %+v, want 40 failed lookups of 40", stats)
	}
	// The lookups in flight when the run gave up had sent their queries.
	if got := int(queries.Load()); got > giveUpAfter+cfg.Concurrency {
		t.Errorf("server saw %d queries, want about %d before the run gave up", got, giveUpAfter)
	}
	if msg, ok := reporter.err.Load().(string); !ok || !strings.Contains(msg, "gave up") {
		t.Errorf("last reported error = %q, want it to say the run gave up", msg)
	}
}

// warmUp sends runs lookups of the domain, one query each.
func TestWarmUp_SendsRunsLookups(t *testing.T) {
	hostPort, roots, queries := dnstest.StartDoH(t)
	server := dnsclient.Server{Addr: "127.0.0.1", DoHURL: "https://example.com/dns-query"}
	r := dnsclient.New(server, 2, dnsclient.WithHostPort(hostPort), dnsclient.WithRootCAs(roots))
	defer r.Close()
	for _, domain := range []string{"a.example.", "b.example."} {
		warmUp(t.Context(), r, domain, 3)
	}
	if got := queries.Load(); got != 6 {
		t.Errorf("server saw %d warmup queries, want 6: two domains, three runs each", got)
	}
}

// A run stopped before or during the prechecks must not blame the
// resolvers: every precheck fails then, but no lookup ran.
func TestRun_CanceledBeforeLookups(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	cfg := Options{Repeats: 3, Timeout: time.Second, Concurrency: 2}
	servers := []dnsclient.Server{{Name: "a", Addr: "192.0.2.1"}, {Name: "b", Addr: "192.0.2.2", TLSName: "dns.example"}}
	results, err := Run(ctx, cfg, servers, []string{"one.example"}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context.Canceled", err)
	}
	for _, r := range results {
		if r.Stats.Errors != 0 || r.Stats.Total != 0 {
			t.Errorf("%s: stats = %+v, want no lookups at all", r.Server.Name, r.Stats)
		}
	}
}

// A filtering resolver answers a blocked name with NXDOMAIN. That is an
// answer, so the lookup counts as answered and blocked, not as failed.
func TestRun_CountsNXDOMAINAsBlocked(t *testing.T) {
	f := dnstest.StartDoT(t, false)
	cfg := Options{Repeats: 2, Timeout: 2 * time.Second, Concurrency: 2, NewResolver: viaDoT(f)}
	results, err := Run(t.Context(), cfg, []dnsclient.Server{{Name: "a", Addr: "127.0.0.1"}}, []string{"ok.example", "nx.example"}, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	s := results[0].Stats
	if s.Count != 4 || s.Blocked != 2 || s.Errors != 0 {
		t.Errorf("stats = %+v, want 4 answered, 2 of them blocked, and no errors", s)
	}
}
