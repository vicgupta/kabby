# kabby benchmarks

Run benchmarks with `bench/run.sh`. Each scenario parses the `[stats]` JSON line
that `kabby -p` emits on stderr.

## Scenarios

| Scenario | Description |
|---|---|
| `mc-compact` | Compact smoke test with `--ctx 12000` |
| `lifecycle` | Background task start/list/stop |
| `acceptance` | `--verify` acceptance gate |

## Example

```sh
./bench/run.sh mc-compact llama3.2:3b
./bench/run.sh acceptance qwen2.5-coder:3b
```

The `[stats]` line carries: tools called, input/output/total tokens, tok/s,
duration, and verification status (SPECS.md §3.6).
