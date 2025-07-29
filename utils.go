package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"runtime"
	"sort"
	"strings"
	"time"
)

func printSummary(results []BenchmarkResult) {
	if len(results) == 0 {
		fmt.Println("\nNo benchmark results to display")
		return
	}

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
	fmt.Printf("%-20s %10s %10s %10s %10s %10s\n",
		"Resolver", "Success%", "Mean(ms)", "Min(ms)", "Max(ms)", "Queries")
	fmt.Println(strings.Repeat("-", 80))

	for _, result := range validResults {
		fmt.Printf("%-20s %9.1f%% %9.2f %9.2f %9.2f %10d\n",
			truncateString(result.Server.Name, 20),
			result.Stats.SuccessRate()*100,
			result.Stats.Mean,
			result.Stats.Min,
			result.Stats.Max,
			result.Stats.Total)
	}

	if len(failedResults) > 0 {
		fmt.Println(strings.Repeat("-", 80))
		fmt.Println("\nFAILED RESOLVERS:")
		fmt.Println(strings.Repeat("-", 80))
		fmt.Printf("%-20s %-15s %10s %10s\n",
			"Resolver", "Address", "Errors", "Total")
		fmt.Println(strings.Repeat("-", 80))

		for _, result := range failedResults {
			fmt.Printf("%-20s %-15s %10d %10d\n",
				truncateString(result.Server.Name, 20),
				result.Server.Addr,
				result.Stats.Errors,
				result.Stats.Total)
		}
	}

	if len(validResults) > 0 {
		fmt.Println(strings.Repeat("-", 80))
		fmt.Printf("Summary: %d resolvers tested successfully, %d failed\n",
			len(validResults), len(failedResults))
		fmt.Printf("Each resolver processed %d total queries\n",
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
	if maxRetries < 1 {
		return val, errors.New("maxRetries must be positive")
	}

	backoff := min(initialBackoff, maxBackoff)

	for attempt := range maxRetries {
		if cErr := ctx.Err(); cErr != nil {
			return val, cErr
		}

		val, err = f(attempt)
		if err == nil {
			return val, nil
		}

		if attempt == maxRetries-1 {
			break
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
	return !strings.Contains(domain, " ") &&
		strings.Contains(domain, ".") &&
		!strings.HasPrefix(domain, ".") &&
		!strings.HasSuffix(domain, ".")
}

func truncateString(s string, maxLen int) string {
	if maxLen < 4 {
		return s
	}
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
