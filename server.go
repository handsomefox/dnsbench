package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
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

//go:embed webui/dist/* webui/dist/assets/*
var webUIFS embed.FS

var webUISub fs.FS

func init() {
	sub, err := fs.Sub(webUIFS, "webui/dist")
	if err == nil {
		webUISub = sub
	}
}

type runOptions struct {
	Repeats     int  `json:"repeats"`
	TimeoutMs   int  `json:"timeoutMs"`
	Concurrency int  `json:"concurrency"`
	Warmup      int  `json:"warmup"`
	OnlyMajor   bool `json:"onlyMajor"`
}

type runRequest struct {
	Domains   []string    `json:"domains"`
	Resolvers []DNSServer `json:"resolvers"`
	Options   runOptions  `json:"options"`
}

type defaultsResponse struct {
	Resolvers      []DNSServer `json:"resolvers"`
	MajorResolvers []DNSServer `json:"majorResolvers"`
	Domains        []string    `json:"domains"`
	Options        runOptions  `json:"options"`
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
	if webUISub == nil {
		return errors.New("embedded UI assets not found; run `make ui-build` first")
	}

	hub := NewSSEHub()
	srv := &uiServer{
		hub:        hub,
		baseConfig: config,
		ctx:        ctx,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/defaults", srv.handleDefaults)
	mux.HandleFunc("/api/run", srv.handleRun)
	mux.HandleFunc("/api/stop", srv.handleStop)
	mux.HandleFunc("/api/reset", srv.handleReset)
	mux.HandleFunc("/api/events", func(w http.ResponseWriter, r *http.Request) {
		hub.Handle(w, r)
	})

	// Static UI at root
	fileServer := http.FileServer(http.FS(webUISub))
	mux.Handle("/assets/", fileServer)
	mux.Handle("/", spaHandler{fs: webUISub, fileServer: fileServer})

	server := &http.Server{
		Addr:              config.ListenAddr,
		Handler:           mux,
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

func (s *uiServer) handleDefaults(w http.ResponseWriter, _ *http.Request) {
	resp := defaultsResponse{
		Resolvers:      builtInResolvers,
		MajorResolvers: builtinMajorResolvers,
		Domains:        defaultSites,
		Options: runOptions{
			Repeats:     s.baseConfig.Repeats,
			TimeoutMs:   int(s.baseConfig.LookupTimeout.Milliseconds()),
			Concurrency: s.baseConfig.MaxConcurrency,
			Warmup:      s.baseConfig.WarmupRuns,
		},
	}
	writeJSON(w, resp)
}

//nolint:contextcheck // uses server lifetime context so runs persist beyond the request
func (s *uiServer) handleRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

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

func (s *uiServer) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

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
		Detail: map[string]interface{}{
			"at": time.Now().UnixMilli(),
		},
	})

	writeJSON(w, map[string]string{"status": "stopped", "runId": runID})
}

func (s *uiServer) handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.cancel = nil
	s.currentRun = ""
	s.mu.Unlock()

	s.hub.Broadcast(SSEEvent{
		Type: "reset",
		Detail: map[string]interface{}{
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
	cfg.OnlyMajorResolvers = cfg.OnlyMajorResolvers || req.Options.OnlyMajor

	domains := req.Domains
	if len(domains) == 0 {
		domains = defaultSites
	}

	servers := req.Resolvers
	if len(servers) == 0 {
		if cfg.OnlyMajorResolvers {
			servers = builtinMajorResolvers
		} else {
			servers = builtInResolvers
		}
	}

	return &cfg, servers, domains, nil
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}

// spaHandler serves static files and falls back to index.html for SPA routes.
type spaHandler struct {
	fs         fs.FS
	fileServer http.Handler
}

func (h spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Try to open the requested path from the embedded fs.
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if f, err := h.fs.Open(path); err == nil {
		defer func() {
			if cerr := f.Close(); cerr != nil {
				slog.Warn("failed to close asset file", slogErr(cerr))
			}
		}()
		h.fileServer.ServeHTTP(w, r)
		return
	}
	// Fallback to index.html for client-side routing.
	index, err := h.fs.Open("index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() {
		if cerr := index.Close(); cerr != nil {
			slog.Warn("failed to close index.html", slogErr(cerr))
		}
	}()
	r.URL.Path = "/"
	h.fileServer.ServeHTTP(w, r)
}

// openBrowser tries to open the given URL in the user's default browser.
func openBrowser(ctx context.Context, url string) error {
	var cmd string
	var args []string

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
