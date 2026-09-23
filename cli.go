package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/handsomefox/dnsbench/internal/bench"
	"github.com/handsomefox/dnsbench/internal/catalog"
	"github.com/handsomefox/dnsbench/internal/report"
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
	Family             catalog.Family
	Transport          catalog.Transport
	MaxConcurrency     int
	Retries            int

	// Output and logging
	OutputType report.Format
	LogType    LogType

	WarmupRuns int

	// List prints the resolvers a run would use and exits.
	List bool

	// Web UI
	ServeUI    bool
	ListenAddr string
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

// errInterrupted ends a run that the user stopped with Ctrl+C after its
// partial report is written.
var errInterrupted = errors.New("interrupted")

func run(ctx context.Context, config *Config) error {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	// After the first interrupt, a second one ends the process at once.
	context.AfterFunc(ctx, stop)

	domains, err := catalog.LoadDomains(config.SitesFile)
	if err != nil {
		return fmt.Errorf("loading domains: %w", err)
	}
	servers, err := catalog.LoadServers(config.ResolversFile, config.filter())
	if err != nil {
		return fmt.Errorf("loading servers: %w", err)
	}

	var reporter bench.Reporter = bench.NoopReporter{}
	var p *progress
	if config.LogType == LogDefault && isTerminal(os.Stderr) {
		p = newProgress(os.Stderr, len(servers)*len(domains)*config.Repeats)
		slog.SetDefault(slog.New(progressHandler{next: slog.Default().Handler(), p: p}))
		p.run()
		reporter = p
	}
	results, err := bench.Run(ctx, config.benchOptions(), servers, domains, reporter)
	if p != nil {
		p.finish()
	}
	interrupted := err != nil && ctx.Err() != nil && len(results) > 0
	if err != nil && !interrupted {
		return fmt.Errorf("benchmark run failed: %w", err)
	}
	if interrupted {
		fmt.Fprintln(os.Stderr, "Interrupted. The report covers the lookups that finished.")
	}

	if err := report.Write(os.Stdout, results, config.OutputType); err != nil {
		return fmt.Errorf("writing the report: %w", err)
	}
	if interrupted {
		return errInterrupted
	}
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

	flag.StringVar(&config.ResolversFile, "f", "", "File of resolvers that replaces the built-in list, one per line: name;ip, name;ip;tls-name (DoT), name;ip;https-url (DoH), or name;ip;quic://tls-name (DoQ)")
	flag.DurationVar(&config.LookupTimeout, "t", 3*time.Second, "Timeout for one lookup attempt (e.g. 1500ms, 2s)")
	flag.IntVar(&config.Repeats, "n", 10, "Number of times each domain is queried")
	flag.StringVar(&config.SitesFile, "s", "", "File of domains, one per line, that replaces the built-in list")
	flag.StringVar(&outputType, "output", "default", "Report format: table, csv, or json. default is table")
	flag.StringVar(&logType, "log", "default", "Logging level: default, verbose, or disabled")
	flag.IntVar(&config.Retries, "retries", 2, "Retries of a failed lookup attempt, after a short wait. 0 disables them")
	flag.IntVar(&config.MaxConcurrency, "c", max(runtime.NumCPU()/2, 2), "Maximum lookups in flight at once, across all resolvers")
	flag.BoolVar(&config.OnlyMajorResolvers, "major", false, "Benchmark only major DNS resolvers")
	flag.BoolVar(&config.PrimaryOnly, "primary", false, "Benchmark only the first address of each built-in provider, such as Cloudflare-1")
	flag.StringVar(&family, "family", "ipv4", "Address family of the built-in resolvers: ipv4, ipv6, or all")
	flag.StringVar(&transport, "proto", "plain", "Transport of the built-in resolvers: plain, dot (DNS over TLS), doh (DNS over HTTPS), doq (DNS over QUIC), or all")
	flag.IntVar(&warmupRuns, "warmup", 0, "Unmeasured lookups of a domain right before a resolver's first measured lookup of it")
	flag.BoolVar(&config.List, "list", false, "Print the resolvers a run would use, after the filters or from -f, and exit")
	flag.BoolVar(&serveUI, "ui", false, "Start the embedded Web UI dashboard server instead of running the CLI benchmark")
	flag.StringVar(&listenAddr, "listen", "127.0.0.1:8080", "Address for the Web UI HTTP server (used with -ui). Use :8080 to accept connections from other machines")

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

	if config.Retries < 0 {
		fmt.Fprintf(os.Stderr, "Error: retries must be 0 or more\n")
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

	format, err := report.ParseFormat(outputType)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	config.OutputType = format

	fam, err := catalog.ParseFamily(family)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	config.Family = fam

	tr, err := catalog.ParseTransport(transport)
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

// benchOptions returns the options of a run that the flags set.
func (c *Config) benchOptions() bench.Options {
	return bench.Options{
		Repeats:     c.Repeats,
		Timeout:     c.LookupTimeout,
		Concurrency: c.MaxConcurrency,
		Retries:     c.Retries,
		Warmup:      c.WarmupRuns,
	}
}

// filter returns the built-in resolver filter that the flags set.
func (c *Config) filter() catalog.Filter {
	return catalog.Filter{
		Major:     c.OnlyMajorResolvers,
		Primary:   c.PrimaryOnly,
		Family:    c.Family,
		Transport: c.Transport,
	}
}

// listServers prints the resolvers that a run with config would use, one
// per line: name, address, transport, and the TLS name or DoH URL.
func listServers(w io.Writer, config *Config) error {
	servers, err := catalog.LoadServers(config.ResolversFile, config.filter())
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "Resolver\tAddress\tTransport\tEndpoint") //nolint:errcheck // Flush reports write errors
	for _, s := range servers {
		endpoint := s.TLSName + s.DoHURL + s.DoQName                                 // at most one is set
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", s.Name, s.Addr, s.Transport(), endpoint) //nolint:errcheck // Flush reports write errors
	}
	return tw.Flush()
}
