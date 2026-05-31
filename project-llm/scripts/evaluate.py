"""
evaluate.py —— 模型评估（G-Eval + RAGAS + Langfuse）

支持四个对比目标（按方案 2.4.1）：
    - base          基座 Qwen3-8B（thinking off）
    - base-think    基座 Qwen3-8B（thinking on）
    - sft           微调后模型
    - rag           基座 + Agentic RAG（占位，需外部 RAG 服务）
    - sft-rag       微调 + Agentic RAG

推理后端：
    - hf     : transformers 原生（小规模）
    - vllm   : OpenAI 兼容 API（http://localhost:8000/v1）推荐
    - openai : 直接走外部服务

使用：
    python scripts/evaluate.py \\
        --model_path ./output/knowledge_sft_merged \\
        --test_set data/test/knowledge_test.json \\
        --report eval/knowledge_eval_report.md \\
        --mode knowledge \\
        --engine hf \\
        --max_samples 50
"""
from __future__ import annotations

import argparse
import json
import os
import statistics
import sys
import time
from pathlib import Path
from typing import Any

from dotenv import load_dotenv

load_dotenv()


# =========================================================================
# 测试集
# =========================================================================
def load_test_set(path: Path, max_samples: int = 0) -> list[dict]:
    data = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(data, list):
        raise ValueError("测试集必须是 JSON 数组，每条含 question/reference[/context]")
    if max_samples and len(data) > max_samples:
        data = data[:max_samples]
    return data


# =========================================================================
# 推理后端
# =========================================================================
class Inferencer:
    """统一推理接口"""

    def __init__(
        self,
        engine: str,
        model_path: str,
        model_name: str | None = None,
        base_url: str | None = None,
        enable_thinking: bool = False,
    ):
        self.engine = engine
        self.model_path = model_path
        self.model_name = model_name or model_path
        self.base_url = base_url
        self.enable_thinking = enable_thinking
        self._hf_pipe = None
        self._openai = None

    # ---- HuggingFace ----
    def _lazy_hf(self):
        if self._hf_pipe is not None:
            return
        from transformers import AutoModelForCausalLM, AutoTokenizer, pipeline

        print(f"[hf] loading {self.model_path} ...")
        tok = AutoTokenizer.from_pretrained(self.model_path, trust_remote_code=True)
        model = AutoModelForCausalLM.from_pretrained(
            self.model_path, trust_remote_code=True, device_map="auto"
        )
        self._hf_pipe = pipeline("text-generation", model=model, tokenizer=tok)

    # ---- OpenAI 兼容（vLLM / 官方）----
    def _lazy_openai(self):
        if self._openai is not None:
            return
        from openai import OpenAI

        if self.engine == "openai":
            api_key = os.getenv("OPENAI_API_KEY", "sk-dummy")
            base_url = self.base_url or os.getenv("OPENAI_BASE_URL", "https://api.openai.com/v1")
        else:  # vllm / sglang
            api_key = "dummy"
            base_url = self.base_url or "http://localhost:8000/v1"
        self._openai = OpenAI(api_key=api_key, base_url=base_url)

    # ---- 推理 ----
    def __call__(self, question: str, max_tokens: int = 512, temperature: float = 0.0) -> tuple[str, float]:
        start = time.time()
        if self.engine == "hf":
            self._lazy_hf()
            messages = [{"role": "user", "content": question}]
            # 使用 chat template（Qwen3 原生支持 enable_thinking）
            tok = self._hf_pipe.tokenizer
            text = tok.apply_chat_template(
                messages, tokenize=False, add_generation_prompt=True,
                enable_thinking=self.enable_thinking,
            ) if hasattr(tok, "apply_chat_template") else question
            out = self._hf_pipe(
                text,
                max_new_tokens=max_tokens,
                do_sample=temperature > 0,
                temperature=max(temperature, 0.01),
                return_full_text=False,
            )
            answer = out[0]["generated_text"]
        else:
            self._lazy_openai()
            resp = self._openai.chat.completions.create(
                model=self.model_name,
                messages=[{"role": "user", "content": question}],
                max_tokens=max_tokens,
                temperature=temperature,
            )
            answer = resp.choices[0].message.content or ""
        return answer.strip(), time.time() - start


