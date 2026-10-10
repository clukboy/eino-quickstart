#!/usr/bin/env bash
# Manage only the named, loopback-bound P02 dependency project.
# This script never removes volumes and never touches unrelated compose projects.
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
COMPOSE_FILE="$ROOT/configs/acceptance/compose.yaml"
PROJECT=${EINO_ACCEPTANCE_COMPOSE_PROJECT:-eino-acceptance}

usage() {
  cat >&2 <<USAGE
usage: $0 {up|status|logs|down}

up      start PostgreSQL, Redis, and the Milvus standalone dependencies
status  show only the P02 dependency project
logs    follow/show recent logs for the P02 dependency project
down    stop and remove only the P02 containers; named volumes are preserved

Required for up:
  EINO_STORAGE_PASSWORD=...  PostgreSQL password used by configs/acceptance/*.yaml
USAGE
}

compose() {
  # Compose must interpolate POSTGRES_PASSWORD even for status/logs/down.
  # A placeholder is safe for read/stop operations; `up` still requires the
  # real EINO_STORAGE_PASSWORD before it reaches this helper.
  local password="${EINO_STORAGE_PASSWORD:-compose-command-placeholder}"
  EINO_STORAGE_PASSWORD="$password" docker compose -p "$PROJECT" -f "$COMPOSE_FILE" "$@"
}

command -v docker >/dev/null 2>&1 || {
  echo "docker CLI is required; install/start Docker Desktop first" >&2
  exit 1
}

action=${1:-}
case "$action" in
  up)
    [ -n "${EINO_STORAGE_PASSWORD:-}" ] || {
      echo "EINO_STORAGE_PASSWORD must be set; it must match configs/acceptance/*.yaml" >&2
      exit 2
    }
    docker info >/dev/null 2>&1 || {
      echo "Docker daemon is unavailable; start Docker Desktop and retry" >&2
      exit 1
    }
    compose up -d
    compose ps
    ;;
  status)
    compose ps
    ;;
  logs)
    compose logs --tail="${EINO_ACCEPTANCE_LOG_LINES:-100}" "${@:2}"
    ;;
  down)
    # Deliberately omit --volumes: database/vector data survives a normal stop.
    compose down
    ;;
  *)
    usage
    exit 2
    ;;
esac
