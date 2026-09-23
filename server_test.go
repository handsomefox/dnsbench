package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestUIServer_BuildRunConfig(t *testing.T) {
	base := &Config{Repeats: 10, LookupTimeout: 3 * time.Second, MaxConcurrency: 4}

	tests := []struct {
		name    string
		req     runRequest
		wantErr string
	}{
		{
			name: "defaults",
			req:  runRequest{},
		},
		{
			name: "valid custom lists",
			req: runRequest{
				Domains:   []string{"example.com"},
				Resolvers: []DNSServer{{Name: "a", Addr: "192.0.2.1"}, {Addr: "2001:db8::1"}},
			},
		},
		{
			name:    "hostname as resolver",
			req:     runRequest{Resolvers: []DNSServer{{Name: "a", Addr: "dns.example.com"}}},
			wantErr: "invalid resolver address",
		},
		{
			name:    "resolver with port",
			req:     runRequest{Resolvers: []DNSServer{{Name: "a", Addr: "192.0.2.1:53"}}},
			wantErr: "invalid resolver address",
		},
		{
			name:    "invalid domain",
			req:     runRequest{Domains: []string{"not a domain"}},
			wantErr: "invalid domain",
		},
		{
			name:    "timeout too short",
			req:     runRequest{Options: runOptions{TimeoutMs: 10}},
			wantErr: "timeout must be at least 100ms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &uiServer{baseConfig: base}
			_, servers, domains, err := s.buildRunConfig(&tt.req)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("buildRunConfig() error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("buildRunConfig() error = %v", err)
			}
			if len(servers) == 0 || len(domains) == 0 {
				t.Fatalf("buildRunConfig() returned %d servers and %d domains", len(servers), len(domains))
			}
			for _, srv := range servers {
				if srv.Name == "" {
					t.Errorf("server %q has no name", srv.Addr)
				}
			}
		})
	}
}

func TestUIServer_Routes(t *testing.T) {
	s := &uiServer{
		hub:        NewSSEHub(),
		baseConfig: &Config{Repeats: 7, LookupTimeout: 2 * time.Second, MaxConcurrency: 3},
	}
	ts := httptest.NewServer(s.routes())
	t.Cleanup(ts.Close)

	tests := []struct {
		method, path string
		wantStatus   int
		wantBody     []string
	}{
		{method: http.MethodGet, path: "/", wantStatus: http.StatusOK, wantBody: []string{
			`<textarea id="domains"`,
			"google.com\n",
			`id="repeats" min="1" value="7"`,
			`id="timeout" min="100" step="100" value="2000"`,
		}},
		{method: http.MethodGet, path: "/static/app.js", wantStatus: http.StatusOK, wantBody: []string{"connectEvents"}},
		{method: http.MethodGet, path: "/static/style.css", wantStatus: http.StatusOK},
		{method: http.MethodGet, path: "/nope", wantStatus: http.StatusNotFound},
		{method: http.MethodGet, path: "/api/run", wantStatus: http.StatusMethodNotAllowed},
		{method: http.MethodPost, path: "/api/stop", wantStatus: http.StatusNoContent},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req, err := http.NewRequestWithContext(t.Context(), tt.method, ts.URL+tt.path, http.NoBody)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer closeQuietly(resp.Body)
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			for _, want := range tt.wantBody {
				if !strings.Contains(string(body), want) {
					t.Errorf("body does not contain %q", want)
				}
			}
		})
	}
}

// The page hands the built-in lists to app.js as JSON inside a script tag.
// html/template must emit valid JSON there, not a Go-escaped string.
func TestUIServer_BuiltinsDataIsland(t *testing.T) {
	s := &uiServer{hub: NewSSEHub(), baseConfig: &Config{LookupTimeout: time.Second}}
	rec := httptest.NewRecorder()
	s.handleIndex(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody))

	body := rec.Body.String()
	const open = `<script type="application/json" id="builtins">`
	start := strings.Index(body, open)
	if start < 0 {
		t.Fatal("page has no builtins data island")
	}
	raw, _, found := strings.Cut(body[start+len(open):], "</script>")
	if !found {
		t.Fatal("builtins data island is not closed")
	}

	var got builtinLists
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("data island is not valid JSON: %v\n%s", err, raw)
	}
	if len(got.Resolvers) != len(builtInResolvers) || len(got.MajorResolvers) != len(builtinMajorResolvers) {
		t.Errorf("data island has %d and %d resolvers, want %d and %d",
			len(got.Resolvers), len(got.MajorResolvers), len(builtInResolvers), len(builtinMajorResolvers))
	}
}
