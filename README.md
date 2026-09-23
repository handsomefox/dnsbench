# dnsbench

[![CI](https://github.com/handsomefox/dnsbench/actions/workflows/ci.yml/badge.svg)](https://github.com/handsomefox/dnsbench/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

dnsbench measures how fast and how reliably DNS resolvers answer from your
machine. It benchmarks one resolver at a time against a list of domains, over
plain DNS or DNS over TLS, on IPv4 or IPv6. It reports latency and success rate
as text, a table, CSV, or JSON, and an embedded Web UI shows a run while it
happens.

## Install

Download an archive for your system from the
[latest release](https://github.com/handsomefox/dnsbench/releases/latest), or
install with Go 1.27.1 or later:

```bash
go install github.com/handsomefox/dnsbench@latest
```

The examples below run `./bin/dnsbench` from a checkout. After `go install`,
run `dnsbench` instead.

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

To test the major built-in resolvers with five lookups per domain, run:

```bash
./bin/dnsbench -major -n 5 -c 8 -warmup 2 -log disabled -output table
```

To test every built-in resolver with more repeats and a longer timeout, run:

```bash
./bin/dnsbench -n 20 -t 5s
```

Most providers list two addresses that perform alike. To halve a run, pass
`-primary` to test only the first, such as `Cloudflare-1`. The dashboard has
the same choice as **First address only**.

The built-in resolver and domain lists are in [`data.go`](data.go). Every flag,
its default, and every report field is in the [CLI reference](docs/cli.md).

## Test IPv6 and encrypted DNS

The built-in resolvers run over plain DNS on IPv4 by default. `-family` picks
the address family: `ipv4`, `ipv6`, or `all`. `-proto` picks the transport:
`plain`, `dot` for DNS over TLS, `doh` for DNS over HTTPS, `doq` for DNS over
QUIC, or `all`. To compare everything the major providers
offer, one address each, run:

```bash
./bin/dnsbench -major -primary -family all -proto all -output table
```

The resolvers get names like `Cloudflare-v6-1`, `Cloudflare-DoT-1`,
`Cloudflare-DoH-v6-1`, and `AdGuard-DoQ-1`. Only AdGuard, NextDNS, Quad9, and
AliDNS serve DoQ. If your host has no IPv6 route, dnsbench marks each IPv6
resolver as failed at once instead of retrying it.

The encrypted transports measure different things. Each DoT lookup opens a new
TLS connection and pays for the handshakes. DoH and DoQ keep their connection
open, so after the first lookups they measure a warm connection. Compare
resolvers within one transport. For details, see
[DNS over TLS](docs/cli.md#dns-over-tls),
[DNS over HTTPS](docs/cli.md#dns-over-https), and
[DNS over QUIC](docs/cli.md#dns-over-quic).

## Use your own resolvers and domains

Write `resolvers.txt` with one `name;ip` pair per line. Addresses can be IPv4 or
IPv6. For DNS over TLS, add a third field with the name on the resolver's
certificate. For DNS over HTTPS, make the third field the DoH URL. For DNS over
QUIC, make it `quic://` and the certificate name:

```text
Cloudflare-1;1.1.1.1
Google-v6-1;2001:4860:4860::8888
Quad9-DoT;9.9.9.9;dns.quad9.net
Quad9-DoH;9.9.9.9;https://dns.quad9.net/dns-query
Quad9-DoQ;9.9.9.9;quic://dns.quad9.net
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

Each file replaces the matching built-in list instead of adding to it. `-major`,
`-family`, and `-proto` do not filter a resolver file. For the validation rules,
see [input files](docs/cli.md#input-files).

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
<http://127.0.0.1:8080> yourself. Pick the domains, the resolvers, and the
options, then click **Start benchmark**. **Stop** ends a run early. **Reset**
clears the results and restores the default settings.

Each resolver gets one row, with every lookup plotted as a dot on a log-scale
millisecond axis. The bar marks the median, the caret marks the 95th
percentile, and the red gutter shows the share of failed lookups, so a
resolver that is fast on average but erratic stands out. Click a row for its
slowest domains and its errors. **Download CSV** and **Download JSON** save the
table with the median and 95th percentile, which the CLI reports do not
include.

`-listen` defaults to `:8080`, which accepts connections from anywhere that can
reach your machine. The dashboard has no authentication, and it runs lookups
against whatever resolver addresses a request names. If you expose it beyond
your own machine, put it behind a firewall or a reverse proxy.

## Documentation

- [CLI reference](docs/cli.md): flags, lookup behavior, input files, and report formats.
- [Contributing](AGENTS.md): checks, the dashboard, and conventions.

dnsbench uses the [MIT license](LICENSE).
