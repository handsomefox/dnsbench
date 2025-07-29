package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"runtime"
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
	Server     DNSServer
	Stats      Stats
	DomainMean map[string]float64
}

// Stats contains latency statistics for a resolver
type Stats struct {
	Min    float64
	Max    float64
	Mean   float64
	Median float64
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

func runBenchmark(ctx context.Context, config Config, servers []DNSServer, domains []string) []BenchmarkResult {
	results := make([]BenchmarkResult, len(servers))
	for i, server := range servers {
		if cErr := ctx.Err(); cErr != nil {
			slog.Error("Context error, ending the benchmark", "error", cErr)
			return results
		}

		slog.Info("Benchmarking resolver", "name", server.Name, "addr", server.Addr)

		start := time.Now()

		stats, domainMeans := benchmarkResolver(ctx, config, server, domains)
		results[i] = BenchmarkResult{
			Server:     server,
			Stats:      stats,
			DomainMean: domainMeans,
		}

		took := time.Since(start)

		slog.Info("Finished benchmarking resolver", "name", server.Name, "addr", server.Addr, "took_ms", took.Milliseconds())

		runtime.GC()
		runtime.GC()
		time.Sleep(time.Second)
	}
	return results
}

func benchmarkResolver(ctx context.Context, config Config, server DNSServer, domains []string) (Stats, map[string]float64) {
	type result struct {
		domain  string
		latency float64
		err     error
	}

	total := len(domains) * config.Repeats
	results := make(chan result, total)

	errg, ctx := errgroup.WithContext(ctx)
	errg.SetLimit(config.MaxConcurrency)

	for range config.Repeats {
		for _, domain := range domains {
			errg.Go(func() error {
				lat, err := queryDNS(ctx, domain, server.Addr, config.LookupTimeout)
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
		_ = errg.Wait()
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

	// compute per‐domain means
	domainMeans := make(map[string]float64, len(perDomainLats))
	for d, lats := range perDomainLats {
		sum := 0.0
		for _, v := range lats {
			sum += v
		}
		domainMeans[d] = sum / float64(len(lats))
	}

	stats := calculateStats(allLatencies, errorCount, total)
	return stats, domainMeans
}

func queryDNS(
	ctx context.Context,
	domain, resolver string,
	timeout time.Duration,
) (time.Duration, error) {
	dialer := &net.Dialer{}
	netResolver := &net.Resolver{
		PreferGo: true,
		Dial: func(dialCtx context.Context, network, address string) (net.Conn, error) {
			return dialer.DialContext(dialCtx, "udp", net.JoinHostPort(resolver, "53"))
		},
	}

	log := slog.With("domain", domain, "resolver", resolver)

	try := func(attempt int) (time.Duration, error) {
		log := log.With("attempt", attempt)

		if attempt > 0 {
			log.Debug("Attempting query again")
		}

		attemptCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		start := time.Now()
		addrs, err := netResolver.LookupHost(attemptCtx, domain)
		took := time.Since(start)

		if err != nil {
			log.Debug("Failed query", "error", err)
			return took, err
		}

		if took > timeout {
			log.Debug("Query exceeded timeout", "took_ms", took.Milliseconds())
			return took, context.DeadlineExceeded
		}

		if len(addrs) == 0 {
			log.Debug("No addresses found")
			return took, fmt.Errorf("no addresses found for domain %s by resolver %s", domain, resolver)
		}

		if took > 200*time.Millisecond {
			log.Debug("Slow query", "took_ms", took.Milliseconds())
		}

		return took, nil
	}

	elapsed, err := retryWithBackoff(ctx, try, 10, 2*time.Second, 60*time.Second) // Delay from 2 to 60 seconds, max 10 tries
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return 0, fmt.Errorf("DNS query timeout for %s via %s: %w", domain, resolver, err)
		}
		return 0, fmt.Errorf("DNS query failed for %s via %s: %w", domain, resolver, err)
	}

	return elapsed, nil
}

func calculateStats(latencies []float64, errors, total int) Stats {
	if len(latencies) == 0 {
		return Stats{
			Min:    math.NaN(),
			Max:    math.NaN(),
			Mean:   math.NaN(),
			Median: math.NaN(),
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

	median := latencies[len(latencies)/2]
	if len(latencies)%2 == 0 && len(latencies) > 1 {
		median = (latencies[len(latencies)/2-1] + latencies[len(latencies)/2]) / 2
	}

	return Stats{
		Min:    latencies[0],
		Max:    latencies[len(latencies)-1],
		Mean:   sum / float64(len(latencies)),
		Median: median,
		Count:  len(latencies),
		Errors: errors,
		Total:  total,
	}
}
