// Package zhiyan provides telemetry integration with Zhiyan observability platform.
package zhiyan

import (
	tlog "git.code.oa.com/trpc-go/trpc-go/log"
	"git.code.oa.com/trpc-go/trpc-go/plugin"
	opentelemetry "git.woa.com/opentelemetry/opentelemetry-go-ecosystem"
	"go.opentelemetry.io/otel"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/telemetry/metric"
	"trpc.group/trpc-go/trpc-agent-go/telemetry/trace"

	// Import the zhiyan trpc plugin for telemetry
	_ "git.woa.com/opentelemetry/opentelemetry-go-ecosystem/instrumentation/oteltrpc"
	// Import as a side effect to automatically use the internal utilities.
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
)

func init() {
	pluginKey := "telemetry-opentelemetry"
	hook := plugin.GetSetupHook(pluginKey)
	plugin.RegisterSetupHook(pluginKey, func(setup func() error) error {
		if err := hook(setup); err != nil {
			return err
		}
		trace.Tracer = opentelemetry.GlobalTracer()
		if err := metric.InitMeterProvider(otel.GetMeterProvider()); err != nil {
			return err
		}
		log.Default = tlog.DefaultLogger
		return nil
	})
}
