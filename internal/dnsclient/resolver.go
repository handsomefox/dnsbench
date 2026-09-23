package dnsclient

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// Waits between the attempts of one lookup. They stay short: a benchmark
// wants to see a resolver that drops queries, not to wait it out.
const (
	retryBackoff    = 250 * time.Millisecond
	retryBackoffMax = time.Second
)

// Lookup is the outcome of one lookup: the latency of the attempt that got
// the answer, and how many attempts the lookup made.
type Lookup struct {
	Latency  time.Duration
	Attempts int
	// NoAddress is set when the resolver answered that the name does not
	// exist or has no A record. Query returns the answer's error with it,
	// and Latency holds the time that answer took. Filtering resolvers
	// answer blocked names this way.
	NoAddress bool
}

// ednsUDPSize is the UDP payload size that queries advertise. 1232 bytes
// fits in one packet on any path, as DNS Flag Day 2020 recommends.
const ednsUDPSize = 1232

// exchanger sends one DNS message to a resolver over one transport and
// returns the answer. Every transport measures the same span: from the
// call, including any dial or handshake it needs, to the full answer.
type exchanger interface {
	exchange(ctx context.Context, query []byte) ([]byte, error)
	// precheck returns an error only when no lookup can succeed, such as
	// a certificate that does not match.
	precheck(ctx context.Context) error
	// close releases any connections the transport keeps.
	close()
}

// Resolver sends A queries for hostnames to one resolver address. It builds
// the DNS messages itself, so the host's /etc/hosts, search domains, and
// resolv.conf options never touch a lookup.
type Resolver struct {
	transport  exchanger
	dialer     *net.Dialer
	hostPort   string
	serverAddr string
	sem        chan struct{}
}

// Option changes how New reaches a server. Tests use them to point a
// resolver at a local fake server.
type Option func(*options)

type options struct {
	hostPort string
	roots    *x509.CertPool
}

// WithHostPort sends the queries to hostPort instead of the server's
// address on its transport's standard port.
func WithHostPort(hostPort string) Option {
	return func(o *options) { o.hostPort = hostPort }
}

// WithRootCAs verifies TLS certificates against roots instead of the
// system's pool.
func WithRootCAs(roots *x509.CertPool) Option {
	return func(o *options) { o.roots = roots }
}

// New queries server with plain DNS on port 53, DNS over TLS on port 853,
// DNS over HTTPS on port 443, or DNS over QUIC on UDP port 853, depending
// on which of the server's fields is set. At most concurrency queries are
// in flight at once.
func New(server Server, concurrency int, opts ...Option) *Resolver {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	hostPort := func(port string) string {
		if o.hostPort != "" {
			return o.hostPort
		}
		return net.JoinHostPort(server.Addr, port)
	}
	switch {
	case server.DoQName != "":
		tlsConfig := &tls.Config{ServerName: server.DoQName, RootCAs: o.roots, MinVersion: tls.VersionTLS13, NextProtos: []string{"doq"}}
		return newDoQResolver(server.Addr, hostPort("853"), tlsConfig, concurrency)
	case server.DoHURL != "":
		// Validate has checked the URL, so the error cannot happen.
		u, _ := url.Parse(server.DoHURL) //nolint:errcheck // checked by Validate
		tlsConfig := &tls.Config{ServerName: u.Hostname(), RootCAs: o.roots, MinVersion: tls.VersionTLS12}
		return newDoHResolver(server.Addr, server.DoHURL, hostPort("443"), tlsConfig, concurrency)
	case server.TLSName != "":
		tlsConfig := &tls.Config{ServerName: server.TLSName, RootCAs: o.roots, MinVersion: tls.VersionTLS12}
		return newResolver(server.Addr, hostPort("853"), tlsConfig, concurrency)
	default:
		return newResolver(server.Addr, hostPort("53"), nil, concurrency)
	}
}

