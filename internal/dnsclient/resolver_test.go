package dnsclient

import (
	"context"
	"crypto/tls"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/handsomefox/dnsbench/internal/dnstest"
)

func TestResolver_QueryDNS(t *testing.T) {
	tests := []struct {
		name       string
		serverAddr string
		domain     string
		timeout    time.Duration
		retries    int
		wantErr    bool
		errMessage string
	}{
		{
			name:       "Valid query with Google DNS",
			serverAddr: "8.8.8.8",
			domain:     "google.com",
			timeout:    2 * time.Second,
			retries:    0,
			wantErr:    false,
		},
		{
			name:       "Empty domain",
			serverAddr: "8.8.8.8",
			domain:     "",
			timeout:    2 * time.Second,
			retries:    0,
			wantErr:    true,
			errMessage: "empty domain name",
		},
		{
			name:       "Invalid resolver IP",
			serverAddr: "256.256.256.256",
			domain:     "google.com",
			timeout:    2 * time.Second,
			retries:    0,
			wantErr:    true,
		},
		{
			name:       "Invalid domain",
			serverAddr: "8.8.8.8",
			domain:     "thisisnotavaliddomain.invalidtld",
			timeout:    2 * time.Second,
			retries:    0,
			wantErr:    true,
		},
		{
			name:       "Timeout too short",
			serverAddr: "8.8.8.8",
			domain:     "google.com",
			timeout:    1 * time.Microsecond,
			retries:    0,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			r := New(Server{Addr: tt.serverAddr}, 1)

			_, err := r.Query(ctx, tt.domain, tt.timeout, tt.retries)
			if !tt.wantErr && err != nil {
				if strings.Contains(err.Error(), "operation not permitted") || strings.Contains(err.Error(), "network is unreachable") {
					t.Skipf("skipping due to restricted network: %v", err)
				}
			}
			if (err != nil) != tt.wantErr {
				t.Errorf("Query() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.errMessage != "" && err != nil {
				if !errors.Is(err, context.DeadlineExceeded) && err.Error() != tt.errMessage {
					t.Errorf("Query() error message = %v, want %v", err.Error(), tt.errMessage)
				}
			}
		})
	}
}

// DoT keeps its connection between lookups, as DoH and DoQ do.
func TestResolver_DoTReusesConnections(t *testing.T) {
	f := dnstest.StartDoT(t, false)
	r := newResolver("127.0.0.1", f.HostPort, f.Config(), 1)
	defer r.Close()
	for range 3 {
		if _, err := r.Query(t.Context(), "dot.example", 2*time.Second, 0); err != nil {
			t.Fatalf("Query() error = %v", err)
		}
	}
	if got := f.Queries.Load(); got != 3 {
		t.Errorf("server saw %d queries, want 3", got)
	}
	if got := f.Conns.Load(); got != 1 {
		t.Errorf("server saw %d connections, want 1 shared by every lookup", got)
	}
}

// A server may close a connection that the resolver keeps for later. The
// next lookup must then open a fresh one within its only attempt.
func TestResolver_DoTRecoversFromClosedIdleConnection(t *testing.T) {
	f := dnstest.StartDoT(t, true)
	r := newResolver("127.0.0.1", f.HostPort, f.Config(), 1)
	defer r.Close()
	for i := range 3 {
		if _, err := r.Query(t.Context(), "dot.example", 2*time.Second, 0); err != nil {
			t.Fatalf("lookup %d: Query() error = %v", i+1, err)
		}
	}
	if got := f.Conns.Load(); got < 3 {
		t.Errorf("server saw %d connections, want a fresh one for each lookup", got)
	}
}

func TestResolver_DNSOverTLS(t *testing.T) {
	dot := dnstest.StartDoT(t, false)
	hostPort, roots, queries := dot.HostPort, dot.Roots, &dot.Queries

	tests := []struct {
		name       string
		serverName string
		wantErr    bool
	}{
		{name: "matching certificate", serverName: "dns.test"},
		{name: "certificate for another name", serverName: "other.test", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &tls.Config{ServerName: tt.serverName, RootCAs: roots, MinVersion: tls.VersionTLS12}
			r := newResolver("127.0.0.1", hostPort, cfg, 1)

			before := queries.Load()
			_, err := r.Query(t.Context(), "dot.example", 2*time.Second, 0)
			if tt.wantErr {
				if err == nil {
					t.Fatal("Query() succeeded against a certificate for another name")
				}
				if queries.Load() != before {
					t.Error("a query reached the server before the certificate check")
				}
				return
			}
			if err != nil {
				t.Fatalf("Query() error = %v", err)
			}
			if queries.Load() == before {
				t.Fatal("Query() never reached the DoT server")
			}
		})
	}
}

func TestResolver_PrecheckCertificate(t *testing.T) {
	dot := dnstest.StartDoT(t, false)
	hostPort, roots := dot.HostPort, dot.Roots

	good := newResolver("127.0.0.1", hostPort, &tls.Config{ServerName: "dns.test", RootCAs: roots, MinVersion: tls.VersionTLS12}, 1)
	if err := good.Precheck(t.Context()); err != nil {
		t.Errorf("Precheck() with a matching certificate = %v, want nil", err)
	}

	wrong := newResolver("127.0.0.1", hostPort, &tls.Config{ServerName: "other.test", RootCAs: roots, MinVersion: tls.VersionTLS12}, 1)
	err := wrong.Precheck(t.Context())
	if err == nil || !strings.Contains(err.Error(), "TLS certificate") {
		t.Errorf("Precheck() with a certificate for another name = %v, want a certificate error", err)
	}
}

