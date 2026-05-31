# taiji proxy

## 部署

### 编译
> 见 `makefile` 

```shell
# linux 编译
make 

# mac 编译
make mac
```

### 依赖配置

> 见 `trpc_go.yaml`、`proxy.json`

假设配置 taiji agent proxy:
```json
[
  {
    "name": "taiji-agent1",
    "proxy_agent_card": {
      "name": "taiji-agent1",
      "description": "test agent",
      "skills": [
        {
          "id": "01",
          "name": "Daily Q&A",
          "examples": [
            "who are you?"
          ]
        }
      ]
    },
    "agent_id": "12344", // agent id
    "remote_target": "http://stream-server-online-hyaide-app.turbotke.production.polaris:81",
    "path": "/openapi/app_platform/app_create",
    "authorization": "Bearer <KEY>" // token
  }
]
```

## 客户端测试

### 获取 agent card

```shell    
 curl -X GET \ 
  -v http://127.0.0.1:8000/api/v1/agent/taiji-agent1/.well-known/agent.json   
```

### stream message 

```shell
curl -X POST \
  -v http://127.0.0.1:8000/api/v1/agent/taiji-agent1/  \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc": "2.0",
    "id": "5eba7453-10f9-4036-9893-ddfa63e46996",
    "method": "message/stream",
    "params": {
        "message": {
            "kind": "message",
            "messageId": "msg-610ca156-571c-4d17-ad8d-535dc199def0",
            "parts": [
                {
                    "kind": "text",
                    "text": "who are you?"
                }
            ],
            "role": "user"
        }
    }
}'

```