func newResolverWith(t exchanger, dialer *net.Dialer, serverAddr, hostPort string, concurrency int) *Resolver {
	return &Resolver{
		transport:  t,
		dialer:     dialer,
		hostPort:   hostPort,
		serverAddr: serverAddr,
		sem:        make(chan struct{}, max(concurrency, 1)),
	}
}

// newResolver sends every query to hostPort: over UDP with a TCP retry for
// a truncated answer, or over DNS over TLS when tlsConfig is set.
func newResolver(serverAddr, hostPort string, tlsConfig *tls.Config, concurrency int) *Resolver {
	dialer := &net.Dialer{}
	if tlsConfig == nil {
		return newResolverWith(&plainTransport{dialer: dialer, hostPort: hostPort}, dialer, serverAddr, hostPort, concurrency)
	}
	t := &dotTransport{
		dialer:   &tls.Dialer{NetDialer: dialer, Config: tlsConfig},
		hostPort: hostPort,
		idle:     make(chan net.Conn, max(concurrency, 1)),
	}
	return newResolverWith(t, dialer, serverAddr, hostPort, concurrency)
}

// newDoHResolver sends every query as an HTTPS POST to dohURL, as RFC 8484
// describes. It connects to hostPort, not to the address the URL's host
// resolves to, so the benchmark measures the address under test. The HTTP
// client keeps its connections, so only the first lookups pay for the
// TCP and TLS handshakes.
func newDoHResolver(serverAddr, dohURL, hostPort string, tlsConfig *tls.Config, concurrency int) *Resolver {
	dialer := &net.Dialer{}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, hostPort)
		},
		TLSClientConfig:     tlsConfig,
		ForceAttemptHTTP2:   true,
		MaxIdleConnsPerHost: max(concurrency, 1),
	}
	t := &dohTransport{
		client:    &http.Client{Transport: transport},
		transport: transport,
		url:       dohURL,
		tlsDialer: &tls.Dialer{NetDialer: dialer, Config: tlsConfig},
		hostPort:  hostPort,
	}
	return newResolverWith(t, dialer, serverAddr, hostPort, concurrency)
}

// Close releases the connections that the resolver keeps between lookups.
func (r *Resolver) Close() {
	r.transport.close()
}

// Precheck returns an error when no lookup against the resolver can
// succeed, however often it is retried.
//
// Connecting a UDP socket sends nothing, but it fails at once when there is
// no route, as with an IPv6 resolver on an IPv4-only host. For DoT, DoH,
// and DoQ, one TLS handshake also catches a certificate that does not match
// the server name. Any other handshake failure, such as a timeout, is left
// to the lookups and their retries.
func (r *Resolver) Precheck(ctx context.Context) error {
	conn, err := r.dialer.DialContext(ctx, "udp", r.hostPort)
	if err != nil {
		return fmt.Errorf("no route to resolver %s: %w", r.serverAddr, err)
	}
	if err := conn.Close(); err != nil {
		return fmt.Errorf("no route to resolver %s: %w", r.serverAddr, err)
	}
	return r.transport.precheck(ctx)
}

const precheckTLSTimeout = 5 * time.Second

// precheckTLS makes one TLS handshake and fails only for a certificate
// that does not verify.
func precheckTLS(ctx context.Context, dialer *tls.Dialer, hostPort string) error {
	ctx, cancel := context.WithTimeout(ctx, precheckTLSTimeout)
	defer cancel()
	conn, err := dialer.DialContext(ctx, "tcp", hostPort)
	var certErr *tls.CertificateVerificationError
	if errors.As(err, &certErr) {
		return fmt.Errorf("TLS certificate of resolver %s is not valid: %w", hostPort, err)
	}
	if err == nil {
		// The handshake worked. A failed close changes nothing for the lookups.
		if cerr := conn.Close(); cerr != nil {
			slog.LogAttrs(ctx, slog.LevelDebug, "Failed to close precheck connection", slog.Any("err", cerr))
		}
	}
	return nil
}

