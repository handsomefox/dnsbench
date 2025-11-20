# Repository Guidelines

## Project Structure & Modules
- Go source lives at repo root (`main.go`, `resolver.go`, `benchmark.go`, etc.). Shared helpers in `utils.go`; server/SSE helpers in `server.go` and `sse.go`.
- Tests sit beside code (e.g., `benchmark_test.go`, `resolver_test.go`).
- CLI binary output: `bin/dnsbench` (created by Make targets).
- Web UI front-end lives in `webui/` (Vite/React). Built assets emitted to `webui/dist/` and embedded at build time.

## Build, Test, and Dev Commands
- `make build` — build Web UI, run Go tests, compile CLI to `bin/dnsbench`.
- `make build-windows` — same as build but produces `bin/dnsbench.exe` for Windows (GOOS/GOARCH set).
- `make test` — run all Go tests with verbose output.
- `make run` — build then execute CLI with defaults (`N` and `TIMEOUT` overridable: `make run N=20 TIMEOUT=2s`).
- `make run-ui` — build everything and start the embedded dashboard on `:8080`.
- Front-end only: `make ui-install`, `make ui-build`, `make ui-dev` (Vite dev server with `--host` for LAN testing).

## Coding Style & Naming
- Go 1.24+; format with `gofmt` (run `go fmt ./...` before committing). Keep imports ordered with standard `goimports` style if you use it.
- Follow standard Go naming: exported symbols in `CamelCase`; unexported in `camelCase`; tests use `TestXxx`.
* Concurrency: prefer context-aware functions; respect existing timeouts and `-t` flag semantics.
- Keep logging consistent with current levels (`default`, `verbose`, `disabled`); avoid noisy output by default.

## Testing Guidelines
- Primary suite: `go test ./...` (already invoked by `make build`). Add focused tests next to implementations (`foo_test.go`) using table-driven cases.
- When introducing new resolver behaviors or parsers, include latency/error path coverage and SSE/JSON formatting checks where applicable.
- For Web UI changes, rely on Vite’s dev server; add minimal smoke tests if you introduce new Go HTTP handlers.

## Commit & PR Guidelines
- Match existing history: short, imperative, conventional-style prefixes (e.g., `feat: add WebUI`, `fix: handle timeouts`).
- Commit scope should stay focused; prefer multiple small commits over one large unrelated change.
- PRs should include: purpose/summary, key commands run (`make test`, `make build`), screenshots or GIFs for UI changes, and links to any related issues.
- Note any flags or env vars needed to reproduce (e.g., `N`, `TIMEOUT`, `RESFILE`).

## Security & Configuration Tips
- Avoid committing resolver/domain lists containing sensitive data. Use sample files instead.
- Networked features listen on `:8080` by default; override `-listen` for non-local use and place behind a firewall/reverse proxy if exposed.
- Keep third-party JS deps in `webui` pinned via `package-lock.json`; run `npm audit` after updates.
