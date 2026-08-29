package migrations

import "embed"

// Files contains the SQL migrations applied during database initialization.
//
//go:embed *.sql
var Files embed.FS
