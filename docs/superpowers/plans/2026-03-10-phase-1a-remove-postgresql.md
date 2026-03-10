# Phase 1a: Remove PostgreSQL Support — Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove all PostgreSQL support from Foreman, making SQLite the sole database backend.

**Architecture:** PostgreSQL is implemented as an alternative `Database` interface via `postgres.go` (~1,463 lines). It is referenced in config structs, driver selection logic (4 switch cases across CLI commands), config validation, environment variable expansion, documentation, and example configs. All references must be removed cleanly while keeping SQLite fully functional.

**Tech Stack:** Go, SQLite, Viper (config)

**Spec:** `docs/superpowers/specs/2026-03-10-multi-project-refactor-design.md` (Section 2, Phase 1a)

---

## Chunk 1: Core Removal

### Task 1: Delete PostgreSQL Implementation Files

**Files:**
- Delete: `internal/db/postgres.go`
- Delete: `internal/db/postgres_test.go`
- Modify: `internal/db/lock.go:9` (update comment)
- Delete function: `tests/integration/db_contract_test.go:131-142` (`TestDBContract_Postgres`)

- [ ] **Step 1: Verify existing tests pass before making changes**

Run: `go test ./internal/db/... -v -short -count=1`
Expected: All tests pass (PostgreSQL test skips due to no connection)

- [ ] **Step 2: Delete postgres.go**

```bash
rm internal/db/postgres.go
```

- [ ] **Step 3: Delete postgres_test.go**

```bash
rm internal/db/postgres_test.go
```

- [ ] **Step 4: Delete TestDBContract_Postgres from integration tests**

In `tests/integration/db_contract_test.go`, delete the `TestDBContract_Postgres` function (lines 131-142) which calls `db.NewPostgresDB` — this will fail to compile after postgres.go is deleted.

- [ ] **Step 5: Update comment in lock.go**

In `internal/db/lock.go`, change line 9 from:

```go
// It is shared by both SQLiteDB and PostgresDB lock implementations.
```

to:

```go
// It is shared by SQLiteDB lock implementations.
```

- [ ] **Step 6: Verify db package still compiles**

Run: `go build ./internal/db/...`
Expected: SUCCESS — no other file in `internal/db/` imports pgx or sqlx

- [ ] **Step 7: Commit**

```bash
git add internal/db/postgres.go internal/db/postgres_test.go internal/db/lock.go tests/integration/db_contract_test.go
git commit -m "refactor: delete PostgreSQL database implementation"
```

---

### Task 2: Remove PostgreSQL Config Struct

**Files:**
- Modify: `internal/models/config.go:303-319`

- [ ] **Step 1: Remove PostgresConfig struct and field from DatabaseConfig**

In `internal/models/config.go`, change the `DatabaseConfig` struct from:

```go
type DatabaseConfig struct {
	Driver   string         `mapstructure:"driver"`
	Postgres PostgresConfig `mapstructure:"postgres"`
	SQLite   SQLiteConfig   `mapstructure:"sqlite"`
}
```

to:

```go
type DatabaseConfig struct {
	Driver string       `mapstructure:"driver"`
	SQLite SQLiteConfig `mapstructure:"sqlite"`
}
```

And delete the `PostgresConfig` struct entirely (lines 316-319):

```go
type PostgresConfig struct {
	URL            string `mapstructure:"url"`
	MaxConnections int    `mapstructure:"max_connections"`
}
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./...`
Expected: FAIL — other files still reference `cfg.Database.Postgres`

- [ ] **Step 3: Commit**

```bash
git add internal/models/config.go
git commit -m "refactor: remove PostgresConfig struct from config model"
```

---

### Task 3: Remove PostgreSQL from Config Loading and Validation

**Files:**
- Modify: `internal/config/config.go:188,231-233`

- [ ] **Step 1: Remove PostgreSQL env var expansion**

In `internal/config/config.go`, in the `expandEnvVars` function, remove line 188:

```go
cfg.Database.Postgres.URL = expandEnv(cfg.Database.Postgres.URL)
```

- [ ] **Step 2: Update validation to enforce max_parallel_tickets <= 3 unconditionally**

In `internal/config/config.go`, in the `Validate` function, change lines 231-233 from:

```go
if cfg.Database.Driver == "sqlite" && cfg.Daemon.MaxParallelTickets > 3 {
	errs = append(errs, fmt.Errorf("max_parallel_tickets cannot exceed 3 with SQLite (got %d), use PostgreSQL for higher concurrency", cfg.Daemon.MaxParallelTickets))
}
```

to:

```go
if cfg.Daemon.MaxParallelTickets > 3 {
	errs = append(errs, fmt.Errorf("max_parallel_tickets cannot exceed 3 (got %d)", cfg.Daemon.MaxParallelTickets))
}
```

