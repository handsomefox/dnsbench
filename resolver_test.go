package main

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
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestResolver_QueryDNS(t *testing.T) {
	tests := []struct {
		name       string
		serverAddr string
		domain     string
		timeout    time.Duration
		retry      ResolverRetry
		wantErr    bool
		errMessage string
	}{
		{
			name:       "Valid query with Google DNS",
			serverAddr: "8.8.8.8",
			domain:     "google.com",
			timeout:    2 * time.Second,
			retry:      ResolverRetryDisabled,
			wantErr:    false,
		},
		{
			name:       "Empty domain",
			serverAddr: "8.8.8.8",
			domain:     "",
			timeout:    2 * time.Second,
			retry:      ResolverRetryDisabled,
			wantErr:    true,
			errMessage: "empty domain name",
		},
		{
			name:       "Invalid resolver IP",
			serverAddr: "256.256.256.256",
			domain:     "google.com",
			timeout:    2 * time.Second,
			retry:      ResolverRetryDisabled,
			wantErr:    true,
		},
		{
			name:       "Invalid domain",
			serverAddr: "8.8.8.8",
			domain:     "thisisnotavaliddomain.invalidtld",
			timeout:    2 * time.Second,
			retry:      ResolverRetryDisabled,
			wantErr:    true,
		},
		{
			name:       "Timeout too short",
			serverAddr: "8.8.8.8",
			domain:     "google.com",
			timeout:    1 * time.Microsecond,
			retry:      ResolverRetryDisabled,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			r := NewResolver(DNSServer{Addr: tt.serverAddr}, 1)

			_, err := r.QueryDNS(ctx, tt.domain, tt.timeout, tt.retry)
			if !tt.wantErr && err != nil {
				if strings.Contains(err.Error(), "operation not permitted") || strings.Contains(err.Error(), "network is unreachable") {
					t.Skipf("skipping due to restricted network: %v", err)
				}
			}
			if (err != nil) != tt.wantErr {
				t.Errorf("QueryDNS() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && tt.errMessage != "" && err != nil {
				if !errors.Is(err, context.DeadlineExceeded) && err.Error() != tt.errMessage {
					t.Errorf("QueryDNS() error message = %v, want %v", err.Error(), tt.errMessage)
				}
			}
		})
	}
}

// fakeDNS answers A queries with 192.0.2.1 over TCP. Over UDP it only sets
// the truncation bit, so a lookup succeeds only if it retries over TCP.
type fakeDNS struct {
	hostPort   string
	tcpQueries atomic.Int32
}

func startFakeDNS(t *testing.T) *fakeDNS {
	t.Helper()

	var lc net.ListenConfig
	tcp, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot listen on loopback: %v", err)
	}
	t.Cleanup(func() { closeQuietly(tcp) })

	udp, err := lc.ListenPacket(t.Context(), "udp", tcp.Addr().String())
	if err != nil {
		t.Skipf("cannot listen on loopback UDP: %v", err)
	}
	t.Cleanup(func() { closeQuietly(udp) })

	f := &fakeDNS{hostPort: tcp.Addr().String()}

	go func() {
		buf := make([]byte, 512)
		for {
			n, from, err := udp.ReadFrom(buf)
			if err != nil {
				return
			}
			resp, ok := fakeAnswer(buf[:n], true)
			if !ok {
				continue
			}
			if _, err := udp.WriteTo(resp, from); err != nil {
				return
			}
		}
	}()

	go serveDNSStream(tcp, &f.tcpQueries)

	return f
}

// serveDNSStream answers length-prefixed DNS queries on every connection
// that ln accepts, as a TCP or DoT server does, and counts the queries.
func serveDNSStream(ln net.Listener, queries *atomic.Int32) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer closeQuietly(conn)
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
				resp, ok := fakeAnswer(query, false)
				if !ok {
					return
				}
				msg := binary.BigEndian.AppendUint16(nil, uint16(len(resp))) //nolint:gosec // resp is far below 64 KiB
				if _, err := conn.Write(append(msg, resp...)); err != nil {
					return
				}
			}
		}()
	}
}

