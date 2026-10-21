# kabby

Zero-config CLI coding agent for local models (Ollama, LM Studio, oMLX, MTPLX).
Single static Go binary, no account, no API key, no config file required.

## Install

### One-liner (copy & paste)

```sh
curl -fsSL https://raw.githubusercontent.com/vicgupta/kabby/main/install.sh | sh
```

This detects your OS/arch, downloads the matching static binary from the
GitHub release, and installs it to `/usr/local/bin` (or `~/.local/bin`).

### Or download manually

Static binaries for linux/amd64, darwin/amd64, darwin/arm64, windows/amd64:

https://github.com/vicgupta/kabby/releases/latest

### Or build from source

```sh
git clone https://github.com/vicgupta/kabby.git
cd kabby
make build
./kabby --help
```

## Usage

```
kabby [workspace] [flags]
```

`workspace` defaults to the current directory.

| Flag | Meaning |
|---|---|
| `-m, --mode <ro\|edit\|bypass>` | permission mode |
| `--model <name>` | pick model by partial name |
| `--ctx <tokens>` | force context window (≥1024) |
| `--verify <command>` | headless acceptance gate (requires `-p`) |
| `--verify-attempts <n>` | max acceptance checks (default 6) |
| `--effort <off\|low\|medium\|high\|default>` | reasoning effort |
| `--web [port]` | browser UI (default 7433) |
| `-p, --print "<prompt>"` | headless: one prompt, exit |
| `-h, --help` / `-v, --version` | help / version |

| Env | Meaning |
|---|---|
| `KABBY_CONFIG` | alternate config path for tests |
| `KABBY_KEEP_ALIVE` | Ollama keep-alive override (minutes) |
| `OLLAMA_HOST` | Ollama base URL |
| `OMLX_API_KEY` / `MTPLX_API_KEY` | fallback API keys |

## Three surfaces

- **Headless:** `kabby -p "add a README" --verify "go test ./..."` — one turn, exit
  code 0 on success, 1 on failure, 130 on cancel.
- **TUI:** `kabby` — raw-mode terminal UI, `/` slash menu, `shift+tab` to cycle
  modes, `ctrl+c`×2 to quit.
- **Web:** `kabby --web` — localhost-only SSE hub at port 7433, multi-session
  sidebar, transcripts under `~/.kabby/sessions/`.

## Modes

- `ro` — read-only tools (`read_file`, `list_files`, `search`, `plan`)
- `edit` — adds `write_file`, `edit_file`, `run_command`, `task`; reach-out
  commands need approval
- `bypass` — never asks; never persisted to config

## Benchmarks

Run benchmarks from `bench/`:

```sh
./bench/run.sh mc-compact <model>
./bench/run.sh acceptance <model>
```

Scenarios parse the `[stats]` JSON line kabby emits on stderr.

## Docs

- `docs/how-it-works.md` — architecture and core loop
- `docs/backend-notes.md` — per-backend notes (Ollama, LM Studio, oMLX, MTPLX)

## Release

Static binaries for linux/amd64, darwin/amd64, darwin/arm64, windows/amd64:

```sh
make release
```

See SPECS.md for the full build specification (Phases 0–14).
