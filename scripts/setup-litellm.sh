#!/usr/bin/env bash
# scripts/setup-litellm.sh — Create Python venv and install latest LiteLLM.
# Idempotent: upgrades to latest version on every run.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

VENV_DIR=".venv-litellm"

echo "==> Setting up LiteLLM..."

# Find Python 3.11+
PYTHON=""
for candidate in python3.12 python3.11 python3; do
    if command -v "$candidate" &>/dev/null; then
        PYTHON=$(command -v "$candidate")
        break
    fi
done

if [ -z "$PYTHON" ]; then
    echo "ERROR: Python 3.11+ required."
    echo "  Install: sudo apt-get install python3.11 python3.11-venv"
    exit 1
fi

PY_VERSION=$($PYTHON --version 2>&1 | cut -d' ' -f2)
PY_MAJOR=$(echo "$PY_VERSION" | cut -d. -f1)
PY_MINOR=$(echo "$PY_VERSION" | cut -d. -f2)

if [ "$PY_MAJOR" -lt 3 ] || { [ "$PY_MAJOR" -eq 3 ] && [ "$PY_MINOR" -lt 11 ]; }; then
    echo "ERROR: Python 3.11+ required (found $PY_VERSION)."
    echo "  Install: sudo apt-get install python3.11 python3.11-venv"
    exit 1
fi

echo "  Python: $PYTHON ($PY_VERSION)"

# Ensure python3-venv is installed (Debian/Ubuntu ship without ensurepip)
if ! $PYTHON -m ensurepip --version &>/dev/null 2>&1; then
    PY_SHORT="${PY_MAJOR}.${PY_MINOR}"
    echo "  Installing python${PY_SHORT}-venv (required on Debian)..."
    sudo apt-get install -y "python${PY_SHORT}-venv" 2>/dev/null || \
        sudo apt-get install -y python3-venv 2>/dev/null || true
fi

# Create or reuse virtualenv
if [ ! -d "$VENV_DIR" ]; then
    echo "  Creating virtualenv: $VENV_DIR"
    $PYTHON -m venv "$VENV_DIR"
else
    echo "  Using existing virtualenv: $VENV_DIR"
fi

# Install/upgrade LiteLLM
echo "  Installing latest litellm[proxy]..."
"$VENV_DIR/bin/pip" install --upgrade pip --quiet 2>&1 | tail -1 || true
"$VENV_DIR/bin/pip" install --upgrade 'litellm[proxy]' --quiet

# Print installed version
INSTALLED=$("$VENV_DIR/bin/pip" show litellm 2>/dev/null | grep '^Version:' | cut -d' ' -f2)
echo "  LiteLLM $INSTALLED installed"

# Write version marker
echo "$INSTALLED" > .litellm-version

echo "  LiteLLM ready in $VENV_DIR/"
echo "  Run: $VENV_DIR/bin/litellm --config configs/litellm.native.config.yaml --port 4000"
