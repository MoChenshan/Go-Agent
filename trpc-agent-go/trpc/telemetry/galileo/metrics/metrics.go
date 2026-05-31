// Package metrics provides integration with the Galileo telemetry system.
// It sets up the telemetry metrics  using the default exporter from Galileo.
// It registers a setup hook to ensure that the telemetry system is initialized correctly.
package metrics

import (
	"git.woa.com/galileo/eco/go/sdk/base/model"
	"git.woa.com/galileo/trpc-agent-go-galileo/metrics"
)

// Setup 设置 metrics 参数
func Setup(res model.Resource, cfg model.OpenTelemetryPushConfig) error {
	return metrics.Setup(res, cfg)
}
