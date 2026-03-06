# Integrations

Foreman connects to three categories of external services: issue trackers (to source work), LLM providers (to perform AI tasks), and git/PR hosts (to manage code and pull requests). Each category is backed by a Go interface — implementations are swappable via configuration.

For the full TOML reference for every integration, see [Configuration](configuration.md).

---

## Issue Trackers

### GitHub Issues

**Setup:**

1. Create a GitHub personal access token (or a bot account token) with `repo` scope.
2. Add the `foreman-ready` label to your repository.
3. For any issue you want Foreman to process, apply the `foreman-ready` label.

**Behaviour:**

- Foreman polls the GitHub Issues API for open issues with the pickup label.
- A comment is posted at each major pipeline stage (planning complete, PR created, blocked, etc.).
- The PR URL is attached to the issue.
- For clarification requests, the `foreman-needs-info` label is applied and removed once the author responds.
- When decomposition is enabled, child tickets are created as new GitHub Issues with a parent reference in the body.

**Required token permissions:** `repo` (full repo access) or at minimum `issues:write` and `pull-requests:write`.

---

### Jira (Cloud and Server)

**Setup:**

1. Create a Jira API token at `https://id.atlassian.com/manage-profile/security/api-tokens`.
2. Create a `foreman-ready` label in your Jira project.
3. Map the four status names (`status_in_progress`, `status_in_review`, `status_done`, `status_blocked`) to the exact names in your Jira workflow.

**Behaviour:**

- Foreman queries the Jira REST API for issues in your project with the pickup label.
- Status transitions are applied automatically at each pipeline stage.
- Comments are posted with progress updates.
- The PR URL is added as a link to the Jira issue.
- When decomposition is enabled, child tickets are created as Jira sub-tasks linked to the parent issue.

**Note:** Status transitions must match your project workflow exactly. Use `foreman doctor` to verify the status names are reachable.

---

### Linear

**Setup:**

1. Create a Linear API key at `https://linear.app/settings/api`.
2. Find your team ID in `https://linear.app/[workspace]/settings/teams`.
3. Apply the `foreman-ready` label to any issue you want processed.

**Behaviour:** Same as GitHub Issues — comment posting, PR attachment, label-based clarification flow. Decomposition creates child issues via the `issueCreate` GraphQL mutation.

---

### Local File Tracker

For local development and CI testing without an external issue tracker. Decomposition creates child ticket JSON files in the tickets directory. Ticket format — a JSON file per ticket in the configured directory:

```json
{
  "external_id": "LOCAL-001",
  "title": "Add user authentication",
  "description": "Implement JWT-based authentication for the API.",
  "acceptance_criteria": "- POST /auth/login returns a JWT on valid credentials\n- All protected routes return 401 without a valid token\n- Tokens expire after 24 hours",
  "labels": ["foreman-ready"],
  "priority": "high"
}
```

To trigger processing, add `"foreman-ready"` to `labels`. Foreman writes status updates and comments back to the file by adding a `_status` and `_comments` field.

---

## LLM Providers

### Anthropic

**Supported models:** Claude Haiku, Sonnet, Opus (any available via the Anthropic Messages API).

**Features specific to Anthropic:**
- Native structured output via forced tool use (`tool_choice: {type: "tool"}`)
- Extended thinking via the `thinking` parameter (for complex reasoning tasks in skills)
- Prompt caching (`cache_control: {type: "ephemeral"}`) to reduce repeated context costs

**Recommended model pairings:**

| Role | Model | Rationale |
|---|---|---|
| `planner` | `claude-sonnet-4-5-20250929` | Requires strong reasoning for decomposition |
| `implementer` | `claude-sonnet-4-5-20250929` | Primary coding role — use the best model |
| `spec_reviewer` | `claude-haiku-4-5-20251001` | Lighter task; cost savings |
| `quality_reviewer` | `claude-haiku-4-5-20251001` | Lighter task; cost savings |
| `final_reviewer` | `claude-sonnet-4-5-20250929` | Full-diff review needs reasoning |
| `clarifier` | `claude-haiku-4-5-20251001` | Simple question generation |

---

### OpenAI

**Supported models:** GPT-4o, o1, o3-mini, and any model accessible via the OpenAI Chat Completions API.

