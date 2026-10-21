# SPECS.md — kabby

Build specification for **kabby**: a zero-config CLI coding agent for local models
(Ollama, LM Studio, oMLX, MTPLX), shipped as a single statically-linked Go binary.

kabby began as a TypeScript/Node codebase (v0.7.1). This spec is the Go rewrite of
that design. Product behavior, invariants, and acceptance criteria carry over
unchanged; only the language, layout, and tooling differ. The original Node spec is
in git history (`SPECS2.md`, removed in the consolidation commit).

## 0. How to use this spec

Build in **Phases 0–14 in order**. Each phase:

1. touches only the files listed,
2. ends with a runnable build (`go build ./...`) and green tests (`go test ./...`),
3. lists acceptance criteria (AC) checkable without a live model server.

Tests use Go stdlib `testing` plus `net/http/httptest` stubs. They point at a scratch
config via `KABBY_CONFIG` (a `t.TempDir()` file) so they never touch `~/.kabby.json`.
When a phase depends on a model server, stub it (§5).

---

## 1. Product definition

### 1.1 One-line spec

A Go CLI binary (`kabby`) that detects whatever local model server is already
running, hands it eight small tools, and drives a read → edit → run → verify agent
loop inside the user's project folder — in a terminal TUI, a browser UI (`--web`), or
headless (`-p "prompt"`).

### 1.2 User stories

| As a | I want to | So that |
|---|---|---|
| developer | run `kabby` in any project with a local model server | I get a coding assistant with no account, API key, or config file |
| user | pick `ro` / `edit` / `bypass` mode | I control what files and commands the agent may touch |
| team | run `kabby --web` | multiple people can share a browser UI with separate workspaces |
| CI script | run `kabby -p "…" --verify "go test ./..."` | the agent self-repairs until acceptance passes or exits nonzero |
| local-model user | use Ollama / LM Studio / oMLX / MTPLX on another machine | my laptop doesn't need a GPU |

### 1.3 Goals