- [ ] **Step 3: Verify compilation**

Run: `go build ./internal/config/...`
Expected: SUCCESS

- [ ] **Step 4: Run config tests**

Run: `go test ./internal/config/... -v -count=1`
Expected: All tests pass

- [ ] **Step 5: Commit**

```bash
git add internal/config/config.go
git commit -m "refactor: remove PostgreSQL references from config loading and validation"
```

---

### Task 4: Remove PostgreSQL from CLI Commands

**Files:**
- Modify: `cmd/helpers.go:46-53`
- Modify: `cmd/config.go:86-91,158-166`
- Modify: `cmd/logs.go:41-47`

- [ ] **Step 1: Simplify openDB in helpers.go**

In `cmd/helpers.go`, change the `openDB` function from:

```go
func openDB(cfg *models.Config) (db.Database, error) {
	switch cfg.Database.Driver {
	case "postgres":
		return db.NewPostgresDB(cfg.Database.Postgres.URL, cfg.Database.Postgres.MaxConnections)
	default:
		return db.NewSQLiteDB(cfg.Database.SQLite.Path)
	}
}
```

to:

```go
func openDB(cfg *models.Config) (db.Database, error) {
	return db.NewSQLiteDB(cfg.Database.SQLite.Path)
}
```

- [ ] **Step 2: Simplify database display in config.go (text output)**

In `cmd/config.go`, change the DATABASE display section (lines 83-91) from:

```go
			// DATABASE
			fmt.Fprintln(w, "[DATABASE]")
			fmt.Fprintf(w, "  driver\t%s\n", cfg.Database.Driver)
			switch cfg.Database.Driver {
			case "postgres":
				fmt.Fprintf(w, "  url\t%s\n", redactConfigKey(cfg.Database.Postgres.URL))
			default:
				fmt.Fprintf(w, "  path\t%s\n", cfg.Database.SQLite.Path)
			}
```

to:

```go
			// DATABASE
			fmt.Fprintln(w, "[DATABASE]")
			fmt.Fprintf(w, "  driver\tsqlite\n")
			fmt.Fprintf(w, "  path\t%s\n", cfg.Database.SQLite.Path)
```

- [ ] **Step 3: Simplify database display in config.go (JSON output)**

In `cmd/config.go`, in the `buildConfigSummaryMap` function, change lines 158-166 from:

```go
	dbInfo := map[string]string{
		"driver": cfg.Database.Driver,
	}
	switch cfg.Database.Driver {
	case "postgres":
		dbInfo["url"] = redactConfigKey(cfg.Database.Postgres.URL)
	default:
		dbInfo["path"] = cfg.Database.SQLite.Path
	}
```

to:

```go
	dbInfo := map[string]string{
		"driver": "sqlite",
		"path":   cfg.Database.SQLite.Path,
	}
```

- [ ] **Step 4: Simplify database opening in logs.go**

In `cmd/logs.go`, change lines 40-47 from:

```go
		var database db.Database
		switch cfg.Database.Driver {
		case "postgres", "postgresql":
			maxConns := 10
			database, err = db.NewPostgresDB(cfg.Database.Postgres.URL, maxConns)
		default: // "sqlite" or empty
			database, err = db.NewSQLiteDB(expandHomePath(cfg.Database.SQLite.Path))
		}
```

to:

```go
		var database db.Database
		database, err = db.NewSQLiteDB(expandHomePath(cfg.Database.SQLite.Path))
```

- [ ] **Step 5: Verify compilation**

Run: `go build ./cmd/...`
Expected: SUCCESS

- [ ] **Step 6: Commit**

```bash
git add cmd/helpers.go cmd/config.go cmd/logs.go
git commit -m "refactor: remove PostgreSQL driver selection from CLI commands"
```

---

### Task 5: Remove PostgreSQL from Dashboard API

**Files:**
- Modify: `internal/dashboard/api.go:328-331`

- [ ] **Step 1: Simplify database path display**

In `internal/dashboard/api.go`, change lines 328-331 from:

```go
	dbPath := cfg.Database.SQLite.Path
	if cfg.Database.Driver == "postgres" {
		dbPath = redactKey(cfg.Database.Postgres.URL)
	}
```

to:

```go
	dbPath := cfg.Database.SQLite.Path
```

- [ ] **Step 2: Verify compilation**

Run: `go build ./internal/dashboard/...`
Expected: SUCCESS

- [ ] **Step 3: Commit**

```bash
git add internal/dashboard/api.go
git commit -m "refactor: remove PostgreSQL reference from dashboard API"
```

---

### Task 6: Remove pgx and sqlx Dependencies

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: Remove pgx and sqlx from go.mod**

Run: `go mod tidy`
Expected: pgx and sqlx removed from go.mod since no code imports them anymore

