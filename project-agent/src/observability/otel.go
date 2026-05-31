// Package observability 提供 GameOps Agent 的 OpenTelemetry 可观测性基建。
//
// 职责：
//   - Init：按环境变量按需初始化 Tracer/Meter Provider
//   - Shutdown：进程退出时 flush & close
//   - 对外暴露 Tracer() / Meter() 给业务侧埋点使用
//
// 环境变量（全部可选；未配置时走 Noop，不影响零依赖本地运行）：
//
//	OTEL_ENABLED                 true/false（默认 false）
//	OTEL_EXPORTER_OTLP_ENDPOINT  gRPC/HTTP endpoint（如 http://localhost:4318）
//	OTEL_EXPORTER_OTLP_PROTOCOL  grpc / http/protobuf（默认 http/protobuf）
//	OTEL_SERVICE_NAME            服务名（默认 gameops-agent）
//	OTEL_SERVICE_VERSION         服务版本（默认 dev）
//	OTEL_DEPLOYMENT_ENVIRONMENT  部署环境（默认 local）
//    OTEL_TRACES_SAMPLER          always_on / always_off / traceidratio
//                                 / parentbased_always_on / parentbased_traceidratio
//                                 （默认 parentbased_always_on）
//    OTEL_TRACES_SAMPLER_ARG      采样比（0.0～1.0），仅对 ratio 系采样器生效
//    OTEL_METRICS_DISABLED        1/true/on 时强制关闭 metric 侧导出（紧急止血开关，D19.1 新增）
//    OTEL_METRICS_INTERVAL        metric 采集/推送间隔，例如 15s / 30s / 1m（默认 15s，与 Prometheus 对齐）
//    OTEL_EXPORTER_OTLP_METRICS_ENDPOINT  仅 metric 的独立 endpoint（可选，留空则复用 OTEL_EXPORTER_OTLP_ENDPOINT）
//    OTEL_EXPORTER_OTLP_METRICS_PROTOCOL  仅 metric 的独立 protocol（可选，留空则复用 OTEL_EXPORTER_OTLP_PROTOCOL）
//
// 设计取舍：
//   - 默认 Noop：保证 `go run .` 本地即可跑，不强依赖 Collector
//   - Provider 对外封装：业务侧只 `observability.Tracer()`，不感知底层实现
//   - Semantic Conv：遵循 OTel GenAI Semantic Conventions v1.30
//     （span 命名 / 属性键统一在 genai_span.go 中落地）
//   - 观测故障不反压业务：metric exporter 初始化失败时自动降级 noop + 日志 warn，
//     避免因 Collector 宕机拖垮 Agent（D19.1 Graceful Degrade 原则）
package observability

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// InstrumentationName 本项目 Tracer/Meter 名称，便于在后端按 scope 过滤。
const InstrumentationName = "git.woa.com/trpc-go/gameops-agent"

// InstrumentationVersion 版本号；随 D 阶段手动打 tag，不做自动注入。
const InstrumentationVersion = "0.16.0"

// Config Init 入参；全部可选，零值走环境变量 / 默认值。
type Config struct {
	// Enabled 强制启用（覆盖 OTEL_ENABLED 环境变量）。
	Enabled *bool
	// ServiceName / Version / Env 同名环境变量的覆盖。
	ServiceName    string
	ServiceVersion string
	Environment    string
	// Endpoint OTLP exporter endpoint；空则读 OTEL_EXPORTER_OTLP_ENDPOINT。
	Endpoint string
	// Protocol grpc / http/protobuf；空则读 OTEL_EXPORTER_OTLP_PROTOCOL。
	Protocol string
	// Logger 初始化日志；nil 时静默。
	Logger func(format string, args ...any)
}

// Provider 持有 Tracer/Meter 句柄及关闭回调，便于 App 层集中管理生命周期。
type Provider struct {
	tracer   trace.Tracer
	meter    metric.Meter
	shutdown func(context.Context) error
	enabled  bool
}

var (
	globalMu  sync.RWMutex
	globalPrv = newNoopProvider()
)