- **Zero config.** Install the binary, then `kabby` works with no account, no API key
  (except oMLX/MTPLX's own), and no config file. Server discovery, model pick, and
  permission defaults are automatic.
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

- **Static binary, no runtime deps.** Build with `CGO_ENABLED=0`. Third-party modules
  are allowed at build time only (currently `golang.org/x/term`).
- Go version: whatever the `go` directive in `go.mod` says (currently 1.26).
- Every command and file access is confined to the workspace by path resolution
  (`internal/sandbox`); anything resolving outside requires approval in `edit` mode.
- `bypass` mode is never persisted across sessions (`LoadConfig` silently coerces a
  saved `bypass`/`yolo` back to `edit`).

---

## 2. Architecture map

```
cmd/kabby/            CLI entry: parseArgs, help/version, three run paths
                      (headless / web / TUI)
internal/agent/       THE agent loop: tool-call batching, checks, recovery,
                      verify gate, stats
internal/context/     context.go  window budgeting, tier-1 eviction, tier-2 handover
                      history.go  transcript model, append/evict bookkeeping
internal/prompt/      System prompt builder + AGENTS.md loader (8k char cap)
internal/plan/        Harness-held checklist (≤20 steps, per-step note ≤1000 chars)
internal/tools/       index.go    registry: 4 read tools + 4 write/exec tools by mode
                      tool.go     ToolContext and shared tool types
                      fs.go       read_file, list_files, write_file, edit_file
                      search.go   bounded search
                      shell.go    run_command (120 s timeout), PickShell
                      tasks.go    background tasks (start/list/logs/stop, KillAll)
                      check.go    post-write syntax check (Go/JS/JSON/HTML/Python)
internal/sandbox/     Path confinement + command reach-out text check + approval
internal/providers/   types, transport, ollama, lmstudio, scheduler
internal/detect/      detect.go   local discovery (ports, docker, WSL/container gateways)
                      hosts.go / network.go   saved remote machines + probes
                      netscan.go  on-demand TCP subnet sweep
                      omlx.go / mtplx.go      health identity, keys, ports from settings
internal/config/      ~/.kabby.json (lastModel+URL, mode, effort, hosts, keys; 0600)
internal/session/     Model prep/selection, prefs, advice lines, slash-command glue
internal/events/      EventBus (compaction notices etc.)
internal/ui/          Common print/status/warn sink used by all surfaces
internal/util/        ANSI helpers, truncateMiddle
tui/                  Raw-mode terminal UI: tui.go, editor.go, keys.go
web/                  hub (SSE server), client, page, styles, store
bench/                run.sh scenarios keyed off the [stats] line
```

### 2.1 Type contracts

```go
// internal/providers/types.go
type Mode string // "ro" | "edit" | "bypass"
type Effort string // "off" | "low" | "medium" | "high"; "" = backend default

type ToolSpec struct {
    Name        string
    Description string
    Parameters  any // flat schema: no nested objects/arrays
}

type ModelInfo struct {
    ID, Name        string
    Backend         string // ollama | lmstudio | omlx | mtplx
    URL             string
    ToolsSupported  bool
    ContextWindow   int
    SupportedEffort []Effort
    DefaultEffort   Effort
}

// internal/plan/plan.go
type PlanStep struct { Text string; Done bool; Note string /* ≤1000 chars */ }

func New() *Plan
func (p *Plan) Exists() bool
func (p *Plan) DoneCount() int
func (p *Plan) CurrentIndex() int
func (p *Plan) Reset()
func (p *Plan) Set(stepsText string) string     // "Plan set (N steps). Current: 1. ..."
func (p *Plan) MarkDone(stepNumber *int) string // nil ⇒ current step; restates next
func (p *Plan) Add(text string) string
func (p *Plan) Show() string
func (p *Plan) Checkpoint(text string) string   // 1–1000 chars, replaces current note
func (p *Plan) ModelView() string
func (p *Plan) CompactLine() (string, bool)
func (p *Plan) PendingSummary() string

// internal/config/config.go
type Config struct {
    LastModel    string
    LastModelURL string // same model id on two machines stays apart
    LastMode     Mode
    Effort       *Effort
    Hosts        []SavedHost
    Keys         map[string]string
}
type SavedHost struct { Address, Name string }

func SetConfigPath(path string) // "" ⇒ ~/.kabby.json; set from KABBY_CONFIG
func LoadConfig() (Config, error)
func SaveConfig(cfg Config) error                            // chmod 0600 before first key write
func UpdateConfig(patch func(Config) Config) (Config, error) // re-reads, then merges
func SavedKey(base string) (string, bool)
func SetKeys(bases []string, key string) error

// internal/session/session.go
type SessionPrefs struct { Mode Mode; Model string; Ctx int; Effort *Effort }

func PrepareModel(prefs SessionPrefs, cfg config.Config, onLabel func(string)) (*BackendHandle, error)
func EffortAdvice(chosen, setting Effort) string
func NoBackendsMessage() string
func SessionLine(chosen ModelInfo, mode Mode) string

// internal/tools
type ToolContext struct {
    Workspace       string
    TaskManager     *TaskManager
    Plan            *plan.Plan
    FilesTouched    map[string]bool
    CommandsRun     []string
    ResultCharLimit int
}

func BuildToolSpecs(mode providers.Mode) []providers.ToolSpec
func ExecuteTool(name string, args map[string]any, ctx *ToolContext, signal <-chan struct{}) (string, error)
func AfterWrite(path, content string) string
func CommandOf(name string, args map[string]any) *string // nil ⇒ not an exec tool
```

### 2.2 Key invariants

1. **The plan never enters the model transcript.** It is harness state (`Plan`); the
   model sees only a bounded re-injection, the user sees it free of tokens, compaction
   cannot destroy it.
2. **Coding inference always wins the backend.** Background work (handover summaries,
   session titles) yields/defers to a foreground call — one inference slot per server
   URL per process (`internal/providers/scheduler.go`).
3. **No fabricated history.** Applied `write_file` bodies become marked harness
   records, not re-imagined assistant text; malformed/cut-off tool calls are never
   executed partially.
4. **Budget from the real window.** Ask the server what is actually loaded
   (Ollama `/api/ps`, LM Studio load response), budget for that, not the advertised
   max.

---

## 3. Functional requirements by subsystem

### 3.1 Server discovery (`internal/detect`)

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
  `MTPLX_API_KEY` are fallbacks. Port 8000 is probed only on local macOS
  (`runtime.GOOS == "darwin"`; oMLX and MTPLX share it by default).
- **Remote hosts** (saved in `~/.kabby.json` as `{address, name?}`): a bare host tries
  both usual ports; `host:port` or a URL names one server. Hosts are probed with the
  local ones at startup (1.5 s timeout); startup search ends early once the remembered
  model appears; local models win when nothing is remembered.
- **"Search my network"** runs only on demand (never at startup, never headless):
  TCP-connect sweep of private IPv4 subnets, 64 at a time, 350 ms timeout, both ports;
  subnets larger than /22 narrow to the /24 around this machine. Found machines show
  name, IP, backend, model count; the machine name is saved when it round-trips.
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
  on Ollama; snap the requested level to the model's actual supported set; one saved
  `effort` in config (known wart — see §7).
- **Transport limits:** `net/http` with `context.Context` cancellation; 3-minute
  silence timeout, 15-minute total deadline, ≤3 transient retries with cancellable
  backoff, one context-overflow recovery via forced compaction, repeated
  empty-response stop. Streams buffer until complete before any tool call is
  dispatched — no partial tool-call execution.
- **Reasoning-exhaustion recovery:** if thinking consumed the whole reply, retry once
  with thinking off; repeats escalate the interval (1 → 2 → max 4 → max 8 responses),
  then retry the session's chosen effort without changing the preference.

Per-backend detail lives in `docs/backend-notes.md`.

### 3.3 Tools (`internal/tools`)

- **Eight tools, two tiers, flat schemas:**
  - read-only (`ro`): `read_file`, `list_files`, `search`, `plan`
  - edit/bypass adds: `write_file`, `edit_file`, `run_command`, `task`
- Every schema: flat parameters (no nested objects/arrays), an example call in the
  description, bounded returns (`read_file` paginates with a continuation line;
  results pass `truncateMiddle` at a 10k-char final cap, `TOOL_RESULT_CAP`; `search`
  runs bounded in a goroutine).
- `edit_file` failures return a bounded source range + guidance; shell pipelines
  preserve upstream exit codes; long logs keep the beginning and the final error.
- `run_command` waits (120 s); long-lived processes go through `task` start/
  list/logs/stop; all tasks are killed on exit and on SIGTERM/SIGHUP/SIGINT.
- **Post-write hook:** `write_file`/`edit_file` on Go (`go/parser`), JS, JSON,
  HTML-inline-script, and Python run a parse check; the first error rides back on the
  success message with a line number.
- `plan` actions: `set` (accepts `\n` **or** semicolon lists; strips bullets/
  numbering; ≤20 steps), `done` (no index = current step; reply restates the next
  step), `add`, `show`, `checkpoint` (1–1000 chars, replaces the current step's note,
  re-injected with the active step). Every action returns a small self-teaching string
  with an example. After four non-plan tool calls with unfinished work, attach a short
  reminder to the tool result.

### 3.4 Sandbox / modes (`internal/sandbox`)

- All paths resolve inside the workspace (`..`, absolute paths, and symlink escapes
  rejected); resolution errors become tool-error strings, never crashes.
- Mode gate: `ro` sends only read tools (and the agent rechecks mode at execution
  time); `edit` runs commands freely *inside* the workspace and text-checks for
  reach-out (home dir, other folders, global installs) → y/n approval prompt stating
  why; `bypass` never asks.

### 3.5 Context management (`internal/context`)

- **Budget:** `reply_reserve = clamp(⌊window/4⌋, 128, 8192)`,
  `safety = min(256, ⌊window·0.05⌋)`, input budget = window − reserve − safety.
  Counts are anchored by real backend usage after each response; estimates between.
- **Tier 1 eviction** (trigger: >80% of the input budget; target: back to 60%): drop
  old reasoning, applied write bodies (kept as marked records), old tool output;
  replace stale reads of overwritten files with stubs immediately; the newest tool
  group is protected; the repeated-read guard counts only evidence still in context.
- **Tier 2 handover:** harness facts section (current + original request, plan first,
  `filesTouched`, recent command outcomes) + a model-written narrative (thinking off,
  no tools, ≤700 tokens, ≤45 s, three headings: In progress / Next / Notes, character
  cap). On failure or tiny windows: facts-only fallback. Whole groups are trimmed above
  the hard budget; futile repeated summaries are suppressed while the final fit check
  stays active.
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
  until a final answer.
- **Project checks:** after file edits, run the project's build and test (Go:
  `go build ./...`, `go test ./...`; npm projects: scripts `build`, `test`,
  `test:e2e`); also every 24 tool calls during longer implementations; not rerun for
  unrelated Q&A.
