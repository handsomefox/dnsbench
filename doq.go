package main

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"

	"github.com/quic-go/quic-go"
)

// newDoQResolver sends every query over DNS over QUIC, as RFC 9250
// describes: UDP port 853, ALPN "doq", one stream per query. It keeps one
// QUIC connection per resolver and opens a stream on it for each query, so
// only the first lookups pay for the QUIC handshake, as with DoH.
func newDoQResolver(serverAddr, hostPort string, tlsConfig *tls.Config, concurrency int) *Resolver {
	client := &doqClient{hostPort: hostPort, tlsConfig: tlsConfig}
	return newResolverWith(client, &net.Dialer{}, serverAddr, hostPort, concurrency)
}

// doqClient holds the QUIC connection that a DoQ resolver's lookups share.
type doqClient struct {
	hostPort  string
	tlsConfig *tls.Config

	mu   sync.Mutex
	conn *quic.Conn
}

// connection returns the shared QUIC connection, dialing a new one when
// there is none or the last one has closed.
func (c *doqClient) connection(ctx context.Context) (*quic.Conn, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil && c.conn.Context().Err() == nil {
		return c.conn, nil
	}
	conn, err := quic.DialAddr(ctx, c.hostPort, c.tlsConfig, &quic.Config{})
	if err != nil {
		return nil, err
	}
	c.conn = conn
	return conn, nil
}

// exchange sends one DNS message on a new stream and returns the answer.
// RFC 9250 requires message ID 0 on the wire, so exchange sends 0 and puts
// the caller's ID back into the answer, which checkAnswer checks.
func (c *doqClient) exchange(ctx context.Context, query []byte) ([]byte, error) {
	if len(query) < 12 {
		return nil, errors.New("DNS query is too short")
	}
	conn, err := c.connection(ctx)
	if err != nil {
		return nil, err
	}
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := stream.SetDeadline(deadline); err != nil {
			return nil, err
		}
	}

	id := binary.BigEndian.Uint16(query)
	msg := binary.BigEndian.AppendUint16(nil, uint16(len(query))) //nolint:gosec // buildQuery never makes a query over 64 KiB
	msg = binary.BigEndian.AppendUint16(msg, 0)
	msg = append(msg, query[2:]...)
	if _, err := stream.Write(msg); err != nil {
		return nil, err
	}
	// Closing the send side tells the server that the query is complete.
	if err := stream.Close(); err != nil {
		return nil, err
	}

	var size [2]byte
	if _, err := io.ReadFull(stream, size[:]); err != nil {
		return nil, err
	}
	answer := make([]byte, binary.BigEndian.Uint16(size[:]))
	if _, err := io.ReadFull(stream, answer); err != nil {
		return nil, err
	}
	if len(answer) < 12 {
		return nil, errors.New("DoQ answer is too short")
	}
	binary.BigEndian.PutUint16(answer, id)
	return answer, nil
}

// precheck makes one QUIC handshake and returns an error only when no
// retry can succeed: the server's certificate does not match, or the
// server aborts the TLS handshake with an alert, as AdGuard does for a
// server name it does not serve. It closes the connection, so the first
// lookups still pay for their handshake.
func (c *doqClient) precheck(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, precheckTLSTimeout)
	defer cancel()
	conn, err := quic.DialAddr(ctx, c.hostPort, c.tlsConfig, &quic.Config{})
	var certErr *tls.CertificateVerificationError
	if errors.As(err, &certErr) {
		return fmt.Errorf("TLS certificate of resolver %s is not valid: %w", c.hostPort, err)
	}
	var transportErr *quic.TransportError
	if errors.As(err, &transportErr) && transportErr.Remote && transportErr.ErrorCode.IsCryptoError() {
		return fmt.Errorf("resolver %s refused the TLS handshake for %s: %w", c.hostPort, c.tlsConfig.ServerName, err)
	}
	if err == nil {
		if cerr := conn.CloseWithError(0, ""); cerr != nil {
			slog.LogAttrs(ctx, slog.LevelDebug, "Failed to close precheck connection", slogErr(cerr))
		}
	}
	return nil
}

func (c *doqClient) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return
	}
	if err := c.conn.CloseWithError(0, ""); err != nil {
		slog.Debug("Failed to close DoQ connection", slogErr(err))
	}
	c.conn = nil
}
