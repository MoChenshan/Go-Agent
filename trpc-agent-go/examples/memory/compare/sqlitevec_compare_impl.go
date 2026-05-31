//go:build !cgo || sqliteveccgo

package main

import (
	"context"
	"os"

	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	"trpc.group/trpc-go/trpc-agent-go/memory"

	memorysqlitevec "trpc.group/trpc-go/trpc-agent-go/memory/sqlitevec"
)

func createSQLiteVecService(
	ctx context.Context,
) (memory.Service, func(), error) {
	db, path, err := openTempSQLiteDB(sqliteVecTempDBPattern)
	if err != nil {
		return nil, func() {}, err
	}

	emb := newOpenAIEmbedderFromEnv()
	svc, err := memorysqlitevec.NewService(
		db,
		memorysqlitevec.WithEmbedder(emb),
	)
	if err != nil {
		_ = db.Close()
		_ = os.Remove(path)
		return nil, func() {}, err
	}
	_ = ctx

	cleanup := func() {
		_ = svc.Close()
		_ = os.Remove(path)
	}
	return svc, cleanup, nil
}
