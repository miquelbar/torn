# torn 🌪️

[![CI](https://github.com/miquelbar/torn/actions/workflows/ci.yml/badge.svg)](https://github.com/miquelbar/torn/actions/workflows/ci.yml)
[![GitHub Release](https://img.shields.io/github/v/release/miquelbar/torn?include_prereleases&label=release)](https://github.com/miquelbar/torn/releases/latest)
[![Go Report Card](https://goreportcard.com/badge/github.com/miquelbar/torn)](https://goreportcard.com/report/github.com/miquelbar/torn)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

> A zero-footprint local hypervisor in Go for bidirectional context relays, quota telemetry, dollar-cost tracking, and seamless handoffs across official AI CLIs (`claude`, `codex`, `vibe`, `agy`, `opencode`) without breaking sandboxes or touching repository state.

---

## Interactive Terminal Demo

```text
$ torn stats
TORN PROJECT STATS (arreu-cli)
────────────────────────────────────────────────────────────────────────────────────────────────────────────
PROVIDER / MODEL                STATUS        COOLDOWN  SESSIONS      TODAY      TOTAL       COST      SPEED
────────────────────────────────────────────────────────────────────────────────────────────────────────────
claude                          ● READY              —         —          —          —          —          —
codex                           ● READY              —         —          —          —          —          —
vibe                            ● READY              —         —          —          —          —          —
agy                             ● READY              —         7          —       114M          —   46.5 t/s
  ├─ gemini-3.8-flash                                      19.4K          —     110.7M          —   47.0 t/s
  └─ gemini-4-argon                                          447          —       3.3M          —   34.3 t/s
opencode                        ● READY              —        37          —        70M     $22.80   37.0 t/s
  ├─ qwen3-coder-next                                       1.7K          —      54.9M     $13.53   40.9 t/s
  ├─ mimo-v2.6-pro                                            2K          —      13.8M      $8.93   36.5 t/s
  ├─ mimo-v2.6-flash                                         182          —       1.3M      $0.31   41.4 t/s
  └─ deepseek-r1                                               3          —      19.5K      $0.03   23.6 t/s
────────────────────────────────────────────────────────────────────────────────────────────────────────────

$ torn prep
🟢 [TORN PREP] Handoff summary prompt copied to clipboard.
Paste it into your active CLI to generate a clean closing summary before hitting limits.

$ torn codex --drop 2
[torn] Switching context from claude -> codex (pruning 2 turns)...
[torn] Injecting structured handoff via syscall.Exec...
```

---

## Why torn?

- **Zero-Footprint in Repositories:** Never writes `.handoff.md`, temporary state, or lock files inside your project repository. All hypervisor state lives strictly isolated under `~/.local/share/torn/projects/<hash>/`.
- **Anti-Sandbox Context Injection:** Traditional file passing fails in sandboxed CLIs. `torn` injects the complete structured handoff directly into process memory (`argv[1]`) upon invocation via `syscall.Exec`.
- **Hierarchical Telemetry & USD Costs:** Aggregates multi-model sessions with live status badges (`● READY`, `● COOLDOWN`), generation speed (`tok/s`), and exact dollar expenditure directly extracted from logs and SQLite stores.
- **Failover & Loop Breaking:** If an agent gets stuck in a hallucinatory loop, relay with `--drop N` to discard the last $N$ turns and hand over to an alternative provider without losing project history.
- **Sub-Millisecond Search:** Instant unified keyword search across all local AI CLI session transcripts (`torn find`).

---

## Installation

### Option 1: Direct Download (Pre-built Binaries from GitHub Actions)

Download the latest standalone binary directly from [GitHub Releases](https://github.com/miquelbar/torn/releases/latest):

#### macOS (Apple Silicon / M-series)
```bash
mkdir -p ~/.local/bin
curl -fsSL https://github.com/miquelbar/torn/releases/latest/download/torn-darwin-arm64 -o ~/.local/bin/torn
chmod +x ~/.local/bin/torn
```

#### macOS (Intel)
```bash
mkdir -p ~/.local/bin
curl -fsSL https://github.com/miquelbar/torn/releases/latest/download/torn-darwin-amd64 -o ~/.local/bin/torn
chmod +x ~/.local/bin/torn
```

#### Linux (x86_64)
```bash
mkdir -p ~/.local/bin
curl -fsSL https://github.com/miquelbar/torn/releases/latest/download/torn-linux-amd64 -o ~/.local/bin/torn
chmod +x ~/.local/bin/torn
```

#### Linux (ARM64)
```bash
mkdir -p ~/.local/bin
curl -fsSL https://github.com/miquelbar/torn/releases/latest/download/torn-linux-arm64 -o ~/.local/bin/torn
chmod +x ~/.local/bin/torn
```

#### Windows
Download `torn-windows-amd64.exe` from [Latest Release Assets](https://github.com/miquelbar/torn/releases/latest) and place it in your `%PATH%`.

---

### Option 2: Go Install

If you have Go 1.24+ installed:

```bash
go install github.com/miquelbar/torn/cmd/torn@latest
```

---

### Option 3: Build from Source

```bash
git clone git@github.com:miquelbar/torn.git
cd torn
make install
```

*(Installs binary stripped of debug symbols into `~/.local/bin/torn`)*.

---

## Command Reference & Examples

### 1. Telemetry Dashboard (`torn stats`)

Displays live quotas, active cooldowns, token consumption, model trees, generation speeds, and dollar costs.

#### Project Scope (Default inside any repo with AI activity)
```bash
torn stats
```

#### Global Account Scope (Account-wide across all repos)
```bash
torn stats --global
# or shorthand:
torn stats -g
```

Output:
```text
TORN HYPERVISOR STATS (GLOBAL)
────────────────────────────────────────────────────────────────────────────────────────────────────────────
PROVIDER / MODEL                STATUS        COOLDOWN  SESSIONS      TODAY      TOTAL       COST      SPEED
────────────────────────────────────────────────────────────────────────────────────────────────────────────
claude                          ● READY              —        30          —      93.5M      $3.1K  191.8 t/s
  ├─ sonnet-5                                              33.7K          —      93.5M          —  191.8 t/s
  └─ sonnet-4-5                                               24          —          —          —          —
codex                           ● READY              —        32          —     125.2M          —   59.5 t/s
  ├─ gpt-5.5                                               11.6K          —     109.7M          —   64.3 t/s
  ├─ gpt-5.6-sol                                             966          —         9M          —   57.9 t/s
  ├─ gpt-5.6-luna                                            553          —       3.6M          —   25.4 t/s
  └─ gpt-5.4                                                 146          —       1.8M          —          —
agy                             ● READY              —        15       4.2M     121.2M          —   46.3 t/s
  ├─ gemini-3.8-flash                                      20.5K         2M     115.7M          —   47.1 t/s
  ├─ gemini-4-argon                                          447          —       3.3M          —   34.3 t/s
  └─ sonnet-4.6                                              604       2.2M       2.2M          —   41.0 t/s
opencode                        ● READY              —       199          —     150.8M     $87.12   29.6 t/s
  ├─ qwen3-coder-next                                       1.7K          —      54.9M     $13.53   40.9 t/s
  ├─ glm5.3-flash                                           2.3K          —        34M          —   13.5 t/s
  ├─ mimo-v2.6-pro                                          2.5K          —      16.5M     $10.62   34.0 t/s
  ├─ mimo-v2.6-flash                                          1K          —        15M      $2.69   29.3 t/s
  ├─ kimi-k2.6                                              1.5K          —       8.6M     $36.06   33.4 t/s
  └─ deepseek-r1                                               3          —      19.5K      $0.03   23.6 t/s
────────────────────────────────────────────────────────────────────────────────────────────────────────────
```

#### Exporting Formats

Audit expenses and export project metrics into Markdown tables, CSV, or JSON:

```bash
# Markdown table (ideal for PRs or issue summaries)
torn stats --format markdown

# RFC 4180 CSV (ideal for spreadsheets or billing pipelines)
torn stats --format csv

# Structured JSON (for machine consumption)
torn stats --format json
```

Example Markdown Export:
```markdown
# Torn Project Stats (arreu-cli)

| Provider | Status | Cooldown | Sessions | Model | Today | Total | Cost | Speed |
| :--- | :--- | :--- | ---: | :--- | ---: | ---: | ---: | ---: |
| claude | READY | — | 0 | — | 0 | 0 | — | — |
| codex | READY | — | 0 | — | 0 | 0 | — | — |
| agy | READY | — | 7 | gemini-3.8-flash (+1) | 0 | 114,000,000 | — | 46.5 t/s |
| opencode | READY | — | 37 | mimo-v2.6-pro (+3) | 0 | 70,000,000 | $22.80 | 37.0 t/s |
```

---

### 2. Context Prep (`torn prep`)

When your active CLI approaches 80–90% context window saturation, run `torn prep`. It places an optimized closing summarization prompt into your system clipboard:

```bash
torn prep
```

Output:
```text
🟢 [TORN PREP] Handoff summary prompt copied to clipboard.
Paste it into your active CLI to generate a clean closing summary before hitting limits.
```

---

### 3. Bidirectional Relay (`torn [cli]`)

Switch seamlessly from your current agent to the next available provider. `torn` inspects local session transcripts, extracts user directives and file modifications, checks `git diff`, and injects the context cleanly.

#### Automatic Failover or Explicit Switch
```bash
# Automatic relay to next ready CLI
torn

# Explicitly switch to Codex
torn codex

# Explicitly switch to OpenCode
torn opencode
```

#### Loop Breaker (`--drop N`)
If the previous agent got stuck in a repetitive loop, prune the last $N$ assistant turns before executing:
```bash
torn codex --drop 2
```

#### Fresh Task (`--fresh`)
Retain repository git status but start a clean conversation context:
```bash
torn claude --fresh
```

#### Dry-Run Mode (`--dry-run`)
Inspect the generated handoff payload without executing the binary:
```bash
torn opencode --dry-run
```

Output:
```text
================================================================================
                               TORN RELAY DRY-RUN                               
================================================================================
• Detected source: claude
  Session file:    ~/.claude/projects/.../session.jsonl
  Parsed turns:    14
• Target:          opencode
• Exec command:    opencode run -i "<HANDOFF_PROMPT>"

--------------------------------------------------------------------------------
GENERATED HANDOFF PROMPT (injected via argv):
--------------------------------------------------------------------------------
# TORN RELAY HANDOFF

**Timestamp:** 2026-11-03T18:30:00Z
**Workspace:** `/Users/miquel/Projects/torn`
**Git HEAD:** `e93198d034f3688e387e9760486a027318eccea2`
**Relay:** `claude` ➔ `opencode`

## Active Task & Handover Context
...
```

---

### 4. Cross-Session History Search (`torn find <query>`)

Perform fast indexed searches across all local session histories (`claude`, `codex`, `vibe`, `agy`, `opencode`):

```bash
torn find "telemetry"
```

Output:
```text
Found 2 session(s) matching "telemetry":

• [agy] 2026-11-03 18:38 (1373 turns) — 068007ff-df66-499e-b2b7-5ead241a6838
  Snippet: ...ompt when quota nears exhaustion. `torn stats` displays a consolidated dashboard of agent status, consump...

• [opencode] 2026-11-02 21:10 (84 turns) — ses_98f12a
  Snippet: ...implement SQLite cost extraction query for session telemetry...
```

---

### 5. Session Resume (`torn resume [cli]`)

Jump straight back into your latest session:

```bash
torn resume
torn resume claude
```

---

## Supported Providers

| Provider | CLI Tool | Session Source | Telemetry Features |
| :--- | :--- | :--- | :--- |
| **Claude** | `claude` | `~/.claude/projects/` | Token count, reset timers, polymorphic JSONL parser, USD costs (`totalCostUSD`) |
| **Codex** | `codex` | `~/.codex/sessions/` | Turn tracking, rollout JSONL parser, generation speed |
| **Vibe** | `vibe` | `~/.vibe/logs/` | Multi-turn parser, Mistral speed telemetry, session state |
| **AGY** | `agy` | `~/.gemini/antigravity-cli/` | Multi-model token attribution, day/week quotas, reset timers |
| **OpenCode** | `opencode` | `~/.local/share/opencode/` | Native SQLite integration, per-turn & session USD cost tracking, speed |

---

## Architecture

```
┌────────────────────────────────────────────────────────────────────────┐
│                              torn CLI                                  │
│  (stats, prep, relay, find, resume)                                    │
└───────────────────────────────────┬────────────────────────────────────┘
                                    │
         ┌──────────────────────────┼──────────────────────────┐
         ▼                          ▼                          ▼
┌──────────────────┐      ┌──────────────────┐      ┌──────────────────┐
│ Provider Registry│      │  Handoff Builder │      │ Telemetry Engine │
│ • claude         │      │ • Parse turns    │      │ • Turn tokens    │
│ • codex          │      │ • Filter loops   │      │ • Gen speed(t/s) │
│ • vibe           │      │ • Dedupe intent  │      │ • Cost in USD    │
│ • agy            │      │ • Inspect git    │      │ • Cache & format │
│ • opencode       │      └─────────┬────────┘      └──────────────────┘
└──────────────────┘                │
                                    ▼
                          ┌──────────────────┐
                          │  syscall.Exec    │
                          │ (argv injection) │
                          └──────────────────┘
```

1. **Isolation:** Zero state is written to working repos. All hypervisor state is hashed by repository canonical path and stored under `~/.local/share/torn/projects/<hash>/`.
2. **Safe Execution:** Context injection bypasses sandbox path restrictions by handing text directly to binary arguments (`argv`) upon process replacement via `syscall.Exec`.
3. **Speed & Efficiency:** Pure Go with zero CGO dependencies (`CGO_ENABLED=0`), sub-second startup, and comprehensive concurrency race safety (`go test -race ./...`).

---

## License

[MIT](LICENSE) © Miquel Barba
