package zhiyanllm

import (
	"context"

	"git.code.oa.com/trpc-go/trpc-go/plugin"
	sdkzhiyanllm "git.woa.com/zhiyan-monitor/sdk/llm_go_sdk"
)

const (
	pluginType = "telemetry"
	pluginName = "zhiyan-llm"
)

func init() {
	plugin.Register(pluginName, &zhiyanLLMPlugin{})
}

// PluginConfig is the trpc_go.yaml config for plugins.telemetry.zhiyan-llm.
// YAML overrides env: empty fields mean "use env (or library defaults)".
type PluginConfig struct {
	APIEndpoint string `yaml:"api_endpoint"`
	APIKey      string `yaml:"api_key"`
	AppName     string `yaml:"app_name"`

	AttributeValueLengthLimit   int `yaml:"attribute_value_length_limit"`
	AttributeCountLimit         int `yaml:"attribute_count_limit"`
	EventCountLimit             int `yaml:"event_count_limit"`
	AttributePerEventCountLimit int `yaml:"attribute_per_event_count_limit"`
}

type zhiyanLLMPlugin struct {
	client *sdkzhiyanllm.Zhiyanllm
}

func (*zhiyanLLMPlugin) Type() string { return pluginType }

func (p *zhiyanLLMPlugin) Setup(_ string, decoder plugin.Decoder) error {
	var cfg PluginConfig
	if err := decoder.Decode(&cfg); err != nil {
		return err
	}

	opts := buildOptions(cfg)

	c, err := Start(context.Background(), opts...)
	if err != nil {
		return err
	}

	p.client = c
	return nil
}

// Close implements plugin.Closer.
// It tries to shutdown the underlying SDK client if supported; otherwise it's a no-op.
func (p *zhiyanLLMPlugin) Close() error {
	p.client.Shutdown(context.Background())
	return nil
}

func buildOptions(yamlCfg PluginConfig) []Option {
	opts := make([]Option, 0, 8)

	// Strings: only override defaults if explicitly set in YAML.
	if yamlCfg.APIEndpoint != "" {
		opts = append(opts, WithAPIEndpoint(yamlCfg.APIEndpoint))
	}
	if yamlCfg.APIKey != "" {
		opts = append(opts, WithAPIKey(yamlCfg.APIKey))
	}
	if yamlCfg.AppName != "" {
		opts = append(opts, WithAppName(yamlCfg.AppName))
	}

	// Limits: only override defaults if explicitly set in YAML.
	// (0 means "unset" because these are limits.)
	if yamlCfg.AttributeValueLengthLimit > 0 {
		opts = append(opts, WithAttributeValueLengthLimit(yamlCfg.AttributeValueLengthLimit))
	}
	if yamlCfg.AttributeCountLimit > 0 {
		opts = append(opts, WithAttributeCountLimit(yamlCfg.AttributeCountLimit))
	}
	if yamlCfg.EventCountLimit > 0 {
		opts = append(opts, WithEventCountLimit(yamlCfg.EventCountLimit))
	}
	if yamlCfg.AttributePerEventCountLimit > 0 {
		opts = append(opts, WithAttributePerEventCountLimit(yamlCfg.AttributePerEventCountLimit))
	}

	return opts
}
