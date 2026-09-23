package main

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
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
			r := NewResolver(tt.serverAddr, 1)

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

	go func() {
		for {
			conn, err := tcp.Accept()
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
					f.tcpQueries.Add(1)
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
	}()

	return f
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

	flags := uint16(0x8180) // QR, RD, RA
	var answers uint16
	switch {
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

func TestResolver_RetriesTruncatedAnswersOverTCP(t *testing.T) {
	f := startFakeDNS(t)
	r := newResolver("127.0.0.1", f.hostPort, 1)

	if _, err := r.QueryDNS(t.Context(), "truncated.example", 2*time.Second, ResolverRetryDisabled); err != nil {
		t.Fatalf("QueryDNS() error = %v", err)
	}
	if f.tcpQueries.Load() == 0 {
		t.Fatal("QueryDNS() never retried over TCP")
	}
}
