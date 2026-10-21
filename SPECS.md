# SPECS.md — kabby (Go)

Build specification for **kabby**: a zero-dependency-at-runtime, zero-config CLI
coding agent for local models (Ollama, LM Studio, oMLX, MTPLX), reimplemented as a
single statically-linked **Go** binary.

This document translates SPECS2.md (the TypeScript/Node specification, v0.7.1,
renamed to `kabby`) into Go terms: packages instead of `src/` modules,
`go build`/`go test` instead of npm scripts, standard-library-first runtime, and one
compiled `kabby` binary. Product behavior, invariants, and acceptance criteria are
unchanged unless explicitly noted as Go-specific.

## 0. How to use this spec

Build in **Phases 0–14 in order**. Each phase:

1. touches only the files listed,
2. ends with a runnable build (`go build ./...`) and green tests (`go test ./...`),
3. lists acceptance criteria (AC) checkable without a live model server.

Tests use Go stdlib `testing` plus `net/http/httptest` stubs (zero runtime
dependencies). Tests point at a scratch config via `KABBY_CONFIG` so they never
touch `~/.kabby.json`.

Target: every phase AC is machine-checkable against the source tree. When a phase
depends on a provider/model server, stub it locally (see §6 "Test harness").

Decisions baked into this Go translation (answered in §9):

- Module `kabby`; Go 1.22 minimum (`go` directive in `go.mod`).
- One binary `kabby`, built with `CGO_ENABLED=0` for a fully static, dependency-free runtime.
- Build-time third-party modules are allowed (compiled into the binary); nothing is required at runtime.

---

## 1. Product definition

### 1.1 One-line spec

A Go ≥ 1.22 CLI binary (`kabby`) that detects whatever local model server is
already running, hands it eight small tools, and drives a read → edit → run → verify
agent loop inside the user's project folder — in a terminal TUI, a browser UI
(`--web`), or headless (`-p "prompt"`).

### 1.2 User stories

| As a | I want to | So that |
|---|---|---|
| developer | run `kabby` in any project with a local model server | I get a coding assistant with no account, API key, or config file |
| user | pick `ro` / `edit` / `bypass` mode | I control what files and commands the agent may touch |
| team | run `kabby --web` | multiple people can share a browser UI with separate workspaces |
| CI script | run `kabby -p "…" --verify "go test ./..."` | the agent self-repairs until acceptance passes or exits nonzero |
| local-model user | use Ollama / LM Studio / oMLX / MTPLX on another machine | my laptop doesn't need a GPU |

### 1.3 Goals

