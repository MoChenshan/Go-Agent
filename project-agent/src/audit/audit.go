// Package audit 提供 GameOps Agent 写操作的统一审计日志。
//
// 目标场景
//
//	所有通过 HITL（Human-in-the-Loop）放行的写操作，例如：
//	  - bcs_helm_manage: deploy/upgrade/rollback/uninstall
//	  - gongfeng_mr_create / gongfeng_mr_merge
//	  - devops_pipeline_rerun / devops_build_cancel
//	  - tapd_bug_create
//	必须在执行"成功"之后追加一条结构化审计日志，便于事后回溯、
//	问责与合规（D17 路线项"RBAC & 审计日志"的 MVP 实现）。
//
// 设计原则
//  1. 零依赖：仅依赖标准库（encoding/json、log、os、sync），
//     不引入 OTel/SLS/Kafka 等重型组件，未来替换只换 Sink。
//  2. 失败不影响主流程：写日志失败仅打印 stderr，不往上抛。
//  3. 可配置：
//     - AUDIT_DISABLE=1                 禁用（仅 CI / 本地调试）
//     - AUDIT_FILE=/path/to/audit.log   追加写文件（未设置仅 stdout）
//     - AUDIT_SINK=stdout|file|both     输出通道，默认 stdout
//  4. 结构化：每条日志一行 JSON（jsonl），字段稳定，便于 Filebeat/Loki 采集。
package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// Record 一条审计记录。字段稳定，下游采集端按此 schema 解析。
type Record struct {
	// TS 记录时间（RFC3339，带时区）
	TS string `json:"ts"`
	// User 触发用户（由调用方从 ctx/session 中提取，缺失则为 "unknown"）
	User string `json:"user"`
	// Agent 发起的 Agent 名（例如 "repair_agent"）
	Agent string `json:"agent,omitempty"`
	// Action 动作名，与 hitl.Plan.Action 对齐（例如 "bcs.helm.rollback"）
	Action string `json:"action"`
	// Severity 破坏等级（critical/high/medium/low）
	Severity string `json:"severity"`
	// Target 作用对象（例如 "BCS-K8S-001/ns-letsgo/game-core"）
	Target string `json:"target"`
	// Params 关键入参（工具层负责脱敏，避免把 token/secret 写进来）
	Params map[string]any `json:"params,omitempty"`
	// Reason 用户提供的变更原因（可选，critical 级别强烈推荐）
	Reason string `json:"reason,omitempty"`
	// Result 执行结果：success / failure
	Result string `json:"result"`
	// ErrorMsg 失败时的错误信息（成功时为空）
	ErrorMsg string `json:"error,omitempty"`
	// SessionID 可关联 SSE 会话 / LLM trace
	SessionID string `json:"session_id,omitempty"`
	// Mock 当次调用是否走 Mock 客户端（true 表示并未真正打到线上 API）
	Mock bool `json:"mock,omitempty"`

	// ---------- D17.6 HMAC 签名字段（omitempty：未签名记录零字节开销） ----------

	// SigAlg 签名算法；目前固定为 "HMAC-SHA256"。
	// 未来若接入 KMS / Ed25519，通过此字段判断验签路径。
	SigAlg string `json:"sig_alg,omitempty"`
	// SigKID 签名密钥 id；验签时按此选 key。
	// 轮换期：老记录用老 kid，新记录用新 kid；验签器知道所有历史 kid 即可。
	SigKID string `json:"sig_kid,omitempty"`
	// PrevSig 上一条记录的 Sig（链式签名，仅 AUDIT_HMAC_CHAIN=1 时填写）。
	// 作用：防止"中间某条被整条删除"而前后仍自洽的攻击。
	PrevSig string `json:"prev_sig,omitempty"`
	// Sig HMAC-SHA256 摘要的十六进制串。
	// **最后一个字段**：Sign/Verify 的 canonical marshal 会清空它再重算，
	// 顺序在这里只影响可读性，不影响正确性（json.Marshal 按声明序输出）。
	Sig string `json:"sig,omitempty"`
}

// Event 是调用方构造 Record 的便捷输入，工具层只关心业务字段。
type Event struct {
	User      string
	Agent     string
	Action    string
	Severity  string
	Target    string
	Params    map[string]any
	Reason    string
	Success   bool
	Err       error
	SessionID string
	Mock      bool
}