- **Acceptance gate (`--verify`):** the caller-owned command must pass before the run
  counts as done; failures feed back as repair evidence with the original request +
  plan + actual failure emphasized; 2 consecutive *equivalent* failures
  (numbers/whitespace insensitive) in a fresh conversation trigger a facts-only
  compaction even under 80%; attempt budget 6 (up to 12 via `--verify-attempts`);
  each check has the normal 120 s deadline; exhaustion or cancellation exits nonzero.
  Seed recovery from repeated tool failures with a known failing check when available.
  Chat-only models refuse immediately.
- **Reminder nudges:** plan reminders on tool results (§3.3), a bounded continuation
  nudge on a premature final answer, coach-not-scold wording everywhere (e.g.
  oversized `write_file` ⇒ "not executed; write it in parts").
- **Stats:** every turn ends with tool count, tokens, tok/s (`LastTurnStats`,
  `Outcome`, `VerificationResult`); headless emits a single `[stats] {…}` JSON line on
  stderr for benchmarks (tools called, input/output/total tokens, tok/s, duration,
  verification status).

### 3.7 Config & memory (`internal/config`, `internal/prompt`)

- `~/.kabby.json`, mode 0600, holds `lastModel` **+ `lastModelUrl`** (same model id on
  two machines stays apart), `lastMode` (bypass reset to edit), `effort`, `hosts[]`,
  `keys{}` (kept even when the file pre-existed; chmod before the first key write).
  `UpdateConfig` merges by re-reading (web sessions share the file). Data dir:
  `~/.kabby/`.
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
  `/clear`, `/stream [on|off]` (print reply tokens as they arrive; on by default;
  off waits for the full reply and renders its markdown), `/help`, `/exit`.
