//go:build !eval

// Package main 的 stub 构建：默认路径，不引入 trpc-agent-go/evaluation 独立 module。
//
// 真实评测链路需要 `go build -tags eval -o bin/evalrun ./eval/cmd/evalrun`，
// 避免默认 `go build ./...` 拖入远程模块依赖（对内网/离线 CI 不友好）。
package main

import (
	"fmt"
	"os"

	"git.woa.com/trpc-go/gameops-agent/eval"
)

func main() {
	path := eval.DefaultDataDir() + "/" + eval.DefaultEvalSetID + "/" + eval.DefaultEvalSetID + ".evalset.json"
	set, err := eval.LoadEvalSet(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ load evalset failed: %v\n", err)
		os.Exit(1)
	}
	sum := set.Summarize()

	fmt.Println("┌──────────────────────────────────────────────┐")
	fmt.Println("│  GameOps Agent — Evaluation CLI (stub)       │")
	fmt.Println("└──────────────────────────────────────────────┘")
	fmt.Printf("EvalSet     : %s (%s)\n", set.EvalSetID, set.Name)
	fmt.Printf("Cases       : %d\n", sum.CaseCount)
	fmt.Printf("Invocations : %d\n", sum.InvCount)
	fmt.Printf("Tool Calls  : %d\n", sum.ToolCalls)
	fmt.Printf("Tool Names  : %v\n", sum.ToolNames)
	fmt.Println()
	fmt.Println("ℹ 当前为 stub 构建，仅做 golden set 结构校验，未执行真实 Agent 推理。")
	fmt.Println("ℹ 要运行真实评测（Agent 推理 + Metric 打分），请：")
	fmt.Println("   go build -tags eval -o bin/evalrun ./eval/cmd/evalrun")
	fmt.Println("   ./bin/evalrun --eval-set gameops-core")
}
