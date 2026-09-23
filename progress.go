package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/handsomefox/dnsbench/internal/bench"
	"github.com/handsomefox/dnsbench/internal/dnsclient"
)

// progress keeps one line on a terminal up to date with a run's lookups:
// how many are done, how long the run has taken, and about how long it has
// left. Log records go through the same lock, so each one clears the line,
// prints, and lets the line come back below it.
type progress struct {
	bench.NoopReporter

	mu      sync.Mutex
	w       io.Writer
	planned int
	done    int
	failed  int
	start   time.Time
	shown   bool
	stop    chan struct{}
	stopped sync.WaitGroup
}

func newProgress(w io.Writer, planned int) *progress {
	return &progress{w: w, planned: planned, start: time.Now(), stop: make(chan struct{})}
}

// isTerminal reports whether f is a terminal, where a line can be redrawn
// in place.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// run redraws the line five times a second until finish.
func (p *progress) run() {
	p.stopped.Go(func() {
		tick := time.NewTicker(200 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-p.stop:
				return
			case <-tick.C:
				p.mu.Lock()
				p.draw()
				p.mu.Unlock()
			}
		}
	})
}

// finish stops the redraws and clears the line.
func (p *progress) finish() {
	close(p.stop)
	p.stopped.Wait()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.clear()
}

func (p *progress) OnQueryResult(_ dnsclient.Server, result bench.QueryResult) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.done++
	if result.Failed() {
		p.failed++
	}
}

// draw writes the line. The caller holds p.mu.
func (p *progress) draw() {
	elapsed := time.Since(p.start)
	line := fmt.Sprintf("%d of %d lookups", p.done, p.planned)
	if p.failed > 0 {
		line += fmt.Sprintf(", %d failed", p.failed)
	}
	line += " · " + elapsed.Round(time.Second).String()
	if p.done > 20 && elapsed > 3*time.Second && p.done < p.planned {
		left := time.Duration(float64(elapsed) * float64(p.planned-p.done) / float64(p.done))
		line += ", about " + left.Round(time.Second).String() + " left"
	}
	// \r returns to the start of the line, and \x1b[K erases the rest.
	if _, err := fmt.Fprintf(p.w, "\r\x1b[K%s", line); err == nil {
		p.shown = true
	}
}

// clear erases the line. The caller holds p.mu.
func (p *progress) clear() {
	if p.shown {
		if _, err := io.WriteString(p.w, "\r\x1b[K"); err == nil {
			p.shown = false
		}
	}
}

// progressHandler hands log records to next without tearing the progress
// line: it clears the line first, and the next redraw brings it back.
type progressHandler struct {
	next slog.Handler
	p    *progress
}

func (h progressHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h progressHandler) Handle(ctx context.Context, r slog.Record) error { //nolint:gocritic // slog.Handler fixes the signature
	h.p.mu.Lock()
	defer h.p.mu.Unlock()
	h.p.clear()
	return h.next.Handle(ctx, r)
}

func (h progressHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return progressHandler{next: h.next.WithAttrs(attrs), p: h.p}
}

func (h progressHandler) WithGroup(name string) slog.Handler {
	return progressHandler{next: h.next.WithGroup(name), p: h.p}
}
