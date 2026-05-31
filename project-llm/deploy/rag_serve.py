"""
rag_serve.py —— Agentic RAG 服务（FastAPI + OpenAI 兼容）

核心链路：
    query → [BGE-M3 dense+sparse 一次前向编码]
         → [Qdrant dense 召回 ∥ Qdrant sparse 召回]   ← hybrid_search
         → [RRF 加权融合 (k=60, dense_w/sparse_w 由 yaml 控制)]
         → [BGE-Reranker-v2-m3 精排 + score≥0.3 过滤]
         → [MMR 多样化 (λ=0.7)]
         → [Qwen3-8B-knowledge-sft via vLLM 融合生成 + citations]
         → stream/json 返回

端点：
    POST /v1/chat/completions   —— OpenAI 兼容（支持 stream）
    POST /rag/query             —— 原生端点，返回 answer + citations
    GET  /healthz               —— 健康检查
    GET  /metrics               —— Prometheus 指标

使用：
    uvicorn deploy.rag_serve:app --host 0.0.0.0 --port 8100
"""
from __future__ import annotations

import asyncio
import json
import os
import time
import uuid
from contextlib import asynccontextmanager
from pathlib import Path
from typing import Any, AsyncIterator

import yaml
from fastapi import FastAPI, HTTPException, Response
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import JSONResponse, StreamingResponse
from pydantic import BaseModel, Field

# ── 观测（可选依赖，未安装则自动降级）──
import sys
sys.path.insert(0, str(Path(__file__).resolve().parent.parent))
try:
    from observability.langfuse_tracing import init_langfuse, trace_scope
except Exception:  # noqa: BLE001
    init_langfuse = lambda: None  # type: ignore
    from contextlib import contextmanager as _cm
    @_cm
    def trace_scope(*_a, **_kw):
        class _N:
            def update(self, **_): pass
        yield _N()

try:
    from prometheus_client import (
        Counter, Histogram, CONTENT_TYPE_LATEST, generate_latest,
    )
    _PROM = True
except Exception:  # noqa: BLE001
    _PROM = False


# =============================== 配置 ===============================

CONFIG_PATH = os.getenv("RAG_CONFIG", "configs/knowledge_rag.yaml")


def _expand(v: str) -> str:
    """支持 ${VAR:-default} 语法"""
    if not isinstance(v, str) or not v.startswith("${"):
        return v
    body = v[2:-1]
    if ":-" in body:
        name, default = body.split(":-", 1)
        return os.getenv(name, default)
    return os.getenv(body, "")


def load_config() -> dict[str, Any]:
    cfg = yaml.safe_load(Path(CONFIG_PATH).read_text(encoding="utf-8"))
    # 递归展开 env 占位
    def walk(o):
        if isinstance(o, dict):
            return {k: walk(v) for k, v in o.items()}
        if isinstance(o, list):
            return [walk(x) for x in o]
        if isinstance(o, str):
            return _expand(o)
        return o
    return walk(cfg)


# =============================== 组件 ===============================

