// Package bench runs a benchmark: every resolver against every domain,
// interleaved, with warmup, retries, and a give-up rule for dead
// resolvers. It returns latency statistics per resolver.
package bench

import (
	"time"

	"github.com/handsomefox/dnsbench/internal/dnsclient"
)

// Reporter receives a run's progress. Run calls it one method at a time.
type Reporter interface {
	OnStart(totalResolvers int, domains []string)
	OnResolverStart(server dnsclient.Server, index, total int)
	OnQueryResult(server dnsclient.Server, result QueryResult)
	OnResolverDone(server dnsclient.Server, stats Stats, took time.Duration)
	OnComplete(results []Result, err error)
}

// QueryResult is one measured lookup as a Reporter sees it.
type QueryResult struct {
	Domain    string
	LatencyMs float64 // 0 when Err is set and Blocked is not
	Attempts  int
	// Blocked is set when the resolver answered that the name does not
	// exist or has no A record. The lookup counts as answered, and Err
	// holds the answer.
	Blocked bool
	Err     error
}

// Failed reports whether the lookup got no answer at all.
func (q *QueryResult) Failed() bool { return q.Err != nil && !q.Blocked }

// NoopReporter is used when no callbacks are needed.
type NoopReporter struct{}

func (NoopReporter) OnStart(_ int, _ []string)                                   {}
func (NoopReporter) OnResolverStart(_ dnsclient.Server, _, _ int)                {}
func (NoopReporter) OnQueryResult(_ dnsclient.Server, _ QueryResult)             {}
func (NoopReporter) OnResolverDone(_ dnsclient.Server, _ Stats, _ time.Duration) {}
func (NoopReporter) OnComplete(_ []Result, _ error)                              {}
