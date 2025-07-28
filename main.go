package main

import (
	"bufio"
	"context"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"net"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"
)

// Config holds all CLI configuration
type Config struct {
	// What to test
	ResolversFile string
	SitesFile     string

	// Test setup
	LookupTimeout      time.Duration
	Repeats            int
	Parallel           bool
	Verbose            bool
	OnlyMajorResolvers bool
	MaxConcurrency     int

	// Reports
	MatrixReportPath  string
	GeneralReportPath string
}

// DNSServer represents a resolver to be benchmarked
type DNSServer struct {
	Name string
	Addr string
}

// BenchmarkResult contains the results for a single resolver
type BenchmarkResult struct {
	Server     DNSServer
	Stats      Stats
	DomainMean map[string]float64
}

// Stats contains latency statistics for a resolver
type Stats struct {
	Min    float64
	Max    float64
	Mean   float64
	Median float64
	Count  int
	Errors int
	Total  int
}

// IsValid returns true if the stats contain valid data
func (s Stats) IsValid() bool {
	return s.Count > 0 && !math.IsNaN(s.Mean)
}

// SuccessRate returns the success rate as a percentage
func (s Stats) SuccessRate() float64 {
	if s.Total == 0 {
		return 0
	}
	return float64(s.Count) / float64(s.Total)
}

var (
	builtInResolvers = []DNSServer{
		// Major providers
		{Name: "Cloudflare", Addr: "1.1.1.1"},
		{Name: "Cloudflare-Alt", Addr: "1.0.0.1"},
		{Name: "Google", Addr: "8.8.8.8"},
		{Name: "Google-Alt", Addr: "8.8.4.4"},
		{Name: "Quad9", Addr: "9.9.9.9"},
		{Name: "Quad9-Alt", Addr: "149.112.112.112"},
		{Name: "OpenDNS", Addr: "208.67.222.222"},
		{Name: "OpenDNS-Alt", Addr: "208.67.220.220"},

		// Ad-blocking and filtering
		{Name: "AdGuard", Addr: "94.140.14.14"},
		{Name: "AdGuard-Alt", Addr: "94.140.15.15"},
		{Name: "CleanBrowsing", Addr: "185.228.168.9"},
		{Name: "CleanBrowsing-Alt", Addr: "185.228.169.9"},
		{Name: "NextDNS", Addr: "45.90.28.0"},
		{Name: "NextDNS-Alt", Addr: "45.90.30.0"},
		{Name: "ControlD", Addr: "76.76.2.0"},
		{Name: "ControlD-Alt", Addr: "76.76.10.0"},

		// Privacy-focused
		{Name: "Mullvad", Addr: "194.242.2.2"},
		{Name: "Mullvad-Alt", Addr: "194.242.2.3"},
		{Name: "DNS0-EU", Addr: "193.110.81.0"},
		{Name: "DNS0-EU-Alt", Addr: "185.253.5.0"},
		{Name: "UncensoredDNS", Addr: "91.239.100.100"},
		{Name: "UncensoredDNS-Alt", Addr: "89.233.43.71"},

		// Regional/National
		{Name: "AliDNS", Addr: "223.5.5.5"},
		{Name: "AliDNS-Alt", Addr: "223.6.6.6"},
		{Name: "DNSPod", Addr: "119.29.29.29"},
		{Name: "DNSPod-Alt", Addr: "119.28.28.28"},
		{Name: "Canadian-Shield", Addr: "149.112.121.10"},
		{Name: "Canadian-Shield-Alt", Addr: "149.112.122.10"},

		// Alternative providers
		{Name: "DNS-SB", Addr: "185.222.222.222"},
		{Name: "DNS-SB-Alt", Addr: "45.11.45.11"},
		{Name: "LibreDNS", Addr: "116.202.176.26"},
		{Name: "LibreDNS-Alt", Addr: "116.203.115.192"},
	}

	builtinMajorResolvers = []DNSServer{
		{Name: "Cloudflare", Addr: "1.1.1.1"},
		{Name: "Cloudflare-Alt", Addr: "1.0.0.1"},
		{Name: "Google", Addr: "8.8.8.8"},
		{Name: "Google-Alt", Addr: "8.8.4.4"},
		{Name: "Quad9", Addr: "9.9.9.9"},
		{Name: "Quad9-Alt", Addr: "149.112.112.112"},
		{Name: "NextDNS", Addr: "45.90.28.0"},
		{Name: "NextDNS-Alt", Addr: "45.90.30.0"},
		{Name: "AdGuard", Addr: "94.140.14.14"},
		{Name: "AdGuard-Alt", Addr: "94.140.15.15"},
	}

	defaultSites = []string{
		// Search engines
		"google.com", "bing.com", "duckduckgo.com", "yahoo.com",

		// Knowledge & reference
		"wikipedia.org", "archive.org", "stackoverflow.com",
		"github.com", "gitlab.com",

		// Programming languages
		"python.org", "golang.org", "nodejs.org", "rust-lang.org",

		// Major news outlets
		"nytimes.com", "bbc.com", "cnn.com", "reuters.com",
		"theguardian.com", "bloomberg.com",

		// E-commerce
		"amazon.com", "ebay.com", "etsy.com", "shopify.com",

		// Streaming & entertainment
		"youtube.com", "netflix.com", "spotify.com", "vimeo.com",

		// Social & communication
		"linkedin.com", "zoom.us", "slack.com",

		// Cloud & tech
		"cloudflare.com", "aws.amazon.com", "microsoft.com",

		// Finance
		"paypal.com", "stripe.com", "visa.com",

		// Government & organizations
		"usa.gov", "europa.eu", "un.org", "nasa.gov",

		// Health & science
		"nih.gov", "cdc.gov", "mayoclinic.org",

		// Travel
		"booking.com", "airbnb.com", "expedia.com",

		// Additional popular sites
		"reddit.com", "twitter.com", "facebook.com", "instagram.com",
		"tiktok.com", "pinterest.com", "wordpress.com", "medium.com",
	}
)

