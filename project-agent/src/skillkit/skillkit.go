// Package skillkit 负责 GameOps Agent 的技能（Skills）系统装配。
//
// 职责：
//  1. 从 ./skills 目录加载 skill.FSRepository（框架原生），得到三档加载策略：
//     once / turn（按会话）/ task（按单次任务）；
//  2. 创建本地 codeexecutor（localexec），为 Skill 脚本提供工作区；
//  3. 对"环境不具备 Python / 目录不存在"两类情况**优雅降级**，
//     Load() 返回 nil repo / nil exec，调用方判 nil 即可跳过挂载。
//
// 设计要点：
//   - 技能脚本运行可能引入副作用（写磁盘、fork 子进程），因此默认关闭；
//     仅当 SKILLS_ENABLE 明确为 1/true/on 时启用。
//   - 兼容本地/测试/CI 三种环境：
//     · 本地无 python3 → skipReason="no-python"，不阻塞启动；
//     · skills/ 目录缺失 → skipReason="no-skills-dir"；
//     · 显式禁用 → skipReason="disabled-by-env"。
package skillkit

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/codeexecutor"
	localexec "trpc.group/trpc-go/trpc-agent-go/codeexecutor/local"
	"trpc.group/trpc-go/trpc-agent-go/skill"
)

// Config 技能装配配置。
type Config struct {
	// SkillsDir 技能仓库根目录，默认 "./skills"。
	SkillsDir string
	// WorkspaceDir Skill 脚本运行的工作区（存放 inputs/outputs），
	// 默认 "./.skill_workspaces"。
	WorkspaceDir string
	// EnableEnv 环境变量名，值为 1/true/on 时才真正启用。默认 "SKILLS_ENABLE"。
	EnableEnv string
}

// DefaultConfig 返回推荐的默认配置。
func DefaultConfig() Config {
	return Config{
		SkillsDir:    "./skills",
		WorkspaceDir: "./.skill_workspaces",
		EnableEnv:    "SKILLS_ENABLE",
	}
}

// Bundle 装配完成的技能套件。
//
// 调用方判 Repo != nil 才调用 llmagent.WithSkills / WithCodeExecutor。
type Bundle struct {
	Repo       *skill.FSRepository
	Executor   codeexecutor.CodeExecutor
	SkillsDir  string
	SkipReason string // 非空表示未启用技能，取值：disabled-by-env / no-skills-dir / no-python / <err>
}

// Enabled 返回是否成功装配。
func (b *Bundle) Enabled() bool {
	return b != nil && b.Repo != nil && b.Executor != nil
}

// Load 按 Config 装配技能。永不返回 error（环境问题走 SkipReason 降级）。
func Load(cfg Config) *Bundle {
	if cfg.SkillsDir == "" {
		cfg.SkillsDir = "./skills"
	}
	if cfg.WorkspaceDir == "" {
		cfg.WorkspaceDir = "./.skill_workspaces"
	}
	if cfg.EnableEnv == "" {
		cfg.EnableEnv = "SKILLS_ENABLE"
	}

	// 1) 显式开关检查：默认不启用，避免在生产意外执行脚本。
	if !isEnabled(cfg.EnableEnv) {
		return &Bundle{SkipReason: "disabled-by-env", SkillsDir: cfg.SkillsDir}
	}

	// 2) 目录存在性检查。
	absDir, err := filepath.Abs(cfg.SkillsDir)
	if err != nil {
		return &Bundle{SkipReason: fmt.Sprintf("abs-path: %v", err), SkillsDir: cfg.SkillsDir}
	}
	info, err := os.Stat(absDir)
	if err != nil || !info.IsDir() {
		return &Bundle{SkipReason: "no-skills-dir", SkillsDir: absDir}
	}

	// 3) Python 可用性检查（技能脚本都是 python3）。
	if !hasPython() {
		return &Bundle{SkipReason: "no-python", SkillsDir: absDir}
	}

	// 4) 构造 FSRepository。
	repo, err := skill.NewFSRepository(absDir)
	if err != nil {
		return &Bundle{SkipReason: fmt.Sprintf("new-fs-repo: %v", err), SkillsDir: absDir}
	}

	// 5) 构造 localexec（工作区目录按需创建）。
	absWork, err := filepath.Abs(cfg.WorkspaceDir)
	if err != nil {
		return &Bundle{SkipReason: fmt.Sprintf("abs-workspace: %v", err), SkillsDir: absDir}
	}
	_ = os.MkdirAll(absWork, 0o755)
	exec := localexec.New(
		localexec.WithWorkDir(absWork),
	)

	return &Bundle{
		Repo:      repo,
		Executor:  exec,
		SkillsDir: absDir,
	}
}

// isEnabled 读取环境变量：1/true/on/yes 视为启用。
func isEnabled(envKey string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(envKey))) {
	case "1", "true", "on", "yes":
		return true
	}
	return false
}

// hasPython 检查 python3（优先）或 python 是否可用。
func hasPython() bool {
	if _, err := exec.LookPath("python3"); err == nil {
		return true
	}
	if _, err := exec.LookPath("python"); err == nil {
		return true
	}
	return false
}
