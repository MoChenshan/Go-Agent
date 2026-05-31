# Subagent runtime

OpenClaw 的 subagent 能力现在复用上游 `agent/taskrun` 运行时。
框架侧把这类能力命名为 task run；OpenClaw 侧继续保留 subagent 的产品
术语，并只保留渠道投递、通知文案、权限和企微命令这些适配层逻辑。

## Agent 侧触发

启用 OpenClaw tools 后，父 agent 可以使用这些工具管理后台任务：

- `subagents_spawn`
- `subagents_list`
- `subagents_get`
- `subagents_cancel`
- `subagents_wait`

`subagents_spawn` 至少需要传入 `task`：

```json
{
  "task": "生成前端页面并在完成后总结代码文件和截图位置",
  "timeout_seconds": 600
}
```

`mode` 用来控制父 agent 是否等待子任务：

- `async`：默认模式。启动子任务后立即返回 run id；任务完成或失败后，
  OpenClaw 会主动向当前聊天发送最终结果。
- `sync`：父 agent 等子任务进入终态后再继续；结果直接作为工具结果返回，
  不再额外发送后台完成通知。
- `review`：父 agent 等子任务进入终态，把结果展示给用户，并把下一次
  用户回复路由回同一个主 agent 续点，适合“用户审核后再继续”的流程。

`timeout_seconds` 限制子任务本身。`wait_timeout_seconds` 只限制
`sync` / `review` 模式下工具等待子任务的时间；等待超时不会取消子任务。

OpenClaw 会把当前会话作为父 session，给子任务注入当前企微投递目标。
子任务会继承当前 runtime profile 的 prompt、模型、工具策略和 workspace
等 context policy，避免绕过父会话的运行时限制。

## 代码侧触发

业务代码如果不依赖 OpenClaw 的渠道适配，可以直接使用上游通用包：

```go
store, err := inprocess.NewFileStore("task-runs/runs.json")
if err != nil {
	return err
}

svc, err := inprocess.NewService(
	r,
	inprocess.WithStore(store),
	inprocess.WithObserver(taskrun.ObserverFunc(
		func(ctx context.Context, run taskrun.Run) {
			if run.Status.IsTerminal() {
				log.Printf("task run %s finished: %s", run.ID, run.Status)
			}
		},
	)),
)
if err != nil {
	return err
}
svc.Start(ctx)
defer svc.Close()

run, err := svc.Spawn(ctx, taskrun.SpawnRequest{
	OwnerUserID:     "user-123",
	ParentSessionID: "chat-456",
	AgentName:       "worker",
	Task:            "review generated code and screenshots",
	Timeout:         10 * time.Minute,
})
if err != nil {
	return err
}

final, err := svc.Wait(ctx, run.ID)
if err != nil {
	return err
}
log.Printf("result: %s", final.Result)
```

如果代码运行在 OpenClaw 进程内，并且需要复用 OpenClaw 已经创建好的
runtime，可以通过 `Runtime.SubagentService()` 查询和取消任务。创建任务
仍建议让 agent 使用 `subagents_spawn`，这样 OpenClaw 可以自动带上当前
会话、用户和渠道投递目标。

## 状态持久化

`agent/taskrun/inprocess` 提供 `MemoryStore` 和 `FileStore`。
OpenClaw 默认使用 `FileStore`，文件位于
`state_dir/subagents/runs.json`。进程重启后，之前处于非终态的任务会被
标记为失败，避免把已经中断的任务展示为仍在运行。
