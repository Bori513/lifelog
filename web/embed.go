package webassets

import "embed"

// Files contains the web templates and static assets compiled into LifeLog.
//
//go:embed templates/*.html static/*
var Files embed.FS
