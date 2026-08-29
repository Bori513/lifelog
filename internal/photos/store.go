package photos

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Store struct {
	db        *sql.DB
	dataDir   string
	photosDir string
}

func NewStore(db *sql.DB, dataDir string) *Store {
	return &Store{db: db, dataDir: dataDir, photosDir: filepath.Join(dataDir, "photos")}
}

func (s *Store) ListForDay(ctx context.Context, userID int64, entryDate string) ([]Photo, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT p.id, p.day_id, p.relative_path, p.original_filename, p.mime_type, p.file_size, p.created_at
		FROM photos p JOIN days d ON d.id = p.day_id JOIN journals j ON j.id = d.journal_id
		WHERE j.user_id = ? AND d.entry_date = ? ORDER BY p.id`, userID, entryDate)
	if err != nil {
		return nil, fmt.Errorf("list photos: %w", err)
	}
	defer rows.Close()
	var result []Photo
	for rows.Next() {
		var p Photo
		if err := rows.Scan(&p.ID, &p.DayID, &p.RelativePath, &p.OriginalFilename, &p.MIMEType, &p.FileSize, &p.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan photo: %w", err)
		}
		result = append(result, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list photos: %w", err)
	}
	return result, nil
}

func (s *Store) Get(ctx context.Context, userID, photoID int64) (Photo, error) {
	var p Photo
	err := s.db.QueryRowContext(ctx, `SELECT p.id, p.day_id, p.relative_path, p.original_filename, p.mime_type, p.file_size, p.created_at
		FROM photos p JOIN days d ON d.id = p.day_id JOIN journals j ON j.id = d.journal_id
		WHERE p.id = ? AND j.user_id = ?`, photoID, userID).
		Scan(&p.ID, &p.DayID, &p.RelativePath, &p.OriginalFilename, &p.MIMEType, &p.FileSize, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Photo{}, ErrNotFound
	}
	if err != nil {
		return Photo{}, fmt.Errorf("get photo: %w", err)
	}
	return p, nil
}

func (s *Store) Add(ctx context.Context, userID, dayID int64, staged []Staged) ([]Photo, error) {
	if len(staged) == 0 {
		return nil, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin photo metadata: %w", err)
	}
	defer tx.Rollback()
	var owns bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM days d JOIN journals j ON j.id = d.journal_id WHERE d.id = ? AND j.user_id = ?)`, dayID, userID).Scan(&owns); err != nil {
		return nil, fmt.Errorf("check photo day ownership: %w", err)
	}
	if !owns {
		return nil, ErrNotFound
	}
	result := make([]Photo, 0, len(staged))
	for _, item := range staged {
		var p Photo
		err := tx.QueryRowContext(ctx, `INSERT INTO photos (day_id, relative_path, original_filename, mime_type, file_size, created_at)
			VALUES (?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now')) RETURNING id, created_at`, dayID, item.RelativePath, item.OriginalFilename, item.MIMEType, item.FileSize).Scan(&p.ID, &p.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("create photo metadata: %w", err)
		}
		p.DayID, p.RelativePath, p.OriginalFilename, p.MIMEType, p.FileSize = dayID, item.RelativePath, item.OriginalFilename, item.MIMEType, item.FileSize
		result = append(result, p)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit photo metadata: %w", err)
	}
	return result, nil
}

func (s *Store) Remove(ctx context.Context, userID, photoID int64) (Photo, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Photo{}, fmt.Errorf("begin remove photo: %w", err)
	}
	defer tx.Rollback()
	var p Photo
	err = tx.QueryRowContext(ctx, `SELECT p.id, p.day_id, p.relative_path, p.original_filename, p.mime_type, p.file_size, p.created_at
		FROM photos p JOIN days d ON d.id = p.day_id JOIN journals j ON j.id = d.journal_id
		WHERE p.id = ? AND j.user_id = ?`, photoID, userID).Scan(&p.ID, &p.DayID, &p.RelativePath, &p.OriginalFilename, &p.MIMEType, &p.FileSize, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Photo{}, ErrNotFound
	}
	if err != nil {
		return Photo{}, fmt.Errorf("find photo to remove: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM photos WHERE id = ?`, p.ID); err != nil {
		return Photo{}, fmt.Errorf("remove photo metadata: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Photo{}, fmt.Errorf("commit photo removal: %w", err)
	}
	return p, nil
}

func (s *Store) ResolvePath(relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) || filepath.Clean(relative) != relative {
		return "", ErrUnsafePath
	}
	full := filepath.Join(s.photosDir, relative)
	rel, err := filepath.Rel(s.photosDir, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", ErrUnsafePath
	}
	return full, nil
}

