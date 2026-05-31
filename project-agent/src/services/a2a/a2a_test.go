package a2a

import (
	"context"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// mockAgent 仅用于 a2a 单测的最小 Agent 实现。
type mockAgent struct{}

func (mockAgent) Run(_ context.Context, _ *agent.Invocation) (<-chan *event.Event, error) {
	ch := make(chan *event.Event)
	close(ch)
	return ch, nil
}
func (mockAgent) Tools() []tool.Tool                 { return nil }
func (mockAgent) Info() agent.Info                   { return agent.Info{Name: "mock"} }
func (mockAgent) SubAgents() []agent.Agent           { return nil }
func (mockAgent) FindSubAgent(_ string) agent.Agent  { return nil }

// 编译期保证 mockAgent 实现 agent.Agent。
var _ agent.Agent = mockAgent{}

// TestNew_NilAgent 验证 Agent 为 nil 时返回错误。
func TestNew_NilAgent(t *testing.T) {
	_, err := New(Config{})
	if err == nil {
		t.Fatal("expected error when agent is nil, got nil")
	}
}

// TestNew_DefaultServiceName 验证未填 ServiceName 时使用默认值。
func TestNew_DefaultServiceName(t *testing.T) {
	srv, err := New(Config{Agent: mockAgent{}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if srv.ServiceName() != DefaultServiceName {
		t.Errorf("ServiceName: want %q, got %q", DefaultServiceName, srv.ServiceName())
	}
}

// TestStub_NotEnabled 验证 stub 构建下 Enabled()==false 且 Mount 返回错误。
// 加 `-tags a2a` 构建时这些断言不适用，自动跳过。
func TestStub_NotEnabled(t *testing.T) {
	srv, err := New(Config{Agent: mockAgent{}, ServiceName: "custom"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if srv.Enabled() {
		t.Skip("running under -tags a2a, skip stub-only assertion")
	}
	if err := srv.Mount(nil, "/a2a"); err == nil {
		t.Errorf("expected error from stub Mount, got nil")
	}
}

// TestServer_Config 验证 Config() 返回原样配置。
func TestServer_Config(t *testing.T) {
	want := Config{Agent: mockAgent{}, ServiceName: "trpc.x.y"}
	srv, err := New(want)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := srv.Config()
	if got.ServiceName != want.ServiceName {
		t.Errorf("Config.ServiceName: want %q, got %q", want.ServiceName, got.ServiceName)
	}
}
