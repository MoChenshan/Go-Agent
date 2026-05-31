"""
generate_qa.py —— Wiki / 运维文档 → QA 对合成（方向一：知识库专家）

2026 主流方案：DeepSeek-V3.2-Exp 合成 + Magpie self-instruct 扩增（可选）
输入：data/raw/wiki_docs/**/*.md
输出：data/processed/knowledge_qa.json（List[{question, answer, difficulty, type, source}]）

使用：
    python scripts/generate_qa.py \\
        --input data/raw/wiki_docs/ \\
        --output data/processed/knowledge_qa.json \\
        --provider deepseek \\
        --max_chunks 0 \\
        --enable_magpie 0

环境变量：
    DEEPSEEK_API_KEY / DEEPSEEK_BASE_URL / DEEPSEEK_MODEL （见 .env.example）
"""
from __future__ import annotations

import argparse
import json
import os
import sys
import time
from pathlib import Path
from typing import Iterable

from dotenv import load_dotenv

load_dotenv()


# =========================================================================
# Prompt 模板
# =========================================================================
GENERATE_QA_PROMPT = """你是一个资深运维数据标注专家。请基于以下文档内容，合成高质量的问答对用于 SFT 训练。

要求：
1. 每个问题必须能仅通过文档内容回答，不允许引入文档外知识
2. 问题要多样化：包含 what / why / how / when / troubleshoot 五类，且覆盖浅层事实与深层原理
3. 答案要准确、完整、专业，保留关键代码/命令/指标阈值
4. 生成 {n_qa} 个问答对，其中至少 2 个是"多跳推理"类（需综合文档多处信息）
5. 为每条标注 difficulty: easy / medium / hard
6. 只输出 JSON 数组，不要任何其他文字，格式：
[{{"question": "...", "answer": "...", "difficulty": "easy", "type": "what"}}, ...]

文档内容：
---
{content}
---
"""

# Magpie self-instruct：利用 Qwen3 chat template 截断触发自发问题生成
MAGPIE_PROMPT_TEMPLATE = """<|im_start|>system
你是游戏服务器运维领域的专家，熟悉 LetsGo 项目与 tRPC-Go 框架。
<|im_end|>
<|im_start|>user
"""


# =========================================================================
# OpenAI 兼容客户端
# =========================================================================
def build_client(provider: str):
    """构造 OpenAI 兼容客户端（DeepSeek / Moonshot / OpenAI）。
    返回 (client, model_name)"""
    from openai import OpenAI

    provider = provider.lower()
    if provider == "deepseek":
        api_key = os.getenv("DEEPSEEK_API_KEY")
        if not api_key:
            raise RuntimeError("DEEPSEEK_API_KEY 未配置，请检查 .env")
        return OpenAI(
            api_key=api_key,
            base_url=os.getenv("DEEPSEEK_BASE_URL", "https://api.deepseek.com/v1"),
        ), os.getenv("DEEPSEEK_MODEL", "deepseek-chat")

    if provider == "moonshot":
        api_key = os.getenv("MOONSHOT_API_KEY")
        if not api_key:
            raise RuntimeError("MOONSHOT_API_KEY 未配置")
        return OpenAI(
            api_key=api_key,
            base_url=os.getenv("MOONSHOT_BASE_URL", "https://api.moonshot.cn/v1"),
        ), os.getenv("MOONSHOT_MODEL", "moonshot-v1-128k")

    return OpenAI(
        api_key=os.getenv("OPENAI_API_KEY"),
        base_url=os.getenv("OPENAI_BASE_URL", "https://api.openai.com/v1"),
    ), os.getenv("OPENAI_JUDGE_MODEL", "gpt-4o")


