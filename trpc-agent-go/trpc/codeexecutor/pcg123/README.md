# 🚀 PCG123 代码执行器 ↔ tRPC-Agent-Go 集成方案

基于 PCG123 平台的远程代码执行器，为 [tRPC-Agent-Go](https://github.com/trpc-group/trpc-agent-go) 生态系统提供安全可靠的代码执行能力。

## ✨ 我们的执行器能做什么？

- 🐍 **多版本 Python 支持**：支持 Python 3.8、3.9、3.10 版本
- 🔒 **多层安全隔离**：基于 PCG123 沙箱实现进程级用户隔离，并通过 NFS POSIX 权限实现 Skill 会话间工作区互不干扰
- 🌊 **交互式执行**：支持交互式和非交互式两种执行模式
- 📊 **多媒体输出**：支持图像等多媒体文件输出
- ⚙️ **灵活配置**：支持自定义执行超时、空闲超时、健康检查等参数
- 🔧 **生产环境高可用**：支持沙箱实例环境故障、实例空闲回收等场景的自动无感知重建
- 🧩 **Agent Skills 集成** 无缝集成 tRPC-Agent 框架标准 Skills 运行时, 安全执行脚本

## 🔔 使用前须知

- 要求运行时网络环境为IDC内网或Dev-net，办公网环境无法使用
- 请勿恶意频繁初始化执行器，容易造成资源浪费，后续会建设完整的按量计费模式

## 🛞 前置准备

- 💡 **创建[API网关](https://apigw.woa.com/release#/api)应用**, 用于后续初始化执行器的必要配置, 你也可以使用已有应用
- 🤖 联系123平台小助手 **123_Helper** 提供应用后授权调用, 完成后即可快速接入

## 🚀 快速开始

### 1. 安装依赖

```bash
go get git.woa.com/trpc-go/trpc-agent-go/trpc/codeexecutor/pcg123
```

### 2. 基础配置

```go
import "git.woa.com/trpc-go/trpc-agent-go/trpc/codeexecutor/pcg123"

// 创建配置
conf := pcg123.Config{
    Language:  pcg123.LanguagePython310,    // Python 版本
    SecretID:  "your-secret-id",            // PCG123 API网关应用ID
    SecretKey: "your-secret-key",           // PCG123 API网关应用密钥
}

// 创建执行器实例：默认采用懒初始化，构造时不向 PCG123 申请沙箱，
// 直到首次 ExecuteCode 或 Engine 通道（Skills）调用时才发起 InitExecutor，
// 多副本、低流量服务可避免每个进程常驻一个沙箱配额。
executor, cancel, err := pcg123.NewCodeExecutor(conf)
if err != nil {
    log.Fatal(err) // 仅本地配置校验失败才会在这里返回 err
}
defer cancel() // 重要：释放资源
```

> 如需进程启动期就连通 PCG123（便于快速发现凭证 / 网络问题），可使用
> `pcg123.WithLazyInit(false)` 关闭懒初始化，恢复"构造即申请沙箱"。
> 该选项与 `WithReconnectMode` 正交：前者管「从未申请过」如何延迟到首次
> 使用，后者管「已经申请过之后」如何修复健康状态。

### 3. 执行代码

```go
import "trpc.group/trpc-go/trpc-agent-go/codeexecutor"

// 执行 Python 代码
result, err := executor.ExecuteCode(context.Background(), codeexecutor.CodeExecutionInput{
    CodeBlocks: []codeexecutor.CodeBlock{
        {Code: "print('Hello, PCG123!')"},
        {Code: "import matplotlib.pyplot as plt; plt.plot([1,2,3], [4,5,6]); plt.show()"},
    },
})
if err != nil {
    log.Fatal(err)
}

// 输出结果
fmt.Println("执行输出:", result.Output)

// 处理输出文件（如图片）
for _, file := range result.OutputFiles {
    fmt.Printf("文件: %s, 类型: %s\n", file.Name, file.MIMEType)
    // file.Content 包含 base64 编码的文件内容
}
```

## 🧩 Agent Skills 集成

执行器实现了 tRPC-Agent-Go 框架的 `EngineProvider` 接口，可直接挂到 Skills 运行时使用：每次 Skill 执行框架按 `execID` 在沙箱内挂载的 NFS 卷上申请独立工作区，执行结束后回收。

同一沙箱并发服务多个 Skill 会话时，执行器默认开启会话隔离：不同会话的工作区被绑定到各自的 Linux 用户/组，相互之间 EACCES。某些场景需要 Skill 显式以沙箱默认用户 mqq 身份运行（例如访问只对 mqq 授权的本地资源），可以关掉：

```go
// 默认开启，无需显式调用：
executor, cancel, err := pcg123.NewCodeExecutor(conf)

// 显式关闭：
executor, cancel, err := pcg123.NewCodeExecutor(conf,
    pcg123.WithSessionIsolation(false),
)
```

接入用法、隔离的实现机制（per-session uid + group + 2770、工作区文件不变量、`su` 调试体验等）详见 [`examples/05_skill_execution/README.md`](./examples/05_skill_execution/README.md)。

## ⚙️ 高级配置

### 自定义执行参数

```go
executor, cancel, err := pcg123.NewCodeExecutor(conf,
    pcg123.WithExecuteTimeout(30*time.Second),  // 代码执行超时
    pcg123.WithIdleTimeout(10*time.Minute),     // 会话空闲超时
    pcg123.WithInteractive(true),               // 启用交互式模式
)
```

### 支持的配置选项

| 配置项 | 类型 | 描述 | 默认值 |
|--------|------|------|--------|
| `Language` | `Language` | 编程语言（必需） | - |
| `SecretID` | `string` | PCG123 API网关应用ID（必需） | - |
| `SecretKey` | `string` | PCG123 API网关平台密钥（必需） | - |
| `ExecuteTimeout` | `time.Duration` | 单次代码执行超时 | 5秒 |
| `IdleTimeout` | `time.Duration` | 会话空闲超时 | 15分钟 |
| `Interactive` | `bool` | 是否启用交互式模式 | false |
| `LazyInit` | `bool` | 首次使用时再申请沙箱（`WithLazyInit`） | true |
| `SessionIsolation` | `bool` | Skill 会话间工作区隔离（`WithSessionIsolation`） | true |

## 🎯 支持的语言版本

```go
// 当前仅支持Python, 可用的 Python 版本
pcg123.LanguagePython38   // Python 3.8
pcg123.LanguagePython39   // Python 3.9
pcg123.LanguagePython310  // Python 3.10
```


## 📝 使用示例

详细的使用示例请参考：**[examples目录](./examples/README.md)**

## 📖 功能规划

- 支持更多语言类型, 如: go、nodejs、bash
- 更加健全的可用性机制, 如: 自动发送心跳,避免空闲回收

## 🚨 注意事项

### 凭证管理

- ⚠️ **安全性**：SecretID 和 SecretKey 是敏感信息，请勿硬编码在代码中
- 🔐 **最佳实践**：使用环境变量或配置文件管理凭证

### 资源管理

- 🏠 **资源限制**：当前执行器资源规格不支持自定义, 默认单执行器4核8G, 请评估资源容量后接入
- 🚫 **输出限制**：代码执行输出以及文件大小存在限制, 总输出内容默认10M
- 🎯 **及时释放**：务必调用 `cancel()` 函数释放执行器资源
- ⏱️ **超时设置**：根据实际需求合理设置执行超时时间
- 🔁 **重试机制**：实现适当的重试逻辑处理网络异常

### 性能优化

- 🐳 **单例模式**：非必要时仅初始化一个全局执行器实例
- 📊 **批量执行**：尽量批量执行多个代码块减少网络开销
- 🎛️ **交互模式**：长时间会话建议启用交互式模式
- 💾 **输出处理**：大型输出文件注意内存使用

### 计费说明

- 💰 **计费模式**：暂不计费， 平台侧承担成本

## 🔗 更多资源

- [tRPC-Agent-Go 框架](https://github.com/trpc-group/trpc-agent-go)
- 如有疑问请咨询123小助手: 123_Helper

## 📄 许可证

本项目采用与 tRPC-Agent-Go 相同的许可证。
