<div align="center">

# Laurus

**Canvas LMS from your terminal.**

Courses, assignments, grades, files, and deadlines - without opening a browser. Built for students, powered by agents.

[![GitHub Stars](https://img.shields.io/github/stars/chrismdemian/laurus?style=flat&logo=github&cacheSeconds=300)](https://github.com/chrismdemian/laurus)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev)

</div>

---

## What is this?

Laurus is a CLI, TUI, and MCP server for [Canvas LMS](https://www.instructure.com/canvas) -the platform used by thousands of universities. One binary, three modes:

```bash
laurus next                    # CLI: what's due next?
laurus tui                     # TUI: interactive lazygit-style dashboard
laurus mcp serve               # MCP: plug into Claude, Symphony, or OpenClaw
```

Works with any Canvas instance. Laurus is Latin for "laurel", the tree of academic achievement.

---

## Install

Coming soon. Laurus will be available via:

```bash
# macOS
brew install chrismdemian/tap/laurus

# Windows
scoop install laurus
winget install chrismdemian.laurus

# Linux / macOS / WSL
curl -fsSL https://laurus.dev/install.sh | sh

# From source
go install github.com/chrismdemian/laurus@latest
```

---

## Quick Start

```bash
# 1. Authenticate with your Canvas instance
laurus auth login

# 2. See what's due
laurus next

# 3. Check your grades
laurus grades

# 4. Launch the interactive dashboard
laurus tui
```

---

## Features

### CLI Mode

The daily drivers. Fast, scriptable, pipe-friendly.

| Command | Description |
|---------|-------------|
| `laurus next` | Next due assignment across all courses |
| `laurus assignments` | All upcoming assignments, sorted by urgency |
| `laurus grades` | Current grades across all courses |
| `laurus grades --what-if "CSC108:85"` | Simulate final grades |
| `laurus announcements` | Recent announcements across all courses |
| `laurus files sync` | Sync all course files locally |
| `laurus submit <course> <assignment> <file>` | Submit from the terminal |
| `laurus calendar --export` | Export deadlines to `.ics` |
| `laurus inbox` | Read and send Canvas messages |
| `laurus search <query>` | AI-powered semantic search across courses |

Every command supports `--json` for scripting and `--cached` for offline use.

### TUI Mode

A lazygit-style interactive terminal dashboard. Navigate courses, browse assignments, check grades, read announcements -all with vim keybindings.

```
laurus tui
```

```
┌─ Courses ──────────┬─ Assignments ──────────────┬─ Details ─────────────────┐
│                    │                            │                           │
│ > CSC108           │   Assignment 3             │  Binary Search Trees      │
│   MAT137           │   Problem Set 7            │                           │
│   ECE253           │ > Lab Report 4             │  Due: Tomorrow 11:59 PM   │
│   PHY180           │   Reading Response 6       │  Points: 40               │
│   ENG195           │                            │  Submitted: No            │
│                    │                            │                           │
├────────────────────┴────────────────────────────┤  Rubric:                  │
│  Status: 3 due this week | 1 overdue | 2 unread │  - Correctness (20)       │
└─────────────────────────────────────────────────┴───────────────────────────┘
```

### MCP Server Mode

Plug Canvas into any AI assistant. Claude, Symphony, Cursor, OpenClaw -anything that speaks [MCP](https://modelcontextprotocol.io).

```bash
laurus mcp serve
```

```json
// claude_desktop_config.json
{
  "mcpServers": {
    "canvas": {
      "command": "laurus",
      "args": ["mcp", "serve"]
    }
  }
}
```

Now your AI assistant can check deadlines, read assignments, look up grades, and submit homework on your behalf.

### Grade Calculator

The first tool to match Canvas's exact grade calculation algorithm: weighted groups, drop-lowest rules (Kane & Kane bisection), extra credit, excused assignments. No more broken third-party calculators.

```bash
laurus grades CSC108 --detailed         # per-assignment breakdown with rubric
laurus grades --what-if "CSC108:A"      # simulate final exam scores
laurus grades --gpa                     # compute GPA across all courses
```

### Offline Mode

Everything is cached locally in SQLite. Check grades on the subway. Review assignments on a plane.

```bash
laurus sync                             # sync all data + files
laurus assignments --cached             # read from cache, no network
```

### Shell Integration

Ambient awareness without opening anything.

```toml
# ~/.config/starship.toml
[custom.canvas]
command = "laurus status --short"
format = "[$output]($style) "
style = "yellow"
interval = 300
```

---

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                    laurus binary                    │
├────────────┬──────────────┬──────────┬──────────────┤
│  CLI Mode  │   TUI Mode   │ MCP Mode │  Daemon Mode │
│  (cobra)   │  (bubbletea) │ (mcp-go) │ (background) │
├────────────┴──────────────┴──────────┴──────────────┤
│                    Core Library                     │
│                                                     │
│  ┌──────────┐   ┌───────────┐   ┌────────────────┐  │
│  │ Canvas   │   │ Grade     │   │ HTML Renderer  │  │
│  │ API      │   │ Calculator│   │                │  │
│  │ Client   │   │ (exact)   │   │                │  │
│  ├──────────┤   ├───────────┤   ├────────────────┤  │
│  │ GraphQL  │   │ File Sync │   │ Notification   │  │
│  │ + REST   │   │ Engine    │   │ Engine         │  │
│  ├──────────┤   ├───────────┤   ├────────────────┤  │
│  │ SQLite   │   │ Auth &    │   │ Calendar       │  │
│  │ Cache    │   │ Keychain  │   │ Export         │  │
│  └──────────┘   └───────────┘   └────────────────┘  │
└─────────────────────────────────────────────────────┘
```

- **REST + GraphQL hybrid** - GraphQL for bulk queries (courses + assignments + submissions in one call), REST for mutations and file uploads
- **SQLite cache** with WAL mode - offline reads, incremental sync, sub-millisecond lookups
- **OS keychain** for token storage (Keychain on macOS, Credential Manager on Windows, Secret Service on Linux)
- **Smart polling** - `graded_since` and `start_date` parameters for efficient change detection

---

## Supported Canvas Features

Laurus covers the full Canvas student API surface:

| Category | Endpoints | Status |
|----------|-----------|--------|
| Courses | List, details, syllabus, people | Coming soon |
| Assignments | List, view, submit, rubrics | Coming soon |
| Grades | Current/final, weighted, what-if | Coming soon |
| Modules | List, tree view, completion tracking | Coming soon |
| Files | Browse, download, sync | Coming soon |
| Announcements | List, filter, unread | Coming soon |
| Discussions | List, read threads, post replies | Coming soon |
| Calendar | Events, deadlines, iCal export | Coming soon |
| Inbox | Read, send, reply | Coming soon |
| Planner | Todo items, mark complete | Coming soon |
| Quizzes | View details, results | Coming soon |
| Groups | List, files, discussions | Coming soon |
| Search | AI-powered Smart Search | Coming soon |
| Analytics | Activity stats, history | Coming soon |
| Office Hours | View slots, book appointments | Coming soon |

---

## Configuration

```bash
laurus auth login
# Opens Canvas in your browser → paste your API token
# Token stored in OS keychain, never in plaintext
```

Config lives at `~/.config/laurus/config.toml`:

```toml
[canvas]
url = "https://q.utoronto.ca"    # your institution's Canvas URL

[sync]
dir = "~/School"                  # where to sync course files
interval = "30m"                  # background sync interval

[display]
theme = "auto"                    # auto, dark, light
```

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and guidelines.

---

## License

[Apache 2.0](LICENSE)
