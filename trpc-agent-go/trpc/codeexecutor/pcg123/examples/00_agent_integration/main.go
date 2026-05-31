package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/codeexecutor/pcg123"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"

	// Import the trpc-agent-go integration for code executor
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
)

func main() {
	fmt.Println("🚀 Agent框架 + PCG123代码执行器集成示例")
	fmt.Println("==========================================")

	// 读取命令行配置
	modelName := flag.String("model", "deepseek-chat", "使用的模型名称")
	flag.Parse()

	fmt.Printf("🔧 配置信息:\n")
	fmt.Printf("- 模型名称: %s\n", *modelName)
	fmt.Printf("- OpenAI SDK 将自动从环境变量读取 OPENAI_API_KEY 和 OPENAI_BASE_URL\n")
	fmt.Printf("- PCG123 将从环境变量读取 PCG123_SECRET_ID 和 PCG123_SECRET_KEY\n")
	fmt.Println()

	// 检查PCG123凭证
	secretID := os.Getenv("PCG123_SECRET_ID")
	secretKey := os.Getenv("PCG123_SECRET_KEY")
	if secretID == "" || secretKey == "" {
		log.Fatal("❌ 请设置环境变量 PCG123_SECRET_ID 和 PCG123_SECRET_KEY")
	}

	// 装配 PCG123 代码执行器：默认懒初始化，
	// 直到 Agent 首次需要跑代码块时才向 123 平台申请沙箱容器
	fmt.Println("🔧 装配PCG123代码执行器(默认懒初始化, 首次代码块到达时再申请沙箱)...")
	pcgConf := pcg123.Config{
		Language:  pcg123.LanguagePython310,
		SecretID:  secretID,
		SecretKey: secretKey,
	}

	codeExecutor, cancel, err := pcg123.NewCodeExecutor(pcgConf,
		// 推荐使用共享执行器用于测试
		pcg123.WithShared(true))
	if err != nil {
		log.Fatalf("❌ 装配PCG123执行器失败: %v", err)
	}
	defer cancel()

	// 创建模型实例
	fmt.Println("🤖 创建OpenAI模型实例...")
	modelInstance := openai.New(*modelName)

	// 创建生成配置
	genConfig := model.GenerationConfig{
		MaxTokens:   intPtr(2000),
		Temperature: floatPtr(0.7),
		Stream:      true,
	}

	// 创建具有代码执行能力的LLM Agent
	fmt.Println("🧠 创建具有代码执行能力的LLM Agent...")
	agentName := "pcg123_data_analyst"
	llmAgent := llmagent.New(
		agentName,
		llmagent.WithModel(modelInstance),
		llmagent.WithDescription("具有PCG123代码执行能力的数据分析助手"),
		llmagent.WithInstruction(baseSystemInstruction()),
		llmagent.WithGenerationConfig(genConfig),
		llmagent.WithCodeExecutor(codeExecutor),
	)

	// 创建运行器
	fmt.Println("🏃 创建Agent运行器...")
	r := runner.NewRunner(agentName, llmAgent)

	// 定义测试任务
	userQuery := "请分析这组销售数据并生成报告：[120, 150, 180, 200, 175, 220, 195, 240, 210, 185]。包括基本统计信息、趋势分析(基于折线图)，并计算预测下个月的销售额。"

	fmt.Printf("📝 用户查询: %s\n\n", userQuery)

	// 执行Agent任务
	fmt.Println("🔄 开始执行Agent任务...")
	eventChan, err := r.Run(context.Background(), "demo-user", "demo-session", model.NewUserMessage(userQuery))
	if err != nil {
		log.Fatalf("❌ 运行Agent失败: %v", err)
	}

	fmt.Println("📊 处理Agent事件流:")
	fmt.Println("=" + fmt.Sprintf("%*s", 50, "="))

	// 处理事件流
	eventCount := 0
	var finalResponse string

	for event := range eventChan {
		eventCount++

		// 显示事件基本信息
		if eventCount == 1 {
			fmt.Printf("🎯 Agent ID: %s\n", event.Author)
			fmt.Printf("🔗 调用ID: %s\n", event.InvocationID)
		}

		// 处理错误
		if event.Error != nil {
			fmt.Printf("❌ 错误: %s (类型: %s)\n", event.Error.Message, event.Error.Type)
			continue
		}

		// 处理消息内容
		if len(event.Choices) > 0 {
			choice := event.Choices[0]

			// 累积最终响应
			if choice.Message.Content != "" {
				finalResponse += choice.Message.Content
			}
			if choice.Delta.Content != "" {
				fmt.Print(choice.Delta.Content) // 实时显示流式输出
				finalResponse += choice.Delta.Content
			}

			// 显示完成原因
			if choice.FinishReason != nil {
				fmt.Printf("\n\n🏁 完成原因: %s\n", *choice.FinishReason)
			}
		}

		// 显示Token使用情况
		if event.Usage != nil {
			fmt.Printf("\n📈 Token使用统计:\n")
			fmt.Printf("  - 提示Token: %d\n", event.Usage.PromptTokens)
			fmt.Printf("  - 完成Token: %d\n", event.Usage.CompletionTokens)
			fmt.Printf("  - 总Token: %d\n", event.Usage.TotalTokens)
		}

		// 检查是否完成
		if event.Done {
			fmt.Println("\n✅ Agent执行完成!")
			break
		}
	}

	// 显示执行摘要
	fmt.Println("\n" + "=" + fmt.Sprintf("%*s", 50, "="))
	fmt.Printf("📋 执行摘要:\n")
	fmt.Printf("- 总事件数: %d\n", eventCount)
	fmt.Printf("- Agent名称: %s\n", agentName)
	fmt.Printf("- 代码执行器: PCG123 (Python 3.10)\n")
	fmt.Printf("- 执行状态: %s\n", getStatusEmoji(eventCount > 0))

	if eventCount == 0 {
		fmt.Println("\n⚠️  未收到任何事件，可能的原因:")
		fmt.Println("- 模型配置问题")
		fmt.Println("- 网络连接问题")
		fmt.Println("- PCG123凭证问题")
		fmt.Println("- 请检查日志获取更多详细信息")
	}

	fmt.Println("\n🎉 Agent框架 + PCG123集成演示完成!")
}
func baseSystemInstruction() string {
	// Read content from instruction.md file.
	content, err := os.ReadFile("instruction.md")
	if err != nil {
		log.Printf("Failed to read instruction.md: %v", err)
		return ""
	}
	return string(content)
}

// getStatusEmoji 根据状态返回表情符号
func getStatusEmoji(success bool) string {
	if success {
		return "✅ 成功"
	}
	return "❌ 失败"
}

// intPtr 返回int指针
func intPtr(i int) *int {
	return &i
}

// floatPtr 返回float64指针
func floatPtr(f float64) *float64 {
	return &f
}
