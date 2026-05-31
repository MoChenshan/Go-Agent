# GameOps Agent — Evaluation Framework (D12)

离线评估体系：让 Agent 的工具选择正确率、路由正确率、回答质量可量化、可回归。

## 目录结构

```
eval/
├── data/
│   └── gameops-core/                  # 核心评测集（D12 最小可用版，5 个 case）
│       ├── gameops-core.evalset.json  # Golden set：用户输入 + 期望轨迹 + 期望回复
│       └── gameops-core.metrics.json  # 指标配置：当前仅 tool_trajectory_avg_score
├── cmd/
│   └── evalrun/
│       ├── main_stub.go   # 默认构建（仅 golden 结构统计）
│       └── main_real.go   # -tags eval 构建（真实 Agent 推理 + 打分）
├── golden.go              # 纯数据层：Load / Validate / Summarize（无外部依赖）
├── golden_test.go         # 5 组静态校验（默认构建可跑）
└── README.md
```

## 5 个核心 case 速览

| EvalID | 场景 | 期望工具轨迹 | 目标验证 |
|---|---|---|---|
| `case_oom_diagnose` | OOM 告警诊断 | `bk_alarm_query` + `bcs_resource_query` + `bk_metrics_query` | DiagnosisAgent 多工具协同 |
| `case_bad_deploy_rollback` | 坏版本紧急回滚 | `devops_pipeline_rerun`（critical HITL） | RepairAgent 两段式确认 |
| `case_create_mr` | 代码修复发 MR | `gongfeng_mr_create`（medium HITL） | gongfeng 工具 + 安全闸门 |
| `case_kb_search` | 内部规范检索 | `knowledge_search` | KnowledgeAgent 本地 RAG |
| `case_status_query` | TAPD Bug 只读查询 | `tapd_bug_query` | 只读工具无 HITL 快速路径 |

## 评估指标（D12 当前启用）

- **`tool_trajectory_avg_score`**（threshold=1.0）
  - 逐 case 比较实际 tool_call 序列与 golden 期望（name / arguments / result 三字段）；
  - `orderSensitive=false` 允许并行工具乱序，与 LLM 真实行为匹配；
  - 三字段默认 exact 匹配，任意不等则该 case 该指标得 0 分；
  - threshold=1.0 表示整体平均分必须 100%，否则整批 `failed`。

**未来扩展（D14+）**：`response_match`（finalResponse 文本相似度）、`llm_judge`（用 judge model 做答复质量打分）、RAGAS 六指标（Faithfulness / AnswerRelevancy / ContextPrecision / ContextRecall / NoiseSensitivity / RetrievalDecision）。

## 两种运行模式

### 模式 1：默认构建（stub，零外部依赖，用于 CI 日常回归）

```bash
# 跑单测 —— 校验 golden set 结构、工具名引用一致性
go test ./eval/... -count=1 -v

# 跑 stub CLI —— 打印 golden 统计摘要
go run ./eval/cmd/evalrun
```

stub 模式只做**静态校验**，不触发 LLM 推理、不消耗 token，跑通代表：
- Golden set JSON 合法、appName 一致；
- 所有引用的工具名在当前 App 工具清单中存在（防止改名/删工具打爆评测）；
- Metric 配置合法，含 `tool_trajectory_avg_score`。

### 模式 2：真实评测（需 LLM 凭据）

```bash
# 编译真实评测 CLI（引入 trpc-agent-go/evaluation 独立 module）
go build -tags eval -o bin/evalrun ./eval/cmd/evalrun

# 执行（默认读 eval/data，输出到 eval/output）
./bin/evalrun --eval-set gameops-core --runs 1 --parallelism 2

# 退出码：0=全部通过，1=存在 failed，便于 CI 判断
```

每个 case 的结果 JSON 会落盘到 `eval/output/gameops-agent/...`。

## 新增 case 的工作流

1. 在 `eval/data/gameops-core/gameops-core.evalset.json` 追加 case：
   - `evalId`：全局唯一，建议 `case_<scene>` 命名；
   - `conversation[]`：至少 1 轮 `userContent` + `finalResponse`；
   - `tools[]`：期望的工具调用轨迹，`name` 必须精确匹配 `function.WithName(...)`；
   - `sessionInput.appName` 必须为 `gameops-agent`；
