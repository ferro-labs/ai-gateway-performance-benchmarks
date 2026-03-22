#!/usr/bin/env bash
# scripts/setup-all.sh — Run all gateway setup scripts in order.
# Single command to prepare a fresh machine for benchmarking.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

echo "========================================"
echo "  Ferro Benchmark Suite — Full Setup"
echo "========================================"
echo ""

PASSED=()
FAILED=()

run_setup() {
    local name="$1"
    local script="$2"
    echo "----------------------------------------"
    if bash "$script"; then
        PASSED+=("$name")
        echo ""
    else
        FAILED+=("$name")
        echo "  FAILED: $name"
        echo ""
    fi
}

run_setup "Mock Server + Go tools" "$SCRIPT_DIR/setup-mockserver.sh"
run_setup "Ferro Gateway"          "$SCRIPT_DIR/setup-ferro.sh"
run_setup "LiteLLM"                "$SCRIPT_DIR/setup-litellm.sh"
run_setup "Bifrost"                "$SCRIPT_DIR/setup-bifrost.sh"
run_setup "Kong"                   "$SCRIPT_DIR/setup-kong.sh"
run_setup "Portkey"                "$SCRIPT_DIR/setup-portkey.sh"

# Create .env if missing
if [ ! -f .env ]; then
    cp .env.example .env
    echo "  Created .env from .env.example"
fi

echo "========================================"
echo "  Setup Summary"
echo "========================================"
echo ""
echo "  Passed: ${PASSED[*]}"

if [ ${#FAILED[@]} -gt 0 ]; then
    echo "  Failed: ${FAILED[*]}"
    echo ""
    echo "  Fix failed components and re-run their individual script:"
    for name in "${FAILED[@]}"; do
        case "$name" in
            *Mock*)   echo "    bash scripts/setup-mockserver.sh" ;;
            *Ferro*)  echo "    bash scripts/setup-ferro.sh" ;;
            *Lite*)   echo "    bash scripts/setup-litellm.sh" ;;
            *Bifrost*)echo "    bash scripts/setup-bifrost.sh" ;;
            *Kong*)   echo "    bash scripts/setup-kong.sh" ;;
            *Portkey*)echo "    bash scripts/setup-portkey.sh" ;;
        esac
    done
    exit 1
fi

touch .setup-complete
echo ""
echo "  All dependencies installed."
echo "  Next step: make bench"
