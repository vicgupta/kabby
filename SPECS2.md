# SPECS2.md — kabby

Build specification for **kabby**: a zero-dependency-at-runtime, zero-config CLI coding
agent for local models (Ollama, LM Studio, oMLX, MTPLX). This document states what
the product is, how each subsystem must behave, and a concrete step-by-step order in
which to implement and validate it. It reflects the codebase as of v0.7.1, renamed to
`kabby`.

---

## 0. How to use this spec

Build in **Phases 0–14 in order**. Each phase:

1. touches only the files listed,
2. ends with a runnable build (`npm run build`) and green tests (`npm test`),
3. lists acceptance criteria (AC) you can check without a live model server.

Tests use `node:test` with zero runtime deps via `scripts/test.cjs` (`npm test` =
`tsc` then `node scripts/test.cjs`). Test files live in `test/`; point them at a
scratch config via the `KABBY_CONFIG` env var so they never touch `~/.kabby.json`.

Target: every phase AC is machine-checkable against the source tree. When a phase
depends on a provider/model server, stub it locally (see §6 "Test harness").

---

## 1. Product definition

### 1.1 One-line spec

A Node ≥ 18 CLI (`kabby`) that detects whatever local model server is already running,
hands it eight small tools, and drives a read → edit → run → verify agent loop inside
the user's project folder — in a terminal TUI, a browser UI (`--web`), or headless
(`-p "prompt"`).

### 1.2 User stories

| As a | I want to | So that |
|---|---|---|
| developer | run `kabby` in any project with a local model server | I get a coding assistant with no account, API key, or config file |
| user | pick `ro` / `edit` / `bypass` mode | I control what files and commands the agent may touch |
| team | run `kabby --web` | multiple people can share a browser UI with separate workspaces |
| CI script | run `kabby -p "…" --verify "npm test"` | the agent self-repairs until acceptance passes or exits nonzero |
| local-model user | use Ollama / LM Studio / oMLX / MTPLX on another machine | my laptop doesn't need a GPU |

### 1.3 Goals

- **Zero config.** `npm install -g kabby && kabby` works with no account, no API key
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

- Only dev-time dependency: TypeScript. Runtime: `node:` builtins only.
- Every command and file access is confined to the workspace by path resolution
  (`src/sandbox.ts`); anything resolving outside requires approval in `edit` mode.
- `bypass` mode is never persisted across sessions (`loadConfig` silently coerces a
  saved `bypass`/`yolo` back to `edit`).
- Node ≥ 18 (engines field in package.json).

---

## 2. Architecture map

```
src/
  index.ts        CLI entry: parseArgs, help/version, three run paths (headless / web / TUI)
  session.ts      Shared session bootstrap: model prep, prefs, advice lines
  agent.ts        THE agent loop: tool-call batching, checks, recovery, verify gate, stats
  context.ts      Context window budgeting, tier-1 eviction, tier-2 handover compaction
  prompt.ts       System prompt builder + AGENTS.md loader (8k char cap)
  plan.ts         Harness-held checklist (≤20 steps, per-step checkpoint note ≤1000 chars)
  tools/index.ts  Tool registry: 4 read tools + 4 write/exec tools by mode
  tools/fs-tools.ts   read_file, list_files, write_file, edit_file (paged reads, mid-truncate)
  tools/shell.ts      run_command (120s timeout), pickShell
  tools/tasks.ts      background task manager (dev servers: start/list/logs/stop, killAll on exit)
  tools/check.ts      post-write syntax check (JS/JSON/HTML/Python)
  sandbox.ts      Path confinement + command reach-out text check + approval
  providers/      Backend abstraction: types, transport, ollama, lmstudio, scheduler
  detect.ts       Local server discovery (ports, docker ps, WSL/container gateway)
  hosts.ts / network.ts / netscan.ts   Remote machines + TCP subnet sweep
  omlx.ts / mtplx.ts   oMLX/MTPLX specifics (health, keys, ports from their settings)
  config.ts       ~/.kabby.json (lastModel+URL, mode, effort, hosts, keys; chmod 600)
  history.ts      Transcript model, append/evict bookkeeping
  attachments.ts  Image/file attachments for the web UI
  ui.ts           Common print/status/warn sink used by all surfaces
  events.ts       EventBus (compaction notices etc.)
  tui/            Raw-mode terminal UI, editor, key handling
  web/            hub (SSE server), client, page, styles, channel, store, terminal, attachments
```

