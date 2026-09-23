package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// Config holds all CLI configuration
type Config struct {
	// What to test
	ResolversFile string
	SitesFile     string

	// Test setup
	LookupTimeout      time.Duration
	Repeats            int
	OnlyMajorResolvers bool
	PrimaryOnly        bool
	Family             AddrFamily
	Transport          Transport
	MaxConcurrency     int

	// Output and logging
	OutputType OutputType
	LogType    LogType

	WarmupRuns int

	// Web UI
	ServeUI    bool
	ListenAddr string
}

type OutputType int

const (
	OutputDefault OutputType = iota
	OutputCSV
	OutputTable
	OutputJSON
)

func (o OutputType) String() string {
	switch o {
	case OutputCSV:
		return "csv"
	case OutputTable:
		return "table"
	case OutputJSON:
		return "json"
	default:
		return "default"
	}
}

// AddrFamily selects built-in resolvers by address family.
type AddrFamily int

const (
	FamilyIPv4 AddrFamily = iota
	FamilyIPv6
	FamilyAll
)

func (f AddrFamily) String() string {
	switch f {
	case FamilyIPv6:
		return "ipv6"
	case FamilyAll:
		return "all"
	default:
		return "ipv4"
	}
}

func parseFamily(s string) (AddrFamily, error) {
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
	TransportAll
)

func (t Transport) String() string {
	switch t {
	case TransportDoT:
		return "dot"
	case TransportAll:
		return "all"
	default:
		return "plain"
	}
}

func parseTransport(s string) (Transport, error) {
	switch strings.ToLower(s) {
	case "plain":
		return TransportPlain, nil
	case "dot":
		return TransportDoT, nil
	case "all":
		return TransportAll, nil
	default:
		return TransportPlain, fmt.Errorf("invalid transport %q: want plain, dot, or all", s)
	}
}

type LogType int

const (
	LogDefault LogType = iota
	LogVerbose
	LogDisabled
)

func (l LogType) String() string {
	switch l {
	case LogVerbose:
		return "verbose"
	case LogDisabled:
		return "disabled"
	default:
		return "default"
	}
}

