#!/usr/bin/env bash
# bench/run.sh — kabby benchmark runner.
#
# Scenarios are keyed off the [stats] JSON line emitted by kabby -p on stderr.
#
# Usage:
#   bench/run.sh <scenario> <model> [flags...]
#
# Scenarios:
#   mc-compact   Compact smoke test (small context window)
#   lifecycle    Process lifecycle (background task + killAll)
#   acceptance   Acceptance gate (--verify)
#
set -euo pipefail

SCENARIO="${1:-}"
MODEL="${2:-}"
shift 2 || true

KABBY="${KABBY:-./kabby}"
WORKSPACE="${BENCH_WORKSPACE:-$(mktemp -d)}"
export KABBY_CONFIG="${KABBY_CONFIG:-$(mktemp)}"

echo "=== kabby bench: $SCENARIO ==="
echo "model: $MODEL"
echo "workspace: $WORKSPACE"
echo "config: $KABBY_CONFIG"

run_stats() {
  "$KABBY" -p "$1" --ctx "${CTX:-16384}" "$@" 2>&1 | tee /dev/stderr | grep '^\[stats\] ' | sed 's/^\[stats\] //'
}

case "$SCENARIO" in
  mc-compact)
    echo "--- compact smoke (--ctx 12000) ---"
    CTX=12000 run_stats "create a file hello.txt with content 'hello world'" --model "$MODEL"
    ;;
  lifecycle)
    echo "--- process lifecycle ---"
    CTX=16384 run_stats "start a background task, list it, then stop it" --model "$MODEL"
    ;;
  acceptance)
    echo "--- acceptance gate ---"
    CTX=16384 "$KABBY" -p "write a file and verify it exists" --verify "test -f /tmp/bench-verify.txt" --verify-attempts 6 --model "$MODEL" 2>&1 | tee /dev/stderr
    ;;
  *)
    echo "unknown scenario: $SCENARIO"
    echo "usage: $0 <mc-compact|lifecycle|acceptance> <model> [flags...]"
    exit 1
    ;;
esac

echo "=== bench complete ==="
