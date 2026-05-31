"""
data_quality.py —— 数据质量检查 + 去重 + LLM-as-Judge + RAGAS 过滤

流程（按方案 2.2.4 节实现）：
    1) 规则过滤：长度 / 必填字段 / PII 脱敏（可选 presidio）
    2) SimHash chunk 去重（answer 级）
    3) Embedding 语义去重（question 级，BGE-M3 余弦 > sim_threshold）
    4) LLM-as-Judge 打分（Kimi-K2 / GPT-4o 异源评审），< judge_threshold 剔除
    5) RAGAS Faithfulness / AnswerRelevancy（需要 context，可选）
    6) 输出质量报告 Markdown

使用：
    python scripts/data_quality.py \\
        --input data/processed/knowledge_qa.json \\
        --output data/processed/knowledge_qa_filtered.json \\
        --report eval/data_quality_report.md \\
        --judge_threshold 3.5 \\
        --sim_threshold 0.9 \\
        --enable_ragas 0 \\
        --enable_judge 1
"""
from __future__ import annotations

import argparse
import json
import os
import re
import sys
from pathlib import Path
from typing import Any

from dotenv import load_dotenv

load_dotenv()


# =========================================================================
# I/O 辅助
# =========================================================================
def load_items(path: Path) -> list[dict]:
    text = path.read_text(encoding="utf-8")
    if path.suffix == ".jsonl":
        return [json.loads(l) for l in text.splitlines() if l.strip()]
    data = json.loads(text)
    if isinstance(data, list):
        return data
    raise ValueError(f"期望 JSON 数组，但在 {path} 读到 {type(data).__name__}")


def dump_items(items: list[dict], path: Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(items, ensure_ascii=False, indent=2), encoding="utf-8")


# =========================================================================
# 1) 规则过滤
# =========================================================================
_PII_PATTERNS = [
    (re.compile(r"1[3-9]\d{9}"), "<PHONE>"),                         # 手机号
    (re.compile(r"\b\d{15,18}[0-9Xx]?\b"), "<ID>"),                  # 身份证
    (re.compile(r"\b[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Z|a-z]{2,}\b"), "<EMAIL>"),
    (re.compile(r"\b(?:\d{1,3}\.){3}\d{1,3}\b"), "<IP>"),
]


def redact_pii(text: str) -> str:
    for pat, repl in _PII_PATTERNS:
        text = pat.sub(repl, text)
    return text


def rule_filter(
    items: list[dict],
    min_q_len: int = 5,
    min_a_len: int = 10,
    max_q_len: int = 512,
    max_a_len: int = 4096,
    enable_pii: bool = True,
) -> tuple[list[dict], dict]:
    """基础规则过滤。返回 (保留, 统计)。"""
    stats = {"total": len(items), "missing_field": 0, "too_short": 0, "too_long": 0, "pii_redacted": 0}
    kept: list[dict] = []
    for it in items:
        q = (it.get("question") or "").strip()
        a = (it.get("answer") or "").strip()
        if not q or not a:
            stats["missing_field"] += 1
            continue
        if len(q) < min_q_len or len(a) < min_a_len:
            stats["too_short"] += 1
            continue
        if len(q) > max_q_len or len(a) > max_a_len:
            stats["too_long"] += 1
            continue
        if enable_pii:
            q_new, a_new = redact_pii(q), redact_pii(a)
            if q_new != q or a_new != a:
                stats["pii_redacted"] += 1
            it["question"], it["answer"] = q_new, a_new
        else:
            it["question"], it["answer"] = q, a
        kept.append(it)
    return kept, stats


# =========================================================================
# 2) SimHash 去重（answer 级，防止长文档 chunk 重叠）
# =========================================================================
def simhash_dedupe(items: list[dict], distance: int = 3) -> tuple[list[dict], int]:
    try:
        from simhash import Simhash
    except ImportError:
        print("[warn] 未安装 simhash，跳过此步（pip install simhash）", file=sys.stderr)
        return items, 0

    seen: list[Any] = []  # type: ignore
    kept: list[dict] = []
    drop = 0
    for it in items:
        sig = Simhash(it.get("answer", ""))
        if any(sig.distance(s) <= distance for s in seen):
            drop += 1
            continue
        seen.append(sig)
        kept.append(it)
    return kept, drop


