---
paths:
  - "internal/**"
  - "pkg/**"
  - "cmd/**"
---

# Architecture

## Directory Structure

```
quercus/
├── main.go                    # Entry point — calls cmd.Execute()
├── cmd/root.go                # Cobra root command, global flags, version
├── internal/                  # Private packages (not importable externally)
│   ├── canvas/                # Canvas LMS REST + GraphQL API client
│   ├── config/                # Config loading (~/.config/quercus/config.toml)
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

- **GraphQL for reads, REST for writes**: GraphQL for bulk data (courses + assignments + submissions in one call), REST for mutations and file uploads
- **SQLite cache with WAL**: Enables concurrent reads (CLI) while background sync writes
- **OS keychain for tokens**: Never plaintext config files for secrets
- **Cobra subcommand pattern**: One package per noun (matches gh CLI structure)
- **Bubble Tea Elm Architecture**: Model/Update/View for TUI, hard separation from domain logic
