package main

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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
	cfg := &Config{Repeats: 1}

	if _, err := runBenchmark(ctx, cfg, nil, []string{"example.com"}, NoopReporter{}); err == nil {
		t.Fatalf("expected error for missing servers")
	}

	if _, err := runBenchmark(ctx, cfg, []DNSServer{{Name: "a", Addr: "1.1.1.1"}}, nil, NoopReporter{}); err == nil {
		t.Fatalf("expected error for missing domains")
	}
}

func TestStats_MarshalJSON(t *testing.T) {
	failed := Stats{Min: math.NaN(), Max: math.NaN(), Mean: math.NaN(), Errors: 3, Total: 3}
	ok := Stats{Min: 1.5, Max: 4, Mean: 2.25, Count: 2, Total: 2}

	tests := []struct {
		name string
		v    any
		want string
	}{
		{
			name: "NaN latencies become null",
			v:    failed,
			want: `{"min":null,"max":null,"mean":null,"count":0,"errors":3,"total":3,"retried":0}`,
		},
		{
			name: "finite latencies stay numbers",
			v:    ok,
			want: `{"min":1.5,"max":4,"mean":2.25,"count":2,"errors":0,"total":2,"retried":0}`,
		},
		{
			// The SSE reporter stores Stats by value inside a map.
			name: "inside a map",
			v:    map[string]any{"stats": failed},
			want: `{"stats":{"min":null,"max":null,"mean":null,"count":0,"errors":3,"total":3,"retried":0}}`,
		},
		{
			name: "inside a result",
			v:    BenchmarkResult{Server: DNSServer{Name: "a", Addr: "192.0.2.1"}, Stats: failed},
			want: `{"server":{"name":"a","addr":"192.0.2.1"},"stats":{"min":null,"max":null,"mean":null,"count":0,"errors":3,"total":3,"retried":0}}`,
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

func (r *countingReporter) OnQueryResult(_ DNSServer, _ string, _ float64, _ int, err error) {
	if err != nil {
		r.failed.Add(1)
		r.err.Store(err.Error())
	}
}

// A resolver the host has no route to must fail at once, not after ten
// attempts with backoff. The zoned link-local address names an interface
// that does not exist, so the UDP connect fails on any host.
func TestRunBenchmark_UnreachableResolverFailsFast(t *testing.T) {
	cfg := &Config{Repeats: 3, LookupTimeout: time.Second, MaxConcurrency: 2}
	servers := []DNSServer{{Name: "nowhere", Addr: "fe80::1%nosuchif0"}}
	domains := []string{"example.com", "example.org"}
	reporter := &countingReporter{}

	start := time.Now()
	results, err := runBenchmark(t.Context(), cfg, servers, domains, reporter)
	if err != nil {
		t.Fatalf("runBenchmark() error = %v", err)
	}
	if took := time.Since(start); took > time.Second {
		t.Errorf("runBenchmark() took %v, want under a second", took)
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

// useFakeDoT points every server of a run at f.
func useFakeDoT(t *testing.T, f *fakeDoT) {
	t.Helper()
	saved := newResolverFor
	t.Cleanup(func() { newResolverFor = saved })
	newResolverFor = func(server DNSServer, concurrency int) *Resolver {
		return newResolver(server.Addr, f.hostPort, f.config(), concurrency)
	}
}

// Each resolver gets every domain Repeats times, plus WarmupRuns warmup
// lookups of each domain before its first measured lookup.
func TestRunBenchmark_CountsLookupsAndWarmups(t *testing.T) {
	f := startFakeDoTWith(t, false)
	useFakeDoT(t, f)

	cfg := &Config{Repeats: 2, WarmupRuns: 1, LookupTimeout: 2 * time.Second, MaxConcurrency: 4}
	servers := []DNSServer{{Name: "a", Addr: "127.0.0.1"}, {Name: "b", Addr: "127.0.0.2"}}
	domains := []string{"one.example", "two.example", "three.example"}

	results, err := runBenchmark(t.Context(), cfg, servers, domains, nil)
	if err != nil {
		t.Fatalf("runBenchmark() error = %v", err)
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
	if got := f.queries.Load(); got != 18 {
		t.Errorf("server saw %d queries, want 18", got)
	}
}

// cancelAfter cancels a run after its first n lookups.
type cancelAfter struct {
	NoopReporter
	n      atomic.Int32
	cancel context.CancelFunc
}

func (r *cancelAfter) OnQueryResult(_ DNSServer, _ string, _ float64, _ int, _ error) {
	if r.n.Add(-1) == 0 {
		r.cancel()
	}
}

// Stopping a run must not count the lookups it cut short as failures.
func TestRunBenchmark_CancelDoesNotCountFailures(t *testing.T) {
	f := startFakeDoTWith(t, false)
	useFakeDoT(t, f)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reporter := &cancelAfter{cancel: cancel}
	reporter.n.Store(3)

	cfg := &Config{Repeats: 20, LookupTimeout: 2 * time.Second, MaxConcurrency: 2}
	servers := []DNSServer{{Name: "a", Addr: "127.0.0.1"}, {Name: "b", Addr: "127.0.0.2"}}
	results, err := runBenchmark(ctx, cfg, servers, []string{"one.example", "two.example"}, reporter)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("runBenchmark() error = %v, want context.Canceled", err)
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

// useSilentDNS points every server of a run at a UDP server that reads
// queries and never answers.
func useSilentDNS(t *testing.T) *atomic.Int32 {
	t.Helper()
	addr, queries := startSilentDNS(t)
	saved := newResolverFor
	t.Cleanup(func() { newResolverFor = saved })
	newResolverFor = func(server DNSServer, concurrency int) *Resolver {
		return newResolver(server.Addr, addr, nil, concurrency)
	}
	return queries
}

// A resolver that passes the precheck but never answers must not cost
// every lookup its full timeout. The run gives up on it after giveUpAfter
// failed lookups and fails the rest at once.
func TestRunBenchmark_GivesUpOnSilentResolver(t *testing.T) {
	queries := useSilentDNS(t)

	cfg := &Config{Repeats: 10, LookupTimeout: 100 * time.Millisecond, Retries: 0, MaxConcurrency: 2}
	domains := []string{"one.example", "two.example", "three.example", "four.example"}
	reporter := &countingReporter{}

	start := time.Now()
	results, err := runBenchmark(t.Context(), cfg, []DNSServer{{Name: "silent", Addr: "127.0.0.1"}}, domains, reporter)
	if err != nil {
		t.Fatalf("runBenchmark() error = %v", err)
	}
	// Forty lookups at 100 ms each, two at a time, would take two seconds.
	if took := time.Since(start); took > time.Second {
		t.Errorf("runBenchmark() took %v, want it to give up well before a second", took)
	}
	if stats := results[0].Stats; stats.Errors != 40 || stats.Total != 40 {
		t.Errorf("stats = %+v, want 40 failed lookups of 40", stats)
	}
	// The lookups in flight when the run gave up had sent their queries.
	if got := int(queries.Load()); got > giveUpAfter+cfg.MaxConcurrency {
		t.Errorf("server saw %d queries, want about %d before the run gave up", got, giveUpAfter)
	}
	if msg, ok := reporter.err.Load().(string); !ok || !strings.Contains(msg, "gave up") {
		t.Errorf("last reported error = %q, want it to say the run gave up", msg)
	}
}
