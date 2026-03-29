// Package migrations embeds all SQL migration files.
// Keeping embed here avoids Go's restriction on ".." in embed paths.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