- **Zero config.** `go install` / copy the binary, then `kabby` works with no
  account, no API key (except oMLX/MTPLX's own), and no config file. Server
  discovery, model pick, and permission defaults are automatic.
- **Designed for small local models.** Short system prompt, flat tool schemas with
  example calls, bounded tool output, harness-held state that survives compaction.
- **Privacy.** Requests go only to servers the user chose: local ones, or network
  machines the user explicitly added. A network-swept machine is never used or keyed
  until picked. Keys typed at add-time never leave the local machine except to that
  server.
- **One codebase, three surfaces.** TUI and web UI are twins of the same
  `Agent`/`EventBus` core; headless reuses the identical loop.

### 1.4 Non-goals

- No cloud providers, no sandbox OS isolation (the mode gate is a text check, not a
  container), no dependency-graph scheduling or subagents, no prompt caching beyond
  what the backend gives, no ranking claims against Claude Code / Codex / OpenCode.

### 1.5 Constraints

- **Go-only runtime.** Third-party build-time deps are OK (compiled into the static
  binary; e.g. bubbletea for the TUI); the shipped binary uses Go builtins only at
  runtime (`CGO_ENABLED=0` for a fully static binary).
- Every command and file access is confined to the workspace by path resolution
  (`internal/sandbox`); anything resolving outside requires approval in `edit` mode.
- `bypass` mode is never persisted across sessions (`loadConfig` silently coerces a
  saved `bypass`/`yolo` back to `edit`).
- Go ≥ 1.22 (`go` directive in `go.mod`).

---

## 2. Architecture map (Go packages)

```
cmd/kabby/         CLI entry: parseArgs, help/version, three run paths
                   (headless / web / TUI)
internal/agent/    THE agent loop: tool-call batching, checks, recovery,
                   verify gate, stats
internal/context/  Context window budgeting, tier-1 eviction, tier-2 handover
                   compaction
internal/prompt/   System prompt builder + AGENTS.md loader (8k char cap)
internal/plan/     Harness-held checklist (≤20 steps, per-step checkpoint ≤1000
                   chars)
internal/tools/    Tool registry: 4 read tools + 4 write/exec tools by mode
internal/tools/fs.go       read_file, list_files, write_file, edit_file
internal/tools/shell.go    run_command (120 s timeout), pickShell
internal/tools/tasks.go    background task manager (dev servers: start/list/
                   logs/stop, killAll on exit)
internal/tools/check.go    post-write syntax check (Go/JS/JSON/HTML/Python)
internal/sandbox/  Path confinement + command reach-out text check + approval
internal/providers/ Backend abstraction: types, transport, ollama, lmstudio,
                   scheduler
internal/detect/   Local server discovery (ports, docker, WSL/container gateways)
internal/hosts/    Saved remote machines + probes
internal/network/  Remote host helpers
internal/netscan/  On-demand TCP subnet sweep
internal/omlx/     oMLX specifics (health, keys, ports from settings)
internal/mtplx/    MTPLX specifics (health, keys, ports from settings)
internal/config/   ~/.kabby.json (lastModel+URL, mode, effort, hosts, keys;
                   chmod 600)
internal/history/  Transcript model, append/evict bookkeeping
internal/events/   EventBus (compaction notices etc.)
internal/ui/       Common print/status/warn sink used by all surfaces
tui/               Raw-mode terminal UI, editor, key handling
web/               hub (SSE server), client, page, styles, channel, store,
                   terminal, attachments
```

### 2.1 Type contracts to implement (Go)

```go
// internal/providers/types.go
package providers

type Mode string

const (
    ModeRO     Mode = "ro"
    ModeEdit   Mode = "edit"
    ModeBypass Mode = "bypass"
)

type Effort string // "off" | "low" | "medium" | "high" | "" (null = default)

type ToolSpec struct {
    Name        string
    Description string
    Parameters  any // flat schema (no nested objects/arrays)
}

type ModelInfo struct {
    ID              string
    Name            string
    Backend         string // ollama | lmstudio | omlx | mtplx
    URL             string
    ToolsSupported  bool
    ContextWindow   int
    SupportedEffort []Effort
    DefaultEffort   Effort
}

// internal/plan.go
package plan

type PlanStep struct {
    Text  string
    Done  bool
    Note  string // ≤1000 chars
}

type Plan struct {
    steps []PlanStep
}

func (p *Plan) Exists() bool
func (p *Plan) DoneCount() int
func (p *Plan) CurrentIndex() int
func (p *Plan) Reset()
func (p *Plan) Set(stepsText string) string        // "Plan set (N steps). Current: 1. ..."
func (p *Plan) MarkDone(stepNumber *int) string    // nil ⇒ current step; restates next
func (p *Plan) Add(text string) string
func (p *Plan) Checkpoint(text string) string      // 1–1000 chars, replaces current note
func (p *Plan) ModelView() string
func (p *Plan) CompactLine() string | nil
func (p *Plan) PendingSummary() string

// internal/config.go
package config

type Config struct {
    LastModel     string
    LastModelURL  string // same model id on two machines stays apart
    LastMode      Mode
    Effort        *Effort
    Hosts         []SavedHost
    Keys          map[string]string
}

type SavedHost struct {
    Address string
    Name    string
}

const (
    CONFIG_PATH string // ~/.kabby.json
    DATA_DIR    string // ~/.kabby
)

func LoadConfig() Config
func SaveConfig(cfg Config) error          // chmod 0600 before first key write
func UpdateConfig(patch map[string]any) Config // merge by re-reading
func SavedKey(base string) (string, bool)
func SetKeys(bases []string, key string)

// internal/session.go
package session

type SessionPrefs struct {
    Mode     Mode
    Model    string
    CTX      int
    Effort   *Effort
}

func PrepareModel(prefs SessionPrefs, cfg config.Config, onLabel func(string)) (*BackendHandle, error)
func EffortAdvice(chosen, setting Effort) string
func NoBackendsMessage() string
func SessionLine(chosen ModelInfo, mode Mode) string

// internal/tools/index.go
package tools

type ToolContext struct {
    Workspace        string
    TaskManager      *TaskManager
    Plan             *plan.Plan
    FilesTouched     map[string]bool
    CommandsRun      []string
    ResultCharLimit  int
}

func BuildToolSpecs(mode Mode) []providers.ToolSpec
func ExecuteTool(name string, args any, ctx *ToolContext, signal <-chan struct{}) (string, error)
func CommandOf(name string, args any) string | nil
```

### 2.2 Key invariants

1. **The plan never enters the model transcript.** It is harness state (`Plan`); the
   model sees only a bounded re-injection, the user sees it free of tokens, compaction
   cannot destroy it.
2. **Coding inference always wins the backend.** Background work (handover summaries,
   session titles) yields/defers to a foreground call — one inference slot per server
   URL per process (`internal/providers/scheduler`).
3. **No fabricated history.** Applied `write_file` bodies become marked harness
   records, not re-imagined assistant text; malformed/cut-off tool calls are never
   executed partially.
4. **Budget from the real window.** Ask the server what is actually loaded
   (Ollama `/api/ps`, LM Studio load response), budget for that, not the advertised
   max.

---

## 3. Functional requirements by subsystem

### 3.1 Server discovery (`internal/detect`, `internal/hosts`, `internal/network`,
`internal/netscan`, `internal/omlx`, `internal/mtplx`)

