# Backend notes

## Ollama

- Native `/api/chat` with tools, thinking, `keep_alive` (30 min, overridable via `KABBY_KEEP_ALIVE`)
- `/api/ps` for loaded context
- `num_ctx` sent on explicit `--ctx`
- Effort: `off` ⇒ `no_think`-style switch

## LM Studio

- Native `/api/v1/models` catalog (supported effort levels + default)
- OpenAI-compatible streaming with tools
- Native load API for unloaded models with explicit `--ctx`
- Never reload an already-loaded model just to enlarge the window
- Effort: `off` ⇒ `none`, snap to model's supported set

## oMLX

- `/health` with `engine_pool` ⇒ oMLX (needs key)
- Port and key from settings file; `OMLX_API_KEY` fallback
- Port 8000 probed only on local macOS

## MTPLX

- `/health` with `mtp_enabled` + `generation_mode` ⇒ MTPLX
- Port and key from settings file; `MTPLX_API_KEY` fallback
- Port 8000 probed only on local macOS

## Transport limits

- 3-minute silence timeout, 15-minute total deadline
- ≤3 transient retries with cancellable backoff
- One context-overflow recovery via forced compaction
- No partial tool-call execution on truncated streams