func main() {
	config := parseFlags()

	level := slog.LevelInfo
	if config.Verbose {
		level = slog.LevelDebug
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	}))
	slog.SetDefault(logger)

	if err := run(config); err != nil {
		slog.Error("benchmark failed", "error", err)
		os.Exit(1)
	}
}

func parseFlags() Config {
	var config Config

	flag.StringVar(&config.ResolversFile, "f", "",
		"Optional file with extra resolvers (name;ip)")
	flag.StringVar(&config.GeneralReportPath, "o", "",
		"Path for the output CSV report")
	flag.DurationVar(&config.LookupTimeout, "t", 2*time.Second,
		"Timeout per DNS query (e.g. 1500ms, 2s)")
	flag.IntVar(&config.Repeats, "n", 10,
		"Number of times each domain is queried")
	flag.BoolVar(&config.Parallel, "p", false,
		"Run benchmark in parallel for each DNS resolver")
	flag.StringVar(&config.SitesFile, "s", "",
		"Optional file with domains to test (one domain per line)")
	flag.BoolVar(&config.Verbose, "v", false,
		"Enable verbose/debug logging")
	flag.StringVar(&config.MatrixReportPath, "matrix", "",
		"Path for the per-site matrix report (domain × resolver)")
	flag.IntVar(&config.MaxConcurrency, "c", max(runtime.NumCPU()/2, 2),
		"Maximum concurrent DNS queries")
	flag.BoolVar(&config.OnlyMajorResolvers, "major", false,
		"Benchmark only major DNS resolvers")

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

	return config
}

func run(config Config) error {
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

	slog.Info("loaded domains", "count", len(domains))

	// Load DNS servers
	servers, err := loadServers(config.ResolversFile, config.OnlyMajorResolvers)
	if err != nil {
		return fmt.Errorf("loading servers: %w", err)
	}

	slog.Info("loaded DNS servers", "count", len(servers))

	// Run benchmark
	results, err := runBenchmark(ctx, config, servers, domains)
	if err != nil {
		return fmt.Errorf("running benchmark: %w", err)
	}

	// Generate reports
	if err := generateReports(config, results, domains); err != nil {
		return fmt.Errorf("generating reports: %w", err)
	}

	// Print summary
	printSummary(results)

	return nil
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
			slog.Warn("skipping invalid domain", "line", lineNum, "domain", line)
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
			slog.Warn("skipping malformed line", "line", lineNum, "content", line)
			continue
		}

		name := strings.TrimSpace(parts[0])
		addr := strings.TrimSpace(parts[1])

		if name == "" || addr == "" {
			slog.Warn("skipping empty name or address", "line", lineNum)
			continue
		}

		// Basic IP validation
		if net.ParseIP(addr) == nil {
			slog.Warn("skipping invalid IP address", "line", lineNum, "ip", addr)
			continue
		}

		servers = append(servers, DNSServer{Name: name, Addr: addr})
	}

	return servers, scanner.Err()
}