// Query looks up the A records of domain. Each attempt sends one query
// and is bounded by timeout. A failed attempt is retried up to retries
// times, after a short wait, unless the answer was final, such as NXDOMAIN.
// The returned Lookup counts the attempts made, even when err is not nil.
func (r *Resolver) Query(ctx context.Context, domain string, timeout time.Duration, retries int) (Lookup, error) {
	if domain == "" {
		return Lookup{}, errors.New("empty domain name")
	}
	// An absolute name, so nothing appends a search domain to it.
	name, err := dnsmessage.NewName(strings.TrimSuffix(domain, ".") + ".")
	if err != nil {
		return Lookup{}, fmt.Errorf("invalid domain name %q: %w", domain, err)
	}

	log := slog.With(
		slog.String("domain", domain),
		slog.String("resolver", r.serverAddr),
	)

	r.sem <- struct{}{}
	defer func() { <-r.sem }()

	try := func(attempt int) (time.Duration, error) {
		log := log.With(slog.Int("attempt", attempt))
		if attempt > 0 {
			log.LogAttrs(ctx, slog.LevelDebug, "Attempting query again")
		}

		attemptCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		id := uint16(rand.N(math.MaxUint16 + 1)) //nolint:gosec // a message ID, not a secret
		query, err := buildQuery(id, &name)
		if err != nil {
			return 0, &finalError{err: err}
		}

		start := time.Now()
		answer, err := r.transport.exchange(attemptCtx, query)
		took := time.Since(start)
		if err == nil {
			err = checkAnswer(answer, id, &name)
		}
		if err != nil {
			log.LogAttrs(ctx, slog.LevelDebug, "Failed query", slog.Any("err", err))
			if attemptCtx.Err() != nil && ctx.Err() == nil {
				return took, fmt.Errorf("%w: %w", context.DeadlineExceeded, err)
			}
			return took, err
		}
		if took > timeout {
			log.LogAttrs(ctx, slog.LevelDebug, "Query exceeded timeout", slog.Int64("took_ms", took.Milliseconds()))
			return took, context.DeadlineExceeded
		}
		return took, nil
	}

	elapsed, attempts, err := retryWithBackoff(ctx, try, 1+max(retries, 0), retryBackoff, retryBackoffMax)
	if err != nil {
		if IsFinalAnswer(err) {
			return Lookup{Latency: elapsed, Attempts: attempts, NoAddress: true}, fmt.Errorf("DNS query for %s via %s: %w", domain, r.serverAddr, err)
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return Lookup{Attempts: attempts}, fmt.Errorf("DNS query timeout for %s via %s: %w", domain, r.serverAddr, err)
		}
		return Lookup{Attempts: attempts}, fmt.Errorf("DNS query failed for %s via %s: %w", domain, r.serverAddr, err)
	}
	return Lookup{Latency: elapsed, Attempts: attempts}, nil
}

// buildQuery returns a recursive A query for name with an EDNS(0) record
// that advertises ednsUDPSize.
func buildQuery(id uint16, name *dnsmessage.Name) ([]byte, error) {
	b := dnsmessage.NewBuilder(make([]byte, 0, 64), dnsmessage.Header{ID: id, RecursionDesired: true})
	b.EnableCompression()
	if err := b.StartQuestions(); err != nil {
		return nil, err
	}
	if err := b.Question(dnsmessage.Question{Name: *name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}); err != nil {
		return nil, err
	}
	if err := b.StartAdditionals(); err != nil {
		return nil, err
	}
	var opt dnsmessage.ResourceHeader
	if err := opt.SetEDNS0(ednsUDPSize, dnsmessage.RCodeSuccess, false); err != nil {
		return nil, err
	}
	if err := b.OPTResource(opt, dnsmessage.OPTResource{}); err != nil {
		return nil, err
	}
	return b.Finish()
}

