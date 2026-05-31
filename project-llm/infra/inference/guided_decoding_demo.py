"""guided decoding demo（vLLM 兼容 / xgrammar 直接调用）。

guided / structured decoding 解决"模型输出格式不可信"的问题：
- json_schema：严格按 schema 输出，字段、类型、枚举一律强约束
- regex：按正则约束（如手机号、IP、版本号）
- choice：在受限选项里选一个

vLLM v1（>=0.7）已内置 xgrammar，作为推理 server 推荐通过 OpenAI API 的
extra_body.guided_* 字段开启；本 demo 同时演示两条路径：

    路径 A：通过 vLLM HTTP API（生产推荐）
    路径 B：直接用 xgrammar 库做约束（调试 / 单测推荐）

用法：
    # 路径 A
    python infra/inference/guided_decoding_demo.py http \
        --base-url http://localhost:8000/v1 \
        --model qwen3-8b-npc

    # 路径 B（仅校验 schema 是否被严格遵守）
    python infra/inference/guided_decoding_demo.py local
"""

import argparse
import json
import os
import sys
from typing import Any, Dict


# 一个会被严格约束的目标 JSON schema
SCHEMA: Dict[str, Any] = {
    "type": "object",
    "properties": {
        "intent":   {"type": "string", "enum": ["query", "scale", "rollback", "restart", "unknown"]},
        "cluster":  {"type": "string", "pattern": "^BCS-(K8S|MESOS)-\\d{3}$"},
        "service":  {"type": "string"},
        "replicas": {"type": "integer", "minimum": 0, "maximum": 1000},
        "reason":   {"type": "string", "maxLength": 200},
    },
    "required": ["intent", "cluster", "service"],
    "additionalProperties": False,
}


def run_http(args) -> int:
    try:
        from openai import OpenAI  # type: ignore
    except Exception as e:  # noqa: BLE001
        print(f"ERROR: openai SDK 未安装: {e}", file=sys.stderr)
        return 2

    client = OpenAI(base_url=args.base_url, api_key=os.getenv("OPENAI_API_KEY", "EMPTY"))
    user = (
        "把以下运维指令解析为结构化 JSON："
        "把集群 BCS-K8S-001 的 game-svr 扩到 8 个副本，原因是流量上涨。"
    )

    print("=== Calling vLLM with guided_json ===")
    resp = client.chat.completions.create(
        model=args.model,
        messages=[
            {"role": "system", "content": "你是运维指令解析器，必须按提供的 JSON schema 严格输出。"},
            {"role": "user", "content": user},
        ],
        extra_body={"guided_json": SCHEMA},
        temperature=0.0,
    )
    out = resp.choices[0].message.content or ""
    print("raw:", out)
    parsed = json.loads(out)
    print("parsed:", json.dumps(parsed, ensure_ascii=False, indent=2))
    # schema 校验（防御）
    try:
        import jsonschema  # type: ignore
        jsonschema.validate(parsed, SCHEMA)
        print("schema OK ✓")
    except Exception as e:  # noqa: BLE001
        print(f"WARN: schema validation failed: {e}", file=sys.stderr)
        return 1
    return 0


def run_local(_args) -> int:
    """本地 xgrammar 演示：用一段 mock 的 logits 流，验证 mask 逻辑生效。"""
    try:
        import xgrammar as xgr  # type: ignore
    except Exception as e:  # noqa: BLE001
        print(f"ERROR: xgrammar 未安装: {e}", file=sys.stderr)
        print("pip install xgrammar", file=sys.stderr)
        return 2
    try:
        from transformers import AutoTokenizer  # type: ignore
    except Exception as e:  # noqa: BLE001
        print(f"ERROR: transformers 未安装: {e}", file=sys.stderr)
        return 2

    # 用一个常见 tokenizer 演示
    tok = AutoTokenizer.from_pretrained("Qwen/Qwen2.5-7B", trust_remote_code=True)
    grammar_compiler = xgr.GrammarCompiler(xgr.TokenizerInfo.from_huggingface(tok))
    grammar = grammar_compiler.compile_json_schema(json.dumps(SCHEMA))
    matcher = xgr.GrammarMatcher(grammar)

    # 模拟逐 token 生成：实际推理时从模型 logits 里挑 token；这里只演示 accept/reject
    tokens_to_test = '{"intent":"scale","cluster":"BCS-K8S-001","service":"game-svr","replicas":8,"reason":"traffic up"}'
    ids = tok.encode(tokens_to_test, add_special_tokens=False)
    accepted = 0
    for tid in ids:
        if matcher.accept_token(tid):
            accepted += 1
        else:
            print(f"REJECT at token {tid} ({tok.decode([tid])!r})")
            break
    print(f"accepted {accepted}/{len(ids)} tokens, terminated={matcher.is_terminated()}")
    return 0 if accepted == len(ids) else 1


def main() -> int:
    ap = argparse.ArgumentParser()
    sub = ap.add_subparsers(dest="mode", required=True)
    p_http = sub.add_parser("http")
    p_http.add_argument("--base-url", default=os.getenv("OPENAI_BASE_URL", "http://localhost:8000/v1"))
    p_http.add_argument("--model", default=os.getenv("MODEL_NAME", "qwen3-8b-npc"))
    sub.add_parser("local")
    args = ap.parse_args()
    return run_http(args) if args.mode == "http" else run_local(args)


if __name__ == "__main__":
    sys.exit(main())
