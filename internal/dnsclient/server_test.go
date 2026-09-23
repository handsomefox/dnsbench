package dnsclient

import (
	"strings"
	"testing"
)

func TestIsValidAddr(t *testing.T) {
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
		if got := IsValidAddr(addr); got != want {
			t.Errorf("IsValidAddr(%q) = %v, want %v", addr, got, want)
		}
	}
}

func TestIsValidDomain(t *testing.T) {
	tests := map[string]bool{
		"example.com":                         true,
		"a.b.c.example.co.uk":                 true,
		"_dmarc.example.com":                  true,
		"xn--bcher-kva.example":               true,
		"example":                             false,
		".example.com":                        false,
		"example.com.":                        false,
		"exa mple.com":                        false,
		"-bad.example.com":                    false,
		"bad-.example.com":                    false,
		"http://cloudflare-dns.com/dns-query": false,
		"dns.google:853":                      false,
		strings.Repeat("a", 64) + ".com":      false,
		"":                                    false,
	}
	for domain, want := range tests {
		if got := IsValidDomain(domain); got != want {
			t.Errorf("IsValidDomain(%q) = %v, want %v", domain, got, want)
		}
	}
}