# =========================================================================
# G-Eval（DeepEval）
# =========================================================================
def build_geval_metrics(judge_model: str):
    """构造准确性/完整性两个 G-Eval 指标"""
    try:
        from deepeval.metrics import GEval
        from deepeval.test_case import LLMTestCase, LLMTestCaseParams  # noqa: F401
    except ImportError:
        print("[warn] 未安装 deepeval，跳过 G-Eval", file=sys.stderr)
        return None, None
    from deepeval.test_case import LLMTestCaseParams

    accuracy = GEval(
        name="Accuracy",
        criteria="模型回答是否与参考答案在事实层面一致，不允许出现幻觉或错误",
        evaluation_params=[
            LLMTestCaseParams.INPUT,
            LLMTestCaseParams.ACTUAL_OUTPUT,
            LLMTestCaseParams.EXPECTED_OUTPUT,
        ],
        model=judge_model,
    )
    completeness = GEval(
        name="Completeness",
        criteria="模型回答是否覆盖参考答案的所有关键点",
        evaluation_params=[
            LLMTestCaseParams.INPUT,
            LLMTestCaseParams.ACTUAL_OUTPUT,
            LLMTestCaseParams.EXPECTED_OUTPUT,
        ],
        model=judge_model,
    )
    return accuracy, completeness


def measure_geval(metric, input_q, actual, expected) -> float:
    from deepeval.test_case import LLMTestCase
    tc = LLMTestCase(input=input_q, actual_output=actual, expected_output=expected)
    metric.measure(tc)
    return float(metric.score or 0.0)


# =========================================================================
# RAGAS
# =========================================================================
def run_ragas(items: list[dict]) -> dict[str, float]:
    try:
        from datasets import Dataset
        from ragas import evaluate
        from ragas.metrics import answer_relevancy, context_precision, faithfulness
    except ImportError:
        print("[warn] 未安装 ragas，跳过 RAGAS", file=sys.stderr)
        return {}

    with_ctx = [it for it in items if it.get("context") or it.get("contexts")]
    if not with_ctx:
        return {}

    ds = Dataset.from_dict(
        {
            "question": [it["question"] for it in with_ctx],
            "answer": [it["actual"] for it in with_ctx],
            "contexts": [
                it.get("contexts")
                or ([it["context"]] if isinstance(it.get("context"), str) else it.get("context", []))
                for it in with_ctx
            ],
            "ground_truth": [it.get("reference", "") for it in with_ctx],
        }
    )
    try:
        result = evaluate(ds, metrics=[faithfulness, answer_relevancy, context_precision])
    except Exception as e:  # noqa: BLE001
        print(f"[warn] ragas 执行失败：{e}", file=sys.stderr)
        return {}

    out: dict[str, float] = {}
    for key in ("faithfulness", "answer_relevancy", "context_precision"):
        val = result.get(key)
        if val is None:
            continue
        if hasattr(val, "__iter__"):
            vals = [float(x) for x in val if x is not None]
            out[key] = sum(vals) / len(vals) if vals else 0.0
        else:
            out[key] = float(val)
    return out


# =========================================================================
# Langfuse 包裹（可选）
# =========================================================================
def get_langfuse_client():
    if not (os.getenv("LANGFUSE_PUBLIC_KEY") and os.getenv("LANGFUSE_SECRET_KEY")):
        return None
    try:
        from langfuse import Langfuse

        return Langfuse()
    except ImportError:
        return None