- **Identify by response, never by port.** `/api/tags` ⇒ Ollama; `/api/v1/models`
  (or older catalog) ⇒ LM Studio; `/health` with `engine_pool` ⇒ oMLX (needs key);
  `/health` with `mtp_enabled` + `generation_mode` ⇒ MTPLX. All probes fire
  concurrently — a dead address costs one timeout.
- **Local candidates:** loopback spellings of 11434, `OLLAMA_HOST`, LM Studio's port
  from `http-server-config.json` (follow `~/.lmstudio-home-pointer`), default 1234;
  `docker ps`/`podman ps` published ports; inside WSL/containers add the default
  gateway and `host.docker.internal`. Spellings of one port collapse to one server.
- **oMLX/MTPLX ports and keys** come from their own settings files on this computer;
  a key is prompted only when adding the machine remotely. `OMLX_API_KEY` /
  `MTPLX_API_KEY` are fallbacks. Port 8000 is probed only on local macOS (oMLX /
  MTPLX share it by default).
- **Remote hosts** (saved in `~/.kabby.json` as `{address, name?}`): a bare host tries
  both usual ports; `host:port` or a URL names one server. Hosts are probed with the
  local ones at startup (1.5 s timeout); startup search ends early once the remembered
  model appears; local models win when nothing is remembered.
- **"Search my network"** runs only on demand (never at startup, never headless):
  TCP-connect sweep of private IPv4 subnets, 64 at a time, 350 ms timeout, both ports;
  > /22 narrows to the /24 around this machine. Found machines show name, IP, backend,
  model count; the machine name is saved when it round-trips.
- **Chat-only marking:** models without tool support are listed but get the chat-only
  prompt and no tools.

### 3.2 Providers (`internal/providers`)

- **Ollama:** native `/api/chat` with tools, thinking, `keep_alive` (30 min,
  overridable via `KABBY_KEEP_ALIVE`), usage + timing fields; `/api/ps` for loaded
  context; `num_ctx` sent on explicit `--ctx`.
- **LM Studio:** native `/api/v1/models` catalog (supported effort levels + default),
  OpenAI-compatible streaming with tools; native load API when an unloaded model gets
  explicit `--ctx`; never reload an already-loaded model just to enlarge the window.
- **Reasoning effort mapping:** `off` ⇒ `none` on LM Studio, `no_think`-style switch
  on Ollama; snap requested level to the model's actual supported set; one saved
  `effort` in config (known wart — see §7).
- **Transport limits:** 3-minute silence timeout, 15-minute total deadline, ≤3
  transient retries with cancellable backoff, one context-overflow recovery via forced
  compaction, repeated empty-response stop, no partial tool-call execution.
- **Reasoning-exhaustion recovery:** if thinking consumed the whole reply, retry once
  with thinking off; repeat escalates the interval (1 → 2 → max 4 → max 8 responses),
  then retries the session's chosen effort without changing the preference.

### 3.3 Tools (`internal/tools`)

- **Eight tools, two tiers, flat schemas:**
  - read-only (`ro`): `read_file`, `list_files`, `search`, `plan`
  - edit/bypass adds: `write_file`, `edit_file`, `run_command`, `task`
- Every schema: flat parameters (no nested objects/arrays), an example call in the
  description, bounded returns (`read_file` paginates with a continuation line;
  results pass `truncateMiddle` at a 10k-char final cap; `search` runs bounded in a
  worker/goroutine).
- `edit_file` failures return a bounded source range + guidance; shell pipelines
  preserve upstream exit codes; long logs keep beginning and final error.
- `run_command` waits (120 s); long-lived processes go through `task` start/
  list/logs/stop; all tasks killed on exit and on SIGTERM/SIGHUP/SIGINT.
- **Post-write hook:** `write_file`/`edit_file` on Go/JS/JSON/HTML-inline-script/
  Python run a parse check; first error rides back on the success message with a line
  number.
