#!/usr/bin/env bash
# Real-world overhead benchmark runner.
#
# Builds the realbench tool, starts the AI Gateway pointed at real OpenAI,
# runs the benchmark, and outputs results.
#
# Requires: OPENAI_API_KEY in .env or environment.
#
# Usage:
#   ./scripts/run-realworld.sh [--samples N] [--delay MS]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
GATEWAY_DIR="$(cd "$ROOT_DIR/../ai-gateway" && pwd)"

# Parse arguments
EXTRA_ARGS=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --samples) EXTRA_ARGS+=("-samples" "$2"); shift 2 ;;
    --delay)   EXTRA_ARGS+=("-delay" "$2"); shift 2 ;;
    *)         EXTRA_ARGS+=("$1"); shift ;;
  esac
done

# Load .env
if [[ -f "$ROOT_DIR/.env" ]]; then
  set -a
  source "$ROOT_DIR/.env"
  set +a
fi

if [[ -z "${OPENAI_API_KEY:-}" ]]; then
  echo "ERROR: OPENAI_API_KEY not set. Add it to .env or export it."
  exit 1
fi

# Build tools
echo "==> Building realbench..."
cd "$ROOT_DIR"
go build -o bin/realbench ./cmd/realbench

echo "==> Building gateway..."
cd "$GATEWAY_DIR"
make build 2>/dev/null || go build -o bin/ferrogw ./cmd/ferrogw

# Start gateway
echo "==> Starting AI Gateway (real OpenAI upstream)..."
OPENAI_API_KEY="$OPENAI_API_KEY" \
GATEWAY_CONFIG="$ROOT_DIR/configs/ferrogateway-realworld.config.yaml" \
LOG_LEVEL=error \
  "$GATEWAY_DIR/bin/ferrogw" &
GW_PID=$!

cleanup() {
  echo "==> Stopping gateway (PID $GW_PID)..."
  kill "$GW_PID" 2>/dev/null || true
  wait "$GW_PID" 2>/dev/null || true
}
trap cleanup EXIT

# Wait for health
echo "==> Waiting for gateway health..."
for i in $(seq 1 30); do
  if curl -sf http://localhost:8080/health >/dev/null 2>&1; then
    echo "    Gateway healthy."
    break
  fi
  if [[ $i -eq 30 ]]; then
    echo "ERROR: Gateway did not become healthy in 30s."
    exit 1
  fi
  sleep 1
done

# Run benchmark
echo "==> Running real-world benchmark..."
cd "$ROOT_DIR"
./bin/realbench \
  -config realworld.yaml \
  -dotenv .env \
  -out-dir results \
  "${EXTRA_ARGS[@]}"

echo ""
echo "==> Done. Results in results/"
