// Package preflight 提供部署前/启动中的配置就绪度自检。
//
// 典型使用场景
//
//	1. CI / 容器启动时：作为独立命令 `preflight` 执行，
//	   打印哪些平台已配置真实凭据、哪些仍处 Mock，返回码表示是否"严格就绪"。
//	2. K8s livenessProbe：探针命令可直接调用 preflight --strict，
//	   严格模式下任何 Mock 都视为未就绪（便于灰度验收）。
//	3. 人工排障：`go run ./src/cmd/preflight` 即可打印当前模式矩阵。
package preflight

import (
	"fmt"
	"io"
	"os"
	"strings"

	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bcsapi"
	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/bkapi"
	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/devopsapi"
	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/gongfengapi"
	"git.woa.com/trpc-go/gameops-agent/src/infrastructure/tapdapi"
)

// Mode 枚举单个平台的就绪状态。
type Mode string

const (
	// ModeReal 走真实 API。
	ModeReal Mode = "REAL"
	// ModeMock 走 Mock（缺凭据或显式强制）。
	ModeMock Mode = "MOCK"
	// ModeDisabled 显式禁用（不加载）。
	ModeDisabled Mode = "DISABLED"
)

// Platform 一个平台/能力的就绪信息。
type Platform struct {
	Name     string   // 短名（如 "bk-monitor"）
	Title    string   // 展示名（如 "蓝鲸监控"）
	Mode     Mode     // 当前模式
	EnvVars  []string // 相关环境变量（便于故障时给运维）
	Missing  []string // 未配置的关键变量（Mock 模式下会填写）
	Note     string   // 附加说明
}

// Ready 判断单个平台是否处于"生产就绪"状态（真实模式）。
func (p Platform) Ready() bool { return p.Mode == ModeReal }

// Report 自检汇总结果。
type Report struct {
	Platforms []Platform
	// Model LLM 模型模式信息（是否配置了 API Key）。
	Model Platform
	// Audit 审计日志开关状态。
	Audit Platform
}

// Strict 严格就绪：所有平台必须为 REAL。
func (r Report) Strict() bool {
	if !r.Model.Ready() {
		return false
	}
	for _, p := range r.Platforms {
		if p.Mode == ModeMock {
			return false
		}
	}
	return true
}

// Run 运行自检，不依赖外部服务（只读环境变量 + 构造 client 判断 IsMock）。
func Run() Report {
	return Report{
		Model:     checkModel(),
		Audit:     checkAudit(),
		Platforms: []Platform{checkBK(), checkBCS(), checkGongfeng(), checkDevops(), checkTAPD(), checkIWiki()},
	}
}

// Print 打印报告到 w（通常是 stdout），返回是否严格就绪。
func (r Report) Print(w io.Writer) bool {
	fmt.Fprintln(w, "╔══════════════════════════════════════════════════════════════════════╗")
	fmt.Fprintln(w, "║          GameOps Agent — Preflight Readiness Check                  ║")
	fmt.Fprintln(w, "╚══════════════════════════════════════════════════════════════════════╝")
	// 统一使用 Platform.Title 作为展示名，避免中英文不一致。
	printRow(w, r.Model.Title, r.Model)
	printRow(w, r.Audit.Title, r.Audit)
	fmt.Fprintln(w, "────────────────────────────────────────────────────────────────────────")
	for _, p := range r.Platforms {
		printRow(w, p.Title, p)
	}
	fmt.Fprintln(w, "────────────────────────────────────────────────────────────────────────")
	strict := r.Strict()
	if strict {
		fmt.Fprintln(w, "✅ 全部平台处于真实模式，严格就绪（strict=true）")
	} else {
		fmt.Fprintln(w, "⚠ 部分平台处于 Mock/Disabled 模式；非严格模式可继续启动，生产部署前请配置凭据。")
	}
	return strict
}

// printRow 单行格式化。
func printRow(w io.Writer, title string, p Platform) {
	icon := modeIcon(p.Mode)
	fmt.Fprintf(w, "%s  %-16s  %-9s  ", icon, truncate(title, 16), p.Mode)
	if len(p.Missing) > 0 {
		fmt.Fprintf(w, "[缺: %s]", strings.Join(p.Missing, ","))
	} else if p.Note != "" {
		fmt.Fprintf(w, "[%s]", p.Note)
	}
	fmt.Fprintln(w)
}

func modeIcon(m Mode) string {
	switch m {
	case ModeReal:
		return "✅"
	case ModeMock:
		return "🟡"
	case ModeDisabled:
		return "⛔"
	}
	return "❓"
}

func truncate(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n])
}

// ----- 各平台检查函数 -----

func checkModel() Platform {
	p := Platform{Name: "llm", Title: "LLM 模型", EnvVars: []string{"OPENAI_API_KEY", "OPENAI_BASE_URL"}}
	if strings.TrimSpace(os.Getenv("OPENAI_API_KEY")) == "" {
		p.Mode = ModeMock
		p.Missing = []string{"OPENAI_API_KEY"}
		p.Note = "未配置将无法触发真实对话"
		return p
	}
	p.Mode = ModeReal
	if v := os.Getenv("OPENAI_BASE_URL"); v != "" {
		p.Note = "base=" + v
	}
	return p
}