// Init 初始化全局 Provider。多次调用时以最后一次为准（后者覆盖前者）。
//
// 未开启 OTel 时返回 Noop Provider，同样可以正常 Start Span / Add Counter，
// 调用方无需任何 nil 判空。
func Init(ctx context.Context, cfg Config) (*Provider, error) {
	enabled := readEnabled(cfg)
	logf := cfg.Logger
	if logf == nil {
		logf = func(string, ...any) {}
	}

	if !enabled {
		logf("[observability] OTel disabled (set OTEL_ENABLED=true to enable)")
		p := newNoopProvider()
		setGlobal(p)
		return p, nil
	}

	res, err := buildResource(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("build otel resource: %w", err)
	}

	tp, tpShutdown, err := buildTracerProvider(ctx, cfg, res)
	if err != nil {
		return nil, fmt.Errorf("build tracer provider: %w", err)
	}
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))

	// D19.1：Metric 侧从 D16 的 Noop 升级为真实 OTLP Exporter。
	// 未配置 endpoint 或显式 disabled 时仍回落到 Noop，保持本地零依赖可运行；
	// 真实 Exporter 构造失败时不让整个 Init 失败 —— 观测系统坏了，业务必须继续跑。
	mp, mpShutdown, mpErr := buildMeterProvider(ctx, cfg, res)
	if mpErr != nil {
		logf("[observability] OTel metric exporter init failed, fall back to noop: %v", mpErr)
		mp, mpShutdown = fallbackNoopMeterProvider()
	}
	otel.SetMeterProvider(mp)

	shutdown := composeShutdown(tpShutdown, mpShutdown)
	p := &Provider{
		tracer:   tp.Tracer(InstrumentationName, trace.WithInstrumentationVersion(InstrumentationVersion)),
		meter:    mp.Meter(InstrumentationName, metric.WithInstrumentationVersion(InstrumentationVersion)),
		shutdown: shutdown,
		enabled:  true,
	}
	setGlobal(p)
	sName, sRatio, sFallback := describeSampler()
	logf("[observability] OTel enabled: endpoint=%s protocol=%s service=%s sampler=%s ratio=%s fallback=%t metric=%s interval=%s",
		resolveEndpoint(cfg), resolveProtocol(cfg), resolveServiceName(cfg),
		sName, sRatio, sFallback, describeMetricBackend(cfg), resolveMetricInterval().String())
	return p, nil
}

// Shutdown 关闭 Provider；ctx 用于控制 flush 超时。
func (p *Provider) Shutdown(ctx context.Context) error {
	if p == nil || p.shutdown == nil {
		return nil
	}
	return p.shutdown(ctx)
}

// Enabled 返回是否启用了真实 OTel 后端。
func (p *Provider) Enabled() bool {
	if p == nil {
		return false
	}
	return p.enabled
}

// Tracer 返回全局 Tracer（未 Init 时返回 Noop）。
func Tracer() trace.Tracer {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return globalPrv.tracer
}

// Meter 返回全局 Meter（未 Init 时返回 Noop）。
func Meter() metric.Meter {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return globalPrv.meter
}

// IsEnabled 全局是否启用。
func IsEnabled() bool {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return globalPrv.enabled
}

// ShutdownGlobal 关闭全局 Provider。
func ShutdownGlobal(ctx context.Context) error {
	globalMu.Lock()
	p := globalPrv
	globalPrv = newNoopProvider()
	globalMu.Unlock()
	return p.Shutdown(ctx)
}

// ---------------------------------------------------------------------------
// 内部辅助
// ---------------------------------------------------------------------------

func newNoopProvider() *Provider {
	return &Provider{
		tracer: tracenoop.NewTracerProvider().Tracer(InstrumentationName),
		meter:  metricnoop.NewMeterProvider().Meter(InstrumentationName),
	}
}

func setGlobal(p *Provider) {
	globalMu.Lock()
	globalPrv = p
	globalMu.Unlock()
}

func readEnabled(cfg Config) bool {
	if cfg.Enabled != nil {
		return *cfg.Enabled
	}
	v := strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_ENABLED")))
	return v == "1" || v == "true" || v == "on" || v == "yes"
}

func resolveServiceName(cfg Config) string {
	if cfg.ServiceName != "" {
		return cfg.ServiceName
	}
	if v := os.Getenv("OTEL_SERVICE_NAME"); v != "" {
		return v
	}
	return "gameops-agent"
}

func resolveServiceVersion(cfg Config) string {
	if cfg.ServiceVersion != "" {
		return cfg.ServiceVersion
	}
	if v := os.Getenv("OTEL_SERVICE_VERSION"); v != "" {
		return v
	}
	return "dev"
}

func resolveEnvironment(cfg Config) string {
	if cfg.Environment != "" {
		return cfg.Environment
	}
	if v := os.Getenv("OTEL_DEPLOYMENT_ENVIRONMENT"); v != "" {
		return v
	}
	return "local"
}

