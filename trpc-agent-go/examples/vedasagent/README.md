# Vedas Agent demo演示

本示例演示如何使用 **[vedas](https://iwiki.woa.com/p/4014280951)** 服务构建vedas agent实现对话任务


## 必需的环境变量

运行示例前，请设置以下环境变量：

```bash
# Vedas 服务配置
# vedas认证令牌（authorization）（当前配置vedas的默认值即可），参考https://iwiki.woa.com/p/4014006217
export VEDAS_TOKEN="your-vedas-token"                      
# 进入vedas的工作空间https://venus.woa.com/#/authCenter/appGroupsManage/appgroupJoin
# 加入对应应用组后获取应用组ID & token
export VEDAS_APP_GROUP_ID="your-app-id"
```

## 快速开始

### 1. 导航到示例目录

```bash
cd examples/vedasagent
```

### 2. 基本使用

使用默认设置运行示例（流式输出+文件管理）：

```bash
go run main.go
```

## 配置选项
### 环境变量

| 变量名 | 默认值 | 说明 |
|--------|--------|------|
| `VEDAS_TOKEN` | `7xxxx` | vedas认证令牌 |
| `VEDAS_APP_GROUP_ID` | `18676` | vedas应用 ID |

## 使用方法

### 示例会话

```
go run main.go
🚀 Plan Chat with Vedas Agent
Type '/exit' to end the conversation
Type '/new' to start a new conversation
Type '/file result' to check last conversation result files
Type '/file process' to check last conversation result files
Type '/download <file_id>' to download a specific file
Type '/upload <file_path>' to upload a specific file
==================================================

👤 You: /upload movie.txt
🤖 Assistant: upload success!  

👤 You: /new
start a new vedas agent, session id: vedas-session-1764042845

👤 You: write a english poetry, about 100 letter, please output a txt file
🤖 Assistant: 

已成功为您创建了一首英文诗歌并保存为txt文件。

**诗歌内容：**
Dawn whispers through quiet leaves,
soft light kisses dewy dreams.
Life hums beneath every stone,
hope grows where hearts have gently gone.

**文件信息：**
- 文件路径：/usr/local/app/workspace/plan_c09a8713b6b1d2c65298db49cc3a28ed/poetry.txt
- 字数：约100个字母
- 主题：自然与生活感悟

诗歌已按要求完成并保存为txt文件格式。

👤 You: /file result
vedas plan file list:
e4843205-7881-40ac-91b3-6beb4cd877ad: poetry.txt
👤 You: /download e4843205-7881-40ac-91b3-6beb4cd877ad

👤 You: /exit
👋 Goodbye!
```

### 特殊命令
- **`/new`**: 开始新的会话
- **`/exit`**: 结束对话并退出程序
- **`/file process`** 查看vedas一个任务执行过程中的中间文件
- **`/file result`** 查看vedas一个任务完成后的结果文件
- **`/download <file id>`** 下载文件
- **`/upload <file>`** 上传文件


## 代码架构

### 核心组件

1. **Vedas AgentBuilder**: 主要的vedas agent创建器
   - 管理会话配置
   - 处理任务执行外的文件管理
   - 构建vedas任务agent

2. **Vedas Agent**: vedas服务集成
   - vedas代理
   - 任务执行、流式响应

3. **Session Service**: 会话管理
   - 内存存储：`sessioninmemory.NewSessionService()`
   - 自动管理对话历史和上下文

4. **Runner**: 执行引擎
   - 使用 `runner.NewRunner()` 创建执行器
   - 协调代理和会话服务
   - 处理事件流和错误管理

### 关键函数

- **`run()`**: 初始化vedas代理和会话服务
- **`fileList()`**: 处理任务执行中和执行完成后的文件列表
- **`downloadFile()`**: 下载文件
- **`uploadFiles()`**: 上传任务附件

## Vedas Agent 构建

### 1. 配置vedas builder

首先创建vedas服务的配置选项，这些选项控制着与vedas服务的连接和行为：

```go
   // 创建vedas builder
   // set token & appGroupID
   builder := vedas.New(vedasToken, appGroupID)
```

### 2. 上传附件(可选)
   ```go
   uploadFiles
   ```

### 3. 创建vedas Agent 实例

使用配置选项创建vedas Agent，并设置 Agent 的基本属性：

```go
// 创建vedas Agent
   agentName = "vedas-agent"
   appName   = "vedas-plan"
	vedasOption := sdk.NewVedasOption(
		sdk.WithAttachments(v.attachments), // 步骤2上传成功的附件
	)
	agent, err := v.agentBuilder.Build(
		vedas.WithName(agentName),
		vedas.WithDescription("A helpful AI assistant powered by Vedas."),
		vedas.WithOption(vedasOption),
	)
```

#### WithMaxEventSize 配置说明

`WithMaxEventSize` 用于设置 SSE（Server-Sent Events）流处理时的最大缓冲区大小。

**何时需要调整：**
- 当收到 `bufio.Scanner: token too long` 错误时
- 当vedas服务返回的单行数据超过当前缓冲区大小时
- 处理大型 AI 响应或包含大量数据的流时

**配置建议：**
- **默认值**：128KB（用于放宽 `bufio.Scanner` 默认 64KB 的单行限制）
- **小型应用**：256KB
- **大型应用**：1MB 或更大
- **内存受限**：根据可用内存调整


### 3. 集成到 Runner 中
将vedas Agent 集成到 Runner 中，配置会话服务和其他运行时选项：

```go
// 创建 Runner 并集成vedas Agent
c.runner = runner.NewRunner(
    appName,                                    // 应用名称
    agent,                                      // vedas Agent 实例
    runner.WithSessionService(sessionService), // 会话服务（内存或 Redis）
)
```

### 4. 每次 Run 时指定 Vedas project ID
projectID 标记一次唯一对话
planID 标记一次唯一任务
一次对话中包含多次任务
```go
	eventChan, err := v.runner.Run(ctx, 
      "user", v.sessionID, message, agent.WithRequestID(v.projectID))
   //
	if err != nil {
		return fmt.Errorf("failed to run agent: %w", err)
	}
```

## 故障排除

### 常见问题

1. **认证错误**
   - 验证 `VEDAS_TOKEN` 和 `VEDAS_APP_GROUP_ID` 是否有效
   - 确认账号具有vedas服务访问权限

2. **`bufio.Scanner: token too long` 错误**
   - **原因**：SSE 流中的单行数据超过了缓冲区大小
   - **解决方案**：增加 `WithMaxEventSize` 的值
   ```go
   vedas.WithMaxEventSize(1024 * 1024), // 增加到 1MB
   ```
   - **调试步骤**：
     1. 检查vedas服务返回的数据大小
     2. 根据实际数据量调整缓冲区大小
     3. 确保系统有足够的可用内存

3. **附件上传完成后不生效**
   - **原因**  vedas 知识库文件指代性问题，待vedas解决，短时间内你可以在Prompt告知具体文件名或者说明文件来自知识库

### 调试建议

- 启用详细日志查看具体错误信息
- 检查环境变量配置是否完整

## 参考文档

- [vedas API 文档](https://iwiki.woa.com/p/4014325318)
- [tRPC-Agent-Go 框架文档](../../README.md)
- [Runner 使用指南](../runner/README.md)
- [Session 管理文档](../../docs/session.md)