- `plan` actions: `set` (accepts `\n` **or** semicolon lists; strips bullets/
  numbering; ≤20 steps), `done` (no index = current step; reply restates the next
  step), `add`, `show`, `checkpoint` (1–1000 chars, replaces the current step's note,
  re-injected with the active step). After four non-plan tool calls with unfinished
  work, attach a short reminder to the tool result.

### 3.4 Sandbox / modes (`internal/sandbox`)

- All paths resolve inside the workspace; resolution errors become tool-error strings,
  never crashes.
- Mode gate: `ro` sends only read tools (and the agent rechecks mode at execution
  time); `edit` runs commands freely *inside* the workspace and text-checks for
  reach-out (home dir, other folders, global installs) → y/n approval prompt stating
  why; `bypass` never asks.

### 3.5 Context management (`internal/context`, `internal/history`)

- **Budget:** `reply_reserve = clamp(⌊window/4⌋, 128, 8192)`,
  `safety = min(256, ⌊window·0.05⌋)`, input budget = window − reserve − safety.
  Counts anchored by real backend usage after each response; estimates between.
- **Tier 1 eviction** (trigger: >80% of the input budget; target: back to 60%): drop
  old reasoning, applied write bodies (kept as marked records), old tool output;
  replace stale reads of overwritten files with stubs immediately; newest tool group
  protected; repeated-read guard counts only evidence still in context.
- **Tier 2 handover:** harness facts section (current + original request, plan first,
  `filesTouched`, recent command outcomes) + a model-written narrative (thinking off,
  no tools, ≤700 tokens, ≤45 s, three headings: In progress / Next / Notes, character
  cap). On failure or tiny windows: facts-only fallback. Whole groups are trimmed above
  the hard budget; futile repeated summaries suppressed while the final fit check stays
  active.
- **Background handover:** at >60% with enough history, during a still-running
  `run_command` (past 750 ms), snapshot the frozen prefix and prepare the handover on
  the same model; reuse it only if the prefix is unchanged, the result is smaller, and
  the merged prompt fits 80%; otherwise cancel and release the inference slot before
  coding resumes.
- `/compact` forces compaction below the threshold; a window that cannot even hold the
  instructions yields an actionable error, never a lie.
- If reasoning exhausts the reply repeatedly, announce one response with thinking
  disabled and use the escalation ladder from §3.2.

### 3.6 Agent loop & checks (`internal/agent`)

- Turns: system prompt + AGENTS.md (message[0], survives compaction) + budgeted
  history ⇒ model ⇒ execute returned tool calls (bounded results ride back) ⇒ repeat
  until final answer.
- **Project checks:** after file edits, run available project scripts `build`,
  `test`, `test:e2e` (Go: `go build ./...`, `go test ./...`); also every 24 tool calls
  during longer implementations; not rerun for unrelated Q&A.
- **Acceptance gate (`--verify`):** caller-owned command must pass before the run
  counts done; failures feed back as repair evidence with the original request + plan +
  actual failure emphasized; 2 consecutive *equivalent* failures (numbers/whitespace
  insensitive) in a fresh conversation trigger a facts-only compaction even under 80%;
  attempts budget 6 (12 via `--verify-attempts`); each check has the normal 120 s
  deadline; exhaustion or cancellation exits nonzero. Seed recovery from repeated tool
  failures with a known failing check when available. Chat-only models refuse
  immediately.
- **Reminder nudges:** plan reminders on tool results (§3.3), bounded continuation
  nudge on a premature final answer, coach-not-scold wording everywhere (e.g.
  oversized `write_file` ⇒ "not executed; write it in parts").
- **Stats:** every turn ends with tool count, tokens, tok/s; headless emits a single
  `[stats] {…}` JSON line on stderr for benchmarks.

### 3.7 Config & memory (`internal/config`, `internal/prompt`)

- `~/.kabby.json`, mode 0600, holds `lastModel` **+ `lastModelUrl`** (same model id on
  two machines stays apart), `lastMode` (bypass reset to edit), `effort`, `hosts[]`,
  `keys{}` (kept even when the file pre-existed; chmod before the first key write),
  and `UpdateConfig` merges by re-reading (web sessions share the file).
- `AGENTS.md` at the workspace root loads into message[0] after the system prompt,
  capped at 8000 chars (~2k tokens), chat-only-safe.

### 3.8 Interactive TUI (`tui/`, `internal/session`, `internal/ui`)

- Raw-mode inline UI (logo, input editor, status line: mode · model · effort · context
  meter · background tasks). Keys: `/` opens the slash menu with autocomplete,
  `shift+tab` cycles ro → edit → bypass, `esc` cancels the running turn then clears
  input, `ctrl+c`×2 quits.
