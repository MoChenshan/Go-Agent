//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//

package main

import (
	"context"
	"fmt"

	"git.code.oa.com/trpc-go/trpc-go"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	tpromptiter "git.woa.com/trpc-go/trpc-agent-go/trpc/promptiter"

	spromptiter "trpc.group/trpc-go/trpc-agent-go/server/promptiter"
)

const serviceName = "trpc.test.promptiter.server"

func runPromptIterServer(ctx context.Context, cfg serverConfig) error {
	runtime, err := buildPromptIterRuntime(ctx, cfg)
	if err != nil {
		return err
	}
	defer runtime.close()
	server, err := spromptiter.New(
		spromptiter.WithAppName(appName),
		spromptiter.WithBasePath(cfg.BasePath),
		spromptiter.WithEngine(runtime.engine),
		spromptiter.WithManager(runtime.manager),
	)
	if err != nil {
		return fmt.Errorf("create promptiter server: %w", err)
	}
	trpcServer := trpc.NewServer()
	if err := tpromptiter.RegisterPromptIterServer(trpcServer, serviceName, server); err != nil {
		return fmt.Errorf("register promptiter server: %w", err)
	}
	return trpcServer.Serve()
}
