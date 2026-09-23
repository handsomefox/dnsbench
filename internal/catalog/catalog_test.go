package catalog

import (
	"context"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/handsomefox/dnsbench/internal/dnsclient"
)

func TestBuiltinServers(t *testing.T) {
	var plain4, plain6, dot4, dot6, doh4, doh6, doq4, doq6, majorPlain4, primaryPlain4 int
	for _, p := range services {
		if !p.encryptedOnly {
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
		if p.dohURL != "" {
			doh4 += len(p.ipv4)
			doh6 += len(p.ipv6)
		}
		if p.doqName != "" {
			doq4 += len(p.ipv4)
			doq6 += len(p.ipv6)
		}
	}

	tests := []struct {
		name   string
		filter Filter
		want   int
	}{
		{name: "default", filter: Filter{}, want: plain4},
		{name: "IPv6", filter: Filter{Family: FamilyIPv6}, want: plain6},
		{name: "both families", filter: Filter{Family: FamilyAll}, want: plain4 + plain6},
		{name: "major", filter: Filter{Major: true}, want: majorPlain4},
		{name: "first address", filter: Filter{Primary: true}, want: primaryPlain4},
		{name: "DoT", filter: Filter{Transport: TransportDoT}, want: dot4},
		{name: "DoH over IPv6", filter: Filter{Family: FamilyIPv6, Transport: TransportDoH}, want: doh6},
		{name: "DoQ", filter: Filter{Transport: TransportDoQ}, want: doq4},
		{name: "everything", filter: Filter{Family: FamilyAll, Transport: TransportAll}, want: plain4 + plain6 + dot4 + dot6 + doh4 + doh6 + doq4 + doq6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Servers(tt.filter)
			if len(got) != tt.want {
				t.Fatalf("Servers(%+v) returned %d resolvers, want %d", tt.filter, len(got), tt.want)
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
				if (s.DoHURL != "") != strings.Contains(s.Name, "-DoH-") {
					t.Errorf("%s has DoH URL %q: the -DoH- name and the transport disagree", s.Name, s.DoHURL)
				}
				if (s.DoQName != "") != strings.Contains(s.Name, "-DoQ-") {
					t.Errorf("%s has DoQ name %q: the -DoQ- name and the transport disagree", s.Name, s.DoQName)
				}
				if s.DoHURL != "" && !dnsclient.IsValidDoHURL(s.DoHURL) {
					t.Errorf("%s has invalid DoH URL %q", s.Name, s.DoHURL)
				}
				if tt.filter.Family == FamilyIPv4 && addr.Is6() || tt.filter.Family == FamilyIPv6 && addr.Is4() {
					t.Errorf("%s (%s) does not belong to family %s", s.Name, s.Addr, tt.filter.Family)
				}
				plain := s.TLSName == "" && s.DoHURL == "" && s.DoQName == ""
				if tt.filter.Transport == TransportPlain && !plain ||
					tt.filter.Transport == TransportDoT && s.TLSName == "" ||
					tt.filter.Transport == TransportDoH && s.DoHURL == "" ||
					tt.filter.Transport == TransportDoQ && s.DoQName == "" {
					t.Errorf("%s does not belong to transport %s", s.Name, tt.filter.Transport)
				}
				if tt.filter.Primary && !strings.HasSuffix(s.Name, "-1") {
					t.Errorf("%s is not a first address", s.Name)
				}
			}
		})
	}

	// Existing reports and scripts refer to these names.
	first := Servers(Filter{})[0]
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

	servers := Servers(Filter{Family: FamilyAll, Transport: TransportAll})
	var wg sync.WaitGroup
	for _, server := range servers {
		wg.Go(func() {
			r := dnsclient.New(server, 1)
			var err error
			for range 3 {
				var lookup dnsclient.Lookup
				lookup, err = r.Query(context.Background(), "wikipedia.org", 4*time.Second, 0)
				if err == nil {
					t.Logf("%-32s %-24s %v", server.Name, server.Addr, lookup.Latency.Round(time.Millisecond))
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
		want    []dnsclient.Server
		wantErr string
	}{
		{
			name: "plain, DoT, and IPv6",
			file: "# comment\nCF;1.1.1.1\n\nCF-DoT; 1.1.1.1 ; cloudflare-dns.com\nCF-DoH;1.1.1.1;https://cloudflare-dns.com/dns-query\nQ9-DoQ;9.9.9.9;quic://dns.quad9.net\nRouter;fe80::1%eth0\n",
			want: []dnsclient.Server{
				{Name: "CF", Addr: "1.1.1.1"},
				{Name: "CF-DoT", Addr: "1.1.1.1", TLSName: "cloudflare-dns.com"},
				{Name: "CF-DoH", Addr: "1.1.1.1", DoHURL: "https://cloudflare-dns.com/dns-query"},
				{Name: "Q9-DoQ", Addr: "9.9.9.9", DoQName: "dns.quad9.net"},
				{Name: "Router", Addr: "fe80::1%eth0"},
			},
		},
		{name: "too many fields", file: "a;1.1.1.1;x.example;extra\n", wantErr: "invalid format at line 1"},
		{name: "bad TLS name", file: "a;1.1.1.1;not a name\n", wantErr: "invalid TLS name at line 1"},
		{name: "DoH URL without host", file: "a;1.1.1.1;https:///dns-query\n", wantErr: "invalid DoH URL at line 1"},
		{name: "DoQ without a name", file: "a;1.1.1.1;quic://\n", wantErr: "invalid DoQ name at line 1"},
		{name: "hostname address", file: "a;dns.google\n", wantErr: "invalid IP address at line 1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "resolvers.txt")
			if err := os.WriteFile(path, []byte(tt.file), 0o600); err != nil {
				t.Fatal(err)
			}
			// The filter must not apply to a file.
			got, err := LoadServers(path, Filter{Major: true, Family: FamilyIPv6, Transport: TransportDoT})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("LoadServers() error = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadServers() error = %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("LoadServers() = %+v, want %+v", got, tt.want)
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
	for in, want := range map[string]Family{"ipv4": FamilyIPv4, "IPv6": FamilyIPv6, "all": FamilyAll} {
		got, err := ParseFamily(in)
		if err != nil || got != want {
			t.Errorf("ParseFamily(%q) = %v, %v, want %v", in, got, err, want)
		}
		if got.String() != strings.ToLower(in) {
			t.Errorf("Family(%d).String() = %q, want %q", got, got.String(), strings.ToLower(in))
		}
	}
	if _, err := ParseFamily("6"); err == nil {
		t.Error(`ParseFamily("6") returned no error`)
	}
}

func TestParseTransport(t *testing.T) {
	for in, want := range map[string]Transport{"plain": TransportPlain, "DoT": TransportDoT, "DoH": TransportDoH, "DoQ": TransportDoQ, "all": TransportAll} {
		got, err := ParseTransport(in)
		if err != nil || got != want {
			t.Errorf("ParseTransport(%q) = %v, %v, want %v", in, got, err, want)
		}
		if got.String() != strings.ToLower(in) {
			t.Errorf("Transport(%d).String() = %q, want %q", got, got.String(), strings.ToLower(in))
		}
	}
	if _, err := ParseTransport("http"); err == nil {
		t.Error(`ParseTransport("http") returned no error`)
	}
}

// The dashboard filters the catalog, and the CLI filters through
// Servers. Both must agree with the catalog's own fields.
func TestBuiltinCatalog(t *testing.T) {
	categories := map[string]bool{"Global": true, "Filtering": true, "Privacy": true, "Regional": true}
	names := map[string]bool{}
	for _, e := range All() {
		if !categories[e.Category] {
			t.Errorf("%s has unknown category %q", e.Name, e.Category)
		}
		if names[e.Name] {
			t.Errorf("duplicate name %s", e.Name)
		}
		names[e.Name] = true
		if e.Primary != strings.HasSuffix(e.Name, "-1") {
			t.Errorf("%s: primary is %v", e.Name, e.Primary)
		}
		plain := e.TLSName == "" && e.DoHURL == "" && e.DoQName == ""
		if plain != (e.Transport == "plain") {
			t.Errorf("%s has transport %s, which its fields contradict", e.Name, e.Transport)
		}
	}
}

func TestFilterKind(t *testing.T) {
	all := Servers(Filter{Family: FamilyAll, Transport: TransportAll})
	var total int
	for _, kind := range Kinds {
		got := Servers(Filter{Family: FamilyAll, Transport: TransportAll, Kind: kind})
		if len(got) == 0 {
			t.Errorf("kind %s selects no resolver", kind)
		}
		total += len(got)
	}
	if total != len(all) {
		t.Errorf("the kinds select %d resolvers together, want all %d", total, len(all))
	}
	for _, e := range All() {
		if e.Name == "Cloudflare-Family-1" && e.Category != "Filtering" {
			t.Errorf("Cloudflare-Family-1 has kind %s, want Filtering", e.Category)
		}
	}
}

func TestParseKind(t *testing.T) {
	for in, want := range map[string]string{"all": "", "filtering": "Filtering", "GLOBAL": "Global", "Privacy": "Privacy", "regional": "Regional"} {
		if got, err := ParseKind(in); err != nil || got != want {
			t.Errorf("ParseKind(%q) = %q, %v, want %q", in, got, err, want)
		}
	}
	if _, err := ParseKind("family"); err == nil {
		t.Error(`ParseKind("family") returned no error`)
	}
}

// Each service's name starts with its provider's, so the dashboard can
// group Cloudflare-Family under Cloudflare, and a typo in either shows.
func TestServiceProviders(t *testing.T) {
	for _, s := range services {
		if s.provider == "" || s.name != s.provider && !strings.HasPrefix(s.name, s.provider+"-") {
			t.Errorf("service %q has provider %q", s.name, s.provider)
		}
	}
}

func TestParseProviders(t *testing.T) {
	got, err := ParseProviders(" cloudflare, QUAD9 ,google,cloudflare,")
	if err != nil || !slices.Equal(got, []string{"Cloudflare", "Quad9", "Google"}) {
		t.Errorf("ParseProviders() = %q, %v", got, err)
	}
	if got, err := ParseProviders(""); err != nil || got != nil {
		t.Errorf(`ParseProviders("") = %q, %v, want no providers`, got, err)
	}
	if _, err := ParseProviders("cloudflare-family"); err == nil || !strings.Contains(err.Error(), "Cloudflare, Google") {
		t.Errorf("ParseProviders(cloudflare-family) error = %v, want one that lists the providers", err)
	}
}

// -provider keeps every service of the chosen companies, filtering ones
// included, and nothing else.
func TestFilterProviders(t *testing.T) {
	got := Servers(Filter{Primary: true, Providers: []string{"Cloudflare", "Quad9"}})
	names := make([]string, 0, len(got))
	for _, s := range got {
		names = append(names, s.Name)
	}
	want := []string{"Cloudflare-1", "Quad9-Unfiltered-1", "Quad9-1", "Quad9-ECS-1", "Cloudflare-Security-1", "Cloudflare-Family-1"}
	if !slices.Equal(names, want) {
		t.Errorf("Servers() = %q, want %q", names, want)
	}
}
