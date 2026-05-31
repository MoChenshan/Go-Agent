#!/usr/bin/env bash
# smoke_llm.sh —— project-llm 最小 smoke 验证
#
# 在不下载真实大模型的前提下，验证：
#   1. data_pipeline.py 能跑通（产出 sft_demo.jsonl）
#   2. red_team_eval.py 能 import & --help
#   3. evaluate.py 能 import & --help
#   4. merge_lora.py 能 --help
#   5. eval/golden_50.jsonl 是合法 JSONL
#   6. eval/red_team.jsonl 是合法 JSONL

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$ROOT/project-llm"

echo "[1/6] data_pipeline.py dry-run ..."
python scripts/data_pipeline.py --in data/raw --out /tmp/gago_pipe_out

echo
echo "[2/6] red_team_eval.py --help ..."
python scripts/red_team_eval.py --help >/dev/null
echo "OK"

echo
echo "[3/6] evaluate.py --help ..."
python scripts/evaluate.py --help >/dev/null
echo "OK"

echo
echo "[4/6] merge_lora.py --help ..."
python scripts/merge_lora.py --help >/dev/null
echo "OK"

echo
echo "[5/6] golden_50.jsonl JSON 合法性 ..."
python - <<'PY'
import json
n = 0
with open("eval/golden_50.jsonl", encoding="utf-8") as f:
    for i, line in enumerate(f, 1):
        line = line.strip()
        if not line:
            continue
        try:
            json.loads(line)
            n += 1
        except json.JSONDecodeError as e:
            raise SystemExit(f"line {i}: {e}")
print(f"OK ({n} lines)")
PY

echo
echo "[6/6] red_team.jsonl JSON 合法性 ..."
python - <<'PY'
import json
n = 0
with open("eval/red_team.jsonl", encoding="utf-8") as f:
    for i, line in enumerate(f, 1):
        line = line.strip()
        if not line:
            continue
        try:
            json.loads(line)
            n += 1
        except json.JSONDecodeError as e:
            raise SystemExit(f"line {i}: {e}")
print(f"OK ({n} lines)")
PY

echo
echo "✅ llm smoke passed"
