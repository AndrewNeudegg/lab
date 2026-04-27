#!/usr/bin/env bash
set -euo pipefail

cmd="${1:-help}"
shift || true

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ARTIFACTS_DIR="${ROOT_DIR}/.artifacts"
GO_CACHE_DIR="${ARTIFACTS_DIR}/gocache"
GO_TMP_DIR="${ARTIFACTS_DIR}/gotmp"
DATA_DIR="${ROOT_DIR}/data"
mkdir -p "$ARTIFACTS_DIR" "$GO_CACHE_DIR" "$GO_TMP_DIR" "$DATA_DIR"

SUPERVISORD_URL="${SUPERVISORD_URL:-http://127.0.0.1:18082}"

GO_SOURCE_FIND_ARGS=(
  -name '*.go'
  -not -path './.git/*'
  -not -path './.artifacts/*'
)

run_go() {
  env TMPDIR="$GO_TMP_DIR" GOTMPDIR="$GO_TMP_DIR" GOCACHE="$GO_CACHE_DIR" "$@"
}

clean_go_cache() {
  env TMPDIR="$GO_TMP_DIR" GOTMPDIR="$GO_TMP_DIR" GOCACHE="$GO_CACHE_DIR" go clean -cache
}

supervisord_up() {
  curl -fsS "${SUPERVISORD_URL}/supervisord" >/dev/null 2>&1
}

wait_for_supervisord() {
  local deadline=$((SECONDS + 45))
  until supervisord_up; do
    if [ "$SECONDS" -ge "$deadline" ]; then
      echo "supervisord did not become ready at ${SUPERVISORD_URL}" >&2
      tail -80 "${DATA_DIR}/supervisord.log" 2>/dev/null || true
      exit 1
    fi
    sleep 1
  done
}

start_supervisord() {
  if supervisord_up; then
    return
  fi
  mkdir -p "${DATA_DIR}/supervisord"
  : > "${DATA_DIR}/supervisord.log"
  if command -v nix >/dev/null 2>&1; then
    setsid -f bash -lc "cd '${ROOT_DIR}' && exec nix develop -c go run ./cmd/supervisord >> '${DATA_DIR}/supervisord.log' 2>&1"
  else
    setsid -f bash -lc "cd '${ROOT_DIR}' && exec go run ./cmd/supervisord >> '${DATA_DIR}/supervisord.log' 2>&1"
  fi
  wait_for_supervisord
}

post_supervisord() {
  local path="$1"
  local body="${2:-{}}"
  curl -fsS -X POST "${SUPERVISORD_URL}${path}" \
    -H 'Content-Type: application/json' \
    -d "$body" >/dev/null
}

port_pid() {
  local port="$1"
  if command -v lsof >/dev/null 2>&1; then
    lsof -nP -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null | head -n 1
    return
  fi
  if command -v ss >/dev/null 2>&1; then
    ss -ltnp "sport = :$port" 2>/dev/null | sed -n 's/.*pid=\([0-9][0-9]*\).*/\1/p' | head -n 1
  fi
}

process_pid() {
  local name="$1"
  if [ "$name" = "dashboard" ]; then
    local pid
    pid="$(port_pid 5173 || true)"
    if [ -n "$pid" ]; then
      echo "$pid"
      return
    fi
  fi
  ps -eo pid=,args= | awk -v name="$name" '
    name == "healthd" && ($0 ~ /\.\/\.bin\/healthd/ || $0 ~ /go run \.\/cmd\/healthd/ || $0 ~ /\/exe\/healthd/) { print $1; exit }
    name == "homelabd" && ($0 ~ /\.\/\.bin\/homelabd/ || $0 ~ /go run \.\/cmd\/homelabd/ || $0 ~ /\/exe\/homelabd/) { print $1; exit }
    name == "dashboard" && ($0 ~ /bun run dev -- --host 0\.0\.0\.0/ || $0 ~ /vite dev .*--host[ =]?0\.0\.0\.0/) { print $1; exit }
  '
}

adopt_existing_processes() {
  local app pid
  for app in healthd homelabd dashboard; do
    pid="$(process_pid "$app" || true)"
    if [ -n "$pid" ]; then
      post_supervisord "/supervisord/apps/${app}/adopt" "{\"pid\":${pid}}" || true
    fi
  done
}

stack_start() {
  start_supervisord
  adopt_existing_processes
  post_supervisord /supervisord/apps/healthd/start
  post_supervisord /supervisord/apps/homelabd/start
  post_supervisord /supervisord/apps/dashboard/start
  echo "stack started; dashboard: http://lab:5173/supervisord"
}

stack_stop_apps() {
  post_supervisord /supervisord/apps/dashboard/stop || true
  post_supervisord /supervisord/apps/homelabd/stop || true
  post_supervisord /supervisord/apps/healthd/stop || true
}

