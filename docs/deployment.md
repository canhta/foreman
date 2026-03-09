# Deployment

This guide covers production deployment using Docker Compose or as a native systemd service, plus SSL setup, observability, and common troubleshooting steps.

> **First time?** Get Foreman running locally before deploying to production. See [Getting Started](getting-started.md).

## Prerequisites

| Resource | Minimum | Recommended |
|----------|---------|-------------|
| CPU      | 1 vCPU  | 2 vCPU      |
| RAM      | 1 GB    | 2 GB        |
| Disk     | 10 GB SSD | 20 GB SSD |
| OS       | Ubuntu 22.04+ or Debian 12+ | Ubuntu 22.04+ or Debian 12+ |

### Required API Keys

| Variable | Required | Purpose |
|----------|----------|---------|
| `ANTHROPIC_API_KEY` | Mandatory | LLM provider for all agent calls |
| `GITHUB_TOKEN` | Mandatory | GitHub tracker and git operations |
| `FOREMAN_DASHBOARD_TOKEN` | Recommended | Authenticate access to the web dashboard |

---

## Option A: Docker Compose

1. **Clone the repo:**
   ```bash
   git clone https://github.com/canhta/foreman.git && cd foreman
   ```

2. **Copy and edit the config:**
   ```bash
   cp foreman.example.toml foreman.toml
   # Edit foreman.toml with your tracker, LLM, and pipeline settings
   ```

3. **Create a `.env` file with your API keys:**
   ```bash
   ANTHROPIC_API_KEY=sk-ant-...
   GITHUB_TOKEN=ghp_...
   FOREMAN_DASHBOARD_TOKEN=your-dashboard-secret
   ```

4. **Start the stack:**
   ```bash
   docker compose up -d
   ```

5. **Set up SSH for private repositories (if applicable):**
   ```bash
   docker compose exec foreman foreman setup-ssh
   # Copy the printed public key → GitHub repo → Settings → Deploy Keys
   ```

6. **Verify the installation:**
   ```bash
   docker compose exec foreman foreman doctor
   ```

7. **Check logs:**
   ```bash
   docker compose logs -f
   ```

> **Warning:** Never run `docker compose down -v` — this destroys the database volume. Always use `docker compose up --build -d` to update in place.

---

## Option B: Systemd Native Binary

1. **Build the binary (includes dashboard assets):**
   ```bash
   make build
   ```

2. **Install with the systemd helper script:**
   ```bash
   sudo ./deploy/install-systemd.sh ./foreman
   ```

3. **Edit the configuration:**
   ```bash
   sudo vim /var/lib/foreman/foreman.toml
   ```

4. **Set API keys:**
   ```bash
   sudo vim /etc/foreman/env
   # Add: ANTHROPIC_API_KEY=sk-ant-...
   # Add: GITHUB_TOKEN=ghp_...
   # Add: FOREMAN_DASHBOARD_TOKEN=your-dashboard-secret
   ```

5. **Set up SSH for private repositories (if applicable):**
   ```bash
   foreman setup-ssh
   # Copy the printed public key → GitHub repo → Settings → Deploy Keys
   ```

6. **Verify the installation:**
   ```bash
   foreman doctor
   ```

7. **Start the service:**
   ```bash
   sudo systemctl start foreman
   ```

8. **Check logs:**
   ```bash
   sudo journalctl -u foreman -f
   ```

---

## SSL Setup (Optional)

Applies to both deployment options. Requires a domain with a DNS A record pointing to your server IP.

```bash
sudo ./scripts/setup-ssl.sh --domain foreman.example.com --email you@email.com
```

The script checks DNS resolution, installs nginx and certbot if needed, configures a reverse proxy with WebSocket support, and obtains an SSL certificate. Auto-renewal is handled by the certbot systemd timer.

---

## Observability

- **Logs:** `docker compose logs -f` or `journalctl -u foreman -f`
- **Dashboard:** `http://<server-ip>:8080` (or `https://<domain>` with SSL) — requires `FOREMAN_DASHBOARD_TOKEN`
- **Active pipelines:** `foreman ps --all`
- **Cost tracking:** `foreman cost today` / `foreman cost month` / `foreman cost ticket <ID>`
- **Metrics:** Prometheus endpoint at `/api/metrics`

---

## Updating

### Docker

```bash
git pull
docker compose up --build -d
```

### Systemd

```bash
git pull
make build
foreman doctor          # validate new binary before restart
sudo systemctl restart foreman
```

---

## Troubleshooting

| Problem | What to check |
|---------|---------------|
| `foreman doctor` fails | Verify API keys are set and valid. Check network connectivity to api.anthropic.com and github.com. |
| SSH / git clone fails | Run `foreman setup-ssh`, add the printed public key as a GitHub Deploy Key, then re-run `foreman doctor`. |
| Dashboard not accessible | Ensure port 8080 is open in your firewall. Confirm `FOREMAN_DASHBOARD_TOKEN` is set. |
| Daemon not picking up tickets | Check tracker configuration in `foreman.toml`. Verify `pickup_label` matches your issue labels. Run `foreman ps` to see current state. |
| High costs | Run `foreman cost today` to inspect spend. Adjust token and cost limits in `foreman.toml`. |

---

## See Also

- [Getting Started](getting-started.md) — local setup before deploying to a server
- [Configuration](configuration.md) — full `foreman.toml` reference
- [Dashboard](dashboard.md) — web UI, REST API, and authentication details
- [Integrations](integrations.md) — connecting to issue trackers and LLM providers
