# dnsbench

A simple CLI tool to benchmark DNS resolvers against a list of domains, measuring latency and success rate, and producing reports in multiple formats (CSV, table, JSON).

## Features

- Built-in list of major, privacy-focused, regional, and alternative DNS resolvers
- Option to supply custom resolvers (`-f resolvers.txt`)
- Default popular domains list; can supply your own (`-s domains.txt`)
- Configurable number of repeats per domain (`-n`)
- Configurable per-query timeout (`-t`)
- Adjustable concurrency (`-c`)
- Multiple output formats: default, table, CSV, and JSON for integration with other tools
- Configurable logging levels (default, verbose, disabled)
- Warmup runs: Optionally perform warmup queries before benchmarking to reduce cold-start effects (`--warmup N`)

## Installation

Build locally (requires Go 1.24+):

```bash
git clone https://github.com/handsomefox/dnsbench.git
cd dnsbench
go mod tidy
make build
```

Or install directly:

```bash
go install github.com/handsomefox/dnsbench@latest
```

This produces `dnsbench` (and `dnsbench.exe` for Windows).

## Usage

```bash
# Default benchmark (10 repeats, 3s timeout)
./dnsbench

# Recommended
./dnsbench -log=disabled -c=8 -n=5 -major=true -output=table -warmup=2

# More repeats, longer timeout
./dnsbench -n 20 -t 5s

# Output results in CSV format
./dnsbench -output csv

# Output as a simple table
./dnsbench -output table

# Output as JSON
./dnsbench --output json > results.json

# Disable logging
./dnsbench -log disabled

# Verbose logging with CSV output
./dnsbench -log verbose -output csv

# Custom resolvers list, custom concurrency
./dnsbench -f myresolvers.txt -c 8

# Custom domains list
./dnsbench -s mydomains.txt

# Only benchmark major resolvers
./dnsbench -major

# Perform 3 warmup queries per resolver/domain before benchmarking
./dnsbench --warmup 3

# Custom output paths
./dnsbench -o summary.csv -matrix matrix.csv
```

### Flags

- `-f string` Optional file with extra resolvers (`name;ip` per line)
- `-s string` Optional file with domains to test (one domain per line)
- `-n int` (default 10) Number of times each domain is queried (must be 1–100)
- `-t duration` Timeout per DNS query (e.g. 1500ms, 2s)
- `-c int` Maximum concurrent DNS queries (default max(CPU/2, 2))
- `-output string` Output format: "default", "csv", "table", or "json"
- `-log string` Logging level: "default", "verbose", or "disabled"
- `-o string` Path for the output CSV report
- `-matrix string` Path for the per-site matrix report (domain × resolver)
- `-major` Benchmark only major DNS resolvers
- `--warmup int` Number of warmup queries per resolver/domain before benchmarking

Reports will be written to the current directory by default unless `-o` or `-matrix` are specified.

### Example JSON Output Structure

```json
{
  "results": [
    {
      "server": { "name": "Cloudflare-1", "addr": "1.1.1.1" },
      "stats": { "min": 12.3, "max": 25.6, "mean": 15.2, "count": 10, "errors": 0, "total": 10 },
      "per_domain_stats": {
        "google.com": { "min": 12.3, ... },
        "github.com": { ... }
      }
    },
    ...
  ],
  "summary": {
    "fastest_resolver": "Cloudflare-1",
    "slowest_resolver": "SomeDNS",
    "overall_success_rate": 0.98,
    ...
  }
}
```

## Makefile

- `make build` – compile binaries
- `make run` – build and run with default flags
- `make fmt` – run `gofmt`
- `make vet` – run `go vet`
- `make test` – run tests (if any)
- `make clean` – remove binaries

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE) for details.
