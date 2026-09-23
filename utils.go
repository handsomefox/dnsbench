package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/netip"
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
	case OutputJSON:
		printResultsJSON(valid, failed)
	default:
		printDefaultSummary(valid, failed)
	}
}

//nolint:errcheck // printing helper
func printResultsCSV(w io.Writer, results []BenchmarkResult, failed bool) {
	if len(results) == 0 {
		return
	}
	if failed {
		_, _ = fmt.Fprintln(w, "\nFailed resolvers:")
		_, _ = fmt.Fprintln(w, "Resolver,Address,Errors,Total")
		for _, r := range results {
			_, _ = fmt.Fprintf(w, "%s,%s,%d,%d\n", r.Server.Name, r.Server.Addr, r.Stats.Errors, r.Stats.Total)
		}
		return
	}
	_, _ = fmt.Fprintln(w, "Resolver,Success Rate,Mean (ms),Min (ms),Max (ms),Total Queries")
	for _, r := range results {
		_, _ = fmt.Fprintf(w, "%s,%.1f,%.2f,%.2f,%.2f,%d\n",
			r.Server.Name,
			r.Stats.SuccessRate()*100,
			r.Stats.Mean,
			r.Stats.Min,
			r.Stats.Max,
			r.Stats.Total)
	}
}

//nolint:errcheck // printing helper
func printResultsTable(w io.Writer, results []BenchmarkResult, failed bool) {
	if len(results) == 0 {
		return
	}
	if failed {
		// IPv6 addresses run longer than the 15 characters of an IPv4 one.
		addrWidth := len("255.255.255.255")
		for _, r := range results {
			addrWidth = max(addrWidth, len(r.Server.Addr))
		}
		_, _ = fmt.Fprintln(w, "\nFailed resolvers:")
		_, _ = fmt.Fprintf(w, "%-20s %-*s %10s %10s\n", "Resolver", addrWidth, "Address", "Errors", "Total")
		for _, r := range results {
			_, _ = fmt.Fprintf(w, "%-20s %-*s %10d %10d\n",
				truncateString(r.Server.Name, 20), addrWidth, r.Server.Addr, r.Stats.Errors, r.Stats.Total)
		}
		return
	}
	_, _ = fmt.Fprintf(w, "%-20s %10s %10s %10s %10s %10s\n",
		"Resolver", "Success%", "Mean(ms)", "Min(ms)", "Max(ms)", "Queries")
	_, _ = fmt.Fprintf(w, "%s\n", strings.Repeat("-", 80))
	for _, r := range results {
		_, _ = fmt.Fprintf(w, "%-20s %9.1f%% %9.2f %9.2f %9.2f %10d\n",
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

		//nolint:gosec // jitter timing here is non-security critical
		jitter := time.Duration(rand.N(int(backoff)))
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
	if domain == "" || len(domain) > 253 {
		return false
	}
	return !strings.Contains(domain, " ") &&
		strings.Contains(domain, ".") &&
		!strings.HasPrefix(domain, ".") &&
		!strings.HasSuffix(domain, ".")
}

// isValidServerAddr reports whether addr is an IP literal without a port.
// Resolver files and the Web UI API both accept only such addresses. An
// IPv6 address may carry a zone, as in fe80::1%eth0, to reach a link-local
// resolver such as a home router.
func isValidServerAddr(addr string) bool {
	_, err := netip.ParseAddr(addr)
	return err == nil
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

func printResultsJSON(valid, failed []BenchmarkResult) {
	type Summary struct {
		TotalResolvers   int              `json:"total_resolvers"`
		SuccessResolvers int              `json:"success_resolvers"`
		FailedResolvers  int              `json:"failed_resolvers"`
		OverallSuccess   float64          `json:"overall_success_rate"`
		Fastest          *BenchmarkResult `json:"fastest_resolver,omitempty"`
		Slowest          *BenchmarkResult `json:"slowest_resolver,omitempty"`
	}

	all := append([]BenchmarkResult{}, valid...)
	all = append(all, failed...)

	var fastest, slowest *BenchmarkResult
	if len(valid) > 0 {
		fastest = &valid[0]
		slowest = &valid[len(valid)-1]
	}

	totalQueries := 0
	totalSuccess := 0
	for _, r := range valid {
		totalQueries += r.Stats.Total
		totalSuccess += r.Stats.Count
	}
	overallSuccess := 0.0
	if totalQueries > 0 {
		overallSuccess = float64(totalSuccess) / float64(totalQueries)
	}

	summary := Summary{
		TotalResolvers:   len(all),
		SuccessResolvers: len(valid),
		FailedResolvers:  len(failed),
		OverallSuccess:   overallSuccess * 100,
		Fastest:          fastest,
		Slowest:          slowest,
	}

	// Encode an empty group as [] rather than null.
	if valid == nil {
		valid = []BenchmarkResult{}
	}
	if failed == nil {
		failed = []BenchmarkResult{}
	}

	output := struct {
		Summary  Summary           `json:"summary"`
		Results  []BenchmarkResult `json:"results"`
		Failures []BenchmarkResult `json:"failures"`
	}{
		Summary:  summary,
		Results:  valid,
		Failures: failed,
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(output); err != nil {
		fmt.Fprintf(os.Stderr, "failed to encode json results: %v\n", err)
	}
}
