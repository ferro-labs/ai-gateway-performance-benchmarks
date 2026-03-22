#!/usr/bin/env bash
# scripts/setup-mockserver.sh — Build Go mock server and bench tools from source.
# Idempotent: safe to run multiple times, always rebuilds from latest local code.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR/.."

echo "==> Setting up Mock Server..."

if ! command -v go &>/dev/null; then
    echo "ERROR: Go not found. Install Go 1.24+ from https://go.dev/doc/install"
    exit 1
fi

GO_VER=$(go version | grep -oP '\d+\.\d+' | head -1)
echo "  Go version: $GO_VER"

mkdir -p bin

echo "  Building bin/mockserver..."
go build -o bin/mockserver ./cmd/mockserver

echo "  Building bin/bench..."
go build -o bin/bench ./cmd/bench

echo "  Building bin/report..."
go build -o bin/report ./cmd/report

echo "  Mock server ready at bin/mockserver"
echo "  Bench runner ready at bin/bench"
echo "  Report generator ready at bin/report"
