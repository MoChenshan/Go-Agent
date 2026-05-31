package tmemory

import (
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/session"
)

func TestService_Watermark_ReadWrite(t *testing.T) {
	svc := &Service{}
	key := "app/user/s1"

	// No watermark set => zero time.
	if got := svc.readWatermark(key); !got.IsZero() {
		t.Fatalf("expected zero time, got %v", got)
	}

	// Write and read back.
	now := time.Now().UTC().Truncate(time.Nanosecond)
	svc.writeWatermark(key, now)
	if got := svc.readWatermark(key); !got.Equal(now) {
		t.Fatalf("expected %v, got %v", now, got)
	}
}

func TestService_Watermark_Monotonic(t *testing.T) {
	svc := &Service{}
	key := "app/user/s1"

	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Second)

	svc.writeWatermark(key, t2)
	// Attempt to move backwards: must be ignored.
	svc.writeWatermark(key, t1)
	if got := svc.readWatermark(key); !got.Equal(t2) {
		t.Fatalf("watermark must not move backwards: want %v, got %v", t2, got)
	}
}

func TestService_Watermark_ZeroIgnored(t *testing.T) {
	svc := &Service{}
	key := "k"
	// Writing zero time on a fresh key should not store anything.
	svc.writeWatermark(key, time.Time{})
	if got := svc.readWatermark(key); !got.IsZero() {
		t.Fatalf("zero write should not advance watermark, got %v", got)
	}
}

func TestScanDeltaSince_NilSession(t *testing.T) {
	ts, msgs := scanDeltaSince(nil, time.Time{})
	if !ts.IsZero() || len(msgs) != 0 {
		t.Fatal("expected empty results for nil session")
	}
}

func makeEvent(ts time.Time, role model.Role, content string) event.Event {
	return event.Event{
		Timestamp: ts,
		Response: &model.Response{
			Choices: []model.Choice{
				{Message: model.Message{Role: role, Content: content}},
			},
		},
	}
}

func TestScanDeltaSince_Basic(t *testing.T) {
	sess := session.NewSession("app", "user", "sess1")
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Second)
	t3 := t2.Add(time.Second)

	sess.Events = []event.Event{
		makeEvent(t1, model.RoleUser, "hello"),
		makeEvent(t2, model.RoleAssistant, "hi"),
		makeEvent(t3, model.RoleUser, "how are you"),
	}

	// Scan all (since zero).
	latestTs, msgs := scanDeltaSince(sess, time.Time{})
	if !latestTs.Equal(t3) {
		t.Fatalf("expected latestTs %v, got %v", t3, latestTs)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgs))
	}

	// Scan since t1 (should exclude t1).
	latestTs, msgs = scanDeltaSince(sess, t1)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages after t1, got %d", len(msgs))
	}
	if msgs[0].Message.Content != "hi" {
		t.Fatalf("expected 'hi', got %q", msgs[0].Message.Content)
	}

	// Scan since t3 (should return nothing).
	_, msgs = scanDeltaSince(sess, t3)
	if len(msgs) != 0 {
		t.Fatalf("expected 0 messages after t3, got %d", len(msgs))
	}
}

func TestScanDeltaSince_SkipToolMessages(t *testing.T) {
	sess := session.NewSession("app", "user", "sess1")
	ts := time.Now()

	sess.Events = []event.Event{
		makeEvent(ts, model.RoleUser, "hello"),
		{
			Timestamp: ts.Add(time.Second),
			Response: &model.Response{
				Choices: []model.Choice{
					{Message: model.Message{Role: model.RoleTool, Content: "tool result"}},
				},
			},
		},
		{
			Timestamp: ts.Add(2 * time.Second),
			Response: &model.Response{
				Choices: []model.Choice{
					{Message: model.Message{Role: model.RoleAssistant, ToolCalls: []model.ToolCall{{ID: "tc1"}}}},
				},
			},
		},
		{
			Timestamp: ts.Add(3 * time.Second),
			Response: &model.Response{
				Choices: []model.Choice{
					{Message: model.Message{Role: model.RoleTool, ToolID: "tc1", Content: "tool output"}},
				},
			},
		},
		makeEvent(ts.Add(4*time.Second), model.RoleAssistant, "final answer"),
	}

	_, msgs := scanDeltaSince(sess, time.Time{})
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages (user+final assistant), got %d", len(msgs))
	}
	if msgs[0].Message.Content != "hello" {
		t.Fatalf("first should be 'hello', got %q", msgs[0].Message.Content)
	}
	if msgs[1].Message.Content != "final answer" {
		t.Fatalf("second should be 'final answer', got %q", msgs[1].Message.Content)
	}
}

