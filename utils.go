package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"
)

func printSummary(results []BenchmarkResult, outputType OutputType) {
	if len(results) == 0 {
		fmt.Println("\nNo benchmark results to display")
		return
	}

	var valid, failed []BenchmarkResult
	for _, r := range results {
		if r.Stats.IsValid() {
			valid = append(valid, r)
		} else {
			failed = append(failed, r)
		}
	}

	sort.Slice(valid, func(i, j int) bool {
		vi, vj := valid[i].Stats.SuccessRate(), valid[j].Stats.SuccessRate()
		if vi == vj {
			return valid[i].Stats.Mean < valid[j].Stats.Mean
		}
		return vi > vj
	})

	printByType(outputType, valid, failed)
}

func printByType(t OutputType, valid, failed []BenchmarkResult) {
	switch t {
	case OutputCSV:
		printResultsCSV(os.Stdout, valid, false)
		printResultsCSV(os.Stderr, failed, true)
	case OutputTable:
		printResultsTable(os.Stdout, valid, false)
		printResultsTable(os.Stderr, failed, true)
	default:
		printDefaultSummary(valid, failed)
	}
}

func printResultsCSV(w io.Writer, results []BenchmarkResult, failed bool) {
	if len(results) == 0 {
		return
	}
	if failed {
		fmt.Fprintln(w, "\nFailed resolvers:")
		fmt.Fprintln(w, "Resolver,Address,Errors,Total")
		for _, r := range results {
			fmt.Fprintf(w, "%s,%s,%d,%d\n", r.Server.Name, r.Server.Addr, r.Stats.Errors, r.Stats.Total)
		}
		return
	}
	fmt.Fprintln(w, "Resolver,Success Rate,Mean (ms),Min (ms),Max (ms),Total Queries")
	for _, r := range results {
		fmt.Fprintf(w, "%s,%.1f,%.2f,%.2f,%.2f,%d\n",
			r.Server.Name,
			r.Stats.SuccessRate()*100,
			r.Stats.Mean,
			r.Stats.Min,
			r.Stats.Max,
			r.Stats.Total)
	}
}

func printResultsTable(w io.Writer, results []BenchmarkResult, failed bool) {
	if len(results) == 0 {
		return
	}
	if failed {
		fmt.Fprintln(w, "\nFailed resolvers:")
		fmt.Fprintf(w, "%-20s %-15s %10s %10s\n", "Resolver", "Address", "Errors", "Total")
		for _, r := range results {
			fmt.Fprintf(w, "%-20s %-15s %10d %10d\n",
				truncateString(r.Server.Name, 20), r.Server.Addr, r.Stats.Errors, r.Stats.Total)
		}
		return
	}
	fmt.Fprintf(w, "%-20s %10s %10s %10s %10s %10s\n",
		"Resolver", "Success%", "Mean(ms)", "Min(ms)", "Max(ms)", "Queries")
	fmt.Fprintf(w, "%s\n", strings.Repeat("-", 80))
	for _, r := range results {
		fmt.Fprintf(w, "%-20s %9.1f%% %9.2f %9.2f %9.2f %10d\n",
			truncateString(r.Server.Name, 20),
			r.Stats.SuccessRate()*100,
			r.Stats.Mean,
			r.Stats.Min,
			r.Stats.Max,
			r.Stats.Total)
	}
}

func printDefaultSummary(valid, failed []BenchmarkResult) {
	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("DNS BENCHMARK RESULTS - TOP PERFORMERS")
	fmt.Println(strings.Repeat("=", 80))
	printResultsTable(os.Stdout, valid, false)
	if len(failed) > 0 {
		fmt.Println(strings.Repeat("-", 80))
		fmt.Println("\nFAILED RESOLVERS:")
		fmt.Println(strings.Repeat("-", 80))
		printResultsTable(os.Stdout, failed, true)
	}
	if len(valid) > 0 {
		fmt.Println(strings.Repeat("-", 80))
		fmt.Printf("Summary: %d resolvers tested successfully, %d failed\n", len(valid), len(failed))
		fmt.Printf("Each resolver processed %d total queries\n", valid[0].Stats.Total)
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
