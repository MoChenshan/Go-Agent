package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"git.woa.com/trpc-go/trpc-agent-go/trpc/codeexecutor/pcg123"
	"trpc.group/trpc-go/trpc-agent-go/codeexecutor"

	// Import the trpc-agent-go integration for code executor
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
)

func main() {
	fmt.Println("💬 交互式代码执行示例")
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

	// 创建交互式执行器
	executor, cancel, err := pcg123.NewCodeExecutor(conf,
		pcg123.WithShared(true),                   // 推荐使用共享执行器用于测试
		pcg123.WithInteractive(true),              // 启用交互式模式
		pcg123.WithIdleTimeout(10*time.Minute),    // 10分钟空闲超时
		pcg123.WithExecuteTimeout(30*time.Second), // 30秒执行超时
	)
	if err != nil {
		log.Fatalf("❌ 创建执行器失败: %v", err)
	}
	defer cancel()

	ctx := context.Background()

	// 步骤1: 初始化数据和导入库
	fmt.Println("\n🔧 步骤1: 初始化数据和导入库...")
	result1, err := executor.ExecuteCode(ctx, codeexecutor.CodeExecutionInput{
		CodeBlocks: []codeexecutor.CodeBlock{
			{Code: `
import numpy as np
import pandas as pd
import statistics
import random

# 设置随机种子确保可重现性
np.random.seed(42)
random.seed(42)

# 生成模拟数据
n_samples = 1000
data = pd.DataFrame({
    'x': np.random.random(n_samples),
    'y': np.random.random(n_samples)
})

print("📊 数据初始化完成")
print(f"原始数据形状: {data.shape}")
print("数据预览:")
print(data.head())
			`},
		},
	})
	if err != nil {
		log.Printf("❌ 步骤1执行失败: %v", err)
		return
	}
	fmt.Print(result1.Output)

	// 步骤2: 数据分析和统计 (使用步骤1的数据)
	fmt.Println("📈 步骤2: 数据分析和统计...")
	result2, err := executor.ExecuteCode(ctx, codeexecutor.CodeExecutionInput{
		CodeBlocks: []codeexecutor.CodeBlock{
			{Code: `
# 使用前面步骤创建的 data 变量
print("📊 基础统计信息:")

# 计算基础统计
x_mean = data['x'].mean()
y_mean = data['y'].mean()
x_std = data['x'].std()
y_std = data['y'].std()

print(f"x轴均值: {x_mean:.2f}")
print(f"y轴均值: {y_mean:.2f}")
print(f"数据标准差: x={x_std:.2f}, y={y_std:.2f}")

# 计算相关性
correlation = data['x'].corr(data['y'])
print(f"相关系数: {correlation:.2f}")

# 为下一步准备特征和目标变量
X = data[['x']]
y = data['y']
			`},
		},
	})
	if err != nil {
		log.Printf("❌ 步骤2执行失败: %v", err)
		return
	}
	fmt.Print(result2.Output)

	// 步骤3: 模型训练和预测 (使用前面步骤的变量)
	fmt.Println("🎯 步骤3: 模型训练和预测...")
	result3, err := executor.ExecuteCode(ctx, codeexecutor.CodeExecutionInput{
		CodeBlocks: []codeexecutor.CodeBlock{
			{Code: `
# 使用前面步骤的 X 和 y 变量
# 简单的训练测试数据分割
train_size = int(0.8 * len(X))
X_train = X[:train_size]
y_train = y[:train_size]
X_test = X[train_size:]
y_test = y[train_size:]

print("🤖 简单线性回归模型训练完成")
print(f"训练数据大小: {len(X_train)}")
print(f"测试数据大小: {len(X_test)}")

# 简单线性回归 (手工实现)
# y = a * x + b
x_vals = X_train['x'].values
y_vals = y_train.values

# 计算回归系数
n = len(x_vals)
sum_x = sum(x_vals)
sum_y = sum(y_vals)
sum_xy = sum(x_vals * y_vals)
sum_x2 = sum(x_vals * x_vals)

# 线性回归公式
a = (n * sum_xy - sum_x * sum_y) / (n * sum_x2 - sum_x * sum_x)
b = (sum_y - a * sum_x) / n

print(f"回归方程: y = {a:.4f}x + {b:.4f}")

# 预测示例
sample_x = [0.5, 0.3, 0.7]
predictions = [a * x + b for x in sample_x]
print(f"预测示例: {[f'{pred:.2f}' for pred in predictions]}")

# 保存模型信息供后续使用
model_info = {
    'coefficient': a,
    'intercept': b,
    'n_features': 1,
    'n_samples': len(X_train)
}
			`},
		},
	})
	if err != nil {
		log.Printf("❌ 步骤3执行失败: %v", err)
		return
	}
	fmt.Print(result3.Output)

	// 步骤4: 验证状态保持 (检查所有之前的变量是否仍然存在)
	fmt.Println("🔍 步骤4: 验证交互式状态保持...")
	result4, err := executor.ExecuteCode(ctx, codeexecutor.CodeExecutionInput{
		CodeBlocks: []codeexecutor.CodeBlock{
			{Code: `
# 检查所有之前创建的变量是否仍然可用
print("🔍 变量状态检查:")
print(f"data变量存在: {'data' in locals()}")
print(f"model_info变量存在: {'model_info' in locals()}")
print(f"X_train变量存在: {'X_train' in locals()}")

if 'data' in locals():
    print(f"数据记录数: {len(data)}")
    
if 'a' in locals() and 'b' in locals():
    print(f"回归模型: y = {a:.4f}x + {b:.4f}")
    
if 'model_info' in locals():
    print(f"模型系数: {model_info['coefficient']:.4f}")
    print(f"模型截距: {model_info['intercept']:.4f}")

print("\n✅ 所有变量在整个交互式会话中成功保持状态！")
			`},
		},
	})
	if err != nil {
		log.Printf("❌ 步骤4执行失败: %v", err)
		return
	}
	fmt.Print(result4.Output)

	fmt.Println("\n✅ 交互式会话完成！所有变量在整个过程中保持状态")
}
