# 服务端示例

该目录下包含以下启动 AG-UI 服务的示例：

| 子目录 | 适用场景 |
|--------|----------|
| `default/`   | 结合 `trpc-go.yaml` 配置文件使用 `trpc-go` 启动 SSE 服务。 |
| `event_emitter/` | GraphAgent + NodeFunc 中通过 EventEmitter 主动推送自定义事件、进度与流式文本。 |
| `follow/` | 启用 `/history` follow：快照返回后继续跟随输出直到当前 run 结束。 |
| `graph/` | GraphAgent 节点生命周期/中断事件（`ACTIVITY_DELTA`）示例，包含中断与恢复。 |
| `externaltool/` | GraphAgent 外部工具两段式调用（`role=user` → 中断 → `role=tool` 续跑）。 |
| `finishresult/` | 通过自定义 Translator 将模型 finish_reason 注入 `RUN_FINISHED.result`。 |
| `multiagent/`   | 单个 `http_no_protocol` service 上注册多个 AG-UI server，通过不同 `basePath` 区分路由。 |
| `react/`   | 通过自定义 Translator 按照 react 标签划分自定义事件。 |
| `langfuse/`   | 通过 TranslateCallback 接入 Langfuse 可观测平台。 |
| `report/` | Report 形态输出示例：通过工具信号在前端打开/关闭文档侧栏并渲染结构化报告。 |
| `thinkaggregator/` | 将模型 think 作为自定义事件输出，并按会话聚合与持久化。 |
| `zhiyan/`   | 通过 TranslateCallback 接入智研监控宝。 |
| `galileo/`   | 上报伽利略平台。 |