- No local server found ⇒ open the TUI early and offer network search instead of
  exiting; esc/ctrl+c while searching quits (exit 130).
- Shell approval prompts render inline; killing signals restore the terminal from raw
  mode and kill background tasks.

### 3.9 Web hub (`web/`)

- `kabby --web [port]` (default 7433), localhost-only, URL carries a random token; a
  second `kabby --web` hands its folder to the running hub and exits.
- Server-rendered HTML + SSE, Go only.
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
- Port in use → error suggesting another port; exit 1.

### 3.10 Headless (`-p`)

- `kabby -p "prompt"` runs one turn and exits (nonzero on failure/incomplete);
  `--verify` only valid with `-p`; `--web`+`-p` rejected; headless never sweeps the
  network, only uses saved machines.
- Exit codes: 0 completed, 1 incomplete/failure, 130 cancelled.

### 3.11 CLI flag & env contract (exact)

| Flag / env | Meaning |
|---|---|
| `[workspace]` | project folder; defaults to cwd |
| `-m, --mode <ro\|edit\|bypass>` | permission mode |
| `--model <name>` | pick model by partial name |
| `--ctx <tokens>` | force context window (≥1024) |
| `--verify <command>` | headless acceptance gate (requires `-p`) |
| `--verify-attempts <n>` | max acceptance checks (default 6) |
| `--effort <off\|low\|medium\|high\|default>` | reasoning effort |
| `--web [port]` | browser UI (default 7433) |
| `-p, --print "<prompt>"` | headless: one prompt, exit |
| `-h, --help` / `-v, --version` | help / version |
| `KABBY_CONFIG` | alternate config path (tests) |
| `KABBY_KEEP_ALIVE` | Ollama keep-alive override (minutes) |
| `OLLAMA_HOST` | Ollama base URL |
| `OMLX_API_KEY` / `MTPLX_API_KEY` | fallback API keys |

