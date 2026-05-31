### 配置智研和langfuse 环境变量

```bash
export ZHIYANLLM_API_ENDPOINT="https://trace.zhiyan.tencent-cloud.net:4318"
export ZHIYANLLM_API_KEY="key-xxxx"
export ZHIYANLLM_APP_NAME="llm-trpc-go-server"

export LANGFUSE_PUBLIC_KEY="your-public-key"
export LANGFUSE_SECRET_KEY="your-secret-key"
export LANGFUSE_HOST="your-langfuse-host"
export LANGFUSE_INSECURE="true" # for insecure connections (development only)
```