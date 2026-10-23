package db

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5/pgxpool"
)

var Pool *pgxpool.Pool

func InitDB(ctx context.Context, databaseURL string) error {
	var err error
	Pool, err = pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("db: failed to connect to database: %w", err)
	}

	// Ping database to confirm connection
	if err := Pool.Ping(ctx); err != nil {
		return fmt.Errorf("db: failed to ping database: %w", err)
	}

	// Run initial migration
	if err := RunMigration(ctx); err != nil {
		return fmt.Errorf("db: failed to run migrations: %w", err)
	}

	return nil
}

func RunMigration(ctx context.Context) error {
	migrationPath := filepath.Join("db", "migrations", "001_initial.sql")
	migrationBytes, err := os.ReadFile(migrationPath)
	if err != nil {
		// Fallback for execution from the monorepo root
		migrationPath = filepath.Join("indexer", "db", "migrations", "001_initial.sql")
		migrationBytes, err = os.ReadFile(migrationPath)
		if err != nil {
			// Tests run with the package directory as the working directory.
			migrationPath = filepath.Join("migrations", "001_initial.sql")
			migrationBytes, err = os.ReadFile(migrationPath)
			if err != nil {
				return fmt.Errorf("failed to read migration file: %w", err)
			}
		}
	}

	_, err = Pool.Exec(ctx, string(migrationBytes))
	if err != nil {
		return fmt.Errorf("failed to execute migration script: %w", err)
	}

	return nil
}
