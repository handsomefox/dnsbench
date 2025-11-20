package main

import (
	"context"
	"errors"
	"strings"
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