class Retriever:
    """BGE-M3 稠密检索 + BGE-Reranker 重排"""

    def __init__(self, cfg: dict[str, Any]):
        self.cfg = cfg
        self._embed = None
        self._rerank = None
        self._qdrant = None

    def lazy_init(self):
        if self._embed is not None:
            return
        from FlagEmbedding import BGEM3FlagModel, FlagReranker
        from qdrant_client import QdrantClient

        emb = self.cfg["embedding"]
        self._embed = BGEM3FlagModel(emb["model"],
                                       use_fp16=(emb.get("device") == "cuda"))
        rer = self.cfg.get("reranker", {})
        if rer.get("enabled"):
            self._rerank = FlagReranker(rer["model"], use_fp16=True)
        vs = self.cfg["vector_store"]
        self._qdrant = QdrantClient(url=vs["url"], timeout=vs.get("timeout", 30))

    def search(self, query: str) -> list[dict[str, Any]]:
        """
        检索流程：
        1. BGE-M3 一次前向同时产出 dense + sparse 两路向量
        2. dense 走 Qdrant 标准向量检索
        3. sparse 走 Qdrant Sparse Vector 检索（BM25 等价：lexical 稀疏）
        4. 两路结果用 RRF（Reciprocal Rank Fusion）按权重融合
        5. 走 BGE-Reranker-v2-m3 精排 + 阈值过滤
        当 hybrid_search=false 时，自动降级为纯 dense（保留旧行为）。
        """
        self.lazy_init()
        emb = self.cfg["embedding"]
        vs = self.cfg["vector_store"]
        top_k = self.cfg.get("top_k", 20)
        hybrid = bool(self.cfg.get("hybrid_search", True))
        sparse_w = float(emb.get("sparse_weight", 0.0))
        dense_w = float(emb.get("dense_weight", 1.0))
        # 仅当真正配置了混合 + sparse_weight>0 才走 sparse 分支，避免无意义编码开销
        use_sparse = hybrid and sparse_w > 0.0

        enc = self._embed.encode(
            [query],
            return_dense=True,
            return_sparse=use_sparse,
            return_colbert_vecs=False,
            max_length=emb.get("max_length", 8192),
        )
        qvec = enc["dense_vecs"][0].tolist()

        # ---- 1) dense 召回 ----
        dense_hits = self._qdrant.search(
            collection_name=vs["collection"],
            query_vector=qvec,
            limit=top_k,
            with_payload=True,
        )

        # ---- 2) sparse 召回（可选） ----
        sparse_hits = []
        if use_sparse:
            try:
                from qdrant_client import models as qm
                lw = enc["lexical_weights"][0]  # dict[token_id -> weight]
                indices = [int(k) for k in lw.keys()]
                values = [float(v) for v in lw.values()]
                sparse_hits = self._qdrant.search(
                    collection_name=vs["collection"],
                    query_vector=qm.NamedSparseVector(
                        name="sparse",
                        vector=qm.SparseVector(indices=indices, values=values),
                    ),
                    limit=top_k,
                    with_payload=True,
                )
            except Exception as e:  # noqa: BLE001
                # collection 未建 sparse 索引时优雅降级，不影响主链路
                print(f"[retriever] sparse search disabled: {e}")
                sparse_hits = []

        # ---- 3) RRF 融合（Reciprocal Rank Fusion） ----
        # 公式：score(d) = Σ_i  w_i / (k + rank_i(d))，k=60 业界常用值
        candidates = self._rrf_merge(
            [dense_hits, sparse_hits],
            weights=[dense_w, sparse_w if use_sparse else 0.0],
            limit=top_k,
        )

        # Rerank
        rer = self.cfg.get("reranker", {})
        if rer.get("enabled") and self._rerank and candidates:
            pairs = [[query, c["text"]] for c in candidates]
            scores = self._rerank.compute_score(pairs, normalize=True)
            if not isinstance(scores, list):
                scores = [scores]
            for c, s in zip(candidates, scores):
                c["rerank_score"] = float(s)
            threshold = rer.get("score_threshold", 0.0)
            candidates = [c for c in candidates if c["rerank_score"] >= threshold]
            candidates.sort(key=lambda x: x["rerank_score"], reverse=True)
            top_k = rer.get("top_k", 5)
            candidates = candidates[:top_k]

        # ---- 4) MMR 多样化（同源大段切片去冗余） ----
        if self.cfg.get("mmr_enabled") and len(candidates) > 1:
            candidates = self._mmr(
                query_vec=qvec,
                candidates=candidates,
                lambda_=float(self.cfg.get("mmr_lambda", 0.7)),
            )
        return candidates

    # ------------------------------------------------------------------ #
    # 融合 / 多样化工具方法
    # ------------------------------------------------------------------ #
    @staticmethod
    def _rrf_merge(hit_lists, weights, limit: int, k_const: int = 60):
        """
        Reciprocal Rank Fusion：把 N 路检索结果按 rank 倒数加权求和。
        - hit_lists: 每路的 hits（Qdrant ScoredPoint 列表，已按分数降序）
        - weights:   每路的权重（与 hit_lists 一一对应）
        - k_const:   RRF 常量，k=60 来自 Cormack 2009，业界默认
        返回融合后按融合分降序的候选 dict 列表。
        """
        bucket: dict[Any, dict[str, Any]] = {}
        for hits, w in zip(hit_lists, weights):
            if not hits or w <= 0:
                continue
            for rank, h in enumerate(hits, start=1):
                key = getattr(h, "id", None) or id(h)
                payload = h.payload or {}
                contrib = w / (k_const + rank)
                if key in bucket:
                    bucket[key]["fusion_score"] += contrib
                else:
                    bucket[key] = {
                        "fusion_score": contrib,
                        "score": float(getattr(h, "score", 0.0)),
                        **payload,
                    }
        merged = sorted(bucket.values(),
                        key=lambda x: x["fusion_score"], reverse=True)
        return merged[:limit]

    @staticmethod
    def _mmr(query_vec, candidates, lambda_: float = 0.7):
        """
        Maximal Marginal Relevance：在相关性与多样性之间取平衡。
        score = λ·rel(q, d) - (1-λ)·max_{d'∈S} sim(d, d')
        这里用 candidates 已有的 rerank_score / score 作为 rel(q,d)，
        d 之间相似度走 payload 中可选的 dense_vec；缺失则按 source 去重退化。
        """
        try:
            import numpy as np
        except Exception:  # noqa: BLE001
            return candidates

        def _vec(c):
            v = c.get("dense_vec")
            return np.array(v, dtype=np.float32) if v else None

        def _cos(a, b):
            if a is None or b is None:
                return 0.0
            denom = (np.linalg.norm(a) * np.linalg.norm(b)) or 1.0
            return float(np.dot(a, b) / denom)

        remaining = list(candidates)
        selected: list[dict[str, Any]] = []
        seen_sources: set[str] = set()
        while remaining and len(selected) < len(candidates):
            best_idx, best_score = 0, -1e9
            for i, c in enumerate(remaining):
                rel = float(c.get("rerank_score", c.get("fusion_score", c.get("score", 0.0))))
                if not selected:
                    diversity = 0.0
                else:
                    a = _vec(c)
                    if a is not None:
                        diversity = max(_cos(a, _vec(s)) for s in selected)
                    else:
                        # 退化策略：同 source 视为 1.0 相似
                        diversity = 1.0 if c.get("source") in seen_sources else 0.0
                mmr = lambda_ * rel - (1 - lambda_) * diversity
                if mmr > best_score:
                    best_score, best_idx = mmr, i
            picked = remaining.pop(best_idx)
            picked["mmr_score"] = best_score
            seen_sources.add(picked.get("source", ""))
            selected.append(picked)
        return selected


