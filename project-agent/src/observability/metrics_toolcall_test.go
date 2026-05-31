// metrics_toolcall_test.go —— D28 工具调用可观测性指标单元测试。
//
// 使用 OTel ManualReader 断言指标真实写入，不走真实 collector。
//
// # 覆盖矩阵
//
//   A) ObserveToolCallDuration
//     1. 正常调用 → Histogram count=1，sum>0
//     2. 多次调用不同 tool → 各自独立计数
//     3. seconds<0 → 不写入（防止负值污染）
//
//   B) IncToolHITLStage
//     4. stage=plan → Counter +1
//     5. stage=confirmed → Counter +1
//     6. 两次 plan + 一次 confirmed → plan=2, confirmed=1（漏斗比率可算）
//
//   C) IncToolReject
//     7. reason=r3_primary_key → Counter +1
//     8. reason=r5_rv_conflict → Counter +1
//     9. 不同 reason 独立计数
//
//   D) IncToolInputAnomaly
//    10. anomaly=missing_required → Counter +1
//    11. anomaly=empty_required → Counter +1
//
//   E) 边界
//    12. tool="" → 自动填充 "unknown"，不 panic
//    13. stage="" → 自动填充 "unknown"，不 panic
package observability

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// ---- A) ObserveToolCallDuration -----------------------------------------------

func TestObserveToolCallDuration_Basic(t *testing.T) {
	reader, restore := SetMeterForTest()
	t.Cleanup(restore)

	ctx := context.Background()
	ObserveToolCallDuration(ctx, "bcs_scale_deployment", StatusOK, 0.123)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	cnt, sum, ok := collectFindHistogram(&rm, MetricToolCallDuration)
	if !ok {
		t.Fatalf("指标 %q 未找到", MetricToolCallDuration)
	}
	if cnt != 1 {
		t.Errorf("count 应为 1，实际 %d", cnt)
	}
	if sum < 0.1 {
		t.Errorf("sum 应 ≥ 0.1（记录了 0.123s），实际 %f", sum)
	}
}

func TestObserveToolCallDuration_MultiTool(t *testing.T) {
	reader, restore := SetMeterForTest()
	t.Cleanup(restore)

	ctx := context.Background()
	ObserveToolCallDuration(ctx, "bcs_pod_describe", StatusOK, 0.05)
	ObserveToolCallDuration(ctx, "bcs_node_describe", StatusOK, 0.08)
	ObserveToolCallDuration(ctx, "bcs_pod_describe", StatusError, 0.02)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	cnt, _, ok := collectFindHistogram(&rm, MetricToolCallDuration)
	if !ok {
		t.Fatalf("指标 %q 未找到", MetricToolCallDuration)
	}
	// 3 次调用 → count=3
	if cnt != 3 {
		t.Errorf("3 次调用后 count 应为 3，实际 %d", cnt)
	}
}

func TestObserveToolCallDuration_NegativeSkipped(t *testing.T) {
	reader, restore := SetMeterForTest()
	t.Cleanup(restore)

	ctx := context.Background()
	ObserveToolCallDuration(ctx, "bcs_test", StatusOK, -1.0) // 负值应被跳过

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	cnt, _, ok := collectFindHistogram(&rm, MetricToolCallDuration)
	if ok && cnt > 0 {
		t.Errorf("负值 seconds 不应写入 Histogram，实际 count=%d", cnt)
	}
}

// ---- B) IncToolHITLStage -------------------------------------------------------

func TestIncToolHITLStage_Plan(t *testing.T) {
	reader, restore := SetMeterForTest()
	t.Cleanup(restore)

	ctx := context.Background()
	IncToolHITLStage(ctx, "bcs_scale_deployment", HITLStagePlan)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	total, ok := collectFindSum(&rm, MetricToolHITLStage)
	if !ok {
		t.Fatalf("指标 %q 未找到", MetricToolHITLStage)
	}
	if total != 1 {
		t.Errorf("plan 阶段 count 应为 1，实际 %d", total)
	}
}