---

## 4. Step-by-step implementation

Tooling: `make build` (`go build -o kabby ./cmd/kabby`), `make test`, `make vet`,
`make prepublishOnly` (vet + test), `make cross` / `make release` (static binaries for
every release target into `dist/`).

### Phase 0 — Project shell

1. `go.mod` (module `kabby`), `go.sum`, `Makefile`, `.gitignore`, `LICENSE` (MIT),
   `README.md`, `install.sh`.
2. `cmd/kabby/main.go`: `parseArgs`, help/version, three run-path stubs.
3. **AC:** `go build ./...` emits the binary; `go test ./...` passes; `kabby --help` /
   `kabby --version` print.

### Phase 1 — Utilities and primitives

1. `internal/util`: ANSI color helpers, `truncateMiddle(str, cap)`.
2. `internal/events`: `EventBus` (subscribe/unsubscribe/emit typed events like
   `compaction`).
3. `internal/ui`: `UI` — `Println`/`Status`/`Warn`/`Error`/`Close`, no-op close
   usable by TUI, web, and headless.
4. Tests: `util_test.go` (truncate edge cases), `events_test.go` (fan-out, once
   semantics).
5. **AC:** truncate never exceeds cap; event order preserved.

### Phase 2 — Sandbox and file tools

1. `internal/sandbox`: `ResolveInWorkspace(workspace, rel)` with traversal rejection;
   `SandboxError`; command reach-out text classification.
2. `internal/tools/fs.go`: `read_file` (paged offset/limit + continuation line),
   `list_files` (folders end with `/`), `write_file` (mkdir parents), `edit_file`
   (exact old_text match → bounded error with source range + continuation args).
3. Bound every tool output (per-tool cap + `TOOL_RESULT_CAP` 10k final).
4. Tests: `sandbox_test.go` (escape attempts, symlink sanity),
   `tools/edit_feedback_test.go` (failure message shape, pipeline exit codes).
5. **AC:** no path escapes the workspace; edit failure returns a bounded source range.

### Phase 3 — Plan state

1. `internal/plan`: exactly per §3.3 and §2.1.
2. Plan-reminder constant (after 4 non-plan calls with unfinished work) for the agent
   loop.
3. Tests: `plan_test.go` (semicolon lists, numbered stripping, done/next replies,
   checkpoint bounds, 20-step cap).
4. **AC:** `Set` accepts both `\n` and `;` lists; `MarkDone(nil)` advances the current
   step; `Checkpoint` rejects empty/>1000 chars.

### Phase 4 — Shell and background tasks

1. `internal/tools/shell.go`: `PickShell()` per OS; `RunCommand(command, workspace,
   signal)` with cwd=workspace, 120 s timeout, upstream-preserved pipelines,
   beginning + error-tail retention.
2. `internal/tools/tasks.go`: `TaskManager` — `Start` (early output detection),
   `List`/`Logs`/`Stop`, ring-buffered output, `KillAll()` for exit + signals.
3. Tests: `tools/process_lifecycle_test.go` (incl. workspace confinement of spawned
   processes).
4. **AC:** a background task survives `ctrl+c` until explicit stop; `KillAll` stops
   all.

### Phase 5 — Tool registry and syntax check

1. `internal/tools/check.go`: post-write parse check per §3.3.
2. `internal/tools/index.go`: `BuildToolSpecs`, `ExecuteTool` (per-request char cap),
   `AfterWrite`, `CommandOf` per §2.1.
3. Tests: `tools/check_test.go`, `tools/registry_test.go`.
4. **AC:** `ro` tool list = exactly `[read_file, list_files, search, plan]`; `edit`
   adds `write_file`, `edit_file`, `run_command`, `task`.

