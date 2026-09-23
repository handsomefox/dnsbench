// Package web serves the dashboard and runs benchmarks for it, streaming
// each lookup to the page over server-sent events.
package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/handsomefox/dnsbench/internal/bench"
	"github.com/handsomefox/dnsbench/internal/catalog"
	"github.com/handsomefox/dnsbench/internal/dnsclient"
	"github.com/handsomefox/dnsbench/ui"
)

var pageTemplate = template.Must(template.ParseFS(ui.FS, "index.html.tmpl"))

type runOptions struct {
	Repeats     int    `json:"repeats"`
	TimeoutMs   int    `json:"timeoutMs"`
	Concurrency int    `json:"concurrency"`
	Warmup      int    `json:"warmup"`
	OnlyMajor   bool   `json:"onlyMajor"`
	PrimaryOnly bool   `json:"primaryOnly"`
	Family      string `json:"family"`
	Transport   string `json:"transport"`
	Kind        string `json:"kind"`
}

type runRequest struct {
	Domains   []string           `json:"domains"`
	Resolvers []dnsclient.Server `json:"resolvers"`
	Options   runOptions         `json:"options"`
}

// pageData fills ui/index.html.tmpl. Builtins and DefaultDomains go into
// JSON data islands that ui/static/app.js reads. The dashboard filters the
// catalog itself and always sends /api/run an explicit resolver list.
type pageData struct {
	Domains        string
	DefaultDomains []string
	Options        runOptions
	Builtins       []catalog.Entry
}

// Options set up the dashboard. Bench and Filter are the defaults the page
// starts from.
type Options struct {
	Listen string
	Bench  bench.Options
	Filter catalog.Filter
}

type uiServer struct {
	hub        *SSEHub
	baseConfig *Options
	ctx        context.Context
	mu         sync.Mutex
	cancel     context.CancelFunc
	currentRun string
}

// Serve runs the dashboard on opts.Listen until ctx ends, and tries to open
// it in the default browser.
func Serve(ctx context.Context, config *Options) error {
	srv := &uiServer{
		hub:        NewSSEHub(),
		baseConfig: config,
		ctx:        ctx,
	}

	server := &http.Server{
		Addr:              config.Listen,
		Handler:           srv.routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Attempt to open the UI in the browser (best effort).
	go func(ctx context.Context) {
		select {
		case <-ctx.Done():
			return
		default:
		}
		url := config.Listen
		if strings.HasPrefix(url, ":") {
			url = "localhost" + url
		}
		if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
			url = "http://" + url
		}
		if !strings.HasSuffix(url, "/") {
			url += "/"
		}
		if err := openBrowser(ctx, url); err != nil {
			slog.Warn(
				"failed to open browser for Web UI",
				slog.Any("err", err),
				slog.String("url", url),
			)
		}
	}(ctx)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Warn("graceful shutdown failed", slog.Any("err", err))
		}
	}()

	slog.Info("Starting Web UI server", slog.String("addr", config.Listen))
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http server: %w", err)
	}
	return nil
}

