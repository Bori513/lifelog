package profiles

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Bori513/lifelog/internal/database"
)

func TestCreateProfileValidationAndDefaultJournal(t *testing.T) {
	store, db := newTestStore(t)
	ctx := context.Background()

	profile, err := store.CreateProfile(ctx, CreateProfileInput{Name: "  Boris  ", Timezone: "Europe/Bratislava"})
	if err != nil {
		t.Fatalf("CreateProfile() error = %v", err)
	}
	if profile.Name != "Boris" || profile.Timezone != "Europe/Bratislava" || profile.HasPIN {
		t.Fatalf("profile = %+v", profile)
	}
	journals, err := store.ListJournals(ctx, profile.ID)
	if err != nil || len(journals) != 1 || journals[0].Name != "Personal" || journals[0].UserID != profile.ID {
		t.Fatalf("journals = %+v, error = %v", journals, err)
	}
	utc, err := store.CreateProfile(ctx, CreateProfileInput{Name: "UTC User"})
	if err != nil || utc.Timezone != "UTC" {
		t.Fatalf("default timezone profile = %+v, error = %v", utc, err)
	}

	for _, test := range []struct {
		name  string
		input CreateProfileInput
		want  error
	}{
		{name: "blank name", input: CreateProfileInput{Name: " \t\n"}, want: ErrInvalidName},
		{name: "invalid timezone", input: CreateProfileInput{Name: "User", Timezone: "Mars/Olympus"}, want: ErrInvalidTimezone},
		{name: "short PIN", input: CreateProfileInput{Name: "User", PIN: "123"}, want: ErrPINTooShort},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := store.CreateProfile(ctx, test.input); !errors.Is(err, test.want) {
				t.Fatalf("CreateProfile() error = %v, want %v", err, test.want)
			}
		})
	}

	if _, err := db.Exec(`CREATE TRIGGER fail_default_journal BEFORE INSERT ON journals BEGIN SELECT RAISE(ABORT, 'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateProfile(ctx, CreateProfileInput{Name: "Rolled Back"}); err == nil {
		t.Fatal("CreateProfile() unexpectedly succeeded when journal insert failed")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE name = 'Rolled Back'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partially created users = %d, error = %v", count, err)
	}
}

func TestListGetUpdateAndAutoSelection(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	profiles, err := store.ListProfiles(ctx)
	if err != nil || profiles != nil {
		t.Fatalf("empty ListProfiles() = %+v, %v", profiles, err)
	}
	if _, eligible, err := store.AutoSelectableProfile(ctx); err != nil || eligible {
		t.Fatalf("empty AutoSelectableProfile() eligible = %v, error = %v", eligible, err)
	}

	first := mustCreateProfile(t, store, CreateProfileInput{Name: "First"})
	selected, eligible, err := store.AutoSelectableProfile(ctx)
	if err != nil || !eligible || selected.ID != first.ID {
		t.Fatalf("AutoSelectableProfile() = %+v, %v, %v", selected, eligible, err)
	}
	if err := store.RenameProfile(ctx, first.ID, "  Renamed  "); err != nil {
		t.Fatal(err)
	}
	if err := store.RenameProfile(ctx, first.ID, " "); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("blank rename error = %v", err)
	}
	if err := store.ChangeTimezone(ctx, first.ID, "Europe/Zagreb"); err != nil {
		t.Fatal(err)
	}
	if err := store.ChangeTimezone(ctx, first.ID, "Invalid/Zone"); !errors.Is(err, ErrInvalidTimezone) {
		t.Fatalf("invalid timezone error = %v", err)
	}
	updated, err := store.GetProfile(ctx, first.ID)
	if err != nil || updated.Name != "Renamed" || updated.Timezone != "Europe/Zagreb" {
		t.Fatalf("updated profile = %+v, error = %v", updated, err)
	}
	if _, err := store.GetProfile(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing GetProfile() error = %v", err)
	}
	if err := store.RenameProfile(ctx, 999, "Missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing RenameProfile() error = %v", err)
	}

	second := mustCreateProfile(t, store, CreateProfileInput{Name: "Second", PIN: "0000"})
	profiles, err = store.ListProfiles(ctx)
	if err != nil || !reflect.DeepEqual([]int64{profiles[0].ID, profiles[1].ID}, []int64{first.ID, second.ID}) || profiles[0].HasPIN || !profiles[1].HasPIN {
		t.Fatalf("ListProfiles() = %+v, error = %v", profiles, err)
	}
	if _, eligible, err := store.AutoSelectableProfile(ctx); err != nil || eligible {
		t.Fatalf("multiple AutoSelectableProfile() eligible = %v, error = %v", eligible, err)
	}
}

func TestPINLifecycle(t *testing.T) {
	store, db := newTestStore(t)
	ctx := context.Background()
	plain := mustCreateProfile(t, store, CreateProfileInput{Name: "Plain"})
	if err := store.VerifyPIN(ctx, plain.ID, "anything"); !errors.Is(err, ErrPINNotConfigured) {
		t.Fatalf("plain VerifyPIN() error = %v", err)
	}
	protected := mustCreateProfile(t, store, CreateProfileInput{Name: "Protected", PIN: "0000"})
	var stored string
	if err := db.QueryRow(`SELECT pin_hash FROM users WHERE id = ?`, protected.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == "0000" || stored == "" {
		t.Fatalf("stored PIN hash = %q", stored)
	}
	if err := store.VerifyPIN(ctx, protected.ID, "0000"); err != nil {
		t.Fatalf("correct VerifyPIN() error = %v", err)
	}
	if err := store.VerifyPIN(ctx, protected.ID, "wrong"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("wrong VerifyPIN() error = %v", err)
	}
	if err := store.SetPIN(ctx, protected.ID, "new password"); err != nil {
		t.Fatal(err)
	}
	if err := store.VerifyPIN(ctx, protected.ID, "0000"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old PIN error = %v", err)
	}
	if err := store.VerifyPIN(ctx, protected.ID, "new password"); err != nil {
		t.Fatalf("new PIN error = %v", err)
	}
	if err := store.SetPIN(ctx, protected.ID, ""); !errors.Is(err, ErrPINTooShort) {
		t.Fatalf("empty SetPIN() error = %v", err)
	}
	if err := store.RemovePIN(ctx, protected.ID); err != nil {
		t.Fatal(err)
	}
	var hash sql.NullString
	if err := db.QueryRow(`SELECT pin_hash FROM users WHERE id = ?`, protected.ID).Scan(&hash); err != nil || hash.Valid {
		t.Fatalf("removed hash = %+v, error = %v", hash, err)
	}
}

func TestPINProtectedProfileIsNotAutoSelectable(t *testing.T) {
	store, _ := newTestStore(t)
	mustCreateProfile(t, store, CreateProfileInput{Name: "Protected", PIN: "1234"})
	if _, eligible, err := store.AutoSelectableProfile(context.Background()); err != nil || eligible {
		t.Fatalf("AutoSelectableProfile() eligible = %v, error = %v", eligible, err)
	}
}

func TestSessionsLifecycleExpiryAndCleanup(t *testing.T) {
	store, db := newTestStore(t)
	ctx := context.Background()
	first := mustCreateProfile(t, store, CreateProfileInput{Name: "First", PIN: "1234"})
	second := mustCreateProfile(t, store, CreateProfileInput{Name: "Second"})
	for _, ttl := range []time.Duration{0, -time.Second} {
		if _, err := store.CreateSession(ctx, first.ID, ttl); !errors.Is(err, ErrInvalidTTL) {
			t.Fatalf("CreateSession(%v) error = %v", ttl, err)
		}
	}
	if _, err := store.CreateSession(ctx, 999, time.Hour); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing user CreateSession() error = %v", err)
	}

	session, err := store.CreateSession(ctx, first.ID, time.Hour)
	if err != nil || session.Token == "" || !session.ExpiresAt.After(time.Now()) {
		t.Fatalf("CreateSession() = %+v, %v", session, err)
	}
	var storedHash, storedExpiry string
	var storedUser int64
	if err := db.QueryRow(`SELECT user_id, token_hash, expires_at FROM sessions`).Scan(&storedUser, &storedHash, &storedExpiry); err != nil {
		t.Fatal(err)
	}
	wantHashBytes := sha256.Sum256([]byte(session.Token))
	if storedUser != first.ID || storedHash == session.Token || storedHash != hex.EncodeToString(wantHashBytes[:]) {
		t.Fatalf("stored session user/hash = %d/%q", storedUser, storedHash)
	}
	profile, err := store.GetProfileBySession(ctx, session.Token)
	if err != nil || profile.ID != first.ID || !profile.HasPIN {
		t.Fatalf("GetProfileBySession() = %+v, %v", profile, err)
	}
	var expiryAfter string
	if err := db.QueryRow(`SELECT expires_at FROM sessions WHERE token_hash = ?`, storedHash).Scan(&expiryAfter); err != nil || expiryAfter != storedExpiry {
		t.Fatalf("lookup changed expiry from %q to %q, error = %v", storedExpiry, expiryAfter, err)
	}
	if _, err := store.GetProfileBySession(ctx, "unknown"); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("unknown session error = %v", err)
	}
	if _, err := db.Exec(`UPDATE sessions SET expires_at = '2000-01-01T00:00:00.000000000Z' WHERE token_hash = ?`, storedHash); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetProfileBySession(ctx, session.Token); !errors.Is(err, ErrExpiredSession) {
		t.Fatalf("expired session error = %v", err)
	}
	valid := mustCreateSession(t, store, second.ID)
	deleted, err := store.DeleteExpiredSessions(ctx)
	if err != nil || deleted != 1 {
		t.Fatalf("DeleteExpiredSessions() = %d, %v", deleted, err)
	}
	if profile, err := store.GetProfileBySession(ctx, valid.Token); err != nil || profile.ID != second.ID {
		t.Fatalf("valid session after cleanup = %+v, %v", profile, err)
	}
	if err := store.DeleteSession(ctx, valid.Token); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteSession(ctx, valid.Token); err != nil {
		t.Fatalf("idempotent DeleteSession() error = %v", err)
	}
	if _, err := store.GetProfileBySession(ctx, valid.Token); !errors.Is(err, ErrInvalidSession) {
		t.Fatalf("deleted session error = %v", err)
	}
}

func TestJournalOwnershipIsolation(t *testing.T) {
	store, _ := newTestStore(t)
	first := mustCreateProfile(t, store, CreateProfileInput{Name: "First"})
	second := mustCreateProfile(t, store, CreateProfileInput{Name: "Second"})
	firstJournals, err := store.ListJournals(context.Background(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	secondJournals, err := store.ListJournals(context.Background(), second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstJournals) != 1 || len(secondJournals) != 1 || firstJournals[0].ID == secondJournals[0].ID || firstJournals[0].UserID != first.ID || secondJournals[0].UserID != second.ID {
		t.Fatalf("journals leaked ownership: first=%+v second=%+v", firstJournals, secondJournals)
	}
	if _, err := store.ListJournals(context.Background(), 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing ListJournals() error = %v", err)
	}
}

func newTestStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewStore(db), db
}

func mustCreateProfile(t *testing.T, store *Store, input CreateProfileInput) Profile {
	t.Helper()
	profile, err := store.CreateProfile(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	return profile
}

func mustCreateSession(t *testing.T, store *Store, profileID int64) Session {
	t.Helper()
	session, err := store.CreateSession(context.Background(), profileID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return session
}
