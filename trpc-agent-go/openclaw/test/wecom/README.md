# WeCom Online-Like Tests

这套测试位于 `openclaw/test/wecom`，用于在接近真实部署的容器链路下验证 `openclaw` 的 WeCom 能力。

当前方案会：

- 使用真实 `CLAW_ID` 拉取远端 claw 配置。
- 使用线上同款镜像和 runner 启动容器。
- 使用当前源码构建出的 `trpc-claw` binary 覆盖容器内 release binary。
- 使用 fake WeCom websocket 和 fake media server 替代真实企微平台。
- 对开放式场景使用 evaluation 判分。

## 前置条件

运行前需要满足：

- 本机可用 Docker。
- 对应 `CLAW_ID` 的线上平台 container 已下掉。
- 该 `CLAW_ID` 不会被多个任务并发复用。
- 远端 claw 配置里已经包含：
  - `OPENAI_MODEL`
  - `OPENAI_API_KEY`
  - `OPENAI_BASE_URL`

## 环境变量

最小必需项：

- `WECOM_OPENCLAW_E2E=1`
- `WECOM_OPENCLAW_CLAW_ID`
- `WECOM_OPENCLAW_CLAW_AUTH_HEADERS`

兼容旧名字：

- `CLAW_ID`
- `CLAW_CONFIG_AUTH_HEADERS`

可选覆盖项：

- `WECOM_OPENCLAW_CLAW_CONFIG_URL`
- `WECOM_OPENCLAW_DOCKER_IMAGE`
- `WECOM_OPENCLAW_RUNNER_URL`
- `WECOM_OPENCLAW_JUDGE_MODEL`
- `WECOM_OPENCLAW_JUDGE_API_KEY`
- `WECOM_OPENCLAW_JUDGE_BASE_URL`
- `WECOM_OPENCLAW_EVAL_RESULT_DIR`

默认值见 [env_test.go](env_test.go)。

## 环境文件

模板文件在：

- [`.env.example`](.env.example)

推荐复制成私有文件再加载：

```bash
cp wecom/.env.example wecom/.env.local
set -a
source wecom/.env.local
set +a
```

## 运行方式

先进入 `openclaw/test` 模块目录：

```bash
cd openclaw/test
```

跑整包：

```bash
go test ./wecom -count=1 -v
```

跑单组：

```bash
go test ./wecom -run TestWeComOnlineLikeContainerImage -count=1 -v
go test ./wecom -run TestWeComOnlineLikeContainerPDF -count=1 -v
go test ./wecom -run TestWeComOnlineLikeContainerCron -count=1 -v
go test ./wecom -run TestWeComOnlineLikeContainerSmoke -count=1 -v
```

## 输出

evaluation 结果默认写到：

```text
output/wecom/evalresult
```

可通过 `WECOM_OPENCLAW_EVAL_RESULT_DIR` 覆盖。

具体测试场景和断言逻辑以当前目录下的 `*_test.go` 文件为准。
