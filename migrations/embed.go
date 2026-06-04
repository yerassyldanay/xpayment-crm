// Package migrations embeds the SQLite schema so the binary carries it (doc 04).
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
