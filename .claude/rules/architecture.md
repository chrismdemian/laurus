---
paths:
  - "internal/**"
  - "pkg/**"
  - "cmd/**"
---

# Architecture

## Directory Structure

```
laurus/
├── main.go                    # Entry point — calls cmd.Execute()
├── cmd/root.go                # Cobra root command, global flags, version
├── internal/                  # Private packages (not importable externally)
│   ├── canvas/                # Canvas LMS REST + GraphQL API client
│   ├── config/                # Config loading (~/.config/laurus/config.toml)
│   ├── auth/                  # Token management, OS keychain integration
│   ├── cache/                 # SQLite cache (WAL, one conn per handle, ReplaceAll + ListFresh)
│   ├── syncer/                # Per-(job, course) cache fill; shared by CLI sync and MCP
│   ├── onboard/               # Non-interactive setup/login: resolve, TTY guard, configure
│   ├── pathsafe/              # Sanitises Canvas-supplied names before any filesystem write
│   ├── render/                # Canvas HTML -> markdown
│   ├── update/                # Self-update check
│   └── iostreams/             # Color, pager, stdout/stderr abstraction
├── pkg/                       # Public packages (could be imported externally)
│   ├── cmd/                   # One package per subcommand (gh pattern)
│   │   ├── courses/
│   │   ├── assignments/
│   │   ├── grades/
│   │   └── ...
│   ├── tui/                   # Bubble Tea TUI (lazygit pattern)
│   │   ├── views/
│   │   ├── style/
│   │   ├── components/
│   │   └── keybindings/
│   ├── mcp/                   # MCP server (tools + handlers)
│   ├── grade/                 # Grade calculation engine (standalone)
│   └── cmdutil/               # Shared cobra helpers, factory
└── test/integration/          # E2E tests against live binary
```

## Dependency Rules

- `pkg/cmd/*` → `internal/*` (allowed)
- `pkg/tui/` → `internal/*` (allowed)
- `pkg/mcp/` → `internal/*` (allowed)
- `pkg/tui/` → `pkg/cmd/*` (FORBIDDEN)
- `pkg/cmd/*` → `pkg/tui/` (FORBIDDEN)
- `pkg/mcp/` → `pkg/tui/` or `pkg/cmd/*` (FORBIDDEN)
- `internal/*` → `pkg/*` (FORBIDDEN — internal never imports public)
- `pkg/mcp/` → `internal/syncer` (allowed; this is how MCP refreshes the cache without importing `pkg/cmd/sync`)

## Key Design Decisions

- **Per-operation GraphQL**: GraphQL only for single-course grade queries (one round-trip for groups→assignments→submissions); REST for everything else (server-side filtering makes it faster for course/assignment listing). REST always for writes and file uploads.
- **SQLite cache with WAL**: Enables concurrent reads (CLI) while background sync writes. Pragmas live in the DSN (every connection), one connection per handle; a `*sql.Tx` holder must never call another `DB` method until it commits.
- **Sync is per job, replaces atomically**: `cache.ReplaceAll` upserts, prunes and stamps `sync_meta` in one transaction; a truncated fetch is marked `suspect` and never prunes; an honest empty fetch prunes (upstream deleted everything). Every tx is `BEGIN IMMEDIATE` (`_txlock=immediate` in the DSN) so cross-process read-then-write never hits an unretryable SQLITE_BUSY. Reads use `ListFresh` (rows from the last complete sync).
- **OS keychain for tokens**: Never plaintext config files for secrets
- **Cobra subcommand pattern**: One package per noun (matches gh CLI structure)
- **Bubble Tea Elm Architecture**: Model/Update/View for TUI, hard separation from domain logic