# =========================================================================
# Markdown chunking（按段落 + 最大长度切分）
# =========================================================================
def split_into_chunks(text: str, max_len: int = 4000) -> list[str]:
    """按空行切段落，再按 max_len 累计聚合成 chunk。"""
    paragraphs = [p for p in text.split("\n\n") if p.strip()]
    chunks: list[str] = []
    current = ""
    for p in paragraphs:
        # 单段超长直接独立成 chunk
        if len(p) > max_len:
            if current:
                chunks.append(current)
                current = ""
            chunks.append(p[:max_len])
            continue
        if len(current) + len(p) > max_len:
            if current:
                chunks.append(current)
            current = p
        else:
            current += "\n\n" + p if current else p
    if current:
        chunks.append(current)
    return chunks


def iter_doc_files(input_dir: Path) -> Iterable[Path]:
    """遍历 .md / .txt / .rst 文件"""
    exts = {".md", ".txt", ".rst", ".markdown"}
    for root, _, files in os.walk(input_dir):
        for f in files:
            if Path(f).suffix.lower() in exts:
                yield Path(root) / f


# =========================================================================
# QA 合成核心
# =========================================================================
def _parse_qa_json(text: str) -> list[dict]:
    """鲁棒解析 LLM 返回的 JSON"""
    text = text.strip()
    # 去除 markdown code fence
    if text.startswith("```"):
        text = text.split("\n", 1)[1] if "\n" in text else text
        text = text.rsplit("```", 1)[0]
    text = text.strip()
    # 尝试直接解析
    try:
        data = json.loads(text)
        if isinstance(data, list):
            return data
        if isinstance(data, dict):
            for key in ("qa_pairs", "data", "results", "items"):
                if key in data and isinstance(data[key], list):
                    return data[key]
    except json.JSONDecodeError:
        pass
    # 兜底：截取数组片段
    start, end = text.find("["), text.rfind("]") + 1
    if start >= 0 and end > start:
        try:
            return json.loads(text[start:end])
        except json.JSONDecodeError:
            pass
    return []


def generate_qa_from_chunk(
    client, model: str, chunk: str, n_qa: int = 8, retry: int = 2
) -> list[dict]:
    """从单个 chunk 合成 QA 对"""
    last_err: Exception | None = None
    for attempt in range(retry + 1):
        try:
            resp = client.chat.completions.create(
                model=model,
                messages=[
                    {
                        "role": "user",
                        "content": GENERATE_QA_PROMPT.format(content=chunk, n_qa=n_qa),
                    }
                ],
                temperature=0.7,
                response_format={"type": "json_object"},
            )
            text = resp.choices[0].message.content or ""
            qa_list = _parse_qa_json(text)
            # 基本字段校验
            cleaned = [
                {
                    "question": (x.get("question") or "").strip(),
                    "answer": (x.get("answer") or "").strip(),
                    "difficulty": x.get("difficulty", "medium"),
                    "type": x.get("type", "what"),
                }
                for x in qa_list
                if x.get("question") and x.get("answer")
            ]
            if cleaned:
                return cleaned
            last_err = ValueError("解析出空 QA 列表")
        except Exception as e:  # noqa: BLE001
            last_err = e
            time.sleep(1.5 * (attempt + 1))
    print(f"  [warn] chunk 合成失败：{last_err}", file=sys.stderr)
    return []


# =========================================================================
# Magpie self-instruct（可选）
# =========================================================================
def magpie_self_instruct(
    synth_client, synth_model: str, n_samples: int = 100, base_model_url: str | None = None
) -> list[dict]:
    """Magpie self-instruct：让本地 Qwen3 基座自发问，再用合成主模型答。
    仅在 base_model_url 指向本地 vLLM 服务时启用。"""
    if not base_model_url:
        print("[magpie] 未提供 base_model_url，跳过 Magpie 扩增", file=sys.stderr)
        return []
    from openai import OpenAI

    local = OpenAI(base_url=base_model_url, api_key="dummy")
    out: list[dict] = []
    for i in range(n_samples):
        try:
            # 第一步：模型自发生成问题
            q_resp = local.completions.create(
                model="qwen3-8b",
                prompt=MAGPIE_PROMPT_TEMPLATE,
                max_tokens=128,
                temperature=1.0,
                stop=["<|im_end|>"],
            )
            question = q_resp.choices[0].text.strip()
            if len(question) < 10:
                continue
            # 第二步：合成主模型给高质量答案
            a_resp = synth_client.chat.completions.create(
                model=synth_model,
                messages=[{"role": "user", "content": question}],
                temperature=0.3,
            )
            out.append(
                {
                    "question": question,
                    "answer": a_resp.choices[0].message.content or "",
                    "difficulty": "medium",
                    "type": "what",
                    "source": "magpie_self_instruct",
                }
            )
        except Exception as e:  # noqa: BLE001
            print(f"  [magpie] #{i} 失败：{e}", file=sys.stderr)
    return out


