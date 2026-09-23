package main

import (
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
