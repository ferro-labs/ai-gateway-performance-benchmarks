#!/usr/bin/env bash
# scripts/run-benchmarks.sh — Native-process benchmark runner for publishable results.
#
# Runs each gateway as a native process with zero networking overhead.
# Each gateway gets the full machine in complete isolation — the only
# defensible methodology for publishable µs-level measurements.
#
# Architecture:
#   1. Build Go binaries natively
#   2. Start mock server as native binary on :9000
#   3. For each gateway (one at a time, full machine isolation):
#      a. Start as native process on localhost
#      b. Health-check with curl retry loop (30s timeout)
#      c. Run bench runner (single gateway × selected scenarios)
#      d. Kill the gateway process cleanly
#      e. Sleep 5s for OS to release ports/sockets
#   4. Merge per-gateway CSV files into a single combined CSV
#   5. Generate BENCHMARK-REPORT.md + BENCHMARK-REPORT.json
#
# Usage:
#   ./scripts/run-benchmarks.sh                                   # All 4 gateways, all scenarios
#   ./scripts/run-benchmarks.sh --gateways ferrogateway,litellm   # Specific gateways
#   ./scripts/run-benchmarks.sh --scenarios smoke,baseline         # Specific scenarios
#   ./scripts/run-benchmarks.sh --repeat 3                         # Publication-quality (3 averaged runs)
#
# Gateway binary resolution (override with env vars):
#   FERROGATEWAY_BIN  — Ferro AI Gateway binary (default: search for ferro-gw, ferrogw, ai-gateway in PATH and ./bin/)
#   BIFROST_BIN       — Bifrost binary (default: search for bifrost in PATH and ./bin/)
#   LITELLM_VENV      — Path to Python venv containing litellm (default: .venv-litellm)
#
# Prerequisites (installed by scripts/setup-all.sh):
#   - Go 1.24+
#   - Ferro AI Gateway binary
#   - LiteLLM (Python venv)
#   - Bifrost binary
#   - Kong (native package, DB-less mode)
#   - Portkey (Node.js, npm global)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_DIR"

# ---------------------------------------------------------------------------
# Defaults
# ---------------------------------------------------------------------------
DEFAULT_GATEWAYS="ferrogateway,bifrost,litellm,kong,portkey"
GATEWAYS_STR=""
SCENARIOS_STR=""
REPEAT=1
TIMESTAMP=$(date +%Y%m%d-%H%M%S)
RESULTS_DIR="results/native-$TIMESTAMP"

# ---------------------------------------------------------------------------
# Usage
# ---------------------------------------------------------------------------
usage() {
    cat <<'EOF'
Usage: ./scripts/run-benchmarks.sh [OPTIONS]

Options:
  --gateways LIST   Comma-separated gateway names (default: ferrogateway,bifrost,litellm,kong,portkey)
  --scenarios LIST  Comma-separated scenario names (default: all from benchmarks.yaml)
  --repeat N        Run each benchmark N times and average (default: 1, use 3 for publication)
  -h, --help        Show this help

Environment:
  FERROGATEWAY_BIN  Path to Ferro AI Gateway binary
  BIFROST_BIN       Path to Bifrost binary
  LITELLM_VENV      Path to Python venv with litellm (default: .venv-litellm)
EOF
}

# ---------------------------------------------------------------------------
# Parse arguments
# ---------------------------------------------------------------------------
while [[ $# -gt 0 ]]; do
    case "$1" in
        --gateways)    GATEWAYS_STR="$2"; shift 2 ;;
        --scenarios)   SCENARIOS_STR="$2"; shift 2 ;;
        --repeat)      REPEAT="$2"; shift 2 ;;
        -h|--help)     usage; exit 0 ;;
        *)             echo "ERROR: Unknown option: $1"; usage; exit 1 ;;
    esac
done

GATEWAYS_STR="${GATEWAYS_STR:-$DEFAULT_GATEWAYS}"
IFS=',' read -ra GATEWAYS <<< "$GATEWAYS_STR"

# ---------------------------------------------------------------------------
# Preflight
# ---------------------------------------------------------------------------
if [ ! -f .setup-complete ]; then
    echo "ERROR: Setup not complete. Run first:"
    echo "  make setup"
    exit 1
fi

# ---------------------------------------------------------------------------
# Process tracking + cleanup
# ---------------------------------------------------------------------------
MOCK_PID=""
GW_PID=""
KONG_RUNNING=false

