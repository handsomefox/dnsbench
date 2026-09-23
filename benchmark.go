package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"slices"
	"sort"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

// DNSServer represents a resolver to be benchmarked. A resolver with a
// TLSName is queried with DNS over TLS on port 853, and TLSName is the name
// its certificate must match. A resolver with a DoHURL is queried with DNS
// over HTTPS at that URL, through Addr on port 443. A resolver with a
// DoQName is queried with DNS over QUIC on UDP port 853, and DoQName is the
// name its certificate must match. Without any of them it gets plain DNS on
// port 53. At most one of TLSName, DoHURL, and DoQName is set.
type DNSServer struct {
	Name    string `json:"name"`
	Addr    string `json:"addr"`
	TLSName string `json:"tlsName,omitempty"`
	DoHURL  string `json:"dohURL,omitempty"`
	DoQName string `json:"doqName,omitempty"`
}

// BenchmarkResult contains the results for a single resolver
type BenchmarkResult struct {
	Server DNSServer `json:"server"`
	Stats  Stats     `json:"stats"`
}

// Stats contains latency statistics for a resolver
type Stats struct {
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
	Mean float64 `json:"mean"`
	// Median and P95 are the 50th and 95th percentiles, interpolated
	// between the two nearest lookups, as the dashboard computes them.
	Median float64 `json:"median"`
	P95    float64 `json:"p95"`
	Count  int     `json:"count"`
	Errors int     `json:"errors"`
	Total  int     `json:"total"`
	// Retried counts the successful lookups that needed more than one
	// attempt.
	Retried int `json:"retried"`
}

// MarshalJSON encodes the latency fields as null when no lookup succeeded,
// where they would be NaN, which encoding/json rejects outright.
// The receiver is a value so the method also applies inside maps and
// interfaces, where the SSE reporter puts Stats.
func (s Stats) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Min     *float64 `json:"min"`
		Max     *float64 `json:"max"`
		Mean    *float64 `json:"mean"`
		Median  *float64 `json:"median"`
		P95     *float64 `json:"p95"`
		Count   int      `json:"count"`
		Errors  int      `json:"errors"`
		Total   int      `json:"total"`
		Retried int      `json:"retried"`
	}{
		Min:     s.latency(s.Min),
		Max:     s.latency(s.Max),
		Mean:    s.latency(s.Mean),
		Median:  s.latency(s.Median),
		P95:     s.latency(s.P95),
		Count:   s.Count,
		Errors:  s.Errors,
		Total:   s.Total,
		Retried: s.Retried,
	})
}

// latency returns f for JSON, or nil when no lookup succeeded or f is not
// finite.
func (s Stats) latency(f float64) *float64 {
	if s.Count == 0 {
		return nil
	}
	return finiteOrNil(f)
}

func finiteOrNil(f float64) *float64 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil
	}
	return &f
}

// IsValid returns true if the stats contain valid data
func (s Stats) IsValid() bool {
	return s.Count > 0 && !math.IsNaN(s.Mean)
}

// SuccessRate returns the success rate as a percentage
func (s Stats) SuccessRate() float64 {
	if s.Total == 0 {
		return 0
	}
	return float64(s.Count) / float64(s.Total)
}

// newResolverFor builds the resolver for each server of a run. Tests
// replace it to point a run at local fake servers.
var newResolverFor = NewResolver

// giveUpAfter is how many lookups in a row may fail before a run gives up
// on a resolver and fails the rest of its lookups at once. Answers about
// the name, such as NXDOMAIN, do not count. Without it, a resolver behind a
// firewall that drops its queries would cost every one of its lookups the
// full timeout of every attempt.
const giveUpAfter = 8

// precheckLimit caps how many resolvers are prechecked at once. A precheck
// can wait for a TLS handshake, so running them together saves time.
const precheckLimit = 16

