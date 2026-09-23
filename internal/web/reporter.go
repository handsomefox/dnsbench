package web

import (
	"time"

	"github.com/handsomefox/dnsbench/internal/bench"
	"github.com/handsomefox/dnsbench/internal/dnsclient"
)

// SSEReporter emits progress updates over SSE.
type SSEReporter struct {
	hub   *SSEHub
	runID string
}

func NewSSEReporter(hub *SSEHub, runID string) *SSEReporter {
	return &SSEReporter{hub: hub, runID: runID}
}

func (r *SSEReporter) OnStart(totalResolvers int, domains []string) {
	r.hub.Broadcast(SSEEvent{
		Type:  "start",
		RunID: r.runID,
		Detail: map[string]any{
			"totalResolvers": totalResolvers,
			"domainCount":    len(domains),
			"domains":        domains,
		},
	})
}

func (r *SSEReporter) OnResolverStart(server dnsclient.Server, index, total int) {
	r.hub.Broadcast(SSEEvent{
		Type:  "resolver_start",
		RunID: r.runID,
		Detail: map[string]any{
			"server": server,
			"index":  index,
			"total":  total,
		},
	})
}

// OnQueryResult sends a query event. A blocked lookup carries its latency
// and "blocked": true instead of an error.
func (r *SSEReporter) OnQueryResult(server dnsclient.Server, result bench.QueryResult) {
	detail := map[string]any{
		"server":   server,
		"domain":   result.Domain,
		"latency":  result.LatencyMs,
		"attempts": result.Attempts,
	}
	switch {
	case result.Blocked:
		detail["blocked"] = true
	case result.Err != nil:
		detail["error"] = result.Err.Error()
	}
	r.hub.Broadcast(SSEEvent{
		Type:   "query",
		RunID:  r.runID,
		Detail: detail,
	})
}

func (r *SSEReporter) OnResolverDone(server dnsclient.Server, stats bench.Stats, took time.Duration) {
	r.hub.Broadcast(SSEEvent{
		Type:  "resolver_done",
		RunID: r.runID,
		Detail: map[string]any{
			"server": server,
			"stats":  stats,
			"tookMs": took.Milliseconds(),
		},
	})
}

func (r *SSEReporter) OnComplete(results []bench.Result, err error) {
	detail := map[string]any{
		"results": results,
	}
	if err != nil {
		detail["error"] = err.Error()
	}
	r.hub.Broadcast(SSEEvent{
		Type:   "complete",
		RunID:  r.runID,
		Detail: detail,
	})
}