cleanup() {
    local exit_code=$?
    echo ""
    echo "==> Cleaning up..."
    if [ -n "$GW_PID" ]; then
        kill "$GW_PID" 2>/dev/null && wait "$GW_PID" 2>/dev/null || true
    fi
    if $KONG_RUNNING; then
        kong stop 2>/dev/null || true
    fi
    if [ -n "$MOCK_PID" ]; then
        kill "$MOCK_PID" 2>/dev/null && wait "$MOCK_PID" 2>/dev/null || true
    fi
    exit "$exit_code"
}
trap cleanup EXIT INT TERM

# ---------------------------------------------------------------------------
# Helper: wait for HTTP health endpoint
# ---------------------------------------------------------------------------
wait_healthy() {
    local url="$1"
    local name="$2"
    local timeout="${3:-30}"
    local elapsed=0

    echo "  Waiting for $name health ($url)..."
    while ! curl -sf -o /dev/null --max-time 2 "$url" 2>/dev/null; do
        sleep 1
        elapsed=$((elapsed + 1))
        if [ "$elapsed" -ge "$timeout" ]; then
            echo "  FATAL: $name did not become healthy within ${timeout}s"
            echo "  URL: $url"
            exit 1
        fi
    done
    echo "  $name healthy (${elapsed}s)"
}

# ---------------------------------------------------------------------------
# Helper: find a binary by checking env var, ./bin/, and PATH
# ---------------------------------------------------------------------------
find_bin() {
    local env_var="$1"
    shift
    local names=("$@")

    # Check env var override
    local env_val="${!env_var:-}"
    if [ -n "$env_val" ]; then
        if [ -x "$env_val" ] || command -v "$env_val" &>/dev/null; then
            echo "$env_val"
            return 0
        fi
        echo "  WARNING: $env_var=$env_val not found or not executable" >&2
    fi

    # Search ./bin/ and PATH
    for name in "${names[@]}"; do
        if [ -x "./bin/$name" ]; then
            echo "./bin/$name"
            return 0
        fi
        if command -v "$name" &>/dev/null; then
            command -v "$name"
            return 0
        fi
    done

    return 1
}

# ---------------------------------------------------------------------------
# Helper: stop current gateway process
# ---------------------------------------------------------------------------
stop_gateway() {
    local gw_name="$1"

    if [ "$gw_name" = "kong" ]; then
        echo "  Stopping Kong..."
        kong stop 2>/dev/null || true
        KONG_RUNNING=false
    elif [ -n "$GW_PID" ]; then
        echo "  Stopping $gw_name (PID $GW_PID)..."
        kill "$GW_PID" 2>/dev/null || true
        wait "$GW_PID" 2>/dev/null || true
    fi
    GW_PID=""
}

# ---------------------------------------------------------------------------
# Gateway start functions
# ---------------------------------------------------------------------------

start_ferrogateway() {
    local bin
    if ! bin=$(find_bin FERROGATEWAY_BIN ferro-gw ferrogw ai-gateway); then
        echo "  SKIP: Ferro AI Gateway binary not found."
        echo "        Set FERROGATEWAY_BIN=/path/to/binary or place it in ./bin/"
        echo "        Binary names searched: ferro-gw, ferrogw, ai-gateway"
        echo "        Install: make setup-ferro"
        return 1
    fi
    echo "  Binary: $bin"

    GATEWAY_CONFIG=configs/ferrogateway.config.yaml \
    OPENAI_BASE_URL=http://localhost:9000/v1 \
    OPENAI_API_KEY=mock-key \
    LOG_LEVEL=error \
    ENABLE_PPROF=false \
        "$bin" &
    GW_PID=$!
    echo "  Started ferrogateway (PID $GW_PID)"
    wait_healthy "http://localhost:8080/health" "ferrogateway" 30
}

start_litellm() {
    local litellm_cmd=""

    # Check for litellm in PATH
    if command -v litellm &>/dev/null; then
        litellm_cmd="litellm"
    else
        # Try venv
        local venv="${LITELLM_VENV:-.venv-litellm}"
        if [ -x "$venv/bin/litellm" ]; then
            litellm_cmd="$venv/bin/litellm"
        elif [ -x "$venv/bin/python" ]; then
            litellm_cmd="$venv/bin/python -m litellm"
        fi
    fi

    if [ -z "$litellm_cmd" ]; then
        echo "  SKIP: litellm not found in PATH or venv."
        echo "        Install: make setup-litellm"
        return 1
    fi
    echo "  Command: $litellm_cmd"

    # shellcheck disable=SC2086
    $litellm_cmd --config configs/litellm.native.config.yaml --port 4000 &
    GW_PID=$!
    echo "  Started litellm (PID $GW_PID)"
    # LiteLLM takes longer to start (Python + model loading)
    # Use /health/liveliness which doesn't require auth (master_key protects /health)
    wait_healthy "http://localhost:4000/health/liveliness" "litellm" 60
}

