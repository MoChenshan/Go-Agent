// scale_hpa_test.go D20 —— scale 与 HPA 感知的端到端测试。
//
// # 为什么与 scale_test.go 分离
//
//   scale_test.go 的辅助函数 newMockScaleClient 走 Mock 模式，永远返回 HPA Found=false。
//   D20 的核心验证是"检测到真实 HPA 时三档策略表现"，Mock 模式无法覆盖。
//
// 本文件走 httptest.Server 真路径：
//   - fakeBCSRouter 让服务器根据请求路径分流：deployment spec + HPA 列表 + scale 写
//   - 用真实 bcsapi.Client 指向 server，走完整 HTTP + HPA 检测 + 策略分支
//   - 使用 WithBaseURL(srv.URL) + WithToken 组合让 client.IsMock()=false
//
// # 测试矩阵
//
//   A. 无 HPA：三档策略等价于 D19 行为（未 confirmed 返 Plan，无 HPA 字段）
//   B. 有 HPA 且 target ∈ [min,max]：Plan 含 HPA 信息但无冲突，放行
//   C. 有 HPA 且 target ∉ [min,max]：
//      - warn（默认）     → Plan 里 SideEffect 开头带 ⚠；Severity 升到 High+
//      - block             → 直接返回 OK=false，writeHits=0（证明不下发）
//      - force 缺 reason   → 被 R1 拦截（Severity=Critical+requireReason）
//      - force 带 reason   → 通过，审计含 hpa_bypass=true
package bcstools

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"trpc.group/trpc-go/trpc-agent-go/tool"

	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bcsapi"
	"git.woa.com/trpc-go/gameops-agent/src/tools/hitl"
)

// ---- Fake BCS Server -------------------------------------------------------

// deploymentSpecResp 返回 scale.fetchCurrentReplicas 期望的 storage 响应。
func deploymentSpecResp(current int) string {
	return fmt.Sprintf(`{"data":[{"data":{"metadata":{"generation":1},"spec":{"replicas":%d}}}]}`, current)
}

// emptyHPAListResp 无 HPA 的返回。
func emptyHPAListResp() string { return `{"data":[]}` }

// oneHPAListResp 返回匹配到单个 HPA 的响应。
func oneHPAListResp(name, targetDeploy string, min, max int) string {
	return fmt.Sprintf(
		`{"data":[{"data":{"metadata":{"name":"%s"},"spec":{"minReplicas":%d,"maxReplicas":%d,"scaleTargetRef":{"kind":"Deployment","name":"%s"}},"status":{"desiredReplicas":%d}}}]}`,
		name, min, max, targetDeploy, min,
	)
}

// fakeBCSRouter 按 HTTP 路径分流，模拟 BCS bcs-storage 的多资源代理。
func fakeBCSRouter(
	t *testing.T,
	currentReplicas int,
	hpaResp string,
	writeHits *atomic.Int32,
) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/horizontalpodautoscaler"):
			_, _ = fmt.Fprint(w, hpaResp)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/deployment"):
			_, _ = fmt.Fprint(w, deploymentSpecResp(currentReplicas))
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/scale"):
			if writeHits != nil {
				writeHits.Add(1)
			}
			_, _ = fmt.Fprint(w, `{}`)
		default:
			// 兜底防 nil dereference
			_, _ = fmt.Fprint(w, `{"data":[]}`)
		}
	}))
}

// newRealScaleTool 指向 fake BCS Server 构造真实 scale tool（非 Mock 模式）。
// 使用 Noop Waiter 避免 wait_for_ready 的副作用（D19.5 已覆盖其行为）。
func newRealScaleTool(baseURL string) tool.Tool {
	client := bcsapi.NewClient(
		bcsapi.WithBaseURL(baseURL),
		bcsapi.WithToken("test-token"),
	)
	return newScaleToolWithWaiter(client, NewNoopReadyWaiter())
}

// ---- A. 无 HPA：三档策略都应放行 --------------------------------------------

