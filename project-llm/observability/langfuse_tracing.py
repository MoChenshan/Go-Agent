"""
langfuse_tracing.py —— 统一 Langfuse 埋点工具

提供：
  - init_langfuse()        初始化全局 client（懒加载，未配置时返回 None）
  - observe_rag            RAG 链路装饰器（retrieve + generate 合并 trace）
  - observe_train          训练 step 装饰器（记录 loss / lr / step）
  - link_agent_trace       关联 Agent 侧 session_id，形成端到端链路

设计原则：
  - 未设置 LANGFUSE_* 环境变量时，所有装饰器降级为 no-op，绝不影响主流程
  - 只记录结构化元数据，不泄漏完整 prompt（通过 REDACT_PROMPT=1 控制）
  - 与 OpenTelemetry 兼容：Langfuse client 同时上报 OTLP（可通过 otel_genai_config.yaml）

环境变量：
  LANGFUSE_HOST        https://cloud.langfuse.com
  LANGFUSE_PUBLIC_KEY  pk-lf-xxx
  LANGFUSE_SECRET_KEY  sk-lf-xxx
  LANGFUSE_PROJECT     gameops-rag
  REDACT_PROMPT        1 时脱敏 prompt 内容
"""
from __future__ import annotations

import functools
import os
import time
import uuid
from contextlib import contextmanager
from typing import Any, Callable

__all__ = [
    "init_langfuse",
    "get_client",
    "observe_rag",
    "observe_train",
    "link_agent_trace",
    "trace_scope",
]

_CLIENT: Any = None
_INIT_DONE: bool = False


def init_langfuse() -> Any | None:
    """懒初始化 Langfuse client；未配置环境变量时返回 None。"""
    global _CLIENT, _INIT_DONE
    if _INIT_DONE:
        return _CLIENT
    _INIT_DONE = True

    pk = os.getenv("LANGFUSE_PUBLIC_KEY")
    sk = os.getenv("LANGFUSE_SECRET_KEY")
    if not (pk and sk):
        print("[langfuse] 未配置 LANGFUSE_PUBLIC_KEY/SECRET_KEY，观测降级为 no-op")
        return None

    try:
        from langfuse import Langfuse
    except ImportError:
        print("[langfuse] 未安装 langfuse 包，观测降级为 no-op。pip install langfuse")
        return None

    _CLIENT = Langfuse(
        public_key=pk,
        secret_key=sk,
        host=os.getenv("LANGFUSE_HOST", "https://cloud.langfuse.com"),
    )
    print(f"[langfuse] client ready → project={os.getenv('LANGFUSE_PROJECT','default')}")
    return _CLIENT


def get_client() -> Any | None:
    return init_langfuse()


def _redact(text: str, max_len: int = 200) -> str:
    """按 REDACT_PROMPT 开关脱敏"""
    if not text:
        return ""
    if os.getenv("REDACT_PROMPT") == "1":
        return f"[REDACTED, len={len(text)}]"
    if len(text) > max_len:
        return text[:max_len] + f"...[+{len(text)-max_len}]"
    return text


# =========================== 通用 trace 上下文 ===========================

@contextmanager
def trace_scope(name: str, *, user_id: str | None = None,
                session_id: str | None = None, metadata: dict | None = None):
    """
    创建一个 trace 上下文；未配置 Langfuse 时返回 no-op object。

    用法：
        with trace_scope("rag_query", session_id=sid) as t:
            t.update(output=answer)
    """
    client = init_langfuse()
    if client is None:
        class _NoOp:
            def update(self, **_):
                pass
            def span(self, **kwargs):
                return _NoOp()
            def generation(self, **kwargs):
                return _NoOp()
            def end(self, **_):
                pass
        yield _NoOp()
        return

    trace = client.trace(
        name=name, user_id=user_id, session_id=session_id,
        metadata=metadata or {},
    )
    try:
        yield trace
    finally:
        try:
            client.flush()
        except Exception:  # noqa: BLE001
            pass


# =========================== RAG 专用装饰器 ===========================

def observe_rag(fn: Callable) -> Callable:
    """
    装饰 RAG 主入口函数。

    约定被装饰函数签名形如：
        async def rag_query(query: str, session_id: str | None = None, ...) -> dict

    会自动记录：
      - input:  query
      - output: answer + citation 数量
      - metadata: latency_ms / trace_id / top_k
    """
    @functools.wraps(fn)
    async def wrapper(*args, **kwargs):
        query = kwargs.get("query") or (args[0] if args else "")
        session_id = kwargs.get("session_id") or uuid.uuid4().hex[:12]
        t0 = time.perf_counter()
        with trace_scope("rag_query", session_id=session_id,
                         metadata={"query_len": len(query or "")}) as tr:
            try:
                result = await fn(*args, **kwargs)
            except Exception as e:
                tr.update(level="ERROR", status_message=str(e))
                raise
            latency_ms = int((time.perf_counter() - t0) * 1000)
            meta_out = {}
            if isinstance(result, dict):
                meta_out = {
                    "latency_ms": latency_ms,
                    "n_citations": len(result.get("citations") or []),
                    "trace_id": result.get("trace_id"),
                }
            tr.update(
                input=_redact(query),
                output=_redact(str(result.get("answer", "")) if isinstance(result, dict) else str(result)),
                metadata=meta_out,
            )
            return result
    return wrapper


# =========================== 训练专用装饰器 ===========================

def observe_train(stage: str = "sft"):
    """
    装饰训练 step 函数。被装饰函数应返回 dict，至少包含 loss 字段。

    用法：
        @observe_train(stage="sft")
        def step(batch): ...
            return {"loss": 0.42, "lr": 5e-5}
    """
    def deco(fn: Callable) -> Callable:
        @functools.wraps(fn)
        def wrapper(*args, **kwargs):
            client = init_langfuse()
            t0 = time.perf_counter()
            result = fn(*args, **kwargs)
            if client and isinstance(result, dict):
                try:
                    client.event(
                        name=f"train_step:{stage}",
                        metadata={
                            "latency_ms": int((time.perf_counter() - t0) * 1000),
                            **{k: v for k, v in result.items()
                                if isinstance(v, (int, float, str))},
                        },
                    )
                except Exception:  # noqa: BLE001
                    pass
            return result
        return wrapper
    return deco


# =========================== Agent 关联 ===========================

def link_agent_trace(session_id: str, agent_trace_id: str,
                      extra: dict | None = None) -> None:
    """
    当 Agent 侧把 session_id 透传进来后，调用本函数登记一条关联事件，
    Langfuse UI 即可通过 session 视图看到 Agent ↔ RAG ↔ 训练 全链路。
    """
    client = init_langfuse()
    if client is None:
        return
    try:
        client.event(
            name="agent_trace_link",
            metadata={"agent_trace_id": agent_trace_id, **(extra or {})},
            session_id=session_id,
        )
    except Exception:  # noqa: BLE001
        pass