### 2.1 Type contracts to implement (reference)

```ts
// src/providers/types.ts
export type Mode = "ro" | "edit" | "bypass";
export type Effort = "off" | "low" | "medium" | "high" | null; // null = default
export interface ToolSpec { name: string; description: string; parameters: any; }

// src/plan.ts
export interface PlanStep { text: string; done: boolean; note?: string; }
export class Plan {
  steps: PlanStep[];
  get exists(): boolean; get doneCount(): number; get currentIndex(): number;
  reset(): void;
  set(stepsText: string): string;       // returns "Plan set (N steps). Current: 1. ..."
  markDone(stepNumber?: number): string;
  add(text: string): string;
  checkpoint(text: string): string;
  modelView(): string;
  compactLine(): string | null;
  pendingSummary(): string;
}

// src/config.ts
export interface Config {
  lastModel?: string; lastModelUrl?: string; lastMode?: Mode;
  effort?: Effort | null; hosts?: SavedHost[]; keys?: Record<string,string>;
}
export const CONFIG_PATH: string; export const DATA_DIR: string;
export function loadConfig(): Config; export function saveConfig(cfg: Config): void;
export function updateConfig(patch: Partial<Config>): Config;
export function savedKey(base: string): string | undefined;
export function setKeys(bases: string[], key?: string): void;

// src/tools/index.ts
export interface ToolContext {
  workspace: string; taskManager: TaskManager; plan: Plan;
  filesTouched: Set<string>; commandsRun: string[]; resultCharLimit?: number;
}
export function buildToolSpecs(mode: Mode): ToolSpec[];
export function executeTool(name, args, ctx, signal?): Promise<string>;
export function commandOf(name, args): string | null;

// src/session.ts
export interface SessionPrefs { mode?: Mode; model?: string; ctx?: number; effort?: Effort | null; }
export function prepareModel(prefs, cfg, onLabel?): Promise<{ backend; id; toolsSupported; contextWindow; ... } | null>;
export function effortAdvice(chosen, effortSetting): string | null;
export function noBackendsMessage(): string;
export function sessionLine(chosen, mode): string;
```

### 2.2 Key invariants

1. **The plan never enters the model transcript.** It is harness state (`Plan`); the
   model sees only a bounded re-injection, the user sees it free of tokens, compaction
   cannot destroy it.
2. **Coding inference always wins the backend.** Background work (handover summaries,
   session titles) yields/defers to a foreground call — one inference slot per server
   URL per process (`providers/scheduler.ts`).
3. **No fabricated history.** Applied `write_file` bodies become marked harness
   records, not re-imagined assistant text; malformed/cut-off tool calls are never
   executed partially.
4. **Budget from the real window.** Ask the server what is actually loaded
   (Ollama `/api/ps`, LM Studio load response), budget for that, not the advertised
   max.

---

## 3. Functional requirements by subsystem

### 3.1 Server discovery (`detect.ts`, `hosts.ts`, `netscan.ts`, `omlx.ts`, `mtplx.ts`)

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

### 3.2 Providers (`providers/`)

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

### 3.3 Tools (`tools/`)

- **Eight tools, two tiers, flat schemas:**
  - read-only (`ro`): `read_file`, `list_files`, `search`, `plan`
  - edit/bypass adds: `write_file`, `edit_file`, `run_command`, `task`
- Every schema: flat parameters (no nested objects/arrays), an example call in the
  description, bounded returns (`read_file` paginates with a continuation line;
  results pass `truncateMiddle` at a 10k-char final cap; `search` runs bounded in a
  worker).