- Slash commands: `/models` (switch, add machines, network search/address entry,
  rename/remove/rediscover hosts), `/mode`, `/effort` (LM Studio levels read from the
  model), `/plan`, `/context`, `/compact`, `/tasks`, `/logs <id>`, `/stop <id>`,
  `/clear`, `/help`, `/exit`.
- No local server found ⇒ open the TUI early and offer network search instead of
  exiting; esc/ctrl+c while searching quits (exit 130).
- Shell approval prompts render inline; killing signals restore the terminal from raw
  mode and kill background tasks.

### 3.9 Web hub (`web/`)

- `kabby --web [port]` (default 7433), localhost-only, URL carries a random token; a
  second `kabby --web` hands its folder to the running hub and exits.
- Sidebar of workspaces → sessions: status dots, new/close/delete/rename, multiple
  sessions concurrently (queued per server URL), transcripts persisted under
  `~/.kabby/sessions/` and resumable after restart; model-written session titles
  replace the verbatim first message (thinking off, one call, manual renames never
  overwritten).
- Chat supports markdown, paste/drop/paperclip attachments (images to vision models
  with a capability warning; text files inlined), browser + terminal panels (persistent
  shell, no PTY).
- Restored sessions repair interrupted tool conversations, retire stale approval
  buttons, and require inspecting an interrupted command before retry.
- Launching from home or a drive root opens the hub with an empty sidebar and a folder
  picker; from a real project, auto-starts a session.
- Port conflict: EADDRINUSE → error with suggestion to pick another port; exit 1.

### 3.10 Headless (`-p`)

- `kabby -p "prompt"` runs one turn and exits (nonzero on failure/incomplete);
  `--verify` only valid with `-p`; `--web`+`-p` rejected; headless never sweeps the
  network, only uses saved machines.
- Exit codes: 0 completed, 1 incomplete/failure, 130 cancelled.

### 3.11 CLI flag & env contract (exact)

| Flag / env | Meaning |
|---|---|
| `[workspace]` | project folder; defaults to cwd |
| `-m, --mode <ro|edit|bypass>` | permission mode |
| `--model <name>` | pick model by partial name |
| `--ctx <tokens>` | force context window (≥1024) |
| `--verify <command>` | headless acceptance gate (requires `-p`) |
| `--verify-attempts <n>` | max acceptance checks (default 6) |
| `--effort <off\|low\|medium\|high\|default>` | reasoning effort |
| `--web [port]` | browser UI (default 7433) |
| `-p, --print "<prompt>"` | headless: one prompt, exit |
| `-h, --help` / `-v, --version` | help / version |
| `KABBY_CONFIG` | alternate config path for tests |
| `KABBY_KEEP_ALIVE` | Ollama keep-alive override (minutes) |
| `OLLAMA_HOST` | Ollama base URL |
| `OMLX_API_KEY` / `MTPLX_API_KEY` | fallback API keys |


---

## 4. Step-by-step implementation (Go)

Replace npm tooling with Go tooling: `go mod init kabby`, `go build -o kabby
./cmd/kabby`, `go test ./...`, `go vet ./...`. A `Makefile` (or `justfile`) may wrap
these as `make build`, `make test`, `make dev`.

### Phase 0 — Project shell

1. `go.mod`: module `kabby`, go `1.22`.
2. `go.sum` (generated by `go mod tidy`).
3. `Makefile`: targets `build`, `test`, `dev`, `clean`, `prepublishOnly` (runs
   `go vet ./... && go test ./...`).
4. `.gitignore`, `LICENSE` (MIT), `README.md` mirroring v0.7.1 usage with `kabby`.
5. `cmd/kabby/main.go`: stub `parseArgs`, help/version, three run path placeholders.
6. **AC:** `go build ./...` emits the `kabby` binary; `go test ./...` passes an empty
   suite; `kabby --help` / `kabby --version` print.

### Phase 1 — Utilities and primitives

1. `internal/util.go`: ANSI color helpers, `truncateMiddle(str, cap)`, small shared
   types.
2. `internal/events.go`: `EventBus` (subscribe/unsubscribe/emit typed events like
   `compaction`).
3. `internal/ui.go`: `UI` type — `Println`/`Status`/`Warn`/`Error`/`Close`, no-op
   close usable by TUI, web, headless.
4. Tests: `internal/util_test.go` (truncate edge cases), `internal/events_test.go`
   (fan-out, once semantics).
5. **AC:** `go test ./...` green; truncate never exceeds cap; event order preserved.

### Phase 2 — Sandbox and file tools

1. `internal/sandbox.go`: `ResolveInWorkspace(workspace, rel)` → abs path with traversal
   rejection (`..`, absolute, symlink escape); `SandboxError` type; command reach-out
   text classification.
