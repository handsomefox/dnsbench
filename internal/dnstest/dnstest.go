// Package dnstest runs fake DNS servers on loopback for tests: plain DNS
// over UDP and TCP, DNS over TLS, DNS over HTTPS, DNS over QUIC, and a
// server that never answers. Each counts the queries it sees.
package dnstest

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
)

// DNS answers A queries with 192.0.2.1 over TCP. Over UDP it only sets
// the truncation bit, so a lookup succeeds only if it retries over TCP.
type DNS struct {
	HostPort   string
	TCPQueries atomic.Int32
}

func StartDNS(t *testing.T) *DNS {
	t.Helper()

	var lc net.ListenConfig
	tcp, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot listen on loopback: %v", err)
	}
	t.Cleanup(func() { CloseQuietly(tcp) })

	udp, err := lc.ListenPacket(t.Context(), "udp", tcp.Addr().String())
	if err != nil {
		t.Skipf("cannot listen on loopback UDP: %v", err)
	}
	t.Cleanup(func() { CloseQuietly(udp) })

	f := &DNS{HostPort: tcp.Addr().String()}

	go func() {
		buf := make([]byte, 512)
		for {
			n, from, err := udp.ReadFrom(buf)
			if err != nil {
				return
			}
			resp, ok := Answer(buf[:n], true)
			if !ok {
				continue
			}
			if _, err := udp.WriteTo(resp, from); err != nil {
				return
			}
		}
	}()

	go serveDNSStream(tcp, &f.TCPQueries)

	return f
}

// serveDNSStream answers length-prefixed DNS queries on every connection
// that ln accepts, as a TCP or DoT server does, and counts the queries.
func serveDNSStream(ln net.Listener, queries *atomic.Int32) {
	serveDNSStreamWith(ln, queries, nil, false)
}

// serveDNSStreamWith is serveDNSStream that also counts connections in
// conns, when it is not nil. With oneShot, it closes each connection after
// its first answer, as a server does with a connection it considers idle.
func serveDNSStreamWith(ln net.Listener, queries, conns *atomic.Int32, oneShot bool) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		if conns != nil {
			conns.Add(1)
		}
		go func() {
			defer CloseQuietly(conn)
			for {
				var size [2]byte
				if _, err := io.ReadFull(conn, size[:]); err != nil {
					return
				}
				query := make([]byte, binary.BigEndian.Uint16(size[:]))
				if _, err := io.ReadFull(conn, query); err != nil {
					return
				}
				queries.Add(1)
				resp, ok := Answer(query, false)
				if !ok {
					return
				}
				msg := binary.BigEndian.AppendUint16(nil, uint16(len(resp))) //nolint:gosec // resp is far below 64 KiB
				if _, err := conn.Write(append(msg, resp...)); err != nil {
					return
				}
				if oneShot {
					return
				}
			}
		}()
	}
}

// DoT is a DNS over TLS server on loopback with a self-signed
// certificate for dns.test, which counts its connections and queries.
type DoT struct {
	HostPort string
	Roots    *x509.CertPool
	Queries  atomic.Int32
	Conns    atomic.Int32
}

func (f *DoT) Config() *tls.Config {
	return &tls.Config{ServerName: "dns.test", RootCAs: f.Roots, MinVersion: tls.VersionTLS12}
}

// StartDoT starts a DoT. With oneShot, the server closes each
// connection after its first answer.
func StartDoT(t *testing.T, oneShot bool) *DoT {
	t.Helper()
	cert, roots := SelfSignedCert(t, "dns.test")

	var lc net.ListenConfig
	tcp, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot listen on loopback: %v", err)
	}
	ln := tls.NewListener(tcp, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	t.Cleanup(func() { CloseQuietly(ln) })

	f := &DoT{HostPort: tcp.Addr().String(), Roots: roots}
	go serveDNSStreamWith(ln, &f.Queries, &f.Conns, oneShot)
	return f
}

// SelfSignedCert returns a certificate for name and a pool that trusts it.
func SelfSignedCert(t *testing.T, name string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, roots
}

// StartDoH serves DNS over HTTPS at /dns-query with httptest's
// certificate, which is valid for example.com.
func StartDoH(t *testing.T) (hostPort string, roots *x509.CertPool, queries *atomic.Int32) {
	t.Helper()
	queries = &atomic.Int32{}
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/dns-query" || r.Header.Get("Content-Type") != "application/dns-message" {
			http.Error(w, "not a DoH request", http.StatusBadRequest)
			return
		}
		query, err := io.ReadAll(r.Body)
		if err != nil {
			return
		}
		queries.Add(1)
		answer, ok := Answer(query, false)
		if !ok {
			http.Error(w, "bad query", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/dns-message")
		if _, err := w.Write(answer); err != nil {
			return
		}
	}))
	ts.EnableHTTP2 = true
	ts.StartTLS()
	t.Cleanup(ts.Close)

	roots = x509.NewCertPool()
	roots.AddCert(ts.Certificate())
	return ts.Listener.Addr().String(), roots, queries
}