stack_stop() {
  start_supervisord
  adopt_existing_processes
  stack_stop_apps
  post_supervisord /supervisord/stop || true
  sleep 1
  echo "stack stopped"
}

stack_restart() {
  start_supervisord
  adopt_existing_processes
  stack_stop_apps
  post_supervisord /supervisord/restart || true
  sleep 2
  wait_for_supervisord
  post_supervisord /supervisord/apps/healthd/start
  post_supervisord /supervisord/apps/homelabd/start
  post_supervisord /supervisord/apps/dashboard/start
  echo "stack restarted; dashboard: http://lab:5173/supervisord"
}

case "$cmd" in
  # ── Agent YOLO launchers ──────────────────────────────────────────
  claude)
    if ! command -v claude >/dev/null 2>&1; then
      echo "claude CLI is not installed" >&2
      exit 1
    fi
    exec claude --dangerously-skip-permissions "$@"
    ;;
  codex)
    if ! command -v codex >/dev/null 2>&1; then
      echo "codex CLI is not installed" >&2
      exit 1
    fi
    export CODEX_UNSAFE_ALLOW_NO_SANDBOX=1
    exec codex --dangerously-bypass-approvals-and-sandbox "$@"
    ;;
  gemini)
    if ! command -v gemini >/dev/null 2>&1; then
      echo "gemini CLI is not installed" >&2
      exit 1
    fi
    exec gemini --yolo "$@"
    ;;

  # ── Go quality gates ──────────────────────────────────────────────
  fmt-check)
    out="$(find . "${GO_SOURCE_FIND_ARGS[@]}" -print0 | xargs -0 gofmt -l)"
    if [ -n "$out" ]; then
      echo "gofmt check failed; run: find . -name '*.go' -not -path './.git/*' -not -path './.artifacts/*' -print0 | xargs -0 gofmt -w" >&2
      echo "$out" >&2
      exit 1
    fi
    ;;
  vet)
    run_go go vet ./...
    ;;
  lint)
    "$0" fmt-check
    "$0" vet
    ;;
  test)
    run_go go test ./...
    ;;
  test-race)
    run_go go test -race ./...
    ;;
  build)
    out="${1:-./.bin/homelabd}"
    mkdir -p "$(dirname "$out")"
    run_go env CGO_ENABLED=0 go build -trimpath -ldflags='-s -w -buildid=' -o "$out" ./cmd/homelabd
    ;;
  build-healthd)
    out="${1:-./.bin/healthd}"
    mkdir -p "$(dirname "$out")"
    run_go env CGO_ENABLED=0 go build -trimpath -ldflags='-s -w -buildid=' -o "$out" ./cmd/healthd
    ;;
  clean-cache)
    clean_go_cache
    ;;
  shell)
    exec nix develop "$@"
    ;;

  # ── Run ───────────────────────────────────────────────────────────
  serve)
    exec go run ./cmd/homelabd "$@"
    ;;
  serve-healthd)
    exec go run ./cmd/healthd "$@"
    ;;
  serve-supervisord)
    exec go run ./cmd/supervisord "$@"
    ;;
  stack-start)
    stack_start
    ;;
  stack-stop)
    stack_stop
    ;;
  stack-restart)
    stack_restart
    ;;

  # ── Aggregate ─────────────────────────────────────────────────────
  verify)
    "$0" fmt-check
    "$0" vet
    "$0" test
    ;;

  help|--help|-h)
    cat <<'EOF'
Usage:
  ./run.sh claude  [args...]   # claude-code with --dangerously-skip-permissions
  ./run.sh codex   [args...]   # codex with --dangerously-bypass-approvals-and-sandbox
  ./run.sh gemini  [args...]   # gemini-cli with --yolo
  ./run.sh fmt-check           # verify gofmt has no diffs
  ./run.sh vet                 # go vet ./...
  ./run.sh lint                # fmt-check + vet
  ./run.sh test                # go test ./...
  ./run.sh test-race           # go test -race ./...
  ./run.sh build [out]         # build homelabd (CGO disabled)
  ./run.sh build-healthd [out] # build healthd (CGO disabled)
  ./run.sh serve [args...]     # go run ./cmd/homelabd
  ./run.sh serve-healthd       # go run ./cmd/healthd
  ./run.sh serve-supervisord   # go run ./cmd/supervisord
  ./run.sh stack-start         # start/adopt healthd, homelabd, dashboard
  ./run.sh stack-stop          # gracefully stop dashboard, homelabd, healthd, supervisord
  ./run.sh stack-restart       # gracefully restart the full local stack
  ./run.sh verify              # fmt-check + vet + test
  ./run.sh clean-cache         # clear the Go build cache
  ./run.sh shell               # enter the nix dev shell
EOF
    ;;
  *)
    echo "unknown command: $cmd" >&2
    echo "run './run.sh help' for usage" >&2
    exit 2
    ;;
esac