// 未 confirmed：返回 Plan；confirmed=true：真正下发
func TestScaleHPA_NoHPA_DefaultPolicyGoesThrough(t *testing.T) {
	var writeHits atomic.Int32
	srv := fakeBCSRouter(t, 3, emptyHPAListResp(), &writeHits)
	defer srv.Close()
	tl := newRealScaleTool(srv.URL)

	// 未 confirmed：返回 Plan
	r := mustCallScale(t, tl, ScaleInput{
		Action: "scale", ClusterID: "c", Namespace: "ns", Deployment: "d",
		Replicas: 5,
	})
	if r.OK {
		t.Fatal("未 confirmed 应返回 OK=false（pending Plan）")
	}
	if writeHits.Load() != 0 {
		t.Errorf("未 confirmed 不应触发写，实际 %d 次", writeHits.Load())
	}
	// Plan 里不应有 hpa 段（证明 Plan 渲染对无 HPA 情况的沉默行为）
	if p, ok := r.Data.(hitl.PendingResult); ok {
		if _, has := p.Plan.Params["hpa"]; has {
			t.Error("无 HPA 时 Plan.Params 不应出现 hpa 字段")
		}
	}

	// confirmed=true：真正下发
	r2 := mustCallScale(t, tl, ScaleInput{
		Action: "scale", ClusterID: "c", Namespace: "ns", Deployment: "d",
		Replicas: 5, Confirmed: true,
	})
	if !r2.OK {
		t.Fatalf("confirmed 后应成功，实际 msg=%s", r2.Message)
	}
	if writeHits.Load() != 1 {
		t.Errorf("confirmed 后应触发 1 次写，实际 %d 次", writeHits.Load())
	}
}

// ---- B. 有 HPA 且 target 在区间内：放行，Plan 含 HPA 摘要 --------------------

func TestScaleHPA_HasHPA_InRange_Allowed(t *testing.T) {
	var writeHits atomic.Int32
	// HPA 区间 [3, 10]，目标 6 在区间内
	srv := fakeBCSRouter(t, 3, oneHPAListResp("hpa-core", "d", 3, 10), &writeHits)
	defer srv.Close()
	tl := newRealScaleTool(srv.URL)

	r := mustCallScale(t, tl, ScaleInput{
		Action: "scale", ClusterID: "c", Namespace: "ns", Deployment: "d",
		Replicas: 6, Confirmed: true,
		HPAPolicy: "warn",
	})
	if !r.OK {
		t.Fatalf("区间内应放行，实际 msg=%s", r.Message)
	}
	if writeHits.Load() != 1 {
		t.Errorf("应真实下发 1 次，实际 %d 次", writeHits.Load())
	}
}

// ---- C. 区间外：warn 默认告警但放行 -----------------------------------------

func TestScaleHPA_OutOfRange_WarnShowsInPlan(t *testing.T) {
	var writeHits atomic.Int32
	// HPA 区间 [3, 10]，目标 20 超上界
	srv := fakeBCSRouter(t, 3, oneHPAListResp("hpa-core", "d", 3, 10), &writeHits)
	defer srv.Close()
	tl := newRealScaleTool(srv.URL)

	// 未 confirmed：Plan 里应含 HPA 警告
	r := mustCallScale(t, tl, ScaleInput{
		Action: "scale", ClusterID: "c", Namespace: "ns", Deployment: "d",
		Replicas: 20,
		// HPAPolicy 留空，验证默认 = warn
	})
	if r.OK {
		t.Fatal("未 confirmed 应返回 pending Plan")
	}
	pending, ok := r.Data.(hitl.PendingResult)
	if !ok {
		t.Fatalf("Data 应为 PendingResult，实际 %T", r.Data)
	}
	// Severity 应被 HPA 冲突升级到 High 或以上
	if pending.Plan.Severity == hitl.SeverityMedium {
		t.Errorf("warn+HPA 冲突应升 Severity 到 High 以上，实际 %q", pending.Plan.Severity)
	}
	// SideEffect 里应出现 HPA 提示
	if !strings.Contains(pending.Plan.SideEffect, "HPA") {
		t.Errorf("warn 模式 SideEffect 应提示 HPA 冲突，实际 %q", pending.Plan.SideEffect)
	}
	// Params 里应含 hpa.in_range=false 和 hpa_policy=warn
	hpaParam, _ := pending.Plan.Params["hpa"].(map[string]any)
	if hpaParam == nil || hpaParam["in_range"] != false {
		t.Errorf("Plan.Params.hpa.in_range 应为 false，实际 %+v", hpaParam)
	}
	if pending.Plan.Params["hpa_policy"] != "warn" {
		t.Errorf("hpa_policy 应为 warn，实际 %v", pending.Plan.Params["hpa_policy"])
	}

	// confirmed=true：warn 模式应放行真实下发
	r2 := mustCallScale(t, tl, ScaleInput{
		Action: "scale", ClusterID: "c", Namespace: "ns", Deployment: "d",
		Replicas: 20, Confirmed: true, HPAPolicy: "warn",
	})
	if !r2.OK {
		t.Fatalf("warn+confirmed 应放行，实际 msg=%s", r2.Message)
	}
	if writeHits.Load() != 1 {
		t.Errorf("warn+confirmed 应下发 1 次写，实际 %d", writeHits.Load())
	}
}

