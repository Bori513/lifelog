package database

import (
	"database/sql"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Bori513/lifelog/migrations"
)

func migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
        version INTEGER PRIMARY KEY,
        applied_at TEXT NOT NULL
    )`); err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	entries, err := fs.ReadDir(migrations.Files, ".")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}
	type migrationFile struct {
		name    string
		version int
	}
	files := make([]migrationFile, 0, len(entries))
	versions := make(map[int]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		version, err := migrationVersion(entry.Name())
		if err != nil {
			return err
		}
		if previous, exists := versions[version]; exists {
			return fmt.Errorf("duplicate migration version %03d in %q and %q", version, previous, entry.Name())
		}
		versions[version] = entry.Name()
		files = append(files, migrationFile{name: entry.Name(), version: version})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].version < files[j].version })

	for _, file := range files {
		var applied bool
		if err := db.QueryRow(
			"SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = ?)", file.version,
		).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %03d: %w", file.version, err)
		}
		if applied {
			continue
		}
		contents, err := migrations.Files.ReadFile(file.name)
		if err != nil {
			return fmt.Errorf("read migration %q: %w", file.name, err)
		}
		if err := applyMigration(db, file.version, file.name, string(contents)); err != nil {
			return err
		}
	}
	return nil
}

func migrationVersion(name string) (int, error) {
	prefix, _, ok := strings.Cut(name, "_")
	if !ok || prefix == "" {
		return 0, fmt.Errorf("invalid migration filename %q: want numeric prefix followed by underscore", name)
	}
	version, err := strconv.Atoi(prefix)
	if err != nil || version <= 0 {
		return 0, fmt.Errorf("invalid migration filename %q: version must be a positive integer", name)
	}
	return version, nil
}

func applyMigration(db *sql.DB, version int, name, contents string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin migration %q: %w", name, err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(contents); err != nil {
		return fmt.Errorf("execute migration %q: %w", name, err)
	}
	if _, err := tx.Exec(
		"INSERT INTO schema_migrations (version, applied_at) VALUES (?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))",
		version,
	); err != nil {
		return fmt.Errorf("record migration %q: %w", name, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %q: %w", name, err)
	}
	return nil
}