2. `internal/tools/fs.go`: `read_file` (paged: offset/limit, continuation line),
   `list_files` (folders end with `/`), `write_file` (mkdir parents), `edit_file`
   (exact old_text match → bounded error with source range + continuation args).
3. Bound every tool output (per-tool cap + `TOOL_RESULT_CAP` 10k final).
4. Tests: `internal/sandbox_test.go` (escape attempts, symlink sanity),
   `internal/tools/edit_feedback_test.go` (failure message shape, pipeline exit codes).
5. **AC:** no path escapes workspace; edit failure returns bounded source range.

### Phase 3 — Plan state

1. `internal/plan.go`: exactly per §3.3 — `Set` (strip bullets, both separators, ≤20
   steps), `MarkDone` (no index = current; reply restates next step), `Add`, `Show`,
   `Checkpoint` (1–1000 chars replaces current step note), `ModelView`, `CompactLine`,
   `PendingSummary`. Every action returns a small self-teaching string with an example.
2. Wire the plan-reminder constant (attach after 4 non-plan calls with unfinished
   work) for the agent loop.
3. Tests: `internal/plan_test.go` (semicolon lists, numbered stripping, done/next
   replies, checkpoint bounds, 20-step cap).
4. **AC:** `Set` accepts both `\n` and `;` lists; `MarkDone` with no args advances the
   current step; checkpoint rejects empty/>1000 chars.

### Phase 4 — Shell and background tasks

1. `internal/tools/shell.go`: `PickShell()` per OS; `RunCommand(command, workspace,
   signal)` with cwd=workspace, 120 s timeout, upstream-preserved pipelines, beginning
   + error-tail retention.
2. `internal/tools/tasks.go`: `TaskManager` — `Start` (early output detection),
   `List`/`Logs`/`Stop`, ring-buffered output, `KillAll()` for exit + signals.
3. Tests: `internal/tools/process_lifecycle_test.go`, `internal/tools/isolation_test.go`
   (workspace confinement of spawned processes).
4. **AC:** background task survives `ctrl+c` until explicit stop; `KillAll` stops all.

### Phase 5 — Tool registry and syntax check

1. `internal/tools/check.go`: post-write parse check for Go/JS/JSON/HTML-inline-script/
   Python (use `go/parser` for Go files; `encoding/json`, `html/template` or regexp for
   others as appropriate).
2. `internal/tools/index.go`: build the 4+4 `ToolSpec`s per mode with flat schemas and
   example-call descriptions; `ExecuteTool` dispatch with per-request char cap;
   `AfterWrite` hooks the syntax check; `CommandOf` exposes exec strings for the
   approval gate.
3. Tests: `internal/tools/check_test.go`; registry tests (ro sends exactly 4 tools;
   edit sends 8).
4. **AC:** ro mode tool list = exactly `[read_file, list_files, search, plan]`;
   edit adds `write_file`, `edit_file`, `run_command`, `task`.

### Phase 6 — Providers

1. `internal/providers/types.go`: `ToolSpec`, `Effort`, capability/model info.
2. `internal/providers/transport.go`: `net/http` wrapper — silence/total timeouts
   (3 min / 15 min), ≤3 transient retries with cancellable backoff, abort via
   `context.Context`.
3. `internal/providers/ollama.go` and `internal/providers/lmstudio.go` per §3.2
   (chat + streaming with tools, usage anchoring, loaded-context query, effort
   mapping). Stream parsing must buffer until complete before any tool call is
   dispatched — no partial execution on truncated streams.
4. `internal/providers/scheduler.go`: one inference slot per server URL; foreground
   priority; background callers defer or cancel.
5. Tests: `internal/providers/providers_test.go` with an `httptest.NewServer` stub
   transport (stream parsing, tool-call extraction, no partial execution, effort
   snapping).
6. **AC:** a truncated stream never executes partial tool calls; effort `high` snaps to
   the model's supported maximum.

### Phase 7 — Discovery

1. `internal/detect.go` (local candidates + docker + WSL/container gateways),
   `internal/omlx.go`/`internal/mtplx.go` (health identities, settings-file port/key
   reading), `internal/hosts.go`/`internal/network.go` (saved remote hosts, probes),
   `internal/netscan.go` (on-demand TCP sweep, name round-trip).
2. Tests: `internal/detect_test.go`, `internal/omlx_test.go`, `internal/mtplx_test.go`,
   `internal/network_test.go` against recorded responses; port 8000 probed only on
   local macOS (`runtime.GOOS == "darwin"`).
3. **AC:** `/api/tags` identifies Ollama regardless of port; a dead address costs one
   timeout and never crashes startup.

### Phase 8 — Config and prompts

