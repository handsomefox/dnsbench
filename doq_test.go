package main

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
)

// fakeDoQ counts what a DNS over QUIC server sees.
type fakeDoQ struct {
	hostPort   string
	conns      atomic.Int32
	queries    atomic.Int32
	nonzeroIDs atomic.Int32 // RFC 9250 requires message ID 0
	unfinished atomic.Int32 // streams whose query had no FIN after it
}

// startFakeDoQ serves DNS over QUIC on loopback with a certificate for
// dns.test, answering on each stream the way RFC 9250 describes. Like
// AdGuard, it aborts the handshake with an alert for the name refuse.test.
func startFakeDoQ(t *testing.T) (*fakeDoQ, *tls.Config) {
	t.Helper()
	cert, roots := selfSignedCert(t, "dns.test")
	base := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"doq"},
		MinVersion:   tls.VersionTLS13,
	}
	server := base.Clone()
	server.GetConfigForClient = func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		if hello.ServerName == "refuse.test" {
			return nil, errors.New("unknown server name")
		}
		return base, nil
	}
	ln, err := quic.ListenAddr("127.0.0.1:0", server, nil)
	if err != nil {
		t.Skipf("cannot listen on loopback UDP: %v", err)
	}
	t.Cleanup(func() { closeQuietly(ln) })

	f := &fakeDoQ{hostPort: ln.Addr().String()}
	go func() {
		for {
			conn, err := ln.Accept(t.Context())
			if err != nil {
				return
			}
			f.conns.Add(1)
			go f.serveConn(t.Context(), conn)
		}
	}()

	client := &tls.Config{RootCAs: roots, NextProtos: []string{"doq"}, MinVersion: tls.VersionTLS13}
	return f, client
}

func (f *fakeDoQ) serveConn(ctx context.Context, conn *quic.Conn) {
	for {
		stream, err := conn.AcceptStream(ctx)
		if err != nil {
			return
		}
		go func() {
			defer closeQuietly(stream)
			// The client closes its side after the query, so ReadAll ends.
			msg, err := io.ReadAll(stream)
			if err != nil || len(msg) < 14 {
				f.unfinished.Add(1)
				return
			}
			query := msg[2:]
			f.queries.Add(1)
			if binary.BigEndian.Uint16(query) != 0 {
				f.nonzeroIDs.Add(1)
			}
			answer, ok := fakeAnswer(query, false)
			if !ok {
				return
			}
			out := binary.BigEndian.AppendUint16(nil, uint16(len(answer))) //nolint:gosec // answers are far below 64 KiB
			if _, err := stream.Write(append(out, answer...)); err != nil {
				return
			}
		}()
	}
}

func TestResolver_DNSOverQUIC(t *testing.T) {
	f, tlsConfig := startFakeDoQ(t)
	named := func(name string) *tls.Config {
		c := tlsConfig.Clone()
		c.ServerName = name
		return c
	}

	t.Run("answers on one connection", func(t *testing.T) {
		r := newDoQResolver("127.0.0.1", f.hostPort, named("dns.test"), 2)
		defer r.Close()
		for range 3 {
			// checkAnswer rejects an answer whose ID differs from its
			// query, so success also shows that the ID was restored.
			if _, err := r.QueryDNS(t.Context(), "doq.example.", 2*time.Second, ResolverRetryDisabled); err != nil {
				t.Fatalf("QueryDNS() error = %v", err)
			}
		}
		if got := f.queries.Load(); got != 3 {
			t.Errorf("server saw %d queries, want 3: one A query per lookup", got)
		}
		if got := f.conns.Load(); got != 1 {
			t.Errorf("server saw %d connections, want 1 shared by every lookup", got)
		}
		if got := f.nonzeroIDs.Load(); got != 0 {
			t.Errorf("%d queries had a nonzero message ID", got)
		}
		if got := f.unfinished.Load(); got != 0 {
			t.Errorf("%d streams did not end after the query", got)
		}
	})

	t.Run("NXDOMAIN is final", func(t *testing.T) {
		r := newDoQResolver("127.0.0.1", f.hostPort, named("dns.test"), 1)
		defer r.Close()
		start := time.Now()
		if _, err := r.QueryDNS(t.Context(), "nx.example.", 2*time.Second, ResolverRetryEnabled); err == nil {
			t.Fatal("QueryDNS() succeeded for a name that does not exist")
		}
		if took := time.Since(start); took > 900*time.Millisecond {
			t.Errorf("QueryDNS() took %v, so it retried a final answer", took)
		}
	})

	t.Run("certificate for another name", func(t *testing.T) {
		r := newDoQResolver("127.0.0.1", f.hostPort, named("other.test"), 1)
		defer r.Close()
		err := r.Precheck(t.Context())
		if err == nil || !strings.Contains(err.Error(), "TLS certificate") {
			t.Errorf("Precheck() = %v, want a certificate error", err)
		}
	})

	t.Run("server refuses the name", func(t *testing.T) {
		r := newDoQResolver("127.0.0.1", f.hostPort, named("refuse.test"), 1)
		defer r.Close()
		err := r.Precheck(t.Context())
		if err == nil || !strings.Contains(err.Error(), "refused the TLS handshake") {
			t.Errorf("Precheck() = %v, want a refused handshake", err)
		}
	})

	t.Run("reconnects after close", func(t *testing.T) {
		r := newDoQResolver("127.0.0.1", f.hostPort, named("dns.test"), 1)
		defer r.Close()
		before := f.conns.Load()
		for range 2 {
			if _, err := r.QueryDNS(t.Context(), "doq.example.", 2*time.Second, ResolverRetryDisabled); err != nil {
				t.Fatalf("QueryDNS() error = %v", err)
			}
			r.Close()
		}
		if got := f.conns.Load() - before; got != 2 {
			t.Errorf("server saw %d new connections, want 2", got)
		}
	})
}
