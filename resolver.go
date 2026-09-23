package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/url"
	"time"
)

type ResolverRetry bool

const (
	ResolverRetryDisabled ResolverRetry = false
	ResolverRetryEnabled  ResolverRetry = true
)

type Resolver struct {
	netResolver *net.Resolver
	dialer      *net.Dialer
	tlsDialer   *tls.Dialer // nil for plain DNS
	doq         *doqClient  // nil unless DNS over QUIC
	close       func()      // releases kept connections; nil when there are none
	hostPort    string
	serverAddr  string
	concurrency int
	sem         chan struct{}
}

// NewResolver queries server with plain DNS on port 53, or with DNS over
// TLS on port 853 when server.TLSName is set.
func NewResolver(server DNSServer, concurrency int) *Resolver {
	if server.DoQName != "" {
		tlsConfig := &tls.Config{ServerName: server.DoQName, MinVersion: tls.VersionTLS13, NextProtos: []string{"doq"}}
		return newDoQResolver(server.Addr, net.JoinHostPort(server.Addr, "853"), tlsConfig, concurrency)
	}
	if server.DoHURL != "" {
		// isValidDoHURL has checked the URL, so the error cannot happen.
		u, _ := url.Parse(server.DoHURL) //nolint:errcheck // validated by isValidDoHURL
		tlsConfig := &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12}
		return newDoHResolver(server.Addr, server.DoHURL, net.JoinHostPort(server.Addr, "443"), tlsConfig, concurrency)
	}
	if server.TLSName == "" {
		return newResolver(server.Addr, net.JoinHostPort(server.Addr, "53"), nil, concurrency)
	}
	tlsConfig := &tls.Config{
		ServerName: server.TLSName,
		MinVersion: tls.VersionTLS12,
		// Go's resolver opens a new connection for every query. Session
		// resumption at least lets the later handshakes skip the
		// certificate exchange, as a real DoT client would.
		ClientSessionCache: tls.NewLRUClientSessionCache(0),
	}
	return newResolver(server.Addr, net.JoinHostPort(server.Addr, "853"), tlsConfig, concurrency)
}

// newResolver sends every query to hostPort.
//
// For plain DNS, Go's resolver asks Dial for "udp" first and for "tcp" when
// the UDP answer comes back truncated, so Dial keeps the network it is given.
// With tlsConfig set, Dial always returns a TLS connection over TCP. It is
// not a net.PacketConn, so Go's resolver frames the queries for a stream,
// which is what DNS over TLS expects.
func newResolver(serverAddr, hostPort string, tlsConfig *tls.Config, concurrency int) *Resolver {
	dialer := &net.Dialer{}
	if concurrency < 1 {
		concurrency = 1
	}
	dial := func(ctx context.Context, network, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, hostPort)
	}
	var tlsDialer *tls.Dialer
	if tlsConfig != nil {
		tlsDialer = &tls.Dialer{NetDialer: dialer, Config: tlsConfig}
		dial = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return tlsDialer.DialContext(ctx, "tcp", hostPort)
		}
	}
	return &Resolver{
		netResolver: &net.Resolver{
			PreferGo: true,
			Dial:     dial,
		},
		dialer:      dialer,
		tlsDialer:   tlsDialer,
		hostPort:    hostPort,
		serverAddr:  serverAddr,
		concurrency: concurrency,
		sem:         make(chan struct{}, concurrency),
	}
}

// newDoHResolver sends every query as an HTTPS POST to dohURL, as RFC 8484
// describes. It connects to hostPort, not to the address the URL's host
// resolves to, so the benchmark measures the address under test. The HTTP
// client keeps its connections, so only the first lookups pay for the
// TCP and TLS handshakes.
func newDoHResolver(serverAddr, dohURL, hostPort string, tlsConfig *tls.Config, concurrency int) *Resolver {
	dialer := &net.Dialer{}
	if concurrency < 1 {
		concurrency = 1
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, hostPort)
		},
		TLSClientConfig:     tlsConfig,
		ForceAttemptHTTP2:   true,
		MaxIdleConnsPerHost: concurrency,
	}
	client := &http.Client{Transport: transport}
	return &Resolver{
		close: transport.CloseIdleConnections,
		netResolver: &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return &dohConn{ctx: ctx, client: client, url: dohURL}, nil
			},
		},
		dialer:      dialer,
		tlsDialer:   &tls.Dialer{NetDialer: dialer, Config: tlsConfig},
		hostPort:    hostPort,
		serverAddr:  serverAddr,
		concurrency: concurrency,
		sem:         make(chan struct{}, concurrency),
	}
}

// dohConn lets Go's resolver speak DNS over HTTPS. The resolver writes
// each query to a connection that is not a net.PacketConn with the TCP
// framing: a two-byte length, then the message. dohConn posts each message
// to the DoH URL and frames the answer the same way for the resolver to
// read back.
type dohConn struct {
	ctx     context.Context
	client  *http.Client
	url     string
	pending bytes.Buffer // written bytes that do not yet form a message
	answers bytes.Buffer // framed answers the resolver has not read
}

func (c *dohConn) Write(b []byte) (int, error) {
	c.pending.Write(b)
	for c.pending.Len() >= 2 {
		size := int(binary.BigEndian.Uint16(c.pending.Bytes()))
		if c.pending.Len() < 2+size {
			break
		}
		c.pending.Next(2)
		answer, err := c.exchange(bytes.Clone(c.pending.Next(size)))
		if err != nil {
			return 0, err
		}
		c.answers.Write(binary.BigEndian.AppendUint16(nil, uint16(len(answer)))) //nolint:gosec // exchange caps answers at 65535 bytes
		c.answers.Write(answer)
	}
	return len(b), nil
}

