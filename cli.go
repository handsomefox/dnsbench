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
	Verbose            bool
	OnlyMajorResolvers bool
	MaxConcurrency     int

	// Reports
	MatrixReportPath  string
	GeneralReportPath string
}

func run(config *Config) error {
	ctx, cancel := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer cancel()

	// Load domain list
	domains, err := loadDomains(config.SitesFile)
	if err != nil {
		return fmt.Errorf("loading domains: %w", err)
	}

	slog.Info("Loaded domains", slog.Int("count", len(domains)))

	// Load DNS servers
	servers, err := loadServers(config.ResolversFile, config.OnlyMajorResolvers)
	if err != nil {
		return fmt.Errorf("loading servers: %w", err)
	}

	slog.Info("Loaded DNS servers", slog.Int("count", len(servers)))

	// Run benchmark
	results := runBenchmark(ctx, config, servers, domains)

	// Generate reports
	if err := generateReports(config, results, domains); err != nil {
		return fmt.Errorf("generating reports: %w", err)
	}

	// Print summary
	printSummary(results)

	return nil
}

func parseFlags() *Config {
	var config Config

	flag.StringVar(&config.ResolversFile, "f", "", "Optional file with extra resolvers (name;ip)")
	flag.StringVar(&config.GeneralReportPath, "o", "", "Path for the output CSV report")
	flag.DurationVar(&config.LookupTimeout, "t", 3*time.Second, "Timeout per DNS query (e.g. 1500ms, 2s)")
	flag.IntVar(&config.Repeats, "n", 10, "Number of times each domain is queried")
	flag.StringVar(&config.SitesFile, "s", "", "Optional file with domains to test (one domain per line)")
	flag.BoolVar(&config.Verbose, "v", false, "Enable verbose/debug logging")
	flag.StringVar(&config.MatrixReportPath, "matrix", "", "Path for the per-site matrix report (domain × resolver)")
	flag.IntVar(&config.MaxConcurrency, "c", max(runtime.NumCPU()/2, 2), "Maximum concurrent DNS queries")
	flag.BoolVar(&config.OnlyMajorResolvers, "major", false, "Benchmark only major DNS resolvers")

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
	if config.Repeats < 1 || config.Repeats > 100 {
		fmt.Fprintf(os.Stderr, "Error: repeats must be between 1 and 100\n")
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
		return nil, err
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
			slog.Warn("Skipping malformed line",
				slog.Int("line", lineNum),
				slog.String("content", line),
			)
			continue
		}

		name := strings.TrimSpace(parts[0])
		addr := strings.TrimSpace(parts[1])

		if name == "" || addr == "" {
			slog.Warn("Skipping empty name or address", slog.Int("line", lineNum))
			continue
		}

		// Basic IP validation
		if net.ParseIP(addr) == nil {
			slog.Warn("Skipping invalid IP address",
				slog.Int("line", lineNum),
				slog.String("ip", addr),
			)
			continue
		}

		servers = append(servers, DNSServer{Name: name, Addr: addr})
	}

	return servers, scanner.Err()
}
