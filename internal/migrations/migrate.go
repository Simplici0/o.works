package migrations

import (
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"
)

const sqliteDialect = "sqlite3"

// Up runs all pending SQL migrations found in migrationsDir.
func Up(db *sql.DB, migrationsDir string) error {
	if err := goose.SetDialect(sqliteDialect); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}

	if err := goose.Up(db, migrationsDir); err != nil {
		return fmt.Errorf("run goose up migrations: %w", err)
	}

	return nil
}
