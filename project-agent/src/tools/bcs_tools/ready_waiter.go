// ready_waiter.go D19.5 —— Pod Ready 等待抽象与真实实现。
//
// # 存在意义
//
// D18.2 首次落地 pod_restart 时，waitPodsReady 是一个"Mock sleep 3s + 真实模式空占位"
// 的半成品——真实模式里直接返回 {"ready": false, "reason": "wait_not_implemented_in_real_mode"}。
// 这意味着 LLM 调 wait_for_ready=true 在生产里根本没等，只是返回了一个欺骗性的 OK。
// D19.5 的工作就是把这个占位换成可用的真实实现。
//
// # 核心抽象：ReadyWaiter
//
// 不把"轮询逻辑"直接写进 pod_restart.go 的理由有三：
//
//   1. **测试成本**：直接写在 tool 函数里，每个单测都要 stub time + bcsapi；
//      抽成接口后，pod_restart 的测试只需注入 fakeWaiter。
//   2. **复用性**：未来 bcs_scale_deployment.wait_for_ready / helm_upgrade.wait_ready
//      都是同类需求（轮询 deployment 状态），不能每个工具自己写一遍。
//   3. **可替换**：K8s 原生 watch API 比轮询更高效，未来切 watch 实现时
//      只需换 ReadyWaiter 实例，不动上层工具。
//
// # 语义精确性
//
// "Ready" 在不同 mode 下的判定不同，这是本文件要明确回答的：
//
//   - delete_pod(pod_names=[P1,P2]):
//       等目标 ReplicaSet 的 readyReplicas 恢复到 spec.replicas（"新 Pod 起来补齐"）
//       —— 不能盯被删的 Pod 名，它们本来就该消失
//
//   - rollout_restart(deployment=D):
//       等 Deployment.status 满足三条件：
//         observedGeneration >= metadata.generation
//         updatedReplicas == spec.replicas
//         readyReplicas == spec.replicas
//       —— 这是"滚动重启完成"的标准判据
//
//   - evict_pod: 同 delete_pod（仍由 RS 拉起新副本）
//
// # 默认参数
//
//	Interval = 2s   —— 比 K8s readiness probe 默认 periodSeconds=10 更密
//	Timeout  = 5min —— 足够大多数场景；超时不代表失败，只代表"等不动"
//	Jitter   = ±20% —— 轻量抖动避免 N 个 waiter 同步轮询打爆 BCS
//
// 这些参数都可通过构造函数注入，便于测试和 SRE 按场景调。
package bcstools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bcsapi"
)

// ReadyWaiter 把"等 Pod/Deployment 就绪"抽象成一个可测试、可替换的接口。
//
// Wait 返回：
//   - ready=true, err=nil  —— 在 timeout 内达到就绪
//   - ready=false, err=ctx.Err() —— 被 ctx 取消
//   - ready=false, err=context.DeadlineExceeded —— 自带 timeout 到期未就绪
//   - ready=false, err=<其它>      —— 不可恢复错误（认证失败/资源不存在）
type ReadyWaiter interface {
	Wait(ctx context.Context, spec ReadySpec) (ready bool, err error)
}

// ReadySpec 描述一次"等待就绪"的完整条件。
//
// 为什么用一个结构体而不是多参数：字段会增长（未来可能有 LabelSelector/MinPodAge 等），
// 结构体让 API 向后兼容，调用方只填关心的字段。
type ReadySpec struct {
	// Mode 命名约定（便于 observability 面板按调用方分桶）：
	//   - "delete_pod" / "rollout_restart" / "evict_pod"  —— D19.5 pod_restart
	//   - "scale_deployment"                              —— D19.6 scale
	//   - "helm_rollback" / "helm_install"                —— D19.7 helm_manage
	// 新接入方请沿用 "<tool>_<action>" 格式，方便指标下钻。
	Mode       string
	ClusterID  string
	Namespace  string
	Deployment string // rollout_restart / scale_deployment / helm_* 必填
	// ReplicaSetName 可选；若未提供将在首轮查询时通过 Deployment 关系解析
	// （目前简化实现直接盯 Deployment 级 readyReplicas）
	// 保留字段供未来扩展
	ReplicaSetName string

	// PodNames 用于 delete_pod / evict_pod：虽然被删的 Pod 会消失，
	// 但我们实际盯的是它们所属 Deployment 的 readyReplicas（在首轮调用时通过 ownerRef 解析）。
	// 首期实现：要求调用方同时提供 Deployment；若未提供则只做 timeout 保护后放行。
	PodNames []string

	// Interval / Timeout 零值使用 Config 默认；非零值覆盖。
	Interval time.Duration
	Timeout  time.Duration
}

