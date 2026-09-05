# CLI reference

dnsbench benchmarks one resolver at a time. For each resolver it runs the
hostname lookups concurrently, up to `-c` at once, over UDP port 53. The
built-in resolver and domain lists are in [`data.go`](../data.go).

## Flags

`./bin/dnsbench -h` prints the flags and defaults of the binary you built.

| Flag | Default | Meaning |
| --- | --- | --- |
| `-f string` | Empty | Resolver file that replaces the built-in list |
| `-s string` | Empty | Domain file that replaces the built-in list |
| `-n int` | `10` | Measured lookups per domain for each resolver. Minimum `1`. |
| `-t duration` | `3s` | Timeout for one lookup attempt. Minimum `100ms`. Takes Go durations such as `1500ms` and `2s`. |
| `-c int` | `max(runtime.NumCPU()/2, 2)` | Maximum concurrent lookups against the current resolver. Minimum `1`. |
| `-output string` | `default` | Report format: `default`, `csv`, `table`, or `json` |
| `-log string` | `default` | Logging level: `default`, `verbose`, or `disabled` |
| `-major` | `false` | Uses the built-in major resolver list. `-f` overrides it. |
| `-warmup int` | `0` | Warmup lookups before each measured lookup. Zero or less disables warmup. |
| `-ui` | `false` | Serves the Web UI instead of running a CLI benchmark |
| `-listen string` | `:8080` | Web UI listen address. The default accepts connections on every interface. |

The flag parser takes one or two leading hyphens. The report format and logging
level values are case-insensitive. dnsbench exits `1` with a message on standard
error when a value is out of range or a format name is unknown.

## Lookup behavior

`-t` bounds one attempt, not a lookup and not a run. A measured lookup makes up
to ten attempts and waits between failed ones. That wait comes from a base delay
that doubles from two seconds up to a sixty-second ceiling, plus random jitter.
A resolver that keeps failing can therefore hold a run open for minutes.

The reported latency covers the successful attempt alone. It leaves out the
earlier attempts, the backoff, and the time the lookup spent waiting for a
concurrency slot.

Warmup runs before every measured lookup, including each repeat, so `-warmup 2`
with `-n 10` means twenty warmup lookups per domain. Warmup lookups use a
one-second timeout, do not retry, and never reach the statistics.

## Input files

Both formats trim whitespace, then skip blank lines and lines that start with
`#`.

### Resolver file

Each line is `name;ip`. Both halves must be nonempty, and the address must be an
IPv4 or IPv6 literal with no port. One bad line stops the benchmark with an
error naming the line number. A file with no valid resolvers is an error too.

A resolver file replaces the built-in list even when `-major` is set.

### Domain file

Each line holds one domain. The loader rejects a name longer than 253 bytes, a
name containing a space, a name without a dot, and a name that starts or ends
with a dot. Rejected lines produce a warning and are skipped, so a typo costs
you one domain rather than the run. A file with no valid domains is an error.

## Report formats

Resolvers that answered sort by descending success rate, then by ascending mean
latency. Resolvers with no valid latency statistics form a separate failed
group. All latencies are milliseconds and count successful lookups only.

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
| `server.addr` | Resolver IP address |
| `stats.min` | Fastest successful lookup, in milliseconds |
| `stats.max` | Slowest successful lookup, in milliseconds |
| `stats.mean` | Mean successful lookup, in milliseconds |
| `stats.count` | Successful measured lookups |
| `stats.errors` | Measured lookups that failed after all retries |
| `stats.total` | Planned measured lookups, the domain count multiplied by `-n` |

The `summary` object has these fields:

| Field | Meaning |
| --- | --- |
| `total_resolvers` | Resolvers in both groups |
| `success_resolvers` | Resolvers with valid latency statistics |
| `failed_resolvers` | Resolvers without valid latency statistics |
| `overall_success_rate` | Success percentage from `0` to `100`, counted across `results` only |
| `fastest_resolver` | The first result object in the sorted `results` group |
| `slowest_resolver` | The last result object in the sorted `results` group |

`fastest_resolver` and `slowest_resolver` follow the sort order, which puts
success rate ahead of latency. A resolver that answered every lookup slowly
therefore outranks one that answered most of them fast, and neither field is a
reliable answer to "which resolver has the lowest mean latency". Read
`results` yourself if that is the question. The encoder drops both fields when
no valid result exists, and an empty result group encodes as `null`.

The report carries no per-domain statistics.

A resolver with no successful lookup gets `NaN` for `min`, `max`, and `mean`.
`encoding/json` refuses `NaN`, so dnsbench writes `failed to encode json
results` to standard error and produces no report at all. It still exits `0`,
which means a script that only checks the exit status sees a successful run with
empty output.

For the commands that produce these reports, see
[Save a report](../README.md#save-a-report).
