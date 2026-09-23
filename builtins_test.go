package main

import (
	"context"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBuiltinServers(t *testing.T) {
	var plain4, plain6, dot4, dot6, majorPlain4, primaryPlain4 int
	for _, p := range providers {
		if !p.dotOnly {
			plain4 += len(p.ipv4)
			primaryPlain4 += min(len(p.ipv4), 1)
			plain6 += len(p.ipv6)
			if p.major {
				majorPlain4 += len(p.ipv4)
			}
		}
		if p.tlsName != "" {
			dot4 += len(p.ipv4)
			dot6 += len(p.ipv6)
		}
	}

	tests := []struct {
		name   string
		filter builtinFilter
		want   int
	}{
		{name: "default", filter: builtinFilter{}, want: plain4},
		{name: "IPv6", filter: builtinFilter{family: FamilyIPv6}, want: plain6},
		{name: "both families", filter: builtinFilter{family: FamilyAll}, want: plain4 + plain6},
		{name: "major", filter: builtinFilter{onlyMajor: true}, want: majorPlain4},
		{name: "first address", filter: builtinFilter{primaryOnly: true}, want: primaryPlain4},
		{name: "DoT", filter: builtinFilter{transport: TransportDoT}, want: dot4},
		{name: "everything", filter: builtinFilter{family: FamilyAll, transport: TransportAll}, want: plain4 + plain6 + dot4 + dot6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := builtinServers(tt.filter)
			if len(got) != tt.want {
				t.Fatalf("builtinServers(%+v) returned %d resolvers, want %d", tt.filter, len(got), tt.want)
			}
			names := make(map[string]bool)
			for _, s := range got {
				if names[s.Name] {
					t.Errorf("duplicate name %q", s.Name)
				}
				names[s.Name] = true

				addr, err := netip.ParseAddr(s.Addr)
				if err != nil {
					t.Fatalf("%s has invalid address %q: %v", s.Name, s.Addr, err)
				}
				if addr.Is6() != strings.Contains(s.Name, "-v6-") {
					t.Errorf("%s has address %s: the -v6- name and the family disagree", s.Name, s.Addr)
				}
				if (s.TLSName != "") != strings.Contains(s.Name, "-DoT-") {
					t.Errorf("%s has TLS name %q: the -DoT- name and the transport disagree", s.Name, s.TLSName)
				}
				if tt.filter.family == FamilyIPv4 && addr.Is6() || tt.filter.family == FamilyIPv6 && addr.Is4() {
					t.Errorf("%s (%s) does not belong to family %s", s.Name, s.Addr, tt.filter.family)
				}
				if tt.filter.transport == TransportPlain && s.TLSName != "" || tt.filter.transport == TransportDoT && s.TLSName == "" {
					t.Errorf("%s does not belong to transport %s", s.Name, tt.filter.transport)
				}
				if tt.filter.primaryOnly && !strings.HasSuffix(s.Name, "-1") {
					t.Errorf("%s is not a first address", s.Name)
				}
			}
		})
	}

	// Existing reports and scripts refer to these names.
	first := builtinServers(builtinFilter{})[0]
	if first.Name != "Cloudflare-1" || first.Addr != "1.1.1.1" || first.TLSName != "" {
		t.Errorf("first default resolver = %+v, want plain Cloudflare-1 at 1.1.1.1", first)
	}
}

// TestBuiltinServersAnswer sends one lookup to every built-in resolver on
// every transport and family. It needs the internet and IPv6, so it runs
// only when DNSBENCH_LIVE is set. Run it before changing data.go.
func TestBuiltinServersAnswer(t *testing.T) {
	if os.Getenv("DNSBENCH_LIVE") == "" {
		t.Skip("set DNSBENCH_LIVE=1 to query the built-in resolvers")
	}

	servers := builtinServers(builtinFilter{family: FamilyAll, transport: TransportAll})
	var wg sync.WaitGroup
	for _, server := range servers {
		wg.Go(func() {
			r := NewResolver(server, 1)
			var err error
			for range 3 {
				var took time.Duration
				took, err = r.QueryDNS(context.Background(), "wikipedia.org", 4*time.Second, ResolverRetryDisabled)
				if err == nil {
					t.Logf("%-32s %-24s %v", server.Name, server.Addr, took.Round(time.Millisecond))
					return
				}
			}
			t.Errorf("%s (%s) did not answer: %v", server.Name, server.Addr, err)
		})
	}
	wg.Wait()
}

func TestLoadServers(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		want    []DNSServer
		wantErr string
	}{
		{
			name: "plain, DoT, and IPv6",
			file: "# comment\nCF;1.1.1.1\n\nCF-DoT; 1.1.1.1 ; cloudflare-dns.com\nRouter;fe80::1%eth0\n",
			want: []DNSServer{
				{Name: "CF", Addr: "1.1.1.1"},
				{Name: "CF-DoT", Addr: "1.1.1.1", TLSName: "cloudflare-dns.com"},
				{Name: "Router", Addr: "fe80::1%eth0"},
			},
		},
		{name: "too many fields", file: "a;1.1.1.1;x.example;extra\n", wantErr: "invalid format at line 1"},
		{name: "bad TLS name", file: "a;1.1.1.1;not a name\n", wantErr: "invalid TLS name at line 1"},
		{name: "hostname address", file: "a;dns.google\n", wantErr: "invalid IP address at line 1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "resolvers.txt")
			if err := os.WriteFile(path, []byte(tt.file), 0o600); err != nil {
				t.Fatal(err)
			}
			// The filter must not apply to a file.
			got, err := loadServers(path, builtinFilter{onlyMajor: true, family: FamilyIPv6, transport: TransportDoT})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("loadServers() error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("loadServers() error = %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("loadServers() = %+v, want %+v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("server %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestParseFamily(t *testing.T) {
	for in, want := range map[string]AddrFamily{"ipv4": FamilyIPv4, "IPv6": FamilyIPv6, "all": FamilyAll} {
		got, err := parseFamily(in)
		if err != nil || got != want {
			t.Errorf("parseFamily(%q) = %v, %v, want %v", in, got, err, want)
		}
		if got.String() != strings.ToLower(in) {
			t.Errorf("AddrFamily(%d).String() = %q, want %q", got, got.String(), strings.ToLower(in))
		}
	}
	if _, err := parseFamily("6"); err == nil {
		t.Error(`parseFamily("6") returned no error`)
	}
}

func TestParseTransport(t *testing.T) {
	for in, want := range map[string]Transport{"plain": TransportPlain, "DoT": TransportDoT, "all": TransportAll} {
		got, err := parseTransport(in)
		if err != nil || got != want {
			t.Errorf("parseTransport(%q) = %v, %v, want %v", in, got, err, want)
		}
		if got.String() != strings.ToLower(in) {
			t.Errorf("Transport(%d).String() = %q, want %q", got, got.String(), strings.ToLower(in))
		}
	}
	if _, err := parseTransport("doh"); err == nil {
		t.Error(`parseTransport("doh") returned no error`)
	}
}

func TestIsValidServerAddr(t *testing.T) {
	tests := map[string]bool{
		"1.1.1.1":                   true,
		"2606:4700:4700::1111":      true,
		"fe80::1%eth0":              true,
		"1.1.1.1%eth0":              false,
		"1.1.1.1:53":                false,
		"[2606:4700:4700::1111]:53": false,
		"dns.google":                false,
		"":                          false,
	}
	for addr, want := range tests {
		if got := isValidServerAddr(addr); got != want {
			t.Errorf("isValidServerAddr(%q) = %v, want %v", addr, got, want)
		}
	}
}
