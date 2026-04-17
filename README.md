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

Laurus is a CLI and MCP server for [Canvas LMS](https://www.instructure.com/canvas) -the platform used by thousands of universities. One binary, two modes:

```bash
laurus next                    # CLI: what's due next?
laurus mcp serve               # MCP: plug into Claude, Symphony, or OpenClaw
```

Works with any Canvas instance. Laurus is Latin for "laurel", the tree of academic achievement.

---

## Install

```bash
# macOS
brew install chrismdemian/tap/laurus

# Windows
scoop bucket add laurus https://github.com/chrismdemian/scoop-bucket
scoop install laurus

# From source (requires Go 1.26+)
go install github.com/chrismdemian/laurus@latest
```

Pre-built binaries for Linux, macOS, and Windows are available on the [Releases](https://github.com/chrismdemian/laurus/releases) page.

---

## Quick Start

```bash
# 1. First-run setup (prompts for Canvas URL + API token)
laurus setup

# 2. See what's due
laurus next

# 3. Check your grades
laurus grades

# 4. Plug into Claude / Symphony / Cursor (see MCP section below)
laurus mcp serve
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
| `laurus sync` | Sync Canvas data + course files to local cache |
| `laurus submit <course> <assignment> <file>` | Submit from the terminal |
| `laurus calendar --export` | Export deadlines to `.ics` |
| `laurus inbox` | Read and send Canvas messages |
| `laurus search <query>` | AI-powered semantic search across courses |

Every command supports `--json` for scripting and `--cached` for offline use.

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

Works with Claude Desktop, Claude Code, Symphony, Cursor, and any MCP-compatible client. The same config format applies to all of them.

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

```bash
# ~/.tmux.conf
set -g status-right '#(laurus status --short)'
set -g status-interval 300
```

### Background Notifications

Get desktop notifications for new grades, announcements, and upcoming deadlines.

```bash
laurus daemon install              # install background polling (systemd/launchd/Task Scheduler)
laurus daemon status               # check if it's running
laurus daemon uninstall            # remove it
laurus watch                       # or run manually in the foreground
```

---

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                    laurus binary                    │
├───────────────┬──────────────────┬──────────────────┤
│    CLI Mode   │     MCP Mode     │   Daemon Mode    │
│    (cobra)    │     (mcp-go)     │   (background)   │
├───────────────┴──────────────────┴──────────────────┤
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
| Courses | List, details, syllabus, people | Done |
| Assignments | List, view, submit, rubrics | Done |
| Grades | Current/final, weighted, what-if | Done |
| Modules | List, tree view, completion tracking | Done |
| Files | Browse, download, sync | Done |
| Announcements | List, filter, unread | Done |
| Discussions | List, read threads, post replies | Done |
| Calendar | Events, deadlines, iCal export | Done |
| Inbox | Read, send, reply | Done |
| Planner | Todo items, mark complete | Done |
| Quizzes | View details, results | Done |
| Groups | List, files, discussions | Done |
| Search | AI-powered Smart Search | Done |
| Analytics | Activity stats, history | Done |
| Office Hours | View slots, book appointments | Done |

---

## Configuration

```bash
laurus setup
# Prompts for Canvas URL, opens browser with token instructions, stores token in OS keychain
```

Config lives in your OS config directory:
- **Linux**: `~/.config/laurus/config.toml`
- **macOS**: `~/Library/Application Support/laurus/config.toml`
- **Windows**: `%AppData%\laurus\config.toml`

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
