// Package trace provides integration with the Galileo telemetry system.
// It sets up the telemetry tracer  using the default exporter from Galileo.
// It registers a setup hook to ensure that the telemetry system is initialized correctly.
package trace

import (
	"git.woa.com/galileo/eco/go/sdk/base/configs"
	"git.woa.com/galileo/trpc-agent-go-galileo/trace"
)

// Setup 伽利略采集初始化,如果是非 trpc 服务，需要手动调用一次
func Setup(traceConf *configs.Traces) error {
	return trace.Setup(traceConf)
}
