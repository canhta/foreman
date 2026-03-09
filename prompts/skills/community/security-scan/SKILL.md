---
name: security-scan
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
    allowed_tools:
      - Read
      - Glob
      - Grep
    max_turns: 6
    output_key: result

  - id: write-report
    type: file_write
    path: .foreman/security-report.json
    content: "{{ steps_audit_result }}"
    mode: overwrite
---

# Security Scan Skill

This community skill runs after linting to audit the codebase for security vulnerabilities and writes a structured JSON report to `.foreman/security-report.json`.
