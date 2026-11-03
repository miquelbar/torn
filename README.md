# torn 🌪️

A 100% pragmatic, zero-footprint local hypervisor written in Go for bidirectional context relays, quota telemetry, and seamless switching between AI CLIs (`claude`, `codex`, `vibe`, `agy`).

## Why torn?
- **Zero-Footprint:** Never leaves `.handoff.md`, temporary files, or pollution inside your working repositories. All state lives isolated in `~/.local/share/torn/projects/<hash>/`.
- **Anti-Sandbox Injection:** Bypasses strict file-system sandboxes by injecting context directly as text into `argv[1]` via `syscall.Exec`.
- **Turn-Level Model Telemetry & Speed:** Tracks mid-session model switches, computes precise token consumption, and measures generation speed (`tok/s`) filtering out human idle pauses.

## Installation
```bash
go build -o ~/.local/bin/torn ./cmd/torn
```

## Usage

* **`torn` / `torn <cli>`:** Failover or voluntary relay to the next available or specified AI CLI, preserving context, git HEAD state, and pruning stuck loops via `--drop N`.
* **`torn prep`:** Copies a clean closing handoff prompt to your clipboard before hitting your context window limit (85-90%).
* **`torn stats` / `torn stats --refresh`:** Global terminal dashboard with live status badges (`🟢 READY`, `🟡 ACTIVE`, `🔴 COOLDOWN`), exact reset timers, token counters, and per-model performance breakdown (fast cached view, `--refresh` forces live rescan with braille spinner).
* **`torn find <text>`:** Cross-session unified search across all local CLI histories.
* **`torn resume [cli]`:** Resumes yesterday's active session in your preferred agent.

## License

MIT License. See [LICENSE](LICENSE) for details.
