# dnsbench

A simple CLI tool to benchmark DNS resolvers against a list of domains, measuring latency and success rate, and producing CSV reports.

## Features

- Built-in list of major, privacy-focused, regional and alternative DNS resolvers
- Option to supply custom resolvers (`-f resolvers.txt`)
- Default popular domains list; can supply your own (`-s domains.txt`)
- Configurable number of repeats per domain (`-n`)
- Configurable per-query timeout (`-t`)
- Adjustable concurrency (`-c`)
- Pretty-printed summary in terminal
- Verbose/debug logging

## Installation

Build locally (requires Go 1.24+):

```bash
git clone https://github.com/handsomefox/dnsbench.git
cd dnsbench
go mod tidy
make build
```

Install:

```bash
go install github.com/handsomefox/dnsbench@latest
```

This produces `dnsbench` (and `dnsbench.exe` for Windows).

## Usage

```bash
# Default benchmark (10 repeats, 3s timeout)
./dnsbench

# Recommended: 10 repeats, 4-way concurrency, only major resolvers
./dnsbench -n=10 -c=4 -major=true

# More repeats, longer timeout
./dnsbench -n 20 -t 5s

# Custom resolvers list, custom concurrency
./dnsbench -f myresolvers.txt -c 8

# Custom domains list
./dnsbench -s mydomains.txt

# Only benchmark major resolvers
./dnsbench -major

# Custom output paths
./dnsbench -o summary.csv -matrix matrix.csv
```

### Flags

- `-f string`
  Optional file with extra resolvers (`name;ip` per line)
- `-s string`
  Optional file with domains to test (one domain per line)
- `-n int` (default 10)
  Number of times each domain is queried (must be 1–100)
- `-t duration` (default 3s)
  Timeout per DNS query (e.g. 1500ms, 2s)
- `-c int` (default max(CPU/2, 2))
  Maximum concurrent DNS queries
- `-v`
  Enable verbose/debug logging
- `-o string`
  Path for the output CSV report
- `-matrix string`
  Path for the per-site matrix report (domain × resolver)
- `-major`
  Benchmark only major DNS resolvers

Reports will be written to the current directory by default unless `-o` or `-matrix` are specified.

## Makefile

- `make build` – compile binaries
- `make run` – build and run with default flags
- `make fmt` – run `gofmt`
- `make vet` – run `go vet`
- `make test` – run tests (if any)
- `make clean` – remove binaries

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE) for details.