### Phase 6 — Providers

1. `internal/providers`: `types.go`, `transport.go`, `ollama.go`, `lmstudio.go`,
   `scheduler.go` per §3.2 and invariant 2.
2. Tests: `providers_test.go` with an `httptest.NewServer` stub (stream parsing,
   tool-call extraction, no partial execution, effort snapping).
3. **AC:** a truncated stream never executes partial tool calls; effort `high` snaps
   to the model's supported maximum.

### Phase 7 — Discovery

1. `internal/detect`: `detect.go`, `omlx.go`, `mtplx.go`, `hosts.go`, `network.go`,
   `netscan.go` per §3.1.
2. Tests: `detect_test.go` against recorded responses; port 8000 probed only on local
   macOS.
3. **AC:** `/api/tags` identifies Ollama regardless of port; a dead address costs one
   timeout and never crashes startup.

### Phase 8 — Config and prompts

1. `internal/config` per §3.7 (0600, bypass → edit, key filtering, re-read merge).
2. `internal/prompt`: system prompt per mode + chat-only variant + AGENTS.md
   injection (8000-char cap).
3. Tests: `config_test.go` (API keys, round-trips against `KABBY_CONFIG`),
   `prompt_test.go` (chat-only, branding).
4. **AC:** config file mode 0600; saved `bypass` loads as `edit`; AGENTS.md truncated
   at 8000 chars.

### Phase 9 — Context management

1. `internal/context/context.go`: budget math (§3.5), estimate calibration, tier-1
   eviction, tier-2 handover (on the scheduler, thinking off, capped), facts-only
   fallback, final fit gate.
2. `internal/context/history.go`: group removal, marked write-body records.
3. Tests: `context_test.go` (budget, read budget, history, equivalent-failure
   recovery, repeat detection ignoring numbers/whitespace).
4. **AC:** budget math correct at 4k / 16k windows; eviction targets 60%; `/compact`
   below threshold still works; impossible window → actionable error.

### Phase 10 — Agent loop

1. `internal/agent`: turn loop, tool batches with mode recheck, project checks, the
   `--verify` state machine (§3.6), nudges, per-turn stats.
2. Tests: `agent_test.go` with scripted providers (happy path, repeated-tool
   recovery, acceptance pass/exhaustion).
3. **AC:** acceptance passes → exit 0; chat-only model + `--verify` → exit 1 before
   the first model call; equivalent failures trigger facts-only compaction.

### Phase 11 — Session glue + headless

1. `internal/session`: model prep/selection by partial name, chat-only warnings,
   effort advice, `NoBackendsMessage`, session line.
2. `cmd/kabby/main.go` headless arm: flag parsing (§3.11), `--verify` validation,
   signal cleanup, `[stats]` on stderr, exit codes, TUI-requires-terminal check.
3. Tests: `session_test.go`; end-to-end `-p` against the stub model.
4. **AC:** `kabby -p "x" --verify "true"` exits 0; `kabby --verify "x"` without `-p`
   errors; `kabby -p "x" --web` errors.

### Phase 12 — Terminal TUI

1. `tui/keys.go` + `tui/editor.go`: raw-mode input editor (wrapping, history), key
   semantics per §3.8.
2. `tui/tui.go`: render loop (logo, transcript, status line, slash menu +
   autocomplete, approval prompt, esc-cancel mid-stream); `internal/session` binds
   slash commands, the model picker, and the no-local-server offer.
3. Tests: `tui_test.go` with a puppeted terminal.
4. **AC:** `shift+tab` cycles ro → edit → bypass; `esc` cancels a running turn then
   clears input; `ctrl+c`×2 quits; no local server → network-search offer, not an
   error dump.

### Phase 13 — Web hub

1. `web/store.go`: transcripts under `~/.kabby/sessions/`, hub record for join
   detection (`ReadHubRecord`/`PingHub`/`AskHubToOpen`).
2. `web/hub.go`: HTTP + SSE, token auth, workspace/session model, inference slotting,
   `OpenSession`, panel terminal (no PTY), attachments.
