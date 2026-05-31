//go:build cgo && !sqliteveccgo

package main

import (
	"context"
	"testing"

	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
)

func TestCreateSQLiteVecService_DefaultCGoBuild(t *testing.T) {
	svc, cleanup, err := createSQLiteVecService(context.Background())
	defer cleanup()

	if err == nil {
		t.Fatal("expected sqlitevec compare stub error")
	}
	if err.Error() != sqliteVecCompareUnavailableMessage {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc != nil {
		t.Fatalf("expected nil service, got %T", svc)
	}
}
