package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/handsomefox/dnsbench/internal/bench"
	"github.com/handsomefox/dnsbench/internal/catalog"
	"github.com/handsomefox/dnsbench/internal/dnsclient"
)

func TestUIServer_BuildRunConfig(t *testing.T) {
	base := &Options{Bench: bench.Options{Repeats: 10, Timeout: 3 * time.Second, Concurrency: 4}}

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
				Resolvers: []dnsclient.Server{{Name: "a", Addr: "192.0.2.1"}, {Addr: "2001:db8::1"}},
			},
		},
		{
			name: "IPv6 built-ins",
			req:  runRequest{Options: runOptions{Family: "ipv6"}},
		},
		{
			name: "DoT built-ins",
			req:  runRequest{Options: runOptions{Transport: "dot"}},
		},
		{
			name: "custom DoT resolver",
			req:  runRequest{Resolvers: []dnsclient.Server{{Name: "a", Addr: "192.0.2.1", TLSName: "dns.example"}}},
		},
		{
			name:    "bad TLS name",
			req:     runRequest{Resolvers: []dnsclient.Server{{Name: "a", Addr: "192.0.2.1", TLSName: "not a name"}}},
			wantErr: "invalid TLS name",
		},
		{
			name: "custom DoH resolver",
			req:  runRequest{Resolvers: []dnsclient.Server{{Name: "a", Addr: "192.0.2.1", DoHURL: "https://dns.example/dns-query"}}},
		},
		{
			name:    "DoH URL over plain HTTP",
			req:     runRequest{Resolvers: []dnsclient.Server{{Name: "a", Addr: "192.0.2.1", DoHURL: "http://dns.example/dns-query"}}},
			wantErr: "invalid DoH URL",
		},
		{
			name:    "both DoT and DoH",
			req:     runRequest{Resolvers: []dnsclient.Server{{Name: "a", Addr: "192.0.2.1", TLSName: "dns.example", DoHURL: "https://dns.example/dns-query"}}},
			wantErr: "pick one",
		},
		{
			name: "custom DoQ resolver",
			req:  runRequest{Resolvers: []dnsclient.Server{{Name: "a", Addr: "192.0.2.1", DoQName: "dns.example"}}},
		},
		{
			name:    "both DoH and DoQ",
			req:     runRequest{Resolvers: []dnsclient.Server{{Name: "a", Addr: "192.0.2.1", DoHURL: "https://dns.example/q", DoQName: "dns.example"}}},
			wantErr: "pick one",
		},
		{
			name:    "bad DoQ name",
			req:     runRequest{Resolvers: []dnsclient.Server{{Name: "a", Addr: "192.0.2.1", DoQName: "quic://dns.example"}}},
			wantErr: "invalid DoQ name",
		},
		{
			name:    "unknown transport",
			req:     runRequest{Options: runOptions{Transport: "http"}},
			wantErr: "invalid transport",
		},
		{
			name:    "unknown family",
			req:     runRequest{Options: runOptions{Family: "ipv5"}},
			wantErr: "invalid address family",
		},
		{
			name:    "hostname as resolver",
			req:     runRequest{Resolvers: []dnsclient.Server{{Name: "a", Addr: "dns.example.com"}}},
			wantErr: "invalid resolver address",
		},
		{
			name:    "resolver with port",
			req:     runRequest{Resolvers: []dnsclient.Server{{Name: "a", Addr: "192.0.2.1:53"}}},
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
		baseConfig: &Options{Bench: bench.Options{Repeats: 7, Timeout: 2 * time.Second, Concurrency: 3}},
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
		{method: http.MethodGet, path: "/static/fonts/martian-mono.woff2", wantStatus: http.StatusOK, wantBody: []string{"wOF2"}},
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
			defer resp.Body.Close() //nolint:errcheck // a close error changes nothing in a test
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
	s := &uiServer{hub: NewSSEHub(), baseConfig: &Options{Bench: bench.Options{Timeout: time.Second}}}
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

	var got []catalog.Entry
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("data island is not valid JSON: %v\n%s", err, raw)
	}
	if len(got) != len(catalog.All()) {
		t.Fatalf("data island has %d resolvers, want %d", len(got), len(catalog.All()))
	}
	// app.js filters on these fields, so every entry needs them.
	for _, e := range got {
		if e.Name == "" || e.Addr == "" || e.Provider == "" || e.Category == "" || e.Family == "" || e.Transport == "" {
			t.Errorf("entry is missing a field: %+v", e)
		}
	}
}