func (s *uiServer) routes() http.Handler {
	static, err := fs.Sub(ui.FS, "static")
	if err != nil {
		panic(err) // ui/static is embedded at build time.
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	mux.HandleFunc("POST /api/run", s.handleRun)
	mux.HandleFunc("POST /api/stop", s.handleStop)
	mux.HandleFunc("POST /api/reset", s.handleReset)
	mux.HandleFunc("GET /api/events", s.hub.Handle)
	return mux
}

func (s *uiServer) handleIndex(w http.ResponseWriter, _ *http.Request) {
	data := pageData{
		Domains: strings.Join(catalog.DefaultDomains, "\n"),
		Options: runOptions{
			Repeats:     s.baseConfig.Bench.Repeats,
			TimeoutMs:   int(s.baseConfig.Bench.Timeout.Milliseconds()),
			Concurrency: s.baseConfig.Bench.Concurrency,
			Warmup:      s.baseConfig.Bench.Warmup,
			OnlyMajor:   s.baseConfig.Filter.Major,
			PrimaryOnly: s.baseConfig.Filter.Primary,
			Family:      s.baseConfig.Filter.Family.String(),
			Transport:   s.baseConfig.Filter.Transport.String(),
			Kind:        s.baseConfig.Filter.Kind,
		},
		DefaultDomains: catalog.DefaultDomains,
		Builtins:       catalog.All(),
	}

	// Render into a buffer so a template error still produces a clean 500.
	var buf bytes.Buffer
	if err := pageTemplate.Execute(&buf, data); err != nil {
		slog.Error("failed to render the dashboard", slog.Any("err", err))
		http.Error(w, "failed to render page", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := buf.WriteTo(w); err != nil {
		slog.Warn("failed to write the dashboard", slog.Any("err", err))
	}
}

//nolint:contextcheck // uses server lifetime context so runs persist beyond the request
func (s *uiServer) handleRun(w http.ResponseWriter, r *http.Request) {
	var req runRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	cfg, servers, domains, err := s.buildRunConfig(&req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	runID := strconv.FormatInt(time.Now().UnixNano(), 10)
	reporter := NewSSEReporter(s.hub, runID)

	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	// using server lifetime context so benchmark continues after HTTP request finishes
	runCtx, cancel := context.WithCancel(s.ctx) //nolint:contextcheck // handler should outlive request
	s.cancel = cancel
	s.currentRun = runID
	s.mu.Unlock()

	go func() {
		results, runErr := bench.Run(runCtx, *cfg, servers, domains, reporter)
		if runErr != nil {
			slog.LogAttrs(runCtx, slog.LevelWarn, "benchmark finished with error", slog.Any("err", runErr))
		}
		if runErr == nil {
			slog.LogAttrs(runCtx, slog.LevelInfo, "benchmark completed", slog.Int("results", len(results)))
		}
	}()

	writeJSON(w, map[string]string{"runId": runID})
}

func (s *uiServer) handleStop(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	cancel := s.cancel
	runID := s.currentRun
	s.cancel = nil
	s.currentRun = ""
	s.mu.Unlock()

	if cancel == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	cancel()
	s.hub.Broadcast(SSEEvent{
		RunID: runID,
		Type:  "stop",
		Detail: map[string]any{
			"at": time.Now().UnixMilli(),
		},
	})

	writeJSON(w, map[string]string{"status": "stopped", "runId": runID})
}

func (s *uiServer) handleReset(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.cancel = nil
	s.currentRun = ""
	s.mu.Unlock()

	s.hub.Broadcast(SSEEvent{
		Type: "reset",
		Detail: map[string]any{
			"at": time.Now().UnixMilli(),
		},
	})

	writeJSON(w, map[string]string{"status": "reset"})
}

// buildRunConfig checks a run request and returns the run's options, its
// resolvers, and its domains. A request without resolvers gets the
// built-in ones that its filter options select.
func (s *uiServer) buildRunConfig(req *runRequest) (*bench.Options, []dnsclient.Server, []string, error) {
	opts := s.baseConfig.Bench
	filter := s.baseConfig.Filter

	if req.Options.Repeats > 0 {
		opts.Repeats = req.Options.Repeats
	}
	if req.Options.Concurrency > 0 {
		opts.Concurrency = req.Options.Concurrency
	}
	if req.Options.TimeoutMs > 0 {
		opts.Timeout = time.Duration(req.Options.TimeoutMs) * time.Millisecond
	}
	if opts.Timeout < 100*time.Millisecond {
		return nil, nil, nil, errors.New("timeout must be at least 100ms")
	}
	opts.Warmup = req.Options.Warmup
	filter.Major = req.Options.OnlyMajor
	filter.Primary = req.Options.PrimaryOnly
	if req.Options.Family != "" {
		family, err := catalog.ParseFamily(req.Options.Family)
		if err != nil {
			return nil, nil, nil, err
		}
		filter.Family = family
	}
	if req.Options.Kind != "" {
		kind, err := catalog.ParseKind(req.Options.Kind)
		if err != nil {
			return nil, nil, nil, err
		}
		filter.Kind = kind
	}
	if req.Options.Transport != "" {
		transport, err := catalog.ParseTransport(req.Options.Transport)
		if err != nil {
			return nil, nil, nil, err
		}
		filter.Transport = transport
	}

	domains := req.Domains
	if len(domains) == 0 {
		domains = catalog.DefaultDomains
	}
	for _, d := range domains {
		if !dnsclient.IsValidDomain(d) {
			return nil, nil, nil, fmt.Errorf("invalid domain %q", d)
		}
	}

	servers := req.Resolvers
	for i := range servers {
		if err := servers[i].Validate(); err != nil {
			return nil, nil, nil, err
		}
		if servers[i].Name == "" {
			servers[i].Name = servers[i].Addr
		}
	}
	if len(servers) == 0 {
		servers = catalog.Servers(filter)
	}

	return &opts, servers, domains, nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}

// openBrowser tries to open the given URL in the user's default browser.
func openBrowser(ctx context.Context, url string) error {
	var cmd string
	args := make([]string, 0, 3)

	switch runtime.GOOS {
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start"}
	case "darwin":
		cmd = "open"
	default:
		cmd = "xdg-open"
	}
	args = append(args, url)
	//nolint:gosec // opening user-selected URL in default browser is expected behavior
	return exec.CommandContext(ctx, cmd, args...).Start()
}