// sink 统一出口，默认 stdout；测试可通过 SetSink 注入内存 buffer。
var (
	sinkMu sync.RWMutex
	sink   Sink = defaultSink{}
)

// Sink 审计输出端抽象。
type Sink interface {
	Write(line []byte) error
}

// SetSink 替换 sink（测试用）。返回旧 sink 便于恢复。
func SetSink(s Sink) Sink {
	sinkMu.Lock()
	defer sinkMu.Unlock()
	old := sink
	if s == nil {
		sink = defaultSink{}
	} else {
		sink = s
	}
	return old
}

// Emit 输出一条审计记录。工具层只需要在 HITL 通过且 API 调用结束后调用一次。
//
// 典型用法（以 gongfeng_mr_merge 为例）：
//
//	// HITL 通过后真正执行 API
//	_, apiErr := client.MergeMR(ctx, req)
//	audit.Emit(ctx, audit.Event{
//	    User:     session.User(ctx),
//	    Agent:    "repair_agent",
//	    Action:   "gongfeng.mr.merge",
//	    Severity: string(hitl.SeverityHigh),
//	    Target:   fmt.Sprintf("%s!%d", in.ProjectID, in.IID),
//	    Params:   map[string]any{"project_id": in.ProjectID, "iid": in.IID},
//	    Reason:   in.Reason,
//	    Success:  apiErr == nil,
//	    Err:      apiErr,
//	    Mock:     client.IsMock(),
//	})
func Emit(ev Event) {
	if isDisabled() {
		return
	}
	rec := Record{
		TS:        time.Now().Format(time.RFC3339),
		User:      defaultStr(ev.User, "unknown"),
		Agent:     ev.Agent,
		Action:    ev.Action,
		Severity:  ev.Severity,
		Target:    ev.Target,
		Params:    ev.Params,
		Reason:    ev.Reason,
		SessionID: ev.SessionID,
		Mock:      ev.Mock,
	}
	if ev.Success {
		rec.Result = "success"
	} else {
		rec.Result = "failure"
		if ev.Err != nil {
			rec.ErrorMsg = ev.Err.Error()
		}
	}

	// D17.6：在 marshal 前加盖 HMAC。
	// 设计要点：
	//   - 未配 Signer（本地开发）→ activeSigner() 返 nil，跳过；
	//   - 签名失败只 warn 不阻塞：审计记录可以"弱完整性"，但绝不能"因签名故障被丢失"。
	if sg := activeSigner(); sg != nil {
		if err := sg.Sign(&rec); err != nil {
			fmt.Fprintf(os.Stderr, "[audit] sign failed (kid=%s): %v\n",
				sg.KeyID(), err)
		}
	}

	buf, err := json.Marshal(rec)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[audit] marshal failed: %v\n", err)
		return
	}
	buf = append(buf, '\n')

	sinkMu.RLock()
	s := sink
	sinkMu.RUnlock()
	if err := s.Write(buf); err != nil {
		fmt.Fprintf(os.Stderr, "[audit] write failed: %v\n", err)
	}
}

// isDisabled 读取 AUDIT_DISABLE 环境变量。
func isDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AUDIT_DISABLE"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func defaultStr(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

// ----- 默认 Sink 实现 -----

type defaultSink struct{}

func (defaultSink) Write(line []byte) error {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("AUDIT_SINK")))
	if mode == "" {
		mode = "stdout"
	}
	var firstErr error
	if mode == "stdout" || mode == "both" {
		if _, err := os.Stdout.Write(line); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if mode == "file" || mode == "both" {
		path := strings.TrimSpace(os.Getenv("AUDIT_FILE"))
		if path == "" {
			path = "audit.log"
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			return firstErr
		}
		defer f.Close()
		if _, err := f.Write(line); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// MemorySink 测试用内存 Sink，线程安全。
type MemorySink struct {
	mu    sync.Mutex
	Lines [][]byte
}

// Write 实现 Sink 接口。
func (m *MemorySink) Write(line []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]byte, len(line))
	copy(cp, line)
	m.Lines = append(m.Lines, cp)
	return nil
}

// Snapshot 线程安全获取副本。
func (m *MemorySink) Snapshot() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([][]byte, len(m.Lines))
	copy(out, m.Lines)
	return out
}

// Reset 清空。
func (m *MemorySink) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Lines = nil
}
