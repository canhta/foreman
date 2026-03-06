#!/usr/bin/env bash
set -euo pipefail

# deploy/install-systemd.sh
# Installs Foreman as a systemd service on Linux.
# Usage: sudo ./deploy/install-systemd.sh [path-to-binary]
#
# Prerequisites:
#   - Linux with systemd
#   - Root privileges (sudo)
#   - Foreman binary built with CGO_ENABLED=1

BINARY="${1:-./foreman}"
INSTALL_DIR="/var/lib/foreman"
CONFIG_DIR="/etc/foreman"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

if [ ! -f "$BINARY" ]; then
    echo "Error: Binary not found at $BINARY"
    echo "Build first: CGO_ENABLED=1 go build -o foreman ./main.go"
    exit 1
fi

echo "=== Foreman Systemd Installer ==="

# 1. Create system user
if ! id -u foreman &>/dev/null; then
    echo "Creating foreman user..."
    useradd --system --shell /usr/sbin/nologin --home-dir "$INSTALL_DIR" foreman
else
    echo "User foreman already exists"
fi

# 2. Install binary
echo "Installing binary to /usr/local/bin/foreman..."
cp "$BINARY" /usr/local/bin/foreman
chmod 755 /usr/local/bin/foreman

# 3. Create directories
echo "Creating directories..."
mkdir -p "$INSTALL_DIR/.foreman"
mkdir -p "$CONFIG_DIR"
chown -R foreman:foreman "$INSTALL_DIR"

# 4. Copy config template if no config exists
if [ ! -f "$INSTALL_DIR/foreman.toml" ]; then
    if [ -f "$SCRIPT_DIR/../foreman.example.toml" ]; then
        cp "$SCRIPT_DIR/../foreman.example.toml" "$INSTALL_DIR/foreman.toml"
        chown foreman:foreman "$INSTALL_DIR/foreman.toml"
        echo "Copied foreman.example.toml -> $INSTALL_DIR/foreman.toml (edit before starting)"
    else
        echo "Warning: foreman.example.toml not found, create $INSTALL_DIR/foreman.toml manually"
    fi
fi

# 5. Create env file template if not exists
if [ ! -f "$CONFIG_DIR/env" ]; then
    cat > "$CONFIG_DIR/env" <<'ENVEOF'
# Foreman environment variables — edit before starting the service.
ANTHROPIC_API_KEY=
GITHUB_TOKEN=
FOREMAN_DASHBOARD_TOKEN=
ENVEOF
    chmod 600 "$CONFIG_DIR/env"
    echo "Created $CONFIG_DIR/env (edit before starting)"
else
    echo "Env file already exists at $CONFIG_DIR/env"
fi

# 6. Install systemd service
echo "Installing systemd service..."
cp "$SCRIPT_DIR/foreman.service" /etc/systemd/system/foreman.service
systemctl daemon-reload
systemctl enable foreman

echo ""
echo "=== Installation complete ==="
echo ""
echo "Next steps:"
echo "  1. Edit $INSTALL_DIR/foreman.toml with your settings"
echo "  2. Edit $CONFIG_DIR/env with your API keys"
echo "  3. Run: foreman doctor"
echo "  4. Run: sudo systemctl start foreman"
echo "  5. Check: sudo journalctl -u foreman -f"