# =========================================================================
# 3) Embedding 语义去重（question 级）
# =========================================================================
def embedding_dedupe(
    items: list[dict], sim_threshold: float = 0.9, model_name: str | None = None
) -> tuple[list[dict], int]:
    try:
        import numpy as np
        from sentence_transformers import SentenceTransformer
    except ImportError:
        print("[warn] 未安装 sentence-transformers 或 numpy，跳过语义去重", file=sys.stderr)
        return items, 0

    model_name = model_name or os.getenv("EMBED_MODEL", "BAAI/bge-m3")
    try:
        encoder = SentenceTransformer(model_name)
    except Exception as e:  # noqa: BLE001
        print(f"[warn] 加载 {model_name} 失败，跳过语义去重：{e}", file=sys.stderr)
        return items, 0

    questions = [it.get("question", "") for it in items]
    if not questions:
        return items, 0

    emb = encoder.encode(questions, normalize_embeddings=True, show_progress_bar=False)
    emb = np.asarray(emb)

    kept_idx: list[int] = []
    drop = 0
    for i in range(len(items)):
        if not kept_idx:
            kept_idx.append(i)
            continue
        kept_mat = emb[kept_idx]
        sims = kept_mat @ emb[i]
        if float(sims.max()) >= sim_threshold:
            drop += 1
            continue
        kept_idx.append(i)
    return [items[i] for i in kept_idx], drop


# =========================================================================
# 4) LLM-as-Judge 打分（Kimi-K2 / GPT-4o 异源评审）
# =========================================================================
JUDGE_PROMPT = """你是一位资深数据标注质检员。请对以下问答对从 5 个维度打分（每项 1-5 整数），最后给综合分。

评分维度：
1. 准确性（答案是否基于事实、无幻觉）
2. 完整性（是否覆盖问题所有关键信息）
3. 清晰度（表达是否清晰，结构是否合理）
4. 专业性（术语是否规范，领域深度是否足够）
5. 可训练价值（该样本是否对模型学习有用）

只输出 JSON：{"accuracy":3,"completeness":3,"clarity":3,"professionalism":3,"value":3,"overall":3.0,"reason":"..."}

【问题】{question}
【答案】{answer}
"""


def _judge_score(client, model: str, q: str, a: str) -> tuple[float, str]:
    try:
        resp = client.chat.completions.create(
            model=model,
            messages=[{"role": "user", "content": JUDGE_PROMPT.format(question=q, answer=a)}],
            temperature=0.1,
            response_format={"type": "json_object"},
        )
        text = resp.choices[0].message.content or "{}"
        data = json.loads(text)
        if "overall" in data:
            return float(data["overall"]), str(data.get("reason", ""))
        # 兜底平均
        vals = [data.get(k) for k in ("accuracy", "completeness", "clarity", "professionalism", "value")]
        vals = [float(v) for v in vals if isinstance(v, (int, float))]
        return (sum(vals) / len(vals) if vals else 0.0), str(data.get("reason", ""))
    except Exception as e:  # noqa: BLE001
        return 0.0, f"judge_error: {e}"


def llm_judge_filter(
    items: list[dict], provider: str, threshold: float = 3.5
) -> tuple[list[dict], dict]:
    from generate_qa import build_client  # 复用客户端构造

    client, model = build_client(provider)
    print(f"[judge] provider={provider} model={model} threshold={threshold}")

    kept: list[dict] = []
    dropped: list[dict] = []
    for i, it in enumerate(items, 1):
        score, reason = _judge_score(client, model, it.get("question", ""), it.get("answer", ""))
        it.setdefault("_judge", {})["score"] = score
        it["_judge"]["reason"] = reason
        if score >= threshold:
            kept.append(it)
        else:
            dropped.append(it)
        if i % 20 == 0:
            print(f"  judged {i}/{len(items)}  kept={len(kept)}  dropped={len(dropped)}")
    return kept, {"kept": len(kept), "dropped": len(dropped)}


# =========================================================================
# 5) RAGAS Faithfulness / AnswerRelevancy（可选，需要 context 字段）
# =========================================================================
def ragas_filter(
    items: list[dict], threshold: float = 0.7
) -> tuple[list[dict], dict]:
    try:
        from datasets import Dataset
        from ragas import evaluate
        from ragas.metrics import answer_relevancy, faithfulness
    except ImportError:
        print("[warn] 未安装 ragas / datasets，跳过 RAGAS 过滤", file=sys.stderr)
        return items, {"skipped": True}

    with_ctx = [it for it in items if it.get("context") or it.get("contexts")]
    if not with_ctx:
        print("[warn] 所有样本都缺少 context 字段，跳过 RAGAS", file=sys.stderr)
        return items, {"skipped": True}

    ds = Dataset.from_dict(
        {
            "question": [it["question"] for it in with_ctx],
            "answer": [it["answer"] for it in with_ctx],
            "contexts": [
                it.get("contexts") or ([it["context"]] if isinstance(it.get("context"), str) else it.get("context", []))
                for it in with_ctx
            ],
            "ground_truth": [it.get("reference", it.get("answer", "")) for it in with_ctx],
        }
    )
    result = evaluate(ds, metrics=[faithfulness, answer_relevancy])
    faith = result.get("faithfulness", [])
    rel = result.get("answer_relevancy", [])

    kept: list[dict] = []
    dropped = 0
    for it, f, r in zip(with_ctx, faith, rel):
        it.setdefault("_ragas", {})
        it["_ragas"]["faithfulness"] = float(f) if f is not None else 0.0
        it["_ragas"]["answer_relevancy"] = float(r) if r is not None else 0.0
        if it["_ragas"]["faithfulness"] >= threshold and it["_ragas"]["answer_relevancy"] >= threshold:
            kept.append(it)
        else:
            dropped += 1

    no_ctx = [it for it in items if not (it.get("context") or it.get("contexts"))]
    return kept + no_ctx, {"ragas_kept": len(kept), "ragas_dropped": dropped, "no_ctx": len(no_ctx)}


