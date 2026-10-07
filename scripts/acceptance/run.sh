#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
case "${1:---list}" in
  --list)
    printf '%s\n' 'P03: non-mutating API generation/compile check (no live services)' \
      'P04 --local: deterministic lifecycle + HTTP contract regression' \
      'P04: live isolated API/worker lifecycle, report + resource ledger' \
      'P04 --expect-paused-worker: verify queued state, then manually resume worker' \
      'P04 --expect-queue-outage: verify publish error, then manually restore Redis/worker'
    ;;
  P03)
    [ "$#" -eq 1 ] || { echo 'P03 accepts no options' >&2; exit 2; }
    REPORT="$ROOT/logs/acceptance/P03/$(date +%Y%m%d-%H%M%S)-$$"
    mkdir -p "$REPORT"
    if bash "$ROOT/scripts/check-api.sh" > "$REPORT/check.log" 2>&1; then
      cat "$REPORT/check.log"; echo "PASS P03; report: $REPORT"
    else
      status=$?; cat "$REPORT/check.log"; echo "P03 failed/blocked; report: $REPORT" >&2; exit "$status"
    fi
    ;;
  P04)
    shift
    if [ "${1:-}" = '--local' ]; then
      [ "$#" -eq 1 ] || { echo '--local accepts no other options' >&2; exit 2; }
      REPORT="$ROOT/logs/acceptance/P04/$(date +%Y%m%d-%H%M%S)-$$-local"
      mkdir -p "$REPORT"
      if (cd "$ROOT" && make test-knowledge-lifecycle) > "$REPORT/tests.log" 2>&1; then
        cat "$REPORT/tests.log"; echo "PASS P04 local regression (not live E2E); report: $REPORT"
      else
        status=$?; cat "$REPORT/tests.log"; echo "P04 local regression failed; report: $REPORT" >&2; exit "$status"
      fi
    else
      exec python3 "$ROOT/scripts/acceptance/p04.py" "$@"
    fi
    ;;
  *) echo "BLOCKED: unsupported plan ${1:-}; run --list" >&2; exit 2 ;;
esac
