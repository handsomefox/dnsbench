# Contributing to dnsbench

Build commands are in the [Makefile](Makefile). Flag and report behavior is in
the [CLI reference](docs/cli.md). Go and Node versions are in
[Build](README.md#build).

## Build the Web UI first

`server.go` embeds `webui/dist/`, and that directory is gitignored. In a fresh
checkout `go build` and `go test` both fail until it exists, so run `make build`
before you reach for the Go toolchain directly.

## Run the linter yourself

```bash
go fmt ./...
go test ./...
golangci-lint run
```

`.golangci.yaml` configures the linter, and no workflow runs it. A pull request
can go green with lint failures in it.

## Go and TypeScript disagree silently

`server.go` defines the request and response structs, and `webui/src/types.ts`
repeats them by hand. The event payloads are looser still: `reporter.go` builds
each `Detail` as a `map[string]interface{}` with string-literal keys, and the
dashboard reads them back out of an untyped `Record<string, unknown>`. Rename a
key on either side and nothing fails to compile. The field just arrives
undefined. Change both files together, then check the dashboard against a live
run, following [Web UI checks](webui/README.md#check-your-changes).

## What not to change

- `-t` bounds one lookup attempt. It is not a budget for a lookup or for a run, and `docs/cli.md` documents it that way.
- The logging levels are `default`, `verbose`, and `disabled`. Keep `default` quiet enough to pipe a report into a file.
- `data.go` holds the built-in lists. Do not put internal hostnames or addresses there, or in a sample resolver file.

## Commits

One change per commit. Short imperative subject with a conventional prefix, such
as `feat: add Web UI` or `fix: handle timeouts`. In the pull request, say what
was broken and what the behavior is now, and paste the commands you ran.
Screenshots for dashboard changes.
