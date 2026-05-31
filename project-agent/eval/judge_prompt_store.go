// judge_prompt_store.go D17.2.1 — LLM Judge prompt YAML 热加载。
//
// 为什么独立文件而非塞进 judge_prompt.go：
//   - judge_prompt.go 只管 prompt 模板 & 响应解析，属于"数据与格式层"；
//   - judge_prompt_store.go 管"配置热加载 + 并发原子替换"，属于"运行时层"。
//   - 关注点分离后各自单测更干净。
//
// 为什么不复用 src/plugin/rule_watcher.go：
//   - RuleWatcher 硬绑定 InputGuard/OutputGuard 两个具体类型；
//   - 强行引入泛型或反射反而增大复杂度；
//   - 这里代码量不大（<200 行），直接按同一模式复写更清晰。
//
// 设计原则：
//  1. **零破坏**：LLMJudge 未设 PromptStore 时走原硬编码常量，与 D17.2 完全一致；
//  2. **原子替换**：Get() 返回快照；Replace() 用写锁保护；并发 Score 永远读到完整副本；
//  3. **YAML 失败不清空**：解析错误保留旧 snapshot，避免"SRE 手滑写错 YAML 导致 Judge 全罢工"；
//  4. **mtime 轮询而非 fsnotify**：与 rule_watcher 同理（零依赖、Windows 友好、低频变更）；
//  5. **可观测性**：reload 事件接 observability.IncRuleReload(kind="judge_prompt", status=...)，
//     复用 D17.4 的 gameops.rule.reload.total Counter。
package eval

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v3"

	"git.woa.com/trpc-go/gameops-agent/src/observability"
)

// ---------------------------------------------------------------------------
// JudgePromptSnapshot — 一份不可变的 prompt 配置
// ---------------------------------------------------------------------------

// JudgePromptSnapshot 是 PromptStore 对外暴露的"只读视图"。
//
// 故意用指针 + 整体替换（而非字段级 RWMutex）实现原子性：
// 并发 Score 读 snapshot 永远看到一致的 (system_prompt + dimensions) 组合，
// 中途 SRE reload YAML 也不会看到"新 prompt + 旧 dimensions"的错配状态。
type JudgePromptSnapshot struct {
	// SystemPrompt system 提示；空则由调用方走 DefaultJudgeSystemPrompt 兜底。
	SystemPrompt string
	// Dimensions 维度列表；空则由调用方走 DefaultJudgeDimensions() 兜底。
	Dimensions []JudgeDimension
	// Version YAML 中的 version 字段；空则保持 JudgePromptVersion 常量。
	// 出现在 JudgeReport 的元数据里，便于跨版本对比。
	Version string
}

// IsEmpty 当 snapshot 完全为空时返回 true，调用方据此决定走默认路径。
func (s *JudgePromptSnapshot) IsEmpty() bool {
	if s == nil {
		return true
	}
	return strings.TrimSpace(s.SystemPrompt) == "" && len(s.Dimensions) == 0
}

// ---------------------------------------------------------------------------
// JudgePromptStore — 并发安全的原子快照容器
// ---------------------------------------------------------------------------

// JudgePromptStore 持有当前生效的 JudgePromptSnapshot，支持热替换。
//
// 并发模型：用 atomic.Value 而非 RWMutex —— Get 是 Score 链路上的高频调用，
// 用 atomic.Load 零锁；Replace 是低频的 reload 事件，不在意开销。
type JudgePromptStore struct {
	v atomic.Value // *JudgePromptSnapshot
}

// NewJudgePromptStore 构造空 store（snapshot 为 nil，IsEmpty=true）。
// 调用方可立即 Replace 填入初始值，也可保持空让 LLMJudge 走默认行为。
func NewJudgePromptStore() *JudgePromptStore {
	s := &JudgePromptStore{}
	s.v.Store((*JudgePromptSnapshot)(nil))
	return s
}

// Get 返回当前快照（永远不会 panic；nil 视为空）。调用方应视作不可变。
func (s *JudgePromptStore) Get() *JudgePromptSnapshot {
	if s == nil {
		return nil
	}
	v, _ := s.v.Load().(*JudgePromptSnapshot)
	return v
}

