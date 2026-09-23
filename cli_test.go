package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/handsomefox/dnsbench/internal/catalog"
)

// -list prints what a run would use: here, the major providers' plain
// IPv4 addresses, one line each after the header.
func TestListServers(t *testing.T) {
	config := &Config{OnlyMajorResolvers: true, Family: catalog.FamilyIPv4, Transport: catalog.TransportPlain}
	var out bytes.Buffer
	if err := listServers(&out, config); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	want := len(catalog.Servers(config.filter()))
	if len(lines) != want+1 {
		t.Fatalf("got %d lines, want a header and %d resolvers:\n%s", len(lines), want, out.String())
	}
	if !strings.HasPrefix(lines[1], "Cloudflare-1 ") || !strings.Contains(lines[1], "1.1.1.1") {
		t.Errorf("first resolver line = %q, want Cloudflare-1 at 1.1.1.1", lines[1])
	}
}
