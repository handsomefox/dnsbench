package main

import (
	"context"
	"log/slog"
	"math"
	"sort"
	"time"

	"golang.org/x/sync/errgroup"
)

// DNSServer represents a resolver to be benchmarked
type DNSServer struct {
	Name string
	Addr string
}

// BenchmarkResult contains the results for a single resolver
type BenchmarkResult struct {
	Server DNSServer
	Stats  Stats
}

// Stats contains latency statistics for a resolver
type Stats struct {
	Min    float64
	Max    float64
	Mean   float64
	Count  int
	Errors int
	Total  int
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

func runBenchmark(ctx context.Context, config *Config, servers []DNSServer, domains []string) []BenchmarkResult {
	results := make([]BenchmarkResult, len(servers))
	for i, server := range servers {
		if cErr := ctx.Err(); cErr != nil {
			slog.LogAttrs(ctx, slog.LevelError, "Context error, ending the benchmark", slogErr(cErr))
			return results
		}

		slog.LogAttrs(ctx, slog.LevelInfo, "Benchmarking resolver",
			slog.String("name", server.Name),
			slog.String("addr", server.Addr),
		)

		start := time.Now()

		stats := benchmarkResolver(ctx, config, server, domains)
		results[i] = BenchmarkResult{
			Server: server,
			Stats:  stats,
		}

		took := time.Since(start)

		slog.LogAttrs(ctx, slog.LevelInfo, "Finished benchmarking resolver",
			slog.String("name", server.Name),
			slog.String("addr", server.Addr),
			slog.Int64("took_ms", took.Milliseconds()),
		)

		// Cool off after each server.
		gcAndWait()
	}
	return results
}

func benchmarkResolver(ctx context.Context, config *Config, server DNSServer, domains []string) Stats {
	type result struct {
		domain  string
		latency float64
		err     error
	}

	total := len(domains) * config.Repeats
	results := make(chan result, total)

	errg, ctx := errgroup.WithContext(ctx)
	errg.SetLimit(config.MaxConcurrency)

	// Create resolver once.
	resolver := NewResolver(server.Addr)

	for range config.Repeats {
		for _, domain := range domains {
			errg.Go(func() error {
				lat, err := resolver.QueryDNS(ctx, domain, config.LookupTimeout)
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
			panic(err)
		}
		close(results)
	}()

	var (
		allLatencies  = make([]float64, 0, total)
		perDomainLats = make(map[string][]float64, len(domains))
		errorCount    int
	)
	for r := range results {
		if r.err != nil {
			errorCount++
			continue
		}
		allLatencies = append(allLatencies, r.latency)
		perDomainLats[r.domain] = append(perDomainLats[r.domain], r.latency)
	}

	return calculateStats(allLatencies, errorCount, total)
}

func calculateStats(latencies []float64, errors, total int) Stats {
	if len(latencies) == 0 {
		return Stats{
			Min:    math.NaN(),
			Max:    math.NaN(),
			Mean:   math.NaN(),
			Count:  0,
			Errors: errors,
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
		Errors: errors,
		Total:  total,
	}
}
