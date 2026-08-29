package database

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenInitializesDatabase(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "new", "data")
	db, err := Open(dataDir)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })

	var foreignKeys int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("read foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}

	wantTables := []string{
		"schema_migrations", "users", "journals", "questions", "question_options",
		"days", "answers", "answer_options", "photos", "sessions",
	}
	for _, table := range wantTables {
		var name string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("expected table %q: %v", table, err)
		}
	}

	var migrationsApplied int
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = 1").Scan(&migrationsApplied); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if migrationsApplied != 1 {
		t.Fatalf("migration 001 count = %d, want 1", migrationsApplied)
	}

	if err := migrate(db); err != nil {
		t.Fatalf("second migrate() error = %v", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM schema_migrations").Scan(&migrationsApplied); err != nil {
		t.Fatalf("count migrations after second run: %v", err)
	}
	if migrationsApplied != 1 {
		t.Fatalf("migration count after second run = %d, want 1", migrationsApplied)
	}
}

func TestForeignKeyRejectsInvalidJournal(t *testing.T) {
	db := openTestDatabase(t)
	_, err := db.Exec(`INSERT INTO journals (user_id, name, created_at, updated_at)
        VALUES (999, 'Invalid', '2026-08-29T00:00:00Z', '2026-08-29T00:00:00Z')`)
	if err == nil {
		t.Fatal("inserting a journal with an invalid user_id unexpectedly succeeded")
	}
}

func TestUniqueJournalDate(t *testing.T) {
	db := openTestDatabase(t)
	mustExec(t, db, `INSERT INTO users (id, name, created_at, updated_at)
        VALUES (1, 'Test User', '2026-08-29T00:00:00Z', '2026-08-29T00:00:00Z')`)
	mustExec(t, db, `INSERT INTO journals (id, user_id, name, created_at, updated_at)
        VALUES (1, 1, 'Journal', '2026-08-29T00:00:00Z', '2026-08-29T00:00:00Z')`)
	mustExec(t, db, `INSERT INTO days (journal_id, entry_date, created_at, updated_at)
        VALUES (1, '2026-08-29', '2026-08-29T00:00:00Z', '2026-08-29T00:00:00Z')`)

	_, err := db.Exec(`INSERT INTO days (journal_id, entry_date, created_at, updated_at)
        VALUES (1, '2026-08-29', '2026-08-29T00:00:00Z', '2026-08-29T00:00:00Z')`)
	if err == nil {
		t.Fatal("inserting a duplicate journal date unexpectedly succeeded")
	}
}

func openTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func mustExec(t *testing.T, db *sql.DB, query string) {
	t.Helper()
	if _, err := db.Exec(query); err != nil {
		t.Fatalf("Exec() error = %v", err)
	}
}