- `edit_file` failures return a bounded source range + guidance; shell pipelines
  preserve upstream exit codes; long logs keep beginning and final error.
- `run_command` waits (120 s); long-lived processes go through `task` start/
  list/logs/stop; all tasks killed on exit and on SIGTERM/SIGHUP/SIGINT.
- **Post-write hook:** `write_file`/`edit_file` on JS/JSON/HTML-inline-script/
  Python run a parse check; first error rides back on the success message with a line
  number.
- `plan` actions: `set` (accepts `\n` **or** semicolon lists; strips bullets/
  numbering; ≤20 steps), `done` (no index = current step; reply restates the next
  step), `add`, `show`, `checkpoint` (1–1000 chars, replaces the current step's note,
  re-injected with the active step). After four non-plan tool calls with unfinished
  work, attach a short reminder to the tool result.

### 3.4 Sandbox / modes (`sandbox.ts`)

- All paths resolve inside the workspace; resolution errors become tool-error strings,
  never crashes.
- Mode gate: `ro` sends only read tools (and the agent rechecks mode at execution
  time); `edit` runs commands freely *inside* the workspace and text-checks for
  reach-out (home dir, other folders, global installs) → y/n approval prompt stating
  why; `bypass` never asks.

### 3.5 Context management (`context.ts`, `history.ts`)

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

### 3.6 Agent loop & checks (`agent.ts`)

- Turns: system prompt + AGENTS.md (message[0], survives compaction) + budgeted
  history ⇒ model ⇒ execute returned tool calls (bounded results ride back) ⇒ repeat
  until final answer.
- **Project checks:** after file edits, run available project scripts `build`,
  `test`, `test:e2e`; also every 24 tool calls during longer implementations; not
  rerun for unrelated Q&A.
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

### 3.7 Config & memory (`config.ts`, `prompt.ts`)

- `~/.kabby.json`, mode 0600, holds `lastModel` **+ `lastModelUrl`** (same model id on
  two machines stays apart), `lastMode` (bypass reset to edit), `effort`, `hosts[]`,
  `keys{}` (kept even when the file pre-existed; chmod before the first key write),
  and `updateConfig` merges by re-reading (web sessions share the file).
- `AGENTS.md` at the workspace root loads into message[0] after the system prompt,
  capped at 8000 chars (~2k tokens), chat-only-safe.

### 3.8 Interactive TUI (`tui/`, `session.ts`, `ui.ts`, `logo.ts`)

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
| `SMOLCODER_CONFIG` / `SMOLCODER_KEEP_ALIVE` | legacy aliases, still honored |

---

## 4. Step-by-step implementation

Build in order. Each phase ends runnable and tested. Every phase lists **AC** — do not
proceed until its AC pass.

### Phase 0 — Project shell

1. `package.json`: name `kabby`, version `0.7.1`, bin `{ "kabby": "dist/index.js" }`,
   engines `>=18`, scripts `clean`/`build`/`dev`/`test`/`prepublishOnly` (test runs
   `npm run build && node scripts/test.cjs`), devDeps `@types/node` ^20.14.0 +
   `typescript` ^5.5.0, files `["dist","README.md"]`.
2. `tsconfig.json`: strict, target ES2022, module Node16/NodeNext, outDir `dist`.
3. `.gitignore`, `LICENSE` (MIT), `scripts/test.cjs` (node:test runner globbing
   `test/*.test.js`), `README.md` mirroring v0.7.1 usage with `kabby`.
4. **AC:** `npm run build` emits `dist/`; `npm test` passes an empty suite; `kabby
   --help` / `kabby --version` print.

### Phase 1 — Utilities and primitives

1. `src/util.ts`: ANSI color helpers (`c.bold`, `c.dim`, `c.warn`, `c.error`),
   `truncateMiddle(str, cap)`, small shared types.
2. `src/events.ts`: `EventBus` (subscribe/unsubscribe/emit typed events like
   `compaction`).
3. `src/ui.ts`: `UI` class — `println`/`status`/`warn`/`error`/`close`, no-op close
   usable by TUI, web, headless.
