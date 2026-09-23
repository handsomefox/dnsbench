package report

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/handsomefox/dnsbench/internal/bench"
	"github.com/handsomefox/dnsbench/internal/dnsclient"
)

// results holds two resolvers that answered, in the wrong order, and one
// that never did. The first name has a comma, which CSV must quote.
func results() []bench.Result {
	nan := math.NaN()
	return []bench.Result{
		{Server: dnsclient.Server{Name: "slow, but sure", Addr: "192.0.2.1"}, Stats: bench.Stats{Min: 20, Max: 40, Mean: 30, Median: 30, P95: 39, Count: 4, Total: 4}},
		{Server: dnsclient.Server{Name: "dead", Addr: "192.0.2.2", TLSName: "dns.example"}, Stats: bench.Stats{Min: nan, Max: nan, Mean: nan, Median: nan, P95: nan, Errors: 4, Total: 4}},
		{Server: dnsclient.Server{Name: "fast", Addr: "2001:db8::1"}, Stats: bench.Stats{Min: 1, Max: 3, Mean: 2, Median: 2, P95: 2.9, Count: 4, Total: 4, Retried: 1}},
	}
}

func TestWrite_CSV(t *testing.T) {
	var out bytes.Buffer
	if err := Write(&out, results(), CSV); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&out).ReadAll()
	if err != nil {
		t.Fatalf("output is not valid CSV: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("got %d rows, want a header and 3 resolvers", len(rows))
	}
	names := []string{rows[1][0], rows[2][0], rows[3][0]}
	if names[0] != "fast" || names[1] != "slow, but sure" || names[2] != "dead" {
		t.Errorf("rows are %q, want fast, slow, then dead", names)
	}
	dead := rows[3]
	if dead[2] != "dot" || dead[5] != "4" || dead[7] != "" {
		t.Errorf("dead row = %q, want transport dot, 4 failed, and no median", dead)
	}
	if rows[1][6] != "1" {
		t.Errorf("fast row retried = %q, want 1", rows[1][6])
	}
}

func TestWrite_Table(t *testing.T) {
	var out bytes.Buffer
	if err := Write(&out, results(), Default); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, want := range []string{"fast", "slow, but sure", "No answer at all:", "dead", "2 answered, 1 did not."} {
		if !strings.Contains(text, want) {
			t.Errorf("table does not contain %q:\n%s", want, text)
		}
	}
	if strings.Index(text, "fast") > strings.Index(text, "slow, but sure") {
		t.Error("the faster resolver does not come first")
	}
}

func TestWrite_JSON(t *testing.T) {
	var out bytes.Buffer
	if err := Write(&out, results(), JSON); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Results  []bench.Result `json:"results"`
		Failures []bench.Result `json:"failures"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if len(got.Results) != 2 || len(got.Failures) != 1 || got.Results[0].Server.Name != "fast" {
		t.Errorf("results = %+v, failures = %+v", got.Results, got.Failures)
	}
}
