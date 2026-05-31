package zhiyanllm

import (
	"context"
	"testing"

	sdkzhiyanllm "git.woa.com/zhiyan-monitor/sdk/llm_go_sdk"
)

func TestWithBusinessScenarioMergesExistingProperties(t *testing.T) {
	original := map[string]string{
		"user":       "alice",
		"session_id": "sess-001",
	}
	ctx := context.WithValue(context.Background(), sdkzhiyanllm.AssociationPropertiesKey{}, original)

	updated := WithBusinessScenario(ctx, "customer_service")

	props, ok := updated.Value(sdkzhiyanllm.AssociationPropertiesKey{}).(map[string]string)
	if !ok {
		t.Fatal("expected association properties in updated context")
	}
	if got := props["user"]; got != "alice" {
		t.Fatalf("user = %q, want %q", got, "alice")
	}
	if got := props["session_id"]; got != "sess-001" {
		t.Fatalf("session_id = %q, want %q", got, "sess-001")
	}
	if got := props[associationPropertyBusinessScenario]; got != "customer_service" {
		t.Fatalf("business scenario = %q, want %q", got, "customer_service")
	}
	if _, ok := original[associationPropertyBusinessScenario]; ok {
		t.Fatal("original properties map was mutated")
	}
}

func TestWithBusinessScenarioEmptyStringNoop(t *testing.T) {
	ctx := context.Background()
	if got := WithBusinessScenario(ctx, ""); got != ctx {
		t.Fatal("expected empty business scenario to keep original context")
	}
}
