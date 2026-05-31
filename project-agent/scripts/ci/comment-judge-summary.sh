#!/usr/bin/env bash
# comment-judge-summary.sh — 把 judge_report.json 解析为 Markdown 评论贴到 MR
#
# 定位：D30.2 CI post-eval stage 的"最后一公里"——把 D30.1 落盘的 JSON 变成
# MR 页面上可读的分数表格。
#
# 为什么是 bash + jq 而非完整机器人：
#   1. 仓库里已有若干 shell 脚本，SRE 一眼就能读懂
#   2. 零额外依赖（alpine + jq + curl 足够）
#   3. 可本地复现：失败时 SRE 在本机直接 bash 跑就能复现 + 调整格式
#
# 用法：
#   bash comment-judge-summary.sh <json_path> <project_id> <mr_iid> <token>
#
# 参数语义：
#   json_path   D30.1 落盘的 judge_report.json
#   project_id  GitLab 项目 ID（CI 变量 $CI_PROJECT_ID）
#   mr_iid      MR 的 internal ID（CI 变量 $CI_MERGE_REQUEST_IID）
#   token       有 api 权限的 token（建议 Project Access Token + CI 变量 Masked）
#
# 设计决策：
#   - 任一步失败（文件不存在 / JSON 解析错 / API 调用失败）都只 warn 不 fail，
#     评论是旁路增强，绝不拖垮 CI。
#   - 分数表格按维度名字母序显示（利用 D30.1 的 dim_avg_order）稳定 diff。
#   - 低分用 ❌、达标用 ✅、无数据用 ➖ —— 一眼就能看出哪里退步。

set -u  # 未声明变量 → 报错；**不用 -e**，因为我们要自己决定哪些失败能吞

JSON_PATH="${1:-}"
PROJECT_ID="${2:-}"
MR_IID="${3:-}"
TOKEN="${4:-}"

warn() { echo "[comment-judge-summary] WARN: $*" >&2; }
info() { echo "[comment-judge-summary] $*"; }

# -------------------- 前置校验（失败即 soft-exit 0） --------------------

if [ -z "$JSON_PATH" ] || [ -z "$PROJECT_ID" ] || [ -z "$MR_IID" ] || [ -z "$TOKEN" ]; then
  warn "参数不全：json_path=$JSON_PATH project_id=$PROJECT_ID mr_iid=$MR_IID token=<hidden>"
  warn "跳过 MR 评论（通常发生在 nightly schedule 场景）"
  exit 0
fi

if [ ! -f "$JSON_PATH" ]; then
  warn "JSON 文件不存在: $JSON_PATH（可能 evalrun 失败了，跳过评论）"
  exit 0
fi

if ! command -v jq >/dev/null 2>&1; then
  warn "jq 未安装，跳过 MR 评论"
  exit 0
fi

# -------------------- Schema 版本守护 --------------------

SCHEMA_VER=$(jq -r '.schema_version // ""' "$JSON_PATH")
if [ "$SCHEMA_VER" != "v1" ]; then
  warn "schema_version=$SCHEMA_VER 与本脚本预期(v1)不符；可能 DTO 升级了"
  warn "本脚本会尽力解析，若字段缺失评论会显示 ➖"
fi

# -------------------- 生成 Markdown 评论正文 --------------------

# 为什么用 /tmp/body.md：curl --data-urlencode 在超长 body 下容易截断，用 @file 最稳。
BODY_FILE=$(mktemp)
trap 'rm -f "$BODY_FILE"' EXIT

