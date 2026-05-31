// Package main demonstrates using PCG123 remote executor with Agent Skills.
// This example shows how to integrate PCG123 executor into the Skills framework.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	"github.com/google/uuid"
	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/codeexecutor"
	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/log"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/skill"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/codeexecutor/pcg123"
)

var (
	flagModel    = flag.String("model", "deepseek-chat", "LLM model name")
	flagStream   = flag.Bool("stream", true, "enable streaming")
	flagSkills   = flag.String("skills", "", "skills root directory")
	flagLogLevel = flag.String("level", "info", "log level")
)

const appName = "pcg123-skill-demo"

const instructionText = `你是一个智能助手，可以在需要时使用 Agent Skills 来完成复杂任务。

<工作流程>
1. 根据需求加载所需技能
2. 组合工具完成任务
3. 输出最终结果
</工作流程>

<注意事项>
1. 在 skill 工作空间中，将 inputs/ 和 work/inputs/ 视为只读的文件视图，除非 skill 文档说明它们是可写的。
   不要在 inputs/ 或 work/inputs/ 下创建、移动或修改文件。
2. 执行 skills 时生成的临时、最终制品(文件、图片等)，请输出到 out/（或 $OUTPUT_DIR）中，以确保能否完成工作区结果的收集。
3. 当链式组合多个 skills 时，直接从 out/（或 $OUTPUT_DIR）读取之前的结果，并将新文件写回 out/。
   尽可能使用 skill_run 的 inputs/outputs 字段来映射文件，而不是使用像 cp 或 mv 这样的 shell 命令。
</注意事项>
`

var delimiter = codeexecutor.CodeBlockDelimiter{
	Start: "```",
	End:   "```",
}

