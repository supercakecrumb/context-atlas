// Package migrations exposes the embedded Goose migration set.
package migrations

import "embed"

// FS is intentionally rooted at this package so the executable never relies on
// a working-directory-relative migration path.
//
//go:embed *.sql
var FS embed.FS
