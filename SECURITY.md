# Security Policy

## Threat Model

Foreman is an autonomous daemon that:

- **Clones repositories** and checks out branches
- **Executes LLM-generated code** in sandboxed environments (Docker or local)
- **Holds API keys** for LLM providers, issue trackers, and git hosts
- **Makes autonomous git commits and opens pull requests**

These capabilities require careful deployment. Foreman is designed to run in trusted environments where the operator controls repository access and review policies.

## Security Design

### Secrets Management

- API keys are loaded from environment variables via `${ENV_VAR}` substitution in `foreman.toml` — secrets are never stored in config files
- The built-in secrets scanner (`internal/context/`) detects and excludes secrets from LLM context assembly
- Dashboard auth tokens are stored as SHA-256 hashes in the database

### Code Execution Isolation

- **Docker mode** (recommended for production): LLM-generated code runs inside disposable containers with no host network access
- **Local mode**: Code runs directly on the host — use only in trusted/development environments

### Dashboard

- Bearer token authentication on all API endpoints
- Default bind to `127.0.0.1` (loopback only)
- Do not expose to `0.0.0.0` without a reverse proxy and TLS

### Git Operations

- File reservations prevent parallel pipelines from modifying the same files
- All commits are attributed to the Foreman bot identity
- PRs require human review before merge (Foreman does not merge its own PRs)

## Supported Versions

| Version | Supported |
|---------|-----------|
| latest  | Yes       |

## Reporting a Vulnerability

If you discover a security vulnerability, please report it responsibly:

1. **Do not** open a public GitHub issue
2. Email **security@foreman.dev** with a description of the vulnerability
3. Include steps to reproduce if possible
4. You will receive an acknowledgment within 48 hours

We will coordinate disclosure and credit you in the advisory unless you prefer to remain anonymous.

## Best Practices for Operators

- Run Foreman with Docker runner mode in production
- Use read-only tokens where possible (e.g., tracker read + git write)
- Set daily and monthly cost budgets in `foreman.toml` to limit runaway LLM spend
- Review the `AGENTS.md` injected into LLM context — it controls what the LLM "knows" about your repo
- Monitor the `/api/metrics` Prometheus endpoint for anomalies
- Rotate dashboard tokens periodically via `./foreman token generate`
