# CLI reference

dnsbench benchmarks every resolver in the same run, with the lookups
interleaved. Each of the `-n` rounds visits the domains in a new random order,
and for each domain the resolvers in a new random order. Up to `-c` lookups run
at once, across all resolvers. A slow patch on your network therefore spreads
over every resolver instead of landing on whichever ran at the time, and no
resolver always asks second for a domain that another just put in a shared
cache. A lookup is one recursive
query for the domain's A records, and it succeeds when the answer holds at
least one. A plain DNS resolver gets its queries over UDP port 53, and over TCP
when a UDP answer arrives truncated. A DNS over TLS (DoT) resolver gets them
over TLS on TCP port 853. A DNS over HTTPS (DoH) resolver gets them as HTTPS
POST requests on TCP port 443. A DNS over QUIC (DoQ) resolver gets them over
QUIC on UDP port 853. The built-in resolver and domain lists are in
[`data.go`](../data.go).

dnsbench builds each query itself and sends it straight to the resolver. The
host's `/etc/hosts`, its search domains, and its `resolv.conf` options play no
part, and every name is queried as an absolute name. Each query advertises an
EDNS UDP size of 1232 bytes, so answers rarely need the TCP retry.

## Flags

`./bin/dnsbench -h` prints the flags and defaults of the binary you built.

| Flag | Default | Meaning |
| --- | --- | --- |
| `-f string` | Empty | Resolver file that replaces the built-in list |
| `-s string` | Empty | Domain file that replaces the built-in list |
| `-n int` | `10` | Measured lookups per domain for each resolver. Minimum `1`. |
| `-t duration` | `3s` | Timeout for one lookup attempt. Minimum `100ms`. Takes Go durations such as `1500ms` and `2s`. |
| `-retries int` | `2` | Retries of a failed lookup attempt. `0` disables them. |
| `-c int` | `max(runtime.NumCPU()/2, 2)` | Maximum lookups in flight at once, across all resolvers. Minimum `1`. |
| `-output string` | `default` | Report format: `default`, `csv`, `table`, or `json` |
| `-log string` | `default` | Logging level: `default`, `verbose`, or `disabled` |
| `-major` | `false` | Uses only the major providers from the built-in list. `-f` overrides it. |
| `-primary` | `false` | Uses only the first address of each built-in provider, such as `Cloudflare-1` and `Cloudflare-v6-1`. `-f` overrides it. |
| `-family string` | `ipv4` | Address family of the built-in resolvers: `ipv4`, `ipv6`, or `all`. `-f` overrides it. |
| `-proto string` | `plain` | Transport of the built-in resolvers: `plain`, `dot`, `doh`, `doq`, or `all`. `-f` overrides it. |
| `-warmup int` | `0` | Unmeasured lookups of a domain right before a resolver's first measured lookup of it. Zero or less disables warmup. |
| `-ui` | `false` | Serves the Web UI instead of running a CLI benchmark |
| `-listen string` | `:8080` | Web UI listen address. The default accepts connections on every interface. |

The flag parser takes one or two leading hyphens. The values of `-output`,
`-log`, `-family`, and `-proto` are case-insensitive. dnsbench exits `1` with a
message on standard error when a value is out of range or a name is unknown.

## Lookup behavior

`-t` bounds one attempt, not a lookup and not a run. A measured lookup makes
one attempt and, when it fails, up to `-retries` more. Between attempts it waits
between 125 and 375 milliseconds, then between 250 and 750, and never more than
a second and a half. An answer that the name does not exist (NXDOMAIN), or that
it has no A record, is final, so dnsbench does not retry it.

A lookup that answered only after a retry still counts as a success. The
reports count those lookups separately as `retried`, and the dashboard
underlines the success rate of a resolver that has any, so a resolver that
drops queries does not pass for a reliable one.

Before it benchmarks a resolver, dnsbench checks for two failures that no retry
can fix:

- It connects a UDP socket to the resolver. That sends nothing, but it fails at
  once when the host has no route to the address, as with an IPv6 resolver on
  an IPv4-only host.
- For a DoT, DoH, or DoQ resolver, it makes one TLS handshake and checks the
  certificate against the TLS name or the host in the DoH URL. For DoQ, a TLS
  alert from the server during the handshake counts too, since some servers
  send one instead of a certificate for a name they do not serve. Other
  handshake failures, such as a timeout, are left to the lookups and their
  retries.

If either check fails, dnsbench logs a warning and counts every planned lookup
against that resolver as failed, without retries.

A resolver can pass both checks and still never answer, such as one behind a
firewall that drops its queries. When 8 lookups in a row against one resolver
fail, dnsbench gives up on it. It cuts that resolver's lookups in flight short
and fails the rest of its planned lookups at once, with an error that says it
gave up. An answer that the name does not exist, or that it has no A record,
does not count toward the 8, and any successful lookup starts the count over.

The reported latency covers the successful attempt alone. It leaves out the
earlier attempts, the waits between them, and the time the lookup spent waiting
for a concurrency slot.

Warmup runs in the first round. Before a resolver's first measured lookup of a
domain, the same job sends that resolver `-warmup` lookups of the same domain,
one after another. `-warmup 2` therefore sends two lookups of each domain to
each resolver whatever `-n` is, so 54 domains mean 108 warmup lookups per
resolver. They put the answer in the resolver's cache and open the connection
that DoT, DoH, and DoQ reuse. Warmup lookups use a one-second timeout, do not
retry, and never reach the statistics.

### DNS over TLS

A DoT resolver must present a certificate that your system trusts and that
matches its TLS name. If it does not, the pre-check above fails the resolver.

