# Changelog

All notable changes to Laurus will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-04-16

Initial public release.

### Added
- **Auth**: `laurus auth login/status/logout` — OS keychain (Keychain/CredManager/SecretService) with FileBackend fallback. Token expiry tracking with 14-day warning.
- **Courses**: `laurus courses`, `laurus course <name>` — fuzzy matching, syllabus rendering, JSON output.
- **Assignments**: `laurus assignments`, `laurus next`, `laurus assignment <course> <name>` — filters for upcoming/overdue/missing/unsubmitted/today/week. HTML→markdown→terminal rendering pipeline.
- **Grades**: `laurus grades`, `laurus grades <course> --detailed`, `--what-if`, `--gpa`, `--statistics` — matches Canvas's exact algorithm (Kane & Kane bisection for drop-lowest, weighted groups, excused handling, `shopspring/decimal` precision).
- **Content**: `laurus modules`, `laurus pages`, `laurus files`, `laurus download`, `laurus mark-done` — tree views, file sync, module completion tracking.
- **Communication**: `laurus announcements`, `laurus discussions`, `laurus inbox`, `laurus reply`, `laurus inbox send/reply` — read + write across courses.
- **Submit**: `laurus submit <course> <assignment> <file...>` — Canvas 3-step upload flow (preflight → S3/InstFS → confirm), text/URL submissions, retry on token expiry, progress reporting.
- **Todo**: `laurus todo add/done/dismiss` — planner notes with due dates and course linking.
- **Cache**: SQLite (WAL mode, pure Go via modernc.org/sqlite), 14 entity tables, per-resource TTLs, `--cached` flag, `laurus sync` with bounded parallelism.
- **Calendar**: `laurus calendar`, `--month`, `--export` (iCal), `--from/--to`.
- **Search**: `laurus search` — Canvas Smart Search with REST fallback.
- **Office hours**: `laurus office-hours`, `laurus office-hours book`.
- **Browser**: `laurus open <course>` / `<course> <assignment>`.
- **Shell integration**: `laurus status --short/--json` (cache-only, <10ms) — Starship and tmux examples.
- **Notifications**: `laurus watch`, `laurus daemon install/status/uninstall` — systemd timer (Linux), launchd (macOS), Task Scheduler (Windows). Deduplicated, configurable lead times.
- **MCP**: `laurus mcp serve` — 23 tools (read + write) via stdio transport. Works with Claude Desktop, Claude Code, Symphony, Cursor, any MCP client.
- **GraphQL**: Selective per-operation strategy — GraphQL for single-course grades (faster), REST everywhere else (Quercus-specific benchmark-driven decision). `LAURUS_ENABLE_GRAPHQL=1` opt-in, `LAURUS_DISABLE_GRAPHQL` kill switch.
- **Distribution**: GoReleaser builds linux/darwin/windows × amd64/arm64. Homebrew tap (`chrismdemian/homebrew-tap`), Scoop bucket (`chrismdemian/scoop-bucket`), GitHub Release binaries.
- **Polish**: `laurus doctor`, `laurus setup` (first-run onboarding), `laurus update` (self-update), `laurus completion bash/zsh/fish/powershell`, version injection via ldflags.

### Known limitations
- TUI mode (`laurus tui`) planned but deferred — not shipped in v0.1.0.
- Single Canvas instance per config; multi-instance support not yet implemented.
