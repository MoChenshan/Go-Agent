// Package main 是 GameOps Agent 的启动入口。
//
// 本入口支持两种模式：
//
//	HTTP 模式（默认）：
//	  启动 HTTP 服务，监听 -addr（默认 :8080），暴露 POST /v1/agent SSE 流式接口。
//
//	CLI 模式：
//	  启动交互式终端（-cli），在命令行逐条对话，便于本地联调模型与 Agent。
//
// 使用示例：
//
//	go run . -model hunyuan-turbo-s
//	go run . -cli
//	go run . -config config.yaml -addr :9090
//
// D1 阶段不引入 tRPC 服务注册，使用标准 net/http 启动，保证本地零依赖可运行。
// D8 起在 A2A / AGUI 完整接入后，再切换到 `trpc.NewServer()` 模式。
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"

	"git.woa.com/trpc-go/gameops-agent/src/app"
	"git.woa.com/trpc-go/gameops-agent/src/config"
	"git.woa.com/trpc-go/gameops-agent/src/observability"
	"git.woa.com/trpc-go/gameops-agent/src/services/sse"
)

var (
	flagConfig = flag.String("config", "", "YAML 配置文件路径（留空使用默认配置 + 环境变量）")
	flagAddr   = flag.String("addr", ":8080", "HTTP 监听地址")
	flagModel  = flag.String("model", "", "覆盖配置中的 LLM 模型名称")
	flagCLI    = flag.Bool("cli", false, "启用交互式终端模式（调试用）")
	flagDebug  = flag.Bool("debug", false, "开启调试模式（输出工具参数、Token 用量等）")
)

// loadDotEnv 从同目录 .env 文件加载环境变量（不覆盖已存在的）。
// docker compose 会自动加载 .env，本地 go run 时需要手动加载。
func loadDotEnv() {
	f, err := os.Open(".env")
	if err != nil {
		return // 文件不存在不报错，兼容无 .env 的场景
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		// 不覆盖已存在的环境变量（shell 显式设置的优先级更高）
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
}

func main() {
	loadDotEnv()
	flag.Parse()

	cfg, err := config.Load(*flagConfig)
	if err != nil {
		log.Fatalf("load config failed: %v", err)
	}
	if *flagModel != "" {
		cfg.Model.Name = *flagModel
	} else if v := os.Getenv("MODEL_NAME"); v != "" {
		cfg.Model.Name = v
	}
	if *flagDebug {
		cfg.Debug = true
	}
	// 流式输出默认开启
	cfg.Gen.Stream = true

	ctx, cancelRoot := context.WithCancel(context.Background())
	defer cancelRoot()

	// D16：OpenTelemetry 可观测性初始化（Tracer/Meter）。
	// 全程按环境变量开关，OTEL_ENABLED!=true 时走 Noop Provider，零侵入。
	otelProvider, err := observability.Init(ctx, observability.Config{
		Logger: func(format string, args ...any) {
			log.Printf(format, args...)
		},
	})
	if err != nil {
		log.Fatalf("init observability failed: %v", err)
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := otelProvider.Shutdown(shutdownCtx); err != nil {
			log.Printf("otel shutdown: %v", err)
		}
	}()

	application, err := app.Init(ctx, cfg)
	if err != nil {
		log.Fatalf("init app failed: %v", err)
	}

	banner(cfg)

	if *flagCLI {
		runCLI(ctx, application)
		return
	}
	runHTTP(ctx, application, *flagAddr, cancelRoot)
}

// banner 打印启动横幅。
func banner(cfg *config.Config) {
	fmt.Println("========================================")
	fmt.Println("  GameOps Agent — D1 骨架")
	fmt.Printf("  Model : %s\n", cfg.Model.Name)
	fmt.Printf("  Debug : %v\n", cfg.Debug)
	fmt.Println("========================================")
}

// runHTTP 启动 HTTP 服务。
//
// cancelRoot 由调用方传入，graceful shutdown 完成后会调用它，
// 触发 main 中其他 defer（OTel flush 等）按序执行。
func runHTTP(ctx context.Context, a *app.App, addr string, cancelRoot context.CancelFunc) {
	mux := http.NewServeMux()

	// 健康检查
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// SSE 流式 Agent 接口（与 AG-UI 共享同一 session）
	sseSvc := sse.New("gameops-agent", a.Entrance, a.Session, a.Cfg.Debug)
	mux.HandleFunc("/v1/agent", sseSvc.HandleSSE)

	// D11：AG-UI Web 前端（浏览器直访 /agui 即可对话）
	//   - stub 构建（默认）：Enabled()=false，跳过 Mount 不影响 HTTP 启动
	//   - 真实构建（-tags agui）：挂载 /agui 端点
	if a.AGUI != nil && a.AGUI.Enabled() {
		if err := a.AGUI.Mount(mux); err != nil {
			log.Fatalf("mount agui: %v", err)
		}
	}

	// D15：Webhook 入口 + 报告查询端点
	//   - POST /webhook/bk_alarm  — 蓝鲸告警
	//   - POST /webhook/tapd      — TAPD 新单 / 状态变更
	//   - GET  /v1/report/{case_id}?format=markdown|json — 拉回自动生成的报告
	if a.Webhook != nil {
		a.Webhook.Mount(mux)
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		// IdleTimeout 给 SSE 长连接留够空间，HITL 长会话可能连接 >5min
		IdleTimeout: 10 * time.Minute,
	}

	// ---------------- Graceful Shutdown ----------------
	//
	// 关闭顺序（每一步都有独立超时，避免某一步卡住整体）：
	//   1. SIGINT/SIGTERM 触发 → 拒绝新请求（srv.Shutdown）
	//   2. 等 in-flight HTTP 请求完成（30s 总预算）
	//   3. 等待 async.Runner inflight 任务（已通过 a.Shutdown 链路）
	//   4. flush audit / OTel（在 main 末尾的 defer 链中）
	//
	// HITL 等待中的会话由 Redis Session 持久化，重启后新副本可继续。
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		sig := <-sigCh
		fmt.Printf("\n收到信号 %v，开始优雅关闭...\n", sig)

		// Step 1: 停止接受新连接
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer shutdownCancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Printf("[shutdown] http.Shutdown error: %v", err)
		} else {
			fmt.Println("[shutdown] HTTP server stopped accepting new connections")
		}

		// Step 2: 通知 application 关闭依赖（async runner / audit sink / 缓存等）
		if a != nil {
			appCtx, appCancel := context.WithTimeout(context.Background(), 20*time.Second)
			a.Shutdown(appCtx)
			appCancel()
			fmt.Println("[shutdown] application resources released")
		}

		// 触发 main 退出（main 中 defer otel shutdown 会随之执行）
		cancelRoot()
	}()

	fmt.Printf("🌐 HTTP listening on %s\n", addr)
	fmt.Printf("   - POST %s/v1/agent   (SSE 流式对话)\n", addr)
	if a.AGUI != nil && a.AGUI.Enabled() {
		fmt.Printf("   - ALL  %s%s        (AG-UI Web 前端)\n", addr, a.AGUI.Path())
	}
	fmt.Printf("   - GET  %s/healthz    (健康检查)\n", addr)
	if a.Webhook != nil {
		fmt.Printf("   - POST %s/webhook/bk_alarm (蓝鲸告警 Webhook)\n", addr)
		fmt.Printf("   - POST %s/webhook/tapd     (TAPD Webhook)\n", addr)
		fmt.Printf("   - GET  %s/v1/report/{case_id}?format=markdown|json\n", addr)
	}
	if a.A2A != nil && a.A2A.Enabled() {
		fmt.Printf("   - A2A  service=%s (需通过 trpc.NewServer 注册)\n", a.A2A.ServiceName())
	}
	fmt.Println()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
	_ = ctx
}

