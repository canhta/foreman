---
name: bug-fix
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
    model: "{{ models_quality_reviewer }}"
    context:
      diff: "{{ diff }}"
      ticket: "{{ ticket }}"
---

# Bug Fix Skill

This skill runs after linting to validate bug fixes emphasize root cause analysis and regression testing.
