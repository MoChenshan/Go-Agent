# Debug 服务端示例

本示例演示如何创建一个兼容 [ADK Web UI](https://github.com/google/adk-web) 的独立 HTTP 服务端，展示了 trpc-agent-go 与 trpc-go 框架的集成方式，并且完全对齐了 ADK Web 所需的接口规范。

## 接口规范

Debug Server 实现了 ADK Web 所需的核心接口，支持基本的 agent 交互功能，具体接口定义可参考 [OpenAPI 规范](../../trpc/server/debug/openapi.json)。注意：某些高级功能如图片生成、详细追踪和扩展元数据等可能未完全实现。

## 前置条件

- Go 1.21 及以上版本
- NodeJS & npm（用于运行 ADK Web UI）

## 功能特性

- **HTTP 服务**：兼容 ADK Web UI，便于手动测试
- **LLM Agent**：集成 DeepSeek Chat 模型，内置计算器和时间工具
- **trpc-go 集成**：基于 trpc-go 框架实现 HTTP 服务
- **CORS 支持**：内置 CORS 中间件，支持 Web 浏览器访问

## 配置说明

服务端通过 `trpc_go.yaml` 进行配置：

```yaml
server:
  service:
    - name: trpc.test.debug.stdhttp
      ip: 127.0.0.1
      port: 8000
      protocol: http_no_protocol
      timeout: 0 # 0 表示 LLM 请求不超时
```

本示例使用了内网包。请导入如下依赖：

```go
import (
  // 导入 内网 trpc-agent-go 以获取内网相关支持。
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
  // 导入 trpc-go 包以创建 tRPC 服务端。
	"git.code.oa.com/trpc-go/trpc-go"
	// 导入 trpc-go/http 包以注册 HTTP 服务 handler。
	thttp "git.code.oa.com/trpc-go/trpc-go/http"
)
```

## 启动服务端

```bash
# 在仓库根目录下
cd examples/debugserver

# 启动服务（自动加载 trpc_go.yaml 配置）
go run .

# 服务将监听 trpc_go.yaml 中指定的 IP:端口
# 默认：http://127.0.0.1:8000
```

## 启动 ADK Web UI

首次运行请先克隆前端并安装依赖：

```bash
git clone https://github.com/google/adk-web.git
cd adk-web
npm install

# 指定后端为本地 Go 服务
npm run serve --backend=http://localhost:8000
```

在浏览器中打开 <http://localhost:4200>，在左侧选择 `assistant` 应用，创建新会话并开始对话。消息将通过 `/run_sse` 接口实时流式返回。

## API 接口

服务端为 ADK Web 提供如下核心接口：

- `GET /list-apps` - 获取可用 agent 列表
- `POST /run` - 执行 agent 工具
- `POST /run_sse` - 执行 agent 并通过 SSE 实时流式返回
- `GET /apps/{appName}/users/{userId}/sessions` - 获取会话列表
- `POST /apps/{appName}/users/{userId}/sessions` - 创建会话
- `GET /apps/{appName}/users/{userId}/sessions/{sessionId}` - 获取会话详情

注意：本实现专注于基本的 agent 功能。高级功能如图片生成、详细执行追踪和完整元数据等可能需要额外实现。

## 工具说明

示例 agent 内置两个工具：

1. **计算器**：支持基本的加减乘除运算
2. **时间查询**：获取指定时区的当前时间和日期

## 说明

- 服务端会自动加载 `trpc_go.yaml` 配置文件
- 已启用 CORS，支持 Web 浏览器访问
- LLM 请求无超时限制（timeout: 0），适合大模型响应较慢的场景

---

你可以根据需要在 `main.go` 中替换 agent 逻辑或添加更多工具。

# 123 平台部署

## 部署准备

- 请确保已成功申请 123 平台的环境资源。
- 复制 [示例代码](./main.go) 到您的代码仓库，并将相关名称替换为您在 123 平台申请的实际环境信息。

```go
s := trpc.NewServer()
server := debug.New(agents)
// 请将 {app} 和 {server} 替换为您在 123 平台申请的 app 和 server 名称
thttp.RegisterNoProtocolServiceMux(s.Service("trpc.{app}.{server}.stdhttp"), server.Handler())

log.Infof("Debug server listening on %s (apps: %v)", getIPPort(), agents)
if err := s.Serve(); err != nil {
	log.Fatalf("server error: %v", err)
}
```

并同步修改对应的配置文件：

```yaml
server: # 服务端配置
  app: { app } # 业务的应用名
  server: { server } # 进程服务名
  service: # 业务服务提供的 service，可以有多个
    - name: trpc.{app}.{server}.stdhttp # service 的路由名称
      ip: 127.0.0.1 # 服务监听 ip 地址，可使用占位符 ${ip}，ip 和 nic 二选一，优先 ip
      port: 8000 # 服务监听端口，可使用占位符 ${port}
      network: tcp # 网络监听类型 tcp/udp
      protocol: http_no_protocol # 应用层协议 trpc/http
      timeout: 0 # 请求最长处理时间，单位毫秒。LLM 请求时间较长，建议设置为 0 表示不超时
```

## 配置 123 平台服务参数

![](../.resources/debugserver/img/trpc-framework-config.png)

## 构建镜像

![](../.resources/debugserver/img/images-building.png)

## 发布节点

![](../.resources/debugserver/img/node-release.png)

此外，您也可以使用 `dtools` 工具发布自己编译好的二进制文件，具体可参考 [Dtools 命令行工具文档](https://iwiki.woa.com/p/887410188)。

登录容器后台后，可看到服务已成功运行。

![](../.resources/debugserver/img/running-success.png)

## 启动 ADK Web UI

在 **自己的 DevCloud 开发环境** 中运行以下命令：

```bash
# 首次运行请先克隆前端并安装依赖
git clone https://github.com/google/adk-web.git
cd adk-web
npm install

# 指定后端为 123 平台发布的节点
npm run serve --backend=http://{your_node_ip:port} -- --port=4200 --host=localhost
```

其中 `{your_node_ip:port}` 为发布节点的宿主机/节点 IP 和服务运行端口，`--port` 与 `--host` 用于指定 ADK Web 的监听地址与端口。注意：DevCloud 网络可能无法直接访问 123 平台节点的 IDC 网络，如遇网络不通问题，可通过调整网络策略（可以参考 [DevCloud 机器如何访问 IDC 服务](https://iwiki.woa.com/p/1440211908)），或为 123 服务 [配置 IAS 域名转发](https://ias.woa.com/)。

本示例本质上是一个通用的 HTTP 标准服务实现。您可以参考 [搭建泛 HTTP 标准服务](https://iwiki.woa.com/p/490796278) 和 [调用泛 HTTP 标准服务](https://iwiki.woa.com/p/482598119) 以获取更多部署与调用细节。