func TestScanDeltaSince_SkipNonUserAssistant(t *testing.T) {
	sess := session.NewSession("app", "user", "sess1")
	ts := time.Now()

	sess.Events = []event.Event{
		{
			Timestamp: ts,
			Response: &model.Response{
				Choices: []model.Choice{
					{Message: model.Message{Role: model.RoleSystem, Content: "system prompt"}},
				},
			},
		},
		makeEvent(ts.Add(time.Second), model.RoleUser, "hello"),
	}

	_, msgs := scanDeltaSince(sess, time.Time{})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
}

func TestScanDeltaSince_SkipEmptyContent(t *testing.T) {
	sess := session.NewSession("app", "user", "sess1")
	ts := time.Now()

	sess.Events = []event.Event{
		makeEvent(ts, model.RoleUser, ""),
		makeEvent(ts.Add(time.Second), model.RoleAssistant, "reply"),
	}

	_, msgs := scanDeltaSince(sess, time.Time{})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
}

func TestScanDeltaSince_NilResponse(t *testing.T) {
	sess := session.NewSession("app", "user", "sess1")
	ts := time.Now()

	sess.Events = []event.Event{
		{Timestamp: ts, Response: nil},
		makeEvent(ts.Add(time.Second), model.RoleUser, "hello"),
	}

	latestTs, msgs := scanDeltaSince(sess, time.Time{})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	// latestTs should be the max of all event timestamps.
	if !latestTs.Equal(ts.Add(time.Second)) {
		t.Fatalf("unexpected latestTs: %v", latestTs)
	}
}

func TestMessageText_PlainContent(t *testing.T) {
	msg := model.Message{Content: "  hello world  "}
	if got := messageText(msg); got != "hello world" {
		t.Fatalf("expected trimmed content, got %q", got)
	}
}

func TestMessageText_ContentParts(t *testing.T) {
	text1 := "part one"
	text2 := "  part two  "
	empty := "   "
	msg := model.Message{
		ContentParts: []model.ContentPart{
			{Type: model.ContentTypeText, Text: &text1},
			{Type: model.ContentTypeText, Text: &empty},
			{Type: model.ContentTypeText, Text: &text2},
			{Type: "image", Text: nil}, // Non-text part, should be skipped.
		},
	}
	got := messageText(msg)
	if got != "part one\npart two" {
		t.Fatalf("expected 'part one\\npart two', got %q", got)
	}
}

func TestMessageText_EmptyMessage(t *testing.T) {
	msg := model.Message{}
	if got := messageText(msg); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestMessageText_ContentTakesPrecedence(t *testing.T) {
	text := "from parts"
	msg := model.Message{
		Content: "from content",
		ContentParts: []model.ContentPart{
			{Type: model.ContentTypeText, Text: &text},
		},
	}
	if got := messageText(msg); got != "from content" {
		t.Fatalf("plain Content should take precedence, got %q", got)
	}
}

func TestRoleToName(t *testing.T) {
	tests := []struct {
		role model.Role
		want string
	}{
		{model.RoleUser, "用户"},
		{model.RoleAssistant, "助手"},
		{model.RoleSystem, "system"},
		{model.Role("custom"), "custom"},
	}
	for _, tt := range tests {
		if got := roleToName(tt.role); got != tt.want {
			t.Fatalf("roleToName(%s) = %q, want %q", tt.role, got, tt.want)
		}
	}
}
