
// Package knowledge 封装 trpc-agent-go/knowledge 模块的构造器，
// 提供「启动时懒初始化 + 凭据缺失时降级为 stub 工具」两种模式，
// 保证没有 OPENAI_API_KEY 时应用仍可启动。
//
// 设计要点：
//   - 使用框架自带的 BuiltinKnowledge（支持 inmemory / pgvector / tcvector 等向量库）
//   - D4：本地目录 (data/knowledge/) + OpenAI embedding + inmemory 向量库
//   - D12+：支持 iWiki / Confluence / Wuji source（见 TODO）
//   - D15+：切换到 tcvector 以支撑生产规模
//
// 凭据检测：
//   - OPENAI_API_KEY 未设置 → 使用 stub 工具（fn 返回"RAG 未就绪"提示）
//   - KNOWLEDGE_DISABLE=1  → 强制禁用 RAG
package knowledge

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/knowledge"
	openaiembedder "trpc.group/trpc-go/trpc-agent-go/knowledge/embedder/openai"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/source"
	dirsource "trpc.group/trpc-go/trpc-agent-go/knowledge/source/dir"
	filesource "trpc.group/trpc-go/trpc-agent-go/knowledge/source/file"
	knowledgetool "trpc.group/trpc-go/trpc-agent-go/knowledge/tool"
	vectorinmemory "trpc.group/trpc-go/trpc-agent-go/knowledge/vectorstore/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	// 注册文档读取器，以便正确解析 .md / .txt 等文件。
	_ "trpc.group/trpc-go/trpc-agent-go/knowledge/document/reader/markdown"
	_ "trpc.group/trpc-go/trpc-agent-go/knowledge/document/reader/text"
)

// ToolName 注入到 Agent 的工具名，system_prompt 中直接使用这个名字。
const ToolName = "knowledge_search"

// Config RAG 配置。
type Config struct {
	// DataDir 本地知识库根目录，D4 默认 data/knowledge/
	DataDir string
	// EmbeddingModel 嵌入模型名（默认 text-embedding-3-small）
	EmbeddingModel string
	// Disabled 强制禁用（读取 KNOWLEDGE_DISABLE 环境变量）
	Disabled bool
	// LoadTimeout 首次加载超时（默认 2 分钟）
	LoadTimeout time.Duration
}

// DefaultConfig 返回默认配置（从环境变量补齐）。
func DefaultConfig() Config {
	c := Config{
		DataDir:        getenvDefault("KNOWLEDGE_DATA_DIR", "data/knowledge"),
		EmbeddingModel: getenvDefault("OPENAI_EMBEDDING_MODEL", "text-embedding-3-small"),
		LoadTimeout:    2 * time.Minute,
	}
	if isTruthy(os.Getenv("KNOWLEDGE_DISABLE")) {
		c.Disabled = true
	}
	return c
}

// Builder 可复用的 RAG 工具构造器。
type Builder struct {
	cfg     Config
	kb      *knowledge.BuiltinKnowledge
	stubOn  bool
	stubMsg string
}

// NewBuilder 创建 Builder（尚未实际加载）。
func NewBuilder(cfg Config) *Builder {
	return &Builder{cfg: cfg}
}

// Build 根据凭据情况构造 knowledge_search 工具：
//   - 正常：构造 BuiltinKnowledge，调用 Load 加载 sources，返回 AgenticFilterSearchTool
//   - 降级：返回 stub FunctionTool，始终回复"未就绪"提示
//
// 该方法幂等：重复调用会复用已构造的 kb。
func (b *Builder) Build(ctx context.Context) (tool.Tool, error) {
	// 降级路径
	if b.cfg.Disabled {
		return b.stubTool("KnowledgeAgent RAG 已被禁用（KNOWLEDGE_DISABLE=1）。"), nil
	}
	if os.Getenv("OPENAI_API_KEY") == "" {
		return b.stubTool("KnowledgeAgent RAG 未就绪：缺少 OPENAI_API_KEY，无法生成 embedding；请配置后重启服务。"), nil
	}

	// 真实路径：装配 kb
	if b.kb == nil {
		kb, err := b.buildRealKB(ctx)
		if err != nil {
			// 加载失败也走 stub，而不是让整个进程崩溃
			return b.stubTool(fmt.Sprintf("KnowledgeAgent RAG 加载失败：%v", err)), nil
		}
		b.kb = kb
	}

	// 使用 AgenticFilterSearchTool，让 LLM 可以按元数据过滤
	metadata := source.GetAllMetadata(b.listSources())
	t := knowledgetool.NewAgenticFilterSearchTool(
		b.kb,
		metadata,
		knowledgetool.WithToolName(ToolName),
		knowledgetool.WithToolDescription(
			"LetsGo 运维知识库检索：查询架构文档、历史故障复盘、FAQ、操作 Runbook。"+
				"适用场景：用户询问概念/流程/历史案例时优先使用。"+
				"可按 category/topic/source 等元数据过滤。"),
	)
	return t, nil
}