4. Tests: `test/util.test.js` (truncate edge cases), `test/events.test.js`
   (fan-out, once semantics).
5. **AC:** `npm test` green; truncate never exceeds cap; event order preserved.

### Phase 2 — Sandbox and file tools

1. `src/sandbox.ts`: `resolveInWorkspace(workspace, rel)` → abs path with traversal
   rejection (`..`, absolute, symlink escape); `SandboxError` class; command reach-out
   text classification.
2. `src/tools/fs-tools.ts`: `read_file` (paged: offset/limit, continuation line),
   `list_files` (folders end with `/`), `write_file` (mkdir parents), `edit_file`
   (exact old_text match → bounded error with source range + continuation args).
3. Bound every tool output (per-tool cap + `TOOL_RESULT_CAP` 10k final).
4. Tests: `test/sandbox.test.js` (escape attempts, symlink sanity),
   `test/edit-feedback.test.js` (failure message shape, pipeline exit codes).
5. **AC:** no path escapes workspace; edit failure returns bounded source range.

### Phase 3 — Plan state

1. `src/plan.ts`: exactly per §3.3 — `set` (strip bullets, both separators, ≤20 steps),
   `done` (no index = current; reply restates next step), `add`, `show`, `checkpoint`
   (1–1000 chars replaces current step note), `modelView`, `compactLine`,
   `pendingSummary`. Every action returns a small self-teaching string with an example.
2. Wire the plan-reminder constant (attach after 4 non-plan calls with unfinished
   work) for the agent loop.
3. Tests: `test/plan.test.js` (semicolon lists, numbered stripping, done/next replies,
   checkpoint bounds, 20-step cap).
4. **AC:** `set` accepts both `\n` and `;` lists; `markDone` with no args advances the
   current step; checkpoint rejects empty/>1000 chars.

### Phase 4 — Shell and background tasks

1. `src/tools/shell.ts`: `pickShell()` per OS; `runCommand(command, workspace, signal)`
   with cwd=workspace, 120 s timeout, upstream-preserved pipelines, beginning +
   error-tail retention.
2. `src/tools/tasks.ts`: `TaskManager` — `start` (early output detection),
   `list`/`logs`/`stop`, ring-buffered output, `killAll()` for exit + signals.
3. Tests: `test/process-lifecycle.test.js()`, `test/isolation.test.js` (workspace
   confinement of spawned processes).
4. **AC:** background task survives `ctrl+c` until explicit stop; `killAll` stops all.

### Phase 5 — Tool registry and syntax check

1. `src/tools/check.ts`: post-write parse check for JS/JSON/HTML-inline-script/Python.
2. `src/tools/index.ts`: build the 4+4 `ToolSpec`s per mode with flat schemas and
   example-call descriptions; `executeTool` dispatch with per-request char cap;
   `afterWrite` hooks the syntax check; `commandOf` exposes exec strings for the
   approval gate.
3. Tests: `test/check.test.js`; registry tests (ro sends exactly 4 tools; edit sends 8).
4. **AC:** ro mode tool list = exactly `[read_file, list_files, search, plan]`;
   edit adds `write_file`, `edit_file`, `run_command`, `task`.

### Phase 6 — Providers

1. `src/providers/types.ts`: `ToolSpec`, `Effort`, capability/model info.
2. `src/providers/transport.ts`: fetch wrapper — silence/total timeouts (3 min / 15
   min), ≤3 transient retries with cancellable backoff, abort.
3. `src/providers/ollama.ts` and `lmstudio.ts` per §3.2 (chat + streaming with tools,
   usage anchoring, loaded-context query, effort mapping).
4. `src/providers/scheduler.ts`: one inference slot per server URL; foreground priority;
   background callers defer or cancel.
5. Tests: `test/providers.test.js` with a stub transport (stream parsing, tool-call
   extraction, no partial execution on truncated streams, effort snapping).
6. **AC:** a truncated stream never executes partial tool calls; effort `high` snaps to
   the model's supported maximum.

