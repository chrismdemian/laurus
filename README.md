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

Every command supports `--json` for scripting. `laurus status` reads only the local cache and `laurus courses --cached` serves from it; other commands are live and refresh the cache as they go.

### MCP Server Mode

Plug Canvas into any AI assistant. Claude, Symphony, Cursor, OpenClaw -anything that speaks [MCP](https://modelcontextprotocol.io).

```bash
laurus mcp serve               # add --read-only to hide every tool that writes to Canvas
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

Reads are **cache-first with an honest stamp**: every tool result is an
envelope `{as_of, stale, source, data}` where `source` is `cache` (served
from the local sync cache, refreshed automatically when older than the
tool's freshness tier: 5 min for anything carrying grades, 30 min for
announcements and discussions, 1 h for file metadata, 4 h for modules and
pages) or `live` (fetched just now). Time-critical tools (next assignment,
overdue, calendar, todo, search, office hours, inbox, unread count,
reading a conversation, grade calculations) are always live. Pass
`fresh: true` to any cache-served tool to force a refresh, call
`laurus_sync` to refresh everything and get the error list, and use
`search_local_files` to grep the course files you downloaded with
`laurus sync files` or `laurus download-all` (their contents are treated
as untrusted text).

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
canvas_url = "https://q.utoronto.ca"   # your institution's Canvas URL
sync_dir = "~/School"                  # where `laurus sync files` puts course files
theme = "auto"                         # auto, dark, light
```

Environment variables override the file and the keychain, so a headless
machine needs no config at all:

| Variable | Overrides |
|---|---|
| `CANVAS_TOKEN` | the stored API token |
| `CANVAS_URL` | `canvas_url` in config.toml |

The token is stored in the OS keychain (macOS Keychain, Windows Credential
Manager, Secret Service on Linux) with a `credentials` file next to
config.toml as the fallback.

---

## Setting up with a coding agent (non-interactive)

Everything below is safe to hand to an agent. The human's only inputs are
**their Canvas API token** (Canvas > Account > Settings > New Access Token)
and **their institution's Canvas URL**. Nothing prompts; missing values are
an error, not a hung form.

```bash
# 1. Configure: validates the token against Canvas, stores it, saves the URL.
laurus setup --token "$CANVAS_TOKEN" --url https://canvas.school.edu --yes

# Same, keeping the token out of argv and shell history:
echo "$CANVAS_TOKEN" | laurus setup --token-stdin --url https://canvas.school.edu --yes

# 2. Verify. Exit code is non-zero when any check fails.
laurus doctor --json && echo ok

# 3. Wire it into Claude Code (token already in the keychain after step 1):
claude mcp add laurus -- laurus mcp serve

# Or skip setup entirely and pass everything through the environment:
claude mcp add laurus -e CANVAS_TOKEN=... -e CANVAS_URL=https://canvas.school.edu -- laurus mcp serve
```

`laurus mcp install --client claude-code|cursor|vscode` writes the entry
for you, merging with the servers already there and doing nothing on a
second run:

```bash
laurus mcp install --client claude-code                 # project: ./.mcp.json
laurus mcp install --client claude-code --scope user    # user: ~/.claude.json (backed up to .bak first)
laurus mcp install --client cursor --scope user         # ~/.cursor/mcp.json
laurus mcp install --client vscode --scope user         # VS Code user mcp.json
laurus mcp install --client vscode --read-only --print  # show the entry, write nothing
```

The entry passes `CANVAS_TOKEN` through from the environment in the
client's own syntax (`${CANVAS_TOKEN}` for Claude Code, `${env:CANVAS_TOKEN}`
for Cursor and VS Code), so no token is written to disk; `--no-env` drops
that when the token is already in the keychain. The Claude Code project
form it writes:

```json
{
  "mcpServers": {
    "laurus": {
      "command": "laurus",
      "type": "stdio",
      "args": ["mcp", "serve"],
      "env": { "CANVAS_TOKEN": "${CANVAS_TOKEN}" }
    }
  }
}
```

Add `"CANVAS_URL": "https://canvas.school.edu"` to `env` if `laurus setup`
was never run on that machine. Add `--read-only` to the `serve` args when the assistant should never be
able to submit, post, send, or book anything in Canvas; the write tools are
then not registered at all.

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and guidelines.

---

## License

[Apache 2.0](LICENSE)
