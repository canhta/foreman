# Contributing to Foreman

Thanks for contributing to Foreman. This document describes the expected workflow for code, tests, and pull requests.

## Development Setup

### Prerequisites

- Go `1.26+`
- C toolchain for CGO (required by `github.com/mattn/go-sqlite3`)
- `git`

### Bootstrap

```bash
git clone https://github.com/canhta/foreman.git
cd foreman
make build
make test
```

Optional local config:

```bash
cp foreman.example.toml foreman.toml
```

## Branch and PR Workflow

1. Create a branch from `main`.
2. Keep changes focused and scoped to a single concern.
3. Add or update tests for behavioral changes.
4. Run checks locally before opening/updating the PR.
5. Open a PR with clear context and rationale.

Suggested branch naming:

- `feat/<short-description>`
- `fix/<short-description>`
- `chore/<short-description>`

## Local Checks Before PR

Run these from repo root:

```bash
make test
make lint
```

If `golangci-lint` is not installed, run at minimum:

```bash
go test ./...
go vet ./...
```

## Coding Standards

- Keep designs interface-first across `internal/` packages.
- Prefer deterministic behavior for scaffolding paths (git ops, runners, parsing).
- Wrap errors with context, for example: `fmt.Errorf("loading config: %w", err)`.
- Avoid panics in normal control flow; return typed or wrapped errors.
- Keep functions small and explicit; prioritize readability over cleverness.

## Testing Expectations

- Add unit tests for new logic.
- Update existing tests when behavior changes.
- Include regression tests for bug fixes.
- Keep tests deterministic and independent.

## Commit Message Guidelines

Use imperative, specific commit messages.

Good examples:

- `add sqlite busy-timeout validation`
- `implement planner yaml fallback parser`
- `fix fuzzy search replace threshold handling`

## Pull Request Checklist

- Clear summary of what changed and why.
- Linked issue/ticket when available.
- Notes on tradeoffs or follow-up work.
- Test evidence (commands run and outcomes).
- Screenshots/log samples when UI or output changes are relevant.

## Reporting Bugs

Please include:

- Expected behavior
- Actual behavior
- Reproduction steps
- Relevant logs or error output (with secrets redacted)
- Environment details (OS, Go version)

## Security

Do not post secrets or credentials in issues, discussions, or PRs.

If you discover a security issue, report it privately to the maintainers rather than opening a public issue.