# =========================================================================
# 主流程
# =========================================================================
def main():
    parser = argparse.ArgumentParser(description="Wiki → QA 对合成（方向一）")
    parser.add_argument("--input", type=str, required=True, help="原始 Markdown 目录")
    parser.add_argument("--output", type=str, required=True, help="输出 JSON 文件")
    parser.add_argument(
        "--provider",
        type=str,
        default="deepseek",
        choices=["deepseek", "moonshot", "openai"],
    )
    parser.add_argument("--n_per_chunk", type=int, default=8,
                        help="每个 chunk 合成多少 QA 对")
    parser.add_argument("--max_len", type=int, default=4000, help="chunk 最大字符数")
    parser.add_argument("--max_chunks", type=int, default=0, help="0=不限；>0 用于快速 smoke test")
    parser.add_argument("--enable_magpie", type=int, default=0)
    parser.add_argument("--magpie_n", type=int, default=100)
    parser.add_argument("--magpie_base_url", type=str, default=None,
                        help="本地 vLLM 地址，例如 http://localhost:8000/v1")
    args = parser.parse_args()

    input_dir = Path(args.input)
    out_path = Path(args.output)
    out_path.parent.mkdir(parents=True, exist_ok=True)

    if not input_dir.is_dir():
        print(f"[error] input 目录不存在: {input_dir}", file=sys.stderr)
        sys.exit(1)

    client, model = build_client(args.provider)
    print(f"[generate_qa] provider={args.provider}  model={model}")
    print(f"[generate_qa] input={input_dir}")
    print(f"[generate_qa] output={out_path}")

    all_qa: list[dict] = []
    doc_files = list(iter_doc_files(input_dir))
    if not doc_files:
        print(f"[warn] 未在 {input_dir} 下找到 .md/.txt/.rst 文件", file=sys.stderr)

    chunk_budget = args.max_chunks or 10 ** 9
    produced_chunks = 0

    for idx, fp in enumerate(doc_files, 1):
        try:
            content = fp.read_text(encoding="utf-8")
        except Exception as e:  # noqa: BLE001
            print(f"  [skip] 读取失败 {fp}: {e}", file=sys.stderr)
            continue
        chunks = split_into_chunks(content, max_len=args.max_len)
        print(f"[{idx}/{len(doc_files)}] {fp} ({len(chunks)} chunks, {len(content)} chars)")
        for ci, chunk in enumerate(chunks, 1):
            if produced_chunks >= chunk_budget:
                break
            qa_list = generate_qa_from_chunk(client, model, chunk, n_qa=args.n_per_chunk)
            for qa in qa_list:
                qa["source"] = str(fp.relative_to(input_dir))
            all_qa.extend(qa_list)
            produced_chunks += 1
            print(f"  chunk {ci}/{len(chunks)} → +{len(qa_list)} QA  (累计 {len(all_qa)})")
        if produced_chunks >= chunk_budget:
            print(f"[info] 达到 max_chunks={args.max_chunks}，提前结束")
            break

    if args.enable_magpie:
        print(f"[magpie] 开始自问自答扩增 n={args.magpie_n}")
        all_qa.extend(
            magpie_self_instruct(client, model, args.magpie_n, args.magpie_base_url)
        )

    with out_path.open("w", encoding="utf-8") as f:
        json.dump(all_qa, f, ensure_ascii=False, indent=2)
    print(f"\n[done] 总计 {len(all_qa)} 条 QA → {out_path}")


if __name__ == "__main__":
    main()
