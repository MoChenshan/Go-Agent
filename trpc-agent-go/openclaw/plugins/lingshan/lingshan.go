// Package lingshan registers the LingShan knowledge provider with the
// OpenClaw registry, allowing it to be used via knowledges.providers YAML
// configuration with type: "lingshan".
package lingshan

import (
	"fmt"
	"net/http"
	"strings"

	lingshan "git.woa.com/trpc-go/trpc-agent-go/trpc/knowledge/lingshan"
	"trpc.group/trpc-go/trpc-agent-go/knowledge"
	"trpc.group/trpc-go/trpc-agent-go/openclaw/registry"
)

const PluginType = "lingshan"

func init() {
	if err := registry.RegisterKnowledgeProvider(PluginType, newLingshanKnowledge); err != nil {
		panic(err)
	}
}

type lingshanConfig struct {
	URL             string            `yaml:"url,omitempty"`
	ServiceName     string            `yaml:"service_name,omitempty"`
	KnowledgeBaseID string            `yaml:"knowledge_base_id"`
	Headers         map[string]string `yaml:"headers,omitempty"`
}

func newLingshanKnowledge(
	_ registry.KnowledgeProviderDeps,
	spec registry.PluginSpec,
) (knowledge.Knowledge, error) {
	var cfg lingshanConfig
	if err := registry.DecodeStrict(spec.Config, &cfg); err != nil {
		return nil, fmt.Errorf("decode lingshan config: %w", err)
	}
	if strings.TrimSpace(cfg.ServiceName) == "" && strings.TrimSpace(cfg.URL) == "" {
		return nil, fmt.Errorf("lingshan: at least one of service_name or url must be set")
	}
	if strings.TrimSpace(cfg.KnowledgeBaseID) == "" {
		return nil, fmt.Errorf("lingshan: knowledge_base_id is required")
	}

	opts := []lingshan.Option{
		lingshan.WithKnowledgeBaseID(cfg.KnowledgeBaseID),
	}
	if v := strings.TrimSpace(cfg.ServiceName); v != "" {
		opts = append(opts, lingshan.WithServiceName(v))
	}
	if v := strings.TrimSpace(cfg.URL); v != "" {
		opts = append(opts, lingshan.WithURL(v))
	}
	if len(cfg.Headers) > 0 {
		h := make(http.Header, len(cfg.Headers))
		for k, v := range cfg.Headers {
			h.Set(k, v)
		}
		opts = append(opts, lingshan.WithHTTPHeaders(h))
	}
	return lingshan.New(opts...), nil
}