3. `web/page.go`/`styles.go`/`client.go`: chat UI, markdown, sidebar, model picker,
   approval buttons, `/`-command parity.
4. Tests: `web_test.go` (token gate, resume, rename, attachments, RTL).
5. **AC:** a second `kabby --web` joins the running hub; port conflict suggests
   another port; a restored session retires stale approval buttons.

### Phase 14 — Benchmarks, docs, release

1. `bench/run.sh` scenarios (`mc-compact`, `lifecycle`, `acceptance`) keyed off the
   `[stats]` line.
2. Keep `README.md`, `docs/how-it-works.md`, `docs/backend-notes.md`, and this spec
   current.
3. **AC:** `make prepublishOnly` passes; `make cross` produces one static binary per
   target.

---

## 5. Test harness

For model-server-dependent phases (6, 7, 10, 11), use an `httptest.NewServer` stub
that:

- returns canned `/api/tags`, `/api/v1/models`, `/health` responses,
- streams a canned chat response with tool calls (optionally truncated mid-stream),
- reports loaded context via Ollama `/api/ps`,
- fails on demand (timeout, empty stream, context overflow).

Recorded discovery responses go in `internal/detect/testdata/` (one JSON file per
backend/endpoint).

---

## 6. Cross-cutting test matrix

| Area | Files | Non-negotiables |
|---|---|---|
| Sandbox | `sandbox/sandbox_test.go`, `tools/process_lifecycle_test.go` | No escape via `..`, absolute paths, symlinks; shell reach-out detection |
| Context | `context/context_test.go` | Budget math at 4k/16k, eviction targets 60%, handover caps |
| Recovery | `context/context_test.go`, `agent/agent_test.go` | Equivalent-failure detection, attempt limits respected |
| Plan | `plan/plan_test.go` | Survives compaction, ≤20 steps |
| Providers | `providers/providers_test.go` | No partial tool execution; effort snapping |
| Web | `web/web_test.go` | Token-gated, transcript resume, approval retirement |
| CLI | `session/session_test.go`, `-p` end-to-end | Flag validation, exit codes, `[stats]` shape |

---

## 7. Outstanding work

1. **Close spec gaps found in the code** (as of v0.7.11):
   - `KABBY_KEEP_ALIVE` is never read, and nothing sends Ollama `keep_alive` (§3.2).
   - The TUI implements only `/help`, `/model`, `/stream`, and `/exit`; §3.8 lists the full set
     (and names it `/models`).
   - `internal/detect/testdata/` does not exist yet (§5).
   - The TUI bypasses the agent loop: `tui.submit` calls the model directly with no
     tools and no history, so the terminal is chat-only (§3.6, §3.8).
   - `web.TestStoreList` is flaky (about 2 runs in 5 fail cleaning up `t.TempDir()`:
     "directory not empty").
2. **Human pass on the terminal TUI** — drive the logo, slash menu, shift+tab,
   pickers, approval prompt, and esc-cancel live on both Ollama and LM Studio; file
   findings in `docs/`.
3. **Plan persistence across sessions** — decide the store (workspace file vs
   `~/.kabby/`), schema (`{steps:[{text,done,note}]}`), and restore rules (offer,
   don't auto-load; `/clear` clears it). Reuses `Plan` untouched — the behavior is
   already exactly what web save/restore does.
4. **Per-backend effort memory** — replace `config.effort` with
   `effort: { ollama?: Effort, lmstudio?: Effort }`, migrating `cfg.effort` into both;
   `/effort` writes only the active backend's slot. Needed because LM Studio's `high`
   snaps to `medium` while Ollama maps it to "thinking on".

### 7.1 Proposed features

Each item is adapted to kabby's goals (small local models, zero
config, privacy). None is implemented yet. Suggested order: F5 → F4 → F2 → the
TUI/agent-loop gap in item 1 → F1 → F3, then the rest as needed. F1, F3, and F7 depend
on the TUI running through the agent loop.

**Strong fits**

- **F1. Steering.** Input typed during a running turn goes into a queue. After each
  tool call finishes, the agent skips the remaining calls in the batch, appends the
  queued message, and calls the model again. Unlike `esc`, the turn keeps its
  progress. Requires the TUI to read keys while a turn runs. *Medium.*
