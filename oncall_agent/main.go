// Package main 启动oncall agent服务
package main

import (
	"context"

	trpc "git.code.oa.com/trpc-go/trpc-go"
	thttp "git.code.oa.com/trpc-go/trpc-go/http"
	"git.code.oa.com/trpc-go/trpc-go/log"
	"git.code.oa.com/trpc-go/trpc-go/server"
	a2atrpc "git.woa.com/trpc-go/trpc-a2a-go/trpc"
	tagui "git.woa.com/trpc-go/trpc-agent-go/trpc/agui"
	pb "git.woa.com/trpcprotocol/magic/oncall_agent_oncall_agent_debug"

	"git.woa.com/video_pay_oss/magic_group/oncall_agent/infrastructure/config/rainbow"

	_ "git.code.oa.com/trpc-go/trpc-config-rainbow"
	_ "git.code.oa.com/trpc-go/trpc-filter/recovery"
	_ "git.code.oa.com/trpc-go/trpc-filter/validation"
	_ "git.code.oa.com/trpc-go/trpc-metrics-runtime"
	_ "git.code.oa.com/trpc-go/trpc-naming-polaris"
	_ "git.woa.com/galileo/trpc-go-galileo"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/galileo"
	_ "git.woa.com/video-libs/trpc-jce-compatible"
	_ "git.woa.com/video_pay_middle_platform/pay-go-comm/tcache/filter/infcache"
	_ "go.uber.org/automaxprocs"
)

const (
	a2aServiceName      = "trpc.magic.oncall_agent.a2a"
	sseServiceName      = "trpc.magic.oncall_agent.sse"
	aguiServiceName     = "trpc.magic.oncall_agent.agui"
	cdkeySSEServiceName = "trpc.magic.oncall_agent.cdkey_sse"
	mysqlFeedbackName   = "trpc.mysql.oncall.feedback"
)

var (
	// pathMap 将sse服务名映射到path
	pathMap = map[string]string{
		sseServiceName:      "/v1/agent",
		cdkeySSEServiceName: "/v1/cdkey_agent",
	}
)

func main() {
	s := trpc.NewServer(server.WithFilter(reqFilter))

	if err := rainbow.Init(); err != nil {
		log.Fatalf("rainbow.Init failed: %v", err)
	}

	app, err := InitApp(rainbow.GetCfg())
	if err != nil {
		log.Fatalf("InitApp failed: %v", err)
	}

	for name, srv := range app.A2AServers {
		if err := a2atrpc.RegisterA2AServer(s, name, srv); err != nil {
			log.Fatalf("RegisterA2AServer failed: %v", err)
		}
	}
	for name, srv := range app.SSEServers {
		thttp.HandleFunc(pathMap[name], srv.HandleSSE)
		thttp.RegisterNoProtocolService(s.Service(name))
	}
	for name, srv := range app.AguiServers {
		if err := tagui.RegisterAGUIServer(s, name, srv); err != nil {
			log.Fatalf("RegisterAGUIServer failed: %v", err)
		}
	}
	pb.RegisterDebugService(s, app.DebugSrv)

	// 启动企微WeCom AI Bot WebSocket服务 (如果已启用)
	if app.WeComServer != nil {
		go func() {
			if err := app.WeComServer.Run(context.Background()); err != nil {
				log.Errorf("WeCom server error: %v", err)
			}
		}()
	}

	if err := s.Serve(); err != nil {
		log.Fatalf("serve failed: %v", err)
	}
}
