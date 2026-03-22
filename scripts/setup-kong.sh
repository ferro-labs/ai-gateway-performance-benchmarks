#!/usr/bin/env bash
# scripts/setup-kong.sh — Install Kong OSS (Apache 2.0) from official apt repo.
# Kong OSS (Apache 2.0) — NOT Kong Enterprise.
# Idempotent: skips install if Kong OSS is already present.
# Requires sudo for apt operations.
set -euo pipefail

echo "==> Setting up Kong Gateway OSS..."

# Check if already installed
if command -v kong &>/dev/null; then
    CURRENT=$(kong version 2>/dev/null | head -1)
    # Warn if Enterprise is installed instead of OSS
    if echo "$CURRENT" | grep -qi enterprise; then
        echo "  WARNING: Kong Enterprise detected ($CURRENT)"
        echo "  Removing Enterprise and installing OSS for fair benchmarking..."
        sudo apt-get remove -y kong-enterprise-edition -qq 2>/dev/null || true
    else
        echo "  Kong OSS already installed: $CURRENT"
        echo "  To upgrade: sudo apt-get update && sudo apt-get upgrade kong"
        echo "  Kong ready"
        exit 0
    fi
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

# Use gateway-39 channel for Kong OSS 3.9.x
KONG_CHANNEL="gateway-39"

echo "  Adding Kong OSS apt repository ($KONG_CHANNEL) for $KONG_DIST/$KONG_CODENAME..."
echo "  (requires sudo)"

# Add Kong GPG key
curl -fsSL "https://packages.konghq.com/public/${KONG_CHANNEL}/gpg.key" \
    | sudo gpg --dearmor -o "/usr/share/keyrings/kong-${KONG_CHANNEL}-archive-keyring.gpg" 2>/dev/null

# Add Kong OSS apt source
echo "deb [signed-by=/usr/share/keyrings/kong-${KONG_CHANNEL}-archive-keyring.gpg] https://packages.konghq.com/public/${KONG_CHANNEL}/deb/${KONG_DIST} ${KONG_CODENAME} main" \
    | sudo tee "/etc/apt/sources.list.d/kong-${KONG_CHANNEL}.list" >/dev/null

# Install Kong OSS (package name: "kong", NOT "kong-enterprise-edition")
echo "  Running apt-get update..."
sudo apt-get update -qq

echo "  Installing Kong OSS..."
if sudo apt-get install -y -qq kong; then
    INSTALLED=$(kong version 2>/dev/null | head -1)
    echo "  Kong $INSTALLED installed"
    echo "  Kong OSS ready"
else
    echo "ERROR: apt install failed."
    echo "  Try manual install: https://docs.konghq.com/gateway/latest/install/linux/${KONG_DIST}/"
    exit 1
fi
