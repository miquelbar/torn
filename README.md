# torn

Zero-footprint local hypervisor written in Go, designed to manage bidirectional handoffs, quota telemetry, and context recovery across official AI CLIs (`claude`, `codex`, `vibe`, `agy`, etc.) without breaking sandboxes or touching repository state.

## Core Features

- **Zero-Footprint in Repositories:** Never writes control files, caches, or handoff notes into the target repository. State is stored in an isolated directory under `~/.local/share/torn/projects/<hash>/`.
- **Anti-Sandbox Context Injection:** Injects handoff context directly into `argv[1]` upon process invocation via `syscall.Exec`, avoiding sandbox file permission restrictions (`/tmp` or `$HOME`).
- **Resilient CWD & Git Inspection:** Scans CLI session logs filtering strictly by current project workspace `cwd` and validates against current `git rev-parse HEAD`.
- **Soft Landing (`torn prep`):** Copies standardized handoff prompts to the system clipboard when reaching quota limits (85-90%).
- **Unified Quota & Cooldowns (`torn stats`):** Real-time traffic light dashboard (🟢 READY, 🟡 ACTIVE, 🔴 COOLDOWN) with cooldown countdowns and total historical tokens.
- **Cross-Session Search (`torn find <query>`):** Searches local session histories across all CLI tools for keywords.
- **Next-Day Resume (`torn resume`):** Restores active context from previous sessions into your preferred CLI.

## Installation & Build

### Prerequisites

- Go 1.22 or higher
- `make`

### Building from Source

```bash
make setup
make build
```

Binary is compiled to `bin/torn`. To install globally into your `$GOPATH/bin` or `$HOME/.local/bin`:

```bash
cp bin/torn ~/.local/bin/torn
```

### Running Tests & Quality Checks

```bash
make check
```

## Command Reference

### 1. Relay (`torn [cli]`)

Switches execution to the specified CLI (or the next available free CLI in queue):

```bash
# Automatic failover relay to the next available CLI
torn

# Intentional return to primary agent
torn claude

# Relay to Codex dropping the last 2 turns (prune loops or hallucinations)
torn codex --drop 2

# Fresh relay clearing the accumulated chain for a new feature
torn codex --fresh
```

### 2. Soft Landing (`torn prep`)

When nearing your native CLI quota limit (85-90%), run `torn prep` to copy a structured closing prompt to the clipboard:

```bash
torn prep
```

Paste it into the active CLI session so the agent documents its current state, test coverage, and pending tasks before hitting limits.

### 3. Global Stats Dashboard (`torn stats`)

Inspect operational availability, cooldown countdowns, and token metrics:

```bash
torn stats
```

Output preview:
```txt
===================================================================================================================
                                               TORN HYPERVISOR STATS                                               
===================================================================================================================
PROVIDER   | STATUS         | COOLDOWN   | SESSIONS   | TOKENS TODAY   | THIS WEEK      | TOTAL TOKENS     
-----------+----------------+------------+------------+----------------+----------------+------------------
claude     | 🟢 READY       | --         | 30         | --             | --             | 92,162,540       
codex      | 🟢 READY       | --         | 32         | --             | --             | 65,856,161       
vibe       | 🟢 READY       | --         | 40         | --             | --             | 85,457,547       
agy        | 🟡 ACTIVE      | --         | 15         | 3,853,620      | 3,853,620      | 116,849,490      
===================================================================================================================
```

### 4. Unified Session Search (`torn find <query>`)

Search across past local sessions for a keyword or issue:

```bash
torn find "deadlock"
```

### 5. Session Resume (`torn resume [cli]`)

Pick up the latest session thread from yesterday or earlier today:

```bash
torn resume claude
```

### 6. Rules Consistency (`torn sync`)

Ensures rules and agent configurations are aligned in `$HOME` without injecting files into the repository:

```bash
torn sync
```

## Project Architecture

```txt
cmd/torn/                       # CLI entrypoint
internal/
  cli/                          # Command routing and flag parsing
  clipboard/                    # System clipboard integration (macOS/Linux)
  cooldown/                     # Rate-limit cooldown state tracking
  executor/                     # Sandbox-safe syscall.Exec process replacement
  handoff/                      # Prompt synthesizer with --drop and --fresh
  project/                      # Canonical path hashing and Git HEAD inspection
  provider/                     # Log discovery & parsers (claude, codex, vibe, agy)
  search/                       # Cross-provider local session search
  stats/                        # Telemetry collector and dashboard renderer
  storage/                      # Isolated zero-footprint state store
Makefile                        # Automation targets
go.mod                          # Go module dependencies
LICENSE                         # MIT License
```

## Contributing

1. Create a feature branch (`git checkout -b feat/my-improvement`)
2. Follow [Conventional Commits](https://www.conventionalcommits.org/) (single-line only, no co-author trailers)
3. Run `make check`
4. Submit a Pull Request

## License

MIT License. See [LICENSE](LICENSE) for details.
