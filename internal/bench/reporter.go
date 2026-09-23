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
	OnQueryResult(server dnsclient.Server, domain string, latencyMs float64, attempts int, err error)
	OnResolverDone(server dnsclient.Server, stats Stats, took time.Duration)
	OnComplete(results []Result, err error)
}

// NoopReporter is used when no callbacks are needed.
type NoopReporter struct{}

func (NoopReporter) OnStart(_ int, _ []string)                                             {}
func (NoopReporter) OnResolverStart(_ dnsclient.Server, _, _ int)                          {}
func (NoopReporter) OnQueryResult(_ dnsclient.Server, _ string, _ float64, _ int, _ error) {}
func (NoopReporter) OnResolverDone(_ dnsclient.Server, _ Stats, _ time.Duration)           {}
func (NoopReporter) OnComplete(_ []Result, _ error)                                        {}