// runBenchmark measures every server against every domain, config.Repeats
// times, and returns one result per server in the order given.
//
// Lookups interleave. Each round visits the domains in a new random order,
// and for each domain the servers in a new random order, so no server
// always goes first for a domain or always runs while the network is
// busy. Up to config.MaxConcurrency lookups run at once, across all
// servers. In the first round, a server's lookup of a domain starts with
// config.WarmupRuns unmeasured lookups of the same domain.
func runBenchmark(ctx context.Context, config *Config, servers []DNSServer, domains []string, reporter BenchmarkReporter) ([]BenchmarkResult, error) {
	if len(servers) == 0 {
		return nil, errors.New("no DNS servers provided")
	}
	if len(domains) == 0 {
		return nil, errors.New("no domains provided")
	}
	if reporter == nil {
		reporter = NoopReporter{}
	}
	// Workers finish lookups in parallel. The reporters expect one call at
	// a time.
	reporter = &serialReporter{next: reporter}

	reporter.OnStart(len(servers), domains)
	slog.LogAttrs(ctx, slog.LevelInfo, "Starting benchmark",
		slog.Int("resolvers", len(servers)),
		slog.Int("domains", len(domains)),
		slog.Int("lookups", len(servers)*len(domains)*config.Repeats),
	)

	runs := make([]*resolverRun, len(servers))
	for i, server := range servers {
		runCtx, cancel := context.WithCancelCause(ctx)
		runs[i] = &resolverRun{
			ctx:       runCtx,
			giveUp:    cancel,
			server:    server,
			resolver:  newResolverFor(server, config.MaxConcurrency),
			planned:   len(domains) * config.Repeats,
			remaining: len(domains) * config.Repeats,
			start:     time.Now(),
		}
		reporter.OnResolverStart(server, i+1, len(servers))
	}
	defer func() {
		for _, run := range runs {
			run.giveUp(nil)
			run.resolver.Close()
		}
	}()

	live := precheckAll(ctx, runs, domains, config.Repeats, reporter)

	jobs := make(chan lookupJob)
	var wg sync.WaitGroup
	for range max(config.MaxConcurrency, 1) {
		wg.Go(func() {
			for job := range jobs {
				job.run.lookup(ctx, config, job, reporter)
			}
		})
	}

feed:
	for round := range config.Repeats {
		for _, domain := range shuffled(domains) {
			for _, run := range shuffled(live) {
				job := lookupJob{run: run, domain: domain}
				if round == 0 {
					job.warmup = config.WarmupRuns
				}
				select {
				case jobs <- job:
				case <-ctx.Done():
					break feed
				}
			}
		}
	}
	close(jobs)
	wg.Wait()

	runErr := ctx.Err()
	if runErr != nil {
		slog.LogAttrs(ctx, slog.LevelWarn, "Benchmark canceled", slogErr(runErr))
	}
	results := make([]BenchmarkResult, len(runs))
	for i, run := range runs {
		results[i] = BenchmarkResult{Server: run.server, Stats: run.stats()}
	}
	reporter.OnComplete(results, runErr)
	return results, runErr
}

// precheckAll prechecks every resolver, a few at a time, and returns the
// ones that passed. A resolver that fails cannot answer however often it
// is asked, so every one of its planned lookups fails at once, and the
// reporter still sees one result per lookup.
func precheckAll(ctx context.Context, runs []*resolverRun, domains []string, repeats int, reporter BenchmarkReporter) []*resolverRun {
	errs := make([]error, len(runs))
	var g errgroup.Group
	g.SetLimit(precheckLimit)
	for i, run := range runs {
		g.Go(func() error {
			errs[i] = run.resolver.Precheck(ctx)
			return nil
		})
	}
	_ = g.Wait() //nolint:errcheck // the goroutines keep their errors in errs

	live := make([]*resolverRun, 0, len(runs))
	for i, run := range runs {
		if errs[i] == nil {
			live = append(live, run)
			continue
		}
		slog.LogAttrs(ctx, slog.LevelWarn, "Skipping resolver that cannot answer",
			slog.String("name", run.server.Name),
			slogErr(errs[i]),
		)
		run.giveUp(errs[i])
		for range repeats {
			for _, domain := range domains {
				run.record(ctx, domain, Lookup{}, errs[i], reporter)
			}
		}
	}
	return live
}

func shuffled[T any](items []T) []T {
	out := slices.Clone(items)
	rand.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] }) //nolint:gosec // lookup order, not a secret
	return out
}

// lookupJob is one measured lookup of domain against run's resolver,
// after warmup unmeasured ones.
type lookupJob struct {
	run    *resolverRun
	domain string
	warmup int
}

// resolverRun collects one resolver's lookups during a run.
type resolverRun struct {
	// ctx ends with the run, or when the run gives up on this resolver.
	ctx    context.Context
	giveUp context.CancelCauseFunc

	server   DNSServer
	resolver *Resolver
	planned  int
	start    time.Time

	mu         sync.Mutex
	latencies  []float64
	errors     int
	retried    int
	remaining  int
	failStreak int // lookups in a row that failed, not counting final answers
}

// lookup runs job's warmup lookups, then its measured lookup, and records
// the result. A lookup that the run's cancellation cut short is not
// recorded: it says nothing about the resolver. After the run gives up on
// the resolver, its lookups fail at once with the reason.
//
// The lookups use r.ctx, which derives from the run's ctx and also ends
// when the run gives up on this resolver.
//
//nolint:contextcheck // r.ctx is a child of ctx, kept per resolver
func (r *resolverRun) lookup(ctx context.Context, config *Config, job lookupJob, reporter BenchmarkReporter) {
	if ctx.Err() != nil {
		return
	}
	if cause := context.Cause(r.ctx); errors.Is(cause, errGaveUp) {
		r.record(ctx, job.domain, Lookup{}, cause, reporter)
		return
	}
	warmUp(r.ctx, r.resolver, job.domain, job.warmup)
	result, err := r.resolver.QueryDNS(r.ctx, job.domain, config.LookupTimeout, config.Retries)
	if err != nil && ctx.Err() != nil {
		return
	}
	if cause := context.Cause(r.ctx); err != nil && errors.Is(cause, errGaveUp) {
		err = cause
	}
	r.record(ctx, job.domain, result, err, reporter)
}

