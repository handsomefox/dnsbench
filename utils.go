package main

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"runtime"
	"sort"
	"strings"
	"time"
)

func printSummary(results []BenchmarkResult) {
	// Filter valid results and sort by success rate, then by mean latency
	var validResults []BenchmarkResult
	var failedResults []BenchmarkResult
	for _, result := range results {
		if result.Stats.IsValid() {
			validResults = append(validResults, result)
		} else {
			failedResults = append(failedResults, result)
		}
	}

	sort.Slice(validResults, func(i, j int) bool {
		iRate := validResults[i].Stats.SuccessRate()
		jRate := validResults[j].Stats.SuccessRate()

		if iRate == jRate {
			return validResults[i].Stats.Mean < validResults[j].Stats.Mean
		}
		return iRate > jRate
	})

	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("DNS BENCHMARK RESULTS - TOP PERFORMERS")
	fmt.Println(strings.Repeat("=", 80))
	fmt.Printf("%-20s %10s %10s %10s %10s\n",
		"Resolver", "Success%", "Mean(ms)", "Min(ms)", "Max(ms)")
	fmt.Println(strings.Repeat("-", 80))

	for i := range validResults {
		result := validResults[i]
		fmt.Printf("%-20s %9.1f%% %9.2f %9.2f %9.2f\n",
			truncateString(result.Server.Name, 20),
			result.Stats.SuccessRate()*100,
			result.Stats.Mean,
			result.Stats.Min,
			result.Stats.Max)
	}

	// Add failed resolvers section
	if len(failedResults) > 0 {
		fmt.Println(strings.Repeat("-", 80))
		fmt.Println("\nFAILED RESOLVERS:")
		fmt.Println(strings.Repeat("-", 80))
		fmt.Printf("%-20s %10s %10s\n", "Resolver", "Address", "Errors")
		fmt.Println(strings.Repeat("-", 80))

		for _, result := range failedResults {
			fmt.Printf("%-20s %10s %10d\n",
				truncateString(result.Server.Name, 20),
				result.Server.Addr,
				result.Stats.Errors)
		}
	}

	if len(validResults) > 0 {
		fmt.Println(strings.Repeat("-", 80))
		fmt.Printf("Tested %d resolvers with %d total queries each\n",
			len(results),
			validResults[0].Stats.Total)
	}
}

func retryWithBackoff[T any](
	ctx context.Context,
	f func(attempt int) (T, error),
	maxRetries int,
	initialBackoff time.Duration,
	maxBackoff time.Duration,
) (val T, err error) {
	backoff := min(initialBackoff, maxBackoff)

	for attempt := range maxRetries {
		if cErr := ctx.Err(); cErr != nil {
			return val, cErr
		}

		val, err = f(attempt)
		if err == nil {
			return val, nil
		}

		jitter := time.Duration(rand.N(backoff))
		wait := backoff/2 + jitter

		select {
		case <-ctx.Done():
			return val, ctx.Err()
		case <-time.After(wait):
		}

		backoff = min(backoff*2, maxBackoff)
	}

	return val, err
}

func isValidDomain(domain string) bool {
	if len(domain) == 0 || len(domain) > 253 {
		return false
	}
	return !strings.Contains(domain, " ") && strings.Contains(domain, ".")
}

func formatFloat(f float64) string {
	if math.IsNaN(f) {
		return ""
	}
	return fmt.Sprintf("%.2f", f)
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func gcAndWait() {
	runtime.GC()
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
}

func slogErr(err error) slog.Attr {
	if err != nil {
		return slog.String("err", err.Error())
	}
	return slog.String("err", "<nil>")
}
