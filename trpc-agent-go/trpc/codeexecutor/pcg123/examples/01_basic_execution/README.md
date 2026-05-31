# 🚀 基础代码执行示例

**简单演示** 如何使用PCG123代码执行器执行基础Python代码。

## 展示内容

- ✨ **简单计算**: 数学运算和变量操作
- 📊 **数据处理**: 基础的字符串和列表操作  
- 🔢 **内置库使用**: math、random等标准库
- 📝 **输出展示**: print语句和计算结果

## 核心代码

```go
// 创建配置
conf := pcg123.Config{
    Language:  pcg123.LanguagePython310,
    SecretID:  os.Getenv("PCG123_SECRET_ID"),
    SecretKey: os.Getenv("PCG123_SECRET_KEY"),
}

// 创建执行器
executor, cancel, err := pcg123.NewCodeExecutor(conf)
defer cancel()

// 执行代码
result, err := executor.ExecuteCode(ctx, codeexecutor.CodeExecutionInput{
    CodeBlocks: []codeexecutor.CodeBlock{
        {Code: "print('Hello, PCG123!')"},
        {Code: "result = 2 + 3 * 4; print(f'计算结果: {result}')"},
    },
})
```

## 运行方式

```bash
# 设置环境变量
export PCG123_SECRET_ID="your-secret-id"
export PCG123_SECRET_KEY="your-secret-key"

# 运行示例
cd 01_basic_execution
go run main.go
```

## 预期输出

```
🚀 基础代码执行示例
====================

📝 执行简单计算...
Hello, PCG123!
计算结果: 14

📝 执行字符串操作...
原始文本: Hello World
大写: HELLO WORLD
单词数量: 2

📝 执行列表操作...
原始列表: [1, 2, 3, 4, 5]
平方后: [1, 4, 9, 16, 25]
总和: 55

✅ 基础代码执行完成！
```

## 示例说明

### 1. 简单数学计算
- 展示基础的算术运算
- 变量赋值和使用
- f-string格式化输出

### 2. 字符串处理
- 字符串方法调用
- 文本转换操作
- 简单文本分析

### 3. 列表操作
- 列表创建和遍历
- 列表推导式
- 内置函数使用

## 配置特点

- **非交互式模式**: 适合批量执行独立的代码块
- **默认超时**: 5秒执行超时，适合简单任务
- **Python 3.10**: 使用最新的Python特性

## 下一步

- **02_plot_generation**: 学习图形输出和可视化
- **03_interactive_execution**: 了解交互式执行模式
