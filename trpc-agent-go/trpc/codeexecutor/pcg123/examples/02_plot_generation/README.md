# 📊 图形绘制示例

**演示** 如何使用PCG123代码执行器生成matplotlib图表并获取图像输出。

## 展示内容

- 📈 **基础图表**: 线图、散点图、柱状图
- 🎨 **图表美化**: 颜色、标签、网格设置
- 💾 **图像输出**: 自动保存和获取生成的图像文件
- 📋 **多图展示**: 在一个执行中生成多个图表

## 核心代码

```go
// 创建执行器 (非交互式模式)
executor, cancel, err := pcg123.NewCodeExecutor(conf)
defer cancel()

// 执行绘图代码
result, err := executor.ExecuteCode(ctx, codeexecutor.CodeExecutionInput{
    CodeBlocks: []codeexecutor.CodeBlock{
        {Code: `
import matplotlib.pyplot as plt
import numpy as np

x = np.linspace(0, 10, 100)
y = np.sin(x)

plt.figure(figsize=(10, 6))
plt.plot(x, y, 'b-', linewidth=2)
plt.title('正弦函数图像')
plt.show()
        `},
    },
})

// 处理输出的图像文件
for _, file := range result.OutputFiles {
    fmt.Printf("📸 生成图像: %s (类型: %s)\n", file.Name, file.MIMEType)
}
```

## 运行方式

```bash
# 设置环境变量
export PCG123_SECRET_ID="your-secret-id"
export PCG123_SECRET_KEY="your-secret-key"

# 运行示例
cd 02_plot_generation
go run main.go
```

## 预期输出

```
📊 图形绘制示例
================

📈 绘制正弦函数图像...
📸 生成图像: image.0.png -> 保存为: sine_wave_0.png (类型: image/png)

📊 绘制数据分析图表...
📸 生成图像: image.0.png -> 保存为: data_analysis_0.png (类型: image/png)

📈 绘制多函数对比图...
📸 生成图像: image.0.png -> 保存为: multi_function_0.png (类型: image/png)

✅ 图形绘制完成！生成了 3 个图像文件
```

运行完成后，当前目录下会生成以下PNG图片文件：
- `sine_wave_0.png` - 正弦函数图像
- `data_analysis_0.png` - 数据分析图表
- `multi_function_0.png` - 多函数对比图

## 示例说明

### 1. 数学函数图像
- 使用numpy生成数据
- matplotlib基础绘图
- 图表标题和标签设置

### 2. 数据分析可视化
- pandas数据处理
- 统计信息展示
- 直方图和散点图

### 3. 多子图布局
- subplot子图创建
- 不同类型图表组合
- 美观的布局设计

## 图像输出特性

### 自动保存机制
- PCG123自动捕获`plt.show()`生成的图像
- 图像以base64格式在`OutputFiles`中返回
- 支持PNG、JPEG等常见图像格式

### 本地PNG保存
- 🆕 新增功能：自动将图像保存为PNG文件到当前目录
- 图片文件名按功能分类：`sine_wave_*.png`、`data_analysis_*.png`等
- 使用`.gitignore`防止图片文件被意外提交到版本控制

### 文件信息
```go
type File struct {
    Name     string // 如: "image.0.png"
    Content  string // base64编码的图像内容
    MIMEType string // 如: "image/png"
}
```

### 处理多个图像
- 每次`plt.show()`生成一个图像文件
- 文件按生成顺序编号: image.0.png, image.1.png...
- 可在一个代码块中生成多个图表

## 配置特点

- **非交互式模式**: 适合生成静态图表
- **默认超时**: 5秒，适合简单图表生成
- **自动图像捕获**: 无需手动保存图像

## 下一步

- **03_interactive_execution**: 学习交互式代码执行
- **04_interactive_plot**: 在交互式环境中生成复杂图表
