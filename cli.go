package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
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
	MaxConcurrency     int

	// Output and logging
	OutputType OutputType
	LogType    LogType

	WarmupRuns int
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
	servers, err := loadServers(config.ResolversFile, config.OnlyMajorResolvers)
	if err != nil {
		return fmt.Errorf("loading servers: %w", err)
	}

	slog.LogAttrs(ctx, slog.LevelInfo, "Loaded DNS servers", slog.Int("count", len(servers)))

	// Run benchmark
	results := runBenchmark(ctx, config, servers, domains)

	// Print summary
	printSummary(results, config.OutputType)

	return nil
}

func parseFlags() *Config {
	var config Config

	var (
		outputType string
		logType    string
		warmupRuns int
	)

	flag.StringVar(&config.ResolversFile, "f", "", "Optional file with extra resolvers (name;ip)")
	flag.DurationVar(&config.LookupTimeout, "t", 3*time.Second, "Timeout per DNS query (e.g. 1500ms, 2s)")
	flag.IntVar(&config.Repeats, "n", 10, "Number of times each domain is queried")
	flag.StringVar(&config.SitesFile, "s", "", "Optional file with domains to test (one domain per line)")
	flag.StringVar(&outputType, "output", "default", "Output format: default, csv, table, or json")
	flag.StringVar(&logType, "log", "default", "Logging level: default, verbose, or disabled")
	flag.IntVar(&config.MaxConcurrency, "c", max(runtime.NumCPU()/2, 2), "Maximum concurrent DNS queries")
	flag.BoolVar(&config.OnlyMajorResolvers, "major", false, "Benchmark only major DNS resolvers")
	flag.IntVar(&warmupRuns, "warmup", 0, "Number of warmup queries per resolver/domain before benchmarking")

	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `DNS Benchmark Tool

Test DNS resolvers against popular websites to measure latency and reliability.

Usage:
  dnsbench [options]

Options:
`)
		flag.PrintDefaults()
		fmt.Fprintf(flag.CommandLine.Output(), `
Examples:
  # Default benchmark
  dnsbench

  # Test with more repeats and longer timeout
  dnsbench -n 20 -t 3s

  # Use custom resolver list and increase concurrency
  dnsbench -f myresolvers.txt -c 10

  # Benchmark with custom domain list
  dnsbench -s mydomains.txt
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

	config.WarmupRuns = warmupRuns

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

	file, err := os.Open(sitesFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

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

// loadServers loads DNS servers from a file or uses built-in resolvers.
// Format: name;ip per line. Comments start with #.
// If resolversFile is empty, built-in resolvers are used based on onlyMajor flag.
func loadServers(resolversFile string, onlyMajor bool) ([]DNSServer, error) {
	servers := make([]DNSServer, 0)

	if resolversFile == "" {
		if onlyMajor {
			servers = append(servers, builtinMajorResolvers...)
		} else {
			servers = append(servers, builtInResolvers...)
		}
		return servers, nil
	}

	file, err := os.Open(resolversFile)
	if err != nil {
		return nil, fmt.Errorf("opening resolvers file: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Split(line, ";")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid format at line %d: expected 'name;ip'", lineNum)
		}

		name := strings.TrimSpace(parts[0])
		addr := strings.TrimSpace(parts[1])

		if name == "" || addr == "" {
			return nil, fmt.Errorf("empty name or IP at line %d", lineNum)
		}

		// Basic IP validation
		if net.ParseIP(addr) == nil {
			return nil, fmt.Errorf("invalid IP address at line %d: %s", lineNum, addr)
		}

		servers = append(servers, DNSServer{Name: name, Addr: addr})
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading resolvers file: %w", err)
	}

	if len(servers) == 0 {
		return nil, errors.New("no valid resolvers found in file")
	}

	return servers, nil
}