func runBenchmark(
	ctx context.Context,
	config Config,
	servers []DNSServer,
	domains []string,
) ([]BenchmarkResult, error) {
	results := make([]BenchmarkResult, len(servers))

	if config.Parallel {
		return runParallelBenchmark(ctx, config, servers, domains)
	}

	// Sequential benchmark
	for i, server := range servers {
		slog.Info("benchmarking resolver", "name", server.Name, "addr", server.Addr)

		stats, domainMeans, err := benchmarkResolver(ctx, config, server, domains)
		if err != nil {
			return nil, fmt.Errorf("benchmarking %s: %w", server.Name, err)
		}

		results[i] = BenchmarkResult{
			Server:     server,
			Stats:      stats,
			DomainMean: domainMeans,
		}
	}

	return results, nil
}

func runParallelBenchmark(
	ctx context.Context,
	config Config,
	servers []DNSServer,
	domains []string,
) ([]BenchmarkResult, error) {
	results := make([]BenchmarkResult, len(servers))
	var mu sync.Mutex

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(runtime.NumCPU())

	for i, server := range servers {
		g.Go(func() error {
			slog.Debug("benchmarking resolver", "name", server.Name, "addr", server.Addr)

			stats, domainMeans, err := benchmarkResolver(ctx, config, server, domains)
			if err != nil {
				return fmt.Errorf("benchmarking %s: %w", server.Name, err)
			}

			mu.Lock()
			results[i] = BenchmarkResult{
				Server:     server,
				Stats:      stats,
				DomainMean: domainMeans,
			}
			mu.Unlock()

			return nil
		})
	}

	return results, g.Wait()
}

func benchmarkResolver(
	ctx context.Context,
	config Config,
	server DNSServer,
	domains []string,
) (Stats, map[string]float64, error) {
	var (
		allLatencies  []float64
		errorCount    int
		mu            sync.Mutex
		domainResults = make(map[string][]float64)
	)

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(config.MaxConcurrency)

	totalQueries := len(domains) * config.Repeats

	for _, domain := range domains {
		for range config.Repeats {
			g.Go(func() error {
				latency, err := queryDNS(ctx, domain, server.Addr, config.LookupTimeout)

				mu.Lock()
				defer mu.Unlock()

				if err != nil {
					errorCount++
					if errors.Is(err, context.DeadlineExceeded) {
						slog.Debug("DNS query timeout",
							"resolver", server.Name,
							"domain", domain,
							"timeout", config.LookupTimeout)
					} else {
						slog.Debug("DNS query error",
							"resolver", server.Name,
							"domain", domain,
							"error", err)
					}
					return nil
				}

				latencyMs := latency.Seconds() * 1000
				allLatencies = append(allLatencies, latencyMs)
				domainResults[domain] = append(domainResults[domain], latencyMs)

				return nil
			})
		}
	}

	if err := g.Wait(); err != nil {
		return Stats{}, nil, err
	}

	// Calculate domain means
	domainMeans := make(map[string]float64)
	for domain, latencies := range domainResults {
		if len(latencies) > 0 {
			sum := 0.0
			for _, lat := range latencies {
				sum += lat
			}
			domainMeans[domain] = sum / float64(len(latencies))
		}
	}

	stats := calculateStats(allLatencies, errorCount, totalQueries)
	return stats, domainMeans, nil
}

func queryDNS(
	ctx context.Context,
	domain, resolver string,
	timeout time.Duration,
) (time.Duration, error) {
	queryCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	dialer := &net.Dialer{
		Timeout: timeout,
	}

	netResolver := &net.Resolver{
		PreferGo: true,
		Dial: func(dialCtx context.Context, network, address string) (net.Conn, error) {
			return dialer.DialContext(dialCtx, "udp", net.JoinHostPort(resolver, "53"))
		},
	}

	start := time.Now()

	_, err := netResolver.LookupHost(queryCtx, domain)
	elapsed := time.Since(start)

	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return 0, fmt.Errorf("DNS query timeout for %s via %s: %w", domain, resolver, err)
		}
		return 0, fmt.Errorf("DNS query failed for %s via %s: %w", domain, resolver, err)
	}

	return elapsed, nil
}

func calculateStats(latencies []float64, errors, total int) Stats {
	if len(latencies) == 0 {
		return Stats{
			Min:    math.NaN(),
			Max:    math.NaN(),
			Mean:   math.NaN(),
			Median: math.NaN(),
			Count:  0,
			Errors: errors,
			Total:  total,
		}
	}

	sort.Float64s(latencies)

	sum := 0.0
	for _, lat := range latencies {
		sum += lat
	}

	median := latencies[len(latencies)/2]
	if len(latencies)%2 == 0 && len(latencies) > 1 {
		median = (latencies[len(latencies)/2-1] + latencies[len(latencies)/2]) / 2
	}

	return Stats{
		Min:    latencies[0],
		Max:    latencies[len(latencies)-1],
		Mean:   sum / float64(len(latencies)),
		Median: median,
		Count:  len(latencies),
		Errors: errors,
		Total:  total,
	}
}