func (c *dohConn) exchange(query []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(c.ctx, http.MethodPost, c.url, bytes.NewReader(query))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			slog.LogAttrs(c.ctx, slog.LevelDebug, "Failed to close DoH response", slogErr(cerr))
		}
	}()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("DoH server answered %s", resp.Status)
	}
	answer, err := io.ReadAll(io.LimitReader(resp.Body, math.MaxUint16+1))
	if err != nil {
		return nil, err
	}
	if len(answer) > math.MaxUint16 {
		return nil, errors.New("DoH answer is larger than a DNS message can be")
	}
	return answer, nil
}

func (c *dohConn) Read(b []byte) (int, error) {
	if c.answers.Len() == 0 {
		return 0, io.EOF
	}
	return c.answers.Read(b)
}

func (c *dohConn) Close() error                     { return nil }
func (c *dohConn) LocalAddr() net.Addr              { return dohAddr(c.url) }
func (c *dohConn) RemoteAddr() net.Addr             { return dohAddr(c.url) }
func (c *dohConn) SetDeadline(time.Time) error      { return nil } // the request context carries the deadline
func (c *dohConn) SetReadDeadline(time.Time) error  { return nil }
func (c *dohConn) SetWriteDeadline(time.Time) error { return nil }

type dohAddr string

func (a dohAddr) Network() string { return "https" }
func (a dohAddr) String() string  { return string(a) }

// Close releases the connections that DoH and DoQ resolvers keep between
// lookups.
func (r *Resolver) Close() {
	if r.close != nil {
		r.close()
	}
}

// Precheck returns an error when no lookup against the resolver can
// succeed, however often it is retried.
//
// Connecting a UDP socket sends nothing, but it fails at once when there is
// no route, as with an IPv6 resolver on an IPv4-only host. For DoT and DoH,
// one TLS handshake also catches a certificate that does not match the
// server name.
// Any other handshake failure, such as a timeout, is left to the lookups
// and their retries.
func (r *Resolver) Precheck(ctx context.Context) error {
	conn, err := r.dialer.DialContext(ctx, "udp", r.hostPort)
	if err != nil {
		return fmt.Errorf("no route to resolver %s: %w", r.serverAddr, err)
	}
	if err := conn.Close(); err != nil {
		return fmt.Errorf("no route to resolver %s: %w", r.serverAddr, err)
	}

	if r.doq != nil {
		return r.doq.precheck(ctx)
	}
	if r.tlsDialer == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, precheckTLSTimeout)
	defer cancel()
	tlsConn, err := r.tlsDialer.DialContext(ctx, "tcp", r.hostPort)
	var certErr *tls.CertificateVerificationError
	if errors.As(err, &certErr) {
		return fmt.Errorf("TLS certificate of resolver %s is not valid: %w", r.serverAddr, err)
	}
	if err == nil {
		// The handshake worked. A failed close changes nothing for the lookups.
		if cerr := tlsConn.Close(); cerr != nil {
			slog.LogAttrs(ctx, slog.LevelDebug, "Failed to close precheck connection", slogErr(cerr))
		}
	}
	return nil
}

const precheckTLSTimeout = 5 * time.Second

func (r *Resolver) QueryDNS(ctx context.Context, domain string, timeout time.Duration, retry ResolverRetry) (time.Duration, error) {
	if domain == "" {
		return 0, errors.New("empty domain name")
	}

	log := slog.With(
		slog.String("domain", domain),
		slog.String("resolver", r.serverAddr),
	)

	// Acquire semaphore for concurrency control
	if r.sem != nil && r.concurrency > 0 {
		r.sem <- struct{}{}
		defer func() { <-r.sem }()
	}

	try := func(attempt int) (time.Duration, error) {
		log := log.With(slog.Int("attempt", attempt))

		if attempt > 0 {
			log.LogAttrs(ctx, slog.LevelDebug, "Attempting query again")
		}

		attemptCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		start := time.Now()
		addrs, err := r.netResolver.LookupHost(attemptCtx, domain)
		took := time.Since(start)

		if err != nil {
			log.LogAttrs(ctx, slog.LevelDebug, "Failed query", slogErr(err))
			// The resolver answered that the name does not exist. Asking
			// again gets the same answer after the backoff.
			var dnsErr *net.DNSError
			if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
				return took, &finalError{err: err}
			}
			return took, err
		}

		if took > timeout {
			log.LogAttrs(ctx, slog.LevelDebug, "Query exceeded timeout", slog.Int64("took_ms", took.Milliseconds()))
			return took, context.DeadlineExceeded
		}

		if len(addrs) == 0 {
			log.LogAttrs(ctx, slog.LevelDebug, "No addresses found")
			return took, fmt.Errorf("no addresses found for domain %s by resolver %s", domain, r.serverAddr)
		}

		if took > 200*time.Millisecond {
			log.LogAttrs(ctx, slog.LevelDebug, "Slow query", slog.Int64("took_ms", took.Milliseconds()))
		}

		return took, nil
	}

	retries := 10
	if !retry {
		retries = 1
	}

	elapsed, err := retryWithBackoff(ctx, try, retries, 2*time.Second, 60*time.Second) // Delay from 2 to 60 seconds, max 10 tries
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return 0, fmt.Errorf("DNS query timeout for %s via %s: %w", domain, r.serverAddr, err)
		}
		return 0, fmt.Errorf("DNS query failed for %s via %s: %w", domain, r.serverAddr, err)
	}

	return elapsed, nil
}
