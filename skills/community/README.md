# Community Skills

User-contributed skills for the Foreman YAML skill engine.

## Schema

Each skill is a YAML file with the following structure:

```yaml
id: my-skill                    # Unique identifier
description: "What this skill does"
trigger: post_lint | pre_pr | post_pr   # Hook point

steps:
  - id: step-name               # Unique within this skill
    type: llm_call | run_command | file_write | git_diff | agentsdk | subskill
    # Type-specific fields (see below)
```

### Step Types

| Type | Key Fields | Description |
|------|-----------|-------------|
| `llm_call` | `prompt_template`, `model`, `context` | Send a prompt to the configured LLM |
| `run_command` | `command` | Execute a shell command |
| `file_write` | `path`, `content`, `mode` (overwrite/prepend/append) | Write output to a file |
| `git_diff` | — | Capture the current git diff |
| `agentsdk` | `content`, `allowed_tools`, `max_turns`, `output_key` | Run an agent with tool access |
| `subskill` | `skill_id` | Invoke another skill |

### Template Variables

Steps can reference:
- `{{ .Diff }}` — current git diff
- `{{ .Ticket }}` — ticket object (`.Title`, `.Description`, etc.)
- `{{ .Models.Clarifier }}` / `{{ .Models.Implementer }}` — configured model names
- `{{ .Steps.<step-id>.<output_key> }}` — output from a previous step

## Included Skills

- **security-scan.yml** — Post-lint security audit using agent tools (Read, Glob, Grep)
- **write-changelog.yml** — Pre-PR changelog generation from diff and ticket context

## Contributing

1. Create a YAML file following the schema above
2. Use existing skills as reference
3. Test your skill locally with `./foreman skill run <path>`
4. Submit a PR to this directory
