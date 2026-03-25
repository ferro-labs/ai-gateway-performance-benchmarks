#!/usr/bin/env bash
# scripts/setup-portkey.sh — Install Portkey AI Gateway (TypeScript/Node.js).
set -euo pipefail

echo "==> Setting up Portkey Gateway..."

# Check Node.js 18+
if ! command -v node &>/dev/null; then
    echo "Installing Node.js LTS..."
    curl -fsSL https://deb.nodesource.com/setup_lts.x | sudo -E bash -
    sudo apt-get install -y nodejs
fi

NODE_VERSION=$(node --version)
echo "Node.js: $NODE_VERSION"

# Install latest Portkey gateway globally (sudo needed for system Node.js)
npm install -g @portkey-ai/gateway

# Get installed version
INSTALLED=$(npm list -g @portkey-ai/gateway --depth=0 2>/dev/null \
    | grep portkey | awk -F@ '{print $NF}')
echo "✓ Portkey $INSTALLED ready"
echo "  Run with: node $(npm root -g)/@portkey-ai/gateway/build/start-server.js --port=8787"

echo "$INSTALLED" > .portkey-version
