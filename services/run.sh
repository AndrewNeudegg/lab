#!/usr/bin/env bash
set -euo pipefail

SERVICES_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SERVICES_DIR}/.." && pwd)"

usage() {
  cat <<'EOF'
Usage:
  ./run.sh <service> start      # bootstrap if available, then up -d
  ./run.sh <service> stop       # down
  ./run.sh <service> restart    # bootstrap if available, then down + up -d
  ./run.sh <service> status     # podman compose ps
  ./run.sh <service> logs       # podman compose logs --tail 120

Examples:
  ./run.sh element start
  ./run.sh element restart
  ./run.sh element stop
EOF
}

service="${1:-help}"
action="${2:-help}"
shift 2 2>/dev/null || true

if [ "${service}" = "help" ] || [ "${service}" = "--help" ] || [ "${service}" = "-h" ]; then
  usage
  exit 0
fi

service_dir="${SERVICES_DIR}/${service}"
compose_file="${service_dir}/compose.yaml"
env_file="${service_dir}/.env"
bootstrap="${service_dir}/bootstrap/init.sh"

if [ ! -d "${service_dir}" ]; then
  echo "unknown service: ${service}" >&2
  echo "expected directory: ${service_dir}" >&2
  exit 2
fi

if [ ! -f "${compose_file}" ]; then
  echo "missing compose file: ${compose_file}" >&2
  exit 2
fi

run_podman() {
  if command -v podman >/dev/null 2>&1; then
    podman "$@"
  elif command -v nix >/dev/null 2>&1; then
    nix develop "${ROOT_DIR}" -c podman "$@"
  else
    echo "podman is not available; run from nix develop or install podman" >&2
    exit 127
  fi
}

compose() {
  local args=()

  if [ -f "${env_file}" ]; then
    args+=(--env-file "${env_file}")
  fi

  (
    cd "${service_dir}"
    run_podman compose "${args[@]}" -f "${compose_file}" "$@"
  )
}

bootstrap_if_present() {
  if [ -x "${bootstrap}" ]; then
    "${bootstrap}"
  fi
}

case "${action}" in
  start)
    bootstrap_if_present
    compose up -d "$@"
    ;;
  stop)
    compose down "$@"
    ;;
  restart)
    bootstrap_if_present
    compose down
    compose up -d "$@"
    ;;
  status|ps)
    compose ps "$@"
    ;;
  logs)
    compose logs --tail 120 "$@"
    ;;
  help|--help|-h)
    usage
    ;;
  *)
    echo "unknown action: ${action}" >&2
    echo "run './run.sh help' for usage" >&2
    exit 2
    ;;
esac
