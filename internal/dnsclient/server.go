// Package dnsclient sends DNS lookups to one resolver over plain DNS, DNS
// over TLS, DNS over HTTPS, or DNS over QUIC, and times them.
package dnsclient

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"
)

// Server is a resolver to benchmark. A resolver with a TLSName is queried
// with DNS over TLS on port 853, and TLSName is the name its certificate
// must match. A resolver with a DoHURL is queried with DNS over HTTPS at
// that URL, through Addr on port 443. A resolver with a DoQName is queried
// with DNS over QUIC on UDP port 853, and DoQName is the name its
// certificate must match. Without any of them it gets plain DNS on port 53.
// At most one of TLSName, DoHURL, and DoQName is set.
type Server struct {
	Name    string `json:"name"`
	Addr    string `json:"addr"`
	TLSName string `json:"tlsName,omitempty"`
	DoHURL  string `json:"dohURL,omitempty"`
	DoQName string `json:"doqName,omitempty"`
}

// Transport names how the server is queried: plain, dot, doh, or doq.
func (s *Server) Transport() string {
	switch {
	case s.DoQName != "":
		return "doq"
	case s.DoHURL != "":
		return "doh"
	case s.TLSName != "":
		return "dot"
	default:
		return "plain"
	}
}

// Validate returns an error when the server cannot be queried as written:
// an address that is not an IP literal, a malformed TLS name, DoH URL, or
// DoQ name, or more than one of them.
func (s *Server) Validate() error {
	if !IsValidAddr(s.Addr) {
		return fmt.Errorf("invalid resolver address %q: want an IP address without a port", s.Addr)
	}
	if s.TLSName != "" && !IsValidDomain(s.TLSName) {
		return fmt.Errorf("invalid TLS name %q for resolver %s", s.TLSName, s.Addr)
	}
	if s.DoHURL != "" && !IsValidDoHURL(s.DoHURL) {
		return fmt.Errorf("invalid DoH URL %q for resolver %s: want an https URL", s.DoHURL, s.Addr)
	}
	if s.DoQName != "" && !IsValidDomain(s.DoQName) {
		return fmt.Errorf("invalid DoQ name %q for resolver %s", s.DoQName, s.Addr)
	}
	set := 0
	for _, f := range []string{s.TLSName, s.DoHURL, s.DoQName} {
		if f != "" {
			set++
		}
	}
	if set > 1 {
		return errors.New("resolver " + s.Addr + " sets more than one of a TLS name, a DoH URL, and a DoQ name: pick one")
	}
	return nil
}

// IsValidDoHURL reports whether s is an https URL with a host and no user
// info, which is what a DoH resolver needs.
func IsValidDoHURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil
}

// IsValidDomain reports whether domain is a hostname with at least two
// labels. Each label is 1 to 63 letters, digits, hyphens, or underscores,
// and does not start or end with a hyphen. Underscores appear in real
// names such as _dmarc.example.com.
func IsValidDomain(domain string) bool {
	if len(domain) > 253 {
		return false
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		if strings.ContainsFunc(label, func(c rune) bool { return !isLabelChar(c) }) {
			return false
		}
	}
	return true
}

func isLabelChar(c rune) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_'
}

// IsValidAddr reports whether addr is an IP literal without a port.
// Resolver files and the Web UI API both accept only such addresses. An
// IPv6 address may carry a zone, as in fe80::1%eth0, to reach a link-local
// resolver such as a home router.
func IsValidAddr(addr string) bool {
	_, err := netip.ParseAddr(addr)
	return err == nil
}
