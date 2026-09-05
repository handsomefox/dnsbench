# dnsbench

[![CI](https://github.com/handsomefox/dnsbench/actions/workflows/ci.yml/badge.svg)](https://github.com/handsomefox/dnsbench/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

dnsbench measures how fast and how reliably DNS resolvers answer from your
machine. It benchmarks one resolver at a time against a list of domains and
reports latency and success rate as text, a table, CSV, or JSON. An embedded Web
UI shows a run while it happens.

## Build

You need Go 1.27.1 or later, Make, and Node.js. Vite 7 pins Node to 20.19.0 or
later on the 20.x line, or to 22.12.0 and later.

```bash
git clone https://github.com/handsomefox/dnsbench.git
cd dnsbench
make build
```

`make build` builds the Web UI, runs the Go tests, and writes `bin/dnsbench`.

`server.go` embeds `webui/dist/`, and that directory is not committed, so plain
`go build` and `go test` fail in a fresh checkout until the Web UI is built.
`go install github.com/handsomefox/dnsbench@latest` fails for the same reason.
Build through Make.

## Run a benchmark

To test the built-in major resolvers with five lookups per domain, run:

```bash
./bin/dnsbench -major -n 5 -c 8 -warmup 2 -log disabled -output table
```

To test every built-in resolver with more repeats and a longer timeout, run:

```bash
./bin/dnsbench -n 20 -t 5s
```

The built-in resolver and domain lists are in [`data.go`](data.go). Every flag,
its default, and every report field is in the [CLI reference](docs/cli.md).

## Use your own resolvers and domains

Write `resolvers.txt` with one `name;ip` pair per line:

```text
Cloudflare-1;1.1.1.1
Google-1;8.8.8.8
```

Write `domains.txt` with one domain per line:

```text
github.com
google.com
```

Pass both files:

```bash
./bin/dnsbench -f resolvers.txt -s domains.txt -output table
```

Each file replaces the matching built-in list instead of adding to it, and `-f`
overrides `-major`. For the validation rules, see
[input files](docs/cli.md#input-files).

## Save a report

```bash
./bin/dnsbench -log disabled -output json > results.json
./bin/dnsbench -log disabled -output csv > results.csv
```

CSV writes the resolvers that answered to standard output and the ones that
failed to standard error, so the redirect above captures only the successes.
JSON puts both groups in one document, under `results` and `failures`.

One catch before you script against JSON. A resolver with no successful lookup
gets `NaN` for its latencies, `encoding/json` refuses `NaN`, and dnsbench then
writes `failed to encode json results` to standard error and produces no report
at all. It still exits `0`. See [report formats](docs/cli.md#report-formats).

## Start the Web UI

```bash
./bin/dnsbench -ui -listen 127.0.0.1:8080
```

dnsbench tries to open your browser at that address. If it does not, open
<http://127.0.0.1:8080> yourself. Pick the domains, resolvers, and options, then
click **Start benchmark**. **Stop** ends a run early, **Reset** clears the
results, and **View results** switches to the results table.

`-listen` defaults to `:8080`, which accepts connections from anywhere that can
reach your machine. The dashboard has no authentication, and it runs lookups
against whatever resolver addresses a request names. If you expose it beyond
your own machine, put it behind a firewall or a reverse proxy.

## Documentation

- [CLI reference](docs/cli.md): flags, lookup behavior, input files, and report formats.
- [Web UI development](webui/README.md): running and checking the dashboard.
- [Contributing](AGENTS.md): build commands, conventions, and what to check.

dnsbench uses the [MIT license](LICENSE).
