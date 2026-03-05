# Skills

The YAML skill engine lets you extend Foreman's pipeline with custom steps at three hook points — without modifying Go code. Skills are composable, typed, and run deterministically at fixed points in the pipeline.

---

## Hook Points

Skills are triggered at one of four pipeline hook points:

| Hook | When it runs | Common uses |
|---|---|---|
| `post_lint` | After lint passes, before spec review | Security scanning, custom static analysis |
| `pre_pr` | Before PR creation, after final review | Changelog generation, documentation updates |
| `post_pr` | After PR is created and tracker is synced | Slack notifications, Jira automations |
| `post_merge` | After PR is merged | Deployment triggers, cleanup tasks, metrics |

Hook failures are **logged but do not block the pipeline**. A skill that errors or times out is recorded as a `hook_skill_failed` event and execution continues.

Enable skills at hook points in your `foreman.toml`:

```toml
[pipeline.hooks]
post_lint  = ["security-scan"]
pre_pr     = ["write-changelog"]
post_pr    = []
post_merge = []
```

---

## Skill File Format

Skills are YAML files located in the `skills/` directory. Community contributions go in `skills/community/`. Each skill has a unique `id`, a `description`, a `trigger`, and a list of `steps`.

```yaml
id: my-skill            # Must be unique; used in foreman.toml hooks config
description: "What this skill does"
trigger: post_lint      # post_lint | pre_pr | post_pr | post_merge
steps:
  - id: step-id
    type: step-type
    # ... step-type-specific fields
```

---

## Step Types

### `llm_call`

Calls an LLM with a templated prompt. The response is stored under `steps.<step-id>.output`.

```yaml
- id: review
  type: llm_call
  prompt_template: |
    Review this diff for security issues:
    {{ .Diff }}
    Ticket: {{ .Ticket.Title }}
  model: "{{ .Models.QualityReviewer }}"
  context:
    diff: "{{ .Diff }}"
    ticket: "{{ .Ticket }}"
```

**Fields:**

| Field | Required | Description |
|---|---|---|
| `prompt_template` | Yes | Jinja2-compatible template. Variables: `.Diff`, `.Ticket`, `.Models.*`, `.Steps.<id>.*` |
| `model` | Yes | Provider:model string, or a template reference like `{{ .Models.Implementer }}` |
| `context` | No | Additional key-value context injected into the template |
| `output_key` | No | Key name to store the response under (default: `output`) |

---

### `run_command`

Runs a shell command in the working directory. Used for deterministic steps like running tests or formatters.

```yaml
- id: test
  type: run_command
  command: "go"
  args: ["test", "./..."]
  allow_failure: false
```

**Fields:**

