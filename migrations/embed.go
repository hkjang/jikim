package migrations

import "embed"

// Files contains the immutable database migrations shipped with the server.
//
//go:embed *.sql
var Files embed.FS