func main() {
	flag.Parse()

	secretID := os.Getenv("PCG123_SECRET_ID")
	secretKey := os.Getenv("PCG123_SECRET_KEY")
	if secretID == "" || secretKey == "" {
		fmt.Println("请设置环境变量 PCG123_SECRET_ID 和 PCG123_SECRET_KEY")
		os.Exit(1)
	}

	log.SetLevel(*flagLogLevel)

	sampleDataPath, err := prepareSampleData()
	if err != nil {
		fmt.Printf("生成样本数据失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("已生成本地样本数据: %s\n", sampleDataPath)
	fmt.Printf("可通过 host://%s 在 skill inputs 中引用\n\n", sampleDataPath)

	skillsRoot := *flagSkills
	if skillsRoot == "" {
		if s := os.Getenv("SKILLS_ROOT"); s != "" {
			skillsRoot = s
		} else {
			cwd, _ := os.Getwd()
			skillsRoot = filepath.Join(cwd, "skills")
		}
	}

	chat := &skillChat{
		modelName:  *flagModel,
		stream:     *flagStream,
		skillsRoot: skillsRoot,
		secretID:   secretID,
		secretKey:  secretKey,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	defer cancel()

	if err := chat.run(ctx); err != nil {
		fmt.Printf("❌ Error: %v\n", err)
	}

	if chat.cleanup != nil {
		chat.cleanup()
	}
}

type skillChat struct {
	modelName  string
	stream     bool
	skillsRoot string
	secretID   string
	secretKey  string
	runner     runner.Runner
	userID     string
	sessionID  string
	cleanup    func()
}

func (c *skillChat) run(ctx context.Context) error {
	if err := c.setup(ctx); err != nil {
		return err
	}

	return c.startChat(ctx)
}

func (c *skillChat) setup(_ context.Context) error {
	mdl := openai.New(c.modelName)

	repo, err := skill.NewFSRepository(c.skillsRoot)
	if err != nil {
		return fmt.Errorf("skills repo: %w", err)
	}

	cfg := pcg123.Config{
		Language:  pcg123.LanguagePython310,
		SecretID:  c.secretID,
		SecretKey: c.secretKey,
	}

	// 默认懒初始化：首次 skill_run / 直跑代码块到达时再向 123 申请沙箱。
	// ReconnectMode 负责的是「已经申请成功之后」的健康修复，与懒初始化正交。
	executor, cancel, err := pcg123.NewCodeExecutor(cfg,
		pcg123.WithExecuteTimeout(60*time.Second),
		pcg123.WithIdleTimeout(15*time.Minute),
		pcg123.WithShared(true),
		pcg123.WithCodeBlockDelimiter(delimiter),
		pcg123.WithReconnectMode(pcg123.ReconnectLazy),
		pcg123.WithProbeTimeout(time.Second*2),
		pcg123.WithProbeInterval(time.Second),
		pcg123.WithMaxFailedProbes(3),
	)
	if err != nil {
		return fmt.Errorf("create code executor: %w", err)
	}

	c.cleanup = cancel

	gen := model.GenerationConfig{
		MaxTokens:   intPtr(50000),
		Temperature: floatPtr(0.4),
		Stream:      c.stream,
	}

	llm := llmagent.New("pcg123-skills-agent",
		llmagent.WithModel(mdl),
		llmagent.WithDescription("使用 PCG123 远程执行器运行 Agent Skills"),
		llmagent.WithInstruction(instructionText),
		llmagent.WithGenerationConfig(gen),
		llmagent.WithSkills(repo),
		llmagent.WithCodeExecutor(executor))

	c.runner = runner.NewRunner(appName, llm)
	c.userID = "user"
	c.sessionID = fmt.Sprintf("session-%d", time.Now().Unix())

	fmt.Println("🚀 PCG123 Skills Demo")
	fmt.Printf("Model: %s\n", c.modelName)
	fmt.Printf("Skills: %s\n", c.skillsRoot)
	fmt.Printf("Executor: pcg123 (remote, lazy-init; 首条会话触发时申请沙箱)\n")
	fmt.Println(strings.Repeat("=", 50))
	fmt.Println("提示:")
	fmt.Println(" - 输入 '列出技能' 查看可用技能")
	fmt.Println(" - 输入 /exit 退出")
	fmt.Println()

	return nil
}

func (c *skillChat) startChat(ctx context.Context) error {
	in := bufio.NewScanner(os.Stdin)
	for {
		select {
		case <-ctx.Done():
			fmt.Println("\n⚠️  收到取消信号，退出聊天")
			return ctx.Err()
		default:
		}

		fmt.Print("👤 You: ")
		if !in.Scan() {
			break
		}
		text := strings.TrimSpace(in.Text())
		if text == "" {
			continue
		}
		if strings.EqualFold(text, "/exit") {
			fmt.Println("👋 Bye!")
			return nil
		}
		if err := c.processMessage(ctx, text); err != nil {
			fmt.Printf("❌ Error: %v\n", err)
		}
		fmt.Println()
	}
	return in.Err()
}

func (c *skillChat) processMessage(ctx context.Context, userMessage string) error {
	msg := model.NewUserMessage(userMessage)
	reqID := uuid.New().String()
	ch, err := c.runner.Run(ctx, c.userID, c.sessionID, msg, agent.WithRequestID(reqID))
	if err != nil {
		return err
	}
	return c.processResponse(ch)
}

func (c *skillChat) processResponse(ch <-chan *event.Event) error {
	fmt.Print("🤖 Assistant: ")
	var (
		toolCalls bool
		started   bool
	)
	for ev := range ch {
		if ev.Error != nil {
			fmt.Printf("\n❌ Error: %s\n", ev.Error.Message)
			continue
		}

		if len(ev.Response.Choices) > 0 &&
			len(ev.Response.Choices[0].Message.ToolCalls) > 0 {
			toolCalls = true
			if started {
				fmt.Println()
			}
			fmt.Println("🔧 工具调用:")
			for _, tc := range ev.Response.Choices[0].Message.ToolCalls {
				argsStr := string(tc.Function.Arguments)
				fmt.Printf("   • %s(%s)\n", tc.Function.Name, truncate(argsStr, 2000))
			}
			fmt.Println("🔄 执行中...")
			continue
		}

		if ev.Response != nil && len(ev.Response.Choices) > 0 {
			for _, ch := range ev.Response.Choices {
				if ch.Message.Role == model.RoleTool && ch.Message.ToolID != "" {
					fmt.Printf("✅ 工具结果: %s\n", truncate(strings.TrimSpace(ch.Message.Content), 2000))
				}
			}
		}

		if len(ev.Response.Choices) == 0 {
			continue
		}

		choice := ev.Response.Choices[0]
		var content string
		if c.stream {
			content = choice.Delta.Content
		} else {
			content = choice.Message.Content
		}

		if content != "" {
			if !started {
				if toolCalls {
					fmt.Print("\n🤖 Assistant: ")
				}
				started = true
			}
			fmt.Print(content)
		}

		if ev.IsFinalResponse() {
			fmt.Println()
			break
		}
	}
	return nil
}

func prepareSampleData() (string, error) {
	dir, err := os.MkdirTemp("", "pcg123-host-demo")
	if err != nil {
		return "", err
	}

	csvPath := filepath.Join(dir, "sample_data.csv")
	f, err := os.Create(csvPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	header := "user_id,timestamp,action,category,value,rating\n"
	if _, err := f.WriteString(header); err != nil {
		return "", err
	}

	rows := []string{
		"user_1,2026-03-01 10:00:00,view,electronics,42.50,3\n",
		"user_2,2026-03-01 11:30:00,click,clothing,15.00,5\n",
		"user_3,2026-03-02 09:15:00,purchase,books,128.90,4\n",
		"user_1,2026-03-02 14:00:00,add_to_cart,home,67.20,\n",
		"user_4,2026-03-03 08:45:00,purchase,sports,299.00,5\n",
		"user_2,2026-03-03 16:20:00,view,electronics,88.00,2\n",
		"user_5,2026-03-04 12:00:00,click,clothing,33.50,4\n",
		"user_3,2026-03-05 10:30:00,purchase,electronics,450.00,5\n",
		"user_6,2026-03-05 15:45:00,remove_from_cart,home,22.10,1\n",
		"user_4,2026-03-06 09:00:00,view,books,55.00,3\n",
	}
	for _, row := range rows {
		if _, err := f.WriteString(row); err != nil {
			return "", err
		}
	}

	return csvPath, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func intPtr(i int) *int           { return &i }
func floatPtr(f float64) *float64 { return &f }
