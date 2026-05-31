package trpc

import (
	runtime "git.code.oa.com/trpc-go/trpc-metrics-runtime"
	"trpc.group/trpc-go/trpc-agent-go/telemetry/appid"
)

const version = "v1.8.0"

func init() {
	// Inject resolvers so runtime.Stat can fallback to Runner names.
	runtime.ResolveApp = appid.DefaultApp
	runtime.ResolveServer = appid.DefaultAgent
	go runtime.StatReport(version, "trpc-agent-go")
}
