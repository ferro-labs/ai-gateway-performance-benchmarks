#!/usr/bin/env bash
# scripts/setup-bifrost.sh — Download latest Bifrost binary from GitHub releases.
# Falls back to building from source if no pre-built binary is available.
# Idempotent: re-downloads/rebuilds latest version on every run.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

echo "==> Setting up Bifrost..."

# Detect OS and arch
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case $ARCH in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    *)       echo "ERROR: Unsupported architecture: $ARCH"; exit 1 ;;
esac
echo "  Platform: ${OS}/${ARCH}"

mkdir -p bin

# Try downloading from GitHub releases first
echo "  Fetching latest release..."
LATEST=$(curl -fsSL \
    https://api.github.com/repos/maximhq/bifrost/releases/latest 2>/dev/null \
    | grep '"tag_name"' | head -1 | cut -d'"' -f4) || true

if [ -n "$LATEST" ]; then
    echo "  Latest version: $LATEST"
    DOWNLOADED=false

    for name in "bifrost_${OS}_${ARCH}" "bifrost-${OS}-${ARCH}" "bifrost"; do
        URL="https://github.com/maximhq/bifrost/releases/download/${LATEST}/${name}"
        if curl -fsSL "$URL" -o bin/bifrost 2>/dev/null; then
            chmod +x bin/bifrost
            DOWNLOADED=true
            echo "  Downloaded from release: $name"
            break
        fi
    done

    if $DOWNLOADED; then
        if ./bin/bifrost --version 2>/dev/null; then
            true
        else
            echo "  Binary downloaded (--version flag may not be supported)"
        fi
        echo "  Bifrost ready at bin/bifrost (${LATEST})"
        exit 0
    fi

    echo "  No pre-built binary found in release assets, falling back to source build..."
fi

# Fallback: build from source
if ! command -v go &>/dev/null; then
    echo "ERROR: No release binary available and Go not installed for source build."
    echo "  Install Go 1.24+ from https://go.dev/doc/install"
    echo "  Or download Bifrost manually from https://github.com/maximhq/bifrost/releases"
    exit 1
fi

# Use a specific tagged release for reproducible builds
BIFROST_TAG="transports/v1.4.12"
BIFROST_SRC=$(mktemp -d)
echo "  Cloning github.com/maximhq/bifrost @ $BIFROST_TAG..."

if ! git clone --depth 1 --branch "$BIFROST_TAG" https://github.com/maximhq/bifrost "$BIFROST_SRC" 2>/dev/null; then
    rm -rf "$BIFROST_SRC"
    echo "ERROR: git clone failed. Check network connectivity."
    echo "  Manual install: https://github.com/maximhq/bifrost"
    exit 1
fi

echo "  Building from source (transports/bifrost-http)..."
REPO_ROOT="$(pwd)"

# The HTTP transport embeds a UI directory. Create a minimal placeholder
# so we can build without running the full Node.js UI build pipeline.
mkdir -p "$BIFROST_SRC/transports/bifrost-http/ui"
echo "<!-- benchmark build -->" > "$BIFROST_SRC/transports/bifrost-http/ui/index.html"

if (cd "$BIFROST_SRC/transports/bifrost-http" && \
    CGO_ENABLED=1 GOWORK=off go build -ldflags="-w -s" -trimpath -o "$REPO_ROOT/bin/bifrost" . 2>&1); then
    rm -rf "$BIFROST_SRC"
    chmod +x bin/bifrost
    echo "  Bifrost built from source"
    if ./bin/bifrost --version 2>/dev/null; then
        true
    fi
    echo "  Bifrost ready at bin/bifrost"
else
    rm -rf "$BIFROST_SRC"
    echo "ERROR: Build failed. Check Go version and dependencies."
    exit 1
fi