// CloseQuietly closes c in a test helper, where a close error changes nothing.
func CloseQuietly(c io.Closer) {
	if err := c.Close(); err != nil {
		return
	}
}

// Answer builds a response to query. A truncated response carries no
// answers. A full response to an A query carries one A record.
func Answer(query []byte, truncated bool) ([]byte, bool) {
	if len(query) < 12 {
		return nil, false
	}
	// The question is the name's labels, a zero byte, then type and class.
	i := 12
	for i < len(query) && query[i] != 0 {
		i += int(query[i]) + 1
	}
	end := i + 5
	if end > len(query) {
		return nil, false
	}
	isA := binary.BigEndian.Uint16(query[i+1:]) == 1
	// Names whose first label is "nx" do not exist.
	nx := query[12] == 2 && string(query[13:15]) == "nx"

	flags := uint16(0x8180) // QR, RD, RA
	var answers uint16
	switch {
	case nx:
		flags |= 3 // NXDOMAIN
	case truncated:
		flags |= 0x0200 // TC
	case isA:
		answers = 1
	}

	resp := append([]byte{}, query[0], query[1])
	resp = binary.BigEndian.AppendUint16(resp, flags)
	resp = binary.BigEndian.AppendUint16(resp, 1) // QDCOUNT
	resp = binary.BigEndian.AppendUint16(resp, answers)
	resp = binary.BigEndian.AppendUint16(resp, 0) // NSCOUNT
	resp = binary.BigEndian.AppendUint16(resp, 0) // ARCOUNT
	resp = append(resp, query[12:end]...)
	if answers == 1 {
		// Name pointer to the question, type A, class IN, TTL 60, 192.0.2.1.
		resp = append(resp, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 60, 0, 4, 192, 0, 2, 1)
	}
	return resp, true
}

// StartSilent reads DNS queries on a loopback UDP port and never
// answers, like a resolver behind a firewall that drops packets.
func StartSilent(t *testing.T) (hostPort string, queries *atomic.Int32) {
	t.Helper()
	var lc net.ListenConfig
	udp, err := lc.ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot listen on loopback UDP: %v", err)
	}
	t.Cleanup(func() { CloseQuietly(udp) })
	queries = &atomic.Int32{}
	go func() {
		buf := make([]byte, 512)
		for {
			if _, _, err := udp.ReadFrom(buf); err != nil {
				return
			}
			queries.Add(1)
		}
	}()
	return udp.LocalAddr().String(), queries
}

// DoQ counts what a DNS over QUIC server sees.
type DoQ struct {
	HostPort   string
	Conns      atomic.Int32
	Queries    atomic.Int32
	NonzeroIDs atomic.Int32 // RFC 9250 requires message ID 0
	Unfinished atomic.Int32 // streams whose query had no FIN after it
}

// StartDoQ serves DNS over QUIC on loopback with a certificate for
// dns.test, answering on each stream the way RFC 9250 describes. Like
// AdGuard, it aborts the handshake with an alert for the name refuse.test.
func StartDoQ(t *testing.T) (*DoQ, *tls.Config) {
	t.Helper()
	cert, roots := SelfSignedCert(t, "dns.test")
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
	t.Cleanup(func() { CloseQuietly(ln) })

	f := &DoQ{HostPort: ln.Addr().String()}
	go func() {
		for {
			conn, err := ln.Accept(t.Context())
			if err != nil {
				return
			}
			f.Conns.Add(1)
			go f.serveConn(t.Context(), conn)
		}
	}()

	client := &tls.Config{RootCAs: roots, NextProtos: []string{"doq"}, MinVersion: tls.VersionTLS13}
	return f, client
}

func (f *DoQ) serveConn(ctx context.Context, conn *quic.Conn) {
	for {
		stream, err := conn.AcceptStream(ctx)
		if err != nil {
			return
		}
		go func() {
			defer CloseQuietly(stream)
			// The client closes its side after the query, so ReadAll ends.
			msg, err := io.ReadAll(stream)
			if err != nil || len(msg) < 14 {
				f.Unfinished.Add(1)
				return
			}
			query := msg[2:]
			f.Queries.Add(1)
			if binary.BigEndian.Uint16(query) != 0 {
				f.NonzeroIDs.Add(1)
			}
			answer, ok := Answer(query, false)
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
