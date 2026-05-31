package zhiyanllm

import (
	"context"

	sdkzhiyanllm "git.woa.com/zhiyan-monitor/sdk/llm_go_sdk"
)

const associationPropertyBusinessScenario = "business_scenario"

// WithBusinessScenario stores the Zhiyan business scenario in the context so
// spans created by the Zhiyan SDK can inherit it through association properties.
// Callers should pass a non-nil ctx, following Go context conventions.
// https://iwiki.woa.com/p/4013906196#%E9%93%BE%E8%B7%AF%E9%99%84%E5%8A%A0%E5%B1%9E%E6%80%A7(%E5%85%A8%E5%B1%80%E7%94%9F%E6%95%88)
func WithBusinessScenario(ctx context.Context, businessScenario string) context.Context {
	if businessScenario == "" {
		return ctx
	}

	props := make(map[string]string, 1)
	if existing, ok := ctx.Value(sdkzhiyanllm.AssociationPropertiesKey{}).(map[string]string); ok {
		props = make(map[string]string, len(existing)+1)
		for k, v := range existing {
			props[k] = v
		}
	}
	props[associationPropertyBusinessScenario] = businessScenario

	return context.WithValue(ctx, sdkzhiyanllm.AssociationPropertiesKey{}, props)
}