// buildRealKB 构造真实的 BuiltinKnowledge 并加载 sources。
func (b *Builder) buildRealKB(ctx context.Context) (*knowledge.BuiltinKnowledge, error) {
	sources := b.listSources()
	if len(sources) == 0 {
		return nil, fmt.Errorf("数据目录 %q 下未发现任何文档", b.cfg.DataDir)
	}

	emb := openaiembedder.New(openaiembedder.WithModel(b.cfg.EmbeddingModel))
	vs := vectorinmemory.New()

	kb := knowledge.New(
		knowledge.WithVectorStore(vs),
		knowledge.WithEmbedder(emb),
		knowledge.WithSources(sources),
	)

	loadCtx, cancel := context.WithTimeout(ctx, b.cfg.LoadTimeout)
	defer cancel()
	if err := kb.Load(
		loadCtx,
		knowledge.WithShowProgress(false),
		knowledge.WithShowStats(false),
	); err != nil {
		return nil, fmt.Errorf("kb.Load: %w", err)
	}
	return kb, nil
}

// listSources 扫描 DataDir，按「一级子目录 → 一个 dir source」组织，
// 这样每个子目录的元数据（category=子目录名）独立，方便 LLM 按 category 过滤。
//
// 目录约定（D4 版本）：
//
//	data/knowledge/
//	  runbook/         # 运行手册
//	  architecture/    # 架构文档
//	  faq/             # 常见问答
//	  incident/        # 历史故障复盘
//
// 若 DataDir 不存在或为空，会自动回落到把 DataDir 本身当作一个 dir source。
func (b *Builder) listSources() []source.Source {
	root := b.cfg.DataDir
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil
	}

	var out []source.Source
	entries, _ := os.ReadDir(root)
	hasSubDir := false
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		hasSubDir = true
		sub := filepath.Join(root, e.Name())
		name := e.Name()
		out = append(out, dirsource.New(
			[]string{sub},
			dirsource.WithName(name),
			dirsource.WithMetadataValue("category", name),
			dirsource.WithMetadataValue("source_type", "local_dir"),
		))
	}
	if !hasSubDir {
		// 退化：把 DataDir 根当作单一 source
		out = append(out, dirsource.New(
			[]string{root},
			dirsource.WithName("knowledge"),
			dirsource.WithMetadataValue("category", "general"),
			dirsource.WithMetadataValue("source_type", "local_dir"),
		))
	}
	// 额外：把 root 下直接放的 *.md 文件也独立加入（filesource）
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		full := filepath.Join(root, name)
		out = append(out, filesource.New(
			[]string{full},
			filesource.WithName(name),
			filesource.WithMetadataValue("category", "general"),
			filesource.WithMetadataValue("source_type", "local_file"),
		))
	}
	return out
}

// stubTool 生成降级占位工具，返回固定提示。
func (b *Builder) stubTool(msg string) tool.Tool {
	b.stubOn = true
	b.stubMsg = msg

	type stubInput struct {
		Query string `json:"query" description:"要检索的问题"`
	}
	type stubOutput struct {
		OK      bool   `json:"ok"`
		Stub    bool   `json:"stub"`
		Message string `json:"message"`
		Hint    string `json:"hint"`
	}
	fn := func(_ context.Context, _ stubInput) (*stubOutput, error) {
		return &stubOutput{
			OK:      false,
			Stub:    true,
			Message: msg,
			Hint:    "请直接基于你的通用知识回答，并在回答中向用户说明：知识库当前未加载，回答仅来自模型常识。",
		}, nil
	}
	return function.NewFunctionTool(
		fn,
		function.WithName(ToolName),
		function.WithDescription("【占位工具】知识库未就绪，调用后返回提示。正常场景下请尽量基于模型自身知识回答。"),
	)
}

// IsStub 当前工具是否处于降级状态。
func (b *Builder) IsStub() bool { return b.stubOn }

// StubMessage 降级原因说明。
func (b *Builder) StubMessage() string { return b.stubMsg }

func getenvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func isTruthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