2. 跑 `go test ./eval/...` 校验通过；
3. （可选）跑 `go build -tags eval ./eval/... && ./bin/evalrun` 观察真实打分；
4. 提 MR。

## 新增工具后需同步的位置

工具新增/改名时：
1. 更新对应 `src/tools/**/xxx.go` 的 `function.WithName(...)`；
2. 同步 `eval/golden_test.go` 里的 `knownTools` map；
3. 相关 case 的 `tools[].name` 也要跟着改。

这三处任意一处错漏，`TestGoldenSet_ToolsExist` 都会红灯拦住你。

## 与执行方案的映射

> 执行方案 7.x：Routing Accuracy / Tool Call Accuracy / Answer Quality / RAGAS 六指标 / Trajectory Eval  
> 本轮 D12 最小可用落地：Tool Call Accuracy（= `tool_trajectory_avg_score`）+ Trajectory Eval（= `orderSensitive + defaultStrategy`）  
> 其余指标（Routing Accuracy 需 Transfer 轨迹、Answer Quality 需 judge model、RAGAS 六指标需 Python 生态）排入 **D14 评估体系进阶**。

## CI 集成（D30.2）

项目根目录的 [.gitlab-ci.yml](../.gitlab-ci.yml) 提供了评测流水线骨架，默认规则：

| Stage | 触发条件 | 作用 |
|---|---|---|
| `build → unit-test` | 每次 MR push / master | Go vet + `go test ./src/...` + `go test -tags eval ./eval/...` |
| `build → evalset-static-check` | 动到 `eval/data/**` 或 `eval/golden*.go` | 独立跑金标数据集静态校验，一眼指出"是数据坏了还是代码坏了" |
| `eval-offline → eval:nightly` | Schedule 触发（变量 `EVAL_NIGHTLY=true`） | 每夜跑一次真实 LLM 评测，产物保留 90 天 |
| `eval-offline → eval:on-demand` | MR 页面 **Play** 按钮 | PR 作者按需触发，比 nightly 快反馈 |
| `post-eval → eval:comment-mr` | `eval:on-demand` 成功后 | 解析 `judge_report.json` 自动贴 MR 评论 |

### 所需 CI 变量（Settings → CI/CD → Variables）

| Key | Masked | Protected | 说明 |
|---|---|---|---|
| `JUDGE_OPENAI_BASE_URL` | ✅ | ✅ | Judge 专用 LLM 的 BaseURL |
| `JUDGE_OPENAI_API_KEY` | ✅ | ✅ | Judge 专用 LLM 的 API Key |
| `GITLAB_TOKEN_COMMENT` | ✅ | ✅ | 有 `api` 权限的 token（建议 Project Access Token），用于发 MR 评论 |
| `EVAL_NIGHTLY` | — | — | 配到 Schedule 上，值为 `"true"` 才会触发 `eval:nightly` |

### 本地复现 CI job

```bash
cd project-agent

# 复现 unit-test
go vet ./...
go test -count=1 -race -timeout 10m ./src/...
go test -tags eval -count=1 -timeout 5m ./eval/...

# 复现 eval:on-demand（需 OPENAI_BASE_URL / OPENAI_API_KEY 环境变量）
mkdir -p ./eval/output
go run -tags eval ./eval/cmd/evalrun \
  --output-dir=./eval/output \
  --enable-llm-judge \
  --judge-model=hunyuan-turbo-s \
  --judge-include-tool-selection \
  --judge-json-out=./eval/output/judge_report.json \
  --judge-fail-on-threshold=false

# 复现 eval:comment-mr（需 jq）
cat ./eval/output/judge_report.json | jq '.judges | keys'
# 若要真跑评论，需要填 project_id / mr_iid / token
bash scripts/ci/comment-judge-summary.sh \
  ./eval/output/judge_report.json \
  "<PROJECT_ID>" "<MR_IID>" "<TOKEN>"
```

### `judge_report.json` Schema 版本

由 `eval/cmd/evalrun/judge_report_dto.go` 中的 `JudgeReportSchemaVersion` 常量锚定。当前 **`v1`**，破坏性字段变更必须同步 bump + 更新 `comment-judge-summary.sh` 的版本守护逻辑。
