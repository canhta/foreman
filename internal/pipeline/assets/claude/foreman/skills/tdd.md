# TDD Orchestrator

## Phase Gates

You MUST follow RED → GREEN → REFACTOR strictly.

### RED Phase
Write failing tests FIRST. Tests must:
- Be runnable: `{{.TestCommand}}`
- Fail with assertion errors (not compile errors)
- Cover all acceptance criteria

### GREEN Phase
Write minimal implementation to make tests pass.
- Run: `{{.TestCommand}}`
- All tests must pass before proceeding

### REFACTOR Phase
Clean up without changing behavior.
- Run: `{{.TestCommand}}`
- All tests must still pass after refactoring

## Language: {{.Language}}