// Replace 原子替换快照。传 nil 等价于"清空 store 回到默认"。
func (s *JudgePromptStore) Replace(snap *JudgePromptSnapshot) {
	if s == nil {
		return
	}
	s.v.Store(snap)
}

// ---------------------------------------------------------------------------
// YAML Loader — 解析 + 校验
// ---------------------------------------------------------------------------

// judgePromptYAML YAML 顶层 schema（仅此一处锚点，改 schema 时只需改这里）。
//
// 示例 YAML：
//
//	version: v1.1
//	system_prompt: |
//	  你是一名严格的 SRE/运维专家评审员...
//	dimensions:
//	  - name: RootCauseAccuracy
//	    threshold: 0.85
//	    criterion: 答案是否准确指出了真正的根因...
//	  - name: EvidenceSufficiency
//	    threshold: 0.80
//	    criterion: 答案是否引用了具体证据...
type judgePromptYAML struct {
	Version      string               `yaml:"version"`
	SystemPrompt string               `yaml:"system_prompt"`
	Dimensions   []judgeDimensionYAML `yaml:"dimensions"`
}

type judgeDimensionYAML struct {
	Name      string  `yaml:"name"`
	Threshold float64 `yaml:"threshold"`
	Criterion string  `yaml:"criterion"`
}

// LoadJudgePromptFromFile 从 YAML 文件加载 snapshot。
//
// 校验策略（失败会被调用方 watcher 捕获并保留旧 snapshot）：
//   - 文件可读；
//   - YAML 可解析；
//   - 至少有 system_prompt 或 dimensions 其中之一（两者都空 = 空 YAML 没意义）；
//   - 每个 dimension 必须有 name；threshold 越界自动夹到 [0, 1]；
//   - 不允许重名 dimension（LLM 会混淆）。
func LoadJudgePromptFromFile(path string) (*JudgePromptSnapshot, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("LoadJudgePromptFromFile: empty path")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return parseJudgePromptYAML(data)
}

// parseJudgePromptYAML 纯函数：便于单测喂 []byte 断言，不落盘。
func parseJudgePromptYAML(data []byte) (*JudgePromptSnapshot, error) {
	var raw judgePromptYAML
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("yaml parse: %w", err)
	}

	sp := strings.TrimSpace(raw.SystemPrompt)
	dims := make([]JudgeDimension, 0, len(raw.Dimensions))
	seen := make(map[string]struct{}, len(raw.Dimensions))
	for i, d := range raw.Dimensions {
		name := strings.TrimSpace(d.Name)
		if name == "" {
			return nil, fmt.Errorf("dimension[%d]: name required", i)
		}
		if _, dup := seen[name]; dup {
			return nil, fmt.Errorf("dimension[%d]: duplicate name %q", i, name)
		}
		seen[name] = struct{}{}
		th := d.Threshold
		if th < 0 {
			th = 0
		}
		if th > 1 {
			th = 1
		}
		dims = append(dims, JudgeDimension{
			Name:      name,
			Threshold: th,
			Criterion: strings.TrimSpace(d.Criterion),
		})
	}

	if sp == "" && len(dims) == 0 {
		return nil, fmt.Errorf("yaml empty: neither system_prompt nor dimensions defined")
	}

	return &JudgePromptSnapshot{
		SystemPrompt: sp,
		Dimensions:   dims,
		Version:      strings.TrimSpace(raw.Version),
	}, nil
}

// ---------------------------------------------------------------------------
// JudgePromptWatcher — 周期轮询 + 原子替换
// ---------------------------------------------------------------------------

// JudgePromptWatcherConfig 构造参数。
type JudgePromptWatcherConfig struct {
	// Path YAML 文件路径；空则 Start 立即 no-op（保留默认行为）。
	Path string
	// Store 要热替换的 store；必填（Start 时校验）。
	Store *JudgePromptStore
	// Interval 轮询间隔，<=0 走默认 10s（judge prompt 变更比 guard rules 更低频）。
	Interval time.Duration
	// Logger 可选事件回调：event ∈ {loaded, skip, error}，msg 为简要描述。
	Logger func(event, msg string)
}

