package profiles

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) CreateProfile(ctx context.Context, input CreateProfileInput) (Profile, error) {
	name, err := validName(input.Name)
	if err != nil {
		return Profile{}, err
	}
	timezone, err := validTimezone(input.Timezone)
	if err != nil {
		return Profile{}, err
	}
	var pinHash any
	if input.PIN != "" {
		if err := validPIN(input.PIN); err != nil {
			return Profile{}, err
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(input.PIN), bcrypt.DefaultCost)
		if err != nil {
			return Profile{}, fmt.Errorf("hash PIN: %w", err)
		}
		pinHash = string(hash)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Profile{}, fmt.Errorf("begin profile creation: %w", err)
	}
	defer tx.Rollback()
	row := tx.QueryRowContext(ctx, `
		INSERT INTO users (name, pin_hash, timezone, created_at, updated_at)
		VALUES (?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
		RETURNING id, name, timezone, pin_hash IS NOT NULL, created_at, updated_at`, name, pinHash, timezone)
	profile, err := scanProfile(row)
	if err != nil {
		return Profile{}, fmt.Errorf("create profile: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO journals (user_id, name, created_at, updated_at)
		VALUES (?, 'Personal', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`, profile.ID); err != nil {
		return Profile{}, fmt.Errorf("create default journal: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Profile{}, fmt.Errorf("commit profile creation: %w", err)
	}
	return profile, nil
}

func (s *Store) ListProfiles(ctx context.Context) ([]Profile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, timezone, pin_hash IS NOT NULL, created_at, updated_at FROM users ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list profiles: %w", err)
	}
	defer rows.Close()
	var profiles []Profile
	for rows.Next() {
		profile, err := scanProfile(rows)
		if err != nil {
			return nil, fmt.Errorf("scan profile: %w", err)
		}
		profiles = append(profiles, profile)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list profiles: %w", err)
	}
	return profiles, nil
}

func (s *Store) GetProfile(ctx context.Context, profileID int64) (Profile, error) {
	profile, err := scanProfile(s.db.QueryRowContext(ctx, `SELECT id, name, timezone, pin_hash IS NOT NULL, created_at, updated_at FROM users WHERE id = ?`, profileID))
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, ErrNotFound
	}
	if err != nil {
		return Profile{}, fmt.Errorf("get profile: %w", err)
	}
	return profile, nil
}

func (s *Store) RenameProfile(ctx context.Context, profileID int64, name string) error {
	name, err := validName(name)
	if err != nil {
		return err
	}
	return updateProfile(ctx, s.db, `UPDATE users SET name = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, "rename profile", name, profileID)
}

func (s *Store) ChangeTimezone(ctx context.Context, profileID int64, timezone string) error {
	timezone, err := validTimezone(timezone)
	if err != nil {
		return err
	}
	return updateProfile(ctx, s.db, `UPDATE users SET timezone = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, "change profile timezone", timezone, profileID)
}

func (s *Store) SetPIN(ctx context.Context, profileID int64, pin string) error {
	if err := validPIN(pin); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash PIN: %w", err)
	}
	return updateProfile(ctx, s.db, `UPDATE users SET pin_hash = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, "set profile PIN", string(hash), profileID)
}

func (s *Store) RemovePIN(ctx context.Context, profileID int64) error {
	return updateProfile(ctx, s.db, `UPDATE users SET pin_hash = NULL, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, "remove profile PIN", profileID)
}

func (s *Store) VerifyPIN(ctx context.Context, profileID int64, supplied string) error {
	var hash sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT pin_hash FROM users WHERE id = ?`, profileID).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("read profile PIN: %w", err)
	}
	if !hash.Valid {
		return ErrPINNotConfigured
	}
	if bcrypt.CompareHashAndPassword([]byte(hash.String), []byte(supplied)) != nil {
		return ErrInvalidCredentials
	}
	return nil
}

func (s *Store) AutoSelectableProfile(ctx context.Context) (Profile, bool, error) {
	profiles, err := s.ListProfiles(ctx)
	if err != nil {
		return Profile{}, false, err
	}
	if len(profiles) != 1 || profiles[0].HasPIN {
		return Profile{}, false, nil
	}
	return profiles[0], true, nil
}

func (s *Store) ListJournals(ctx context.Context, profileID int64) ([]Journal, error) {
	var exists bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id = ?)`, profileID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check profile for journals: %w", err)
	}
	if !exists {
		return nil, ErrNotFound
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, user_id, name, created_at, updated_at FROM journals WHERE user_id = ? ORDER BY id ASC`, profileID)
	if err != nil {
		return nil, fmt.Errorf("list profile journals: %w", err)
	}
	defer rows.Close()
	var journals []Journal
	for rows.Next() {
		var journal Journal
		if err := rows.Scan(&journal.ID, &journal.UserID, &journal.Name, &journal.CreatedAt, &journal.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan profile journal: %w", err)
		}
		journals = append(journals, journal)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list profile journals: %w", err)
	}
	return journals, nil
}

type scanner interface{ Scan(...any) error }

func scanProfile(row scanner) (Profile, error) {
	var profile Profile
	err := row.Scan(&profile.ID, &profile.Name, &profile.Timezone, &profile.HasPIN, &profile.CreatedAt, &profile.UpdatedAt)
	return profile, err
}

func updateProfile(ctx context.Context, db *sql.DB, query, operation string, args ...any) error {
	result, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s: read affected rows: %w", operation, err)
	}
	if changed == 0 {
		return ErrNotFound
	}
	return nil
}
