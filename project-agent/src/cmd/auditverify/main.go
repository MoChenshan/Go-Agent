// Package main D17.6 — 离线审计日志验签 CLI。
//
// 用法：
//
//	auditverify --file /var/log/gameops/audit.log
//
// 环境变量（与 agent 进程完全一致）：
//
//	AUDIT_HMAC_KEY    主 kid 对应的 base64 key（验签时可不是"主"，只要匹配）
//	AUDIT_HMAC_KID    主 kid 名
//	AUDIT_HMAC_KEYS   历史 kid 集合 kid1:base64:xxx,kid2:base64:yyy
//
// 输出：
//
//	total=N verified=M failed=K skipped=S kid_stats={v1:100, v2:50}
//	chain_ok=true|false
//	（--verbose 时追加每行 ok/fail 明细）
//
// 设计要点：
//   - 独立 main package，不依赖 app / config / trpc-agent-go 重型栈；
//     合规机器可以只拷 binary 用；
//   - 链式校验是 best-effort：遇到 kid 未知的行当作"链锚点"重置，保证一批老密钥退役
//     后仍能跑完批次（否则第一条未知 kid 就让所有后续都 fail）。
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"

	"git.woa.com/trpc-go/gameops-agent/src/audit"
)

var (
	flagFile    = flag.String("file", "", "审计日志 JSONL 路径（'-' 表示 stdin）")
	flagVerbose = flag.Bool("verbose", false, "打印每行验签结果")
	flagChain   = flag.Bool("chain", false, "同时校验 prev_sig 链完整性")
)

func main() {
	flag.Parse()
	if *flagFile == "" {
		fmt.Fprintln(os.Stderr, "missing --file")
		flag.Usage()
		os.Exit(2)
	}

	signer, err := audit.NewHMACSignerFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "build signer: %v\n", err)
		os.Exit(2)
	}
	if signer == nil {
		fmt.Fprintln(os.Stderr,
			"no HMAC key configured (set AUDIT_HMAC_KEY)")
		os.Exit(2)
	}

	var rd *bufio.Scanner
	if *flagFile == "-" {
		rd = bufio.NewScanner(os.Stdin)
	} else {
		f, err := os.Open(*flagFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "open %s: %v\n", *flagFile, err)
			os.Exit(2)
		}
		defer f.Close()
		rd = bufio.NewScanner(f)
	}
	// 单行可能很长（Params 里带长字符串），默认 bufio 64KB 不够，调大到 1MB。
	rd.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var (
		total, verified, failed, skipped int
		kidStats                         = map[string]int{}
		chainOK                          = true
		prevSig                          string
	)

	lineNo := 0
	for rd.Scan() {
		lineNo++
		line := rd.Bytes()
		if len(line) == 0 {
			continue
		}
		total++

		// 先尝试解析（即使验签失败也想看看是什么内容）。
		var rec audit.Record
		if err := json.Unmarshal(line, &rec); err != nil {
			skipped++
			if *flagVerbose {
				fmt.Printf("line %d: SKIP (parse err: %v)\n", lineNo, err)
			}
			continue
		}
		// 无 sig 字段 = 遗留的未签名记录：不算 fail，单独算 skipped。
		if rec.Sig == "" {
			skipped++
			if *flagVerbose {
				fmt.Printf("line %d: SKIP (no sig; action=%s)\n", lineNo, rec.Action)
			}
			// 链中断：prev_sig 链仅对连续签名段有意义。
			prevSig = ""
			continue
		}

		if err := signer.Verify(&rec); err != nil {
			failed++
			if *flagVerbose {
				fmt.Printf("line %d: FAIL %v (action=%s, kid=%s)\n",
					lineNo, err, rec.Action, rec.SigKID)
			}
			// 链校验：本条 fail 不影响下条对 prev_sig 的判定 —— 但本条的 sig 不可信了，
			// 下一条 prev_sig 如果指向它也没意义，所以重置 prevSig。
			prevSig = ""
			continue
		}
		verified++
		kidStats[rec.SigKID]++

		// 链校验：仅在 --chain 开启且上一条也 verified 时才判定。
		if *flagChain && prevSig != "" && rec.PrevSig != prevSig {
			chainOK = false
			if *flagVerbose {
				fmt.Printf("line %d: CHAIN BROKEN "+
					"(prev_sig=%s, want=%s)\n",
					lineNo, short(rec.PrevSig), short(prevSig))
			}
		}
		prevSig = rec.Sig

		if *flagVerbose {
			fmt.Printf("line %d: OK action=%s kid=%s\n",
				lineNo, rec.Action, rec.SigKID)
		}
	}
	if err := rd.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "scan: %v\n", err)
		os.Exit(2)
	}

	fmt.Println("──── auditverify ────────────────────────────────────")
	fmt.Printf("total=%d verified=%d failed=%d skipped=%d\n",
		total, verified, failed, skipped)
	if len(kidStats) > 0 {
		ks := make([]string, 0, len(kidStats))
		for k := range kidStats {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		fmt.Print("kid_stats={")
		for i, k := range ks {
			if i > 0 {
				fmt.Print(", ")
			}
			fmt.Printf("%s:%d", k, kidStats[k])
		}
		fmt.Println("}")
	}
	if *flagChain {
		fmt.Printf("chain_ok=%v\n", chainOK)
	}

	// 退出码：failed>0 或 chain 校验打开但 chain 断裂 → exit 1。
	if failed > 0 || (*flagChain && !chainOK) {
		os.Exit(1)
	}
}

// short 截断 hex sig 便于日志可读（保留前 12 位）。
func short(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:12] + "..."
}
