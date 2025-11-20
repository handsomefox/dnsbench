package main

import (
	"time"
)

// BenchmarkReporter provides hooks during benchmark execution.
type BenchmarkReporter interface {
	OnStart(totalResolvers int, domains []string)
	OnResolverStart(server DNSServer, index, total int)
	OnQueryResult(server DNSServer, domain string, latencyMs float64, err error)
	OnResolverDone(server DNSServer, stats Stats, took time.Duration)
	OnComplete(results []BenchmarkResult, err error)
}

// NoopReporter is used when no callbacks are needed.
type NoopReporter struct{}

func (NoopReporter) OnStart(_ int, _ []string)                               {}
func (NoopReporter) OnResolverStart(_ DNSServer, _, _ int)                   {}
func (NoopReporter) OnQueryResult(_ DNSServer, _ string, _ float64, _ error) {}
func (NoopReporter) OnResolverDone(_ DNSServer, _ Stats, _ time.Duration)    {}
func (NoopReporter) OnComplete(_ []BenchmarkResult, _ error)                 {}

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
		Detail: map[string]interface{}{
			"totalResolvers": totalResolvers,
			"domainCount":    len(domains),
			"domains":        domains,
		},
	})
}

func (r *SSEReporter) OnResolverStart(server DNSServer, index, total int) {
	r.hub.Broadcast(SSEEvent{
		Type:  "resolver_start",
		RunID: r.runID,
		Detail: map[string]interface{}{
			"server": server,
			"index":  index,
			"total":  total,
		},
	})
}

func (r *SSEReporter) OnQueryResult(server DNSServer, domain string, latencyMs float64, err error) {
	detail := map[string]interface{}{
		"server":  server,
		"domain":  domain,
		"latency": latencyMs,
	}
	if err != nil {
		detail["error"] = err.Error()
	}
	r.hub.Broadcast(SSEEvent{
		Type:   "query",
		RunID:  r.runID,
		Detail: detail,
	})
}

func (r *SSEReporter) OnResolverDone(server DNSServer, stats Stats, took time.Duration) {
	r.hub.Broadcast(SSEEvent{
		Type:  "resolver_done",
		RunID: r.runID,
		Detail: map[string]interface{}{
			"server": server,
			"stats":  stats,
			"tookMs": took.Milliseconds(),
		},
	})
}

func (r *SSEReporter) OnComplete(results []BenchmarkResult, err error) {
	detail := map[string]interface{}{
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
