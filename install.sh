#!/bin/bash
# install.sh — Install Foreman binary
set -euo pipefail

REPO="canhta/foreman"
INSTALL_DIR="/usr/local/bin"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

LATEST=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed -E 's/.*"([^"]+)".*/\1/')
URL="https://github.com/${REPO}/releases/download/${LATEST}/foreman-${LATEST#v}-${OS}-${ARCH}.tar.gz"

echo "Installing Foreman ${LATEST} (${OS}/${ARCH})..."
TMP=$(mktemp -d)
curl -fsSL "$URL" -o "${TMP}/foreman.tar.gz"
tar -xzf "${TMP}/foreman.tar.gz" -C "$TMP"

if [ -w "$INSTALL_DIR" ]; then
  mv "${TMP}/foreman" "$INSTALL_DIR/foreman"
else
  sudo mv "${TMP}/foreman" "$INSTALL_DIR/foreman"
fi

rm -rf "$TMP"
echo "Foreman installed to ${INSTALL_DIR}/foreman"
foreman --version
