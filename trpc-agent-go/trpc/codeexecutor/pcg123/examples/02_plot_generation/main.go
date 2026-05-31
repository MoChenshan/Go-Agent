package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/codeexecutor/pcg123"
	"trpc.group/trpc-go/trpc-agent-go/codeexecutor"

	// Import the trpc-agent-go integration for code executor
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
)

// saveImageToPNG 将base64编码的图片保存为PNG文件
func saveImageToPNG(filename, base64Content string) error {
	// 解码base64内容
	data, err := base64.StdEncoding.DecodeString(base64Content)
	if err != nil {
		return fmt.Errorf("解码base64失败: %v", err)
	}

	// 写入文件
	err = os.WriteFile(filename, data, 0644)
	if err != nil {
		return fmt.Errorf("写入文件失败: %v", err)
	}

	return nil
}

func main() {
	fmt.Println("📊 图形绘制示例")
	fmt.Println("================")

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
	totalImages := 0

	// 示例1: 正弦函数图像
	fmt.Println("\n📈 绘制正弦函数图像...")
	result1, err := executor.ExecuteCode(ctx, codeexecutor.CodeExecutionInput{
		CodeBlocks: []codeexecutor.CodeBlock{
			{Code: `
import matplotlib.pyplot as plt
import numpy as np

# 生成数据
x = np.linspace(0, 10, 100)
y = np.sin(x)

# 创建图表
plt.figure(figsize=(10, 6))
plt.plot(x, y, 'b-', linewidth=2, label='sin(x)')
plt.title('sine wave', fontsize=14)
plt.xlabel('x')
plt.ylabel('sin(x)')
plt.grid(True, alpha=0.3)
plt.legend()
plt.show()
			`},
		},
	})
	if err != nil {
		log.Printf("❌ 执行失败: %v", err)
	} else {
		for i, file := range result1.OutputFiles {
			filename := fmt.Sprintf("sine_wave_%d.png", i)
			err := saveImageToPNG(filename, file.Content)
			if err != nil {
				log.Printf("❌ 保存图片失败: %v", err)
			} else {
				fmt.Printf("📸 生成图像: %s -> 保存为: %s (类型: %s)\n", file.Name, filename, file.MIMEType)
				totalImages++
			}
		}
	}

	// 示例2: 数据分析图表
	fmt.Println("\n📊 绘制数据分析图表...")
	result2, err := executor.ExecuteCode(ctx, codeexecutor.CodeExecutionInput{
		CodeBlocks: []codeexecutor.CodeBlock{
			{Code: `
import matplotlib.pyplot as plt
import pandas as pd
import numpy as np

# 生成模拟数据
np.random.seed(42)
data = pd.DataFrame({
    'values': np.random.randint(1, 101, 100)
})

print("数据统计:")
print(data.describe())

# 创建直方图
plt.figure(figsize=(12, 5))

plt.subplot(1, 2, 1)
plt.hist(data['values'], bins=20, color='skyblue', alpha=0.7, edgecolor='black')
plt.title('histogram')
plt.xlabel('values')
plt.ylabel('frequency')

plt.subplot(1, 2, 2)
plt.scatter(range(len(data)), data['values'], alpha=0.6, color='coral')
plt.title('scatter plot')
plt.xlabel('index')
plt.ylabel('values')

plt.tight_layout()
plt.show()
			`},
		},
	})
	if err != nil {
		log.Printf("❌ 执行失败: %v", err)
	} else {
		for i, file := range result2.OutputFiles {
			filename := fmt.Sprintf("data_analysis_%d.png", i)
			err := saveImageToPNG(filename, file.Content)
			if err != nil {
				log.Printf("❌ 保存图片失败: %v", err)
			} else {
				fmt.Printf("📸 生成图像: %s -> 保存为: %s (类型: %s)\n", file.Name, filename, file.MIMEType)
				totalImages++
			}
		}
	}

	// 示例3: 多函数对比图
	fmt.Println("\n📈 绘制多函数对比图...")
	result3, err := executor.ExecuteCode(ctx, codeexecutor.CodeExecutionInput{
		CodeBlocks: []codeexecutor.CodeBlock{
			{Code: `
import matplotlib.pyplot as plt
import numpy as np

# 生成数据
x = np.linspace(0, 2*np.pi, 100)

# 创建子图
fig, ((ax1, ax2), (ax3, ax4)) = plt.subplots(2, 2, figsize=(12, 10))

# 正弦函数
ax1.plot(x, np.sin(x), 'b-', linewidth=2)
ax1.set_title('sin(x)')
ax1.grid(True, alpha=0.3)

# 余弦函数
ax2.plot(x, np.cos(x), 'r-', linewidth=2)
ax2.set_title('cos(x)')
ax2.grid(True, alpha=0.3)

# 正切函数
ax3.plot(x, np.tan(x), 'g-', linewidth=2)
ax3.set_title('tan(x)')
ax3.set_ylim(-5, 5)
ax3.grid(True, alpha=0.3)

# 指数函数
ax4.plot(x, np.exp(x/3), 'm-', linewidth=2)
ax4.set_title('exp(x/3)')
ax4.grid(True, alpha=0.3)

plt.tight_layout()
plt.show()
			`},
		},
	})
	if err != nil {
		log.Printf("❌ 执行失败: %v", err)
	} else {
		for i, file := range result3.OutputFiles {
			filename := fmt.Sprintf("multi_function_%d.png", i)
			err := saveImageToPNG(filename, file.Content)
			if err != nil {
				log.Printf("❌ 保存图片失败: %v", err)
			} else {
				fmt.Printf("📸 生成图像: %s -> 保存为: %s (类型: %s)\n", file.Name, filename, file.MIMEType)
				totalImages++
			}
		}
	}

	fmt.Printf("\n✅ 图形绘制完成！生成了 %d 个图像文件\n", totalImages)
}
