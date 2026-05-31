package main

import (
	"git.code.oa.com/trpc-go/trpc-go"
	"git.code.oa.com/trpc-go/trpc-go/log"
	taiji "git.woa.com/trpc-go/trpc-agent-go/trpc/server/taijia2a"
	taijiconf "git.woa.com/trpc-go/trpc-agent-go/trpc/server/taijia2a/config"

	_ "git.code.oa.com/trpc-go/trpc-filter/debuglog"
	_ "git.code.oa.com/trpc-go/trpc-filter/recovery"
	_ "git.code.oa.com/trpc-go/trpc-metrics-runtime"
	_ "git.code.oa.com/trpc-go/trpc-naming-polaris"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	_ "go.uber.org/automaxprocs"
)

func main() {
	s := trpc.NewServer()

	taijiconf.RegisterConfiger(taijiconf.NewLocalConfiger())
	if err := loadProxyConfig("proxy.json"); err != nil {
		log.Fatalf("Failed to load proxy config: %v", err)
	}

	name := "trpc.group.trpc-go.trpc-agent-go.taijia2a" // server name
	if err := taiji.RegisterTaijiA2AProxyServer(s, name, "api/v1/agent/"); err != nil {
		log.Fatalf("Failed to register Taiji A2A proxy server: %v", err)
	}

	if err := s.Serve(); err != nil {
		log.Fatal(err)
	}
}