1. `internal/config.go` per §3.7 (0600 handling, bypass reset to edit, key filtering,
   `UpdateConfig` merge by re-reading).
2. `internal/prompt.go`: system prompt per mode + chat-only variant + AGENTS.md
   injection (8000-char cap).
3. Tests: `internal/config/api_key_test.go`, `internal/prompt/chat_only_test.go`,
   `internal/prompt/branding_test.go`; config round-trips against `KABBY_CONFIG`
   scratch files.
4. **AC:** config file mode 0600; saved `bypass` coerces to `edit` on load; AGENTS.md
   truncated at 8000 chars.

### Phase 9 — Context management

1. `internal/context.go`: budget math (§3.5), estimate calibration from observed usage,
   tier-1 eviction with newest-group protection and read-stub replacement, tier-2
   handover builder (facts first; narrative call on scheduler with thinking off and
   caps), facts-only fallback, final fit gate.
2. `internal/history.go`: transcript structure supporting group removal and marked
   write-body records.
3. Tests: `internal/context_test.go`, `internal/read_budget_test.go`,
   `internal/history_test.go`, `internal/recovery_test.go` (equivalent-failure facts
   compaction, repeat detection ignoring numbers/whitespace).
4. **AC:** budget math correct at 4k / 16k windows; eviction targets 60%; `/compact`
   below threshold still works; impossible window → actionable error.

### Phase 10 — Agent loop

1. `internal/agent.go`: turn loop, tool batch execution with mode recheck, project
   checks after edits and every 24 calls, `--verify` acceptance state machine (§3.6)
   with evidence preservation and attempt budget, reminder nudges, per-turn stats
   (`LastTurnStats`, `Outcome`, `VerificationResult`).
2. Tests: `internal/agent_test.go` (happy path, repeated-tool recovery, acceptance
   exhaustion → nonzero), `internal/verification_test.go` with scripted providers.
3. **AC:** `--verify` acceptance passes → exit 0; chat-only model + `--verify` → exit
   1 before first model call; equivalent failures trigger facts-only compaction.

### Phase 11 — Session glue + headless

1. `internal/session.go`: model prep/selection by partial name, chat-only warnings,
   effort advice, `NoBackendsMessage`, session line format.
2. `cmd/kabby/main.go` headless arm: `parseArgs` (§3.11), `--verify` validation
   (requires `-p`, rejects `--web`), signal cleanup, `[stats]` line on stderr, exit
   codes, TUI-required terminal check.
3. Tests: `internal/capability_test.go`; end-to-end `-p` against the stub model
   (completed/incomplete exit semantics).
4. **AC:** `kabby -p "x" --verify "true"` exits 0; `kabby --verify "x"` without `-p`
   errors; `kabby -p "x" --web` errors.

### Phase 12 — Terminal TUI

1. `tui/keys.go` + `tui/editor.go`: raw-mode input editor (word-wrapping input,
   history), key semantics (`/`, `shift+tab`, `esc`, ctrl+c×2).
2. `tui/tui.go`: render loop (logo via `internal/logo.go` or embedded, transcript
   print, status line with context meter and task badges, slash menu + autocomplete,
   approval prompt, esc-cancel mid-stream). `internal/session.go` binds slash commands,
   model picker (local list, add-machine flow, network search/address), and the
   no-local-server offer.
3. Tests: `tui/tui_test.go` with a puppeted terminal (pty or pseudo-TTY).
4. **AC:** `shift+tab` cycles ro → edit → bypass; `esc` cancels a running turn then
   clears input; `ctrl+c`×2 quits; no local server → TUI opens with network-search
   offer instead of an error dump.

### Phase 13 — Web hub

1. `web/store.go` + config wiring: transcripts under `~/.kabby/sessions/`, hub record
   for join-detection (`ReadHubRecord`/`PingHub`/`AskHubToOpen`).
2. `web/hub.go`: HTTP + SSE server, token auth, workspace/session model, inference
   slotting across sessions, `OpenSession`.
3. `web/page.go`/`web/styles.go`/`web/client.go`: chat UI, markdown, sidebar, status,
   model picker, approval buttons, `/`-commands parity.
4. `web/terminal.go`, `web/attachments.go`: panel shell (no PTY), paste/drop/paperclip
   with vision-capability warning; session restore rules (§3.9).
5. Tests: `web/web_test.go`, `web/rename_test.go`, `web/attachments_test.go`,
   `web/rtl_test.go`; plus browser bench scripts in `bench/`.
6. **AC:** second `kabby --web` joins the running hub; port conflict suggests another
   port; restored session retires stale approval buttons.

### Phase 14 — Benchmarks, docs, release