// ---- C2. 区间外：block 硬拒绝（HITL 不可豁免）--------------------------------

func TestScaleHPA_OutOfRange_BlockHardReject(t *testing.T) {
	var writeHits atomic.Int32
	srv := fakeBCSRouter(t, 3, oneHPAListResp("hpa-core", "d", 3, 10), &writeHits)
	defer srv.Close()
	tl := newRealScaleTool(srv.URL)

	// 即使 confirmed=true，block 模式也应硬拒绝
	r := mustCallScale(t, tl, ScaleInput{
		Action: "scale", ClusterID: "c", Namespace: "ns", Deployment: "d",
		Replicas: 20, Confirmed: true, HPAPolicy: "block",
	})
	if r.OK {
		t.Fatal("block 模式超区间应 OK=false")
	}
	if !strings.Contains(r.Message, "HPA") {
		t.Errorf("block 拒绝消息应提示 HPA，实际 %q", r.Message)
	}
	if writeHits.Load() != 0 {
		t.Errorf("block 应零写，实际 %d 次", writeHits.Load())
	}
}

// ---- C3. 区间外 + force 缺 reason：被 R1 拦截 --------------------------------

func TestScaleHPA_OutOfRange_ForceMissingReasonBlocked(t *testing.T) {
	var writeHits atomic.Int32
	srv := fakeBCSRouter(t, 3, oneHPAListResp("hpa-core", "d", 3, 10), &writeHits)
	defer srv.Close()
	tl := newRealScaleTool(srv.URL)

	// force 强制升 Critical+requireReason；confirmed=true 但无 reason
	r := mustCallScale(t, tl, ScaleInput{
		Action: "scale", ClusterID: "c", Namespace: "ns", Deployment: "d",
		Replicas: 20, Confirmed: true, HPAPolicy: "force",
		// 故意不填 Reason
	})
	if r.OK {
		t.Fatal("force 缺 reason 应被 R1 拦截 OK=false")
	}
	if !strings.Contains(r.Message, "reason") {
		t.Errorf("消息应提示缺 reason，实际 %q", r.Message)
	}
	if writeHits.Load() != 0 {
		t.Errorf("R1 拦截不应下发写，实际 %d 次", writeHits.Load())
	}
}

// ---- C4. 区间外 + force + reason：通过（明知故犯）---------------------------

func TestScaleHPA_OutOfRange_ForceWithReasonPasses(t *testing.T) {
	var writeHits atomic.Int32
	srv := fakeBCSRouter(t, 3, oneHPAListResp("hpa-core", "d", 3, 10), &writeHits)
	defer srv.Close()
	tl := newRealScaleTool(srv.URL)

	r := mustCallScale(t, tl, ScaleInput{
		Action: "scale", ClusterID: "c", Namespace: "ns", Deployment: "d",
		Replicas: 20, Confirmed: true,
		HPAPolicy: "force",
		Reason:    "P0 故障紧急扩容，事后调整 HPA max",
	})
	if !r.OK {
		t.Fatalf("force+reason 应通过，实际 msg=%s", r.Message)
	}
	if writeHits.Load() != 1 {
		t.Errorf("应真实下发 1 次，实际 %d 次", writeHits.Load())
	}
}

// ---- D. force 模式需要先走 HITL：未 confirmed 时 Plan 里 Severity=Critical ---

func TestScaleHPA_Force_HITLShowsCritical(t *testing.T) {
	var writeHits atomic.Int32
	srv := fakeBCSRouter(t, 3, oneHPAListResp("hpa-core", "d", 3, 10), &writeHits)
	defer srv.Close()
	tl := newRealScaleTool(srv.URL)

	r := mustCallScale(t, tl, ScaleInput{
		Action: "scale", ClusterID: "c", Namespace: "ns", Deployment: "d",
		Replicas: 20,
		HPAPolicy: "force",
		// Confirmed=false：应返回 Plan
	})
	if r.OK {
		t.Fatal("未 confirmed 应返回 pending Plan")
	}
	pending, ok := r.Data.(hitl.PendingResult)
	if !ok {
		t.Fatalf("Data 应为 PendingResult，实际 %T", r.Data)
	}
	if pending.Plan.Severity != hitl.SeverityCritical {
		t.Errorf("force+HPA 冲突 Severity 应为 Critical，实际 %q", pending.Plan.Severity)
	}
	if !pending.Plan.RequireReason {
		t.Error("force+HPA 冲突应强制 RequireReason=true")
	}
	if writeHits.Load() != 0 {
		t.Errorf("未 confirmed 不应下发，实际 %d 次", writeHits.Load())
	}
}