// errNoSuchHost is the error for an NXDOMAIN answer. The dashboard groups
// errors by their text, so keep "no such host" in it.
var errNoSuchHost = errors.New("no such host")

// errNoARecord is the error for an answer without any A record.
var errNoARecord = errors.New("no A record in the answer")

// IsFinalAnswer reports whether err is an answer about the name, such as
// NXDOMAIN, rather than a failure of the resolver.
func IsFinalAnswer(err error) bool {
	return errors.Is(err, errNoSuchHost) || errors.Is(err, errNoARecord)
}

// checkAnswer returns nil when answer answers the query with id for name
// with at least one A record. An answer that the name does not exist, or
// that it has no A record, is a finalError: asking again gets the same
// answer.
func checkAnswer(answer []byte, id uint16, name *dnsmessage.Name) error {
	var p dnsmessage.Parser
	h, err := p.Start(answer)
	if err != nil {
		return fmt.Errorf("malformed answer: %w", err)
	}
	if !h.Response || h.ID != id {
		return errors.New("answer does not match the query")
	}
	q, err := p.Question()
	if err != nil {
		return fmt.Errorf("malformed answer: %w", err)
	}
	if !strings.EqualFold(q.Name.String(), name.String()) || q.Type != dnsmessage.TypeA {
		return errors.New("answer is for another question")
	}
	switch h.RCode {
	case dnsmessage.RCodeSuccess:
	case dnsmessage.RCodeNameError:
		return &finalError{err: fmt.Errorf("%w: resolver answered NXDOMAIN", errNoSuchHost)}
	default:
		return fmt.Errorf("resolver answered %s", strings.TrimPrefix(h.RCode.String(), "RCode"))
	}
	if err := p.SkipAllQuestions(); err != nil {
		return fmt.Errorf("malformed answer: %w", err)
	}
	for {
		ah, err := p.AnswerHeader()
		if errors.Is(err, dnsmessage.ErrSectionDone) {
			return &finalError{err: errNoARecord}
		}
		if err != nil {
			return fmt.Errorf("malformed answer: %w", err)
		}
		if ah.Type == dnsmessage.TypeA {
			return nil
		}
		if err := p.SkipAnswer(); err != nil {
			return fmt.Errorf("malformed answer: %w", err)
		}
	}
}

// truncated reports whether answer has the TC bit set.
func truncated(answer []byte) bool {
	return len(answer) >= 3 && answer[2]&0x02 != 0
}

// withContext makes blocking reads and writes on conn end when ctx does:
// at its deadline, or at once when it is canceled.
func withContext(ctx context.Context, conn net.Conn) (stop func() bool) {
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			slog.Debug("Failed to set a connection deadline", slog.Any("err", err))
		}
	}
	return context.AfterFunc(ctx, func() {
		if err := conn.SetDeadline(time.Now()); err != nil {
			slog.Debug("Failed to cut a connection short", slog.Any("err", err))
		}
	})
}

// plainTransport sends queries over UDP, and over TCP when the UDP answer
// arrives truncated.
type plainTransport struct {
	dialer   *net.Dialer
	hostPort string
}

func (t *plainTransport) exchange(ctx context.Context, query []byte) ([]byte, error) {
	answer, err := t.exchangeUDP(ctx, query)
	if err != nil || !truncated(answer) {
		return answer, err
	}
	conn, err := t.dialer.DialContext(ctx, "tcp", t.hostPort)
	if err != nil {
		return nil, err
	}
	defer closeConn(conn)
	defer withContext(ctx, conn)()
	return streamExchange(conn, query)
}

func (t *plainTransport) exchangeUDP(ctx context.Context, query []byte) ([]byte, error) {
	conn, err := t.dialer.DialContext(ctx, "udp", t.hostPort)
	if err != nil {
		return nil, err
	}
	defer closeConn(conn)
	defer withContext(ctx, conn)()

	if _, err := conn.Write(query); err != nil {
		return nil, err
	}
	buf := make([]byte, math.MaxUint16)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return nil, err
		}
		// Skip a stray datagram, such as a late answer to another query.
		if n >= 2 && binary.BigEndian.Uint16(buf) == binary.BigEndian.Uint16(query) {
			return buf[:n], nil
		}
	}
}

