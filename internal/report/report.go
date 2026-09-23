// Package report writes a run's results as a table, CSV, or JSON.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/handsomefox/dnsbench/internal/bench"
)

// Format is a report format.
type Format int

const (
	Default Format = iota
	CSV
	Table
	JSON
)

func (o Format) String() string {
	switch o {
	case CSV:
		return "csv"
	case Table:
		return "table"
	case JSON:
		return "json"
	default:
		return "default"
	}
}

// ParseFormat parses a -output value: default, csv, table, or json, in any
// case.
func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(s) {
	case "default":
		return Default, nil
	case "csv":
		return CSV, nil
	case "table":
		return Table, nil
	case "json":
		return JSON, nil
	default:
		return Default, fmt.Errorf("invalid output type %q", s)
	}
}

// Print writes results to standard output in format, and failed resolvers
// to standard error for the csv and table formats.
func Print(results []bench.Result, outputType Format) {
	if len(results) == 0 {
		fmt.Println("\nNo benchmark results to display")
		return
	}

	var valid, failed []bench.Result
	for _, r := range results {
		if r.Stats.IsValid() {
			valid = append(valid, r)
		} else {
			failed = append(failed, r)
		}
	}

	// The median, unlike the mean, is not pulled up by one slow lookup.
	sort.Slice(valid, func(i, j int) bool {
		vi, vj := valid[i].Stats.SuccessRate(), valid[j].Stats.SuccessRate()
		if vi == vj {
			return valid[i].Stats.Median < valid[j].Stats.Median
		}
		return vi > vj
	})

	printByType(outputType, valid, failed)
}

func printByType(t Format, valid, failed []bench.Result) {
	switch t {
	case CSV:
		printResultsCSV(os.Stdout, valid, false)
		printResultsCSV(os.Stderr, failed, true)
	case Table:
		printResultsTable(os.Stdout, valid, false)
		printResultsTable(os.Stderr, failed, true)
	case JSON:
		printResultsJSON(valid, failed)
	default:
		printDefaultSummary(valid, failed)
	}
}

//nolint:errcheck // printing helper
func printResultsCSV(w io.Writer, results []bench.Result, failed bool) {
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
	_, _ = fmt.Fprintln(w, "Resolver,Success Rate,Retried,Median (ms),P95 (ms),Mean (ms),Min (ms),Max (ms),Total Queries")
	for _, r := range results {
		_, _ = fmt.Fprintf(w, "%s,%.1f,%d,%.2f,%.2f,%.2f,%.2f,%.2f,%d\n",
			r.Server.Name,
			r.Stats.SuccessRate()*100,
			r.Stats.Retried,
			r.Stats.Median,
			r.Stats.P95,
			r.Stats.Mean,
			r.Stats.Min,
			r.Stats.Max,
			r.Stats.Total)
	}
}

//nolint:errcheck // printing helper
func printResultsTable(w io.Writer, results []bench.Result, failed bool) {
	if len(results) == 0 {
		return
	}
	// Names such as Canadian-Shield-DoT-v6-1 and IPv6 addresses run past a
	// fixed width, and a truncated name can no longer tell two resolvers
	// apart. Size both columns to the longest value instead.
	nameWidth, addrWidth := len("Resolver"), len("255.255.255.255")
	for _, r := range results {
		nameWidth = max(nameWidth, len(r.Server.Name))
		addrWidth = max(addrWidth, len(r.Server.Addr))
	}
	if failed {
		_, _ = fmt.Fprintln(w, "\nFailed resolvers:")
		_, _ = fmt.Fprintf(w, "%-*s %-*s %10s %10s\n", nameWidth, "Resolver", addrWidth, "Address", "Errors", "Total")
		for _, r := range results {
			_, _ = fmt.Fprintf(w, "%-*s %-*s %10d %10d\n",
				nameWidth, r.Server.Name, addrWidth, r.Server.Addr, r.Stats.Errors, r.Stats.Total)
		}
		return
	}
	_, _ = fmt.Fprintf(w, "%-*s %10s %8s %10s %10s %10s %10s %10s %10s\n",
		nameWidth, "Resolver", "Success%", "Retried", "Median(ms)", "P95(ms)", "Mean(ms)", "Min(ms)", "Max(ms)", "Queries")
	_, _ = fmt.Fprintf(w, "%s\n", strings.Repeat("-", nameWidth+86))
	for _, r := range results {
		_, _ = fmt.Fprintf(w, "%-*s %9.1f%% %8d %10.2f %10.2f %10.2f %10.2f %10.2f %10d\n",
			nameWidth, r.Server.Name,
			r.Stats.SuccessRate()*100,
			r.Stats.Retried,
			r.Stats.Median,
			r.Stats.P95,
			r.Stats.Mean,
			r.Stats.Min,
			r.Stats.Max,
			r.Stats.Total)
	}
}

func printDefaultSummary(valid, failed []bench.Result) {
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

func printResultsJSON(valid, failed []bench.Result) {
	type Summary struct {
		TotalResolvers   int           `json:"total_resolvers"`
		SuccessResolvers int           `json:"success_resolvers"`
		FailedResolvers  int           `json:"failed_resolvers"`
		OverallSuccess   float64       `json:"overall_success_rate"`
		Fastest          *bench.Result `json:"fastest_resolver,omitempty"`
		Slowest          *bench.Result `json:"slowest_resolver,omitempty"`
	}

	all := append([]bench.Result{}, valid...)
	all = append(all, failed...)

	var fastest, slowest *bench.Result
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
		valid = []bench.Result{}
	}
	if failed == nil {
		failed = []bench.Result{}
	}

	output := struct {
		Summary  Summary        `json:"summary"`
		Results  []bench.Result `json:"results"`
		Failures []bench.Result `json:"failures"`
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