### Phase 7 — Discovery

1. `src/detect.ts` (local candidates + docker + WSL/container gateways),
   `omlx.ts`/`mtplx.ts` (health identities, settings-file port/key reading),
   `hosts.ts`/`network.ts` (saved remote hosts, probes), `netscan.ts` (on-demand TCP
   sweep, name round-trip).
2. Tests: `test/detect.test.js`, `test/omlx.test.js`, `test/mtplx.test.js`,
   `test/network.test.js` against recorded responses; port 8000 probed only on local
   macOS.
3. **AC:** `/api/tags` identifies Ollama regardless of port; a dead address costs one
   timeout and never crashes startup.

### Phase 8 — Config and prompts

1. `src/config.ts` per §3.7 (0600 handling, bypass reset to edit, key filtering,
   `updateConfig` merge by re-reading).
2. `src/prompt.ts`: system prompt per mode + chat-only variant + AGENTS.md injection
   (8000-char cap).
3. Tests: `test/api-key.test.js`, `test/chat-only.test.js`, `test/branding.test.js`,
   config round-trips against `KABBY_CONFIG` scratch files.
4. **AC:** config file mode 0600; saved `bypass` coerces to `edit` on load; AGENTS.md
   truncated at 8000 chars.

### Phase 9 — Context management

1. `src/context.ts`: budget math (§3.5), estimate calibration from observed usage,
   tier-1 eviction with newest-group protection and read-stub replacement, tier-2
   handover builder (facts first; narrative call on scheduler with thinking off and
   caps), facts-only fallback, final fit gate.
2. `src/history.ts`: transcript structure supporting group removal and marked write-body
   records.
3. Tests: `test/context.test.js`, `test/read-budget.test.js`, `test/history.test.js`,
   `test/recovery.test.js` (equivalent-failure facts compaction, repeat detection
   ignoring numbers/whitespace).
4. **AC:** budget math correct at 4k / 16k windows; eviction targets 60%; `/compact`
   below threshold still works; impossible window → actionable error.

### Phase 10 — Agent loop

1. `src/agent.ts`: turn loop, tool batch execution with mode recheck, project checks
   after edits and every 24 calls, `--verify` acceptance state machine (§3.6) with
   evidence preservation and attempt budget, reminder nudges, per-turn stats
   (`lastTurnStats`, `outcome`, `verificationResult`).
2. Tests: `test/agent.test.js` (happy path, repeated-tool recovery, acceptance
   exhaustion → nonzero), `test/verification.test.js` with scripted providers.
3. **AC:** `--verify` acceptance passes → exit 0; chat-only model + `--verify` → exit
   1 before first model call; equivalent failures trigger facts-only compaction.

### Phase 11 — Session glue + headless

1. `src/session.ts`: model prep/selection by partial name, chat-only warnings, effort
   advice, `noBackendsMessage`, session line format.
2. `src/index.ts` headless arm: `parseArgs` (§3.11), `--verify` validation (requires
   `-p`, rejects `--web`), signal cleanup, `[stats]` line on stderr, exit codes,
   TUI-required terminal check.
3. Tests: `test/capability.test.js`; end-to-end `-p` against the stub model
   (completed/incomplete exit semantics).
4. **AC:** `kabby -p "x" --verify "true"` exits 0; `kabby --verify "x"` without `-p`
   errors; `kabby -p "x" --web` errors.

### Phase 12 — Terminal TUI

1. `src/tui/keys.ts` + `editor.ts`: raw-mode input editor (word-wrapping input,
   history), key semantics (`/`, `shift+tab`, `esc`, ctrl+c×2).
2. `src/tui/tui.ts`: render loop (logo via `src/logo.ts`, transcript print, status line
   with context meter and task badges, slash menu + autocomplete, approval prompt,
   esc-cancel mid-stream). `src/session.ts` binds slash commands, model picker (local
   list, add-machine flow, network search/address), and the no-local-server offer.
