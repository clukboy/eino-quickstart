#!/usr/bin/env bash
# Deterministic local quality gate. It intentionally exercises only checks that do
# not require credentials or external services; live dependency gates remain P02/P04/P20.
set -uo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
REPORT=${1:-"$ROOT/logs/acceptance/P23/$(date +%Y%m%d-%H%M%S)-$$-local"}
mkdir -p "$REPORT"

status=0
run_check() {
  local name=$1
  shift
  echo "== $name ==" | tee "$REPORT/$name.log"
  if (cd "$ROOT" && "$@") >>"$REPORT/$name.log" 2>&1; then
    echo "passed" >>"$REPORT/$name.log"
    echo "PASS $name"
  else
    local check_status=$?
    echo "failed: $check_status" >>"$REPORT/$name.log"
    echo "FAIL $name (exit $check_status)" >&2
    status=1
  fi
}

run_check go-regression go test -count=1 ./...
run_check api-contract bash "$ROOT/scripts/check-api.sh"
run_check p20-local bash "$ROOT/scripts/acceptance/run.sh" P20 --local
run_check p21-local bash "$ROOT/scripts/acceptance/run.sh" P21 --local
run_check p22-local bash "$ROOT/scripts/acceptance/run.sh" P22 --local

if [ "$status" -eq 0 ]; then
  cat >"$REPORT/summary.json" <<EOF
{"plan":"P23","mode":"local","status":"passed","checks":["full Go regression","API contract drift","P20 evaluation contract","P21 privacy isolation contract","P22 answer evidence contract"],"external_dependencies":"not run"}
EOF
  echo "PASS P23 local quality gate; report: $REPORT"
else
  cat >"$REPORT/summary.json" <<EOF
{"plan":"P23","mode":"local","status":"failed","checks":["full Go regression","API contract drift","P20 evaluation contract","P21 privacy isolation contract","P22 answer evidence contract"],"external_dependencies":"not run"}
EOF
  echo "P23 local quality gate failed; report: $REPORT" >&2
fi
exit "$status"