# =========================================================================
# 报告生成
# =========================================================================
def write_report(report_path: Path, stats: dict) -> None:
    report_path.parent.mkdir(parents=True, exist_ok=True)
    md = ["# 数据质量过滤报告", ""]
    for section, info in stats.items():
        md.append(f"## {section}")
        if isinstance(info, dict):
            for k, v in info.items():
                md.append(f"- **{k}**: {v}")
        else:
            md.append(str(info))
        md.append("")
    report_path.write_text("\n".join(md), encoding="utf-8")
    print(f"[report] {report_path}")


# =========================================================================
# 主流程
# =========================================================================
def main():
    parser = argparse.ArgumentParser(description="数据质量过滤管道")
    parser.add_argument("--input", type=str, required=True)
    parser.add_argument("--output", type=str, required=True)
    parser.add_argument("--report", type=str, default=None)

    parser.add_argument("--min_q_len", type=int, default=5)
    parser.add_argument("--min_a_len", type=int, default=10)
    parser.add_argument("--max_q_len", type=int, default=512)
    parser.add_argument("--max_a_len", type=int, default=4096)
    parser.add_argument("--disable_pii", action="store_true")

    parser.add_argument("--simhash_distance", type=int, default=3,
                        help="SimHash 汉明距离阈值，<= 视为重复")
    parser.add_argument("--sim_threshold", type=float, default=0.9,
                        help="Embedding 余弦相似度阈值")
    parser.add_argument("--embed_model", type=str, default=None)

    parser.add_argument("--enable_judge", type=int, default=1)
    parser.add_argument("--judge_provider", type=str, default="moonshot",
                        choices=["deepseek", "moonshot", "openai"])
    parser.add_argument("--judge_threshold", type=float, default=3.5)

    parser.add_argument("--enable_ragas", type=int, default=0)
    parser.add_argument("--ragas_threshold", type=float, default=0.7)

    args = parser.parse_args()

    input_path = Path(args.input)
    output_path = Path(args.output)
    report_path = Path(args.report) if args.report else None

    items = load_items(input_path)
    print(f"[load] {len(items)} items from {input_path}")

    stats: dict[str, Any] = {}

    # 1) 规则
    items, s = rule_filter(
        items,
        min_q_len=args.min_q_len,
        min_a_len=args.min_a_len,
        max_q_len=args.max_q_len,
        max_a_len=args.max_a_len,
        enable_pii=not args.disable_pii,
    )
    stats["rule_filter"] = {**s, "after": len(items)}
    print(f"[1/5] rule_filter  → {len(items)}  ({s})")

    # 2) SimHash
    items, d = simhash_dedupe(items, distance=args.simhash_distance)
    stats["simhash_dedupe"] = {"dropped": d, "after": len(items)}
    print(f"[2/5] simhash_dedupe  drop={d}  → {len(items)}")

    # 3) Embedding 去重
    items, d = embedding_dedupe(items, sim_threshold=args.sim_threshold, model_name=args.embed_model)
    stats["embedding_dedupe"] = {"dropped": d, "after": len(items)}
    print(f"[3/5] embedding_dedupe  drop={d}  → {len(items)}")

    # 4) LLM Judge
    if args.enable_judge:
        items, s = llm_judge_filter(items, args.judge_provider, args.judge_threshold)
        stats["llm_judge"] = {**s, "after": len(items), "threshold": args.judge_threshold}
        print(f"[4/5] llm_judge  → {len(items)}")
    else:
        stats["llm_judge"] = {"skipped": True}
        print("[4/5] llm_judge  skipped")

    # 5) RAGAS
    if args.enable_ragas:
        items, s = ragas_filter(items, threshold=args.ragas_threshold)
        stats["ragas_filter"] = {**s, "after": len(items)}
        print(f"[5/5] ragas_filter  → {len(items)}")
    else:
        stats["ragas_filter"] = {"skipped": True}
        print("[5/5] ragas_filter  skipped")

    dump_items(items, output_path)
    print(f"[save] {len(items)} items → {output_path}")

    if report_path:
        write_report(report_path, stats)


if __name__ == "__main__":
    main()
