// Package migrations embeds the goose SQL migrations so binaries carry their own schema.
package migrations

import "embed"

// FS holds every NNNNN_description.sql migration.
//
//go:embed *.sql
var FS embed.FS