// WaiterConfig 注入型默认配置。nil Waiter.Clock 与 Random 在测试中可被替换。
type WaiterConfig struct {
	DefaultInterval time.Duration // 默认 2s
	DefaultTimeout  time.Duration // 默认 5 分钟
	JitterRatio     float64       // [0, 1)，默认 0.2 表示 ±20%

	// Clock / Sleep 可在测试中替换；生产留空走真实 time.
	NowFunc   func() time.Time                // 默认 time.Now
	SleepFunc func(ctx context.Context, d time.Duration) error // 默认 ctx-aware sleep
}

// 默认值。改这两个常量须同步改 system_prompt.md 中对 wait_for_ready 预期耗时的说明。
const (
	defaultWaiterInterval = 2 * time.Second
	defaultWaiterTimeout  = 5 * time.Minute
	defaultWaiterJitter   = 0.2
)

// applyDefaults 把零值字段补成默认。
func (c *WaiterConfig) applyDefaults() {
	if c.DefaultInterval <= 0 {
		c.DefaultInterval = defaultWaiterInterval
	}
	if c.DefaultTimeout <= 0 {
		c.DefaultTimeout = defaultWaiterTimeout
	}
	if c.JitterRatio < 0 || c.JitterRatio >= 1 {
		c.JitterRatio = defaultWaiterJitter
	}
	if c.NowFunc == nil {
		c.NowFunc = time.Now
	}
	if c.SleepFunc == nil {
		c.SleepFunc = ctxSleep
	}
}

// ctxSleep 是默认的 ctx-aware 休眠：遇 ctx 取消立刻返回。
func ctxSleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// bcsReadyWaiter 是 ReadyWaiter 的 BCS 实现：通过 bcsapi.Client 轮询 Deployment 状态。
//
// 注意：不暴露为 *BCSReadyWaiter — 调用方应通过 NewBCSReadyWaiter 拿 ReadyWaiter 接口，
// 这样实现细节可以自由演进（未来切到 watch、加 cache 等）。
type bcsReadyWaiter struct {
	client *bcsapi.Client
	cfg    WaiterConfig
}

// NewBCSReadyWaiter 构造 BCS 实现。client 必填；cfg 为可选配置，零值走默认。
func NewBCSReadyWaiter(client *bcsapi.Client, cfg WaiterConfig) ReadyWaiter {
	cfg.applyDefaults()
	return &bcsReadyWaiter{client: client, cfg: cfg}
}

// NewNoopReadyWaiter 始终立刻返回 ready=true。仅用于测试 / 禁用轮询场景。
func NewNoopReadyWaiter() ReadyWaiter {
	return noopWaiter{}
}

type noopWaiter struct{}

func (noopWaiter) Wait(context.Context, ReadySpec) (bool, error) { return true, nil }

// ---- D19.8：抽象可替换性装配层 ----------------------------------------------
//
// NewReadyWaiterFromEnv 根据环境变量 GAMEOPS_READY_WAITER 选择实现：
//
//	未设置 / "fast"  → FastPollReadyWaiter（D19.8 默认，感知延迟更低）
//	"poll"           → bcsReadyWaiter（D19.5 传统实现，作为回滚逃生通道）
//	"noop"           → noopWaiter（紧急关停，wait_for_ready 永远立刻 ready）
//
// 设计要点：
//
//  1. **上层三个工具（pod_restart/scale/helm）调用的还是 NewBCSReadyWaiter**，
//     但可以在 app 装配层改用 NewReadyWaiterFromEnv —— 这正是 D19.8 要验证的
//     "抽象可替换性"：接口不动、上层工具代码不动、测试不改，底层实现任意切换。
//
//  2. **hook 注入**：observability 通过装配层注入 FastPollMetricsHook，
//     让我们能观测新实现的效果（首探命中率等）。Hook=nil 也安全工作。
//
//  3. **逃生通道**：线上若 FastPoll 出问题，SRE 一个环境变量秒切回 poll，
//     无需重新编译——这是可替换性在运维侧的直接价值。
func NewReadyWaiterFromEnv(client *bcsapi.Client, cfg WaiterConfig, hook FastPollMetricsHook) ReadyWaiter {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("GAMEOPS_READY_WAITER")))
	switch mode {
	case "noop":
		return NewNoopReadyWaiter()
	case "poll":
		return NewBCSReadyWaiter(client, cfg)
	case "", "fast":
		return NewFastPollReadyWaiter(client, cfg, hook)
	default:
		// 未知值：不静默失败，也不 panic；回退到最稳妥的 poll 实现并依赖日志告警
		// （app 装配层会在启动日志里打印最终选定的实现，运维可自查）
		return NewBCSReadyWaiter(client, cfg)
	}
}

