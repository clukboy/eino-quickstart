#!/usr/bin/env bash
# Run one of the four delivery batches without hiding an incomplete/blocked plan.
# This is a local-contract runner: external services are never replaced by mocks
# and P02's real dependency preflight remains a separate explicit command.
set -uo pipefail
ROOT=$(cd "$(dirname "$0")/../.." && pwd)

usage() {
  cat >&2 <<USAGE
usage: $0 {1|2|3|4} [--local]

Runs the local, deterministic acceptance checks for one delivery batch.
For real PostgreSQL/Redis/Milvus/worker/model/ES validation, run the numbered
P02/P04/P20/... commands from the batch report in an isolated environment.
USAGE
}

[ "$#" -ge 1 ] || { usage; exit 2; }
BATCH=$1
shift
[ "$#" -eq 0 ] || { [ "$#" -eq 1 ] && [ "$1" = '--local' ] || { usage; exit 2; }; }

REPORT="$ROOT/logs/acceptance/batch-${BATCH}/$(date +%Y%m%d-%H%M%S)-$$-local"
mkdir -p "$REPORT"

checks=()
failures=0
passed=0

run_check() {
  local name=$1
  shift
  local log="$REPORT/${name}.log"
  printf '[RUN] %s\n' "$name"
  if "$@" >"$log" 2>&1; then
    checks+=("$name:passed")
    passed=$((passed + 1))
    printf '[PASS] %s\n' "$name"
  else
    local rc=$?
    checks+=("$name:failed:$rc")
    failures=$((failures + 1))
    printf '[FAIL] %s (see %s)\n' "$name" "$log" >&2
    tail -40 "$log" >&2 || true
  fi
}

run_p02_local() {
  run_check p02-config-tests env GOCACHE="${GOCACHE:-/tmp/eino-go-cache}" go test -count=1 ./internal/platform/config ./internal/platform/queue/asynq ./cmd/acceptance-preflight
  # The acceptance compose file is an optional isolation template. The four
  # batch runner must not require Docker when the deployment already has its
  # own PostgreSQL/Redis/Milvus and other dependencies.
  run_check p02-existing-configs bash -c '
    set -e
    test -s "$1/configs/api/config.yaml"
    test -s "$1/configs/worker/config.yaml"
    test -s "$1/configs/api/restapi.yaml"
  ' bash "$ROOT"
  run_check p02-shell-syntax bash -n "$ROOT/scripts/acceptance/run.sh" "$ROOT/scripts/acceptance/dependencies.sh"
}

case "$BATCH" in
  1)
    run_p02_local
    run_check p01 bash "$ROOT/scripts/acceptance/run.sh" P01
    run_check p03 bash "$ROOT/scripts/acceptance/run.sh" P03
    run_check p04 bash "$ROOT/scripts/acceptance/run.sh" P04 --local
    run_check p12 bash "$ROOT/scripts/acceptance/run.sh" P12 --local
    run_check p05 bash "$ROOT/scripts/acceptance/run.sh" P05 --local
    run_check p06 bash "$ROOT/scripts/acceptance/run.sh" P06 --local
    run_check p09 bash "$ROOT/scripts/acceptance/run.sh" P09 --local
    run_check p07 bash "$ROOT/scripts/acceptance/run.sh" P07 --local
    run_check p08 bash "$ROOT/scripts/acceptance/run.sh" P08 --local
    run_check p20 bash "$ROOT/scripts/acceptance/run.sh" P20 --local
    ;;
  2)
    run_check p21 bash "$ROOT/scripts/acceptance/run.sh" P21 --local
    run_check p22 bash "$ROOT/scripts/acceptance/run.sh" P22 --local
    run_check p23 bash "$ROOT/scripts/acceptance/run.sh" P23 --local
    run_check p27 bash "$ROOT/scripts/acceptance/run.sh" P27 --local
    ;;
  3)
    run_check p10 bash "$ROOT/scripts/acceptance/run.sh" P10 --local
    run_check p11 bash "$ROOT/scripts/acceptance/run.sh" P11 --local
    run_check p13 bash "$ROOT/scripts/acceptance/run.sh" P13 --local
    run_check p14 bash "$ROOT/scripts/acceptance/run.sh" P14 --local
    run_check p15 bash "$ROOT/scripts/acceptance/run.sh" P15 --local
    run_check p16 bash "$ROOT/scripts/acceptance/run.sh" P16 --local
    run_check p17 bash "$ROOT/scripts/acceptance/run.sh" P17 --local
    run_check p18 bash "$ROOT/scripts/acceptance/run.sh" P18 --local
    run_check p19 bash "$ROOT/scripts/acceptance/run.sh" P19 --local
    ;;
  4)
    run_check p24 bash "$ROOT/scripts/acceptance/run.sh" P24 --local
    run_check p25 bash "$ROOT/scripts/acceptance/run.sh" P25 --local
    run_check p26 bash "$ROOT/scripts/acceptance/run.sh" P26 --local
    run_check p28 bash "$ROOT/scripts/acceptance/run.sh" P28 --local
    run_check p29 bash "$ROOT/scripts/acceptance/run.sh" P29 --local
    ;;
  *)
    usage
    exit 2
    ;;
esac

status=passed
[ "$failures" -eq 0 ] || status=failed
{
  printf '{"batch":"%s","mode":"local","status":"%s","passed":%d,"failed":%d,"checks":[' "$BATCH" "$status" "$passed" "$failures"
  first=1
  for item in "${checks[@]}"; do
    [ "$first" -eq 1 ] || printf ','
    first=0
    printf '"%s"' "$item"
  done
  printf ']}\n'
} > "$REPORT/summary.json"
printf 'batch=%s status=%s report=%s\n' "$BATCH" "$status" "$REPORT"
[ "$failures" -eq 0 ]
