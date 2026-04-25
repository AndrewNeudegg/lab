#!/usr/bin/env bash
set -euo pipefail

cmd="${1:-help}"
shift || true

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ARTIFACTS_DIR="${ROOT_DIR}/.artifacts"
GO_CACHE_DIR="${ARTIFACTS_DIR}/gocache"
GO_TMP_DIR="${ARTIFACTS_DIR}/gotmp"
mkdir -p "$ARTIFACTS_DIR" "$GO_CACHE_DIR" "$GO_TMP_DIR"

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