dnsbench keeps DoT connections open between lookups and sends one query at a
time on each, as systemd-resolved and Android's private DNS do. Only the first
lookups against a DoT resolver pay for the TCP and TLS handshakes, so DoT
latency mostly measures a warm connection, like DoH and DoQ. When a server
closes a connection that dnsbench kept, the next lookup opens a fresh one
within the same attempt, and the handshake counts toward that lookup.

### DNS over HTTPS

dnsbench sends each query to the DoH URL as an HTTPS POST with the
`application/dns-message` body that RFC 8484 describes. It connects to the
resolver's address, not to whatever the URL's host resolves to, and sends the
host in the URL as the TLS server name and the HTTP `Host`.

The HTTP client keeps its connections open between lookups and uses HTTP/2
where the server offers it. Only the first lookups against a DoH resolver pay
for the TCP and TLS handshakes, so DoH latency mostly measures a warm
connection.

### DNS over QUIC

dnsbench follows RFC 9250. It opens one QUIC connection to the resolver on UDP
port 853 with the ALPN protocol `doq`, and sends each query on a new stream of
that connection with a two-byte length prefix. The DNS message ID on the wire
is `0`, as the RFC requires.

Like DoT and DoH, DoQ keeps its connection for the whole resolver, so after
the first lookups it measures a warm connection. QUIC's handshake takes one
round trip, so a cold DoQ lookup costs about two round trips, where a cold DoT
lookup costs three. Few providers serve DoQ: of the built-in list, AdGuard, NextDNS,
Quad9, and AliDNS do.

Concurrent lookups share that one connection, and some servers answer them
more slowly than one at a time. If DoQ numbers look high, run the same
resolvers again with `-c 1` and compare.

## Input files

Both formats trim whitespace, then skip blank lines and lines that start with
`#`.

### Resolver file

Each line is `name;ip` for plain DNS, `name;ip;tls-name` for DoT,
`name;ip;https-url` for DoH, or `name;ip;quic://tls-name` for DoQ. A third
field that starts with `https://` is a DoH URL. One that starts with `quic://`
is a DoQ TLS name. Any other third field is a DoT TLS name:

```text
Cloudflare;1.1.1.1
Cloudflare-DoT;1.1.1.1;cloudflare-dns.com
Cloudflare-DoH;1.1.1.1;https://cloudflare-dns.com/dns-query
AdGuard-DoQ;94.140.14.14;quic://dns.adguard-dns.com
Router;fe80::1%eth0
```

The name and address must be nonempty, and the address must be an IPv4 or IPv6
literal with no port. A link-local IPv6 address needs its zone, as in
`fe80::1%eth0`. The TLS name is the name on the resolver's certificate. A DoH
URL must use `https` and name a host. One bad
line stops the benchmark with an error naming the line number. A file with no
valid resolvers is an error too.

A resolver file replaces the built-in list and runs as written. `-major`,
`-family`, and `-proto` do not filter it.

### Domain file

Each line holds one domain. The loader rejects a name longer than 253 bytes, a
name containing a space, a name without a dot, and a name that starts or ends
with a dot. Rejected lines produce a warning and are skipped, so a typo costs
you one domain rather than the run. A file with no valid domains is an error.

## Report formats

Resolvers that answered sort by descending success rate, then by ascending mean
latency. Resolvers with no successful lookup form a separate failed group. All latencies are milliseconds and count successful lookups only.

| Format | Output |
| --- | --- |
| `default` | Tables and a summary on standard output |
| `table` | Successful resolvers on standard output, failed resolvers on standard error |
| `csv` | Successful resolvers as CSV on standard output, a heading and a second CSV for failed resolvers on standard error |
| `json` | One report object on standard output with `summary`, `results`, and `failures` |

### JSON fields

Each entry in `results` and `failures` has these fields:

| Field | Meaning |
| --- | --- |
| `server.name` | Resolver name |
| `server.addr` | Resolver IPv4 or IPv6 address |
| `server.tlsName` | Certificate name of a DoT resolver. Absent otherwise. |
| `server.dohURL` | URL of a DoH resolver. Absent otherwise. |
| `server.doqName` | Certificate name of a DoQ resolver. Absent otherwise. |
| `stats.min` | Fastest successful lookup, in milliseconds. `null` when no lookup succeeded. |
| `stats.max` | Slowest successful lookup, in milliseconds. `null` when no lookup succeeded. |
| `stats.mean` | Mean successful lookup, in milliseconds. `null` when no lookup succeeded. |
| `stats.count` | Successful measured lookups |
| `stats.errors` | Measured lookups that failed, after any retries |
| `stats.total` | Planned measured lookups, the domain count multiplied by `-n` |
| `stats.retried` | Successful lookups that needed more than one attempt |

The `summary` object has these fields:

| Field | Meaning |
| --- | --- |
| `total_resolvers` | Resolvers in both groups |
| `success_resolvers` | Resolvers with at least one successful lookup |
| `failed_resolvers` | Resolvers with no successful lookup |
| `overall_success_rate` | Success percentage from `0` to `100`, counted across `results` only |
| `fastest_resolver` | The first result object in the sorted `results` group |
| `slowest_resolver` | The last result object in the sorted `results` group |

`fastest_resolver` and `slowest_resolver` follow the sort order, which puts
success rate ahead of latency. A resolver that answered every lookup slowly
therefore outranks one that answered most of them fast, and neither field is a
reliable answer to "which resolver has the lowest mean latency". Read
`results` yourself if that is the question. The encoder drops both fields when
no valid result exists. An empty group encodes as `[]`.

The report carries no per-domain statistics.

For the commands that produce these reports, see
[Save a report](../README.md#save-a-report).
