# Repository Guidelines

## Project Structure & Module Organization
- `main.go` and `cli.go` wire the CLI flags, default lists, and kickoff of benchmarks; `benchmark.go` contains the core runner.
- Resolver definitions and metadata live in `data.go`; DNS resolution helpers are in `resolver.go` with shared helpers in `utils.go`.
- Tests reside alongside code (`benchmark_test.go`, `resolver_test.go`); build artifacts go to `bin/dnsbench*` created by Make targets.

## Build, Test, and Development Commands
- `make test` — run the Go test suite with verbosity across `./...`.
- `make build` — run tests, then build the Linux/host binary at `bin/dnsbench` with `-ldflags '-w -s' -tags netgo`.
- `make build-windows` — same as build, targeting `windows/amd64` and outputting `bin/dnsbench.exe`.
- `make run [N=10 TIMEOUT=3s RESFILE=-f file]` — build then execute the tool with tweakable defaults for quick local checks.
- Fast edit loop: `go test ./...` and `go fmt ./...` (or `gofmt -w *.go`) before pushing.

## Coding Style & Naming Conventions
- Go 1.24.x; format with `gofmt` (required) and keep imports grouped standard/third-party/local.
- Prefer table-driven tests; name test functions `TestXxx`. Keep exported identifiers idiomatic (CamelCase) and concise flag names.
- Avoid global state—pass context and configuration structs through functions where possible.

## Testing Guidelines
- Unit tests use the standard `testing` package; keep fixtures minimal and deterministic.
- When adding benchmarks, place them in `*_test.go` using `BenchmarkXxx`.
- Run `go test ./...` before commits; add `-run`/`-bench` filters as needed during development.

## Commit & Pull Request Guidelines
- Follow the existing history style: short, imperative summaries (e.g., “Add resolver warmup”, “Fix timeout parsing”).
- Ensure commits are small and scoped; include rationale in the body if behavior changes.
- PRs should describe the change, mention user-facing flags or defaults touched (`-n`, `-t`, `-output`, `--warmup`), and note test coverage (`make test` output). Screenshots are optional since this is a CLI.

## Security & Configuration Tips
- Resolver lists (`-f`) and domain lists (`-s`) may contain sensitive infrastructure; avoid committing private files or sample data with internal hosts.
- Validate custom resolver files use `name;ip` lines and reside outside `bin/` to keep build artifacts clean.
