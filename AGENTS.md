# Contributing to dnsbench

Build commands are in the [Makefile](Makefile). Flag and report behavior is in
the [CLI reference](docs/cli.md). The Go version is in [Build from source](README.md#build-from-source).

## Checks

```bash
go fmt ./...
go test -race ./...
golangci-lint run ./...
node --check ui/static/app.js
```

`make test` and `make lint` run the first three. `.golangci.yaml` configures the
Go linter. CI runs all four on every push to `main` and on every pull request,
and a failure blocks the merge. Node is only a syntax check for the dashboard
script. Nothing in the repository installs npm packages.

## Layout

`main.go` and `cli.go` in the root parse the flags and wire the packages
together. They stay in the root so that `go install
github.com/handsomefox/dnsbench@latest` builds the binary. Everything else is
under `internal/`:

| Package | Holds |
| --- | --- |
| `internal/dnsclient` | `Server`, and the lookup over plain DNS, DoT, DoH, and DoQ with retries |
| `internal/bench` | `Run`: the interleaved run, warmup, the give-up rule, and `Stats` |
| `internal/catalog` | The built-in resolvers and domains in `data.go`, the filters, and the file loaders |
| `internal/report` | The table, CSV, and JSON reports |
| `internal/web` | The dashboard server, its API, and the SSE stream |
| `internal/dnstest` | Fake DNS, DoT, DoH, and DoQ servers for tests |

`ui/` is the dashboard page, script, stylesheet, and fonts, embedded by
`ui/embed.go`. Imports run one way. `dnsclient` imports none of the others.
`bench` and `catalog` import only `dnsclient`, and `report` imports only
`bench`. `web` imports `bench`, `catalog`, `dnsclient`, and `ui`. Only `main`
imports `web` and `report`.

## Go and the dashboard script disagree silently

The Web UI is `ui/index.html.tmpl`, `ui/static/app.js`, and
`ui/static/style.css`. `internal/web/server.go` serves them and defines the
request structs that `app.js` sends. `internal/web/reporter.go` builds each
event `Detail` as a
`map[string]any` with string-literal keys, and `app.js` reads those keys back
by name. Rename a key on either side and nothing fails to compile or lint. The
field just arrives `undefined`. Change both files together, then check the
dashboard against a live run:

1. Run `make run-ui` and open <http://127.0.0.1:8080>.
2. Confirm that the presets, the domain list, and the built-in resolvers load,
   and that the filters and the search change the resolver list.
3. Clear one resolver's checkbox, then start a benchmark. Confirm that the
   ladder fills in without that resolver, and that clicking a row opens its
   slowest domains and errors.
4. Reload the page. Confirm that the setup is the one you left.
5. Stop the run. Confirm that the status reads `stopped` and **Start
   benchmark** is enabled again.
6. Click **Reset**. Confirm that the results clear and the form shows the
   defaults again.
7. Check the page with the system in dark mode and at phone width.

`app.js` builds every element with `textContent`. Resolver names and error
strings come from the user, so do not switch to `innerHTML`.

The dashboard filters the resolver catalog from `catalog.All` itself and sends
`/api/run` an explicit resolver list, so the list you see is the list that
runs. The CLI filters the same catalog through `catalog.Servers`.

## What not to change

- `-t` bounds one lookup attempt. It is not a budget for a lookup or for a run, and `docs/cli.md` documents it that way.
- The logging levels are `default`, `verbose`, and `disabled`. Keep `default` quiet enough to pipe a report into a file.
- `internal/catalog/data.go` holds the built-in lists. Do not put internal hostnames or addresses there, or in a sample resolver file. Before and after you change it, run `DNSBENCH_LIVE=1 go test ./internal/catalog -run TestBuiltinServersAnswer -v` from a host with IPv6. It sends one lookup to every built-in resolver on every transport.

## Commits

One change per commit. Short imperative subject with a conventional prefix, such
as `feat: add Web UI` or `fix: handle timeouts`. In the pull request, say what
was broken and what the behavior is now, and paste the commands you ran.
Screenshots for dashboard changes.
