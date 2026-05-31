// Command preflight 是 GameOps Agent 的就绪度自检命令。
//
// 用法：
//
//	go run ./src/cmd/preflight               # 打印状态，始终返回 0
//	go run ./src/cmd/preflight -strict       # 严格模式：任何 Mock 均返回非 0
//	                                         # 可用于 K8s livenessProbe
//
// 示例输出：
//
//	✅  LLM Model        REAL       [base=http://hunyuanapi.woa.com/openapi/v1]
//	✅  审计日志          REAL       [sink=stdout]
//	────────────────────────────────────────────────────────────────
//	🟡  蓝鲸监控          MOCK       [缺: BK_APP_CODE,BK_APP_SECRET]
//	✅  BCS 容器          REAL
//	🟡  工蜂 Git          MOCK       [缺: GONGFENG_TOKEN]
//	...
package main

import (
	"flag"
	"fmt"
	"os"

	"git.woa.com/trpc-go/gameops-agent/src/preflight"
)

func main() {
	strict := flag.Bool("strict", false, "严格模式：任何 MOCK/DISABLED 平台都会使命令以非 0 退出")
	flag.Parse()

	rpt := preflight.Run()
	ok := rpt.Print(os.Stdout)

	if *strict && !ok {
		fmt.Fprintln(os.Stderr, "[preflight] strict 模式：存在非 REAL 平台，退出码 1")
		os.Exit(1)
	}
}