func TestResolver_DNSOverHTTPS(t *testing.T) {
	hostPort, roots, queries := dnstest.StartDoH(t)
	tlsConfig := func(name string) *tls.Config {
		return &tls.Config{ServerName: name, RootCAs: roots, MinVersion: tls.VersionTLS12}
	}

	t.Run("answers", func(t *testing.T) {
		r := newDoHResolver("127.0.0.1", "https://example.com/dns-query", hostPort, tlsConfig("example.com"), 2)
		for range 3 {
			if _, err := r.Query(t.Context(), "doh.example.", 2*time.Second, 0); err != nil {
				t.Fatalf("Query() error = %v", err)
			}
		}
		// Three lookups, one A query each.
		if got := queries.Load(); got != 3 {
			t.Errorf("server saw %d queries, want 3", got)
		}
	})

	t.Run("NXDOMAIN is final", func(t *testing.T) {
		r := newDoHResolver("127.0.0.1", "https://example.com/dns-query", hostPort, tlsConfig("example.com"), 1)
		lookup, err := r.Query(t.Context(), "nx.example.", 2*time.Second, 2)
		if err == nil {
			t.Fatal("Query() succeeded for a name that does not exist")
		}
		if lookup.Attempts != 1 {
			t.Errorf("Query() made %d attempts, so it retried a final answer", lookup.Attempts)
		}
	})

	t.Run("HTTP error", func(t *testing.T) {
		r := newDoHResolver("127.0.0.1", "https://example.com/wrong-path", hostPort, tlsConfig("example.com"), 1)
		_, err := r.Query(t.Context(), "doh.example.", 2*time.Second, 0)
		if err == nil || !strings.Contains(err.Error(), "400 Bad Request") {
			t.Errorf("Query() error = %v, want it to report the 400", err)
		}
	})

	t.Run("certificate for another name", func(t *testing.T) {
		r := newDoHResolver("127.0.0.1", "https://other.test/dns-query", hostPort, tlsConfig("other.test"), 1)
		err := r.Precheck(t.Context())
		if err == nil || !strings.Contains(err.Error(), "TLS certificate") {
			t.Errorf("Precheck() = %v, want a certificate error", err)
		}
	})
}

func TestNewResolver_Ports(t *testing.T) {
	if got := New(Server{Addr: "2001:db8::1"}, 1).hostPort; got != "[2001:db8::1]:53" {
		t.Errorf("plain resolver dials %s, want [2001:db8::1]:53", got)
	}
	if got := New(Server{Addr: "192.0.2.1", TLSName: "dns.test"}, 1).hostPort; got != "192.0.2.1:853" {
		t.Errorf("DoT resolver dials %s, want 192.0.2.1:853", got)
	}
	if got := New(Server{Addr: "2001:db8::1", DoHURL: "https://dns.test/q"}, 1).hostPort; got != "[2001:db8::1]:443" {
		t.Errorf("DoH resolver dials %s, want [2001:db8::1]:443", got)
	}
	if got := New(Server{Addr: "192.0.2.1", DoQName: "dns.test"}, 1).hostPort; got != "192.0.2.1:853" {
		t.Errorf("DoQ resolver dials %s, want 192.0.2.1:853", got)
	}
}

// An NXDOMAIN answer is final, so a lookup with retries left makes one
// attempt and sends one query.
func TestResolver_DoesNotRetryNXDOMAIN(t *testing.T) {
	dot := dnstest.StartDoT(t, false)
	hostPort, roots, queries := dot.HostPort, dot.Roots, &dot.Queries
	r := newResolver("127.0.0.1", hostPort, &tls.Config{ServerName: "dns.test", RootCAs: roots, MinVersion: tls.VersionTLS12}, 1)

	lookup, err := r.Query(t.Context(), "nx.example", 2*time.Second, 2)
	if err == nil {
		t.Fatal("Query() succeeded for a name that does not exist")
	}
	if lookup.Attempts != 1 {
		t.Errorf("Query() made %d attempts, so it retried a final answer", lookup.Attempts)
	}
	if got := queries.Load(); got != 1 {
		t.Errorf("server saw %d queries, want 1", got)
	}
}

// A resolver that never answers gets the first attempt and every retry,
// and the retries wait well under a second each.
func TestResolver_RetriesUnansweredQueries(t *testing.T) {
	addr, queries := dnstest.StartSilent(t)
	r := newResolver("127.0.0.1", addr, nil, 1)

	start := time.Now()
	lookup, err := r.Query(t.Context(), "silent.example", 100*time.Millisecond, 2)
	if err == nil {
		t.Fatal("Query() succeeded against a server that never answers")
	}
	if lookup.Attempts != 3 {
		t.Errorf("Query() made %d attempts, want 3", lookup.Attempts)
	}
	if got := queries.Load(); got != 3 {
		t.Errorf("server saw %d queries, want 3", got)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("Query() took %v, want the retries to wait well under a second each", took)
	}
}

func TestResolver_RetriesTruncatedAnswersOverTCP(t *testing.T) {
	f := dnstest.StartDNS(t)
	r := newResolver("127.0.0.1", f.HostPort, nil, 1)

	if _, err := r.Query(t.Context(), "truncated.example", 2*time.Second, 0); err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if f.TCPQueries.Load() == 0 {
		t.Fatal("Query() never retried over TCP")
	}
}
