package profiles

import (
	"errors"
	"strings"
	"time"
)

var (
	ErrNotFound           = errors.New("profiles: profile not found")
	ErrInvalidName        = errors.New("profiles: name must not be blank")
	ErrInvalidTimezone    = errors.New("profiles: invalid timezone")
	ErrPINTooShort        = errors.New("profiles: PIN must be at least 4 characters")
	ErrPINNotConfigured   = errors.New("profiles: PIN is not configured")
	ErrInvalidCredentials = errors.New("profiles: invalid credentials")
	ErrInvalidTTL         = errors.New("profiles: session TTL must be positive")
	ErrInvalidSession     = errors.New("profiles: invalid session")
	ErrExpiredSession     = errors.New("profiles: session expired")
)

type Profile struct {
	ID        int64
	Name      string
	Timezone  string
	HasPIN    bool
	CreatedAt string
	UpdatedAt string
}

type Journal struct {
	ID        int64
	UserID    int64
	Name      string
	CreatedAt string
	UpdatedAt string
}

type CreateProfileInput struct {
	Name     string
	PIN      string
	Timezone string
}

type Session struct {
	Token     string
	ExpiresAt time.Time
}

func validName(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", ErrInvalidName
	}
	return value, nil
}

func validTimezone(value string) (string, error) {
	if value == "" {
		return "UTC", nil
	}
	location, err := time.LoadLocation(value)
	if err != nil {
		return "", ErrInvalidTimezone
	}
	return location.String(), nil
}

func validPIN(value string) error {
	if len(value) < 4 {
		return ErrPINTooShort
	}
	return nil
}
