package profiles

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

const timestampFormat = "2006-01-02T15:04:05.000000000Z"

func (s *Store) CreateSession(ctx context.Context, profileID int64, ttl time.Duration) (Session, error) {
	if ttl <= 0 {
		return Session{}, ErrInvalidTTL
	}
	var exists bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id = ?)`, profileID).Scan(&exists); err != nil {
		return Session{}, fmt.Errorf("check session profile: %w", err)
	}
	if !exists {
		return Session{}, ErrNotFound
	}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return Session{}, fmt.Errorf("generate session token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(random)
	hash := sessionTokenHash(token)
	now := time.Now().UTC()
	expiresAt := now.Add(ttl)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO sessions (user_id, token_hash, expires_at, created_at) VALUES (?, ?, ?, ?)`, profileID, hash, expiresAt.Format(timestampFormat), now.Format(timestampFormat)); err != nil {
		return Session{}, fmt.Errorf("create session: %w", err)
	}
	return Session{Token: token, ExpiresAt: expiresAt}, nil
}

func (s *Store) GetProfileBySession(ctx context.Context, token string) (Profile, error) {
	var profile Profile
	var expires string
	err := s.db.QueryRowContext(ctx, `
		SELECT u.id, u.name, u.timezone, u.pin_hash IS NOT NULL, u.created_at, u.updated_at, s.expires_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ?`, sessionTokenHash(token)).Scan(
		&profile.ID, &profile.Name, &profile.Timezone, &profile.HasPIN, &profile.CreatedAt, &profile.UpdatedAt, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, ErrInvalidSession
	}
	if err != nil {
		return Profile{}, fmt.Errorf("look up session: %w", err)
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		return Profile{}, fmt.Errorf("parse session expiry: %w", err)
	}
	if !time.Now().UTC().Before(expiresAt) {
		return Profile{}, ErrExpiredSession
	}
	return profile, nil
}

func (s *Store) DeleteSession(ctx context.Context, token string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, sessionTokenHash(token)); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (s *Store) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE julianday(expires_at) <= julianday('now')`)
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read deleted session count: %w", err)
	}
	return count, nil
}

func sessionTokenHash(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}