class Generator:
    """OpenAI 兼容的 LLM 客户端（httpx 直连，支持 fallback）"""

    def __init__(self, cfg: dict[str, Any]):
        self.cfg = cfg
        self._client = None

    def _make_client(self):
        if self._client is None:
            import httpx
            self._client = httpx.AsyncClient(timeout=self.cfg.get("timeout", 120))
        return self._client

    async def complete(self, messages: list[dict], *, stream: bool = False,
                       extra: dict | None = None) -> Any:
        client = self._make_client()
        base = self.cfg["base_url"].rstrip("/")
        headers = {"Authorization": f"Bearer {self.cfg.get('api_key', 'EMPTY')}"}
        payload = {
            "model": self.cfg["model"],
            "messages": messages,
            "temperature": self.cfg.get("temperature", 0.1),
            "top_p": self.cfg.get("top_p", 0.9),
            "max_tokens": self.cfg.get("max_tokens", 1024),
            "stream": stream,
            **(extra or {}),
        }

        url = f"{base}/chat/completions"
        try:
            if stream:
                return self._stream(client, url, headers, payload)
            r = await client.post(url, headers=headers, json=payload)
            r.raise_for_status()
            return r.json()
        except Exception as e:  # noqa: BLE001
            fb = self.cfg.get("fallback") or {}
            if not fb.get("enabled"):
                raise
            print(f"[gen] 主模型失败，降级到 {fb.get('model')}: {e}")
            base = fb["base_url"].rstrip("/")
            headers = {"Authorization": f"Bearer {fb.get('api_key', 'EMPTY')}"}
            payload["model"] = fb["model"]
            if stream:
                return self._stream(client, f"{base}/chat/completions", headers, payload)
            r = await client.post(f"{base}/chat/completions",
                                    headers=headers, json=payload)
            r.raise_for_status()
            return r.json()

    async def _stream(self, client, url: str, headers: dict,
                      payload: dict) -> AsyncIterator[str]:
        async with client.stream("POST", url, headers=headers,
                                  json=payload) as r:
            r.raise_for_status()
            async for line in r.aiter_lines():
                if not line or not line.startswith("data:"):
                    continue
                body = line[5:].strip()
                if body == "[DONE]":
                    yield "data: [DONE]\n\n"
                    return
                yield f"data: {body}\n\n"