// JudgePromptWatcher 周期性检查 YAML mtime+size，变更即重新加载并替换 store。
type JudgePromptWatcher struct {
	cfg      JudgePromptWatcherConfig
	interval time.Duration

	mu      sync.Mutex
	started bool
	stopped bool
	cancel  context.CancelFunc
	doneCh  chan struct{}

	// 文件指纹（mtime+size），用于未变化 short-circuit。
	lastMod  time.Time
	lastSize int64

	// 可观测：成功 reload 次数与失败次数（测试断言用）。
	reloads atomic.Int64
	errors  atomic.Int64
}

// NewJudgePromptWatcher 构造；不启动。
func NewJudgePromptWatcher(cfg JudgePromptWatcherConfig) *JudgePromptWatcher {
	iv := cfg.Interval
	if iv <= 0 {
		iv = 10 * time.Second
	}
	return &JudgePromptWatcher{cfg: cfg, interval: iv}
}

// Start 启动 watcher。空 Path 或空 Store 视为 no-op。
// 首次立即做一次同步加载（失败不阻塞启动），随后起后台 goroutine 轮询。
func (w *JudgePromptWatcher) Start() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.started || w.stopped {
		return
	}
	if w.cfg.Path == "" || w.cfg.Store == nil {
		w.started = true
		return
	}
	if err := w.reloadLocked(); err != nil {
		w.emit("error", fmt.Sprintf("initial load failed: %v", err))
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.doneCh = make(chan struct{})
	w.started = true
	go w.loop(ctx)
}

// Stop 停止 watcher；幂等；等待后台 goroutine 退出。
func (w *JudgePromptWatcher) Stop() {
	w.mu.Lock()
	if !w.started || w.stopped {
		w.stopped = true
		w.mu.Unlock()
		return
	}
	w.stopped = true
	cancel := w.cancel
	done := w.doneCh
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

// Reloads 成功 reload 次数（测试断言）。
func (w *JudgePromptWatcher) Reloads() int64 {
	if w == nil {
		return 0
	}
	return w.reloads.Load()
}

// Errors 失败 reload 次数（测试断言）。
func (w *JudgePromptWatcher) Errors() int64 {
	if w == nil {
		return 0
	}
	return w.errors.Load()
}

func (w *JudgePromptWatcher) loop(ctx context.Context) {
	defer close(w.doneCh)
	tk := time.NewTicker(w.interval)
	defer tk.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tk.C:
			w.mu.Lock()
			err := w.reloadLocked()
			w.mu.Unlock()
			if err != nil {
				w.emit("error", err.Error())
			}
		}
	}
}

// reloadLocked 检查文件指纹；未变化 short-circuit；变化则加载 + 原子替换。
// 调用方必须持有 w.mu。
func (w *JudgePromptWatcher) reloadLocked() error {
	st, err := os.Stat(w.cfg.Path)
	if err != nil {
		// 文件缺失 → 不清空 store，保留最后一次的 snapshot；只记 error。
		return fmt.Errorf("stat %s: %w", w.cfg.Path, err)
	}
	mt := st.ModTime()
	sz := st.Size()
	if !w.lastMod.IsZero() && mt.Equal(w.lastMod) && sz == w.lastSize {
		return nil // 未变化
	}
	snap, err := LoadJudgePromptFromFile(w.cfg.Path)
	if err != nil {
		return err
	}
	w.cfg.Store.Replace(snap)
	w.lastMod, w.lastSize = mt, sz
	w.reloads.Add(1)
	w.emit("loaded", fmt.Sprintf("version=%q dims=%d system_prompt_len=%d",
		snap.Version, len(snap.Dimensions), len(snap.SystemPrompt)))
	return nil
}

// emit 安全调用 Logger + 上报 OTel Counter。
//
// status 映射与 rule_watcher 一致：
//   - loaded → ok
//   - error  → error（同时 errors 计数 +1）
//   - skip   → unchanged（本 watcher 未使用）
func (w *JudgePromptWatcher) emit(event, msg string) {
	if event == "error" {
		w.errors.Add(1)
	}
	if w.cfg.Logger != nil {
		w.cfg.Logger(event, msg)
	}
	status := event
	switch event {
	case "loaded":
		status = observability.StatusOK
	case "error":
		status = observability.StatusError
	case "skip":
		status = "unchanged"
	}
	observability.IncRuleReload(context.Background(), "judge_prompt", status)
}