start_bifrost() {
    local bin
    if ! bin=$(find_bin BIFROST_BIN bifrost); then
        echo "  SKIP: Bifrost binary not found."
        echo "        Set BIFROST_BIN=/path/to/binary or place it in ./bin/"
        echo "        Install: make setup-bifrost"
        return 1
    fi
    echo "  Binary: $bin"

    # Bifrost v1.0.0 requires --app-dir pointing to directory
    # containing config.json — not a direct file path
    mkdir -p /tmp/bifrost-bench
    cp configs/bifrost.config.json /tmp/bifrost-bench/config.json
    "$bin" \
        --app-dir /tmp/bifrost-bench \
        --host 0.0.0.0 \
        --port 8081 \
        --log-level error 2>/dev/null &
    GW_PID=$!
    echo "  Started bifrost (PID $GW_PID)"
    wait_healthy "http://localhost:8081/health" "bifrost" 30
}

start_kong() {
    if ! command -v kong &>/dev/null; then
        echo "  SKIP: Kong not found."
        echo "        Install: make setup-kong"
        return 1
    fi
    echo "  Binary: $(command -v kong)"

    # Kong requires absolute path for declarative_config
    # Use a writable prefix dir (default /usr/local/kong may not be writable)
    export KONG_PREFIX=/tmp/kong-bench
    mkdir -p "$KONG_PREFIX"
    export KONG_DATABASE=off
    export KONG_DECLARATIVE_CONFIG="$(pwd)/configs/kong.yaml"
    export KONG_PROXY_LISTEN="0.0.0.0:8000"
    export KONG_ADMIN_LISTEN="0.0.0.0:8001"
    export KONG_PROXY_ACCESS_LOG=off
    export KONG_ADMIN_ACCESS_LOG=off
    export KONG_PROXY_ERROR_LOG=/dev/stderr
    export KONG_ADMIN_ERROR_LOG=/dev/stderr

    kong start -c configs/kong.conf
    KONG_RUNNING=true
    echo "  Started Kong (DB-less native process)"
    wait_healthy "http://localhost:8001/status" "kong" 30
}

start_portkey() {
    # Node 20 required — Portkey has compatibility issues with Node 22+
    source "${NVM_DIR:-$HOME/.nvm}/nvm.sh" 2>/dev/null || true
    nvm use 20 2>/dev/null || true

    # Find Portkey entry point
    local portkey_entry
    portkey_entry=$(npm root -g 2>/dev/null)/@portkey-ai/gateway/build/start-server.js
    if [ ! -f "$portkey_entry" ]; then
        echo "  SKIP: Portkey not found at $portkey_entry"
        echo "        Install: make setup-portkey"
        return 1
    fi

    echo "  Node: $(node --version)"
    echo "  Entry: $portkey_entry"

    PORT=8787 node "$portkey_entry" 2>/dev/null &
    GW_PID=$!
    echo "  Started portkey (PID $GW_PID)"
    wait_healthy "http://localhost:8787" "portkey" 30
}

# ---------------------------------------------------------------------------
# Helper: merge per-gateway CSVs into a combined file
# ---------------------------------------------------------------------------
merge_csvs() {
    local csv_files
    csv_files=$(find "$RESULTS_DIR" -name 'bench-*.csv' -type f | sort)
    local count
    count=$(echo "$csv_files" | grep -c . || true)

    if [ "$count" -eq 0 ]; then
        echo "  No CSV files to merge."
        return 1
    fi

    if [ "$count" -eq 1 ]; then
        echo "  Single CSV — no merge needed."
        return 0
    fi

    local combined="$RESULTS_DIR/bench-combined-$TIMESTAMP.csv"
    local first=true

    while IFS= read -r csv; do
        if $first; then
            cp "$csv" "$combined"
            first=false
        else
            tail -n +2 "$csv" >> "$combined"
        fi
    done <<< "$csv_files"

    echo "  Merged $count CSVs into $(basename "$combined")"
}

# ===========================================================================
# MAIN
# ===========================================================================

