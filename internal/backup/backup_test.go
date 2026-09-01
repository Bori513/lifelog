package backup

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Bori513/lifelog/internal/database"
)

func testManager(t *testing.T, backupDir string) (*Manager, *sql.DB, string) {
	t.Helper()
	dataDir := t.TempDir()
	db, err := database.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	manager := New(db, dataDir, backupDir)
	manager.now = func() time.Time { return time.Date(2026, 9, 1, 12, 34, 56, 0, time.FixedZone("test", 3600)) }
	return manager, db, dataDir
}

func addJournalData(t *testing.T, db *sql.DB, relativePath string) {
	t.Helper()
	ctx := t.Context()
	result, err := db.ExecContext(ctx, `INSERT INTO users (name, timezone, created_at, updated_at) VALUES ('Backup test', 'UTC', '2026-09-01T00:00:00Z', '2026-09-01T00:00:00Z')`)
	if err != nil {
		t.Fatal(err)
	}
	userID, _ := result.LastInsertId()
	result, err = db.ExecContext(ctx, `INSERT INTO journals (user_id, name, created_at, updated_at) VALUES (?, 'Personal', '2026-09-01T00:00:00Z', '2026-09-01T00:00:00Z')`, userID)
	if err != nil {
		t.Fatal(err)
	}
	journalID, _ := result.LastInsertId()
	result, err = db.ExecContext(ctx, `INSERT INTO days (journal_id, entry_date, general_note, special_moment, location, created_at, updated_at) VALUES (?, '2026-09-01', 'committed WAL content', '', '', '2026-09-01T00:00:00Z', '2026-09-01T00:00:00Z')`, journalID)
	if err != nil {
		t.Fatal(err)
	}
	dayID, _ := result.LastInsertId()
	if relativePath != "" {
		_, err = db.ExecContext(ctx, `INSERT INTO photos (day_id, relative_path, original_filename, mime_type, file_size, created_at) VALUES (?, ?, 'photo.jpg', 'image/jpeg', 5, '2026-09-01T00:00:00Z')`, dayID, relativePath)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestDownloadArchiveSnapshotLayoutManifestAndPhoto(t *testing.T) {
	m, db, dataDir := testManager(t, "")
	photoPath := filepath.Join("1", "2026", "09", "01", "photo.jpg")
	addJournalData(t, db, photoPath)
	fullPhoto := filepath.Join(dataDir, "photos", photoPath)
	if err := os.MkdirAll(filepath.Dir(fullPhoto), 0o755); err != nil {
		t.Fatal(err)
	}
	photoBytes := []byte("photo")
	if err := os.WriteFile(fullPhoto, photoBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	artifact, err := m.CreateDownload(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer artifact.Cleanup()
	if artifact.Filename != "lifelog-backup-2026-09-01-113456.zip" {
		t.Fatalf("filename = %q", artifact.Filename)
	}
	archive, err := zip.OpenReader(artifact.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	entries := map[string]*zip.File{}
	for _, file := range archive.File {
		entries[file.Name] = file
	}
	for _, name := range []string{"lifelog-backup/journal.db", "lifelog-backup/photos/", "lifelog-backup/backup-info.json", "lifelog-backup/photos/1/2026/09/01/photo.jpg"} {
		if entries[name] == nil {
			t.Errorf("missing ZIP entry %q", name)
		}
	}
	gotPhoto, err := readZip(entries["lifelog-backup/photos/1/2026/09/01/photo.jpg"])
	if err != nil || string(gotPhoto) != string(photoBytes) {
		t.Fatalf("photo bytes = %q, err=%v", gotPhoto, err)
	}
	manifestBytes, err := readZip(entries["lifelog-backup/backup-info.json"])
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		FormatVersion int    `json:"format_version"`
		CreatedAt     string `json:"created_at"`
		Scope         string `json:"scope"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	created, err := time.Parse(time.RFC3339, manifest.CreatedAt)
	if err != nil || created.Location() != time.UTC || manifest.FormatVersion != 1 || manifest.Scope != "full-instance" {
		t.Fatalf("manifest = %+v, parsed=%v, err=%v", manifest, created, err)
	}

	snapshotFile := entries["lifelog-backup/journal.db"]
	snapshot, err := snapshotFile.Open()
	if err != nil {
		t.Fatal(err)
	}
	snapshotPath := filepath.Join(t.TempDir(), "journal.db")
	out, err := os.Create(snapshotPath)
	if err != nil {
		t.Fatal(err)
	}
	_, copyErr := io.Copy(out, snapshot)
	closeErr := out.Close()
	snapshot.Close()
	if copyErr != nil || closeErr != nil {
		t.Fatalf("extract snapshot: copy=%v close=%v", copyErr, closeErr)
	}
	snapshotDB, err := sql.Open("sqlite", "file:"+filepath.ToSlash(snapshotPath)+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer snapshotDB.Close()
	var note string
	if err := snapshotDB.QueryRowContext(t.Context(), "SELECT general_note FROM days").Scan(&note); err != nil || note != "committed WAL content" {
		t.Fatalf("snapshot content = %q, err=%v", note, err)
	}
}

func TestEmptyPhotosDirectoryEntry(t *testing.T) {
	m, db, _ := testManager(t, "")
	addJournalData(t, db, "")
	artifact, err := m.CreateDownload(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer artifact.Cleanup()
	archive, err := zip.OpenReader(artifact.Path)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	for _, file := range archive.File {
		if file.Name == "lifelog-backup/photos/" {
			return
		}
	}
	t.Fatal("explicit photos directory entry missing")
}

func TestReferencedMissingOrUnsafePhotoFailsAndCleansDownload(t *testing.T) {
	for _, path := range []string{filepath.Join("1", "missing.jpg"), filepath.Join("..", "escape.jpg")} {
		t.Run(path, func(t *testing.T) {
			m, db, _ := testManager(t, "")
			workspaceParent := t.TempDir()
			m.tempDir = workspaceParent
			addJournalData(t, db, path)
			artifact, err := m.CreateDownload(t.Context())
			if err == nil {
				artifact.Cleanup()
				t.Fatal("backup unexpectedly succeeded")
			}
			if artifact.Path != "" {
				t.Fatalf("failed backup returned artifact %q", artifact.Path)
			}
			entries, readErr := os.ReadDir(workspaceParent)
			if readErr != nil || len(entries) != 0 {
				t.Fatalf("temporary workspace remained: %v, err=%v", entries, readErr)
			}
		})
	}
}

func TestServerPublicationConfigurationCollisionAndFailure(t *testing.T) {
	backupDir := t.TempDir()
	m, db, _ := testManager(t, backupDir)
	addJournalData(t, db, "")
	existing := filepath.Join(backupDir, filename(m.now().UTC(), 0))
	if err := os.WriteFile(existing, []byte("existing"), 0o600); err != nil {
		t.Fatal(err)
	}
	name, err := m.CreateServer(t.Context())
	if err != nil || name != "lifelog-backup-2026-09-01-113456-1.zip" {
		t.Fatalf("name=%q err=%v", name, err)
	}
	if got, _ := os.ReadFile(existing); string(got) != "existing" {
		t.Fatal("existing backup was overwritten")
	}
	entries, _ := os.ReadDir(backupDir)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".lifelog-backup-") {
			t.Fatalf("temporary server file remained: %s", entry.Name())
		}
	}

	unconfigured, _, _ := testManager(t, "")
	if _, err := unconfigured.CreateServer(t.Context()); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unconfigured error = %v", err)
	}
	missing := filepath.Join(t.TempDir(), "missing")
	invalid, _, _ := testManager(t, missing)
	if invalid.ServerAvailable() {
		t.Fatal("missing configured directory reported available")
	}
	if _, err := invalid.CreateServer(t.Context()); err == nil {
		t.Fatal("missing configured directory succeeded")
	}
	filePath := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(filePath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	notDir, _, _ := testManager(t, filePath)
	if _, err := notDir.CreateServer(t.Context()); err == nil {
		t.Fatal("non-directory configured path succeeded")
	}
}

func TestServerFailureLeavesNoPublishedBackup(t *testing.T) {
	backupDir := t.TempDir()
	m, db, _ := testManager(t, backupDir)
	addJournalData(t, db, filepath.Join("1", "missing.jpg"))
	if _, err := m.CreateServer(context.Background()); err == nil {
		t.Fatal("backup unexpectedly succeeded")
	}
	entries, err := os.ReadDir(backupDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed server backup left files: %v, err=%v", entries, err)
	}
}

func TestConcurrencyGuardIsNonBlocking(t *testing.T) {
	m, _, _ := testManager(t, "")
	if !m.acquire() {
		t.Fatal("first acquisition failed")
	}
	defer m.release()
	if _, err := m.CreateDownload(t.Context()); !errors.Is(err, ErrInProgress) {
		t.Fatalf("second backup error = %v", err)
	}
}

func readZip(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}