func run(ctx context.Context, config *Config) error {
	ctx, cancel := signal.NotifyContext(
		ctx,
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer cancel()

	// Load domain list
	domains, err := loadDomains(config.SitesFile)
	if err != nil {
		return fmt.Errorf("loading domains: %w", err)
	}

	slog.LogAttrs(ctx, slog.LevelInfo, "Loaded domains", slog.Int("count", len(domains)))

	// Load DNS servers
	servers, err := loadServers(config.ResolversFile, builtinFilter{
		onlyMajor:   config.OnlyMajorResolvers,
		primaryOnly: config.PrimaryOnly,
		family:      config.Family,
		transport:   config.Transport,
	})
	if err != nil {
		return fmt.Errorf("loading servers: %w", err)
	}

	slog.LogAttrs(ctx, slog.LevelInfo, "Loaded DNS servers", slog.Int("count", len(servers)))

	// Run benchmark
	results, err := runBenchmark(ctx, config, servers, domains, NoopReporter{})
	if err != nil {
		return fmt.Errorf("benchmark run failed: %w", err)
	}

	// Print summary
	printSummary(results, config.OutputType)

	return nil
}

func parseFlags() *Config {
	var config Config

	var (
		outputType string
		logType    string
		family     string
		transport  string
		warmupRuns int
		serveUI    bool
		listenAddr string
	)

	flag.StringVar(&config.ResolversFile, "f", "", "File of resolvers that replaces the built-in list, one name;ip or name;ip;tls-name (DNS over TLS) per line")
	flag.DurationVar(&config.LookupTimeout, "t", 3*time.Second, "Timeout for one lookup attempt (e.g. 1500ms, 2s)")
	flag.IntVar(&config.Repeats, "n", 10, "Number of times each domain is queried")
	flag.StringVar(&config.SitesFile, "s", "", "File of domains, one per line, that replaces the built-in list")
	flag.StringVar(&outputType, "output", "default", "Output format: default, csv, table, or json")
	flag.StringVar(&logType, "log", "default", "Logging level: default, verbose, or disabled")
	flag.IntVar(&config.MaxConcurrency, "c", max(runtime.NumCPU()/2, 2), "Maximum concurrent DNS queries")
	flag.BoolVar(&config.OnlyMajorResolvers, "major", false, "Benchmark only major DNS resolvers")
	flag.BoolVar(&config.PrimaryOnly, "primary", false, "Benchmark only the first address of each built-in provider, such as Cloudflare-1")
	flag.StringVar(&family, "family", "ipv4", "Address family of the built-in resolvers: ipv4, ipv6, or all")
	flag.StringVar(&transport, "proto", "plain", "Transport of the built-in resolvers: plain, dot (DNS over TLS), or all")
	flag.IntVar(&warmupRuns, "warmup", 0, "Warmup lookups to run before each measured lookup")
	flag.BoolVar(&serveUI, "ui", false, "Start the embedded Web UI dashboard server instead of running the CLI benchmark")
	flag.StringVar(&listenAddr, "listen", ":8080", "Address for the Web UI HTTP server (used with -ui)")

	flag.Usage = func() {
		//nolint:errcheck // best-effort help output
		_, _ = fmt.Fprintf(flag.CommandLine.Output(), `DNS Benchmark Tool

Test DNS resolvers against popular websites to measure latency and reliability.

Usage:
  dnsbench [options]

Options:
`)
		flag.PrintDefaults()
		//nolint:errcheck // best-effort help output
		_, _ = fmt.Fprintf(flag.CommandLine.Output(), `
Examples:
  # Default benchmark
  dnsbench

  # Test with more repeats and a longer timeout
  dnsbench -n 20 -t 5s

  # Use custom resolver list and increase concurrency
  dnsbench -f myresolvers.txt -c 10

  # Benchmark with custom domain list
  dnsbench -s mydomains.txt

  # Compare plain DNS with DNS over TLS on IPv4 and IPv6
  dnsbench -major -proto all -family all
`)
	}

	flag.Parse()

	// Validate configuration
	if config.Repeats < 1 {
		fmt.Fprintf(os.Stderr, "Error: repeats must be at least 1\n")
		os.Exit(1)
	}

	if config.MaxConcurrency < 1 {
		fmt.Fprintf(os.Stderr, "Error: concurrency must be at least 1\n")
		os.Exit(1)
	}

	if config.LookupTimeout < 100*time.Millisecond {
		fmt.Fprintf(os.Stderr, "Error: timeout must be at least 100ms\n")
		os.Exit(1)
	}

	// Parse output type
	switch strings.ToLower(outputType) {
	case "default":
		config.OutputType = OutputDefault
	case "csv":
		config.OutputType = OutputCSV
	case "table":
		config.OutputType = OutputTable
	case "json":
		config.OutputType = OutputJSON
	default:
		fmt.Fprintf(os.Stderr, "Error: invalid output type %q\n", outputType)
		os.Exit(1)
	}

	fam, err := parseFamily(family)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	config.Family = fam

	tr, err := parseTransport(transport)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	config.Transport = tr

	config.WarmupRuns = warmupRuns
	config.ServeUI = serveUI
	config.ListenAddr = listenAddr

	// Parse log type
	switch strings.ToLower(logType) {
	case "default":
		config.LogType = LogDefault
	case "verbose":
		config.LogType = LogVerbose
	case "disabled":
		config.LogType = LogDisabled
	default:
		fmt.Fprintf(os.Stderr, "Error: invalid log type %q\n", logType)
		os.Exit(1)
	}

	return &config
}

func loadDomains(sitesFile string) ([]string, error) {
	if sitesFile == "" {
		return defaultSites, nil
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
		if !isValidDomain(line) {
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

// builtinFilter selects from the built-in resolvers.
type builtinFilter struct {
	onlyMajor   bool
	primaryOnly bool // only the first address in each list
	family      AddrFamily
	transport   Transport
}

// builtinServers lists the built-in resolvers that match f, in the order
// of the providers table. For each provider, plain DNS comes before DoT and
// IPv4 before IPv6. The names follow the pattern in the provider comment.
func builtinServers(f builtinFilter) []DNSServer {
	var servers []DNSServer
	add := func(p provider, addrs []string, v6, dot bool) {
		if f.primaryOnly && len(addrs) > 1 {
			addrs = addrs[:1]
		}
		for i, addr := range addrs {
			name := p.name
			s := DNSServer{Addr: addr}
			if dot {
				name += "-DoT"
				s.TLSName = p.tlsName
			}
			if v6 {
				name += "-v6"
			}
			s.Name = fmt.Sprintf("%s-%d", name, i+1)
			servers = append(servers, s)
		}
	}
	for _, p := range providers {
		if f.onlyMajor && !p.major {
			continue
		}
		for _, dot := range []bool{false, true} {
			switch {
			case !dot && (p.dotOnly || f.transport == TransportDoT):
				continue
			case dot && (p.tlsName == "" || f.transport == TransportPlain):
				continue
			}
			if f.family != FamilyIPv6 {
				add(p, p.ipv4, false, dot)
			}
			if f.family != FamilyIPv4 {
				add(p, p.ipv6, true, dot)
			}
		}
	}
	return servers
}

// loadServers loads DNS servers from a file or uses built-in resolvers.
// Format: name;ip or name;ip;tls-name per line. Comments start with #.
// A third field makes the resolver DNS over TLS, with tls-name as the name
// its certificate must match.
// If resolversFile is empty, the built-in resolvers matching the filter are
// used. A file is used as written: the filter does not apply.
func loadServers(resolversFile string, filter builtinFilter) ([]DNSServer, error) {
	if resolversFile == "" {
		return builtinServers(filter), nil
	}

	servers := make([]DNSServer, 0)

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
			return nil, fmt.Errorf("invalid format at line %d: expected 'name;ip' or 'name;ip;tls-name'", lineNum)
		}

		name := strings.TrimSpace(parts[0])
		addr := strings.TrimSpace(parts[1])

		if name == "" || addr == "" {
			return nil, fmt.Errorf("empty name or IP at line %d", lineNum)
		}

		if !isValidServerAddr(addr) {
			return nil, fmt.Errorf("invalid IP address at line %d: %s", lineNum, addr)
		}

		server := DNSServer{Name: name, Addr: addr}
		if len(parts) == 3 {
			server.TLSName = strings.TrimSpace(parts[2])
			if !isValidDomain(server.TLSName) {
				return nil, fmt.Errorf("invalid TLS name at line %d: %q", lineNum, server.TLSName)
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
