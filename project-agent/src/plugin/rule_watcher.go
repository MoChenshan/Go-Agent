// Package plugin 的 rule_watcher 在 input_guard / output_guard 上层做
// 规则集热加载：周期性检查 YAML 文件 mtime+size，发生变化即重新加载并
// 原子替换到 guard 里。
//
// 为什么用 mtime 轮询而不是 fsnotify：
//  1. 零第三方依赖，与当前 go.mod 生态零耦合；
//  2. fsnotify 在 Windows/NFS 上对原子保存、重命名的事件丢失问题
//     广为人知，轮询反而更健壮；
//  3. 规则变更是低频操作（SRE 手动改），5s 轮询延迟完全可接受。
//
// 可观测性：
//   - Logger 回调把 reload 结果（成功/失败）暴露给 app 层，便于接入
//     日志或 OTel Counter；
//   - 错误不会打断 watcher 循环，也不会清空 guard 规则。
package plugin

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"git.woa.com/trpc-go/gameops-agent/src/observability"
)

// RuleWatcherConfig 规则集 watcher 配置。
type RuleWatcherConfig struct {
	// Path YAML 规则文件路径；为空时 Watcher.Start 直接 no-op。
	Path string
	// Interval 轮询间隔；0 或负数使用默认 5s。
	Interval time.Duration
	// InputGuard 要热替换规则的输入防护器；可为 nil。
	InputGuard *InputGuard
	// OutputGuard 要热替换规则的输出防护器；可为 nil。
	OutputGuard *OutputGuard
	// Logger 重载事件回调（可选）。
	//   - event="loaded"：首次加载或后续 reload 成功，rules 形如 "input=N output=M"
	//   - event="error" ：加载或编译失败，msg 为错误描述（此时保留旧规则）
	//   - event="skip"  ：文件未变化（仅 debug 级别场景用，默认不打日志也可）
	Logger func(event, msg string)
}

// RuleWatcher 周期性检查规则文件并热替换 guard 规则集。
//
// 使用方式：
//
//	w := NewRuleWatcher(cfg)
//	w.Start()        // 启动 goroutine；立即执行首次加载
//	defer w.Stop()   // 服务退出前优雅停止
type RuleWatcher struct {
	cfg      RuleWatcherConfig
	interval time.Duration

	mu       sync.Mutex // 保护下面 4 个字段
	started  bool
	stopped  bool
	cancel   context.CancelFunc
	doneCh   chan struct{}

	// 上次成功加载的文件指纹（mtime + size）；通过它判断文件是否变更。
	lastMod  time.Time
	lastSize int64
}

// NewRuleWatcher 构造 watcher；不启动。
func NewRuleWatcher(cfg RuleWatcherConfig) *RuleWatcher {
	iv := cfg.Interval
	if iv <= 0 {
		iv = 5 * time.Second
	}
	return &RuleWatcher{cfg: cfg, interval: iv}
}

// Start 启动 watcher：立即做一次同步加载（让启动期规则就绪），然后
// 起后台 goroutine 周期轮询。幂等：重复调用无副作用。
//
// 当 cfg.Path 为空时，Start 视为 no-op —— 保持默认硬编码规则。
func (w *RuleWatcher) Start() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.started || w.stopped {
		return
	}
	if w.cfg.Path == "" {
		w.started = true
		return
	}
	// 首次同步加载，失败只记日志，不阻断启动（guard 已有默认/旧规则兜底）。
	if err := w.reloadLocked(); err != nil {
		w.emit("error", fmt.Sprintf("initial load failed: %v", err))
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.doneCh = make(chan struct{})
	w.started = true
	go w.loop(ctx)
}

// Stop 停止 watcher；等待后台 goroutine 退出；幂等。
func (w *RuleWatcher) Stop() {
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

// loop 后台轮询循环。
func (w *RuleWatcher) loop(ctx context.Context) {
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

// reloadLocked 检查文件指纹；若未变化则 no-op；变化则加载+编译+原子替换。
// 调用方必须持有 w.mu。错误不会修改 w.lastMod/lastSize，保证下次继续重试。
func (w *RuleWatcher) reloadLocked() error {
	st, err := os.Stat(w.cfg.Path)
	if err != nil {
		if os.IsNotExist(err) {
			// 文件被删：不清空 guard，保留现有规则；记一次 error 便于排查。
			return fmt.Errorf("rules file not found: %s", w.cfg.Path)
		}
		return fmt.Errorf("stat %s: %w", w.cfg.Path, err)
	}
	mt := st.ModTime()
	sz := st.Size()
	if !w.lastMod.IsZero() && mt.Equal(w.lastMod) && sz == w.lastSize {
		return nil // 文件未变化
	}
	rs, err := LoadRulesetFromFile(w.cfg.Path)
	if err != nil {
		return err
	}
	if rs == nil {
		// 文件存在但读出空：视为"显式清空"，走默认规则。
		if w.cfg.InputGuard != nil {
			w.cfg.InputGuard.ReplaceRules(nil)
		}
		if w.cfg.OutputGuard != nil {
			w.cfg.OutputGuard.ReplaceRules(nil)
		}
		w.lastMod, w.lastSize = mt, sz
		w.emit("loaded", "input=default output=default")
		return nil
	}
	inRules, err := CompileInputRules(rs.Input.Rules)
	if err != nil {
		return fmt.Errorf("compile input rules: %w", err)
	}
	outRules, err := CompileOutputRules(rs.Output.Rules)
	if err != nil {
		return fmt.Errorf("compile output rules: %w", err)
	}
	// 两组都编译通过后再原子替换，避免"半成品"状态。
	if w.cfg.InputGuard != nil {
		w.cfg.InputGuard.ReplaceRules(inRules)
	}
	if w.cfg.OutputGuard != nil {
		w.cfg.OutputGuard.ReplaceRules(outRules)
	}
	w.lastMod, w.lastSize = mt, sz
	// 自定义为空时显示降级后的默认规则数，便于 SRE 直观确认实际生效规则。
	inCnt := len(inRules)
	if inCnt == 0 {
		inCnt = len(DefaultInputRules())
	}
	outCnt := len(outRules)
	if outCnt == 0 {
		outCnt = len(DefaultOutputRules())
	}
	w.emit("loaded", fmt.Sprintf("input=%d output=%d", inCnt, outCnt))
	return nil
}

// emit 安全调用 Logger（nil-safe），同时把事件上报 OTel 指标。
//
// event 语义与 observability.IncRuleReload 的 status 对齐：
//   - loaded   → ok       （规则编译 + 原子替换成功）
//   - skip     → unchanged（文件未变化；本文件目前不触发，占位用）
//   - error    → error    （任一阶段失败；此时保留旧规则）
//
// kind 统一打成 "guard_rules"，因为本 watcher 同时管 input+output；
// 若未来拆成独立文件再细化到 input_guard / output_guard。
func (w *RuleWatcher) emit(event, msg string) {
	if w.cfg.Logger != nil {
		w.cfg.Logger(event, msg)
	}
	// 上报 Counter；context.Background 即可，watcher 不走请求链路。
	status := event
	switch event {
	case "loaded":
		status = observability.StatusOK
	case "error":
		status = observability.StatusError
	case "skip":
		status = "unchanged"
	}
	observability.IncRuleReload(context.Background(), "guard_rules", status)
}