echo "========================================"
echo "  Native-Process Benchmark Runner"
echo "  Publishable Results — Zero Overhead"
echo "========================================"
echo ""
echo "  Gateways:  ${GATEWAYS[*]}"
echo "  Scenarios: ${SCENARIOS_STR:-all}"
echo "  Repeat:    $REPEAT"
echo "  Output:    $RESULTS_DIR/"
echo ""

# ---------------------------------------------------------------------------
# Step 1: Build
# ---------------------------------------------------------------------------
echo "==> [1/5] Building Go binaries..."
make build
echo ""

# ---------------------------------------------------------------------------
# Step 2: Start mock server
# ---------------------------------------------------------------------------
echo "==> [2/5] Starting mock server on :9000 (60ms latency, no stream chunk delay)..."
./bin/mockserver --port 9000 --latency 60ms --stream-chunk-delay-ms 0 &
MOCK_PID=$!
echo "  Mock server started (PID $MOCK_PID)"
wait_healthy "http://localhost:9000/health" "mock-server" 15
echo ""

mkdir -p "$RESULTS_DIR"

# ---------------------------------------------------------------------------
# Step 3: Benchmark each gateway in isolation
# ---------------------------------------------------------------------------
echo "==> [3/5] Running benchmarks (${#GATEWAYS[@]} gateways, one at a time)..."

SKIPPED=()

for GW in "${GATEWAYS[@]}"; do
    echo ""
    echo "=== Benchmarking $GW ==="

    # Start gateway
    GW_PID=""
    started=true
    case "$GW" in
        ferrogateway) start_ferrogateway || started=false ;;
        litellm)      start_litellm || started=false ;;
        bifrost)      start_bifrost || started=false ;;
        kong)         start_kong || started=false ;;
        portkey)      start_portkey || started=false ;;
        *)
            echo "  Unknown gateway: $GW — skipping"
            SKIPPED+=("$GW")
            continue
            ;;
    esac

    if ! $started; then
        SKIPPED+=("$GW")
        continue
    fi

    # Run bench
    echo ""
    # Determine PID for resource monitoring
    bench_pid_arg=""
    if [ "$GW" = "kong" ]; then
        # Kong manages its own workers; find master PID
        kong_pid=$(pgrep -f "nginx.*kong" | head -1 || true)
        if [ -n "$kong_pid" ]; then
            bench_pid_arg="-gateway-pid $kong_pid"
        fi
    elif [ -n "$GW_PID" ]; then
        bench_pid_arg="-gateway-pid $GW_PID"
    fi

    BENCH_ARGS="-config benchmarks.yaml -dotenv .env -out-dir $RESULTS_DIR -gateways $GW -repeat $REPEAT -mock-latency 60 $bench_pid_arg"
    if [ -n "$SCENARIOS_STR" ]; then
        BENCH_ARGS="$BENCH_ARGS -scenarios $SCENARIOS_STR"
    fi
    echo "  Running: ./bin/bench $BENCH_ARGS"
    echo ""

    # shellcheck disable=SC2086
    ./bin/bench $BENCH_ARGS || echo "  WARNING: bench exited non-zero for $GW"

    # Stop gateway, let OS release ports
    echo ""
    stop_gateway "$GW"
    echo "  Sleeping 10s for OS port release..."
    sleep 10

    echo "=== $GW done ==="
done

# ---------------------------------------------------------------------------
# Step 4: Merge results
# ---------------------------------------------------------------------------
echo ""
echo "==> [4/5] Merging results..."
merge_csvs

# ---------------------------------------------------------------------------
# Step 5: Generate report
# ---------------------------------------------------------------------------
echo ""
echo "==> [5/5] Generating report..."
./bin/report --input="$RESULTS_DIR" --output="$RESULTS_DIR"

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
echo "========================================"
echo "  Benchmarks complete!"
echo "========================================"
echo ""
echo "  Results: $RESULTS_DIR/"
echo "  Report:  $RESULTS_DIR/BENCHMARK-REPORT.md"
echo "  Data:    $RESULTS_DIR/BENCHMARK-REPORT.json"

if [ ${#SKIPPED[@]} -gt 0 ]; then
    echo ""
    echo "  Skipped:  ${SKIPPED[*]}"
    echo "  (run the corresponding make setup-<gateway> to install)"
fi

echo ""
echo "  View report: cat $RESULTS_DIR/BENCHMARK-REPORT.md"
echo ""