- [ ] **Step 2: Verify pgx is gone from go.mod**

Run: `grep -c "pgx\|sqlx" go.mod`
Expected: 0

- [ ] **Step 3: Full test suite**

Run: `go test ./... -short -count=1`
Expected: All tests pass

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "refactor: remove pgx and sqlx dependencies"
```

---

## Chunk 2: Documentation and Config Cleanup

### Task 7: Update Example Config

**Files:**
- Modify: `foreman.example.toml:7,159`

- [ ] **Step 1: Remove PostgreSQL reference from max_parallel_tickets comment**

In `foreman.example.toml`, change line 7 from:

```toml
max_parallel_tickets      = 3       # Max 3 for SQLite; use PostgreSQL for more
```

to:

```toml
max_parallel_tickets      = 3       # Hard limit: max 3
```

- [ ] **Step 2: Remove postgres from driver comment**

In `foreman.example.toml`, change line 159 from:

```toml
driver = "sqlite"  # sqlite, postgres
```

to:

```toml
driver = "sqlite"
```

- [ ] **Step 3: Commit**

```bash
git add foreman.example.toml
git commit -m "docs: remove PostgreSQL option from example config"
```

---

### Task 8: Update AGENTS.md

**Files:**
- Modify: `AGENTS.md:137`

- [ ] **Step 1: Remove pgx and sqlx from key dependencies**

In `AGENTS.md`, change line 137 from:

```markdown
- **Database:** go-sqlite3 (CGo required), pgx (PostgreSQL), sqlx
```

to:

```markdown
- **Database:** go-sqlite3 (CGo required)
```

- [ ] **Step 2: Commit**

```bash
git add AGENTS.md
git commit -m "docs: remove PostgreSQL references from AGENTS.md"
```

---

### Task 9: Update Architecture Docs

**Files:**
- Modify: `docs/architecture.md:209,317-318`
- Modify: `docs/configuration.md:415,428-436,636`

- [ ] **Step 1: Update architecture.md — db implementations line**

In `docs/architecture.md`, change line 209 from:

```markdown
Implementations: `sqlite.go` (default, serialized writer for concurrency safety), `postgres.go`.
```

to:

```markdown
Implementation: `sqlite.go` (serialized writer for concurrency safety).
```

- [ ] **Step 2: Update architecture.md — technology stack table**

In `docs/architecture.md`, remove lines 317-318:

```markdown
| Database (optional) | PostgreSQL via `pgx/v5` |
| SQL extensions | `sqlx` |
```

- [ ] **Step 3: Update configuration.md — driver options**

In `docs/configuration.md`, change line 415 from:

```markdown
driver = "sqlite"   # sqlite | postgres
```

to:

```markdown
driver = "sqlite"
```

- [ ] **Step 4: Update configuration.md — remove PostgreSQL section and SQLite note**

In `docs/configuration.md`, remove the PostgreSQL note and section (lines 428-436):

```markdown
> SQLite caps `max_parallel_tickets` at 3. For more concurrent pipelines, use PostgreSQL.

### PostgreSQL (Optional)

```toml
[database.postgres]
url             = "${DATABASE_URL}"   # postgres://user:pass@host:5432/foreman
max_connections = 10
```
```

Replace with:

```markdown
> `max_parallel_tickets` is capped at 3.
```

- [ ] **Step 5: Update configuration.md — remove DATABASE_URL env var**

In `docs/configuration.md`, remove line 636:

```markdown
| `DATABASE_URL` | `[database.postgres] url` |
```

- [ ] **Step 6: Commit**

```bash
git add docs/architecture.md docs/configuration.md
git commit -m "docs: remove PostgreSQL references from architecture and configuration docs"
```

---

### Task 10: Final Verification

- [ ] **Step 1: Full build**

Run: `go build ./...`
Expected: SUCCESS

- [ ] **Step 2: Full test suite**

Run: `go test ./... -short -count=1`
Expected: All tests pass

- [ ] **Step 3: Search for any remaining PostgreSQL references**

Run: `grep -ri "postgres\|pgx\|sqlx" --include="*.go" --include="*.toml" --include="*.md" -l`
Expected: Only these should appear (all harmless):
- `docs/superpowers/specs/2026-03-10-multi-project-refactor-design.md` (design spec)
- `docs/superpowers/plans/2026-03-10-phase-1a-remove-postgresql.md` (this plan)
- `internal/pipeline/yaml_parser_test.go` (test fixture string, not functional)
- `internal/envloader/envloader_test.go` (test fixture string `DB=postgres`, not functional)

No functional source code references should remain.

- [ ] **Step 4: Verify no broken imports**

Run: `go vet ./...`
Expected: No issues

- [ ] **Step 5: Commit (if any stragglers found)**

Only if step 3 reveals missed references — fix and commit.
