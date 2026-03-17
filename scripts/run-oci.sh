#!/usr/bin/env bash
# run-oci.sh — One-shot benchmark runner for Oracle OCI (or any Linux server).
#
# Installs dependencies, builds Go binaries, starts Docker services,
# runs the full benchmark matrix, and writes results to results/.
#
# Usage:
#   chmod +x scripts/run-oci.sh
#   ./scripts/run-oci.sh                    # full matrix (all gateways × all scenarios)
#   ./scripts/run-oci.sh --quick            # smoke only (fast validation ~10 min)
#   ./scripts/run-oci.sh --gateways ferrogateway,litellm  # specific gateways
#   ./scripts/run-oci.sh --scenarios smoke,baseline        # specific scenarios
#
# Prerequisites (auto-installed if missing):
#   - Docker + Docker Compose v2
#   - Go 1.24+
#   - curl, make
#
# Results are written to results/<timestamp>/ with CSV, Markdown, and a
# summary printed to stdout at the end.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

cd "$REPO_DIR"

# ---------------------------------------------------------------------------
# Defaults
# ---------------------------------------------------------------------------
GATEWAYS=""
SCENARIOS=""
REPEAT=1
QUICK=false
SKIP_DOCKER=false

# ---------------------------------------------------------------------------
# Parse arguments
# ---------------------------------------------------------------------------
while [[ $# -gt 0 ]]; do
    case "$1" in
        --quick)        QUICK=true; shift ;;
        --gateways)     GATEWAYS="$2"; shift 2 ;;
        --scenarios)    SCENARIOS="$2"; shift 2 ;;
        --repeat)       REPEAT="$2"; shift 2 ;;
        --skip-docker)  SKIP_DOCKER=true; shift ;;
        -h|--help)
            head -20 "$0" | grep '^#' | sed 's/^# \?//'
            exit 0
            ;;
        *)
            echo "Unknown option: $1"; exit 1 ;;
    esac
done

if $QUICK; then
    SCENARIOS="smoke"
fi

# ---------------------------------------------------------------------------
# Step 1: Check / install prerequisites
# ---------------------------------------------------------------------------
echo "==> [1/6] Checking prerequisites..."

if ! command -v go &>/dev/null; then
    echo "ERROR: Go not found. Install Go 1.24+ first:"
    echo "  https://go.dev/doc/install"
    exit 1
fi

if ! command -v docker &>/dev/null; then
    echo "ERROR: Docker not found. Install Docker first:"
    echo "  https://docs.docker.com/engine/install/"
    exit 1
fi

if ! docker compose version &>/dev/null; then
    echo "ERROR: Docker Compose v2 not found."
    exit 1
fi

GO_VER=$(go version | grep -oP '\d+\.\d+')
echo "  Go: $(go version)"
echo "  Docker: $(docker --version)"
echo "  Compose: $(docker compose version)"

# ---------------------------------------------------------------------------
# Step 2: Build Go binaries
# ---------------------------------------------------------------------------
echo ""
echo "==> [2/6] Building Go binaries..."
make build

# ---------------------------------------------------------------------------
# Step 3: Create .env
# ---------------------------------------------------------------------------
echo ""
echo "==> [3/6] Creating .env..."

if [ ! -f .env ]; then
    cp .env.example .env
    echo "  Created .env from .env.example"
else
    echo "  Using existing .env"
fi

# ---------------------------------------------------------------------------
# Step 4: Start Docker services
# ---------------------------------------------------------------------------
if ! $SKIP_DOCKER; then
    echo ""
    echo "==> [4/6] Starting Docker services..."

    # Determine which profiles to use based on gateway selection
    COMPOSE_CMD="docker compose"
    if [ -z "$GATEWAYS" ] || echo "$GATEWAYS" | grep -q "kong"; then
        COMPOSE_CMD="$COMPOSE_CMD --profile kong"
    fi
    if [ -z "$GATEWAYS" ] || echo "$GATEWAYS" | grep -q "portkey"; then
        COMPOSE_CMD="$COMPOSE_CMD --profile portkey"
    fi

    $COMPOSE_CMD up -d --build --wait

    echo ""
    echo "  Services running:"
    docker compose ps --format "    {{.Service}}: {{.Status}}" 2>/dev/null || docker compose ps
else
    echo ""
    echo "==> [4/6] Skipping Docker (--skip-docker)"
fi

# ---------------------------------------------------------------------------
# Step 5: Run benchmarks
# ---------------------------------------------------------------------------
echo ""
echo "==> [5/6] Running benchmarks..."

BENCH_ARGS="-config benchmarks.yaml -dotenv .env -out-dir results -repeat $REPEAT"

if [ -n "$GATEWAYS" ]; then
    BENCH_ARGS="$BENCH_ARGS -gateways $GATEWAYS"
fi
if [ -n "$SCENARIOS" ]; then
    BENCH_ARGS="$BENCH_ARGS -scenarios $SCENARIOS"
fi

echo "  Command: ./bin/bench $BENCH_ARGS"
echo ""

# shellcheck disable=SC2086
./bin/bench $BENCH_ARGS

# ---------------------------------------------------------------------------
# Step 6: Print results summary
# ---------------------------------------------------------------------------
echo ""
echo "==> [6/6] Results"
echo ""

LATEST_MD=$(ls -1t results/*.md 2>/dev/null | head -1)
LATEST_CSV=$(ls -1t results/*.csv 2>/dev/null | head -1)

if [ -n "$LATEST_MD" ]; then
    cat "$LATEST_MD"
    echo ""
    echo "Files:"
    echo "  Markdown: $LATEST_MD"
    echo "  CSV:      $LATEST_CSV"
else
    echo "  No results found — check for errors above."
fi

# ---------------------------------------------------------------------------
# Optional: k6 and wrk (if installed)
# ---------------------------------------------------------------------------
if command -v k6 &>/dev/null; then
    echo ""
    echo "==> [bonus] Running k6 throughput test..."
    mkdir -p results/k6
    k6 run \
        --out "json=results/k6/ferrogateway-$(date +%Y%m%d-%H%M%S).json" \
        k6/chat_completions.js || true
fi

if command -v wrk &>/dev/null; then
    echo ""
    echo "==> [bonus] Running wrk peak-RPS test..."
    wrk -t4 -c100 -d30s \
        -s wrk/chat_completions.lua \
        http://localhost:8080 || true
fi

echo ""
echo "========================================="
echo "  Benchmark run complete!"
echo "  Results directory: results/"
echo "========================================="
