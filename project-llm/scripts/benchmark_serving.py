"""
benchmark_serving.py —— vLLM V1 服务并发压测

用于产出面试级 benchmark 对比表：baseline / FP8 / GPTQ-Marlin / +EAGLE-3 / +Chunked Prefill

支持两种模式：
  1) 本地 vLLM benchmark_serving（vLLM 官方脚本）— 若已安装
  2) OpenAI-compatible 并发压测（httpx + asyncio）— 更通用，无需 vllm 源码

输出指标：
  total_requests, throughput_req/s, throughput_tok/s, TTFT_p50/p95, TPOT_p50/p95

使用：
    # 先启动服务： bash deploy/vllm_v1_server.sh
    python scripts/benchmark_serving.py \\
        --base_url http://localhost:8000/v1 \\
        --model knowledge-expert \\
        --dataset ./data/test/knowledge_test.json \\
        --concurrency 16 \\
        --num_requests 200 \\
        --report ./eval/perf_report.md \\
        --tag "fp8+eagle3"
"""
from __future__ import annotations

import argparse
import asyncio
import json
import os
import statistics
import sys
import time
from dataclasses import dataclass, field
from pathlib import Path


@dataclass
class ReqResult:
    ok: bool
    ttft: float = 0.0       # time to first token (s)
    latency: float = 0.0    # total latency (s)
    in_tokens: int = 0
    out_tokens: int = 0
    error: str = ""


@dataclass
class BenchStats:
    total: int = 0
    success: int = 0
    failed: int = 0
    wall_time: float = 0.0
    ttfts: list[float] = field(default_factory=list)
    latencies: list[float] = field(default_factory=list)
    tpots: list[float] = field(default_factory=list)
    total_in_tokens: int = 0
    total_out_tokens: int = 0


def _pct(data: list[float], p: float) -> float:
    if not data:
        return 0.0
    s = sorted(data)
    k = max(0, min(len(s) - 1, int(len(s) * p / 100)))
    return s[k]


def load_prompts(dataset: str, limit: int) -> list[str]:
    data = json.loads(Path(dataset).read_text(encoding="utf-8"))
    prompts: list[str] = []
    for s in data:
        q = s.get("question") or s.get("prompt") or s.get("instruction")
        if q:
            prompts.append(q)
    # 循环填满到 limit
    if not prompts:
        return []
    out = []
    while len(out) < limit:
        out.extend(prompts)
    return out[:limit]


