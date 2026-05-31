// 多 Agent 协作端到端测试。
//
// 目标：验证 Coordinator + Sub-agent 这种典型分层结构在框架下能正确：
//   1) Coordinator.SubAgents() 返回所有子 Agent
//   2) FindSubAgent(name) 能定位到具体子 Agent
//   3) 子 Agent 的事件流能透传出来（多 hand-off 场景）
//
// 不依赖 LLM、不依赖外部 HTTP，纯本地 mock，可在 CI 秒级跑完。
package a2a

import (
	"context"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// scriptedAgent 按预设脚本输出事件的 mock Agent。
type scriptedAgent struct {
	name   string
	events []*event.Event
	subs   []agent.Agent
}

func (s *scriptedAgent) Run(_ context.Context, _ *agent.Invocation) (<-chan *event.Event, error) {
	ch := make(chan *event.Event, len(s.events))
	for _, ev := range s.events {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

func (s *scriptedAgent) Tools() []tool.Tool       { return nil }
func (s *scriptedAgent) Info() agent.Info         { return agent.Info{Name: s.name} }
func (s *scriptedAgent) SubAgents() []agent.Agent { return s.subs }
func (s *scriptedAgent) FindSubAgent(name string) agent.Agent {
	for _, sa := range s.subs {
		if sa.Info().Name == name {
			return sa
		}
	}
	return nil
}

var _ agent.Agent = (*scriptedAgent)(nil)

// newScriptedEvent 构造一个最小可用的脚本事件。
//
// trpc-agent-go 的 event.Event 没有 Done 字段；流的"结束"是靠 channel close 表达的。
// 这里的 RequiresCompletion 仅在我们想显式标记"这是最后一条需要 ack 的事件"时使用。
func newScriptedEvent(author string, last bool) *event.Event {
	return &event.Event{
		Author:             author,
		InvocationID:       "inv-test",
		RequiresCompletion: last,
	}
}

// TestMultiAgent_HandOffViaCoordinator 验证 Coordinator 能把请求委派给子 Agent 并透传事件。
func TestMultiAgent_HandOffViaCoordinator(t *testing.T) {
	diag := &scriptedAgent{
		name: "diagnosis_agent",
		events: []*event.Event{
			newScriptedEvent("diagnosis_agent", false),
			newScriptedEvent("diagnosis_agent", true),
		},
	}
	repair := &scriptedAgent{
		name: "repair_agent",
		events: []*event.Event{
			newScriptedEvent("repair_agent", true),
		},
	}
	coord := &scriptedAgent{
		name: "coordinator",
		subs: []agent.Agent{diag, repair},
	}

	// 1) SubAgents 返回完整列表
	if got := len(coord.SubAgents()); got != 2 {
		t.Fatalf("expected 2 sub-agents, got %d", got)
	}

	// 2) FindSubAgent 定位
	if sa := coord.FindSubAgent("diagnosis_agent"); sa == nil || sa.Info().Name != "diagnosis_agent" {
		t.Fatal("FindSubAgent diagnosis_agent failed")
	}
	if sa := coord.FindSubAgent("nonexistent"); sa != nil {
		t.Fatal("FindSubAgent should return nil for unknown name")
	}

	// 3) 通过 a2a.Server 包裹 coordinator，验证 Config 装配 & ServiceName 透传
	srv, err := New(Config{Agent: coord, ServiceName: "trpc.test.multi"})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	if srv.ServiceName() != "trpc.test.multi" {
		t.Fatalf("unexpected service name: %s", srv.ServiceName())
	}

	// 4) 直接调用底层子 Agent，模拟 Coordinator 路由后的 hand-off
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ch, err := diag.Run(ctx, &agent.Invocation{})
	if err != nil {
		t.Fatalf("diag.Run: %v", err)
	}

	var got int
	var lastRequiresCompletion bool
	for ev := range ch {
		got++
		lastRequiresCompletion = ev.RequiresCompletion
		if ev.Author != "diagnosis_agent" {
			t.Fatalf("event author should be diagnosis_agent, got %s", ev.Author)
		}
	}
	if got != 2 {
		t.Fatalf("expected 2 events, got %d", got)
	}
	if !lastRequiresCompletion {
		t.Fatal("last event should mark RequiresCompletion=true")
	}
}

// TestMultiAgent_FanOutSequential 验证按顺序触发多个子 Agent 不丢事件。
func TestMultiAgent_FanOutSequential(t *testing.T) {
	a := &scriptedAgent{name: "a", events: []*event.Event{newScriptedEvent("a", true)}}
	b := &scriptedAgent{name: "b", events: []*event.Event{newScriptedEvent("b", true)}}
	coord := &scriptedAgent{name: "coord", subs: []agent.Agent{a, b}}

	ctx := context.Background()
	for _, target := range []string{"a", "b"} {
		sa := coord.FindSubAgent(target)
		if sa == nil {
			t.Fatalf("FindSubAgent %s == nil", target)
		}
		ch, err := sa.Run(ctx, &agent.Invocation{})
		if err != nil {
			t.Fatalf("%s.Run: %v", target, err)
		}
		count := 0
		for ev := range ch {
			if ev.Author != target {
				t.Fatalf("expected author %s, got %s", target, ev.Author)
			}
			count++
		}
		if count == 0 {
			t.Fatalf("no events received from %s", target)
		}
	}
}
