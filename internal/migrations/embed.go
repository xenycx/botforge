// Package migrations embeds the versioned SQL migrations.
package migrations

import "embed"

// FS contains files named NNNN_description.sql, applied in numeric order.
//
//go:embed *.sql
var FS embed.FS