**Features:**
- Structured output via `response_format: {type: "json_schema"}`
- Function calling / tool-use for the builtin agent runner

**Note:** The `o1` and `o3` reasoning models have constraints on system prompts and temperature. If you route a role to these models, ensure your prompt templates are compatible.

---

### OpenRouter

Route to any model available on OpenRouter, including models from Anthropic, OpenAI, Google, Meta, Mistral, and others. Uses the same request/response format as OpenAI. Tool-use support depends on the underlying model.

**Example: route the implementer through OpenRouter:**

```toml
[models]
implementer = "openrouter:anthropic/claude-sonnet-4-5-20250929"
```

---

### Local Models (Ollama and OpenAI-compatible servers)

Any server that implements the OpenAI Chat Completions API can be used as the `local` provider.

**With Ollama:**

```bash
# Install Ollama: https://ollama.com
ollama pull llama3.2
ollama serve
```

Then set `default_provider = "local"` and specify the model in `[models]`:

```toml
[models]
implementer = "local:llama3.2"
```

**Tool-use with local models:** The builtin agent runner attempts tool calls against the local provider. If the model does not return a `tool_use` stop reason, the runner falls back to treating the response as a single-turn text answer. This allows the builtin runner to work with local models that do not support tools, at the cost of multi-turn agentic behaviour.

---

## Git and PR Hosting

### GitHub

Default and most tested backend. Token requirements: `repo` scope for private repos; `public_repo` for public repos. The token must have permission to push branches and create pull requests.

### GitLab

Token requirements: A personal access token or project access token with `api` and `write_repository` scopes.

### Bitbucket

Uses an app password (`BITBUCKET_APP_PASSWORD`). **Note:** Bitbucket integration is defined in the interface but may have gaps. GitHub is the primary tested backend.

### go-git Fallback

When the `git` CLI is not available, Foreman falls back to a pure Go git implementation. Enable explicitly with `backend = "gogit"` in `[git]`, or it activates automatically if `native` is selected but `git` is not on `$PATH`.

**Note:** The go-git fallback may have gaps for complex rebase scenarios. If you encounter rebase issues during PR creation, ensure the `git` CLI is available.

---

## Messaging Channels

### WhatsApp

Foreman integrates with WhatsApp via the Web multi-device protocol (whatsmeow). This provides a bidirectional messaging channel: operators can send commands and ticket descriptions via DM, and Foreman sends proactive notifications at pipeline milestones.

**Setup:**

1. Add the `[channel]` section to `foreman.toml`:
   ```toml
   [channel]
   provider = "whatsapp"

   [channel.whatsapp]
   session_db      = "~/.foreman/whatsapp.db"
   dm_policy       = "allowlist"
   allowed_numbers = ["+84123456789"]
   ```

2. Link your WhatsApp account:
   ```bash
   ./foreman channel login --phone +84123456789
   # Or: ./foreman channel login --mode qr
   ```

3. Start the daemon as usual — the channel connects automatically.

**Behaviour:**

- Messages starting with `/status`, `/pause`, `/resume`, or `/cost` execute daemon commands.
- Free-text messages from allowed senders are classified by the LLM and can create new tickets.
- The orchestrator sends notifications when tickets are picked up, need clarification, start implementation, have PRs created, or fail.
- Per-sender rate limiting (5 messages/60 seconds) prevents abuse.

**Pairing mode:** Set `dm_policy = "pairing"` to allow unknown senders to request access. They receive a time-limited code; an operator approves with `foreman pairing approve <CODE>`.

---

## Connecting to Multiple Providers

You can configure multiple LLM providers simultaneously and route different pipeline roles to different ones:

```toml
[llm]
default_provider = "anthropic"

[llm.anthropic]
api_key = "${ANTHROPIC_API_KEY}"

[llm.openai]
api_key = "${OPENAI_API_KEY}"

[models]
planner          = "anthropic:claude-sonnet-4-5-20250929"
implementer      = "anthropic:claude-sonnet-4-5-20250929"
spec_reviewer    = "openai:gpt-4o"
quality_reviewer = "openai:gpt-4o"
final_reviewer   = "anthropic:claude-sonnet-4-5-20250929"
clarifier        = "anthropic:claude-haiku-4-5-20251001"

# Fall back to OpenAI if Anthropic is down
[llm.outage]
fallback_provider = "openai"
```
