# PCG123 Skills Engine 集成示例

本示例演示如何将 PCG123 远程代码执行器与 Agent Skills 框架集成使用。

## 重要说明

**PCG123 代码执行器仅支持 Python 3.10+ 版本执行 anthropic 标准的 skills**

- 初始化执行器时，远程执行环境必须使用 Python 3.10 或更高版本
- 执行器内置依赖可以满足绝大部分官方 anthropic/skills, 后续平台会考虑支持自定义依赖环境来满足特殊技能

## 功能说明

通过 `llmagent.WithCodeExecutor(executor)` 将 PCG123 执行器集成到 Agent 中，Agent 自动获得 `skill_load`、`skill_run` 等工具，可以：

1. 加载技能文档
2. 在远程 123 环境执行技能命令
3. 支持工作区 CFS 卷挂载，实现工作区数据持久化和共享

## 架构

```
User Input
    │
    ▼
┌─────────────┐
│  LLM Agent  │ ◄── llmagent.WithSkills(repo)
│             │ ◄── llmagent.WithCodeExecutor(pcg123Executor)
└─────────────┘
    │
    ▼ skill_run
┌─────────────────────────────┐
│  PCG123 CodeExecutor        │
│  └── Engine() -> NFSRuntime │
│       ├── Manager (nfs)     │
│       ├── FS (nfs)          │
│       └── Runner (remote)   │
└─────────────────────────────┘
    │
    ▼
┌─────────────┐
│  123 远程   │
│  代码执行   │
└─────────────┘
```

## 工作区 CFS 卷挂载

### 概述

PCG123 执行器支持将 CFS（Cloud File System）卷挂载到 skill 工作区，实现以下功能：

- **数据持久化**：工作区数据保存在 CFS 存储中，不会因为容器销毁而丢失
- **多实例共享**：Agent 多个实例共享同一工作区, 实现分布式场景下多轮会话数据的持久化
- **高性能访问**：CFS 提供高性能的文件读写能力，适合处理大文件和大规模数据

### 配置参数

通过 `pcg123.WithSkillWorkspaceMount` 配置 CFS 卷挂载, 详细配置参考文档: https://iwiki.woa.com/p/1871558973

```go
pcg123.WithSkillWorkspaceMount(pcg123.WorkspaceVolumeMount{
    CFSVolumeMount: &pcg123.CFSVolumeMount{
        Name:    "cfs-name",           // CFS 文件系统名称
        Host:    "cfs-host",           // CFS 挂载点地址
        Path:    "/",                  // 挂载路径（默认 "/"）
        Version: pcg123.CFSVersion3,   // CFS 协议版本（v3 或 v4）
    },
})
```

**参数说明**：

| 参数 | 类型 | 说明 |
|------|------|------|
| `Name` | string | CFS 文件系统名称，必填 |
| `Host` | string | CFS 挂载点主机地址，必填 |
| `Path` | string | 挂载路径，默认为 "/" |
| `Version` | CFSVersion | CFS 协议版本，支持 v3 和 v4 |

**注意事项**：

- ⚠️ **仅在启用 Skills 时可用**：工作区挂载仅在 Python 3.10 环境下有效
- ⚠️ **CFS 资源需要预先创建**：使用前需确保 CFS 文件系统已创建并可访问
- ⚠️ **网络要求**：执行环境需要能够访问 CFS 挂载点

### 使用场景

**适用场景**：
- ✅ 需要持久化保留结果供后续跨Skill等场景使用
- ✅ 需要多个执行器节点共享同一块工作区
- ✅ 需要处理大文件或大规模数据集

## 会话工作区隔离

### 概述

同一个 PCG123 沙箱可能并发服务多个 Skill 会话（不同 `execID` 共享同一个 `CodeExecutor` 实例）。默认情况下执行器为每个会话提供独立、互不可见的工作区，避免会话 A 的 Skill 进程读写到会话 B 的中间产物。开关由 `pcg123.WithSessionIsolation(bool)` 控制，默认开启。

### 实现机制

