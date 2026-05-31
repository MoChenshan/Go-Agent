package audit

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestEmit_WritesJSONL(t *testing.T) {
	mem := &MemorySink{}
	old := SetSink(mem)
	defer SetSink(old)

	_ = os.Unsetenv("AUDIT_DISABLE")

	Emit(Event{
		User:     "alice",
		Agent:    "repair_agent",
		Action:   "gongfeng.mr.merge",
		Severity: "high",
		Target:   "proj!42",
		Params:   map[string]any{"project_id": "proj", "iid": 42},
		Reason:   "rollback oom fix",
		Success:  true,
		Mock:     true,
	})

	lines := mem.Snapshot()
	if len(lines) != 1 {
		t.Fatalf("want 1 line, got %d", len(lines))
	}
	raw := lines[0]
	if !strings.HasSuffix(string(raw), "\n") {
		t.Fatalf("record must end with newline: %q", raw)
	}

	var rec Record
	if err := json.Unmarshal(raw[:len(raw)-1], &rec); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if rec.User != "alice" {
		t.Errorf("user=%q", rec.User)
	}
	if rec.Result != "success" {
		t.Errorf("result=%q", rec.Result)
	}
	if !rec.Mock {
		t.Errorf("mock must be true")
	}
	if rec.TS == "" {
		t.Errorf("ts must not be empty")
	}
}

func TestEmit_FailureCapturesError(t *testing.T) {
	mem := &MemorySink{}
	old := SetSink(mem)
	defer SetSink(old)

	Emit(Event{
		User:    "bob",
		Action:  "devops.pipeline.rerun",
		Success: false,
		Err:     errors.New("network timeout"),
	})

	lines := mem.Snapshot()
	if len(lines) != 1 {
		t.Fatalf("want 1 line, got %d", len(lines))
	}
	var rec Record
	if err := json.Unmarshal(lines[0][:len(lines[0])-1], &rec); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rec.Result != "failure" {
		t.Errorf("result=%q", rec.Result)
	}
	if rec.ErrorMsg != "network timeout" {
		t.Errorf("error=%q", rec.ErrorMsg)
	}
}

func TestEmit_DisabledByEnv(t *testing.T) {
	mem := &MemorySink{}
	old := SetSink(mem)
	defer SetSink(old)

	t.Setenv("AUDIT_DISABLE", "1")
	Emit(Event{Action: "any"})
	if len(mem.Snapshot()) != 0 {
		t.Fatalf("expected no output when disabled")
	}
}

func TestEmit_DefaultUser(t *testing.T) {
	mem := &MemorySink{}
	old := SetSink(mem)
	defer SetSink(old)

	Emit(Event{Action: "a", Success: true})
	lines := mem.Snapshot()
	if len(lines) != 1 {
		t.Fatalf("want 1 line")
	}
	var rec Record
	_ = json.Unmarshal(lines[0][:len(lines[0])-1], &rec)
	if rec.User != "unknown" {
		t.Errorf("default user should be unknown, got %q", rec.User)
	}
}

func TestMemorySink_ThreadSafe(t *testing.T) {
	mem := &MemorySink{}
	_ = SetSink(mem)
	defer SetSink(nil)

	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func() {
			Emit(Event{Action: "x", Success: true})
			done <- struct{}{}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
	if got := len(mem.Snapshot()); got != 10 {
		t.Errorf("want 10 records, got %d", got)
	}
}