func resolveEndpoint(cfg Config) string {
	if cfg.Endpoint != "" {
		return cfg.Endpoint
	}
	return os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
}

func resolveProtocol(cfg Config) string {
	if cfg.Protocol != "" {
		return cfg.Protocol
	}
	if v := os.Getenv("OTEL_EXPORTER_OTLP_PROTOCOL"); v != "" {
		return v
	}
	return "http/protobuf"
}

func buildResource(ctx context.Context, cfg Config) (*resource.Resource, error) {
	kvs := []resource.Option{
		resource.WithAttributes(
			semconv.ServiceName(resolveServiceName(cfg)),
			semconv.ServiceVersion(resolveServiceVersion(cfg)),
			semconv.DeploymentEnvironment(resolveEnvironment(cfg)),
		),
		resource.WithHost(),
		resource.WithProcess(),
		resource.WithOS(),
	}
	return resource.New(ctx, kvs...)
}

func buildTracerProvider(ctx context.Context, cfg Config, res *resource.Resource) (*sdktrace.TracerProvider, func(context.Context) error, error) {
	endpoint := resolveEndpoint(cfg)
	proto := strings.ToLower(resolveProtocol(cfg))
	sampler := resolveSampler()

	var exp *otlptrace.Exporter
	var err error
	if endpoint == "" {
		// 未配置 endpoint 时，仍然创建 TracerProvider 但不接 exporter，
		// 采样器仍然生效：便于本地 debug 时看采样策略是否按预期生效。
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithSampler(sampler),
			sdktrace.WithResource(res),
		)
		return tp, func(ctx context.Context) error {
			return tp.Shutdown(ctx)
		}, nil
	}

	switch proto {
	case "grpc":
		exp, err = otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(stripScheme(endpoint)),
			otlptracegrpc.WithInsecure(),
		)
	default:
		exp, err = otlptracehttp.New(ctx,
			otlptracehttp.WithEndpoint(stripScheme(endpoint)),
			otlptracehttp.WithInsecure(),
		)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("new otlp trace exporter(%s): %w", proto, err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp, sdktrace.WithBatchTimeout(5*time.Second)),
		sdktrace.WithSampler(sampler),
		sdktrace.WithResource(res),
	)
	shutdown := func(ctx context.Context) error {
		// 先 flush，再关 tp；exporter 的 Shutdown 由 tp 负责。
		return tp.Shutdown(ctx)
	}
	return tp, shutdown, nil
}

// resolveSampler 根据环境变量 OTEL_TRACES_SAMPLER(_ARG) 返回采样器。
//
// 默认：parentbased_always_on（遵循上游 trace；根根 Span 全采）。
// 可选值：
//   - always_on：全采
//   - always_off：全不采
//   - traceidratio：按 ARG 比例采样（0~1）
//   - parentbased_always_on：有父跟父、无父全采
//   - parentbased_traceidratio：有父跟父、无父按 ARG 比例采样
func resolveSampler() sdktrace.Sampler {
	name := strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_TRACES_SAMPLER")))
	ratio := parseRatio(os.Getenv("OTEL_TRACES_SAMPLER_ARG"))
	switch name {
	case "always_on":
		return sdktrace.AlwaysSample()
	case "always_off":
		return sdktrace.NeverSample()
	case "traceidratio":
		return sdktrace.TraceIDRatioBased(ratio)
	case "parentbased_always_on", "":
		return sdktrace.ParentBased(sdktrace.AlwaysSample())
	case "parentbased_always_off":
		return sdktrace.ParentBased(sdktrace.NeverSample())
	case "parentbased_traceidratio":
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))
	default:
		// 未知值回落到默认策略，避免启动失败。
		return sdktrace.ParentBased(sdktrace.AlwaysSample())
	}
}

// parseRatio 解析 OTEL_TRACES_SAMPLER_ARG；非法值回落到 1.0（全采）。
func parseRatio(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 1.0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return 1.0
	}
	if v > 1 {
		return 1.0
	}
	return v
}