func (t *plainTransport) precheck(context.Context) error { return nil }
func (t *plainTransport) close()                         {}

// dotTransport sends queries over DNS over TLS, RFC 7858. It keeps idle
// connections and reuses them, one query at a time, as a long-running DoT
// client does. A reused connection that the server has closed in the
// meantime gets one fresh connection within the same exchange.
type dotTransport struct {
	dialer   *tls.Dialer
	hostPort string
	idle     chan net.Conn
}

func (t *dotTransport) exchange(ctx context.Context, query []byte) ([]byte, error) {
	select {
	case conn := <-t.idle:
		answer, err := t.exchangeOn(ctx, conn, query)
		if err == nil || ctx.Err() != nil {
			return answer, err
		}
		// The server most likely closed the idle connection. Try a fresh one.
	default:
	}
	conn, err := t.dialer.DialContext(ctx, "tcp", t.hostPort)
	if err != nil {
		return nil, err
	}
	return t.exchangeOn(ctx, conn, query)
}

// exchangeOn sends query on conn and keeps conn for the next query when the
// exchange worked. A failed connection may still hold part of an answer,
// so it is closed.
func (t *dotTransport) exchangeOn(ctx context.Context, conn net.Conn, query []byte) ([]byte, error) {
	stop := withContext(ctx, conn)
	answer, err := streamExchange(conn, query)
	if !stop() || err != nil {
		closeConn(conn)
		return answer, err
	}
	t.keep(conn)
	return answer, nil
}

// keep puts conn back for the next query, or closes it when the pool is
// full or its deadline cannot be cleared.
func (t *dotTransport) keep(conn net.Conn) {
	if conn.SetDeadline(time.Time{}) != nil {
		closeConn(conn)
		return
	}
	select {
	case t.idle <- conn:
	default:
		closeConn(conn)
	}
}

func (t *dotTransport) precheck(ctx context.Context) error {
	return precheckTLS(ctx, t.dialer, t.hostPort)
}

func (t *dotTransport) close() {
	for {
		select {
		case conn := <-t.idle:
			closeConn(conn)
		default:
			return
		}
	}
}

// streamExchange sends query with the two-byte length prefix that DNS over
// TCP and DoT use, and reads one answer framed the same way.
func streamExchange(conn net.Conn, query []byte) ([]byte, error) {
	msg := binary.BigEndian.AppendUint16(make([]byte, 0, 2+len(query)), uint16(len(query))) //nolint:gosec // queries are far below 64 KiB
	if _, err := conn.Write(append(msg, query...)); err != nil {
		return nil, err
	}
	var size [2]byte
	if _, err := io.ReadFull(conn, size[:]); err != nil {
		return nil, err
	}
	answer := make([]byte, binary.BigEndian.Uint16(size[:]))
	if _, err := io.ReadFull(conn, answer); err != nil {
		return nil, err
	}
	return answer, nil
}

func closeConn(c io.Closer) {
	if err := c.Close(); err != nil {
		slog.Debug("Failed to close a connection", slog.Any("err", err))
	}
}

// dohTransport posts each query to the DoH URL with the
// application/dns-message body that RFC 8484 describes.
type dohTransport struct {
	client    *http.Client
	transport *http.Transport
	url       string
	tlsDialer *tls.Dialer
	hostPort  string
}

func (t *dohTransport) exchange(ctx context.Context, query []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(query))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer closeConn(resp.Body)
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

func (t *dohTransport) precheck(ctx context.Context) error {
	return precheckTLS(ctx, t.tlsDialer, t.hostPort)
}

func (t *dohTransport) close() {
	t.transport.CloseIdleConnections()
}