1. `bench/run.sh` + scenario scripts (compact smoke, lifecycle, acceptance, browser
   checks) keyed off the `[stats]` line.
2. Update `README.md`, docs (`how-it-works.md`, `backend-notes.md`, audit/lifecycle
   records), and this SPECS.md.
3. **AC:** `prepublishOnly` = test suite; `go build -o kabby ./cmd/kabby` produces a
   single binary; version bump + changelog.

---

## 5. Test harness (§6 reference)

For provider/model-server-dependent phases (6, 7, 10, 11), implement a **stub
transport** in `internal/providers/testdata/` (or `test/`) using
`net/http/httptest.NewServer` that:

- returns canned `/api/tags`, `/api/v1/models`, `/health` responses,
- streams a canned chat response with tool calls (optionally truncated mid-stream),
- reports loaded context via Ollama `/api/ps`,
- fails on demand (timeout, empty stream, context overflow).

Recorded responses for discovery tests live in `internal/detect/testdata/` (one JSON
file per backend/endpoint). Tests never touch the real `~/.kabby.json` — they use
`KABBY_CONFIG` pointing at a temp file (`t.TempDir()`).

---

## 6. Cross-cutting test matrix

| Area | Files | Non-negotiables |
|---|---|---|
| Sandbox | `internal/sandbox_test.go`, `internal/tools/isolation_test.go` | No escape via `..`, absolute paths, shell reach-out detection |
| Context | `internal/context_test.go`, `internal/read_budget_test.go` | Budget math at 4k/16k, eviction targets 60%, handover caps |
| Recovery | `internal/recovery_test.go` | Equivalent-failure detection, attempt limits respected |
| Plan | `internal/plan_test.go` | Survival through compaction, ≤20 steps |
| Providers | `internal/providers/providers_test.go` | No partial tool execution; effort snapping |
| Web | `web/web_test.go` | Token-gated, transcript resume, approval retirement |
| CLI | `internal/capability_test.go` | Flag validation, exit codes, `[stats]` shape |

---

## 7. Outstanding work (from TODO.md, restated as tasks)

1. **Human pass on the terminal TUI** — drive logo, slash menu, shift+tab, pickers,
   approval prompt, esc-cancel live on both Ollama and LM Studio; file findings in
   `docs/`.
2. **Plan persistence across sessions** — decide the store (workspace file vs
   `~/.kabby/`), schema (`{steps:[{text,done,note}]}`), and restore rules (offer,
   don't auto-load; `/clear` clears it). Reuses `Plan` untouched — the behavior is
   already exactly what web save/restore does.
3. **Per-backend effort memory** — replace `config.effort` with
   `effort: { ollama?: Effort, lmstudio?: Effort }`, migration `cfg.effort → both`,
   keybind `/effort` writes only the active backend's slot; a must because LM Studio's
   `high` snaps to `medium` while Ollama maps to "thinking on".
4. **First release** — verify binary-only distribution, `go build` reproducibility,
   `prepublishOnly` gate, tag `v0.7.1`.

---

## 8. Release checklist

- [ ] `go test ./...` green on Go 1.22 (and Go 1.21 if supported).
- [ ] Headless smoke on both backends: `bench/run.sh mc-compact <model> off --ctx 12000`
    completes with the plan intact.
- [ ] `[stats]` JSON line parses and carries the fields in §3.6.
- [ ] No `bypass` mode persisted to `~/.kabby.json` after a bypass session.
- [ ] Fresh-machine scenario: no server running → model picker offer, not an
    error dump.
- [ ] Single static binary per target (GOOS/GOARCH: linux/amd64, darwin/amd64,
    darwin/arm64, windows/amd64) via `CGO_ENABLED=0 go build`; each runs on a clean
    machine with no Go runtime installed.
- [ ] `README.md` version-accurate (commands, options, ports table).
- [ ] All Phase 0–14 ACs pass without a live model server.

---

## 9. Decisions (answered)

| Decision | Value |
|---|---|
| Go module path | `kabby` |
| Minimum Go version | 1.22 |
| Build-time dependencies | Third-party OK (compiled into static binary; e.g. bubbletea for TUI) |
| Web UI | Server-rendered HTML + SSE, Go only |
| Release targets | linux/amd64, darwin/amd64, darwin/arm64, windows/amd64 (`CGO_ENABLED=0`) |
| Legacy SMOLCODER env aliases | Dropped (`KABBY_*` only) |
| Config | `~/.kabby.json`, JSON, mode 0600 (unchanged from SPECS2.md) |
| Testing | Go stdlib `testing` + `net/http/httptest` stubs |

These decisions are now baked into SPECS.md; reopen this section if any needs revisiting.