执行器为每个会话在沙箱内创建一个独占的 Linux 用户/组（`pcg123_<uid>`），把工作区目录的 group 设为该 gid，并打开 setgid bit、模式收紧到 `2770`；命令执行时 wrapper 通过 `setuid + setgid` 切到该会话的 (uid, gid) 后再 exec bash。具体效果：

- **跨会话访问**：会话 A 的 Skill 进程读写会话 B 的工作区时以 "other" 身份命中目录的 `2770`，被 NFS 内核态权限直接挡下
- **同会话 SDK 操作**（staging、metadata、collect）：仍以 owner（沙箱默认用户 mqq）身份照常进行

工作区文件不变量：每次 RunProgram 前后，工作区里的所有文件 / 目录都保持 `mqq:pcg123_<gid>` 形态（文件 `0660`、目录 `2770`）。bash 运行时虽然切到 `pcg123_<uid>` 身份，新写出的文件 owner 自然是 `pcg123_<uid>`，但 wrapper 在 bash 退出后会扫一遍 `! -user mqq` 把 owner 收归 mqq，使 SDK 后续以 mqq 身份做 collect / metadata 写入时一直走 owner 通道，不依赖 NFS server 端的特殊能力。跨会话隔离的承担者始终是 group + 目录模式，与 owner 是不是 mqq 无关。

### uid/gid 派生

uid/gid 由 execID 哈希派生（范围 `[100000, 2 × 10⁹)`），沙箱重连或进程重启后能稳定重建，无需额外状态。setgid bit 让工作区子目录与文件自动继承 group，无需逐文件维护。系统用户的存在让 `whoami` / `id -un` / `getpwuid()` 等查找在 bash 里都能拿到合法的名字，避免运行时 stderr 噪声。

### 关闭隔离

某些场景需要 Skill 显式以沙箱默认用户 mqq 身份运行（例如访问只对 mqq 授权的本地资源），可关掉隔离：

```go
executor, cancel, err := pcg123.NewCodeExecutor(conf,
    pcg123.WithSessionIsolation(false),
)
```

> 关闭后多个会话的 Skill 进程会共用同一 owner 通道，相互之间没有 POSIX 级阻挡。如果同一沙箱真有跨会话需求（例如启用了 CFS 卷挂载做"会话间共享"），按业务需要决定是否关闭；否则保持默认。

## 运行方式

### 方式一：使用本地示例 Skills

#### 基础运行（使用默认工作区）

```bash
# 设置环境变量
export PCG123_SECRET_ID="your-secret-id"
export PCG123_SECRET_KEY="your-secret-key"
export OPENAI_API_KEY="your-openai-api-key"
export OPENAI_BASE_URL="https://api.openai.com/v1"

# 运行示例（使用本地 skills/ 目录）
cd 05_skill_execution
go run main.go
```

#### 高级运行（使用 CFS 卷挂载）

如需启用 CFS 工作区挂载以实现数据持久化，需修改 `main.go` 中的配置：

```go
// 修改 CFS 卷挂载配置（第 137-144 行）
pcg123.WithSkillWorkspaceMount(pcg123.WorkspaceVolumeMount{
    CFSVolumeMount: &pcg123.CFSVolumeMount{
        Name:    "your-cfs-name",        // 替换为你的 CFS 名称
        Host:    "your-cfs-host",        // 替换为你的 CFS 挂载点
        Path:    "/",                    // 挂载路径
        Version: pcg123.CFSVersion3,     // CFS 协议版本
    },
})
```

然后运行程序：

```bash
export PCG123_SECRET_ID="your-secret-id"
export PCG123_SECRET_KEY="your-secret-key"
export OPENAI_API_KEY="your-openai-api-key"
export OPENAI_BASE_URL="https://api.openai.com/v1"

go run main.go
```

### 方式二：使用官方 Anthropic Skills

