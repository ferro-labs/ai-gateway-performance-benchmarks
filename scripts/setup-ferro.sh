#!/usr/bin/env bash
# scripts/setup-ferro.sh — Download latest Ferro AI Gateway binary from GitHub releases.
# Idempotent: re-downloads latest version on every run.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

echo "==> Setting up Ferro AI Gateway..."

# Detect OS and arch
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case $ARCH in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    *)       echo "ERROR: Unsupported architecture: $ARCH"; exit 1 ;;
esac
echo "  Platform: ${OS}/${ARCH}"

# Get latest release version from GitHub API
echo "  Fetching latest release..."
LATEST=$(curl -fsSL \
    https://api.github.com/repos/ferro-labs/ai-gateway/releases/latest \
    | grep '"tag_name"' | head -1 | cut -d'"' -f4)

if [ -z "$LATEST" ]; then
    echo "ERROR: Could not determine latest Ferro release."
    echo "  Check: https://github.com/ferro-labs/ai-gateway/releases"
    exit 1
fi
echo "  Latest version: $LATEST"

# Download binary
mkdir -p bin
DOWNLOAD_URL="https://github.com/ferro-labs/ai-gateway/releases/download/${LATEST}/ferro-gw_${OS}_${ARCH}"
echo "  Downloading: $DOWNLOAD_URL"

if ! curl -fsSL "$DOWNLOAD_URL" -o bin/ferro-gw; then
    # Try alternate naming conventions
    for name in "ferrogw_${OS}_${ARCH}" "ai-gateway_${OS}_${ARCH}" "ferro-gw-${OS}-${ARCH}"; do
        ALT_URL="https://github.com/ferro-labs/ai-gateway/releases/download/${LATEST}/${name}"
        echo "  Trying: $ALT_URL"
        if curl -fsSL "$ALT_URL" -o bin/ferro-gw 2>/dev/null; then
            break
        fi
    done
fi

if [ ! -f bin/ferro-gw ]; then
    echo "ERROR: Download failed. Check release assets at:"
    echo "  https://github.com/ferro-labs/ai-gateway/releases/tag/${LATEST}"
    exit 1
fi

chmod +x bin/ferro-gw

# Verify
if ./bin/ferro-gw --version 2>/dev/null; then
    true
else
    echo "  Binary downloaded (--version flag may not be supported)"
fi

echo "  Ferro Gateway ready at bin/ferro-gw (${LATEST})"
