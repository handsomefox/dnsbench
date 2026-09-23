package main

import (
	"bytes"
	"flag"
	"slices"
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

// A phone keyboard can turn "-ui" into "–ui", which the flag parser takes
// for a plain argument. The error must say so and name the flag.
// defineFlag registers name on the default flag set once, for tests that
// run without parseFlags.
func defineFlag(name string) {
	if flag.Lookup(name) == nil {
		flag.Bool(name, false, "")
	}
}

func TestUnexpectedArgument(t *testing.T) {
	defineFlag("ui")
	for arg, want := range map[string][]string{
		"ui":      {`"ui"`, "Did you mean -ui"},
		"–ui":     {"looks like a hyphen", "Did you mean -ui"},
		"——ui":    {"looks like a hyphen", "Did you mean -ui"},
		"nothing": {"Flags start with a hyphen"},
	} {
		got := unexpectedArgument(arg)
		for _, w := range want {
			if !strings.Contains(got, w) {
				t.Errorf("unexpectedArgument(%q) = %q, want it to contain %q", arg, got, w)
			}
		}
	}
}

func TestFixDashes(t *testing.T) {
	defineFlag("ui")
	defineFlag("n")
	defineFlag("major")
	got := fixDashes([]string{"–ui", "——n=5", "-major", "–notaflag", "file–name"})
	want := []string{"-ui", "-n=5", "-major", "–notaflag", "file–name"}
	if !slices.Equal(got, want) {
		t.Errorf("fixDashes() = %q, want %q", got, want)
	}
}
