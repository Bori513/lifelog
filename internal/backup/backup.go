package backup

import (
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const archiveRoot = "lifelog-backup/"

var (
	ErrInProgress  = errors.New("backup: backup already in progress")
	ErrUnavailable = errors.New("backup: server backup directory is not configured")
)

type Manager struct {
	db        *sql.DB
	photosDir string
	backupDir string
	tempDir   string
	guard     chan struct{}
	now       func() time.Time
}

type Artifact struct {
	Path     string
	Filename string
	Size     int64
	cleanup  func()
}

func (a Artifact) Cleanup() {
	if a.cleanup != nil {
		a.cleanup()
	}
}

func New(db *sql.DB, dataDir, backupDir string) *Manager {
	return &Manager{db: db, photosDir: filepath.Join(dataDir, "photos"), backupDir: strings.TrimSpace(backupDir), guard: make(chan struct{}, 1), now: time.Now}
}

func (m *Manager) ServerConfigured() bool { return m.backupDir != "" }

func (m *Manager) ServerAvailable() bool {
	return m.backupDir != "" && validateDirectory(m.backupDir) == nil
}

func (m *Manager) CreateDownload(ctx context.Context) (Artifact, error) {
	if !m.acquire() {
		return Artifact{}, ErrInProgress
	}
	defer m.release()

	workspace, err := os.MkdirTemp(m.tempDir, "lifelog-backup-*")
	if err != nil {
		return Artifact{}, fmt.Errorf("create backup workspace: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(workspace) }
	path := filepath.Join(workspace, "backup.zip")
	created := m.now().UTC()
	if err := m.create(ctx, workspace, path, created); err != nil {
		cleanup()
		return Artifact{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		cleanup()
		return Artifact{}, fmt.Errorf("stat completed backup: %w", err)
	}
	return Artifact{Path: path, Filename: filename(created, 0), Size: info.Size(), cleanup: cleanup}, nil
}

func (m *Manager) CreateServer(ctx context.Context) (string, error) {
	if m.backupDir == "" {
		return "", ErrUnavailable
	}
	if !m.acquire() {
		return "", ErrInProgress
	}
	defer m.release()
	if err := validateDirectory(m.backupDir); err != nil {
		return "", fmt.Errorf("server backup directory: %w", err)
	}

	workspace, err := os.MkdirTemp(m.tempDir, "lifelog-backup-*")
	if err != nil {
		return "", fmt.Errorf("create backup workspace: %w", err)
	}
	defer os.RemoveAll(workspace)
	tmp, err := os.CreateTemp(m.backupDir, ".lifelog-backup-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create temporary server backup: %w", err)
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("close temporary server backup: %w", err)
	}
	_ = os.Remove(tmpPath) // archive creation requires a destination that does not exist
	defer os.Remove(tmpPath)

	created := m.now().UTC()
	if err := m.create(ctx, workspace, tmpPath, created); err != nil {
		return "", err
	}
	file, err := os.OpenFile(tmpPath, os.O_RDWR, 0)
	if err != nil {
		return "", fmt.Errorf("open completed server backup: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return "", fmt.Errorf("sync completed server backup: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close completed server backup: %w", err)
	}
	finalPath, finalName, err := availableName(m.backupDir, created)
	if err != nil {
		return "", err
	}
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return "", fmt.Errorf("publish server backup: %w", err)
	}
	return finalName, nil
}

func (m *Manager) create(ctx context.Context, workspace, archivePath string, created time.Time) (err error) {
	snapshotPath := filepath.Join(workspace, "journal.db")
	if _, err := os.Lstat(snapshotPath); !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("snapshot destination already exists")
	}
	if _, err := m.db.ExecContext(ctx, "VACUUM INTO ?", snapshotPath); err != nil {
		return fmt.Errorf("create SQLite snapshot: %w", err)
	}
	referenced, err := referencedPhotos(ctx, snapshotPath)
	if err != nil {
		return err
	}
	return m.writeArchive(snapshotPath, archivePath, created, referenced)
}

func referencedPhotos(ctx context.Context, snapshotPath string) (map[string]struct{}, error) {
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(snapshotPath)+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("open SQLite snapshot: %w", err)
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, "SELECT relative_path FROM photos")
	if err != nil {
		return nil, fmt.Errorf("query snapshot photos: %w", err)
	}
	defer rows.Close()
	result := make(map[string]struct{})
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, fmt.Errorf("scan snapshot photo path: %w", err)
		}
		clean, err := safeRelative(path)
		if err != nil {
			return nil, fmt.Errorf("invalid referenced photo %q: %w", path, err)
		}
		result[clean] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read snapshot photo paths: %w", err)
	}
	return result, nil
}

