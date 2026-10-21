# How kabby works

kabby is a zero-config CLI coding agent for local models. It detects whatever
local model server is already running (Ollama, LM Studio, oMLX, MTPLX), hands it
eight small tools, and drives a read → edit → run → verify agent loop inside the
user's project folder.

## Architecture

Three surfaces share one core:

- **Headless** (`-p`): one prompt, exit with code 0/1/130
- **TUI**: raw-mode terminal UI with slash menu
- **Web** (`--web`): localhost-only SSE hub with multi-session sidebar

## Core loop

```
system prompt + AGENTS.md + budgeted history
       ↓
model (streaming with tool calls)
       ↓
execute tool calls (bounded results ride back)
       ↓
repeat until final answer
```

## Context management

- Budget from the real loaded window (Ollama `/api/ps`, LM Studio load response)
- Tier 1 eviction at >80% of input budget, targeting 60%
- Tier 2 handover (harness facts + model narrative) for large contexts
- `/compact` forces compaction below threshold

## Modes

- `ro`: read-only tools (read_file, list_files, search, plan)
- `edit`: adds write_file, edit_file, run_command, task (reach-out needs approval)
- `bypass`: never asks; never persisted to config

## Config

`~/.kabby.json` (mode 0600) holds lastModel + lastModelUrl, lastMode, effort,
hosts, and keys. `bypass` is coerced to `edit` on load.

## See also

- SPECS.md — full build specification
- backend-notes.md — per-backend notes
