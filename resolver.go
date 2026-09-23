package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
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
	hostPort    string
	serverAddr  string
	concurrency int
	sem         chan struct{}
}

// NewResolver queries server with plain DNS on port 53, or with DNS over
// TLS on port 853 when server.TLSName is set.
func NewResolver(server DNSServer, concurrency int) *Resolver {
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

// Precheck returns an error when no lookup against the resolver can
// succeed, however often it is retried.
//
// Connecting a UDP socket sends nothing, but it fails at once when there is
// no route, as with an IPv6 resolver on an IPv4-only host. For DoT, one TLS
// handshake also catches a certificate that does not match the TLS name.
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
