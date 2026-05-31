## AG-UI 内网集成指南

### 结合 tRPC-Go 启动 AG-UI 服务

结合 `trpc-go.yaml` 配置文件使用 `trpc-go` 启动 SSE 服务，即可复用 `trpc-go` 框架的能力。

代码：

```go
import (
	"git.code.oa.com/trpc-go/trpc-go"
	_ "git.woa.com/trpc-go/trpc-agent-go/trpc"
	tagui "git.woa.com/trpc-go/trpc-agent-go/trpc/agui" // 1. 导入内网 agui
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/server/agui"
)

agent := newAgent()
runner := runner.NewRunner(agent.Info().Name, agent)
// 2. 加载配置文件，创建 trpc 服务
server := trpc.NewServer()
// 3. 创建 AG-UI 服务
aguiServer, err := agui.New(runner, agui.WithPath("/agui"))
if err != nil {
	log.Fatalf("failed to create AG-UI server: %v", err)
}
// 4. 将 AG-UI server 注册到 trpc service
if err := tagui.RegisterAGUIServer(server, "trpc.test.helloworld.agui", aguiServer); err != nil {
	log.Fatalf("failed to register AG-UI server: %v", err)
}
// 5. 启动 trpc 服务
if err := server.Serve(); err != nil {
	log.Fatalf("server stopped with error: %v", err)
}
```

配置文件：

```yaml
server:
  service:
    - name: trpc.test.helloworld.agui
      ip: 127.0.0.1               # 服务监听 ip 地址
      port: 8080                  # 服务监听端口
      protocol: http_no_protocol  # 应用层协议
```

完整代码参见 [examples/agui/server/default](../examples/agui/server/default/)。

### 前端集成指引

TDesign Oteam近期推出的新版 `Chatbot组件` 已完成对 AG-UI 协议的适配支持，提供开箱即用的聊天界面组件，支持流式对话管理、协议标准事件转换及渲染支持、工具调用注册和状态管理、历史消息结构化等构建Agent应用所需的核心能力，可以无缝集成符合 AG-UI 标准的后端服务。

**快速接入：**

以React框架为例：

```javascript
import { ChatBot } from '@tdesign-react/chat';

export default function AguiChat() {
  const chatServiceConfig = {
    endpoint: '/agui', // 对应上面配置的 AG-UI 服务路径
    protocol: 'agui',  // 启用 AG-UI 协议
    stream: true,
  };

  return <ChatBot chatServiceConfig={chatServiceConfig} />;
}
```

更多详细配置和高级功能（如工具调用、状态管理等）和示例请参考：[TDesign Chat AG-UI 集成文档](https://tdesign.woa.com/react-chat/components/chat-engine#ag-ui-%E5%8D%8F%E8%AE%AE)，接入可以咨询@lincao, @uyarnchen

### 可观测平台上报

#### 伽利略

匿名导入启用伽利略上报，与伽利略的结合示例可参考 [examples/agui/server/galileo](../examples/agui/server/galileo/)。

#### 智研监控宝 -- LLM应用监控

在事件翻译前后插入监控上报逻辑，与智研监控宝的结合示例可参考 [examples/agui/server/zhiyan/llmsdk](../examples/agui/server/zhiyan/llm-sdk/)。
