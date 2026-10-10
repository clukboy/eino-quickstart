#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
REPORT_DIR=${1:-}
CONFIRM=${2:-}
BASE="$ROOT/logs/acceptance/P02"

[ "$CONFIRM" = '--confirm' ] || {
  echo 'usage: cleanup-p02.sh REPORT_DIR --confirm' >&2
  exit 2
}
[ -n "$REPORT_DIR" ] || { echo 'REPORT_DIR is required' >&2; exit 2; }
case "$REPORT_DIR" in
  /*) ;;
  *) REPORT_DIR="$ROOT/$REPORT_DIR" ;;
esac
[ -d "$REPORT_DIR" ] || { echo "report directory not found: $REPORT_DIR" >&2; exit 2; }
REPORT_DIR=$(cd "$REPORT_DIR" && pwd)
BASE=$(cd "$BASE" && pwd)
case "$REPORT_DIR" in
  "$BASE"/*) ;;
  *) echo "refusing to clean outside $BASE" >&2; exit 2 ;;
esac
[ -f "$REPORT_DIR/preflight.json" ] || { echo 'preflight.json is required' >&2; exit 2; }

FIXTURE="$REPORT_DIR/fixture.md"
if [ -e "$FIXTURE" ]; then
  rm -- "$FIXTURE"
  action=removed
else
  action=already-absent
fi
printf '{\n  "plan": "P02",\n  "report": "%s",\n  "fixture": "%s",\n  "action": "%s",\n  "cleaned_at": "%s"\n}\n' \
  "$REPORT_DIR" "$FIXTURE" "$action" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  > "$REPORT_DIR/cleanup.json"
printf 'P02 cleanup: %s (%s)\nreport: %s\n' "$action" "$FIXTURE" "$REPORT_DIR"