3. Tests: `test/tui.test.js` with a puppeted terminal.
4. **AC:** `shift+tab` cycles ro → edit → bypass; `esc` cancels a running turn then
   clears input; `ctrl+c`×2 quits; no local server → TUI opens with network-search
   offer instead of an error dump.

### Phase 13 — Web hub

1. `src/web/store.ts` + config wiring: transcripts under `~/.kabby/sessions/`,
   hub record for join-detection (`readHubRecord`/`pingHub`/`askHubToOpen`).
2. `src/web/hub.ts`: HTTP + SSE server, token auth, workspace/session model, inference
   slotting across sessions, `openSession`.
3. `src/web/page.ts`/`styles.ts`/`client.ts`: chat UI, markdown, sidebar, status,
   model picker, approval buttons, `/`-commands parity.
4. `src/web/terminal.ts`, `src/web/attachments.ts`: panel shell (no PTY), paste/drop/
   paperclip with vision-capability warning; session restore rules (§3.9).
5. Tests: `test/web.test.js`, `test/rename.test.js`, `test/attachments.test.js`,
   `test/rtl.test.js`; plus browser bench scripts in `bench/`.
6. **AC:** second `kabby --web` joins the running hub; port conflict suggests another
   port; restored session retires stale approval buttons.

### Phase 14 — Benchmarks, docs, release

1. `bench/run.sh` + scenario scripts (compact smoke, lifecycle, acceptance, browser
   checks) keyed off the `[stats]` line.
2. Update `README.md`, docs (`how-it-works.md`, `backend-notes.md`, audit/lifecycle
   records), and this SPECS2.md.
3. **AC:** `prepublishOnly` = test suite; `npm pack --dry-run` contains only `dist/`
   + `README.md`; version bump + changelog.

---

## 5. Test harness (§6 reference)

For provider/model-server-dependent phases (6, 7, 10, 11), implement a **stub
transport** in `test/` that:

- returns canned `/api/tags`, `/api/v1/models`, `/health` responses,
- streams a canned chat response with tool calls (optionally truncated mid-stream),
- reports loaded context via Ollama `/api/ps`,
- fails on demand (timeout, empty stream, context overflow).

Recorded responses for discovery tests live in `test/fixtures/` (one JSON file per
backend/endpoint). Tests never touch the real `~/.kabby.json` — they use
`KABBY_CONFIG` pointing at a temp file.

---

## 6. Cross-cutting test matrix

| Area | Files | Non-negotiables |
|---|---|---|
| Sandbox | `isolation.test.js`, `sandbox.test.js` | No escape via `..`, absolute paths, shell reach-out detection |
| Context | `context.test.js`, `read-budget.test.js` | Budget math at 4k/16k, eviction targets 60%, handover caps |
| Recovery | `recovery.test.js` | Equivalent-failure detection, attempt limits respected |
| Plan | `plan.test.js` | Survival through compaction, ≤20 steps |
| Providers | `providers.test.js` | No partial tool execution; effort snapping |
| Web | `web.test.js` | Token-gated, transcript resume, approval retirement |
| CLI | `capability.test.js` | flag validation, exit codes, `[stats]` shape |

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
4. **npm publish** — first `npm publish` of `kabby`: verify `files` whitelist,
   bin shebang, `prepublishOnly` gate, `npm pack --dry-run`, tag `v0.7.1`.

---

## 8. Release checklist

- [ ] `npm test` green on Node 18 and 20.
- [ ] Headless smoke on both backends: `bench/run.sh mc-compact <model> off --ctx 12000`
    completes with the plan intact.
- [ ] `[stats]` JSON line parses and carries the fields in §3.6.
- [ ] No `bypass` mode persisted to `~/.kabby.json` after a bypass session.
- [ ] Fresh-machine scenario: no server running → model picker offer, not an
    error dump.
- [ ] `npm pack --dry-run` contains only `dist/` + `README.md`.
- [ ] `README.md` version-accurate (commands, options, ports table).
- [ ] All Phase 0–14 ACs pass without a live model server.