// describeSampler 返回用于启动日志的结构化采样器描述：
//
//   - name：规范化后的采样器名（例如 `parentbased_always_on`）；若 env 输入为未知值则返回 `parentbased_always_on`（实际生效的回落策略）。
//   - ratio：对 ratio 系返回 "0.10" 这样的字符串；对非 ratio 系返回 "n/a"。
//   - fallback：是否触发了"非法 env 输入 → 默认值"回落。包括 SAMPLER 未知值、SAMPLER_ARG 非法值两种情况。
//
// 该函数只用于日志输出，不参与真实采样决策；真实采样仍由 resolveSampler() 决定，
// 以避免启动日志逻辑与采样逻辑产生不一致。
func describeSampler() (name string, ratio string, fallback bool) {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_TRACES_SAMPLER")))
	argRaw := os.Getenv("OTEL_TRACES_SAMPLER_ARG")
	parsed := parseRatio(argRaw)

	// SAMPLER_ARG 的 fallback 检测：非空 & 非法 / 负 / >1 → fallback。
	argFallback := false
	if trimmed := strings.TrimSpace(argRaw); trimmed != "" {
		if v, err := strconv.ParseFloat(trimmed, 64); err != nil || v < 0 || v > 1 {
			argFallback = true
		}
	}

	switch raw {
	case "", "parentbased_always_on":
		return "parentbased_always_on", "n/a", argFallback
	case "always_on":
		return "always_on", "n/a", argFallback
	case "always_off":
		return "always_off", "n/a", argFallback
	case "traceidratio":
		return "traceidratio", formatRatio(parsed), argFallback
	case "parentbased_always_off":
		return "parentbased_always_off", "n/a", argFallback
	case "parentbased_traceidratio":
		return "parentbased_traceidratio", formatRatio(parsed), argFallback
	default:
		// 未知 SAMPLER 值 → 回落 parentbased_always_on；此时 fallback 一定为 true。
		return "parentbased_always_on", "n/a", true
	}
}

// formatRatio 将 float 转成简洁字符串（例 0.1 → "0.10"，1 → "1.00"）。
func formatRatio(r float64) string {
	return strconv.FormatFloat(r, 'f', 2, 64)
}

// stripScheme 去掉 http(s):// 前缀，OTLP exporter 需要 host:port 形式。
func stripScheme(endpoint string) string {
	e := strings.TrimSpace(endpoint)
	e = strings.TrimPrefix(e, "https://")
	e = strings.TrimPrefix(e, "http://")
	return strings.TrimSuffix(e, "/")
}

// =============================================================================
// D19.1：OTLP Metric Exporter 真实对接
//
// 设计与 trace 侧严格对称：双通道（grpc/http）+ endpoint 解析 + 周期导出 +
// 生命周期合并关闭。增量思想：
//
//   1) 环境变量单独可覆盖（OTEL_EXPORTER_OTLP_METRICS_ENDPOINT/_PROTOCOL），
//      未提供则复用 trace 侧通用配置；生产上通常 trace/metric 会发到同一个
//      OTel Collector，但给独立配置的"逃生通道"。
//   2) OTEL_METRICS_DISABLED=1 时强制走 noop —— 生产紧急止血开关（比如
//      Collector 雪崩反压，先停 metric 保 trace/业务）。
//   3) Exporter 构造失败不拖垮 Init：日志 warn + 降级 noop，continue on error
//      是可观测性基础设施的铁律。
//   4) PeriodicReader interval 默认 15s，与 Prometheus scrape 对齐；可通过
//      OTEL_METRICS_INTERVAL 覆盖（解析失败兜底 15s）。
// =============================================================================

// defaultMetricInterval 周期导出的默认间隔。
//
// 为何是 15s：Prometheus 远程写 / scrape 典型周期就是 15s，SRE 看板和告警
// 规则大多按这个频率估算；选用相同值让 OTel Metric 经 Collector 转 Prom 时
// 呈现的时间粒度与原生指标一致，避免"间隔不一"的面板拼接难题。
const defaultMetricInterval = 15 * time.Second

// readMetricsDisabled 读取 OTEL_METRICS_DISABLED；true/1/on/yes 均视为关闭。
//
// 单独于 OTEL_ENABLED 的开关，便于"trace 保留、metric 关停"或反之这种
// 非对称止血场景。
func readMetricsDisabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("OTEL_METRICS_DISABLED")))
	return v == "1" || v == "true" || v == "on" || v == "yes"
}

// resolveMetricEndpoint 解析 metric 专用 endpoint，留空则回退到通用 endpoint。
func resolveMetricEndpoint(cfg Config) string {
	if v := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT")); v != "" {
		return v
	}
	return resolveEndpoint(cfg)
}

// resolveMetricProtocol 解析 metric 专用 protocol，留空则回退到通用 protocol。
func resolveMetricProtocol(cfg Config) string {
	if v := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_METRICS_PROTOCOL")); v != "" {
		return v
	}
	return resolveProtocol(cfg)
}

