//go:build !cgo

package backends

import "errors"

var errSQLiteMemoryNeedsCGO = errors.New(
	"sqlite memory backend requires cgo-enabled sqlite3 driver",
)

func ensureSQLiteDriver() error {
	return errSQLiteMemoryNeedsCGO
}
