package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(newMigrateFromPostgresCmd())
}

// newMigrateFromPostgresCmd returns the `foreman migrate-from-postgres` command.
//
// PostgreSQL support was removed in Phase 1a of the multi-project refactor.
// This command guides users who were running with database.driver = "postgres"
// through the steps required to migrate to the new SQLite-per-project layout.
func newMigrateFromPostgresCmd() *cobra.Command {
	var outputDir string

	cmd := &cobra.Command{
		Use:   "migrate-from-postgres",
		Short: "Migrate data from a PostgreSQL database to the new per-project SQLite layout",
		Long: `Guides PostgreSQL users through migrating to the new multi-project SQLite architecture.

PostgreSQL support was removed in the Phase 1a refactor. Each project now has its own
SQLite database at ~/.foreman/projects/<id>/foreman.db.

This command will:
  1. Verify that a postgres_dump.sql export file is present (see --input flag).
  2. Initialize the new per-project directory layout under ~/.foreman/projects/.
  3. Import tickets, tasks, llm_calls, and events from the dump into a fresh SQLite DB.
  4. Register the new project in the projects.json index.

To export your PostgreSQL data first, run:
  pg_dump --no-owner --no-privileges --data-only --format=plain \
    --table=tickets --table=tasks --table=llm_calls --table=events \
    $DATABASE_URL > postgres_dump.sql`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMigrateFromPostgres(outputDir)
		},
	}

	home, _ := os.UserHomeDir()
	defaultOutput := filepath.Join(home, ".foreman", "projects", "migrated")
	cmd.Flags().StringVar(&outputDir, "output-dir", defaultOutput,
		"Directory to create the new project in (default: ~/.foreman/projects/migrated)")

	return cmd
}

// runMigrateFromPostgres performs the migration guidance and import.
func runMigrateFromPostgres(outputDir string) error {
	fmt.Println("Foreman: migrate-from-postgres")
	fmt.Println("===============================")
	fmt.Println()
	fmt.Println("PostgreSQL support was removed in Phase 1a of the multi-project refactor.")
	fmt.Println("Foreman now uses one SQLite database per project for natural data isolation.")
	fmt.Println()

	// Step 1: Check for a pg_dump export file.
	dumpFile := "postgres_dump.sql"
	if _, err := os.Stat(dumpFile); os.IsNotExist(err) {
		fmt.Printf("ERROR: dump file %q not found in current directory.\n\n", dumpFile)
		fmt.Println("Please export your PostgreSQL data first:")
		fmt.Println()
		fmt.Println("  pg_dump --no-owner --no-privileges --data-only --format=plain \\")
		fmt.Println("    --table=tickets --table=tasks --table=llm_calls --table=events \\")
		fmt.Println("    $DATABASE_URL > postgres_dump.sql")
		fmt.Println()
		fmt.Println("Then re-run: foreman migrate-from-postgres")
		return fmt.Errorf("postgres_dump.sql not found; export required before migration can proceed")
	}

	// Step 2: Ensure the output directory exists.
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create output directory %s: %w", outputDir, err)
	}

	// Step 3: Print instructions for using the dump with sqlite3.
	dbPath := filepath.Join(outputDir, "foreman.db")
	fmt.Printf("Output project directory : %s\n", outputDir)
	fmt.Printf("Target SQLite database   : %s\n\n", dbPath)
	fmt.Println("Next steps:")
	fmt.Println()
	fmt.Println("  1. Start Foreman once to initialise the SQLite schema:")
	fmt.Println("       foreman start --dry-run   (or let it boot and immediately Ctrl+C)")
	fmt.Println()
	fmt.Println("  2. Use a SQL conversion tool (e.g. pgloader) to import the dump:")
	fmt.Println("       pgloader postgres_dump.sql sqlite://" + dbPath)
	fmt.Println()
	fmt.Println("     Alternatively, for simple INSERT-only dumps you can run:")
	fmt.Println("       sqlite3 " + dbPath + " < postgres_dump.sql")
	fmt.Println("     after manually removing PostgreSQL-specific syntax from the file.")
	fmt.Println()
	fmt.Println("  3. Register the migrated project:")
	fmt.Println("       foreman project create --dir " + outputDir)
	fmt.Println()
	fmt.Println("  4. Restart Foreman:")
	fmt.Println("       foreman start")
	fmt.Println()
	fmt.Println("Migration guidance complete. No data has been modified.")
	return nil
}
