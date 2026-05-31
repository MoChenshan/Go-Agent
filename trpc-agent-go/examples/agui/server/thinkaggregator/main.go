//
// Tencent is pleased to support the open source community by making trpc-agent-go available.
//
// Copyright (C) 2025 Tencent.  All rights reserved.
//
// trpc-agent-go is licensed under the Apache License Version 2.0.
//
//

package main

import (
	"context"
	"encoding/json"
	"flag"
	"time"

	"git.code.oa.com/trpc-go/trpc-go"
	tagui "git.woa.com/trpc-go/trpc-agent-go/trpc/agui"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/server/agui"
	"trpc.group/trpc-go/trpc-agent-go/server/agui/adapter"
	"trpc.group/trpc-go/trpc-agent-go/server/agui/aggregator"
	aguirunner "trpc.group/trpc-go/trpc-agent-go/server/agui/runner"
	"trpc.group/trpc-go/trpc-agent-go/server/agui/translator"
	"trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	_ "trpc.group/trpc-go/trpc-agent-go/session/postgres"
	_ "trpc.group/trpc-go/trpc-agent-go/session/redis"
)

var (
	modelName            = flag.String("model", "deepseek-r1-local-III", "Model to use")
	isStream             = flag.Bool("stream", true, "Whether to stream the response")
	path                 = flag.String("path", "/agui", "HTTP path")
	messagesSnapshotPath = flag.String("messages-snapshot-path", "/history", "Messages snapshot HTTP path")
)

const appName = "demo-app"

func main() {
	flag.Parse()
	agent := newAgent()
	sessionService := inmemory.NewSessionService()
	runner := runner.NewRunner(appName, agent, runner.WithSessionService(sessionService))
	defer runner.Close()
	// 加载配置文件，创建 trpc 服务
	server := trpc.NewServer()
	// 创建 AG-UI 服务
	aguiServer, err := agui.New(
		runner,
		agui.WithPath(*path),
		agui.WithMessagesSnapshotPath(*messagesSnapshotPath),
		agui.WithMessagesSnapshotEnabled(true),
		agui.WithAppName(appName),
		agui.WithSessionService(sessionService),
		agui.WithAGUIRunnerOptions(
			aguirunner.WithUserIDResolver(userIDResolver),
			aguirunner.WithAggregationOption(
				aggregator.WithEnabled(true),
			),
			aguirunner.WithFlushInterval(1*time.Second),
			aguirunner.WithAggregatorFactory(newAggregator),
			aguirunner.WithTranslatorFactory(newTranslator),
			aguirunner.WithTranslateCallbacks(translator.NewCallbacks().RegisterBeforeTranslate(
				func(ctx context.Context, event *event.Event) (*event.Event, error) {
					data, _ := json.Marshal(event)
					log.Infof("before event: %s", string(data))
					return nil, nil
				},
			).RegisterAfterTranslate(func(ctx context.Context, event events.Event) (events.Event, error) {
				data, _ := json.Marshal(event)
				log.Infof("after event: %s", string(data))
				return nil, nil
			}),
			),
		),
	)
	if err != nil {
		log.Fatalf("failed to create AG-UI server: %v", err)
	}
	// 将 AG-UI 服务注册到 trpc 服务
	tagui.RegisterAGUIServer(server, "trpc.test.thinkaggregator.agui", aguiServer)
	// 启动 trpc 服务
	if err := server.Serve(); err != nil {
		log.Fatalf("server stopped with error: %v", err)
	}
}

func userIDResolver(ctx context.Context, input *adapter.RunAgentInput) (string, error) {
	forwardedProps, ok := input.ForwardedProps.(map[string]any)
	if !ok {
		return "anonymous", nil
	}
	user, ok := forwardedProps["userId"].(string)
	if !ok {
		return "anonymous", nil
	}
	if user != "" {
		return user, nil
	}
	return "anonymous", nil
}
