# dnsbench

A simple CLI tool to benchmark DNS resolvers against a list of domains, measuring latency and success rate, and producing CSV reports.

## Features

- Built-in list of major, privacy-focused, regional and alternative DNS resolvers
- Option to supply custom resolvers (`-f resolvers.txt`)
- Default popular domains list; can supply your own (`-s domains.txt`)
- Configurable number of repeats per domain (`-n`)
- Configurable per-query timeout (`-t`)
- Optional parallel resolver benchmarking (`-p`)
- Adjustable concurrency (`-c`)
- Generates two CSV reports:
  - **Main report**: per-resolver summary (`dns_benchmark_report.csv`)
  - **Matrix report**: per-domain × per-resolver latencies (`dns_benchmark_matrix.csv`)
- Pretty-printed summary in terminal
- Verbose/debug logging

## Installation

Build locally (requires Go 1.24+):

```bash
https://github.com/handsomefox/dnsbench.git
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
# Default benchmark (10 repeats, 2s timeout)
./dnsbench

# More repeats, longer timeout
./dnsbench -n 20 -t 3s

# Custom resolvers list, custom concurrency
./dnsbench -f myresolvers.txt -c 8

# Custom domains list
./dnsbench -s mydomains.txt
```

Flags:

- `-f string`
  optional resolvers file (`name;ip` per line)
- `-s string`
  optional domains file (one domain per line)
- `-n int` (default 10)
  number of repeats per domain
- `-t duration` (default 2s)
  DNS query timeout
- `-p`
  benchmark resolvers in parallel
- `-c int` (default `max(CPU/2,2)`)
  max concurrent DNS queries
- `-v`
  verbose logging
- `-o string`
  path to main CSV report
- `-matrix string`
  path to matrix CSV report

Reports will be written to the current directory by default.

## Makefile

- `make build` – compile binaries
- `make run` – build and run with default flags
- `make fmt` – run `gofmt`
- `make vet` – run `go vet`
- `make test` – run tests (if any)
- `make clean` – remove binaries

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE) for details.