// resolveMetricInterval 解析 OTEL_METRICS_INTERVAL；非法值回落到 15s。
//
// 接受 "30s" / "1m" / "500ms" 这类 time.Duration 标准格式。
func resolveMetricInterval() time.Duration {
	raw := strings.TrimSpace(os.Getenv("OTEL_METRICS_INTERVAL"))
	if raw == "" {
		return defaultMetricInterval
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return defaultMetricInterval
	}
	return d
}

// describeMetricBackend 用于启动日志：返回 "noop(disabled)" / "noop(no-endpoint)" /
// "grpc:host:4317" / "http:host:4318" 这类可诊断字符串。
//
// 不参与真实后端决策，仅为了让排障者一眼看出 metric 实际走到哪里。
func describeMetricBackend(cfg Config) string {
	if readMetricsDisabled() {
		return "noop(disabled)"
	}
	ep := resolveMetricEndpoint(cfg)
	if ep == "" {
		return "noop(no-endpoint)"
	}
	return strings.ToLower(resolveMetricProtocol(cfg)) + ":" + stripScheme(ep)
}

// buildMeterProvider 构建真实的 OTLP MeterProvider；与 buildTracerProvider 对称。
//
// 返回 (mp, shutdown, err)：
//   - mp：返回值始终非 nil（即使失败场景也会在上层降级为 noop）
//   - shutdown：永远非 nil；noop 情形下是 no-op 闭包
//   - err：仅在真实 Exporter 构造失败时返回
//
// 三种路径：
//  1. OTEL_METRICS_DISABLED=true 或 endpoint 为空 → noop（合法路径，不回 err）
//  2. grpc/http exporter 构造成功 → 真实 PeriodicReader + MeterProvider
//  3. grpc/http exporter 构造失败 → 返回 err；上层负责降级
func buildMeterProvider(ctx context.Context, cfg Config, res *resource.Resource) (metric.MeterProvider, func(context.Context) error, error) {
	if readMetricsDisabled() {
		mp, sd := fallbackNoopMeterProvider()
		return mp, sd, nil
	}
	endpoint := resolveMetricEndpoint(cfg)
	if endpoint == "" {
		// 与 trace 侧对称：未配置 endpoint 不算错误，保持 noop 即可。
		mp, sd := fallbackNoopMeterProvider()
		return mp, sd, nil
	}

	proto := strings.ToLower(resolveMetricProtocol(cfg))
	var exporter sdkmetric.Exporter
	var err error
	switch proto {
	case "grpc":
		exporter, err = otlpmetricgrpc.New(ctx,
			otlpmetricgrpc.WithEndpoint(stripScheme(endpoint)),
			otlpmetricgrpc.WithInsecure(),
		)
	default:
		exporter, err = otlpmetrichttp.New(ctx,
			otlpmetrichttp.WithEndpoint(stripScheme(endpoint)),
			otlpmetrichttp.WithInsecure(),
		)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("new otlp metric exporter(%s): %w", proto, err)
	}

	reader := sdkmetric.NewPeriodicReader(exporter,
		sdkmetric.WithInterval(resolveMetricInterval()),
	)
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
		sdkmetric.WithResource(res),
	)
	shutdown := func(ctx context.Context) error {
		// mp.Shutdown 会同时 flush reader 和关 exporter，不需要单独调。
		return mp.Shutdown(ctx)
	}
	return mp, shutdown, nil
}

// fallbackNoopMeterProvider 构造 noop MeterProvider + no-op shutdown。
//
// 两种场景复用：1) 显式 disabled / 未配 endpoint；2) 真实 exporter 构造失败降级。
func fallbackNoopMeterProvider() (metric.MeterProvider, func(context.Context) error) {
	return metricnoop.NewMeterProvider(), func(context.Context) error { return nil }
}

// composeShutdown 合并 trace 与 metric 的 shutdown；先 metric 后 trace。
//
// 为何先 metric：metric 是周期性导出，可能积累了最近 15s 的数据点；先 flush
// metric，再关 trace，避免 trace 关后 metric 还在尝试用已失效的 Resource 写出。
// 两者之间一个报错不影响另一个继续关闭；最终返回首个错误（符合"尽力关闭"语义）。
func composeShutdown(tp, mp func(context.Context) error) func(context.Context) error {
	return func(ctx context.Context) error {
		var firstErr error
		if mp != nil {
			if err := mp(ctx); err != nil {
				firstErr = fmt.Errorf("shutdown meter provider: %w", err)
			}
		}
		if tp != nil {
			if err := tp(ctx); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("shutdown tracer provider: %w", err)
			}
		}
		return firstErr
	}
}