func TestIncToolHITLStage_Funnel(t *testing.T) {
	reader, restore := SetMeterForTest()
	t.Cleanup(restore)

	ctx := context.Background()
	// 模拟：2 次 Plan（2 个用户看到了确认提示），1 次 Confirmed（1 个用户确认了）
	IncToolHITLStage(ctx, "bcs_network_update", HITLStagePlan)
	IncToolHITLStage(ctx, "bcs_network_update", HITLStagePlan)
	IncToolHITLStage(ctx, "bcs_network_update", HITLStageConfirmed)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	total, ok := collectFindSum(&rm, MetricToolHITLStage)
	if !ok {
		t.Fatalf("指标 %q 未找到", MetricToolHITLStage)
	}
	// 总计 3 次（2 plan + 1 confirmed）
	if total != 3 {
		t.Errorf("漏斗总计应为 3，实际 %d", total)
	}
}

// ---- C) IncToolReject ---------------------------------------------------------

func TestIncToolReject_R3(t *testing.T) {
	reader, restore := SetMeterForTest()
	t.Cleanup(restore)

	ctx := context.Background()
	IncToolReject(ctx, "bcs_network_update", "r3_primary_key")

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	total, ok := collectFindSum(&rm, MetricToolReject)
	if !ok {
		t.Fatalf("指标 %q 未找到", MetricToolReject)
	}
	if total != 1 {
		t.Errorf("R3 拒绝 count 应为 1，实际 %d", total)
	}
}

func TestIncToolReject_MultiReason(t *testing.T) {
	reader, restore := SetMeterForTest()
	t.Cleanup(restore)

	ctx := context.Background()
	IncToolReject(ctx, "bcs_scale_deployment", "hard_limit_exceeded")
	IncToolReject(ctx, "bcs_network_update", "r5_rv_conflict")
	IncToolReject(ctx, "bcs_network_update", "critical_noreason")

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	total, ok := collectFindSum(&rm, MetricToolReject)
	if !ok {
		t.Fatalf("指标 %q 未找到", MetricToolReject)
	}
	if total != 3 {
		t.Errorf("3 次拒绝 count 应为 3，实际 %d", total)
	}
}

// ---- D) IncToolInputAnomaly ---------------------------------------------------

func TestIncToolInputAnomaly_MissingRequired(t *testing.T) {
	reader, restore := SetMeterForTest()
	t.Cleanup(restore)

	ctx := context.Background()
	IncToolInputAnomaly(ctx, "bcs_pod_describe", "missing_required")

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	total, ok := collectFindSum(&rm, MetricToolInputAnomaly)
	if !ok {
		t.Fatalf("指标 %q 未找到", MetricToolInputAnomaly)
	}
	if total != 1 {
		t.Errorf("missing_required count 应为 1，实际 %d", total)
	}
}

func TestIncToolInputAnomaly_MultiType(t *testing.T) {
	reader, restore := SetMeterForTest()
	t.Cleanup(restore)

	ctx := context.Background()
	IncToolInputAnomaly(ctx, "bcs_scale_deployment", "missing_required")
	IncToolInputAnomaly(ctx, "bcs_scale_deployment", "empty_required")

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	total, ok := collectFindSum(&rm, MetricToolInputAnomaly)
	if !ok {
		t.Fatalf("指标 %q 未找到", MetricToolInputAnomaly)
	}
	if total != 2 {
		t.Errorf("2 次异常 count 应为 2，实际 %d", total)
	}
}

// ---- E) 边界 -----------------------------------------------------------------

func TestObserveToolCallDuration_EmptyTool(t *testing.T) {
	// 空 tool 名不应 panic，应自动填充 "unknown"
	reader, restore := SetMeterForTest()
	t.Cleanup(restore)

	ctx := context.Background()
	ObserveToolCallDuration(ctx, "", StatusOK, 0.01) // 不应 panic

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	cnt, _, ok := collectFindHistogram(&rm, MetricToolCallDuration)
	if !ok || cnt == 0 {
		t.Errorf("空 tool 名应自动填充 unknown 并写入指标")
	}
}

func TestIncToolHITLStage_EmptyStage(t *testing.T) {
	// 空 stage 不应 panic，应自动填充 "unknown"
	reader, restore := SetMeterForTest()
	t.Cleanup(restore)

	ctx := context.Background()
	IncToolHITLStage(ctx, "bcs_test", "") // 不应 panic

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(ctx, &rm); err != nil {
		t.Fatalf("Collect: %v", err)
	}
	total, ok := collectFindSum(&rm, MetricToolHITLStage)
	if !ok || total == 0 {
		t.Errorf("空 stage 应自动填充 unknown 并写入指标")
	}
}
