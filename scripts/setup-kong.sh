#!/usr/bin/env bash
# scripts/setup-kong.sh — Install Kong Gateway natively from official apt repo.
# Idempotent: skips install if Kong is already present.
# Requires sudo for apt operations.
set -euo pipefail

echo "==> Setting up Kong Gateway (native)..."

# Check if already installed
if command -v kong &>/dev/null; then
    CURRENT=$(kong version 2>/dev/null | head -1)
    echo "  Kong already installed: $CURRENT"
    echo "  To upgrade: sudo apt-get update && sudo apt-get upgrade kong-enterprise-edition"
    echo "  Kong ready"
    exit 0
fi

# Require Debian/Ubuntu
if ! command -v apt-get &>/dev/null; then
    echo "ERROR: apt-get not found. This script supports Debian/Ubuntu only."
    echo "  For other platforms, see: https://docs.konghq.com/gateway/latest/install/"
    exit 1
fi

# Detect Debian/Ubuntu release
if [ -f /etc/os-release ]; then
    . /etc/os-release
    DISTRO="$ID"
    CODENAME="${VERSION_CODENAME:-}"
    echo "  Detected: $PRETTY_NAME"
else
    echo "WARNING: Cannot detect OS release, assuming Debian bookworm"
    DISTRO="debian"
    CODENAME="bookworm"
fi

# Map to Kong's supported codenames
case "$DISTRO/$CODENAME" in
    debian/bookworm) KONG_DIST="debian"; KONG_CODENAME="bookworm" ;;
    debian/bullseye) KONG_DIST="debian"; KONG_CODENAME="bullseye" ;;
    ubuntu/noble)    KONG_DIST="ubuntu"; KONG_CODENAME="noble" ;;
    ubuntu/jammy)    KONG_DIST="ubuntu"; KONG_CODENAME="jammy" ;;
    ubuntu/focal)    KONG_DIST="ubuntu"; KONG_CODENAME="focal" ;;
    *)
        echo "WARNING: Unrecognized $DISTRO/$CODENAME, attempting bookworm"
        KONG_DIST="debian"
        KONG_CODENAME="bookworm"
        ;;
esac

echo "  Adding Kong apt repository for $KONG_DIST/$KONG_CODENAME..."
echo "  (requires sudo)"

# Add Kong GPG key
curl -fsSL https://packages.konghq.com/public/gateway-38/gpg.key \
    | sudo gpg --dearmor -o /usr/share/keyrings/kong-gateway-38-archive-keyring.gpg 2>/dev/null

# Add Kong apt source
echo "deb [signed-by=/usr/share/keyrings/kong-gateway-38-archive-keyring.gpg] https://packages.konghq.com/public/gateway-38/deb/${KONG_DIST} ${KONG_CODENAME} main" \
    | sudo tee /etc/apt/sources.list.d/kong-gateway-38.list >/dev/null

# Install
echo "  Running apt-get update..."
sudo apt-get update -qq

echo "  Installing kong-enterprise-edition..."
if sudo apt-get install -y -qq kong-enterprise-edition; then
    INSTALLED=$(kong version 2>/dev/null | head -1)
    echo "  Kong $INSTALLED installed"
    echo "  Kong ready"
else
    echo "ERROR: apt install failed."
    echo "  Try manual install: https://docs.konghq.com/gateway/latest/install/linux/${KONG_DIST}/"
    exit 1
fi