func generateReports(config Config, results []BenchmarkResult, domains []string) error {
	// Generate main report
	if config.GeneralReportPath != "" {
		if err := writeMainReport(config.GeneralReportPath, results); err != nil {
			return fmt.Errorf("writing main report: %w", err)
		}
	}

	// Generate matrix report
	if config.MatrixReportPath != "" {
		if err := writeMatrixReport(config.MatrixReportPath, results, domains); err != nil {
			return fmt.Errorf("writing matrix report: %w", err)
		}
	}

	return nil
}

func writeMainReport(path string, results []BenchmarkResult) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	header := []string{
		"Name", "Address", "Min(ms)", "Max(ms)", "Mean(ms)", "Median(ms)",
		"Successful", "Errors", "Total", "Success Rate(%)",
	}

	if err := writer.Write(header); err != nil {
		return err
	}

	for _, result := range results {
		record := []string{
			result.Server.Name,
			result.Server.Addr,
			formatFloat(result.Stats.Min),
			formatFloat(result.Stats.Max),
			formatFloat(result.Stats.Mean),
			formatFloat(result.Stats.Median),
			strconv.Itoa(result.Stats.Count),
			strconv.Itoa(result.Stats.Errors),
			strconv.Itoa(result.Stats.Total),
			fmt.Sprintf("%.1f", result.Stats.SuccessRate()*100),
		}

		if err := writer.Write(record); err != nil {
			return err
		}
	}

	slog.Info("main report written", "path", path)
	return nil
}

func writeMatrixReport(path string, results []BenchmarkResult, domains []string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Build header
	header := []string{"Domain"}
	for _, result := range results {
		header = append(header, result.Server.Name)
	}

	if err := writer.Write(header); err != nil {
		return err
	}

	// Write data rows
	for _, domain := range domains {
		row := []string{domain}

		for _, result := range results {
			if mean, exists := result.DomainMean[domain]; exists {
				row = append(row, formatFloat(mean))
			} else {
				row = append(row, "")
			}
		}

		if err := writer.Write(row); err != nil {
			return err
		}
	}

	slog.Info("matrix report written", "path", path)
	return nil
}

func printSummary(results []BenchmarkResult) {
	// Filter valid results and sort by success rate, then by mean latency
	var validResults []BenchmarkResult
	var failedResults []BenchmarkResult
	for _, result := range results {
		if result.Stats.IsValid() {
			validResults = append(validResults, result)
		} else {
			failedResults = append(failedResults, result)
		}
	}

	sort.Slice(validResults, func(i, j int) bool {
		iRate := validResults[i].Stats.SuccessRate()
		jRate := validResults[j].Stats.SuccessRate()

		if iRate == jRate {
			return validResults[i].Stats.Mean < validResults[j].Stats.Mean
		}
		return iRate > jRate
	})

	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("DNS BENCHMARK RESULTS - TOP PERFORMERS")
	fmt.Println(strings.Repeat("=", 80))
	fmt.Printf("%-20s %10s %10s %10s %10s\n",
		"Resolver", "Success%", "Mean(ms)", "Min(ms)", "Max(ms)")
	fmt.Println(strings.Repeat("-", 80))

	for i := range validResults {
		result := validResults[i]
		fmt.Printf("%-20s %9.1f%% %9.2f %9.2f %9.2f\n",
			truncateString(result.Server.Name, 20),
			result.Stats.SuccessRate()*100,
			result.Stats.Mean,
			result.Stats.Min,
			result.Stats.Max)
	}

	fmt.Println(strings.Repeat("-", 80))

	// Add failed resolvers section
	if len(failedResults) > 0 {
		fmt.Println("\nFAILED RESOLVERS:")
		fmt.Println(strings.Repeat("-", 80))
		fmt.Printf("%-20s %10s %10s\n", "Resolver", "Address", "Errors")
		fmt.Println(strings.Repeat("-", 80))

		for _, result := range failedResults {
			fmt.Printf("%-20s %10s %10d\n",
				truncateString(result.Server.Name, 20),
				result.Server.Addr,
				result.Stats.Errors)
		}
	}

	fmt.Println(strings.Repeat("-", 80))

	if len(validResults) > 0 {
		fmt.Printf("Tested %d resolvers with %d total queries each\n",
			len(results),
			validResults[0].Stats.Total)
	}
}

func isValidDomain(domain string) bool {
	if len(domain) == 0 || len(domain) > 253 {
		return false
	}
	return !strings.Contains(domain, " ") && strings.Contains(domain, ".")
}

func formatFloat(f float64) string {
	if math.IsNaN(f) {
		return ""
	}
	return fmt.Sprintf("%.2f", f)
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
