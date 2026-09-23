package main

import (
	"net/netip"
	"strings"
	"testing"
)

func TestBuiltinServers(t *testing.T) {
	var all4, all6, major4 int
	for _, p := range providers {
		all4 += len(p.ipv4)
		all6 += len(p.ipv6)
		if p.major {
			major4 += len(p.ipv4)
		}
	}

	tests := []struct {
		name      string
		onlyMajor bool
		family    AddrFamily
		want      int
	}{
		{name: "IPv4", family: FamilyIPv4, want: all4},
		{name: "IPv6", family: FamilyIPv6, want: all6},
		{name: "all", family: FamilyAll, want: all4 + all6},
		{name: "major IPv4", onlyMajor: true, family: FamilyIPv4, want: major4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := builtinServers(tt.onlyMajor, tt.family)
			if len(got) != tt.want {
				t.Fatalf("builtinServers() returned %d resolvers, want %d", len(got), tt.want)
			}
			names := make(map[string]bool)
			for _, s := range got {
				if names[s.Name] {
					t.Errorf("duplicate name %q", s.Name)
				}
				names[s.Name] = true

				addr, err := netip.ParseAddr(s.Addr)
				if err != nil {
					t.Fatalf("%s has invalid address %q: %v", s.Name, s.Addr, err)
				}
				isV6Name := strings.Contains(s.Name, "-v6-")
				if addr.Is6() != isV6Name {
					t.Errorf("%s has address %s: the -v6- name and the family disagree", s.Name, s.Addr)
				}
				if tt.family == FamilyIPv4 && addr.Is6() || tt.family == FamilyIPv6 && addr.Is4() {
					t.Errorf("%s (%s) does not belong to family %s", s.Name, s.Addr, tt.family)
				}
			}
		})
	}

	// Existing reports and scripts refer to these names.
	first := builtinServers(false, FamilyIPv4)[0]
	if first.Name != "Cloudflare-1" || first.Addr != "1.1.1.1" {
		t.Errorf("first IPv4 resolver = %+v, want Cloudflare-1 1.1.1.1", first)
	}
}

func TestParseFamily(t *testing.T) {
	for in, want := range map[string]AddrFamily{"ipv4": FamilyIPv4, "IPv6": FamilyIPv6, "all": FamilyAll} {
		got, err := parseFamily(in)
		if err != nil || got != want {
			t.Errorf("parseFamily(%q) = %v, %v, want %v", in, got, err, want)
		}
		if got.String() != strings.ToLower(in) {
			t.Errorf("AddrFamily(%d).String() = %q, want %q", got, got.String(), strings.ToLower(in))
		}
	}
	if _, err := parseFamily("6"); err == nil {
		t.Error(`parseFamily("6") returned no error`)
	}
}

func TestIsValidServerAddr(t *testing.T) {
	tests := map[string]bool{
		"1.1.1.1":                   true,
		"2606:4700:4700::1111":      true,
		"fe80::1%eth0":              true,
		"1.1.1.1%eth0":              false,
		"1.1.1.1:53":                false,
		"[2606:4700:4700::1111]:53": false,
		"dns.google":                false,
		"":                          false,
	}
	for addr, want := range tests {
		if got := isValidServerAddr(addr); got != want {
			t.Errorf("isValidServerAddr(%q) = %v, want %v", addr, got, want)
		}
	}
}
