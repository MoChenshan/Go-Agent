# 📚 PCG123 代码执行器使用示例

欢迎使用PCG123代码执行器！这些示例展示了如何在tRPC-Agent-Go生态系统中使用PCG123代码执行器进行各种代码执行任务。

## 🎯 学习路径 (建议按顺序学习)

### 0. Agent框架集成 ⭐ 推荐入门
- **[00_agent_integration](./00_agent_integration/)** - Agent框架 + PCG123集成，展示智能对话式代码执行

### 1. 基础使用
- **[01_basic_execution](./01_basic_execution/)** - 简单Python代码执行，快速上手

### 2. 图形输出
- **[02_plot_generation](./02_plot_generation/)** - matplotlib绘图和图像输出

### 3. 高级功能
- **[03_interactive_execution](./03_interactive_execution/)** - 交互式代码执行模式
- **[04_interactive_plot](./04_interactive_plot/)** - 交互式环境下的图形绘制

### 4. Skills Engine 集成
- **[05_skill_execution](./05_skill_execution/)** - Skills Engine 对接，支持完整技能执行流程

## 🚀 根据你的需求选择

### 如果你想要...

#### 🤖 **构建智能对话式代码执行应用** ⭐ 推荐
→ 从 `00_agent_integration` 开始

#### 🔧 **执行简单的计算任务**
→ 从 `01_basic_execution` 开始

#### 📊 **生成图表和可视化**  
→ 直接看 `02_plot_generation`

#### 💬 **需要多轮交互执行**
→ 看 `03_interactive_execution`

#### 🎨 **复杂的交互式数据分析**
→ 看 `04_interactive_plot`

#### 🛠️ **使用 Skills Engine 执行技能**
→ 看 `05_skill_execution`

## 📋 功能对照表

| 示例 | 基础执行 | 图形输出 | 交互式 | Agent集成 | Skills | 难度 |
|------|----------|----------|--------|-----------|--------|------|
| 00_agent_integration | ✅ | ❌ | ❌ | ✅ | ❌ | ⭐⭐ |
| 01_basic_execution | ✅ | ❌ | ❌ | ❌ | ❌ | ⭐ |
| 02_plot_generation | ✅ | ✅ | ❌ | ❌ | ❌ | ⭐⭐ |
| 03_interactive_execution | ✅ | ❌ | ✅ | ❌ | ❌ | ⭐⭐⭐ |
| 04_interactive_plot | ✅ | ✅ | ✅ | ❌ | ❌ | ⭐⭐⭐⭐ |
| 05_skill_execution | ✅ | ❌ | ❌ | ❌ | ✅ | ⭐⭐⭐ |

## 🛠️ 运行示例

每个示例目录都有独立的`README.md`和可运行的代码：

```bash
# 推荐：先从Agent集成示例开始
cd 00_agent_integration

# 配置你的凭证（Agent示例需要两套凭证）
export OPENAI_API_KEY="your-openai-api-key"        # LLM模型
export OPENAI_BASE_URL="https://api.openai.com/v1" # API端点
export PCG123_SECRET_ID="your-secret-id"           # PCG123凭证
export PCG123_SECRET_KEY="your-secret-key"

# 运行Agent示例
go run main.go

# 或者运行基础示例
cd ../01_basic_execution
go run main.go

# 查看详细说明
cat README.md
```

## 📖 核心概念

### 执行器创建
```go
// 基础配置
conf := pcg123.Config{
    Language:  pcg123.LanguagePython310,
    SecretID:  "your-secret-id",
    SecretKey: "your-secret-key",
}

// 创建执行器（默认懒初始化）
executor, cancel, err := pcg123.NewCodeExecutor(conf)
defer cancel()
```

### 沙箱资源的懒申请（默认）

`NewCodeExecutor(conf)` 默认采用**懒初始化**：构造时只做本地配置校验，
真正的 123 沙箱容器要等到第一次 `ExecuteCode` 或第一次通过
`Engine()`（Skills 通道）调用 workspace/program 方法时才向平台申请。

为什么这样设计：

