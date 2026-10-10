#!/usr/bin/env bash
# Verify a running API process and its separate go-zero Prometheus agent.
# This script never starts, stops, or reconfigures a service.
set -euo pipefail
BASE_URL=${1:-http://127.0.0.1:8090}
METRICS_URL=${2:-http://127.0.0.1:9091/metrics}
TMP=$(mktemp "${TMPDIR:-/tmp}/eino-metrics.XXXXXX")
trap 'rm -f "$TMP"' EXIT

curl --fail --silent --show-error --max-time 10 "$BASE_URL/health" >/dev/null
curl --fail --silent --show-error --max-time 10 "$METRICS_URL" >"$TMP"
grep -q '^http_server_requests_code_total' "$TMP" || {
  echo "metrics endpoint did not expose http_server_requests_code_total: $METRICS_URL" >&2
  exit 1
}
printf 'PASS: health responded and Prometheus metrics were scraped from %s\n' "$METRICS_URL"
