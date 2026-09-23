package dnsclient

import (
	"crypto/tls"
	"strings"
	"testing"
	"time"

	"github.com/handsomefox/dnsbench/internal/dnstest"
)

func TestResolver_DNSOverQUIC(t *testing.T) {
	f, tlsConfig := dnstest.StartDoQ(t)
	named := func(name string) *tls.Config {
		c := tlsConfig.Clone()
		c.ServerName = name
		return c
	}

	t.Run("answers on one connection", func(t *testing.T) {
		r := newDoQResolver("127.0.0.1", f.HostPort, named("dns.test"), 2)
		defer r.Close()
		for range 3 {
			// checkAnswer rejects an answer whose ID differs from its
			// query, so success also shows that the ID was restored.
			if _, err := r.Query(t.Context(), "doq.example.", 2*time.Second, 0); err != nil {
				t.Fatalf("Query() error = %v", err)
			}
		}
		if got := f.Queries.Load(); got != 3 {
			t.Errorf("server saw %d queries, want 3: one A query per lookup", got)
		}
		if got := f.Conns.Load(); got != 1 {
			t.Errorf("server saw %d connections, want 1 shared by every lookup", got)
		}
		if got := f.NonzeroIDs.Load(); got != 0 {
			t.Errorf("%d queries had a nonzero message ID", got)
		}
		if got := f.Unfinished.Load(); got != 0 {
			t.Errorf("%d streams did not end after the query", got)
		}
	})

	t.Run("NXDOMAIN is final", func(t *testing.T) {
		r := newDoQResolver("127.0.0.1", f.HostPort, named("dns.test"), 1)
		defer r.Close()
		lookup, err := r.Query(t.Context(), "nx.example.", 2*time.Second, 2)
		if err == nil {
			t.Fatal("Query() succeeded for a name that does not exist")
		}
		if lookup.Attempts != 1 {
			t.Errorf("Query() made %d attempts, so it retried a final answer", lookup.Attempts)
		}
	})

	t.Run("certificate for another name", func(t *testing.T) {
		r := newDoQResolver("127.0.0.1", f.HostPort, named("other.test"), 1)
		defer r.Close()
		err := r.Precheck(t.Context())
		if err == nil || !strings.Contains(err.Error(), "TLS certificate") {
			t.Errorf("Precheck() = %v, want a certificate error", err)
		}
	})

	t.Run("server refuses the name", func(t *testing.T) {
		r := newDoQResolver("127.0.0.1", f.HostPort, named("refuse.test"), 1)
		defer r.Close()
		err := r.Precheck(t.Context())
		if err == nil || !strings.Contains(err.Error(), "refused the TLS handshake") {
			t.Errorf("Precheck() = %v, want a refused handshake", err)
		}
	})

	t.Run("reconnects after close", func(t *testing.T) {
		r := newDoQResolver("127.0.0.1", f.HostPort, named("dns.test"), 1)
		defer r.Close()
		before := f.Conns.Load()
		for range 2 {
			if _, err := r.Query(t.Context(), "doq.example.", 2*time.Second, 0); err != nil {
				t.Fatalf("Query() error = %v", err)
			}
			r.Close()
		}
		if got := f.Conns.Load() - before; got != 2 {
			t.Errorf("server saw %d new connections, want 2", got)
		}
	})
}
