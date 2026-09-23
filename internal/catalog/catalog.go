// Package catalog holds the built-in resolvers and domains, the filters
// that select from them, and the loaders for resolver and domain files.
package catalog

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/handsomefox/dnsbench/internal/dnsclient"
)

// Family selects built-in resolvers by address family.
type Family int

const (
	FamilyIPv4 Family = iota
	FamilyIPv6
	FamilyAll
)

func (f Family) String() string {
	switch f {
	case FamilyIPv6:
		return "ipv6"
	case FamilyAll:
		return "all"
	default:
		return "ipv4"
	}
}

func ParseFamily(s string) (Family, error) {
	switch strings.ToLower(s) {
	case "ipv4":
		return FamilyIPv4, nil
	case "ipv6":
		return FamilyIPv6, nil
	case "all":
		return FamilyAll, nil
	default:
		return FamilyIPv4, fmt.Errorf("invalid address family %q: want ipv4, ipv6, or all", s)
	}
}

// Transport selects built-in resolvers by how they are queried.
type Transport int

const (
	TransportPlain Transport = iota // DNS over UDP port 53, TCP on truncation
	TransportDoT                    // DNS over TLS on TCP port 853
	TransportDoH                    // DNS over HTTPS on TCP port 443
	TransportDoQ                    // DNS over QUIC on UDP port 853
	TransportAll
)

func (t Transport) String() string {
	switch t {
	case TransportDoT:
		return "dot"
	case TransportDoH:
		return "doh"
	case TransportDoQ:
		return "doq"
	case TransportAll:
		return "all"
	default:
		return "plain"
	}
}

func ParseTransport(s string) (Transport, error) {
	switch strings.ToLower(s) {
	case "plain":
		return TransportPlain, nil
	case "dot":
		return TransportDoT, nil
	case "doh":
		return TransportDoH, nil
	case "doq":
		return TransportDoQ, nil
	case "all":
		return TransportAll, nil
	default:
		return TransportPlain, fmt.Errorf("invalid transport %q: want plain, dot, doh, doq, or all", s)
	}
}

// LoadDomains reads a domain file, one domain per line. It skips blank
// lines, comments, and lines that are not a valid domain, with a warning.
// An empty path returns DefaultDomains.
func LoadDomains(sitesFile string) ([]string, error) {
	if sitesFile == "" {
		return DefaultDomains, nil
	}

	//nolint:gosec // file path provided by user intentionally
	file, err := os.Open(sitesFile)
	if err != nil {
		return nil, err
	}
	defer func() {
		if cerr := file.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "failed to close sites file: %v\n", cerr)
		}
	}()

	var domains []string
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Basic domain validation
		if !dnsclient.IsValidDomain(line) {
			slog.Warn("Skipping invalid domain",
				slog.Int("line", lineNum),
				slog.String("domain", line),
			)
			continue
		}

		domains = append(domains, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	if len(domains) == 0 {
		return nil, errors.New("no valid domains found in file")
	}

	return domains, nil
}

// Filter selects from the built-in resolvers.
type Filter struct {
	Major     bool // only the major services
	Primary   bool // only the first address in each list
	Family    Family
	Transport Transport
	Kind      string // a category such as Filtering, or empty for all
}

// Kinds are the resolver categories, in the order the dashboard shows them.
var Kinds = []string{"Global", "Filtering", "Privacy", "Regional"}

// ParseKind parses a -kind value: global, filtering, privacy, regional, or
// all, in any case. It returns the category name, or "" for all.
func ParseKind(s string) (string, error) {
	if strings.EqualFold(s, "all") {
		return "", nil
	}
	for _, k := range Kinds {
		if strings.EqualFold(s, k) {
			return k, nil
		}
	}
	return "", fmt.Errorf("invalid kind %q: want global, filtering, privacy, regional, or all", s)
}

// Entry is one built-in resolver with the facts that the filters
// and the dashboard select by. The JSON form goes to the dashboard.
type Entry struct {
	dnsclient.Server
	Service   string `json:"service"`  // such as Cloudflare-Family
	Provider  string `json:"provider"` // the company, such as Cloudflare
	Category  string `json:"category"`
	Major     bool   `json:"major"`
	Primary   bool   `json:"primary"`   // the first address in its list
	Family    string `json:"family"`    // ipv4 or ipv6
	Transport string `json:"transport"` // plain, dot, doh, or doq
}

