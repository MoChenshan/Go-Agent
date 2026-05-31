//go:build cgo

package backends

import _ "github.com/mattn/go-sqlite3"

func ensureSQLiteDriver() error {
	return nil
}