func (s *Store) Stage(files []*multipart.FileHeader, userID int64, entryDate string) ([]Staged, error) {
	if len(files) > MaxNewPhotos {
		return nil, ErrTooMany
	}
	date, err := time.Parse("2006-01-02", entryDate)
	if err != nil || date.Format("2006-01-02") != entryDate {
		return nil, ErrInvalidDate
	}
	if len(files) == 0 {
		return nil, nil
	}
	if err := os.MkdirAll(s.dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	staged := make([]Staged, 0, len(files))
	defer func() {
		if err != nil {
			s.CleanupStaged(staged)
		}
	}()
	for _, header := range files {
		item, stageErr := s.stageOne(header, userID, date)
		if stageErr != nil {
			err = stageErr
			return nil, err
		}
		staged = append(staged, item)
	}
	return staged, nil
}

func (s *Store) stageOne(header *multipart.FileHeader, userID int64, date time.Time) (Staged, error) {
	source, err := header.Open()
	if err != nil {
		return Staged{}, fmt.Errorf("open uploaded photo: %w", err)
	}
	defer source.Close()
	tmp, err := os.CreateTemp(s.dataDir, ".photo-upload-*")
	if err != nil {
		return Staged{}, fmt.Errorf("create staged photo: %w", err)
	}
	tmpPath := tmp.Name()
	keep := false
	defer func() {
		tmp.Close()
		if !keep {
			os.Remove(tmpPath)
		}
	}()
	buffer := make([]byte, 512)
	n, readErr := io.ReadFull(source, buffer)
	if errors.Is(readErr, io.EOF) {
		return Staged{}, ErrEmpty
	}
	if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return Staged{}, fmt.Errorf("read uploaded photo: %w", readErr)
	}
	if n == 0 {
		return Staged{}, ErrEmpty
	}
	mimeType := http.DetectContentType(buffer[:n])
	ext := map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp"}[mimeType]
	if ext == "" {
		return Staged{}, ErrUnsupported
	}
	if _, err := tmp.Write(buffer[:n]); err != nil {
		return Staged{}, fmt.Errorf("stage uploaded photo: %w", err)
	}
	written, err := io.Copy(tmp, io.LimitReader(source, MaxPhotoBytes+1-int64(n)))
	if err != nil {
		return Staged{}, fmt.Errorf("stage uploaded photo: %w", err)
	}
	size := int64(n) + written
	if size > MaxPhotoBytes {
		return Staged{}, ErrTooLarge
	}
	if err := tmp.Sync(); err != nil {
		return Staged{}, fmt.Errorf("sync staged photo: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return Staged{}, fmt.Errorf("close staged photo: %w", err)
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return Staged{}, fmt.Errorf("generate photo filename: %w", err)
	}
	relative := filepath.Join(strconv.FormatInt(userID, 10), date.Format("2006"), date.Format("01"), date.Format("02"), hex.EncodeToString(random)+ext)
	keep = true
	return Staged{TemporaryPath: tmpPath, RelativePath: relative, OriginalFilename: header.Filename, MIMEType: mimeType, FileSize: size}, nil
}

func (s *Store) Persist(staged []Staged) error {
	persisted := make([]string, 0, len(staged))
	for _, item := range staged {
		destination, err := s.ResolvePath(item.RelativePath)
		if err != nil {
			s.cleanupPaths(persisted)
			return err
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			s.cleanupPaths(persisted)
			return fmt.Errorf("create photo directory: %w", err)
		}
		if err := os.Rename(item.TemporaryPath, destination); err != nil {
			s.cleanupPaths(persisted)
			return fmt.Errorf("persist photo: %w", err)
		}
		persisted = append(persisted, destination)
	}
	return nil
}

func (s *Store) CleanupStaged(staged []Staged) {
	for _, item := range staged {
		_ = os.Remove(item.TemporaryPath)
	}
}
func (s *Store) CleanupPersisted(staged []Staged) {
	for _, item := range staged {
		if path, err := s.ResolvePath(item.RelativePath); err == nil {
			_ = os.Remove(path)
		}
	}
}
func (s *Store) DeleteFile(photo Photo) error {
	path, err := s.ResolvePath(photo.RelativePath)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
func (s *Store) cleanupPaths(paths []string) {
	for _, path := range paths {
		_ = os.Remove(path)
	}
}
