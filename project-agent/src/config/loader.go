// Package config 提供 GameOps Agent 的运行时配置加载能力。
//
// D1 阶段采用轻量实现：从本地 YAML 文件加载配置，后续可无缝扩展为对接七彩石
// （Rainbow）或无极（Wuji）配置中心（参考 oncall_agent/infrastructure/config）。
//
// 主要配置项：
//   - Model：LLM 模型名称、API Key、Base URL
//   - Gen：生成参数（temperature / top_p / max_tokens）
//   - MCPFile：mcp_servers.yaml 文件路径
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	mcptools "git.woa.com/trpc-go/gameops-agent/src/tools/mcp_tools"
)

// Config 全局运行时配置。
type Config struct {
	// Model LLM 模型配置。
	Model ModelConfig `yaml:"model"`
	// Gen 生成参数配置。
	Gen GenerationConfig `yaml:"gen"`
	// MCPFile mcp_servers.yaml 文件路径（相对工作目录）。
	MCPFile string `yaml:"mcp_file"`
	// Debug 是否开启调试模式（在 SSE 响应中输出工具参数等信息）。
	Debug bool `yaml:"debug"`
	// Webhook D15：蓝鲸告警 / TAPD Webhook 接入配置。
	Webhook WebhookConfig `yaml:"webhook"`
	// GuardRulesPath D17.1：input_guard / output_guard 的 YAML 规则集路径。
	//   - 为空：走内置默认规则集（保留 D14 行为）。
	//   - 非空：启动时加载，并由 rule_watcher 周期性轮询热替换。
	GuardRulesPath string `yaml:"guard_rules_path"`
	// Audit D17.3：审计日志远端汇聚配置。
	//   - Remote.URL 为空：仅走本地（stdout/file），保留 D10 行为。
	//   - Remote.URL 非空：追加一个 RemoteSink，和本地 Sink 组成 MultiSink。
	Audit AuditConfig `yaml:"audit"`
	// Async D19.2：异步工具执行器配置。
	//   - Enabled=false 时不装配 AsyncRunner，job_* 工具不注入（行为与 D19.1 之前一致）。
	//   - Enabled=true 时按其余字段配参启用。
	Async AsyncConfig `yaml:"async"`
}

// ModelConfig LLM 模型配置。
type ModelConfig struct {
	// Name 模型名称，如 "hunyuan-turbo-s" / "deepseek-chat"。
	Name string `yaml:"name"`
	// BaseURL 模型 API 的 Base URL，为空时使用 OPENAI_BASE_URL 环境变量。
	BaseURL string `yaml:"base_url"`
	// APIKey API Key，为空时使用 OPENAI_API_KEY 环境变量。
	APIKey string `yaml:"api_key"`
}

// GenerationConfig 生成参数。
type GenerationConfig struct {
	Temperature float64 `yaml:"temperature"`
	TopP        float64 `yaml:"top_p"`
	MaxTokens   int     `yaml:"max_tokens"`
	Stream      bool    `yaml:"stream"`
}

// WebhookConfig 外部告警 Webhook 接入配置（D15）。
//
// 设计原则：
//  1. Secret 留空时 Handler 自动关闭签名校验（仅调试可用）。
//  2. 默认从 YAML 读取；未配置时兜底读环境变量 GAMEOPS_WEBHOOK_SECRET，
//     避免生产部署时把密钥写到配置文件。
type WebhookConfig struct {
	// Secret HMAC-SHA256 密钥；与上游（蓝鲸/TAPD）共享。
	Secret string `yaml:"secret"`
	// StoreFile D16：报告持久化文件（JSONL）。留空时使用内存 Store，重启数据丢失。
	// 约定：路径不存在时 FileStore 会自动创建；父目录必须已存在。
	StoreFile string `yaml:"store_file"`
	// DedupeWindow D16：告警/Webhook 幂等窗口（如 "10m"）。留空或 "0" 表示关闭。
	DedupeWindow string `yaml:"dedupe_window"`
	// Summarizer D16：Outcome 总结器实现标识。
	//   - ""   : 不启用，Outcome 走旧模板文案。
	//   - "mock": 使用 MockSummarizer（测试/Demo）。
	// 真实 LLM 实现通过环境变量接入时可在 app 层扩展识别更多值。
	Summarizer string `yaml:"summarizer"`
}

// AuditConfig 审计日志配置（D17.3）。
//
// 设计原则：
//  1. 不配置任何字段 → 行为与 D10 完全一致（走 defaultSink，由 AUDIT_SINK/AUDIT_FILE 控制）。
//  2. 配置 LocalFile → 显式落到指定路径（不再依赖环境变量）。
//  3. 配置 Remote.URL → 追加 RemoteSink，与本地 Sink 组成 MultiSink；
//     本地永远是 source of truth，远端 best-effort。
type AuditConfig struct {
	// LocalFile 本地审计日志文件路径。空字符串表示沿用环境变量行为。
	LocalFile string `yaml:"local_file"`
	// Remote 远端聚合网关配置。URL 为空则不启用远端。
	Remote AuditRemoteConfig `yaml:"remote"`
}

