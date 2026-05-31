"""RAGAS 评测脚本（独立版）。

把 RAGAS 评测从 evaluate.py 里抽出来单独跑，便于：
- 单独回归 RAG 质量（不必跑全量 LLM-Judge）
- 在 CI 里作为快门禁（faithfulness 阈值 < 0.85 阻塞）
- 与 retriever 调参循环更紧密配合

依赖：
    pip install ragas datasets pandas

输入 schema（jsonl，每行一个）：
    {
      "question":  "BCS 集群标准命名规则",
      "answer":    "BCS-{K8S|MESOS}-{编号} ...",        # 待评模型回答
      "contexts":  ["原始检索回来的若干段...", "..."],   # retriever 返回
      "ground_truth": "BCS 集群名称采用 ..."             # 金标
    }

用法：
    python scripts/ragas_eval.py --golden eval/golden_50.jsonl \
        --metrics faithfulness,answer_relevancy,context_precision,context_recall \
        --threshold-faithfulness 0.85
"""

import argparse
import json
import os
import sys
from typing import Iterable, List


def load_jsonl(path: str) -> Iterable[dict]:
    with open(path, "r", encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            yield json.loads(line)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--golden", required=True)
    ap.add_argument("--metrics", default="faithfulness,answer_relevancy,context_precision,context_recall")
    ap.add_argument("--threshold-faithfulness", type=float, default=0.85)
    ap.add_argument("--threshold-answer-relevancy", type=float, default=0.80)
    ap.add_argument("--report", default="eval/ragas_report.json")
    ap.add_argument("--limit", type=int, default=0)
    args = ap.parse_args()

    try:
        from datasets import Dataset  # type: ignore
        from ragas import evaluate  # type: ignore
        from ragas.metrics import (  # type: ignore
            answer_relevancy,
            context_precision,
            context_recall,
            faithfulness,
        )
    except Exception as e:  # noqa: BLE001
        print(f"ERROR: ragas / datasets 未安装: {e}", file=sys.stderr)
        print("pip install ragas datasets pandas", file=sys.stderr)
        return 2

    metric_map = {
        "faithfulness": faithfulness,
        "answer_relevancy": answer_relevancy,
        "context_precision": context_precision,
        "context_recall": context_recall,
    }
    metrics = []
    for name in [m.strip() for m in args.metrics.split(",") if m.strip()]:
        if name not in metric_map:
            print(f"unknown metric: {name}", file=sys.stderr)
            return 2
        metrics.append(metric_map[name])

    rows: List[dict] = []
    for r in load_jsonl(args.golden):
        # 仅评 RAG 类样本（要求有 contexts）
        if not r.get("contexts"):
            continue
        rows.append({
            "question": r.get("prompt") or r.get("question"),
            "answer": r.get("answer") or r.get("reference") or "",
            "contexts": r["contexts"],
            "ground_truth": r.get("reference") or r.get("ground_truth") or "",
        })
    if args.limit > 0:
        rows = rows[: args.limit]
    if not rows:
        print("ERROR: 数据集中没有可评的 RAG 样本（contexts 为空）", file=sys.stderr)
        return 2

    print(f"evaluating {len(rows)} RAG samples with metrics={[m.name for m in metrics]}")
    ds = Dataset.from_list(rows)
    result = evaluate(ds, metrics=metrics)
    scores = {k: float(v) for k, v in result.scores.items()} if hasattr(result, "scores") else dict(result)
    print(json.dumps(scores, ensure_ascii=False, indent=2))

    os.makedirs(os.path.dirname(args.report) or ".", exist_ok=True)
    with open(args.report, "w", encoding="utf-8") as f:
        json.dump(
            {"n_samples": len(rows), "scores": scores, "thresholds": {
                "faithfulness": args.threshold_faithfulness,
                "answer_relevancy": args.threshold_answer_relevancy,
            }},
            f, ensure_ascii=False, indent=2,
        )
    print(f"report written: {args.report}")

    failed = []
    if "faithfulness" in scores and scores["faithfulness"] < args.threshold_faithfulness:
        failed.append(f"faithfulness {scores['faithfulness']:.3f} < {args.threshold_faithfulness}")
    if "answer_relevancy" in scores and scores["answer_relevancy"] < args.threshold_answer_relevancy:
        failed.append(f"answer_relevancy {scores['answer_relevancy']:.3f} < {args.threshold_answer_relevancy}")
    if failed:
        print("RAGAS GATE FAILED: " + "; ".join(failed), file=sys.stderr)
        return 1
    print("RAGAS gate passed ✓")
    return 0


if __name__ == "__main__":
    sys.exit(main())