// runCLI 启动交互式终端模式，便于本地联调。
func runCLI(ctx context.Context, a *app.App) {
	// 复用 App.Session，保证 CLI 多轮对话可记忆。
	var r runner.Runner
	if a.Session != nil {
		r = runner.NewRunner("gameops-agent-cli", a.Entrance, runner.WithSessionService(a.Session))
	} else {
		r = runner.NewRunner("gameops-agent-cli", a.Entrance)
	}
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	userID := "cli_user"
	sessionID := fmt.Sprintf("cli_session_%d", time.Now().Unix())

	fmt.Println("🤖 CLI 模式已启动，输入 /exit 退出。")
	for {
		fmt.Print("\n👤 你: ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if input == "/exit" || input == "/quit" {
			fmt.Println("👋 再见！")
			return
		}

		eventCh, err := r.Run(ctx, userID, sessionID, model.NewUserMessage(input))
		if err != nil {
			fmt.Printf("❌ Runner error: %v\n", err)
			continue
		}

		fmt.Print("🤖 Agent: ")
		for ev := range eventCh {
			printEvent(ev)
			if ev.Done && ev.Author == a.Entrance.Info().Name {
				fmt.Println()
				break
			}
		}
	}
	if err := scanner.Err(); err != nil {
		log.Printf("scanner error: %v", err)
	}
}

// printEvent 把流式事件的内容增量打印到终端。
func printEvent(ev *event.Event) {
	if ev == nil {
		return
	}
	if ev.Error != nil {
		fmt.Printf("\n❌ Error: %s\n", ev.Error.Message)
		return
	}
	if len(ev.Choices) == 0 {
		return
	}
	if delta := ev.Choices[0].Delta.Content; delta != "" {
		fmt.Print(delta)
	}
}