// AuditRemoteConfig 远端聚合网关配置。
//
// 字段与 audit.RemoteSinkConfig 一一对应；之所以复制一份而不是直接嵌入，
// 是为了保持 config 包对 audit 包零依赖（避免未来拆包时循环依赖），
// 同时让 YAML 字段名稳定在 config 包控制之下。
type AuditRemoteConfig struct {
	// URL POST 目标（Loki/Vector/Fluent Bit/Kafka REST 等）。空则不启用。
	URL string `yaml:"url"`
	// AuthHeader 完整 Authorization 头值（留空则读环境变量 GAMEOPS_AUDIT_AUTH）。
	AuthHeader string `yaml:"auth_header"`
	// Headers 额外 HTTP 头（如 X-Tenant）。
	Headers map[string]string `yaml:"headers"`
	// ContentType 默认 application/x-ndjson；指定 application/json 走数组形式。
	ContentType string `yaml:"content_type"`
	// BatchSize 默认 50；大降带宽，小降延迟。
	BatchSize int `yaml:"batch_size"`
	// FlushEverySec 默认 2；即使未攒满也强刷。
	FlushEverySec int `yaml:"flush_every_sec"`
	// BufferSize 默认 10000；满了丢新。
	BufferSize int `yaml:"buffer_size"`
	// TimeoutSec 默认 5。
	TimeoutSec int `yaml:"timeout_sec"`
	// MaxRetries 默认 3（5xx/429/网络错误）。
	MaxRetries int `yaml:"max_retries"`
}

// AsyncConfig 异步工具执行器配置（D19.2）。
//
// 设计原则：零值即"合理开箱可用"。Enabled=true 但其余字段不填，就会用 async.Config 自带的默认值。
// 生产环境建议只显式覆盖 MaxConcurrent/MaxQueued 两个边界字段。
type AsyncConfig struct {
	// Enabled 总开关；true 时 app.go 会实例化 AsyncRunner 并注入 job_* 工具。
	Enabled bool `yaml:"enabled"`
	// MaxConcurrent 同时运行的最大 Job 数（默认 16）。
	MaxConcurrent int `yaml:"max_concurrent"`
	// MaxQueued 非终态 Job 总数上限（默认 256）。
	MaxQueued int `yaml:"max_queued"`
	// DefaultTimeoutSec 未指定时的默认 timeout，单位秒（默认 300）。
	DefaultTimeoutSec int `yaml:"default_timeout_sec"`
	// MaxTimeoutSec 单个 Job 允许的最长 timeout，单位秒（默认 1800 = 30min）。
	MaxTimeoutSec int `yaml:"max_timeout_sec"`
	// JanitorIntervalSec janitor 运行间隔，单位秒（默认 60）。
	JanitorIntervalSec int `yaml:"janitor_interval_sec"`
	// JanitorRetentionSec 终态 Job 保留时长，单位秒（默认 600 = 10min）。
	JanitorRetentionSec int `yaml:"janitor_retention_sec"`
	// AsyncToolNames 允许 async 化的工具名白名单；空列表表示一律不开放。
	//   - 推荐只开放长耗时写操作：["bcs_pod_restart","bcs_scale_deployment","bcs_helm_manage"]
	//   - 包含 "*" 时表示"所有已注册的带 target 写工具"（由 app.go 解释）
	AsyncToolNames []string `yaml:"async_tool_names"`
}

// Default 返回默认配置（用于缺省启动）。
func Default() *Config {
	return &Config{
		Model: ModelConfig{
			Name: "hunyuan-turbo-s",
		},
		Gen: GenerationConfig{
			Temperature: 0.3,
			TopP:        0.9,
			Stream:      true,
		},
		MCPFile: "mcp_servers.yaml",
		Debug:   false,
	}
}

// Load 从 path 加载 Config，path 为空时返回默认配置。
func Load(path string) (*Config, error) {
	cfg := Default()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// 文件不存在时返回默认配置，方便本地启动。
			return cfg, nil
		}
		return nil, fmt.Errorf("read config file %q: %w", path, err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config file %q: %w", path, err)
	}
	if cfg.Webhook.Secret == "" {
		cfg.Webhook.Secret = os.Getenv("GAMEOPS_WEBHOOK_SECRET")
	}
	if cfg.Audit.Remote.AuthHeader == "" {
		// 避免把真实 token 写进 YAML（12-factor 原则：配置 vs. 密钥分离）。
		cfg.Audit.Remote.AuthHeader = os.Getenv("GAMEOPS_AUDIT_AUTH")
	}
	return cfg, nil
}

// mcpFileWrap mcp_servers.yaml 的根结构。
type mcpFileWrap struct {
	MCPServers []mcptools.ServerConfig `yaml:"mcp_servers"`
}

// LoadMCPServers 从 path 加载 MCP Server 配置列表。
// 文件不存在或 mcp_servers 为空时均返回空列表（不报错），方便 D1 骨架启动。
func LoadMCPServers(path string) ([]mcptools.ServerConfig, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read mcp file %q: %w", path, err)
	}
	var wrap mcpFileWrap
	if err := yaml.Unmarshal(data, &wrap); err != nil {
		return nil, fmt.Errorf("unmarshal mcp file %q: %w", path, err)
	}
	return wrap.MCPServers, nil
}
