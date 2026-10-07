#!/usr/bin/env bash
# Read-only generation check; never delete or overwrite workspace business code.
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd)
command -v goctl >/dev/null || { echo 'goctl is required (repository generator: v1.9.2)' >&2; exit 2; }
TMP=$(mktemp -d "${TMPDIR:-/tmp}/eino-api-check.XXXXXX")
trap 'rm -rf "$TMP"' EXIT
API=internal/transport/restapi
mkdir -p "$TMP/$API"
cp "$ROOT/go.mod" "$ROOT/go.sum" "$TMP/"
cp -R "$ROOT/$API/docs" "$TMP/$API/docs"
# Preserve existing handlers/logic in the isolated generation workspace so goctl
# does not replace handwritten implementations with stubs.
cp -R "$ROOT/$API/internal" "$TMP/$API/internal"
(
  cd "$TMP"
  goctl api go -api "$API/docs/restapi.api" -dir "$API" -style go_zero > generate.log 2>&1
  goctl api swagger -api "$API/docs/restapi.api" -dir "$API" >> generate.log 2>&1
  python3 "$ROOT/scripts/normalize-swagger.py" "$API/restapi.json" "$API/internal/types/types.go" "$API/docs/swagger-overrides.json" >> generate.log 2>&1
) || { cat "$TMP/generate.log" >&2; exit 2; }
python3 -m unittest discover -s "$ROOT/scripts/acceptance" -p 'test_swagger.py'
STATUS=0
for FILE in internal/types/types.go internal/handler/routes.go; do
  if ! diff -u "$ROOT/$API/$FILE" "$TMP/$API/$FILE"; then STATUS=1; fi
done
# x-date is a generation timestamp, not a wire-contract change.
if ! python3 - "$ROOT/$API/restapi.json" "$TMP/$API/restapi.json" <<'PYJSON'
import difflib, json, sys
texts = []
for path in sys.argv[1:]:
    data = json.load(open(path))
    data.pop('x-date', None)
    texts.append(json.dumps(data, indent=2, ensure_ascii=False, sort_keys=True).splitlines(True))
if texts[0] != texts[1]:
    sys.stdout.writelines(difflib.unified_diff(*texts, fromfile=sys.argv[1], tofile=sys.argv[2]))
    sys.exit(1)
PYJSON
then STATUS=1; fi
if [ "$STATUS" -ne 0 ]; then
  echo 'API generated artifacts drifted. Review the diff and reconcile only pure artifacts; do not use regen-api.' >&2
  exit "$STATUS"
fi
# Compile current handwritten code against freshly generated types and routes,
# without modifying the working tree. Compile all packages but run no tests.
python3 - "$ROOT" "$TMP" "$API" <<'PY'
import json, pathlib, sys
root, tmp, api = map(pathlib.Path, sys.argv[1:])
files = ['internal/types/types.go', 'internal/handler/routes.go']
(tmp / 'overlay.json').write_text(json.dumps({'Replace': {
    str(root / api / f): str(tmp / api / f) for f in files
}}))
PY
(cd "$ROOT" && go test -overlay "$TMP/overlay.json" ./internal/transport/restapi/... ./cmd/restapi -run '^$')
echo 'PASS: Go types/routes and Swagger match the API contract; handwritten code compiles.'
