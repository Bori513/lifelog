package database

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/Bori513/lifelog/internal/search"
	_ "modernc.org/sqlite"
)

const databaseFilename = "journal.db"

// Open creates and initializes the LifeLog database in dataDir.
func Open(dataDir string) (*sql.DB, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data directory %q: %w", dataDir, err)
	}

	databasePath := filepath.Join(dataDir, databaseFilename)
	dsn := "file:" + url.PathEscape(databasePath) +
		"?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database %q: %w", databasePath, err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := configureAndVerify(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply database migrations: %w", err)
	}
	if err := search.NewStore(db).Initialize(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize search index: %w", err)
	}

	return db, nil
}

func configureAndVerify(db *sql.DB) error {
	var foreignKeys int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		return fmt.Errorf("read SQLite foreign_keys setting: %w", err)
	}
	if foreignKeys != 1 {
		return fmt.Errorf("configure SQLite: foreign key enforcement is disabled")
	}

	var journalMode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		return fmt.Errorf("read SQLite journal_mode setting: %w", err)
	}
	if journalMode != "wal" {
		return fmt.Errorf("configure SQLite: journal mode is %q, want %q", journalMode, "wal")
	}

	var busyTimeout int
	if err := db.QueryRow("PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		return fmt.Errorf("read SQLite busy_timeout setting: %w", err)
	}
	if busyTimeout != 5000 {
		return fmt.Errorf("configure SQLite: busy timeout is %d ms, want 5000 ms", busyTimeout)
	}
	return nil
}
