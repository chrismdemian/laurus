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
│   ├── cache/                 # SQLite cache (WAL mode, per-resource TTL)
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

## Key Design Decisions

- **Per-operation GraphQL**: GraphQL only for single-course grade queries (one round-trip for groups→assignments→submissions); REST for everything else (server-side filtering makes it faster for course/assignment listing). REST always for writes and file uploads.
- **SQLite cache with WAL**: Enables concurrent reads (CLI) while background sync writes
- **OS keychain for tokens**: Never plaintext config files for secrets
- **Cobra subcommand pattern**: One package per noun (matches gh CLI structure)
- **Bubble Tea Elm Architecture**: Model/Update/View for TUI, hard separation from domain logic
