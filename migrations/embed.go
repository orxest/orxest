// Package migrations embeds the Orxest SQL schema so that a single binary can
// bring an empty database up to date (spec §40).
package migrations

import "embed"

// FS holds all migration files, applied in lexical order.
//
//go:embed *.sql
var FS embed.FS