async def _one_request(client, base_url: str, model: str, prompt: str,
                        max_tokens: int, stream: bool) -> ReqResult:
    """单次请求（stream=True 才能测 TTFT）"""
    url = f"{base_url.rstrip('/')}/chat/completions"
    payload = {
        "model": model,
        "messages": [{"role": "user", "content": prompt}],
        "max_tokens": max_tokens,
        "temperature": 0.7,
        "stream": stream,
    }
    start = time.perf_counter()
    ttft = 0.0
    out_tokens = 0
    try:
        if stream:
            async with client.stream("POST", url, json=payload,
                                     timeout=120.0) as r:
                r.raise_for_status()
                first = True
                async for line in r.aiter_lines():
                    if not line.startswith("data:"):
                        continue
                    body = line[5:].strip()
                    if body == "[DONE]":
                        break
                    try:
                        obj = json.loads(body)
                    except json.JSONDecodeError:
                        continue
                    delta = obj.get("choices", [{}])[0].get("delta", {})
                    content = delta.get("content") or ""
                    if content and first:
                        ttft = time.perf_counter() - start
                        first = False
                    if content:
                        out_tokens += max(1, len(content) // 2)  # 粗略 tok 估算
            latency = time.perf_counter() - start
            return ReqResult(ok=True, ttft=ttft or latency,
                             latency=latency, out_tokens=out_tokens,
                             in_tokens=len(prompt) // 2)
        else:
            r = await client.post(url, json=payload, timeout=120.0)
            r.raise_for_status()
            obj = r.json()
            latency = time.perf_counter() - start
            usage = obj.get("usage", {})
            return ReqResult(
                ok=True, ttft=latency, latency=latency,
                in_tokens=usage.get("prompt_tokens", len(prompt) // 2),
                out_tokens=usage.get("completion_tokens", max_tokens),
            )
    except Exception as e:  # noqa: BLE001
        return ReqResult(ok=False, latency=time.perf_counter() - start,
                         error=str(e)[:160])


async def _worker(name: int, queue: asyncio.Queue, client, base_url: str,
                  model: str, max_tokens: int, stream: bool,
                  results: list[ReqResult]):
    while True:
        item = await queue.get()
        if item is None:
            queue.task_done()
            return
        prompt = item
        res = await _one_request(client, base_url, model, prompt, max_tokens, stream)
        results.append(res)
        queue.task_done()


async def run_bench(args) -> BenchStats:
    try:
        import httpx
    except ImportError:
        print("[error] 需要安装 httpx：pip install httpx", file=sys.stderr)
        sys.exit(1)

    prompts = load_prompts(args.dataset, args.num_requests)
    if not prompts:
        print("[error] 无可用 prompts", file=sys.stderr)
        sys.exit(1)

    q: asyncio.Queue = asyncio.Queue()
    for p in prompts:
        q.put_nowait(p)
    for _ in range(args.concurrency):
        q.put_nowait(None)

    results: list[ReqResult] = []
    headers = {}
    if os.getenv("OPENAI_API_KEY"):
        headers["Authorization"] = f"Bearer {os.getenv('OPENAI_API_KEY')}"

    limits = httpx.Limits(max_connections=args.concurrency * 2,
                          max_keepalive_connections=args.concurrency)
    async with httpx.AsyncClient(headers=headers, limits=limits) as client:
        t0 = time.perf_counter()
        workers = [
            asyncio.create_task(_worker(i, q, client, args.base_url,
                                         args.model, args.max_tokens,
                                         args.stream, results))
            for i in range(args.concurrency)
        ]
        await q.join()
        for w in workers:
            w.cancel()
        wall = time.perf_counter() - t0

    stats = BenchStats(total=len(results), wall_time=wall)
    for r in results:
        if r.ok:
            stats.success += 1
            stats.ttfts.append(r.ttft)
            stats.latencies.append(r.latency)
            stats.total_in_tokens += r.in_tokens
            stats.total_out_tokens += r.out_tokens
            gen_time = max(r.latency - r.ttft, 1e-6)
            if r.out_tokens > 1:
                stats.tpots.append(gen_time / max(r.out_tokens - 1, 1))
        else:
            stats.failed += 1
    return stats


def append_report(report: Path, tag: str, args, stats: BenchStats):
    first_write = not report.exists()
    report.parent.mkdir(parents=True, exist_ok=True)
    with report.open("a", encoding="utf-8") as f:
        if first_write:
            f.write("# Perf Benchmark Report\n\n")
            f.write("| 配置 | 并发 | 请求数 | 成功 | 吞吐(req/s) | 吞吐(tok/s) | "
                    "TTFT p50/p95 (ms) | TPOT p50/p95 (ms) |\n")
            f.write("|------|-----|-------|-----|------------|------------|"
                    "-----------------|-----------------|\n")
        req_tps = stats.success / stats.wall_time if stats.wall_time > 0 else 0.0
        tok_tps = stats.total_out_tokens / stats.wall_time if stats.wall_time > 0 else 0.0
        ttft_p50 = _pct(stats.ttfts, 50) * 1000
        ttft_p95 = _pct(stats.ttfts, 95) * 1000
        tpot_p50 = _pct(stats.tpots, 50) * 1000
        tpot_p95 = _pct(stats.tpots, 95) * 1000
        f.write(f"| `{tag}` | {args.concurrency} | {stats.total} | {stats.success} | "
                f"{req_tps:.2f} | {tok_tps:.1f} | "
                f"{ttft_p50:.0f} / {ttft_p95:.0f} | "
                f"{tpot_p50:.1f} / {tpot_p95:.1f} |\n")


def main():
    parser = argparse.ArgumentParser(description="vLLM 服务并发 benchmark")
    parser.add_argument("--base_url", type=str, default="http://localhost:8000/v1")
    parser.add_argument("--model", type=str, required=True,
                        help="served-model-name（vllm serve 时指定的名字）")
    parser.add_argument("--dataset", type=str, required=True,
                        help="knowledge_test.json / npc_test.json / 任意含 question 字段的 JSON")
    parser.add_argument("--concurrency", type=int, default=16)
    parser.add_argument("--num_requests", type=int, default=200)
    parser.add_argument("--max_tokens", type=int, default=256)
    parser.add_argument("--stream", type=int, default=1, help="1=流式（测 TTFT），0=非流式")
    parser.add_argument("--report", type=str, default="./eval/perf_report.md")
    parser.add_argument("--tag", type=str, required=True,
                        help="本次压测配置标签，如 fp8+eagle3 / gptq_marlin / bf16")
    args = parser.parse_args()
    args.stream = bool(args.stream)

    stats = asyncio.run(run_bench(args))
    append_report(Path(args.report), args.tag, args, stats)

    print(f"\n===== [{args.tag}] =====")
    print(f"requests:       {stats.success}/{stats.total}  failed={stats.failed}")
    print(f"wall_time:      {stats.wall_time:.2f}s")
    print(f"throughput:     {stats.success/stats.wall_time:.2f} req/s  "
          f"{stats.total_out_tokens/stats.wall_time:.1f} tok/s")
    print(f"TTFT  p50/p95:  {_pct(stats.ttfts,50)*1000:.0f}ms / {_pct(stats.ttfts,95)*1000:.0f}ms")
    print(f"TPOT  p50/p95:  {_pct(stats.tpots,50)*1000:.2f}ms / {_pct(stats.tpots,95)*1000:.2f}ms")
    print(f"report 追加到： {args.report}")


if __name__ == "__main__":
    main()