// SelectedWaiterKind 返回当前环境变量下会选中的实现标识，供 app 装配层日志打印使用。
// 仅返回 "fast" / "poll" / "noop" / "unknown" 四个值，稳定可观测。
func SelectedWaiterKind() string {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("GAMEOPS_READY_WAITER")))
	switch mode {
	case "":
		return "fast"
	case "fast", "poll", "noop":
		return mode
	default:
		return "unknown"
	}
}

// Wait 实现核心轮询循环。
//
// 控制流程：
//  1. 解析 interval/timeout（spec 优先于 cfg 默认）
//  2. Mock 模式：sleep 一小段模拟等待返回 true（保持 D18.2 行为，兼容既有测试）
//  3. 真实模式循环：
//     a. 查询 Deployment 状态
//     b. 判定 isReadyByMode
//     c. 就绪即返；未就绪 sleep(interval + jitter) 再查
//  4. 任一环节遇 ctx.Done/Deadline 立即返回 (false, err)
//
// 为什么不抛 panic 或 fmt.Errorf 带满上下文：err 会被 pod_restart 层上报给 LLM，
// 信息过多会污染对话；这里保持简洁，详细诊断靠 observability 侧的指标+日志。
func (w *bcsReadyWaiter) Wait(ctx context.Context, spec ReadySpec) (bool, error) {
	interval := spec.Interval
	if interval <= 0 {
		interval = w.cfg.DefaultInterval
	}
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = w.cfg.DefaultTimeout
	}

	// Mock 模式短路：保留 D18.2 的行为（很多单测依赖 client.IsMock()=true 快速返回）
	if w.client.IsMock() {
		// sleep 少量时间让测试能观察到 "有等待发生"；但不真的 5 分钟
		if err := w.cfg.SleepFunc(ctx, 50*time.Millisecond); err != nil {
			return false, err
		}
		return true, nil
	}

	// rollout_restart 才能盯 Deployment；delete_pod/evict_pod 要求 spec.Deployment 必须指定，
	// 否则我们拿不到"要恢复到多少副本"的基准
	if spec.Deployment == "" {
		return false, fmt.Errorf("ready_waiter: spec.Deployment 必填（delete/evict 需传入 Pod 所属 Deployment 名以便判断 readyReplicas）")
	}

	deadline := w.cfg.NowFunc().Add(timeout)

	// 先做一次立即检查，避免一开始先 sleep（快路径）
	for {
		ready, err := w.probeOnce(ctx, spec)
		if err != nil {
			// probeOnce 内部把 ctx 错误原样抛出
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return false, err
			}
			// 其他临时错误（网络抖动等）不立即失败，继续重试直到 timeout
			// 这里故意不区分类型：保持"尽最大努力等"语义
		}
		if ready {
			return true, nil
		}

		// 是否还有时间继续
		now := w.cfg.NowFunc()
		if !now.Before(deadline) {
			return false, context.DeadlineExceeded
		}

		// sleep 一个 jittered interval；抖动避免 N 个 waiter 同频打 BCS
		wait := withJitter(interval, w.cfg.JitterRatio)
		if remaining := deadline.Sub(now); wait > remaining {
			wait = remaining // 不要睡过 deadline
		}
		if err := w.cfg.SleepFunc(ctx, wait); err != nil {
			return false, err
		}
	}
}