```bash
# 1) 克隆官方 skills 仓库
git clone https://github.com/anthropics/skills \
  "$HOME/src/anthropics-skills"

# 2) 设置 SKILLS_ROOT 指向官方仓库
export SKILLS_ROOT="$HOME/src/anthropics-skills"

# 3) 设置其他环境变量
export PCG123_SECRET_ID="your-secret-id"
export PCG123_SECRET_KEY="your-secret-key"
export OPENAI_API_KEY="your-openai-api-key"
export OPENAI_BASE_URL="https://api.openai.com/v1"

# 4) 运行示例（使用官方 skills）
cd 05_skill_execution
go run main.go
```

官方 Skills 仓库包含丰富的预定义技能，如：
- **算法艺术** (algorithmic-art)：生成程序化艺术
- **数据分析** (data-analysis)：处理和分析数据
- **文档转换** (doc-conversion)：PDF、Word 文档处理
- **网络工具** (web-tools)：网页抓取和搜索
- 等等

## 对话示例

### 使用本地示例 Skills

#### 示例 1: 基础数学计算 (python-math) - **基础特性展示**

```
👤 You: 列出技能
🤖 Assistant: 当前可用技能：
- python-math - Python 数学计算工具
- data-pipeline - 数据流水线分析技能

👤 You: 计算斐波那契数列前10个
🔧 工具调用:
   • skill_load(name="python-math")
🔄 执行中...
✅ 工具结果: 已加载技能文档
🔧 工具调用:
   • skill_run(command="python scripts/fib.py 10")
🔄 执行中...
✅ 工具结果: {"stdout": "Fibonacci sequence (first 10 numbers):\n0, 1, 1, 2, 3, 5, 8, 13, 21, 34\n", "exit_code": 0}
🤖 Assistant: 斐波那契数列前10个数是：0, 1, 1, 2, 3, 5, 8, 13, 21, 34
```

#### 示例 2: 数据流水线分析 (data-pipeline) - **高级特性展示**

```
👤 You: 使用 data-pipeline 技能进行完整的数据分析流程
🤖 Assistant: 我将使用 data-pipeline 技能执行完整的数据分析流程，包括数据生成、清洗、分析和可视化。

🔧 工具调用:
   • skill_load(name="data-pipeline")
🔄 执行中...
✅ 工具结果: 已加载技能文档

🔧 工具调用:
   • skill_run(command="python3 scripts/generate_data.py --rows 1000 --output out/raw_data.csv")
🔄 执行中...
✅ 工具结果: {"stdout": "✅ 已生成 1000 行数据到 out/raw_data.csv", "exit_code": 0}

🔧 工具调用:
   • skill_run(command="python3 scripts/clean_data.py --input work/inputs/raw_data.csv --output out/clean_data.csv",
               inputs=[{"from": "workspace://out/raw_data.csv"}])
🔄 执行中...
✅ 工具结果: {"stdout": "✅ 数据清洗完成，保留率: 92.3%", "exit_code": 0}

🔧 工具调用:
   • skill_run(command="python3 scripts/analyze_data.py --input work/inputs/clean_data.csv --report out/analysis_report.txt",
               inputs=[{"from": "workspace://out/clean_data.csv"}])
🔄 执行中...
✅ 工具结果: {"stdout": "✅ 数据分析完成", "exit_code": 0}

🔧 工具调用:
   • skill_run(command="python3 scripts/visualize_data.py --input work/inputs/clean_data.csv --config work/inputs/config.json --output out/plots/",
               inputs=[
                 {"from": "workspace://out/clean_data.csv"},
                 {"from": "skill://templates/plot_config.json"}
               ],
               outputs=["out/plots/*.png"])
🔄 执行中...
✅ 工具结果: {"stdout": "✅ 所有图表已生成到目录: out/plots/", "output_files": [...]}

🤖 Assistant: 数据分析流程已完成！生成了以下文件：
- 📊 原始数据：out/raw_data.csv
- 🧹 清洗数据：out/clean_data.csv
- 📈 分析报告：out/analysis_report.txt
- 📊 统计数据：out/stats.json
- 🎨 可视化图表：out/plots/*.png（4张图表）

展示了以下高级特性：
✅ workspace:// - 脚本之间通过工作区传递数据
✅ skill:// - 从 skill 目录读取配置模板
✅ outputs - 使用 glob 模式自动收集输出文件
```