{
  echo "## 🧪 GameOps Agent — 离线评测报告"
  echo ""
  GEN_AT=$(jq -r '.generated_at // "N/A"' "$JSON_PATH")
  EVAL_SET=$(jq -r '.eval_set_id // "N/A"' "$JSON_PATH")
  echo "- **生成时间**：\`$GEN_AT\`"
  echo "- **评测集**：\`$EVAL_SET\`"
  echo "- **Pipeline**：[${CI_PIPELINE_ID:-N/A}](${CI_PIPELINE_URL:-#})"
  echo ""

  # 两个 Judge 各自渲染一张表
  for JUDGE_KEY in llm tool; do
    HAS=$(jq -r ".judges[\"$JUDGE_KEY\"] // empty | type" "$JSON_PATH")
    if [ -z "$HAS" ]; then
      continue  # 该 Judge 未启用
    fi

    case "$JUDGE_KEY" in
      llm)  JUDGE_TITLE="LLMJudge（质量维度）" ;;
      tool) JUDGE_TITLE="ToolSelectionJudge（工具选择维度）" ;;
    esac

    NOTE=$(jq -r ".judges[\"$JUDGE_KEY\"].note // \"\"" "$JSON_PATH")
    TOTAL=$(jq -r ".judges[\"$JUDGE_KEY\"].total // 0" "$JSON_PATH")
    PASSED=$(jq -r ".judges[\"$JUDGE_KEY\"].passed // 0" "$JSON_PATH")
    PASS_RATE=$(jq -r ".judges[\"$JUDGE_KEY\"].pass_rate // 0" "$JSON_PATH")
    PASS_PCT=$(awk "BEGIN{printf \"%.1f\", $PASS_RATE * 100}")

    echo "### $JUDGE_TITLE"
    echo ""
    if [ -n "$NOTE" ]; then
      echo "> \`$NOTE\`"
      echo ""
    fi
    echo "**批次汇总**：$PASSED / $TOTAL 通过（${PASS_PCT}%）"
    echo ""

    # 维度均分表
    echo "| 维度 | 均分 |"
    echo "|---|---|"
    jq -r ".judges[\"$JUDGE_KEY\"].dim_avg_order[]? as \$d | \"| \(\$d) | \(.judges[\"$JUDGE_KEY\"].dim_avg[\$d] | . * 100 | floor / 100) |\"" "$JSON_PATH"
    echo ""

    # 低分 case 明细（AllPass=false 的）
    FAILED_COUNT=$(jq -r "[.judges[\"$JUDGE_KEY\"].cases[]? | select(.all_pass==false)] | length" "$JSON_PATH")
    if [ "$FAILED_COUNT" -gt 0 ]; then
      echo "<details><summary>❌ $FAILED_COUNT 个未达标 case（点开展开）</summary>"
      echo ""
      echo "| Case | 均分 | 失败维度 |"
      echo "|---|---|---|"
      jq -r ".judges[\"$JUDGE_KEY\"].cases[]? | select(.all_pass==false) | \"| \(.case_id) | \(.avg_score | . * 100 | floor / 100) | \([.scores[] | select(.pass==false) | .dimension] | join(\", \")) |\"" "$JSON_PATH"
      echo ""
      echo "</details>"
      echo ""
    else
      echo "✅ 全部 case 通过阈值"
      echo ""
    fi
  done

  echo "---"
  echo ""
  echo "<sub>💡 详细 JSON 见 Pipeline 的 \`project-agent/eval/output/judge_report.json\` artifact</sub>"
} > "$BODY_FILE"

# -------------------- 调用 GitLab API 发评论 --------------------

GITLAB_HOST="${CI_SERVER_URL:-https://git.woa.com}"
API_URL="${GITLAB_HOST}/api/v4/projects/${PROJECT_ID}/merge_requests/${MR_IID}/notes"

info "Posting comment to $API_URL"

# 说明：
#   -sS    静默但保留错误信息
#   -o     把 response body 写到临时文件
#   -w     打印 HTTP 状态码
#   **不加 -f**：-f 会让 curl 在 HTTP 4xx/5xx 时以非零退出，配合 || echo 000
#               会把"401 token 错"错误识别成"网络不通"，反而误导 SRE
HTTP_CODE=$(curl -sS -o /tmp/resp.txt -w "%{http_code}" \
  --header "PRIVATE-TOKEN: $TOKEN" \
  --data-urlencode "body@$BODY_FILE" \
  "$API_URL" 2>&1 || echo "000")

case "$HTTP_CODE" in
  201)
    info "MR 评论发布成功 (HTTP 201)"
    ;;
  000)
    warn "curl 调用失败（DNS/TCP 不通或证书错），跳过"
    ;;
  401|403)
    warn "GitLab API 鉴权失败 (HTTP $HTTP_CODE)，请检查 GITLAB_TOKEN_COMMENT 的 scope 是否含 api"
    cat /tmp/resp.txt >&2 || true
    ;;
  *)
    warn "GitLab API 返回 HTTP $HTTP_CODE，响应如下："
    cat /tmp/resp.txt >&2 || true
    ;;
esac

# 无论 API 结果如何，脚本总是返回 0 —— MR 评论是旁路增强，不应拖垮 CI
exit 0
