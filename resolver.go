package main

import (
	"context"
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
	hostPort    string
	serverAddr  string
	concurrency int
	sem         chan struct{}
}

func NewResolver(serverAddr string, concurrency int) *Resolver {
	return newResolver(serverAddr, net.JoinHostPort(serverAddr, "53"), concurrency)
}

// newResolver sends every query to hostPort. Go's resolver asks for "udp"
// first and for "tcp" when the UDP answer comes back truncated, so Dial
// keeps the network it is given.
func newResolver(serverAddr, hostPort string, concurrency int) *Resolver {
	dialer := &net.Dialer{}
	if concurrency < 1 {
		concurrency = 1
	}
	return &Resolver{
		netResolver: &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return dialer.DialContext(ctx, network, hostPort)
			},
		},
		dialer:      dialer,
		hostPort:    hostPort,
		serverAddr:  serverAddr,
		concurrency: concurrency,
		sem:         make(chan struct{}, concurrency),
	}
}

// CheckRoute reports whether this host can reach the resolver at all.
// Connecting a UDP socket sends nothing, but it fails at once when there is
// no route, as with an IPv6 resolver on an IPv4-only host.
func (r *Resolver) CheckRoute(ctx context.Context) error {
	conn, err := r.dialer.DialContext(ctx, "udp", r.hostPort)
	if err != nil {
		return err
	}
	return conn.Close()
}

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
