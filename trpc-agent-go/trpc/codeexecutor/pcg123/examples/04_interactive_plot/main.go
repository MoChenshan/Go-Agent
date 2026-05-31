package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"time"

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
	fmt.Println("🎨 交互式图形绘制示例")
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

	// 创建交互式执行器，优化图形生成参数
	executor, cancel, err := pcg123.NewCodeExecutor(conf,
		pcg123.WithShared(true),                   // 推荐使用共享执行器用于测试
		pcg123.WithInteractive(true),              // 开启交互式模式
		pcg123.WithIdleTimeout(15*time.Minute),    // 图形生成可能需要更长时间
		pcg123.WithExecuteTimeout(60*time.Second), // 复杂图表需要更长执行时间
	)
	if err != nil {
		log.Fatalf("❌ 创建执行器失败: %v", err)
	}
	defer cancel()

	ctx := context.Background()
	totalImages := 0

	// 步骤1: 准备数据和设置绘图环境
	fmt.Println("\n📊 步骤1: 准备数据和设置绘图环境...")
	result1, err := executor.ExecuteCode(ctx, codeexecutor.CodeExecutionInput{
		CodeBlocks: []codeexecutor.CodeBlock{
			{Code: `
import numpy as np
import pandas as pd
import matplotlib.pyplot as plt
import matplotlib.dates as mdates
from datetime import datetime, timedelta
# 设置matplotlib参数
plt.rcParams['figure.figsize'] = (12, 8)
plt.rcParams['figure.dpi'] = 100
plt.style.use('default')

# 设置随机种子
np.random.seed(42)

# 生成模拟股票数据
start_date = datetime(2023, 1, 1)
end_date = datetime(2023, 12, 31)
dates = pd.date_range(start_date, end_date, freq='D')

# 创建三只股票的模拟数据
companies = ['Tech', 'Finance', 'Consumer']
stock_data = pd.DataFrame(index=dates)

for i, company in enumerate(companies):
    # 生成随机游走价格
    price_changes = np.random.normal(0.001, 0.02, len(dates))
    # 添加趋势
    trend = np.linspace(0, 0.3 + i*0.1, len(dates))
    prices = 100 * np.exp(np.cumsum(price_changes) + trend)
    stock_data[company] = prices

print("📊 数据准备完成")
print(f"生成股票数据: {len(stock_data)} 天")
print(f"公司数量: {len(companies)}")
print(f"数据范围: {start_date.strftime('%Y-%m-%d')} 到 {end_date.strftime('%Y-%m-%d')}")
			`},
		},
	})
	if err != nil {
		log.Printf("❌ 步骤1执行失败: %v", err)
		return
	}
	fmt.Print(result1.Output)

	// 步骤2: 生成基础时间序列图表
	fmt.Println("📈 步骤2: 生成基础时间序列图表...")
	result2, err := executor.ExecuteCode(ctx, codeexecutor.CodeExecutionInput{
		CodeBlocks: []codeexecutor.CodeBlock{
			{Code: `
# 使用前面准备的数据创建基础时间序列图
plt.figure(figsize=(14, 8))

# 绘制三只股票的价格走势
colors = ['#2E86AB', '#A23B72', '#F18F01']
for i, company in enumerate(companies):
    plt.plot(stock_data.index, stock_data[company], 
             label=company, linewidth=2, color=colors[i])

plt.title('Stock Price Comparison', fontsize=16, pad=20)
plt.xlabel('Date', fontsize=12)
plt.ylabel('Price (USD)', fontsize=12)
plt.legend(fontsize=11)
plt.grid(True, alpha=0.3)

# 格式化x轴日期
plt.gca().xaxis.set_major_formatter(mdates.DateFormatter('%Y-%m'))
plt.gca().xaxis.set_major_locator(mdates.MonthLocator(interval=2))
plt.xticks(rotation=45)

plt.tight_layout()
plt.show()

print("📈 基础图表生成完成")
print(f"图表包含 {len(companies)} 条股票价格线")
			`},
		},
	})
	if err != nil {
		log.Printf("❌ 步骤2执行失败: %v", err)
		return
	}
	fmt.Print(result2.Output)
	for i, file := range result2.OutputFiles {
		filename := fmt.Sprintf("stock_price_comparison_%d.png", i)
		err := saveImageToPNG(filename, file.Content)
		if err != nil {
			log.Printf("❌ 保存图片失败: %v", err)
		} else {
			fmt.Printf("📸 生成图像: %s -> 保存为: %s (类型: %s)\n", file.Name, filename, file.MIMEType)
			totalImages++
		}
	}

	// 步骤3: 生成技术分析图表
	fmt.Println("\n📊 步骤3: 生成技术分析图表...")
	result3, err := executor.ExecuteCode(ctx, codeexecutor.CodeExecutionInput{
		CodeBlocks: []codeexecutor.CodeBlock{
			{Code: `
# 使用现有数据计算技术指标
company = companies[0]  # 选择第一只股票进行技术分析
prices = stock_data[company]

# 计算移动平均线
ma_short = prices.rolling(window=20).mean()  # 20日移动平均
ma_long = prices.rolling(window=50).mean()   # 50日移动平均

# 计算波动率
volatility = prices.rolling(window=20).std()

# 创建双轴技术分析图
fig, (ax1, ax2) = plt.subplots(2, 1, figsize=(14, 10), height_ratios=[3, 1])

# 上图：价格和移动平均线
ax1.plot(stock_data.index, prices, label=f'{company} Price', linewidth=2, color='#2E86AB')
ax1.plot(stock_data.index, ma_short, label='20-Day MA', linewidth=1.5, color='#F18F01', alpha=0.8)
ax1.plot(stock_data.index, ma_long, label='50-Day MA', linewidth=1.5, color='#A23B72', alpha=0.8)

ax1.set_title(f'{company} Technical Analysis', fontsize=16, pad=20)
ax1.set_ylabel('Price (USD)', fontsize=12)
ax1.legend(fontsize=10)
ax1.grid(True, alpha=0.3)

# 下图：波动率
ax2.plot(stock_data.index, volatility, color='#FF6B35', linewidth=2)
ax2.fill_between(stock_data.index, volatility, alpha=0.3, color='#FF6B35')
ax2.set_title('20-Day Rolling Volatility', fontsize=12)
ax2.set_xlabel('Date', fontsize=12)
ax2.set_ylabel('Volatility', fontsize=12)
ax2.grid(True, alpha=0.3)

# 格式化x轴
for ax in [ax1, ax2]:
    ax.xaxis.set_major_formatter(mdates.DateFormatter('%Y-%m'))
    ax.xaxis.set_major_locator(mdates.MonthLocator(interval=2))
    plt.setp(ax.xaxis.get_majorticklabels(), rotation=45)

plt.tight_layout()
plt.show()

print("📊 技术分析图表生成完成")
print("包含移动平均线和波动率分析")
			`},
		},
	})
	if err != nil {
		log.Printf("❌ 步骤3执行失败: %v", err)
		return
	}
	fmt.Print(result3.Output)
	for i, file := range result3.OutputFiles {
		filename := fmt.Sprintf("technical_analysis_%d.png", i)
		err := saveImageToPNG(filename, file.Content)
		if err != nil {
			log.Printf("❌ 保存图片失败: %v", err)
		} else {
			fmt.Printf("📸 生成图像: %s -> 保存为: %s (类型: %s)\n", file.Name, filename, file.MIMEType)
			totalImages++
		}
	}

	// 步骤4: 生成综合分析仪表板
	fmt.Println("\n🎯 步骤4: 生成综合分析仪表板...")
	result4, err := executor.ExecuteCode(ctx, codeexecutor.CodeExecutionInput{
		CodeBlocks: []codeexecutor.CodeBlock{
			{Code: `
# 计算收益率
returns = stock_data.pct_change().dropna()

# 计算累积收益率
cumulative_returns = (1 + returns).cumprod()

# 创建综合仪表板
fig, ((ax1, ax2), (ax3, ax4)) = plt.subplots(2, 2, figsize=(16, 12))

# 1. 标准化价格走势对比
normalized_prices = stock_data / stock_data.iloc[0]
for i, company in enumerate(companies):
    ax1.plot(stock_data.index, normalized_prices[company], 
             label=company, linewidth=2, color=colors[i])
ax1.set_title('Normalized Price Comparison (Base=1)', fontsize=12, pad=10)
ax1.set_ylabel('Relative Price')
ax1.legend()
ax1.grid(True, alpha=0.3)

# 2. 日收益率分布
for i, company in enumerate(companies):
    ax2.hist(returns[company], bins=30, alpha=0.6, 
             label=company, color=colors[i], density=True)
ax2.set_title('Daily Return Distribution', fontsize=12, pad=10)
ax2.set_xlabel('Daily Return')
ax2.set_ylabel('Density')
ax2.legend()
ax2.grid(True, alpha=0.3)

# 3. 相关性热力图
correlation_matrix = returns.corr()
im = ax3.imshow(correlation_matrix, cmap='RdYlBu', vmin=-1, vmax=1)
ax3.set_title('Stock Correlation Matrix', fontsize=12, pad=10)
ax3.set_xticks(range(len(companies)))
ax3.set_yticks(range(len(companies)))
ax3.set_xticklabels(companies, rotation=45)
ax3.set_yticklabels(companies)

# 添加相关系数文本
for i in range(len(companies)):
    for j in range(len(companies)):
        text = ax3.text(j, i, f'{correlation_matrix.iloc[i, j]:.2f}',
                       ha="center", va="center", color="black", fontweight='bold')

# 4. 累积收益率
for i, company in enumerate(companies):
    ax4.plot(returns.index, cumulative_returns[company], 
             label=company, linewidth=2, color=colors[i])
ax4.set_title('Cumulative Returns', fontsize=12, pad=10)
ax4.set_ylabel('Cumulative Return Multiplier')
ax4.legend()
ax4.grid(True, alpha=0.3)

# 格式化日期轴
for ax in [ax1, ax4]:
    ax.xaxis.set_major_formatter(mdates.DateFormatter('%Y-%m'))
    ax.xaxis.set_major_locator(mdates.MonthLocator(interval=3))

plt.tight_layout()
plt.show()

print("🎯 综合仪表板生成完成")
print("包含价格、收益率、相关性和分布分析")
			`},
		},
	})
	if err != nil {
		log.Printf("❌ 步骤4执行失败: %v", err)
		return
	}
	fmt.Print(result4.Output)
	for i, file := range result4.OutputFiles {
		filename := fmt.Sprintf("comprehensive_dashboard_%d.png", i)
		err := saveImageToPNG(filename, file.Content)
		if err != nil {
			log.Printf("❌ 保存图片失败: %v", err)
		} else {
			fmt.Printf("📸 生成图像: %s -> 保存为: %s (类型: %s)\n", file.Name, filename, file.MIMEType)
			totalImages++
		}
	}

	fmt.Printf("\n✅ 交互式图形绘制完成！生成了 %d 个图表，展示了从基础到高级的可视化流程\n", totalImages)
}