# =============================== 应用 ===============================

CFG: dict[str, Any] = {}
RETRIEVER: Retriever | None = None
GENERATOR: Generator | None = None

# Prometheus 指标
if _PROM:
    RAG_REQ = Counter("rag_requests_total", "RAG requests", ["endpoint", "status"])
    RAG_LAT = Histogram(
        "rag_latency_seconds", "RAG end-to-end latency",
        ["endpoint"],
        buckets=(0.1, 0.3, 0.5, 1, 2, 3, 5, 8, 13, 21),
    )
    RAG_CIT = Histogram(
        "rag_citation_count", "citations per query", buckets=(0, 1, 2, 3, 5, 8, 13),
    )
    RAG_RETRIEVED = Counter("rag_retrieved_chunks_total", "chunks retrieved")


@asynccontextmanager
async def lifespan(app: FastAPI):
    global CFG, RETRIEVER, GENERATOR
    CFG = load_config()
    RETRIEVER = Retriever(CFG["retriever"])
    GENERATOR = Generator(CFG["generator"])
    init_langfuse()
    print(f"[boot] RAG serve ready  collection={CFG['retriever']['vector_store']['collection']}")
    yield


app = FastAPI(title="GameOps Knowledge RAG", version="1.0.0", lifespan=lifespan)
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"], allow_credentials=True,
    allow_methods=["*"], allow_headers=["*"],
)


# =============================== Schema ===============================

class RAGRequest(BaseModel):
    query: str
    top_k: int | None = None
    stream: bool = False
    session_id: str | None = None


class Citation(BaseModel):
    index: int
    title: str
    source: str
    score: float


class RAGResponse(BaseModel):
    answer: str
    citations: list[Citation] = Field(default_factory=list)
    latency_ms: int
    trace_id: str


class ChatMessage(BaseModel):
    role: str
    content: str


class ChatRequest(BaseModel):
    model: str = "knowledge-rag"
    messages: list[ChatMessage]
    stream: bool = False
    temperature: float | None = None
    max_tokens: int | None = None


# =============================== 核心工具函数 ===============================

def build_context(chunks: list[dict]) -> tuple[str, list[Citation]]:
    lines = []
    citations: list[Citation] = []
    for i, c in enumerate(chunks, 1):
        lines.append(f"[{i}] 标题: {c.get('title','')}\n来源: {c.get('source','')}\n内容: {c.get('text','')}\n")
        citations.append(Citation(
            index=i, title=c.get("title", ""), source=c.get("source", ""),
            score=float(c.get("rerank_score", c.get("score", 0.0))),
        ))
    return "\n".join(lines), citations


def build_messages(question: str, context: str) -> list[dict]:
    p = CFG["prompt"]
    user = p["user_template"].format(context=context, question=question)
    return [
        {"role": "system", "content": p["system"]},
        {"role": "user", "content": user},
    ]


# =============================== 路由 ===============================

@app.get("/healthz")
async def health():
    return {"status": "ok", "collection": CFG["retriever"]["vector_store"]["collection"]}


