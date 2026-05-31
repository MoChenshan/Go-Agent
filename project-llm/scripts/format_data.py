"""
format_data.py —— 数据格式转换（Alpaca / ShareGPT / OpenAI-messages 互转）

支持：
    - Alpaca:  {instruction, input, output}
    - ShareGPT: {conversations: [{from, value}], system}
    - Messages: [{role, content}]

使用：
    python scripts/format_data.py \\
        --input data/processed/knowledge_qa.json \\
        --input_format alpaca \\
        --output data/processed/train_messages.jsonl \\
        --output_format messages
"""
from __future__ import annotations

import argparse
import json
from pathlib import Path
from typing import Iterable


# ---------- Alpaca <-> Messages ----------
def alpaca_to_messages(item: dict) -> list[dict]:
    user = item.get("instruction", "")
    if item.get("input"):
        user += "\n" + item["input"]
    return [
        {"role": "user", "content": user},
        {"role": "assistant", "content": item.get("output", "")},
    ]


def messages_to_alpaca(messages: list[dict]) -> dict:
    instruction = next((m["content"] for m in messages if m["role"] == "user"), "")
    output = next((m["content"] for m in messages if m["role"] == "assistant"), "")
    return {"instruction": instruction, "input": "", "output": output}


# ---------- ShareGPT <-> Messages ----------
_ROLE_MAP = {"human": "user", "gpt": "assistant", "system": "system"}
_ROLE_MAP_REV = {v: k for k, v in _ROLE_MAP.items()}


def sharegpt_to_messages(item: dict) -> list[dict]:
    msgs: list[dict] = []
    if item.get("system"):
        msgs.append({"role": "system", "content": item["system"]})
    for turn in item.get("conversations", []):
        msgs.append({"role": _ROLE_MAP.get(turn["from"], turn["from"]),
                     "content": turn["value"]})
    return msgs


def messages_to_sharegpt(messages: list[dict]) -> dict:
    system = ""
    conv: list[dict] = []
    for m in messages:
        if m["role"] == "system":
            system = m["content"]
        else:
            conv.append({"from": _ROLE_MAP_REV.get(m["role"], m["role"]),
                         "value": m["content"]})
    out = {"conversations": conv}
    if system:
        out["system"] = system
    return out


# ---------- IO ----------
def load_any(path: Path) -> Iterable[dict]:
    if path.suffix == ".jsonl":
        with path.open("r", encoding="utf-8") as f:
            for line in f:
                if line.strip():
                    yield json.loads(line)
    else:
        data = json.loads(path.read_text(encoding="utf-8"))
        if isinstance(data, list):
            yield from data
        else:
            yield data


def dump_any(items: list, path: Path) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    if path.suffix == ".jsonl":
        with path.open("w", encoding="utf-8") as f:
            for it in items:
                f.write(json.dumps(it, ensure_ascii=False) + "\n")
    else:
        path.write_text(json.dumps(items, ensure_ascii=False, indent=2), encoding="utf-8")


# ---------- 核心 ----------
def convert(item: dict, src: str, dst: str):
    # 先转成 messages 再转出
    if src == "alpaca":
        messages = alpaca_to_messages(item)
    elif src == "sharegpt":
        messages = sharegpt_to_messages(item)
    elif src == "messages":
        messages = item.get("messages", item) if isinstance(item, dict) else item
    else:
        raise ValueError(f"未知 input_format: {src}")

    if dst == "alpaca":
        return messages_to_alpaca(messages)
    if dst == "sharegpt":
        return messages_to_sharegpt(messages)
    if dst == "messages":
        return {"messages": messages}
    raise ValueError(f"未知 output_format: {dst}")


def main():
    parser = argparse.ArgumentParser(description="数据格式转换")
    parser.add_argument("--input", type=str, required=True)
    parser.add_argument("--input_format", type=str, required=True,
                        choices=["alpaca", "sharegpt", "messages"])
    parser.add_argument("--output", type=str, required=True)
    parser.add_argument("--output_format", type=str, required=True,
                        choices=["alpaca", "sharegpt", "messages"])
    args = parser.parse_args()

    items = [convert(x, args.input_format, args.output_format)
             for x in load_any(Path(args.input))]
    dump_any(items, Path(args.output))
    print(f"[format_data] {len(items)} items: {args.input_format} → {args.output_format}")
    print(f"[format_data] saved to {args.output}")


if __name__ == "__main__":
    main()
