package main

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"sort"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

// DNSServer represents a resolver to be benchmarked
type DNSServer struct {
	Name string `json:"name"`
	Addr string `json:"addr"`
}

// BenchmarkResult contains the results for a single resolver
type BenchmarkResult struct {
	Server DNSServer `json:"server"`
	Stats  Stats     `json:"stats"`
}

// Stats contains latency statistics for a resolver
type Stats struct {
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	Mean   float64 `json:"mean"`
	Count  int     `json:"count"`
	Errors int     `json:"errors"`
	Total  int     `json:"total"`
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

	reporter.OnStart(len(servers), domains)

	results := make([]BenchmarkResult, 0, len(servers))
	var runErr error

	for i, server := range servers {
		if cErr := ctx.Err(); cErr != nil {
			runErr = cErr
			slog.LogAttrs(ctx, slog.LevelWarn, "Benchmark canceled", slogErr(cErr))
			break
		}

		slog.LogAttrs(ctx, slog.LevelInfo, "Benchmarking resolver",
			slog.String("name", server.Name),
			slog.String("addr", server.Addr),
			slog.Int("progress", i+1),
			slog.Int("total", len(servers)),
		)

		reporter.OnResolverStart(server, i+1, len(servers))

		start := time.Now()

		stats := benchmarkResolver(ctx, config, server, domains, reporter)
		results = append(results, BenchmarkResult{
			Server: server,
			Stats:  stats,
		})

		took := time.Since(start)
		slog.LogAttrs(ctx, slog.LevelInfo, "Finished benchmarking resolver",
			slog.String("name", server.Name),
			slog.String("addr", server.Addr),
			slog.Int64("took_ms", took.Milliseconds()),
			slog.Float64("success_rate", stats.SuccessRate()*100),
		)

		reporter.OnResolverDone(server, stats, took)

		// Cool off after each server.
		gcAndWait()
	}

	reporter.OnComplete(results, runErr)
	return results, runErr
}

func benchmarkResolver(ctx context.Context, config *Config, server DNSServer, domains []string, reporter BenchmarkReporter) Stats {
	type result struct {
		domain  string
		latency float64
		err     error
	}

	total := len(domains) * config.Repeats
	results := make(chan result, total)

	errg, ctx := errgroup.WithContext(ctx)
	resolver := NewResolver(server.Addr, config.MaxConcurrency)

	for range config.Repeats {
		for _, domain := range domains {
			errg.Go(func() error {
				// Do warmup for this domain if configured
				if config.WarmupRuns > 0 {
					doWarmupRuns(ctx, resolver, domain, config.WarmupRuns)
				}

				lat, err := resolver.QueryDNS(ctx, domain, config.LookupTimeout, ResolverRetryEnabled)
				if err != nil {
					results <- result{domain: domain, err: err}
				} else {
					results <- result{
						domain:  domain,
						latency: lat.Seconds() * 1000,
					}
				}
				return nil
			})
		}
	}

	// once all lookups are done (or parent ctx canceled), close the channel
	go func() {
		if err := errg.Wait(); err != nil {
			slog.LogAttrs(ctx, slog.LevelError, "Unexpected worker pool error", slogErr(err))
		}
		close(results)
	}()

	var (
		allLatencies = make([]float64, 0, total)
		errorCount   int
	)

	// Collect results
	for r := range results {
		if r.err != nil {
			errorCount++
			reporter.OnQueryResult(server, r.domain, 0, r.err)
			continue
		}
		allLatencies = append(allLatencies, r.latency)
		reporter.OnQueryResult(server, r.domain, r.latency, nil)
	}

	return calculateStats(allLatencies, errorCount, total)
}

func doWarmupRuns(ctx context.Context, resolver *Resolver, domain string, warmupRuns int) {
	if warmupRuns <= 0 {
		return
	}

	slog.LogAttrs(ctx, slog.LevelDebug, "Performing warmup queries",
		slog.Int("warmup_runs", warmupRuns),
		slog.String("domain", domain),
		slog.String("resolver", resolver.serverAddr),
	)

	var wg sync.WaitGroup
	wg.Add(warmupRuns)

	for range warmupRuns {
		go func() {
			defer wg.Done()

			// Perform a warmup query
			if _, err := resolver.QueryDNS(ctx, domain, time.Second, ResolverRetryDisabled); err != nil {
				slog.LogAttrs(ctx, slog.LevelDebug, "Warmup query failed", slogErr(err))
			}
		}()
	}

	wg.Wait()

	gcAndWait()
}

func calculateStats(latencies []float64, errs, total int) Stats {
	if len(latencies) == 0 {
		return Stats{
			Min:    math.NaN(),
			Max:    math.NaN(),
			Mean:   math.NaN(),
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
		Count:  len(latencies),
		Errors: errs,
		Total:  total,
	}
}