### 使用官方 Anthropic Skills

```
👤 You: 列出技能
🤖 Assistant: 可用技能包括：
- algorithmic-art - 生成程序化艺术
- data-analysis - 数据分析和可视化
- pdf - PDF/Word 文档转换
- web-tools - 网页抓取和搜索
- ... 等等

👤 You: 用 pdf 技能生成一个基础pdf文档
🔧 工具调用:
   • skill_load(name="pdf")
🔄 执行中...
🔧 工具调用:
   • skill_run(command="xxxx")
🔄 执行中...
✅ 工具结果: {"stdout": "Generated PDF", "output_files": [{"name": "out/base.pdf"}]}
🤖 Assistant: 已生成一个基础pdf文档，保存在工作区 out/base.pdf
```

## 目录结构

```
05_skill_execution/
├── main.go              # 主程序
├── README.md            # 说明文档
└── skills/              # 技能仓库
    ├── python_math/     # 数学计算技能（基础示例）
    │   ├── SKILL.md     # 技能文档
    │   └── scripts/     # 脚本文件
    │       ├── fib.py   # 斐波那契计算
    │       └── primes.py # 素数计算
    └── data_pipeline/   # 数据流水线技能（高级示例）
        ├── SKILL.md     # 技能文档
        ├── scripts/     # 脚本文件
        │   ├── generate_data.py    # 数据生成
        │   ├── clean_data.py       # 数据清洗
        │   ├── analyze_data.py     # 数据分析
        │   ├── visualize_data.py   # 数据可视化
        │   └── generate_report.py  # 报告生成
        └── templates/   # 配置模板
            └── plot_config.json    # 可视化配置
```

## 与本地执行器的区别

### 核心差异对比

| 特性维度            | 本地执行器<br>(local/container)     | PCG123 远程执行器                 |
|-----------------|--------------------------------|------------------------------|
| **执行环境**        | 本地机器或本地容器                      | PCG123 远程容器                  |
| **工作区管理**       | 本地文件系统 (临时目录或指定目录)             | NFS 远程文件系统（默认）<br>CFS 持久化存储（可选） |
| **代码执行**        | 直接调用本地解释器 (python/bash)        | 通过 PCG123 RPC 接口远程执行         |
| **文件操作**        | 本地文件系统 API 调用                  | NFS 客户端远程读写 / CFS 卷直接访问      |
| **隔离性**         | 无隔离 (local) 或容器级隔离 (container) | 容器级隔离 + 远程网络隔离               |
| **网络访问**        | 取决于本地网络策略                      | 允许外网访问                       |
| **Python 版本**   | 支持多版本 (取决于本地环境)                | 仅支持 Python 3.10+             |
| **文件传输**        | 直接访问，无传输开销                     | 通过 NFS 协议远程操作 / CFS 高性能访问    |
| **数据持久化**       | ❌ 容器销毁后数据丢失                    | ✅ 支持 CFS 卷挂载，数据持久化          |
| **跨会话共享**       | ❌ 不支持                           | ✅ 支持 CFS 卷挂载，多会话共享数据        |


### 使用场景建议

**选择本地执行器**：
- ✅ 快速原型开发和调试
- ✅ 不需要严格的环境隔离
- ✅ 受限网络环境（无法访问外部服务）
- ✅ 需要使用特定 Python 版本或本地依赖

**选择 PCG123 远程执行器（默认 NFS 工作区）**：
- ✅ 需要严格的代码执行隔离
- ✅ 需要执行不受信任的第三方 skills
- ✅ 需要防止本地环境污染
- ✅ 临时性测试和演示场景

**选择 PCG123 远程执行器 + CFS 卷挂载**：
- ✅ 需要保留中间计算结果供后续使用
- ✅ 需要在多个 Agent 会话之间共享数据
- ✅ 需要处理大文件或大规模数据集
- ✅ 需要持久化存储技能生成的制品
- ✅ 需要高性能的文件访问能力

