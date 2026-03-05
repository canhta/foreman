# Contributing to Foreman

Thanks for contributing to Foreman. For dev environment setup, package conventions, testing strategy, and how to add new features, see [docs/development.md](docs/development.md).

## Quick Checks Before Opening a PR

```bash
make test
make lint
```

If `golangci-lint` is not installed:

```bash
go test ./...
go vet ./...
```

## Branch Naming

- `feat/<short-description>`
- `fix/<short-description>`
- `chore/<short-description>`

## Commit Messages

Use imperative, specific messages:

- `add sqlite busy-timeout validation`
- `implement planner yaml fallback parser`
- `fix fuzzy search replace threshold handling`

## PR Workflow

1. Branch from `main`.
2. Keep changes focused on a single concern.
3. Run checks locally before opening the PR.
4. Open a PR with clear context and rationale.

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
- Relevant logs or error output
- Environment details (OS, Go version)

## Security

Never post secrets or credentials in issues, discussions, logs, or PRs.

If you discover a security issue, report it privately to the maintainers rather than opening a public issue.
