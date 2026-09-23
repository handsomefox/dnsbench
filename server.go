package main

import (
	"bytes"
	"context"
	"embed"
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
)

//go:embed ui
var uiFS embed.FS

var pageTemplate = template.Must(template.ParseFS(uiFS, "ui/index.html.tmpl"))

type runOptions struct {
	Repeats     int    `json:"repeats"`
	TimeoutMs   int    `json:"timeoutMs"`
	Concurrency int    `json:"concurrency"`
	Warmup      int    `json:"warmup"`
	OnlyMajor   bool   `json:"onlyMajor"`
	Family      string `json:"family"`
}

type runRequest struct {
	Domains   []string    `json:"domains"`
	Resolvers []DNSServer `json:"resolvers"`
	Options   runOptions  `json:"options"`
}

// pageData fills ui/index.html.tmpl. Builtins goes into a JSON data island
// that ui/static/app.js reads to preview the built-in resolvers. It holds
// every combination of the filters, keyed by builtinsKey, so the preview
// always matches what buildRunConfig runs.
type pageData struct {
	Domains  string
	Options  runOptions
	Builtins map[string][]DNSServer
}

// builtinsKey must match the key that app.js builds in builtinSelection.
func builtinsKey(onlyMajor bool, family AddrFamily) string {
	return fmt.Sprintf("%t/%s", onlyMajor, family)
}

func builtinsByFilter() map[string][]DNSServer {
	lists := make(map[string][]DNSServer)
	for _, onlyMajor := range []bool{false, true} {
		for _, family := range []AddrFamily{FamilyIPv4, FamilyIPv6, FamilyAll} {
			lists[builtinsKey(onlyMajor, family)] = builtinServers(onlyMajor, family)
		}
	}
	return lists
}

type uiServer struct {
	hub        *SSEHub
	baseConfig *Config
	ctx        context.Context
	mu         sync.Mutex
	cancel     context.CancelFunc
	currentRun string
}

func serveDashboard(ctx context.Context, config *Config) error {
	srv := &uiServer{
		hub:        NewSSEHub(),
		baseConfig: config,
		ctx:        ctx,
	}

	server := &http.Server{
		Addr:              config.ListenAddr,
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
		url := config.ListenAddr
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
				slogErr(err),
				slog.String("url", url),
			)
		}
	}(ctx)

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Warn("graceful shutdown failed", slogErr(err))
		}
	}()

	slog.Info("Starting Web UI server", slog.String("addr", config.ListenAddr))
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http server: %w", err)
	}
	return nil
}

func (s *uiServer) routes() http.Handler {
	static, err := fs.Sub(uiFS, "ui/static")
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
		Domains: strings.Join(defaultSites, "\n"),
		Options: runOptions{
			Repeats:     s.baseConfig.Repeats,
			TimeoutMs:   int(s.baseConfig.LookupTimeout.Milliseconds()),
			Concurrency: s.baseConfig.MaxConcurrency,
			Warmup:      s.baseConfig.WarmupRuns,
			OnlyMajor:   s.baseConfig.OnlyMajorResolvers,
			Family:      s.baseConfig.Family.String(),
		},
		Builtins: builtinsByFilter(),
	}

	// Render into a buffer so a template error still produces a clean 500.
	var buf bytes.Buffer
	if err := pageTemplate.Execute(&buf, data); err != nil {
		slog.Error("failed to render the dashboard", slogErr(err))
		http.Error(w, "failed to render page", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := buf.WriteTo(w); err != nil {
		slog.Warn("failed to write the dashboard", slogErr(err))
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
		results, runErr := runBenchmark(runCtx, cfg, servers, domains, reporter)
		if runErr != nil {
			slog.LogAttrs(runCtx, slog.LevelWarn, "benchmark finished with error", slogErr(runErr))
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

func (s *uiServer) buildRunConfig(req *runRequest) (*Config, []DNSServer, []string, error) {
	cfg := *s.baseConfig

	if req.Options.Repeats > 0 {
		cfg.Repeats = req.Options.Repeats
	}
	if req.Options.Concurrency > 0 {
		cfg.MaxConcurrency = req.Options.Concurrency
	}
	if req.Options.TimeoutMs > 0 {
		cfg.LookupTimeout = time.Duration(req.Options.TimeoutMs) * time.Millisecond
	}
	if cfg.LookupTimeout < 100*time.Millisecond {
		return nil, nil, nil, errors.New("timeout must be at least 100ms")
	}
	cfg.WarmupRuns = req.Options.Warmup
	cfg.OnlyMajorResolvers = req.Options.OnlyMajor
	if req.Options.Family != "" {
		family, err := parseFamily(req.Options.Family)
		if err != nil {
			return nil, nil, nil, err
		}
		cfg.Family = family
	}

	domains := req.Domains
	if len(domains) == 0 {
		domains = defaultSites
	}
	for _, d := range domains {
		if !isValidDomain(d) {
			return nil, nil, nil, fmt.Errorf("invalid domain %q", d)
		}
	}

	servers := req.Resolvers
	for i, srv := range servers {
		if !isValidServerAddr(srv.Addr) {
			return nil, nil, nil, fmt.Errorf("invalid resolver address %q: want an IP address without a port", srv.Addr)
		}
		if srv.Name == "" {
			servers[i].Name = srv.Addr
		}
	}
	if len(servers) == 0 {
		servers = builtinServers(cfg.OnlyMajorResolvers, cfg.Family)
	}

	return &cfg, servers, domains, nil
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
