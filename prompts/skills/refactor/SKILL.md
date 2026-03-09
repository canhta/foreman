---
name: refactor
description: "Refactoring workflow — ensures behavior preservation"
trigger: post_lint
steps:
  - id: behavior-check
    type: run_command
    command: "sh"
    args: ["-c", "[ -f go.mod ] || exit 0; go test ./..."]
    allow_failure: false
---

# Refactor Skill

This skill runs after linting to ensure refactoring changes preserve existing behavior by executing the full test suite.