- 多副本部署、低流量的服务无需每个进程都常驻一个沙箱配额；
- main 函数里 `NewCodeExecutor(...)` 不再因 123 平台抖动而阻塞启动；
- 与 `WithReconnectMode` 互补——后者管的是「已经申请过」之后的健康
  修复，懒初始化管的是「从未申请过」如何延迟到首次实际使用。

如果你希望进程启动期就发现凭证 / 网络等配置问题，使用 `WithLazyInit(false)`
强制构造时同步申请沙箱：

```go
executor, cancel, err := pcg123.NewCodeExecutor(conf,
    pcg123.WithLazyInit(false), // 启动期连通 123，便于快速失败
)
```

| 场景 | 推荐配置 |
|------|----------|
| Agent 服务、长驻进程、多副本部署 | 默认（懒初始化） |
| 短脚本、CI 任务、希望启动期就报错 | `WithLazyInit(false)` |

### 代码执行
```go
// 执行代码块
result, err := executor.ExecuteCode(ctx, codeexecutor.CodeExecutionInput{
    CodeBlocks: []codeexecutor.CodeBlock{
        {Code: "print('Hello, PCG123!')"},
    },
})
```

### 交互式模式
```go
// 启用交互式模式
executor, cancel, err := pcg123.NewCodeExecutor(conf,
    pcg123.WithInteractive(true),
    pcg123.WithIdleTimeout(10*time.Minute),
)
```

## 🔧 配置说明

### 环境变量设置
```bash
# 必需的凭证信息
export PCG123_SECRET_ID="your-secret-id"        # PCG123 API网关应用ID
export PCG123_SECRET_KEY="your-secret-key"      # PCG123 API网关平台密钥
```

### 支持的配置选项

| 配置项 | 类型 | 说明 | 默认值 |
|--------|------|------|--------|
| `Language` | `Language` | Python版本 (3.8/3.9/3.10) | - |
| `SecretID` | `string` | PCG123应用ID (必需) | - |
| `SecretKey` | `string` | PCG123密钥 (必需) | - |
| `ExecuteTimeout` | `time.Duration` | 单次执行超时 | 5秒 |
| `IdleTimeout` | `time.Duration` | 会话空闲超时 | 15分钟 |
| `Interactive` | `bool` | 交互式模式 | false |
| `LazyInit` | `bool` | 首次使用时再申请沙箱（`WithLazyInit`） | true |

## 🌟 使用场景

### 1. **智能对话式应用**
- 自然语言驱动的代码执行
- 智能数据分析助手
- 对话式编程教学
- AI驱动的业务分析工具

### 2. **Skills 技能执行**
- 远程执行 Agent Skills
- 工作区文件管理
- 自动化任务流程
- 技能链式执行

### 3. **数据分析任务**
- 快速数据计算和统计
- 数据清洗和转换
- 简单的机器学习模型

### 4. **可视化生成**
- matplotlib图表生成
- 数据可视化报告
- 图像处理和分析

### 5. **科学计算**
- NumPy/SciPy科学计算
- 数学公式求解
- 算法验证和测试

### 6. **教育和演示**
- 代码教学演示
- 算法可视化
- 交互式学习环境

## 🚨 注意事项

### 凭证安全
- ⚠️ **切勿在代码中硬编码凭证**
- 🔐 **使用环境变量管理敏感信息**
- 🛡️ **定期轮换API密钥**

### 资源管理
- 🎯 **始终调用cancel()释放资源**
- ⏱️ **根据任务复杂度设置合理超时**
- 🔄 **交互式会话完成后及时销毁**

### 性能优化
- 📦 **批量执行相关代码块**
- 🏃 **长时间任务使用交互式模式**
- 💾 **注意大型输出的内存使用**

## 🔗 相关资源

- **[PCG123代码执行器主文档](../README.md)** - 完整的API文档和配置指南
- **[tRPC-Agent-Go框架](https://github.com/trpc-group/trpc-agent-go)** - 核心框架文档

## 🤝 需要帮助？

如果遇到问题或有疑问：

1. 查看对应示例的`README.md`
2. 检查PCG123凭证是否正确配置
3. 确保Go版本 >= 1.19
4. 检查网络连接和PCG123服务状态
