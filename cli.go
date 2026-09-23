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
	"unicode/utf8"

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
	Kind               string
	Providers          []string
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
		kind       string
		providers  string
		warmupRuns int
		serveUI    bool
		listenAddr string
	)

	flag.StringVar(&config.ResolversFile, "f", "", "Resolver file that replaces the built-in list, one name;ip per line, with an optional\nthird field: a TLS name for DoT, an https:// URL for DoH, or quic://name for DoQ")
	flag.DurationVar(&config.LookupTimeout, "t", 3*time.Second, "Timeout for one lookup attempt, such as 1500ms or 2s")
	flag.IntVar(&config.Repeats, "n", 10, "Measured lookups of each domain per resolver")
	flag.StringVar(&config.SitesFile, "s", "", "Domain file that replaces the built-in list, one domain per line")
	flag.StringVar(&outputType, "output", "table", "Report format: table, csv, or json")
	flag.StringVar(&logType, "log", "default", "Logging: default, verbose, or disabled")
	flag.IntVar(&config.Retries, "retries", 2, "Retries of a failed lookup attempt, after a short wait. 0 disables them")
	flag.IntVar(&config.MaxConcurrency, "c", max(runtime.NumCPU()/2, 2), "Maximum lookups in flight at once, across all resolvers")
	flag.BoolVar(&config.OnlyMajorResolvers, "major", false, "Only the major providers: Cloudflare, Google, Quad9, NextDNS, and AdGuard")
	flag.BoolVar(&config.PrimaryOnly, "primary", false, "Only the first address of each service, such as Cloudflare-1")
	flag.StringVar(&family, "family", "ipv4", "Address family: ipv4, ipv6, or all")
	flag.StringVar(&transport, "proto", "plain", "Transport: plain, dot, doh, doq, or all")
	flag.StringVar(&providers, "provider", "", "Only the services of these companies, separated by commas, such as cloudflare,google,quad9")
	flag.StringVar(&kind, "kind", "all", "Kind of resolver: global, filtering (malware, ads, or family filters), privacy, regional, or all")
	flag.IntVar(&warmupRuns, "warmup", 0, "Unmeasured lookups of a domain right before a resolver's first measured lookup of it")
	flag.BoolVar(&config.List, "list", false, "Print the resolvers a run would use, after the filters or from -f, and exit")
	flag.BoolVar(&serveUI, "ui", false, "Serve the dashboard instead of running a benchmark")
	flag.StringVar(&listenAddr, "listen", "127.0.0.1:8080", "Dashboard address. :8080 accepts connections from other machines")

	flag.Usage = func() { printUsage(flag.CommandLine) }

	// Parse only fails with ExitOnError by exiting.
	_ = flag.CommandLine.Parse(fixDashes(os.Args[1:])) //nolint:errcheck // see above

	// dnsbench takes no arguments besides flags. Ignoring one would run a
	// benchmark for "dnsbench ui" instead of serving the dashboard.
	if flag.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "Error:", unexpectedArgument(flag.Arg(0)))
		os.Exit(1)
	}

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

	k, err := catalog.ParseKind(kind)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	config.Kind = k

	config.Providers, err = catalog.ParseProviders(providers)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

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
		Kind:      c.Kind,
		Providers: c.Providers,
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

// usageGroups orders the flags in -h by what they control.
var usageGroups = []struct {
	title string
	flags []string
}{
	{"Resolvers", []string{"provider", "major", "primary", "family", "proto", "kind", "f", "list"}},
	{"Domains", []string{"s"}},
	{"Measurement", []string{"n", "t", "c", "retries", "warmup"}},
	{"Output", []string{"output", "log"}},
	{"Dashboard", []string{"ui", "listen"}},
}

// printUsage writes -h: the ways to run dnsbench, the flags by group, and
// examples. Each flag's line comes from its definition, so the text cannot
// drift from the flags.
func printUsage(fs *flag.FlagSet) {
	var b strings.Builder
	b.WriteString(`dnsbench measures how fast and how reliably DNS resolvers answer.

Usage:
  dnsbench [flags]          Run a benchmark and print a report
  dnsbench -ui [flags]      Serve the dashboard and open it in a browser
  dnsbench -list [flags]    Print the resolvers a run would use
`)
	for _, g := range usageGroups {
		fmt.Fprintf(&b, "\n%s:\n", g.title)
		for _, name := range g.flags {
			f := fs.Lookup(name)
			if f == nil {
				continue
			}
			arg, usage := flag.UnquoteUsage(f)
			head := "  -" + f.Name
			if arg != "" {
				head += " " + arg
			}
			// A usage with several lines keeps its column.
			usage = strings.ReplaceAll(usage, "\n", "\n"+strings.Repeat(" ", 23))
			fmt.Fprintf(&b, "%-22s %s", head, usage)
			if f.DefValue != "" && f.DefValue != "false" && f.DefValue != "0" && f.DefValue != "default" {
				fmt.Fprintf(&b, " (default %s)", f.DefValue)
			}
			b.WriteString("\n")
		}
	}
	b.WriteString(`
Flags take one dash or two: -n 5 and --n 5 are the same.

Examples:
  dnsbench -major -primary                   One address of each major provider
  dnsbench -provider cloudflare,quad9        Cloudflare and Quad9, filtering too
  dnsbench -major -proto all -family all     Every transport and family they offer
  dnsbench -f resolvers.txt -s domains.txt   Your own resolvers and domains
  dnsbench -output csv > results.csv         Save a report
  dnsbench -ui                               The dashboard at http://127.0.0.1:8080
`)
	//nolint:errcheck // best-effort help output
	_, _ = io.WriteString(fs.Output(), b.String())
}

// lookalikeDashes are characters that phone keyboards and word processors
// put where a hyphen was typed. The flag parser takes only "-".
const lookalikeDashes = "\u2010\u2011\u2012\u2013\u2014\u2015\u2212\ufe63\uff0d"

// unexpectedArgument explains an argument that is not a flag, and suggests
// the flag it most likely meant.
func unexpectedArgument(arg string) string {
	name := strings.TrimLeft(arg, "-"+lookalikeDashes)
	lead := arg[:len(arg)-len(name)]
	msg := fmt.Sprintf("unexpected argument %q.", arg)
	if strings.ContainsAny(lead, lookalikeDashes) {
		first, _ := utf8.DecodeRuneInString(lead)
		msg += fmt.Sprintf(" It starts with %q, which looks like a hyphen but is not one. Keyboards on phones often swap it in.", first)
	}
	if f := flag.Lookup(name); f != nil {
		return msg + fmt.Sprintf(" Did you mean -%s, with a plain hyphen?", f.Name)
	}
	return msg + " Flags start with a hyphen, and dnsbench -h lists them."
}

// fixDashes replaces a lookalike dash in front of a flag name with a
// hyphen, so "–ui" typed on a phone keyboard works as "-ui". It changes an
// argument only when what follows the dashes names a flag, as in "–ui" or
// "——n=5", so a flag's value is left alone unless it could only be a flag.
func fixDashes(args []string) []string {
	out := make([]string, len(args))
	for i, arg := range args {
		out[i] = arg
		name := strings.TrimLeft(arg, "-"+lookalikeDashes)
		lead := arg[:len(arg)-len(name)]
		if !strings.ContainsAny(lead, lookalikeDashes) {
			continue
		}
		flagName, _, _ := strings.Cut(name, "=")
		if flag.Lookup(flagName) != nil {
			out[i] = "-" + name
		}
	}
	return out
}