// withJitter 给 base 叠加 ±ratio 的随机扰动。ratio<=0 时原样返回。
func withJitter(base time.Duration, ratio float64) time.Duration {
	if ratio <= 0 {
		return base
	}
	// rand.Float64 返回 [0,1)；平移到 [-ratio, ratio)
	delta := (rand.Float64()*2 - 1) * ratio
	jitter := time.Duration(float64(base) * delta)
	result := base + jitter
	if result < 0 {
		return 0
	}
	return result
}

// probeOnce 发起一次状态查询并判定是否就绪。
// 返回 (ready, err)：
//   - err==context.Canceled / DeadlineExceeded 应立即中止循环
//   - err 为其他错误时当"本次查询失败"处理，Wait 循环会继续重试
func (w *bcsReadyWaiter) probeOnce(ctx context.Context, spec ReadySpec) (bool, error) {
	// 通过 BCS 存储层接口查 Deployment 动态对象
	path := fmt.Sprintf(
		"/bcsapi/v4/storage/k8s/dynamic/clusters/%s/namespaces/%s/deployments/%s",
		spec.ClusterID, spec.Namespace, spec.Deployment,
	)
	var resp map[string]any
	if err := w.client.Get(ctx, path, nil, &resp); err != nil {
		return false, err
	}
	return isDeploymentReady(resp), nil
}

// isDeploymentReady 从 BCS 动态对象响应里判断 Deployment 是否滚动完成。
//
// BCS 存储返回结构（简化）：
//
//	{
//	  "data": [{
//	    "data": {  // 真正的 k8s object
//	      "metadata": {"generation": 3, ...},
//	      "spec":     {"replicas": 3},
//	      "status": {
//	        "observedGeneration": 3,
//	        "replicas": 3,
//	        "updatedReplicas": 3,
//	        "readyReplicas": 3,
//	        "availableReplicas": 3
//	      }
//	    }
//	  }]
//	}
//
// 就绪判据（Kubernetes 官方 kubectl rollout status 同款）：
//   observedGeneration >= spec.generation
//   updatedReplicas >= spec.replicas
//   readyReplicas  >= spec.replicas
//
// 三条件"全满足"才算就绪；只要有一个不满足就继续等。
//
// 单独成函数方便单测直接喂 map 进来验证——避免为判据逻辑搭一个假 bcsapi。
func isDeploymentReady(resp map[string]any) bool {
	obj := extractDeploymentObject(resp)
	if obj == nil {
		return false // 对象不存在 = 未就绪（可能正在创建）
	}
	metadata, _ := obj["metadata"].(map[string]any)
	spec, _ := obj["spec"].(map[string]any)
	status, _ := obj["status"].(map[string]any)
	if metadata == nil || spec == nil || status == nil {
		return false
	}
	generation := numberField(metadata, "generation")
	observedGen := numberField(status, "observedGeneration")
	desired := numberField(spec, "replicas")
	updated := numberField(status, "updatedReplicas")
	ready := numberField(status, "readyReplicas")

	// 三条件同时成立
	return observedGen >= generation &&
		updated >= desired &&
		ready >= desired
}

// extractDeploymentObject 从 BCS 存储的 data[0].data 结构里取真实 K8s object。
// 容忍两种响应形态：直接是 object，或者被 BCS 包了 data 数组。
func extractDeploymentObject(resp map[string]any) map[string]any {
	if resp == nil {
		return nil
	}
	// 形态 1：直接就是 k8s object
	if _, hasMeta := resp["metadata"]; hasMeta {
		return resp
	}
	// 形态 2：BCS 包装了一层
	arr, ok := resp["data"].([]any)
	if !ok || len(arr) == 0 {
		// 有些场景 data 是 object 不是数组
		if obj, ok := resp["data"].(map[string]any); ok {
			// 再解一层：data.data 是 k8s object
			if inner, ok := obj["data"].(map[string]any); ok {
				return inner
			}
			return obj
		}
		return nil
	}
	first, _ := arr[0].(map[string]any)
	if first == nil {
		return nil
	}
	if inner, ok := first["data"].(map[string]any); ok {
		return inner
	}
	return first
}

// numberField 从 map 里取数值字段，支持 float64/int/json.Number。
// BCS 走 JSON 解码默认是 float64；保留其他类型以防客户端换成 json.Number 解析。
func numberField(m map[string]any, key string) int64 {
	v, ok := m[key]
	if !ok || v == nil {
		return 0
	}
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int64:
		return x
	case int:
		return int64(x)
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return n
		}
	}
	return 0
}
