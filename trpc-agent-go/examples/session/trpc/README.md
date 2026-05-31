# Redis Session Agent Example

This example demonstrates a session-enabled chat agent that persists conversation state in Redis. The Redis target is not hardcoded; it is resolved from `trpc_go.yaml` via the service name.

## What It Shows

- Session persistence in Redis via `sessionService`.
- Target resolution by service name from config (no hardcoded address).
- Interactive commands: `/new`, `/history`, `/exit`.

## Paths

- Example entry: `trpc-agent-go/examples/session/redis/main.go`
- Config: `trpc-agent-go/examples/session/redis/trpc_go.yaml`
  - Service name: `trpc.test.helloworld.redis`
  - Target format: `redis://<username>:<password>@127.0.0.1:6379`

## Prerequisites

- Go 1.23+
- Docker + Docker Compose (for one-click Redis startup)

## Start Redis (with ACL user)

Use the provided compose file with an init step that creates the ACL user:

```bash
cd trpc-agent-go/examples/session/redis/deployredis
docker compose up -d

# Verify (matches credentials in trpc_go.yaml)
redis-cli -u redis://username:password@127.0.0.1:6379 ping   # PONG
```

How it works: the compose starts Redis with `--requirepass adminpass`, then an init service creates and enables the `username/password` ACL user and disables the default user. The application always connects using `username/password`.

## Run the Example

```bash
cd trpc-agent-go/examples/session/redis
go run main.go -model deepseek-chat
```

启动输出示例：

```
🚀 Redis Session Agent
Model: deepseek-chat
==================================================
✅ Chat ready! Session: chat-session-...
🔗 Session backend: service 'trpc.test.helloworld.redis' (target from trpc_go.yaml)

💡 Commands:
   /new      - Start a new session
   /history  - Show conversation history
   /exit     - End the conversation
```

## Commands

- `/new`: Start a new session (resets conversation context)
- `/history`: Ask the assistant to show conversation history for the current session
- `/exit`: End the conversation

## Notes

- To change the username/password, update both the docker-compose init command and the `target` in `trpc_go.yaml`.
- Streaming is disabled in the example to keep console output concise (enable it in `GenerationConfig` if needed).
