"""
build_index.py —— 构建 Qdrant 向量索引

读取原始文档（markdown / txt / jsonl），按配置 chunk + embed + upload 到 Qdrant。

使用：
    python scripts/build_index.py \\
        --config configs/knowledge_rag.yaml \\
        --source_dir data/raw/kb \\
        --recreate

支持文件格式：
    .md / .txt / .jsonl (每行 {"title": "...", "content": "...", "source": "..."})
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import sys
import uuid
from pathlib import Path
from typing import Any

import yaml


def load_docs(source_dir: str) -> list[dict[str, str]]:
    """读取原始文档，统一为 {title, content, source} 结构"""
    docs: list[dict[str, str]] = []
    src = Path(source_dir)
    if not src.exists():
        print(f"[error] source_dir 不存在：{src}", file=sys.stderr)
        sys.exit(1)

    for p in src.rglob("*"):
        if p.is_dir():
            continue
        rel = str(p.relative_to(src))
        if p.suffix.lower() in (".md", ".txt"):
            text = p.read_text(encoding="utf-8", errors="ignore")
            docs.append({"title": p.stem, "content": text, "source": rel})
        elif p.suffix.lower() == ".jsonl":
            for line in p.read_text(encoding="utf-8").splitlines():
                if not line.strip():
                    continue
                obj = json.loads(line)
                docs.append({
                    "title": obj.get("title", p.stem),
                    "content": obj.get("content", ""),
                    "source": obj.get("source", rel),
                })
    print(f"[build] 读取原始文档 {len(docs)} 条")
    return docs


def chunk_text(text: str, size: int, overlap: int) -> list[str]:
    """滑动窗口切片（简单按字符切，中文场景比按词更稳）"""
    if len(text) <= size:
        return [text]
    out, i = [], 0
    while i < len(text):
        out.append(text[i:i + size])
        if i + size >= len(text):
            break
        i += size - overlap
    return out


def stable_id(source: str, idx: int) -> str:
    """基于 source+idx 生成稳定 UUID，便于增量更新"""
    h = hashlib.md5(f"{source}::{idx}".encode("utf-8")).hexdigest()
    return str(uuid.UUID(h))


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--config", default="configs/knowledge_rag.yaml")
    parser.add_argument("--source_dir", required=True)
    parser.add_argument("--recreate", action="store_true",
                        help="重建 collection（清空旧数据）")
    parser.add_argument("--batch_size", type=int, default=32)
    args = parser.parse_args()

    cfg: dict[str, Any] = yaml.safe_load(Path(args.config).read_text(encoding="utf-8"))
    retr = cfg["retriever"]
    vs_cfg = retr["vector_store"]
    emb_cfg = retr["embedding"]

    # 解析环境变量占位
    vs_url = os.path.expandvars(vs_cfg["url"].replace(":-", ":-"))
    if vs_url.startswith("${"):
        vs_url = "http://localhost:6333"

    try:
        from FlagEmbedding import BGEM3FlagModel
        from qdrant_client import QdrantClient
        from qdrant_client.http import models as qm
    except ImportError as e:
        print("[error] 依赖未装：", e, file=sys.stderr)
        print("  pip install FlagEmbedding qdrant-client", file=sys.stderr)
        sys.exit(1)

    # 1. 加载 embedding 模型
    print(f"[build] 加载 embedding 模型：{emb_cfg['model']}")
    model = BGEM3FlagModel(emb_cfg["model"],
                            use_fp16=(emb_cfg.get("device") == "cuda"))

    # 2. 初始化 Qdrant
    client = QdrantClient(url=vs_url, timeout=vs_cfg.get("timeout", 30))
    collection = vs_cfg["collection"]

    # 探测维度（BGE-M3 固定 1024，但保留自适应）
    dim_probe = model.encode(["probe"], return_dense=True,
                               return_sparse=False, return_colbert_vecs=False)
    dim = len(dim_probe["dense_vecs"][0])
    print(f"[build] embedding dim = {dim}")

    if args.recreate:
        client.recreate_collection(
            collection_name=collection,
            vectors_config=qm.VectorParams(size=dim, distance=qm.Distance.COSINE),
        )
        print(f"[build] 已重建 collection={collection}")
    elif not client.collection_exists(collection):
        client.create_collection(
            collection_name=collection,
            vectors_config=qm.VectorParams(size=dim, distance=qm.Distance.COSINE),
        )

    # 3. 读文档并切片
    docs = load_docs(args.source_dir)
    chunks: list[dict[str, Any]] = []
    chunk_size = retr.get("chunk_size", 512)
    overlap = retr.get("chunk_overlap", 64)
    for d in docs:
        pieces = chunk_text(d["content"], chunk_size, overlap)
        for i, piece in enumerate(pieces):
            chunks.append({
                "id": stable_id(d["source"], i),
                "text": piece,
                "title": d["title"],
                "source": d["source"],
                "chunk_idx": i,
            })
    print(f"[build] 切片后共 {len(chunks)} 条")

    # 4. 批量编码 + upload
    bs = args.batch_size
    total = 0
    for i in range(0, len(chunks), bs):
        batch = chunks[i:i + bs]
        texts = [c["text"] for c in batch]
        enc = model.encode(texts, return_dense=True,
                           return_sparse=False, return_colbert_vecs=False,
                           max_length=emb_cfg.get("max_length", 8192),
                           batch_size=bs)
        vecs = enc["dense_vecs"]
        points = [
            qm.PointStruct(
                id=c["id"], vector=v.tolist(),
                payload={
                    "text": c["text"], "title": c["title"],
                    "source": c["source"], "chunk_idx": c["chunk_idx"],
                },
            )
            for c, v in zip(batch, vecs)
        ]
        client.upsert(collection_name=collection, points=points)
        total += len(points)
        print(f"  upsert {total}/{len(chunks)}")

    print(f"\n==========  ✅ 索引完成  ==========\n  collection: {collection}\n  points: {total}")


if __name__ == "__main__":
    main()