| Field | Required | Description |
|---|---|---|
| `command` | Yes | Executable name (must be in the runner's allowed commands list for local runner) |
| `args` | No | Command arguments |
| `allow_failure` | No | If `false` (default), a non-zero exit code fails the skill and logs an error |
| `timeout_secs` | No | Override the runner's default timeout |

---

### `file_write`

Writes or modifies a file in the repo. Content can reference output from previous steps using template syntax.

```yaml
- id: write-report
  type: file_write
  path: ".foreman/report.json"
  content: "{{ .Steps.audit.result }}"
  mode: overwrite
```

**Fields:**

| Field | Required | Description |
|---|---|---|
| `path` | Yes | Repo-relative file path. Must not match secrets patterns. |
| `content` | Yes | File content. Template syntax supported. |
| `mode` | No | `overwrite` (default), `prepend`, or `append` |

---

### `git_diff`

Exposes the current working diff as a template variable for subsequent steps.

```yaml
- id: get-diff
  type: git_diff
  output_key: current_diff
```

The diff is then available in later steps as `{{ .Steps.get-diff.current_diff }}`.

---

### `agentsdk`

Delegates a task to the configured `AgentRunner`. This is the step type for tasks that require multi-turn reasoning or file exploration.

```yaml
- id: audit
  type: agentsdk
  content: |
    Review the code in the working directory for security issues.
    Focus on OWASP Top 10. Return JSON:
    {"severity": "low|medium|high|critical", "findings": [...]}
  allowed_tools:
    - Read
    - Glob
    - Grep
  max_turns: 6
  output_format: json
  output_schema:
    type: object
    properties:
      severity:
        type: string
        enum: [low, medium, high, critical]
      findings:
        type: array
        items:
          type: object
    required: [severity, findings]
  output_key: result
```

**Fields:**

| Field | Required | Description |
|---|---|---|
| `content` | Yes | The prompt / task description for the agent |
| `allowed_tools` | No | Restrict which tools the agent can use. Empty = runner default. |
| `max_turns` | No | Override the runner's max turns setting |
| `timeout_secs` | No | Override the runner's default timeout |
| `output_format` | No | `json`, `diff`, or `checklist` — used to set expectations in the prompt |
| `output_schema` | No | JSON Schema for structured output. Supported by builtin and Claude Code runners. |
| `fallback_model` | No | Model to use if the primary model fails |
| `output_key` | No | Key to store the result under (default: `output`) |

---

### `subskill`

Embeds another skill as a step. Used for composition.

```yaml
- id: scan-then-report
  type: subskill
  skill_id: security-scan
```

The embedded skill runs with the same context as the parent. Output from the subskill's steps is namespaced under the subskill step ID.

---

## Template Variables

The following variables are available in all template fields:

| Variable | Type | Description |
|---|---|---|
| `.Ticket` | object | The current ticket (`.Title`, `.Description`, `.AcceptanceCriteria`, `.ExternalID`, etc.) |
| `.Diff` | string | The full diff for the current task (or full ticket diff at `pre_pr`/`post_pr`) |
| `.Models` | object | Model names per role (`.Planner`, `.Implementer`, `.SpecReviewer`, `.QualityReviewer`, `.FinalReviewer`, `.Clarifier`) |
| `.Steps.<id>.*` | varies | Output from a previous step in the same skill |
| `.WorkDir` | string | Absolute path to the working repo directory |
| `.BranchName` | string | The current feature branch name |
| `.PRUrl` | string | The PR URL (available at `post_pr` only) |

---

## Built-in Skills

### `feature-dev`

Default workflow — no extra steps. Present as a template showing the skill format.

```yaml
id: feature-dev
description: "Default feature development workflow — no extra steps"
trigger: post_lint
steps: []
```

### `bug-fix`

Adds a regression check after lint. An LLM call reviews the diff to ensure the root cause is addressed and a regression test exists.

```yaml
id: bug-fix
description: "Bug fixing workflow — emphasizes regression tests"
trigger: post_lint
steps:
  - id: regression-check
    type: llm_call
    prompt_template: |
      Review this bug fix diff and check:
      1. Does the fix address the root cause, not just symptoms?
      2. Is there a regression test that would catch this bug if reintroduced?
      3. Are there related areas that might have the same bug?
      Respond with APPROVED or ISSUES: followed by a bullet list.
    model: "{{ .Models.QualityReviewer }}"
    context:
      diff:    "{{ .Diff }}"
      ticket:  "{{ .Ticket }}"
```

### `refactor`

Runs the full test suite after lint to confirm behaviour preservation.

```yaml
id: refactor
description: "Refactoring workflow — ensures behavior preservation"
trigger: post_lint
steps:
  - id: behavior-check
    type: run_command
    command: "go test"
    args: ["./..."]
    allow_failure: false
```

---

## Community Skills

Community-contributed skills live in `skills/community/`. They are not enabled by default — add them to your `[pipeline.hooks]` config to use them.

### `security-scan`

Uses the configured `AgentRunner` to scan the working directory for security vulnerabilities and writes a JSON report.

```yaml
id: security-scan
description: Review code changes for security vulnerabilities
trigger: post_lint
steps:
  - id: audit
    type: agentsdk
    content: |
      Review the code in the current working directory for security issues.
      Focus on: injection vulnerabilities, hardcoded secrets, insecure defaults,
      and OWASP Top 10 issues.
      Return JSON: {"severity": "low|medium|high|critical", "findings": [...]}
    allowed_tools: [Read, Glob, Grep]
    max_turns: 6
    output_key: result

  - id: write-report
    type: file_write
    path: .foreman/security-report.json
    content: "{{ .Steps.audit.result }}"
    mode: overwrite
```

Enable in `foreman.toml`:

```toml
[pipeline.hooks]
post_lint = ["security-scan"]
```

### `write-changelog`

Generates a changelog entry from the PR diff and ticket context, prepended to `CHANGELOG.md`.

```yaml
id: write-changelog
description: "Generate a changelog entry from the PR diff and ticket context"
trigger: pre_pr
steps:
  - id: generate
    type: llm_call
    prompt_template: |
      Generate a concise changelog entry for this change.
      Format: "- [TYPE] Description (ticket reference)"
      Types: Added, Changed, Fixed, Removed
      Diff: {{ .Diff }}
      Ticket: {{ .Ticket.Title }}
    model: "{{ .Models.Clarifier }}"
  - id: write
    type: file_write
    path: "CHANGELOG.md"
    content: "{{ .Steps.generate.output }}"
    mode: prepend
```

Enable in `foreman.toml`:

```toml
[pipeline.hooks]
pre_pr = ["write-changelog"]
```

---

## Writing a Custom Skill

1. Create a YAML file in `skills/` (or `skills/community/` for sharing):

```yaml
id: notify-slack
description: "Post a Slack message after PR creation"
trigger: post_pr
steps:
  - id: notify
    type: run_command
    command: "curl"
    args:
      - "-X"
      - "POST"
      - "-H"
      - "Content-type: application/json"
      - "--data"
      - '{"text":"PR created: {{ .PRUrl }} for {{ .Ticket.Title }}"}'
      - "${SLACK_WEBHOOK_URL}"
    allow_failure: true
```

2. Add the skill ID to the appropriate hook in `foreman.toml`:

```toml
[pipeline.hooks]
post_pr = ["notify-slack"]
```

3. Restart the daemon (`./foreman stop && ./foreman start`). Skill files are loaded at startup.

---

## Skill Execution Context

Each skill step runs with access to the same execution environment as the pipeline:

- **Working directory**: the cloned repository at the current pipeline state
- **Command runner**: the configured local or Docker runner with the same allowed commands and forbidden paths
- **LLM provider**: the configured provider accessible via the `llm_call` and `agentsdk` step types
- **AgentRunner**: the configured runner (builtin, claudecode, or copilot) for `agentsdk` steps

Skills do **not** have direct database access. They communicate with the pipeline via output keys that can be referenced by subsequent steps in the same skill.

---

## Contributing a Community Skill

To contribute a skill to the `skills/community/` directory:

1. Create the skill YAML file following the format above.
2. Test it locally by adding it to your `[pipeline.hooks]` and running `./foreman run <ticket-id>`.
3. Open a pull request to the Foreman repository with:
   - The skill YAML file in `skills/community/`
   - A brief description in the PR of what the skill does and when it's useful
   - Evidence that it was tested on at least one real ticket