func checkAudit() Platform {
	p := Platform{Name: "audit", Title: "审计日志"}
	if strings.ToLower(os.Getenv("AUDIT_DISABLE")) == "1" ||
		strings.ToLower(os.Getenv("AUDIT_DISABLE")) == "true" {
		p.Mode = ModeDisabled
		p.Note = "AUDIT_DISABLE=1"
		return p
	}
	sink := strings.ToLower(strings.TrimSpace(os.Getenv("AUDIT_SINK")))
	if sink == "" {
		sink = "stdout"
	}
	p.Mode = ModeReal
	p.Note = "sink=" + sink
	return p
}

func checkBK() Platform {
	c := bkapi.NewClient()
	// EnvVars 保持与 bkapi.NewClient 内读取的 env 名完全一致（BK_APIGW_BASE_URL，带下划线）。
	p := Platform{Name: "bk-monitor", Title: "蓝鲸监控", EnvVars: []string{"BK_APP_CODE", "BK_APP_SECRET", "BK_APIGW_BASE_URL"}}
	if c.IsMock() {
		p.Mode = ModeMock
		if os.Getenv("BK_APP_CODE") == "" {
			p.Missing = append(p.Missing, "BK_APP_CODE")
		}
		if os.Getenv("BK_APP_SECRET") == "" {
			p.Missing = append(p.Missing, "BK_APP_SECRET")
		}
		return p
	}
	p.Mode = ModeReal
	return p
}

func checkBCS() Platform {
	c := bcsapi.NewClient()
	p := Platform{Name: "bcs", Title: "BCS 容器", EnvVars: []string{"BCS_TOKEN", "BCS_GATEWAY_URL"}}
	if c.IsMock() {
		p.Mode = ModeMock
		if os.Getenv("BCS_TOKEN") == "" {
			p.Missing = append(p.Missing, "BCS_TOKEN")
		}
		return p
	}
	p.Mode = ModeReal
	return p
}

func checkGongfeng() Platform {
	c := gongfengapi.NewClient()
	p := Platform{Name: "gongfeng", Title: "工蜂 Git", EnvVars: []string{"GONGFENG_TOKEN", "GONGFENG_BASE_URL"}}
	if c.IsMock() {
		p.Mode = ModeMock
		if os.Getenv("GONGFENG_TOKEN") == "" {
			p.Missing = append(p.Missing, "GONGFENG_TOKEN")
		}
		return p
	}
	p.Mode = ModeReal
	if strings.ToLower(os.Getenv("GONGFENG_ALLOW_AUTO_MERGE")) == "1" {
		p.Note = "AUTO_MERGE=on"
	} else {
		p.Note = "AUTO_MERGE=off（合并仍走 Mock 软提示）"
	}
	return p
}

func checkDevops() Platform {
	c := devopsapi.NewClient()
	p := Platform{Name: "devops", Title: "蓝盾 CI/CD", EnvVars: []string{"DEVOPS_TOKEN", "DEVOPS_BASE_URL", "DEVOPS_UID"}}
	if c.IsMock() {
		p.Mode = ModeMock
		if os.Getenv("DEVOPS_TOKEN") == "" {
			p.Missing = append(p.Missing, "DEVOPS_TOKEN")
		}
		return p
	}
	p.Mode = ModeReal
	if strings.ToLower(os.Getenv("DEVOPS_ALLOW_AUTO_OPS")) == "1" {
		p.Note = "AUTO_OPS=on"
	} else {
		p.Note = "AUTO_OPS=off（rerun/cancel 走 Mock 软提示）"
	}
	return p
}

func checkTAPD() Platform {
	c := tapdapi.NewClient()
	p := Platform{Name: "tapd", Title: "TAPD", EnvVars: []string{"TAPD_USER", "TAPD_TOKEN", "TAPD_WORKSPACE_ID"}}
	if c.IsMock() {
		p.Mode = ModeMock
		if os.Getenv("TAPD_USER") == "" {
			p.Missing = append(p.Missing, "TAPD_USER")
		}
		if os.Getenv("TAPD_TOKEN") == "" {
			p.Missing = append(p.Missing, "TAPD_TOKEN")
		}
		return p
	}
	p.Mode = ModeReal
	return p
}

func checkIWiki() Platform {
	p := Platform{Name: "iwiki", Title: "iWiki", EnvVars: []string{"IWIKI_PAAS_ID", "IWIKI_TOKEN"}}
	if strings.ToLower(os.Getenv("IWIKI_DISABLE")) == "1" {
		p.Mode = ModeDisabled
		p.Note = "IWIKI_DISABLE=1"
		return p
	}
	paas := os.Getenv("IWIKI_PAAS_ID")
	tok := os.Getenv("IWIKI_TOKEN")
	if paas == "" || tok == "" {
		p.Mode = ModeMock
		if paas == "" {
			p.Missing = append(p.Missing, "IWIKI_PAAS_ID")
		}
		if tok == "" {
			p.Missing = append(p.Missing, "IWIKI_TOKEN")
		}
		p.Note = "降级为 stub（不阻塞启动）"
		return p
	}
	p.Mode = ModeReal
	return p
}
