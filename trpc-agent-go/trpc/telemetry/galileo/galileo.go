// Package galileo provides integration with the Galileo telemetry system.
// It sets up the telemetry tracer and meter using the default exporter from Galileo.
// It registers a setup hook to ensure that the telemetry system is initialized correctly.
package galileo

import (
	"git.code.oa.com/trpc-go/trpc-go/plugin"
	"git.woa.com/galileo/eco/go/sdk/base/configs/traces"
	"git.woa.com/galileo/eco/go/sdk/base/semconv"
	galileo "git.woa.com/galileo/trpc-go-galileo"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/galileo/evaluation"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/galileo/metrics"
	"git.woa.com/trpc-go/trpc-agent-go/trpc/telemetry/galileo/trace"
	"trpc.group/trpc-go/trpc-agent-go/telemetry/errs"

	// Import as a side effect to automatically use the internal utilities.
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
)

func init() {
	pluginKey := "telemetry-galileo"
	hook := plugin.GetSetupHook(pluginKey)
	plugin.RegisterSetupHook(pluginKey, func(setup func() error) error {
		if err := hook(setup); err != nil {
			return err
		}
		galileoConf := galileo.GetGalileoConfig()
		traceConf := traces.NewConfig(
			&galileoConf.Resource,
			traces.WithProcessor(&galileoConf.Config.TracesConfig.Processor),
			traces.WithExporter(&galileoConf.Config.TracesConfig.Exporter),
			traces.WithEnableProfile(galileoConf.Config.ProfilesConfig.Processor.EnableLinkTrace),
			traces.WithSchemaURL(semconv.SchemaURL),
		)
		if err := metrics.Setup(galileoConf.Resource, galileoConf.Config.OpentelemetryPush); err != nil {
			return err
		}
		if err := evaluation.Setup(galileoConf.Resource, galileoConf.OcpAddr); err != nil {
			return err
		}
		return trace.Setup(traceConf)
	})
	errs.ToResponseError = toGalileoResponseError
}
