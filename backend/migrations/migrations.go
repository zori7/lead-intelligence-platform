// Package migrations embeds the goose SQL migrations so the API binary can
// apply them on startup without shipping loose files.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
