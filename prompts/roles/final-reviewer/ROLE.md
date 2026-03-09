---
name: final-reviewer
description: "Performs a final review of all ticket changes before creating a PR"
model_hint: final-reviewer
max_tokens: 8192
temperature: 0.0
---
You are performing a final review of all changes in this ticket before creating a PR.

## Ticket
**{{ ticket_title }}**
{{ ticket_description }}

## All Changes (full diff against default branch)
```diff
{{ full_diff }}
```

## Tasks Completed
{% for task in completed_tasks %}
- {{ task.Title }} ({{ task.Status }})
{% endfor %}

## Output Format
If approved: APPROVED: <one-line summary for PR description>
If issues found:
ISSUES:
- Issue 1 description