// All lists every built-in resolver, in the order of the services table.
// For each service, plain DNS comes first, then DoT, DoH, and DoQ, and
// IPv4 comes before IPv6. The names follow the pattern in the service
// comment.
func All() []Entry {
	var entries []Entry
	for _, p := range services {
		for _, transport := range []Transport{TransportPlain, TransportDoT, TransportDoH, TransportDoQ} {
			suffix := ""
			server := dnsclient.Server{}
			switch transport {
			case TransportPlain:
				if p.encryptedOnly {
					continue
				}
			case TransportDoT:
				if p.tlsName == "" {
					continue
				}
				suffix, server.TLSName = "-DoT", p.tlsName
			case TransportDoH:
				if p.dohURL == "" {
					continue
				}
				suffix, server.DoHURL = "-DoH", p.dohURL
			case TransportDoQ:
				if p.doqName == "" {
					continue
				}
				suffix, server.DoQName = "-DoQ", p.doqName
			default:
				continue
			}
			for _, family := range []Family{FamilyIPv4, FamilyIPv6} {
				addrs, v6 := p.ipv4, ""
				if family == FamilyIPv6 {
					addrs, v6 = p.ipv6, "-v6"
				}
				for i, addr := range addrs {
					s := server
					s.Addr = addr
					s.Name = fmt.Sprintf("%s%s%s-%d", p.name, suffix, v6, i+1)
					entries = append(entries, Entry{
						Server:    s,
						Service:   p.name,
						Provider:  p.provider,
						Category:  p.category,
						Major:     p.major,
						Primary:   i == 0,
						Family:    family.String(),
						Transport: transport.String(),
					})
				}
			}
		}
	}
	return entries
}

// Matches reports whether e passes every part of the filter.
func (f Filter) Matches(e *Entry) bool {
	return (!f.Major || e.Major) &&
		(!f.Primary || e.Primary) &&
		(f.Family == FamilyAll || f.Family.String() == e.Family) &&
		(f.Transport == TransportAll || f.Transport.String() == e.Transport) &&
		(f.Kind == "" || f.Kind == e.Category)
}

// Servers lists the built-in resolvers that match f, in catalog order.
func Servers(f Filter) []dnsclient.Server {
	var servers []dnsclient.Server
	for _, e := range All() {
		if f.Matches(&e) {
			servers = append(servers, e.Server)
		}
	}
	return servers
}

// LoadServers loads DNS servers from a file or uses built-in resolvers.
// Format: name;ip, name;ip;tls-name, name;ip;https-url, or
// name;ip;quic://tls-name per line. Comments start with #. A third field
// that starts with https:// makes the resolver DNS over HTTPS at that URL.
// One that starts with quic:// makes it DNS over QUIC, with the rest as the
// name its certificate must match. Any other third field makes it DNS over
// TLS, with the field as that name.
// If resolversFile is empty, the built-in resolvers matching the filter are
// used. A file is used as written: the filter does not apply.
func LoadServers(resolversFile string, filter Filter) ([]dnsclient.Server, error) {
	if resolversFile == "" {
		return Servers(filter), nil
	}

	servers := make([]dnsclient.Server, 0)

	//nolint:gosec // file path provided by user intentionally
	file, err := os.Open(resolversFile)
	if err != nil {
		return nil, fmt.Errorf("opening resolvers file: %w", err)
	}
	defer func() {
		if cerr := file.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "failed to close resolvers file: %v\n", cerr)
		}
	}()

	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Split(line, ";")
		if len(parts) != 2 && len(parts) != 3 {
			return nil, fmt.Errorf("invalid format at line %d: expected 'name;ip', 'name;ip;tls-name', 'name;ip;https-url', or 'name;ip;quic://tls-name'", lineNum)
		}

		name := strings.TrimSpace(parts[0])
		addr := strings.TrimSpace(parts[1])

		if name == "" || addr == "" {
			return nil, fmt.Errorf("empty name or IP at line %d", lineNum)
		}

		if !dnsclient.IsValidAddr(addr) {
			return nil, fmt.Errorf("invalid IP address at line %d: %s", lineNum, addr)
		}

		server := dnsclient.Server{Name: name, Addr: addr}
		if len(parts) == 3 {
			third := strings.TrimSpace(parts[2])
			switch {
			case strings.HasPrefix(third, "https://"):
				if !dnsclient.IsValidDoHURL(third) {
					return nil, fmt.Errorf("invalid DoH URL at line %d: %q", lineNum, third)
				}
				server.DoHURL = third
			case strings.HasPrefix(third, "quic://"):
				server.DoQName = strings.TrimPrefix(third, "quic://")
				if !dnsclient.IsValidDomain(server.DoQName) {
					return nil, fmt.Errorf("invalid DoQ name at line %d: %q", lineNum, third)
				}
			case dnsclient.IsValidDomain(third):
				server.TLSName = third
			default:
				return nil, fmt.Errorf("invalid TLS name at line %d: %q", lineNum, third)
			}
		}

		servers = append(servers, server)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading resolvers file: %w", err)
	}

	if len(servers) == 0 {
		return nil, errors.New("no valid resolvers found in file")
	}

	return servers, nil
}
