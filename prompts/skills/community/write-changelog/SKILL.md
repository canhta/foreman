---
name: write-changelog
description: "Generate a changelog entry from the PR diff and ticket context"
trigger: pre_pr
steps:
  - id: generate
    type: llm_call
    prompt_template: |
      Generate a concise changelog entry for this change.
      Format: "- [TYPE] Description (ticket reference)"
      Types: Added, Changed, Fixed, Removed
      Diff: {{ diff }}
      Ticket: {{ ticket_title }}
    model: "{{ models_clarifier }}"
    context:
      diff: "{{ diff }}"
      ticket: "{{ ticket }}"
  - id: write
    type: file_write
    path: "CHANGELOG.md"
    content: "{{ steps_generate_output }}"
    mode: prepend
---

# Write Changelog Skill

This community skill runs before pull request creation to automatically generate a formatted changelog entry from the PR diff and associated ticket context.