# =========================================================================
# 主流程
# =========================================================================
def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--model_path", type=str, required=True)
    parser.add_argument("--test_set", type=str, required=True)
    parser.add_argument("--report", type=str, required=True)
    parser.add_argument("--mode", type=str, required=True, choices=["knowledge", "npc"])
    parser.add_argument("--engine", type=str, default="hf", choices=["hf", "vllm", "sglang", "openai"])
    parser.add_argument("--model_name", type=str, default=None,
                        help="vLLM/OpenAI 接口下使用的 model 字段")
    parser.add_argument("--base_url", type=str, default=None)
    parser.add_argument("--enable_thinking", action="store_true")
    parser.add_argument("--judge_model", type=str, default="gpt-4o")
    parser.add_argument("--max_samples", type=int, default=0)
    parser.add_argument("--max_new_tokens", type=int, default=512)
    args = parser.parse_args()

    test_set = load_test_set(Path(args.test_set), max_samples=args.max_samples)
    print(f"[eval] {len(test_set)} test cases from {args.test_set}")

    infer = Inferencer(
        engine=args.engine,
        model_path=args.model_path,
        model_name=args.model_name,
        base_url=args.base_url,
        enable_thinking=args.enable_thinking,
    )

    lf = get_langfuse_client()
    if lf:
        print("[eval] Langfuse trace 已启用")

    accuracy_metric, completeness_metric = build_geval_metrics(args.judge_model)

    results: list[dict] = []
    latencies: list[float] = []
    for i, tc in enumerate(test_set, 1):
        q = tc["question"]
        ref = tc.get("reference") or tc.get("answer", "")
        answer, latency = infer(q, max_tokens=args.max_new_tokens)
        latencies.append(latency)
        row: dict[str, Any] = {
            "question": q,
            "reference": ref,
            "actual": answer,
            "latency_s": round(latency, 3),
            "context": tc.get("context"),
            "contexts": tc.get("contexts"),
        }
        # G-Eval
        if accuracy_metric is not None:
            try:
                row["geval_accuracy"] = measure_geval(accuracy_metric, q, answer, ref)
                row["geval_completeness"] = measure_geval(completeness_metric, q, answer, ref)
            except Exception as e:  # noqa: BLE001
                print(f"  [warn] G-Eval 失败 #{i}: {e}", file=sys.stderr)
        # Langfuse
        if lf:
            try:
                with lf.start_as_current_span(name=f"eval-{Path(args.model_path).name}") as span:
                    span.update(
                        input=q,
                        output=answer,
                        metadata={"reference": ref, "latency_s": row["latency_s"]},
                    )
            except Exception as e:  # noqa: BLE001
                print(f"  [warn] langfuse 上报失败 #{i}: {e}", file=sys.stderr)

        results.append(row)
        if i % 10 == 0 or i == len(test_set):
            print(f"  [{i}/{len(test_set)}] latency_avg={statistics.mean(latencies):.2f}s")

    # RAGAS 批量
    ragas_scores: dict[str, float] = {}
    if args.mode == "knowledge":
        ragas_scores = run_ragas(results)

    # 汇总
    def _avg(key: str) -> float | None:
        vals = [r[key] for r in results if isinstance(r.get(key), (int, float))]
        return statistics.mean(vals) if vals else None

    summary = {
        "model_path": args.model_path,
        "engine": args.engine,
        "mode": args.mode,
        "n_cases": len(results),
        "geval_accuracy_avg": _avg("geval_accuracy"),
        "geval_completeness_avg": _avg("geval_completeness"),
        "latency_p50": statistics.median(latencies) if latencies else None,
        "latency_mean": statistics.mean(latencies) if latencies else None,
        "ragas": ragas_scores,
    }

    # 报告
    report_path = Path(args.report)
    report_path.parent.mkdir(parents=True, exist_ok=True)
    md = [
        f"# 评估报告 —— {Path(args.model_path).name}",
        "",
        f"- 测试集：`{args.test_set}`（{len(results)} 条）",
        f"- 推理后端：`{args.engine}`",
        f"- 评判模型：`{args.judge_model}`",
        "",
        "## 总体指标",
        "",
        "| 指标 | 值 |",
        "| --- | --- |",
    ]
    for k, v in summary.items():
        if k in ("model_path", "engine", "mode", "n_cases"):
            md.append(f"| {k} | {v} |")
    md += [
        f"| G-Eval Accuracy (avg) | {summary['geval_accuracy_avg']} |",
        f"| G-Eval Completeness (avg) | {summary['geval_completeness_avg']} |",
        f"| Latency P50 (s) | {summary['latency_p50']} |",
        f"| Latency Mean (s) | {summary['latency_mean']} |",
    ]
    if ragas_scores:
        md.append("")
        md.append("## RAGAS")
        md.append("")
        md.append("| 指标 | 值 |")
        md.append("| --- | --- |")
        for k, v in ragas_scores.items():
            md.append(f"| {k} | {v:.4f} |")

    md += ["", "## 明细（前 10 条）", ""]
    for r in results[:10]:
        md.append(f"### Q: {r['question']}")
        md.append(f"- **reference**: {r['reference'][:200]}")
        md.append(f"- **actual**:    {r['actual'][:400]}")
        acc = r.get("geval_accuracy")
        cmp = r.get("geval_completeness")
        md.append(f"- G-Eval: accuracy={acc}  completeness={cmp}  latency={r['latency_s']}s")
        md.append("")

    report_path.write_text("\n".join(md), encoding="utf-8")
    print(f"[done] report → {report_path}")

    # 同时输出 JSON 明细
    details_path = report_path.with_suffix(".details.json")
    details_path.write_text(
        json.dumps({"summary": summary, "cases": results}, ensure_ascii=False, indent=2),
        encoding="utf-8",
    )
    print(f"[done] details → {details_path}")


if __name__ == "__main__":
    main()