@app.get("/metrics")
async def metrics():
    if not _PROM:
        return Response("prometheus_client not installed", media_type="text/plain")
    return Response(generate_latest(), media_type=CONTENT_TYPE_LATEST)


@app.post("/rag/query", response_model=RAGResponse)
async def rag_query(req: RAGRequest):
    if RETRIEVER is None or GENERATOR is None:
        raise HTTPException(503, "service not ready")
    trace_id = uuid.uuid4().hex[:12]
    session_id = req.session_id or trace_id
    t0 = time.perf_counter()

    with trace_scope("rag_query", session_id=session_id,
                     metadata={"query_len": len(req.query)}) as tr:
        try:
            chunks = await asyncio.to_thread(RETRIEVER.search, req.query)
            if _PROM:
                RAG_RETRIEVED.inc(len(chunks))
            if not chunks:
                latency_ms = int((time.perf_counter() - t0) * 1000)
                if _PROM:
                    RAG_REQ.labels("rag_query", "empty").inc()
                    RAG_LAT.labels("rag_query").observe(latency_ms / 1000)
                answer = "资料不足，暂无法回答该问题。建议排查方向：检查日志 / 查询监控面板 / 联系值班。"
                tr.update(input=req.query, output=answer,
                          metadata={"latency_ms": latency_ms, "n_citations": 0})
                return RAGResponse(answer=answer, citations=[],
                                   latency_ms=latency_ms, trace_id=trace_id)
            context, citations = build_context(chunks)
            messages = build_messages(req.query, context)
            resp = await GENERATOR.complete(messages, stream=False)
            answer = resp["choices"][0]["message"]["content"]
            latency_ms = int((time.perf_counter() - t0) * 1000)
            if _PROM:
                RAG_REQ.labels("rag_query", "ok").inc()
                RAG_LAT.labels("rag_query").observe(latency_ms / 1000)
                RAG_CIT.observe(len(citations))
            tr.update(
                input=req.query,
                output=answer[:500],
                metadata={
                    "latency_ms": latency_ms,
                    "n_citations": len(citations),
                    "top_score": citations[0].score if citations else 0.0,
                    "trace_id": trace_id,
                },
            )
            return RAGResponse(answer=answer, citations=citations,
                               latency_ms=latency_ms, trace_id=trace_id)
        except Exception as e:
            if _PROM:
                RAG_REQ.labels("rag_query", "error").inc()
            tr.update(level="ERROR", status_message=str(e))
            raise


@app.post("/v1/chat/completions")
async def openai_compat(req: ChatRequest):
    """OpenAI 兼容端点：把最后一条 user 消息作为 query 触发 RAG。"""
    if RETRIEVER is None or GENERATOR is None:
        raise HTTPException(503, "service not ready")
    user_msgs = [m for m in req.messages if m.role == "user"]
    if not user_msgs:
        raise HTTPException(400, "no user message")
    query = user_msgs[-1].content

    chunks = await asyncio.to_thread(RETRIEVER.search, query)
    context, citations = build_context(chunks)
    messages = build_messages(query, context)

    extra = {}
    if req.temperature is not None:
        extra["temperature"] = req.temperature
    if req.max_tokens is not None:
        extra["max_tokens"] = req.max_tokens

    if not req.stream:
        resp = await GENERATOR.complete(messages, stream=False, extra=extra)
        if CFG["service"].get("return_citations", True):
            resp.setdefault("metadata", {})["citations"] = [c.model_dump() for c in citations]
        return JSONResponse(resp)

    async def _gen():
        it = await GENERATOR.complete(messages, stream=True, extra=extra)
        async for chunk in it:
            yield chunk
        # 末尾附加引用（非标准扩展，客户端可选忽略）
        if CFG["service"].get("return_citations", True) and citations:
            payload = {"citations": [c.model_dump() for c in citations]}
            yield f"data: {json.dumps(payload, ensure_ascii=False)}\n\n"

    return StreamingResponse(_gen(), media_type="text/event-stream")
