# laurus

Canvas LMS client for university students, shipped as one Go binary with three surfaces: a
Cobra CLI (`laurus next`, `laurus grades`, `laurus submit`), a lazygit-style Bubble Tea TUI, and
an MCP server for AI assistants. Works against any Canvas deployment. Laurus is Latin for
laurel. Released v0.1.0 on Homebrew and Scoop.

## Stack

Go 1.26 · cobra (CLI) · Bubble Tea v2, Lip Gloss v2, Bubbles v2, huh, bubblezone (TUI) ·
mcp-go with `server.ServeStdio` (MCP) · hashicorp/go-retryablehttp, peterhellberg/link and
x/time/rate (HTTP) · modernc.org/sqlite, pure Go, no CGO, WAL mode (cache) · 99designs/keyring
(tokens) · pelletier/go-toml/v2 (config) · hasura/go-graphql-client (no codegen) ·
html-to-markdown/v2 into glamour (HTML rendering) · shopspring/decimal (grades) · GoReleaser
and GitHub Actions.

## Commands

- `make build` (injects version, commit and date) · `make run ARGS=next` · `make test` ·
  `make lint` · `make clean` · `make install`
- Raw equivalents: `go build ./...` · `go test ./... -v -race` · `golangci-lint run` · `go vet ./...`
- Before pushing: `go build ./... && go test ./... -race && golangci-lint run` must pass.
- On Windows, Go is at `C:\Program Files\Go\bin` and is not on the bash PATH by default:
  `export PATH="$PATH:/c/Program Files/Go/bin"`.
- Integration tests live in `test/integration/` and need a real Canvas API token.

## Rules with a decision or a constraint behind them

- **Layer boundaries are hard.** `pkg/tui/` and `pkg/cmd/` never import each other, and
  `pkg/mcp/` imports neither. All three surfaces depend on `internal/` only. This is what keeps
  a second surface from inheriting the first one's command plumbing.
- GraphQL (`POST /api/graphql`) for read-heavy bulk fetches; REST for writes, uploads,
  announcements, and pagination-heavy flows. Use GraphQL where it actually helps, not reflexively.
- Bearer token in the `Authorization` header only — never as a query parameter. Tokens live in
  the OS keychain, never in plaintext config. Student tokens expire within 120 days, are
  unscoped, and all API activity is visible to institution admins.
- Treat pagination `Link` header URLs as opaque. Never construct a page URL yourself.
- Watch `X-Rate-Limit-Remaining` and back off on 429.
- Canvas error shapes are inconsistent: check for an `errors` array, an `error` string, and a
  `message` string before giving up on a response.
- Canvas timestamps are UTC ISO 8601. Convert to the user's timezone from their profile for
  display.
- File uploads are a three-step flow (preflight, upload to S3/InstFS, confirm) and the `file`
  field must be **last** in the multipart body.
- The grade calculator must match Canvas's algorithm exactly, including Kane & Kane bisection
  for drop-lowest and arbitrary-precision math. Details and edge cases in docs/CANVAS-GOTCHAS.md.
- The SQLite cache is shared first-class infrastructure, not a CLI-local optimisation.
- Table-driven tests, `*_test.go` next to the source.
- Commit directly to `main`; this repo does not use worktrees.

## Where things live

- docs/CANVAS-GOTCHAS.md — Canvas behaviours that have already broken Laurus, plus the grade
  calculator parity rules. Grep it when a Canvas call behaves oddly.
- docs/process/working-posture-and-roadmap.md — working posture and the Phase 9 roadmap steer,
  moved out of the preamble on 2026-09-14. `BUILD_PLAN.md` (local-only) owns roadmap state.
- `.claude/rules/architecture.md` — the module layout in detail, auto-loaded on `internal/`,
  `pkg/` and `cmd/`.
- `.claude/rules/canvas-api.md` — Canvas client rules, auto-loaded on `internal/canvas/` and
  `pkg/mcp/`.
- Module map: `main.go` → `cmd/root.go` → `pkg/cmd/*` → `internal/canvas/`, `internal/cache/`,
  `internal/auth/`. `pkg/tui/` and `pkg/mcp/` sit alongside `pkg/cmd/`. `pkg/grade/` is
  standalone with no dependencies; `internal/iostreams/` abstracts colour, pager and terminal.
- Releases go out through GoReleaser to Homebrew, Scoop, winget and GitHub Releases, driven by
  GitHub Actions.