// errGaveUp is the error of every lookup after a run gives up on a
// resolver.
var errGaveUp = fmt.Errorf("gave up on the resolver after %d lookups in a row failed", giveUpAfter)

// record adds one lookup to the resolver's results and reports it. After
// the resolver's last planned lookup, it reports the resolver done.
func (r *resolverRun) record(ctx context.Context, domain string, result Lookup, err error, reporter BenchmarkReporter) {
	r.mu.Lock()
	latency := result.Latency.Seconds() * 1000
	giveUp := false
	switch {
	case err == nil:
		r.latencies = append(r.latencies, latency)
		if result.Attempts > 1 {
			r.retried++
		}
		r.failStreak = 0
	case isFinalAnswer(err), r.ctx.Err() != nil:
		// An answer about the name, or a lookup after the run gave up on
		// the resolver, says nothing new about whether it answers.
		r.errors++
		latency = 0
	default:
		r.errors++
		latency = 0
		r.failStreak++
		giveUp = r.failStreak == giveUpAfter
	}
	r.remaining--
	done := r.remaining == 0
	r.mu.Unlock()

	if giveUp {
		slog.LogAttrs(ctx, slog.LevelWarn, "Giving up on resolver",
			slog.String("name", r.server.Name),
			slog.Int("failed_in_a_row", giveUpAfter),
		)
		// Cut the lookups in flight short too. They fail with errGaveUp.
		r.giveUp(errGaveUp)
	}

	reporter.OnQueryResult(r.server, domain, latency, result.Attempts, err)
	if done {
		stats := r.stats()
		took := time.Since(r.start)
		slog.LogAttrs(ctx, slog.LevelInfo, "Finished resolver",
			slog.String("name", r.server.Name),
			slog.String("addr", r.server.Addr),
			slog.Float64("success_rate", stats.SuccessRate()*100),
		)
		reporter.OnResolverDone(r.server, stats, took)
	}
}

// stats returns the resolver's statistics so far. Total counts the lookups
// that ran, which is every planned lookup unless the run was canceled.
func (r *resolverRun) stats() Stats {
	r.mu.Lock()
	defer r.mu.Unlock()
	stats := calculateStats(slices.Clone(r.latencies), r.errors, len(r.latencies)+r.errors)
	stats.Retried = r.retried
	return stats
}

// warmUp sends runs unmeasured lookups of domain to resolver, one after
// another. They put the answer in the resolver's cache and, for DoT, DoH,
// and DoQ, open the connection the measured lookups reuse. Each has a
// one-second timeout and no retries, and its result is discarded.
func warmUp(ctx context.Context, resolver *Resolver, domain string, runs int) {
	for range runs {
		if _, err := resolver.QueryDNS(ctx, domain, time.Second, 0); err != nil {
			slog.LogAttrs(ctx, slog.LevelDebug, "Warmup query failed",
				slog.String("domain", domain),
				slog.String("resolver", resolver.serverAddr),
				slogErr(err),
			)
		}
	}
}

// serialReporter passes calls to next one at a time.
type serialReporter struct {
	mu   sync.Mutex
	next BenchmarkReporter
}

func (r *serialReporter) OnStart(totalResolvers int, domains []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next.OnStart(totalResolvers, domains)
}

func (r *serialReporter) OnResolverStart(server DNSServer, index, total int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next.OnResolverStart(server, index, total)
}

func (r *serialReporter) OnQueryResult(server DNSServer, domain string, latencyMs float64, attempts int, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next.OnQueryResult(server, domain, latencyMs, attempts, err)
}

func (r *serialReporter) OnResolverDone(server DNSServer, stats Stats, took time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next.OnResolverDone(server, stats, took)
}

func (r *serialReporter) OnComplete(results []BenchmarkResult, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next.OnComplete(results, err)
}

func calculateStats(latencies []float64, errs, total int) Stats {
	if len(latencies) == 0 {
		return Stats{
			Min:    math.NaN(),
			Max:    math.NaN(),
			Mean:   math.NaN(),
			Median: math.NaN(),
			P95:    math.NaN(),
			Count:  0,
			Errors: errs,
			Total:  total,
		}
	}

	sort.Float64s(latencies)

	sum := 0.0
	for _, lat := range latencies {
		sum += lat
	}

	return Stats{
		Min:    latencies[0],
		Max:    latencies[len(latencies)-1],
		Mean:   sum / float64(len(latencies)),
		Median: percentile(latencies, 0.5),
		P95:    percentile(latencies, 0.95),
		Count:  len(latencies),
		Errors: errs,
		Total:  total,
	}
}

// percentile returns the p-th quantile of sorted, interpolated linearly
// between the two nearest values. sorted must not be empty.
func percentile(sorted []float64, p float64) float64 {
	i := float64(len(sorted)-1) * p
	lo, hi := int(math.Floor(i)), int(math.Ceil(i))
	return sorted[lo] + (sorted[hi]-sorted[lo])*(i-float64(lo))
}
