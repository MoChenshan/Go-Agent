//go:build cgo && !sqliteveccgo

package main

import (
	"context"
	"errors"

	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	"trpc.group/trpc-go/trpc-agent-go/memory"
)

const sqliteVecCompareUnavailableMessage = "" +
	"sqlitevec compare is unavailable in default cgo builds; use " +
	"CGO_ENABLED=0, or build with -tags sqliteveccgo on " +
	"a system that provides sqlite3.h"

func createSQLiteVecService(
	ctx context.Context,
) (memory.Service, func(), error) {
	_ = ctx
	return nil, func() {}, errors.New(sqliteVecCompareUnavailableMessage)
}
