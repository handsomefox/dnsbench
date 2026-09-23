# dnsbench

[![CI](https://github.com/handsomefox/dnsbench/actions/workflows/ci.yml/badge.svg)](https://github.com/handsomefox/dnsbench/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

dnsbench measures how fast and how reliably DNS resolvers answer from your
machine. It benchmarks one resolver at a time against a list of domains and
reports latency and success rate as text, a table, CSV, or JSON. An embedded Web
UI shows a run while it happens.

## Build

You need Go 1.27.1 or later.

```bash
git clone https://github.com/handsomefox/dnsbench.git
cd dnsbench
make build
```

`make build` runs the tests and writes `bin/dnsbench`. Plain `go build` works
too. The Web UI is a Go template, a stylesheet, and one script in `ui/`. The
binary embeds all three, so the build needs no Node.js.

## Run a benchmark

To test the built-in major resolvers with five lookups per domain, run:

```bash
./bin/dnsbench -major -n 5 -c 8 -warmup 2 -log disabled -output table
```

To test every built-in resolver with more repeats and a longer timeout, run:

```bash
./bin/dnsbench -n 20 -t 5s
```

The built-in resolvers use IPv4 by default. To test their IPv6 addresses, or
both families in one run, pass `-family ipv6` or `-family all`:

```bash
./bin/dnsbench -major -family all -output table
```

If your host has no IPv6 route, dnsbench marks each IPv6 resolver as failed at
once instead of retrying it.

The built-in resolver and domain lists are in [`data.go`](data.go). Every flag,
its default, and every report field is in the [CLI reference](docs/cli.md).

## Use your own resolvers and domains

Write `resolvers.txt` with one `name;ip` pair per line. Addresses can be IPv4
or IPv6:

```text
Cloudflare-1;1.1.1.1
Google-v6-1;2001:4860:4860::8888
Router;fe80::1%eth0
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
overrides `-major` and `-family`. For the validation rules, see
[input files](docs/cli.md#input-files).

## Save a report

```bash
./bin/dnsbench -log disabled -output json > results.json
./bin/dnsbench -log disabled -output csv > results.csv
```

CSV writes the resolvers that answered to standard output and the ones that
failed to standard error, so the redirect above captures only the successes.
JSON puts both groups in one document, under `results` and `failures`. A
resolver with no successful lookup has `null` for `min`, `max`, and `mean`. See
[report formats](docs/cli.md#report-formats).

## Start the Web UI

```bash
./bin/dnsbench -ui -listen 127.0.0.1:8080
```

dnsbench tries to open your browser at that address. If it does not, open
<http://127.0.0.1:8080> yourself. Pick the domains, resolvers, and options, then
click **Start benchmark**. **Stop** ends a run early. **Reset** clears the
results and restores the default settings.

`-listen` defaults to `:8080`, which accepts connections from anywhere that can
reach your machine. The dashboard has no authentication, and it runs lookups
against whatever resolver addresses a request names. If you expose it beyond
your own machine, put it behind a firewall or a reverse proxy.

## Documentation

- [CLI reference](docs/cli.md): flags, lookup behavior, input files, and report formats.
- [Contributing](AGENTS.md): build commands, conventions, and what to check.

dnsbench uses the [MIT license](LICENSE).
