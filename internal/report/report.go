// Package report writes a run's results as a table, CSV, or JSON.
package report

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/handsomefox/dnsbench/internal/bench"
)

// Format is a report format. Default is the table.
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

// Write writes results to w in format. Resolvers that answered come first,
// by success rate and then median. The resolvers with no answer at all
// follow in the same output, so a redirect captures every resolver.
func Write(w io.Writer, results []bench.Result, format Format) error {
	valid, failed := split(results)
	switch format {
	case CSV:
		return writeCSV(w, valid, failed)
	case JSON:
		return writeJSON(w, valid, failed)
	default:
		return writeTable(w, valid, failed)
	}
}

// split separates the resolvers that answered from those that did not, and
// sorts the first group.
func split(results []bench.Result) (valid, failed []bench.Result) {
	for _, r := range results {
		if r.Stats.IsValid() {
			valid = append(valid, r)
		} else {
			failed = append(failed, r)
		}
	}
	// The median, unlike the mean, is not pulled up by one slow lookup.
	sort.SliceStable(valid, func(i, j int) bool {
		vi, vj := valid[i].Stats.SuccessRate(), valid[j].Stats.SuccessRate()
		if vi == vj {
			return valid[i].Stats.Median < valid[j].Stats.Median
		}
		return vi > vj
	})
	return valid, failed
}

var csvHeader = []string{
	"Resolver", "Address", "Transport", "Success Rate", "Answered", "Failed", "Retried",
	"Median (ms)", "P95 (ms)", "Mean (ms)", "Min (ms)", "Max (ms)", "Total Queries",
}

// writeCSV writes one row per resolver. A resolver with no answer has
// empty latency cells.
func writeCSV(w io.Writer, valid, failed []bench.Result) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(csvHeader); err != nil {
		return err
	}
	ms := func(v float64, ok bool) string {
		if !ok {
			return ""
		}
		return strconv.FormatFloat(v, 'f', 2, 64)
	}
	for _, r := range append(slices.Clone(valid), failed...) {
		s, ok := r.Stats, r.Stats.IsValid()
		row := []string{
			r.Server.Name, r.Server.Addr, r.Server.Transport(),
			strconv.FormatFloat(s.SuccessRate()*100, 'f', 1, 64),
			strconv.Itoa(s.Count), strconv.Itoa(s.Errors), strconv.Itoa(s.Retried),
			ms(s.Median, ok), ms(s.P95, ok), ms(s.Mean, ok), ms(s.Min, ok), ms(s.Max, ok),
			strconv.Itoa(s.Total),
		}
		if err := cw.Write(row); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// writeTable writes the resolvers that answered as a table, then the ones
// that did not, then a one-line summary. Both columns of names size to the
// longest value: names such as Canadian-Shield-DoT-v6-1 and IPv6 addresses
// run past a fixed width, and a cut name cannot tell two resolvers apart.
func writeTable(w io.Writer, valid, failed []bench.Result) error {
	var b strings.Builder
	if len(valid) > 0 {
		nameWidth := len("Resolver")
		for _, r := range valid {
			nameWidth = max(nameWidth, len(r.Server.Name))
		}
		fmt.Fprintf(&b, "%-*s %9s %8s %10s %10s %10s %10s %10s %8s\n",
			nameWidth, "Resolver", "Success%", "Retried", "Median(ms)", "P95(ms)", "Mean(ms)", "Min(ms)", "Max(ms)", "Queries")
		fmt.Fprintf(&b, "%s\n", strings.Repeat("-", nameWidth+84))
		for _, r := range valid {
			s := r.Stats
			fmt.Fprintf(&b, "%-*s %8.1f%% %8d %10.2f %10.2f %10.2f %10.2f %10.2f %8d\n",
				nameWidth, r.Server.Name, s.SuccessRate()*100, s.Retried,
				s.Median, s.P95, s.Mean, s.Min, s.Max, s.Total)
		}
	}
	if len(failed) > 0 {
		nameWidth, addrWidth := len("Resolver"), len("Address")
		for _, r := range failed {
			nameWidth = max(nameWidth, len(r.Server.Name))
			addrWidth = max(addrWidth, len(r.Server.Addr))
		}
		if len(valid) > 0 {
			b.WriteString("\n")
		}
		b.WriteString("No answer at all:\n")
		fmt.Fprintf(&b, "%-*s %-*s %8s %8s\n", nameWidth, "Resolver", addrWidth, "Address", "Failed", "Queries")
		for _, r := range failed {
			fmt.Fprintf(&b, "%-*s %-*s %8d %8d\n", nameWidth, r.Server.Name, addrWidth, r.Server.Addr, r.Stats.Errors, r.Stats.Total)
		}
	}
	if len(valid)+len(failed) == 0 {
		b.WriteString("No results.\n")
	} else {
		fmt.Fprintf(&b, "\n%d answered, %d did not.\n", len(valid), len(failed))
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func writeJSON(w io.Writer, valid, failed []bench.Result) error {
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

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}
