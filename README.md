# dnsbench

[![CI](https://github.com/handsomefox/dnsbench/actions/workflows/ci.yml/badge.svg)](https://github.com/handsomefox/dnsbench/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

dnsbench measures how fast and how reliably DNS resolvers answer from your
machine. It asks every resolver for the same list of domains, taking turns
between them in random order, over plain DNS, DNS over TLS, DNS over HTTPS, or
DNS over QUIC, on IPv4 or IPv6. It reports latency and success rate as text, a
table, CSV, or JSON, and an embedded Web UI shows a run while it happens.

## Install

Download the archive for your system from the
[latest release](https://github.com/handsomefox/dnsbench/releases/latest) and
extract it, or install with Go 1.27.1 or later:

```bash
go install github.com/handsomefox/dnsbench@latest
```

The examples below run `dnsbench`. From an extracted archive, run
`./dnsbench`. For a phone, see [Run on Android](#run-on-android).

## Quick start

To pick resolvers and watch the results come in, start the dashboard:

```bash
dnsbench -ui
```

To benchmark one address of each major provider from the terminal instead, run:

```bash
dnsbench -major -primary
```

## Use the dashboard

`dnsbench -ui` serves the dashboard at <http://127.0.0.1:8080> and tries to
open it in your browser. If the browser does not open, go to that address
yourself.

Pick a preset such as **Quick check** or **Plain vs encrypted**, or build a
selection yourself:

- Filter the list by address family, transport, and kind of resolver, and
  search it.
- The list groups resolvers by the company that runs them: major providers
  first, then the rest, each in alphabetical order. Tick a company to take all
  its services, such as Cloudflare with Cloudflare-Security and
  Cloudflare-Family. Open it to tick one service or one address.
- The filters only choose what the list shows. A tick holds on every transport
  and family: after **None**, turning on IPv6 or DoT picks nothing new, and a
  company you ticked brings its IPv6 and DoT resolvers along. To compare
  Cloudflare, Google, and Quad9 with their filters, click **None**, then tick
  those three.
- Resolvers you add under **Add your own resolvers** join the selection.
  **Edit list** opens the domain list.

The estimate under the setup shows how many lookups the run makes. Click
**Start benchmark**, or press Ctrl+Enter. **Stop** ends a run early, and
**Reset** clears the results and restores the default setup. The dashboard
remembers your setup between visits.

Each resolver gets one row, with every lookup plotted as a dot on a log-scale
millisecond axis. The dot color is the transport: plain DNS, DoT, DoH, or DoQ.
The bar marks the median, the caret marks the 95th percentile, and the red
gutter shows the share of failed lookups, so a resolver that is fast on
average but erratic stands out. Click a row for its slowest domains and its
errors. **Download CSV** and **Download JSON** save the table.

`-listen` defaults to `127.0.0.1:8080`, so only your own machine can reach the
dashboard. It has no authentication, and it runs lookups against whatever
resolver addresses a request names. To open it to other machines, pass
`-listen :8080`, and put it behind a firewall or a reverse proxy.

## Run from the command line

To test the major built-in resolvers with five lookups per domain, run:

```bash
dnsbench -major -n 5 -warmup 2
```

To test every built-in resolver with more repeats and a longer timeout, run:

```bash
dnsbench -n 20 -t 5s
```

Most providers list two addresses that perform alike. To halve a run, pass
`-primary` to test only the first, such as `Cloudflare-1`. To see which
resolvers a set of flags selects without running anything, add `-list`:

```bash
dnsbench -major -proto doq -list
```

The built-in resolver and domain lists are in
[`internal/catalog/data.go`](internal/catalog/data.go). Every flag, its
default, and every report field is in the [CLI reference](docs/cli.md).

### Test IPv6 and encrypted DNS

The built-in resolvers run over plain DNS on IPv4 by default. `-family` picks
the address family: `ipv4`, `ipv6`, or `all`. `-proto` picks the transport:
`plain`, `dot` for DNS over TLS, `doh` for DNS over HTTPS, `doq` for DNS over
QUIC, or `all`. To compare everything the major providers offer, one address
each, run:

```bash
dnsbench -major -primary -family all -proto all
```

The resolvers get names like `Cloudflare-v6-1`, `Cloudflare-DoT-1`,
`Cloudflare-DoH-v6-1`, and `AdGuard-DoQ-1`. Only AdGuard, NextDNS, Quad9, and
AliDNS serve DoQ. If your host has no IPv6 route, dnsbench marks each IPv6
resolver as failed at once instead of retrying it.

DoT, DoH, and DoQ all keep their connection open, so after the first lookups
they measure a warm connection, as a phone or a system resolver would. Every
lookup is one query for the domain's A records, sent straight to the resolver.
For details, see [DNS over TLS](docs/cli.md#dns-over-tls),
[DNS over HTTPS](docs/cli.md#dns-over-https), and
[DNS over QUIC](docs/cli.md#dns-over-quic).

### Test filtering resolvers

Many providers run filtering addresses next to their plain ones. Cloudflare's
1.1.1.2 blocks malware, and 1.1.1.3 blocks adult content as well. AdGuard,
CleanBrowsing, OpenDNS, DNS4EU, Canadian Shield, ControlD, and Mullvad offer
family or ad filters, and Quad9's main 9.9.9.9 blocks malware. `-kind
filtering` benchmarks only the filters. To compare one address of each over
plain DNS, run:

```bash
dnsbench -kind filtering -primary
```

To compare a few companies with all their services, filtering or not, name
them with `-provider`:

```bash
dnsbench -provider cloudflare,google,quad9 -primary
```

A filter answers a name it blocks with no address or with an address such as
`0.0.0.0`. Either way the lookup counts as answered. The reports count the
first kind as `blocked`, so a family filter that blocks `reddit.com` shows it
there instead of as a failure. In the dashboard, the **Filtering DNS** preset
and the **Filtering** kind select them.

### Use your own resolvers and domains

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
dnsbench -f resolvers.txt -s domains.txt
```

Each file replaces the matching built-in list instead of adding to it. The
resolver filters, such as `-major`, `-family`, and `-proto`, do not apply to a
resolver file. For the validation rules, see
[input files](docs/cli.md#input-files).

### Save a report

```bash
dnsbench -log disabled -output json > results.json
dnsbench -log disabled -output csv > results.csv
```

Both reports go to standard output and hold every resolver. The CSV lists the
resolvers with no answer last, with empty latency cells. JSON puts the two
groups under `results` and `failures`, and a resolver with no successful lookup
has `null` for every latency field. See
[report formats](docs/cli.md#report-formats).

## Run on Android

In [Termux](https://termux.dev), download the `Android_arm64` archive and start
the dashboard:

```bash
curl -fLO https://github.com/handsomefox/dnsbench/releases/latest/download/dnsbench_Android_arm64.tar.gz
tar -xzf dnsbench_Android_arm64.tar.gz
./dnsbench -ui
```

The dashboard opens in the Android browser through `termux-open-url`, which
the `termux-tools` package provides. If it does not open, go to
<http://127.0.0.1:8080>. Use the Android build, not `Linux_arm64`. Termux on
Android 10 and later refuses the Linux build with `has unexpected e_type: 2`,
and only the Android build finds Android's CA certificates.

## Build from source

You need Go 1.27.1 or later.

```bash
git clone https://github.com/handsomefox/dnsbench.git
cd dnsbench
make build
```

`make build` runs the tests and writes `bin/dnsbench`. Plain `go build` works
too. The Web UI is a Go template, a stylesheet, one script, and two font files
in `ui/`. The binary embeds all of them, so the build needs no Node.js.

## Documentation

- [CLI reference](docs/cli.md): flags, lookup behavior, input files, and report formats.
- [Contributing](AGENTS.md): checks, the dashboard, and conventions.

dnsbench uses the [MIT license](LICENSE).