// startFakeDoT serves DNS over TLS on loopback with a self-signed
// certificate for dns.test. It returns the address, a pool that trusts the
// certificate, and a count of the queries the server answered.
func startFakeDoT(t *testing.T) (hostPort string, roots *x509.CertPool, queries *atomic.Int32) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "dns.test"},
		DNSNames:     []string{"dns.test"},
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
	roots = x509.NewCertPool()
	roots.AddCert(cert)

	var lc net.ListenConfig
	tcp, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("cannot listen on loopback: %v", err)
	}
	ln := tls.NewListener(tcp, &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
		MinVersion:   tls.VersionTLS12,
	})
	t.Cleanup(func() { closeQuietly(ln) })

	queries = &atomic.Int32{}
	go serveDNSStream(ln, queries)
	return tcp.Addr().String(), roots, queries
}

func TestResolver_DNSOverTLS(t *testing.T) {
	hostPort, roots, queries := startFakeDoT(t)

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
			_, err := r.QueryDNS(t.Context(), "dot.example", 2*time.Second, ResolverRetryDisabled)
			if tt.wantErr {
				if err == nil {
					t.Fatal("QueryDNS() succeeded against a certificate for another name")
				}
				if queries.Load() != before {
					t.Error("a query reached the server before the certificate check")
				}
				return
			}
			if err != nil {
				t.Fatalf("QueryDNS() error = %v", err)
			}
			if queries.Load() == before {
				t.Fatal("QueryDNS() never reached the DoT server")
			}
		})
	}
}

func TestResolver_PrecheckCertificate(t *testing.T) {
	hostPort, roots, _ := startFakeDoT(t)

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

func TestNewResolver_Ports(t *testing.T) {
	if got := NewResolver(DNSServer{Addr: "2001:db8::1"}, 1).hostPort; got != "[2001:db8::1]:53" {
		t.Errorf("plain resolver dials %s, want [2001:db8::1]:53", got)
	}
	if got := NewResolver(DNSServer{Addr: "192.0.2.1", TLSName: "dns.test"}, 1).hostPort; got != "192.0.2.1:853" {
		t.Errorf("DoT resolver dials %s, want 192.0.2.1:853", got)
	}
}

// closeQuietly closes c in a test helper, where a close error changes nothing.
func closeQuietly(c io.Closer) {
	if err := c.Close(); err != nil {
		return
	}
}

// fakeAnswer builds a response to query. A truncated response carries no
// answers. A full response to an A query carries one A record.
func fakeAnswer(query []byte, truncated bool) ([]byte, bool) {
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

// An NXDOMAIN answer is final. With retries on, a lookup that retried it
// would wait at least a second of backoff before the second attempt.
func TestResolver_DoesNotRetryNXDOMAIN(t *testing.T) {
	hostPort, roots, queries := startFakeDoT(t)
	r := newResolver("127.0.0.1", hostPort, &tls.Config{ServerName: "dns.test", RootCAs: roots, MinVersion: tls.VersionTLS12}, 1)

	start := time.Now()
	_, err := r.QueryDNS(t.Context(), "nx.example", 2*time.Second, ResolverRetryEnabled)
	if err == nil {
		t.Fatal("QueryDNS() succeeded for a name that does not exist")
	}
	if took := time.Since(start); took > 900*time.Millisecond {
		t.Errorf("QueryDNS() took %v, so it retried a final answer", took)
	}
	// One lookup asks for A and AAAA, so one attempt sends two queries.
	if got := queries.Load(); got > 2 {
		t.Errorf("server saw %d queries, want at most 2", got)
	}
}

func TestResolver_RetriesTruncatedAnswersOverTCP(t *testing.T) {
	f := startFakeDNS(t)
	r := newResolver("127.0.0.1", f.hostPort, nil, 1)

	if _, err := r.QueryDNS(t.Context(), "truncated.example", 2*time.Second, ResolverRetryDisabled); err != nil {
		t.Fatalf("QueryDNS() error = %v", err)
	}
	if f.tcpQueries.Load() == 0 {
		t.Fatal("QueryDNS() never retried over TCP")
	}
}
