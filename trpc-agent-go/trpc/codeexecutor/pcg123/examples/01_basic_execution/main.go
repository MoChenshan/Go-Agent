package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/codeexecutor/pcg123"
	"trpc.group/trpc-go/trpc-agent-go/codeexecutor"

	// Import the trpc-agent-go integration for code executor
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
)

func main() {
	fmt.Println("🚀 基础代码执行示例")
	fmt.Println("====================")

	// 从环境变量获取凭证
	secretID := os.Getenv("PCG123_SECRET_ID")
	secretKey := os.Getenv("PCG123_SECRET_KEY")

	if secretID == "" || secretKey == "" {
		log.Fatal("❌ 请设置环境变量 PCG123_SECRET_ID 和 PCG123_SECRET_KEY")
	}

	// 创建配置
	conf := pcg123.Config{
		Language:  pcg123.LanguagePython310,
		SecretID:  secretID,
		SecretKey: secretKey,
	}

	// 创建执行器
	executor, cancel, err := pcg123.NewCodeExecutor(conf,
		// 推荐使用共享执行器用于测试
		pcg123.WithShared(true))
	if err != nil {
		log.Fatalf("❌ 创建执行器失败: %v", err)
	}
	defer cancel()

	ctx := context.Background()

	// 示例1: 简单计算
	fmt.Println("\n📝 执行简单计算...")
	result1, err := executor.ExecuteCode(ctx, codeexecutor.CodeExecutionInput{
		CodeBlocks: []codeexecutor.CodeBlock{
			{Code: "print('Hello, PCG123!')"},
			{Code: "result = 2 + 3 * 4\nprint(f'计算结果: {result}')"},
		},
	})
	if err != nil {
		log.Printf("❌ 执行失败: %v", err)
	} else {
		fmt.Print(result1.Output)
	}

	// 示例2: 字符串操作
	fmt.Println("📝 执行字符串操作...")
	result2, err := executor.ExecuteCode(ctx, codeexecutor.CodeExecutionInput{
		CodeBlocks: []codeexecutor.CodeBlock{
			{Code: `
text = "Hello World"
print(f"原始文本: {text}")
print(f"大写: {text.upper()}")
print(f"单词数量: {len(text.split())}")
			`},
		},
	})
	if err != nil {
		log.Printf("❌ 执行失败: %v", err)
	} else {
		fmt.Print(result2.Output)
	}

	// 示例3: 列表操作
	fmt.Println("📝 执行列表操作...")
	result3, err := executor.ExecuteCode(ctx, codeexecutor.CodeExecutionInput{
		CodeBlocks: []codeexecutor.CodeBlock{
			{Code: `
numbers = [1, 2, 3, 4, 5]
print(f"原始列表: {numbers}")

# 列表推导式
squares = [x**2 for x in numbers]
print(f"平方后: {squares}")

# 内置函数
total = sum(squares)
print(f"总和: {total}")
			`},
		},
	})
	if err != nil {
		log.Printf("❌ 执行失败: %v", err)
	} else {
		fmt.Print(result3.Output)
	}

	fmt.Println("✅ 基础代码执行完成！")
}
