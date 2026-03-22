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

# Pin to specific release (pre-releases aren't returned by /releases/latest)
LATEST="${FERRO_VERSION:-v1.0.0-rc.2}"
echo "  Version: $LATEST"

# Download binary — assets are tarballs: ferrogw_VERSION_OS_ARCH.tar.gz
mkdir -p bin
VERSION="${LATEST#v}"  # strip leading 'v'
DOWNLOADED=false

for name in "ferrogw_${VERSION}_${OS}_${ARCH}.tar.gz" "ferrogw-cli_${VERSION}_${OS}_${ARCH}.tar.gz"; do
    URL="https://github.com/ferro-labs/ai-gateway/releases/download/${LATEST}/${name}"
    echo "  Trying: $URL"
    if curl -fsSL "$URL" -o /tmp/ferro-gw.tar.gz 2>/dev/null; then
        echo "  Extracting $name..."
        tar -xzf /tmp/ferro-gw.tar.gz -C bin/
        rm -f /tmp/ferro-gw.tar.gz
        # Find the extracted binary (could be ferrogw, ferro-gw, or ai-gateway)
        for bin_name in ferro-gw ferrogw ai-gateway; do
            if [ -f "bin/$bin_name" ]; then
                if [ "$bin_name" != "ferro-gw" ]; then
                    mv "bin/$bin_name" bin/ferro-gw
                fi
                DOWNLOADED=true
                break
            fi
        done
        if $DOWNLOADED; then break; fi
    fi
done

if ! $DOWNLOADED; then
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