func (m *Manager) writeArchive(snapshotPath, archivePath string, created time.Time, referenced map[string]struct{}) (err error) {
	file, err := os.OpenFile(archivePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create backup archive: %w", err)
	}
	keep := false
	defer func() {
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close backup archive: %w", closeErr)
		}
		if !keep || err != nil {
			_ = os.Remove(archivePath)
		}
	}()
	zw := zip.NewWriter(file)
	if err = addFile(zw, snapshotPath, archiveRoot+"journal.db"); err != nil {
		_ = zw.Close()
		return err
	}
	if _, err = zw.Create(archiveRoot + "photos/"); err != nil {
		_ = zw.Close()
		return fmt.Errorf("create photos directory entry: %w", err)
	}
	included := make(map[string]struct{})
	if err = m.addPhotos(zw, included); err != nil {
		_ = zw.Close()
		return err
	}
	for path := range referenced {
		if _, ok := included[path]; !ok {
			_ = zw.Close()
			return fmt.Errorf("referenced photo %q was not included", path)
		}
	}
	manifest := struct {
		FormatVersion int    `json:"format_version"`
		CreatedAt     string `json:"created_at"`
		Scope         string `json:"scope"`
	}{1, created.UTC().Format(time.RFC3339), "full-instance"}
	entry, err := zw.Create(archiveRoot + "backup-info.json")
	if err != nil {
		_ = zw.Close()
		return fmt.Errorf("create backup manifest: %w", err)
	}
	if err := json.NewEncoder(entry).Encode(manifest); err != nil {
		_ = zw.Close()
		return fmt.Errorf("write backup manifest: %w", err)
	}
	if err = zw.Close(); err != nil {
		return fmt.Errorf("close ZIP archive: %w", err)
	}
	keep = true
	return nil
}

func (m *Manager) addPhotos(zw *zip.Writer, included map[string]struct{}) error {
	info, err := os.Lstat(m.photosDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect photos directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("photos path is not a directory")
	}
	return filepath.WalkDir(m.photosDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk photos: %w", walkErr)
		}
		if path == m.photosDir {
			return nil
		}
		rel, err := filepath.Rel(m.photosDir, path)
		if err != nil {
			return fmt.Errorf("resolve photo path: %w", err)
		}
		clean, err := safeRelative(rel)
		if err != nil {
			return fmt.Errorf("invalid filesystem photo path %q: %w", rel, err)
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect photo %q: %w", clean, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("photo path %q is a symlink", clean)
		}
		if entry.IsDir() {
			_, err := zw.Create(archiveRoot + "photos/" + clean + "/")
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("photo path %q is not a regular file", clean)
		}
		if err := addFile(zw, path, archiveRoot+"photos/"+clean); err != nil {
			return err
		}
		included[clean] = struct{}{}
		return nil
	})
}

func addFile(zw *zip.Writer, sourcePath, archiveName string) error {
	before, err := os.Lstat(sourcePath)
	if err != nil {
		return fmt.Errorf("inspect %q: %w", archiveName, err)
	}
	if !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%q is not a regular file", archiveName)
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open %q: %w", archiveName, err)
	}
	info, err := source.Stat()
	if err != nil {
		source.Close()
		return fmt.Errorf("stat %q: %w", archiveName, err)
	}
	if !info.Mode().IsRegular() || !os.SameFile(before, info) {
		source.Close()
		return fmt.Errorf("%q changed before it could be archived", archiveName)
	}
	header := &zip.FileHeader{Name: archiveName, Method: zip.Deflate}
	header.SetMode(0o600)
	destination, err := zw.CreateHeader(header)
	if err == nil {
		_, err = io.Copy(destination, source)
	}
	closeErr := source.Close()
	if err != nil {
		return fmt.Errorf("archive %q: %w", archiveName, err)
	}
	if closeErr != nil {
		return fmt.Errorf("close %q: %w", archiveName, closeErr)
	}
	after, err := os.Lstat(sourcePath)
	if err != nil {
		return fmt.Errorf("verify archived file %q: %w", archiveName, err)
	}
	if !after.Mode().IsRegular() || after.Mode()&os.ModeSymlink != 0 || !os.SameFile(before, after) {
		return fmt.Errorf("%q changed while it was archived", archiveName)
	}
	return nil
}

func safeRelative(path string) (string, error) {
	if path == "" || filepath.IsAbs(path) || filepath.Clean(path) != path || path == "." || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return "", errors.New("unsafe relative path")
	}
	clean := filepath.ToSlash(path)
	if strings.HasPrefix(clean, "/") || strings.Contains(clean, "../") || strings.Contains(clean, `\`) {
		return "", errors.New("unsafe relative path")
	}
	return clean, nil
}

func validateDirectory(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("configured path is not a directory")
	}
	return nil
}

func filename(created time.Time, suffix int) string {
	base := "lifelog-backup-" + created.UTC().Format("2006-01-02-150405")
	if suffix > 0 {
		base += fmt.Sprintf("-%d", suffix)
	}
	return base + ".zip"
}

func availableName(dir string, created time.Time) (string, string, error) {
	for suffix := 0; suffix < 10000; suffix++ {
		name := filename(created, suffix)
		path := filepath.Join(dir, name)
		_, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			return path, name, nil
		}
		if err != nil {
			return "", "", fmt.Errorf("check server backup filename: %w", err)
		}
	}
	return "", "", errors.New("could not find an available server backup filename")
}

func (m *Manager) acquire() bool {
	select {
	case m.guard <- struct{}{}:
		return true
	default:
		return false
	}
}

func (m *Manager) release() { <-m.guard }
