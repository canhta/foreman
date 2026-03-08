#!/usr/bin/env bash
set -euo pipefail

# scripts/setup-ssl.sh
# Sets up Nginx reverse proxy with Let's Encrypt SSL for Foreman dashboard.
#
# PREREQUISITE: Your domain's DNS A record must point to this server's
# public IP address before running this script. Certbot uses the HTTP-01
# challenge, which requires the domain to resolve to this server.
#
# Usage: sudo ./scripts/setup-ssl.sh --domain foreman.example.com --email you@email.com

DOMAIN=""
EMAIL=""
UPSTREAM="127.0.0.1:8080"

while [[ $# -gt 0 ]]; do
    case $1 in
        --domain) DOMAIN="$2"; shift 2 ;;
        --email)  EMAIL="$2"; shift 2 ;;
        --upstream) UPSTREAM="$2"; shift 2 ;;
        -h|--help)
            echo "Usage: $0 --domain <domain> --email <email> [--upstream host:port]"
            echo ""
            echo "Sets up Nginx reverse proxy with SSL for Foreman dashboard."
            echo ""
            echo "Options:"
            echo "  --domain     Domain name (required, must have DNS A record pointing here)"
            echo "  --email      Email for Let's Encrypt notifications (required)"
            echo "  --upstream   Foreman dashboard address (default: 127.0.0.1:8080)"
            exit 0
            ;;
        *) echo "Unknown argument: $1. Use --help for usage."; exit 1 ;;
    esac
done

if [[ -z "$DOMAIN" || -z "$EMAIL" ]]; then
    echo "Error: --domain and --email are required."
    echo "Usage: $0 --domain foreman.example.com --email you@email.com"
    exit 1
fi

echo "=== Foreman SSL Setup ==="
echo "Domain:   $DOMAIN"
echo "Email:    $EMAIL"
echo "Upstream: $UPSTREAM"
echo ""

# 1. DNS pre-flight check
echo "Checking DNS resolution..."
SERVER_IP=$(curl -4 -s --max-time 10 ifconfig.me || echo "")
if [[ -z "$SERVER_IP" ]]; then
    echo "Warning: Could not determine server public IP. Skipping DNS check."
else
    DOMAIN_IP=$(dig +short "$DOMAIN" | tail -1)
    if [[ -z "$DOMAIN_IP" ]]; then
        echo "Error: $DOMAIN does not resolve to any IP address."
        echo "Point your DNS A record to $SERVER_IP and wait for propagation."
        exit 1
    fi
    if [[ "$SERVER_IP" != "$DOMAIN_IP" ]]; then
        echo "Error: $DOMAIN resolves to $DOMAIN_IP but this server is $SERVER_IP"
        echo "Point your DNS A record to $SERVER_IP and wait for propagation."
        exit 1
    fi
    echo "DNS OK: $DOMAIN -> $SERVER_IP"
fi

# 2. Install nginx + certbot if missing
echo "Checking dependencies..."
if ! command -v nginx &>/dev/null; then
    echo "Installing nginx..."
    apt-get update -qq && apt-get install -y -qq nginx
fi

if ! command -v certbot &>/dev/null; then
    echo "Installing certbot..."
    apt-get update -qq && apt-get install -y -qq certbot python3-certbot-nginx
fi

# 3. Write nginx site config
echo "Writing nginx configuration..."
cat > /etc/nginx/sites-available/foreman <<NGINXEOF
server {
    listen 80;
    server_name $DOMAIN;

    location / {
        proxy_pass http://$UPSTREAM;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;

        # WebSocket support for dashboard live updates
        proxy_http_version 1.1;
        proxy_set_header Upgrade \$http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_read_timeout 86400;
    }
}
NGINXEOF

# 4. Enable site
ln -sf /etc/nginx/sites-available/foreman /etc/nginx/sites-enabled/foreman

# Remove default site if it would conflict on port 80
if [ -f /etc/nginx/sites-enabled/default ]; then
    rm -f /etc/nginx/sites-enabled/default
fi

echo "Testing nginx configuration..."
nginx -t
systemctl reload nginx

# 5. Obtain SSL certificate
echo "Obtaining SSL certificate via Let's Encrypt..."
certbot --nginx -d "$DOMAIN" --non-interactive --agree-tos -m "$EMAIL"

# 6. Final reload
systemctl reload nginx

echo ""
echo "=== SSL Setup Complete ==="
echo "Dashboard available at https://$DOMAIN"
echo ""
echo "Certificate auto-renewal is handled by certbot's systemd timer."
echo "Verify with: sudo certbot renew --dry-run"