- **F2. `/btw <question>`.** Answer a side question using the current context
  without appending the question or the answer to history, so small windows don't
  spend budget on asides. *Small.*
- **F3. Tool hooks.** An optional external process, exchanging JSON over stdio, is
  consulted at `before_tool`, `after_tool`, and `approve_tool`. It can return
  `continue`, `modify`, `deny_tool`, `respond` (supply the tool result and skip
  execution, which enables plugin tools without growing the built-in eight), or
  `abort_turn`. Off unless configured. *Medium.*
- **F4. Secret filtering in tool results.** Before a tool result reaches the model,
  replace every known secret (keys in `~/.kabby.json`, `OMLX_API_KEY`,
  `MTPLX_API_KEY`) with `[FILTERED]`, using a `strings.Replacer` built once at
  startup and skipping results shorter than 8 chars. *Small.*
- **F5. Slash-command registry.** Each command is defined once (name, description,
  usage, handler); `/help`, slash-menu autocomplete, and web `/`-command parity are
  generated from the registry. Do this before adding the missing §3.8 commands.
  *Small refactor.*
- **F6. Workspace `MEMORY.md`.** A long-term notes file next to `AGENTS.md` that the
  agent can append to, loaded into message[0] under its own char cap. Share the
  storage decision with plan persistence (item 3). *Small.*

**Good fits, with caveats**

- **F7. Light/heavy routing across local models.** Choose between a configured fast
  model and the main model from structural features only, never keywords: message
  length, fenced code blocks, recent tool calls, conversation depth, attachments.
  Caveat: switching models reloads them on Ollama and LM Studio, so route only when
  both are already loaded. *Medium.*
- **F8. Recall of evicted context.** Tier-1 eviction moves dropped messages to a
  per-session store instead of discarding them, and a single `recall` tool fetches
  them by search or ID. Offer the tool only when the window is ≥16k, to keep the tool
  list small for small models. *Medium-large.*
- **F9. Skills.** `SKILL.md` folders in `.kabby/skills/` and `~/.kabby/skills/`. Only
  names and one-line descriptions enter the system prompt, under a hard size cap;
  the full text loads on demand or via `/use <skill>`. *Medium.*
- **F10. Opt-in process isolation (`--isolate`).** Run `run_command` and `task`
  children under bubblewrap on Linux, `sandbox-exec` on macOS, and a restricted token
  plus Job Object on Windows, with `HOME`/`TMPDIR`/XDG dirs redirected. This relaxes
  the §1.4 "no OS isolation" non-goal, so it stays opt-in and default behavior is
  unchanged. *Medium, mostly per-platform testing.*

**Considered and rejected**

- Chat-app channels, cloud providers, OAuth: conflict with the local-only privacy
  goal (§1.3).
- Subagents: a §1.4 non-goal.
- Self-evolution (auto-written skills from past turns): with small models, an easy
  path for bad instructions to persist in the prompt.
- MCP client and cron: MCP tools would swamp the small tool list (F3 covers most
  plugin needs); cron doesn't fit a per-project coding agent.

---

## 8. Release checklist

- [ ] `make prepublishOnly` (vet + test) green.
- [ ] Headless smoke on both backends: `bench/run.sh mc-compact <model> off --ctx 12000`
      completes with the plan intact.
- [ ] `[stats]` JSON line parses and carries the fields in §3.6.
- [ ] No `bypass` mode persisted to `~/.kabby.json` after a bypass session.
- [ ] Fresh machine, no server running → model-picker offer, not an error dump.
- [ ] `make cross`: static binaries for linux/amd64, darwin/amd64, darwin/arm64,
      windows/amd64; each runs on a clean machine with no Go installed.
- [ ] Version bumped in `cmd/kabby/main.go`; tag `vX.Y.Z`; GitHub release carries the
      binaries `install.sh` downloads.
- [ ] `README.md` version-accurate (commands, options, ports table).
- [ ] All Phase 0–14 ACs pass without a live model server.