## 文件传输机制

PCG123 执行器支持两种工作区存储机制：**容器内文件系统**（默认）和 **CFS 卷挂载**（可选）。

### 方式一：容器内文件系统（默认）

容器内文件系统是默认的工作区存储方式，适合临时性任务和测试场景。

### 容器内文件系统工作

```
本地 Agent                    NFS 服务器                     PCG123 容器
   │                              │                               │
   │ 1. CreateWorkspace            │                               │
   ├────────────────────────────>│ 2. MkdirAll(ws_id)            │
   │                              ├────────────────────────────>│
   │                              │                               │
   │ 3. PutFiles                   │                               │
   ├────────────────────────────>│ 4. WriteFile(path, data)      │
   │                              ├────────────────────────────>│
   │                              │                               │
   │ 5. RunProgram                 │                               │
   ├────────────────────────────>│ 6. ExecuteCode RPC            │
   │                              ├────────────────────────────>│ 7. Python 执行
   │                              │                               │
   │ 8. Collect Outputs            │                               │
   ├────────────────────────────>│ 9. ReadFile/Glob               │
   │                              ├────────────────────────────>│
```

### 性能考虑

**优势**：
- ✅ **透明访问**：NFS 提供类似本地文件系统的访问接口
- ✅ **无需编码**：直接二进制传输，无 base64 编码开销
- ✅ **持久化**：工作区持久化在 NFS 服务器上，支持共享访问
- ✅ **大文件支持**：适合处理较大的文件和数据集

**限制**：
- ⚠️ **网络延迟**：所有文件操作都需要网络往返时间 (RTT)
- ⚠️ **NFS 依赖**：依赖 NFS 服务器的可用性和性能
- ⚠️ **并发限制**：NFS 服务器的并发连接数限制
- ⚠️ **临时性**：工作区数据可能因为容器销毁而丢失

### 方式二：CFS 卷挂载（可选）

CFS（Cloud File System）是可选的持久化存储方式，通过 `pcg123.WithSkillWorkspaceMount` 配置启用。

#### CFS 工作原理

```
本地 Agent                    CFS 服务器                      PCG123 容器
   │                              │                               │
   │ 1. InitExecutor              │                               │
   ├────────────────────────────>│ 2. 挂载 CFS 卷到容器             │
   │                              ├────────────────────────────>│ 3. 容器内可访问 /mnt/cfs
   │                              │                               │
   │ 4. CreateWorkspace           │                               │
   ├────────────────────────────>│ 5. 在 CFS 卷上创建工作区目录        │
   │                              ├────────────────────────────>│
   │                              │                               │
   │ 6. PutFiles/RunProgram       │                               │
   ├────────────────────────────>│ 7. 直接在 CFS 工作区目录操作文件      │
   │                              ├────────────────────────────>│ 8. Python 直接读写本地文件
   │                              │                               │
   │ 9. Collect Outputs           │                               │
   ├────────────────────────────>│ 10. 从 CFS 工作区读取结果文件       │
```

#### 性能考虑

**优势**：
- ✅ **数据持久化**：工作区数据持久化存储在 CFS，容器销毁不丢失
- ✅ **多节点共享**：多个执行器实例可共享同一 CFS 工作区
- ✅ **高性能**：CFS 提供高吞吐、低延迟的文件访问能力
- ✅ **大文件支持**：特别适合处理大文件和大规模数据集
- ✅ **直接访问**：容器内直接通过本地文件系统访问，无额外编码开销

**限制**：
- ⚠️ **CFS 依赖**：依赖 CFS 文件系统的可用性和性能
- ⚠️ **配置要求**：需要预先创建 CFS 文件系统并配置正确参数
- ⚠️ **网络依赖**：执行环境需要能够访问 CFS 挂载点
- ⚠️ **成本考虑**：CFS 存储可能产生额外的存储成本
