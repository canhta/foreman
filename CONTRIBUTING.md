# Contributing to Foreman

Thanks for your interest in contributing.

## Development Setup

1. Install Go 1.26+ and a C toolchain (required by `github.com/mattn/go-sqlite3`).
2. Clone the repository.
3. Run:

```bash
make build
make test
```

## Workflow

1. Create a branch from `main`.
2. Make focused changes with tests.
3. Run before opening a PR:

```bash
make test
make lint
```

4. Open a draft PR with a clear summary and rationale.

## Coding Guidelines

- Follow existing package boundaries and interface-first design in `internal/`.
- Wrap errors with context (for example: `fmt.Errorf("context: %w", err)`).
- Prefer deterministic behavior in infrastructure paths (git ops, runners, parsing).
- Add tests for all functional changes.

## Commit Messages

Use clear, imperative commit messages, for example:

- `add sqlite busy timeout validation`
- `implement planner yaml fallback parser`

## Reporting Issues

When filing a bug, include:

- Expected behavior
- Actual behavior
- Reproduction steps
- Logs or error output (redact secrets)
- Environment details (OS, Go version)

## Security

Do not open public issues with secrets. If you find a sensitive vulnerability, report privately to the maintainers.
