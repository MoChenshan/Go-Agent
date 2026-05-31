"""
mcp_expert_server.py —— 将 RAG 服务封装为 MCP Server

对 GameOps Agent（project-agent）暴露工具：
  - knowledge_expert_query  运维/游戏知识库专家问答，返回带引用的答案

协议：MCP Streamable HTTP (2025-03) —— 与 project-agent 默认 transport 一致
端口：默认 8200

启动：
    python deploy/mcp_expert_server.py --rag_url http://localhost:8100

接入 project-agent（conf/mcp_servers.yaml）：
    - name: llm_knowledge_expert
      target: "*"
      url: http://localhost:8200/mcp
      transport: streamable
      timeout: 60
"""
from __future__ import annotations

import argparse
import asyncio
import os
import sys

import httpx


def build_server(rag_url: str, timeout: float = 60.0):
    """构造 FastMCP Server 实例"""
    try:
        from mcp.server.fastmcp import FastMCP
    except ImportError:
        print("[error] 未安装 mcp：pip install 'mcp[server]'", file=sys.stderr)
        sys.exit(1)

    mcp = FastMCP("llm_knowledge_expert")

    @mcp.tool()
    async def knowledge_expert_query(question: str, top_k: int = 5) -> dict:
        """
        运维/游戏知识库专家问答。

        何时调用：
          - 用户询问"为什么告警 XX / 如何扩容 / xx 指标什么意思"等**需要参考内部文档**的问题
          - 其他 MCP 工具（bk-monitor / bcs 等）拿不到结论，需要**结合知识库解释现象**时

        Args:
            question: 用户原始问题（建议保留完整上下文）
            top_k:    检索保留条数（默认 5，复杂问题可设 8-10）

        Returns:
            {
              "answer": "...",           # 带 [^N] 引用编号
              "citations": [             # 引用详情
                {"index": 1, "title": "...", "source": "...", "score": 0.87},
                ...
              ],
              "latency_ms": 420,
              "trace_id": "a1b2c3d4"
            }
        """
        async with httpx.AsyncClient(timeout=timeout) as client:
            r = await client.post(
                f"{rag_url.rstrip('/')}/rag/query",
                json={"query": question, "top_k": top_k, "stream": False},
            )
            r.raise_for_status()
            return r.json()

    @mcp.tool()
    async def knowledge_expert_health() -> dict:
        """检查 RAG 后端是否就绪，返回 collection 名"""
        async with httpx.AsyncClient(timeout=5.0) as client:
            try:
                r = await client.get(f"{rag_url.rstrip('/')}/healthz")
                return r.json()
            except Exception as e:  # noqa: BLE001
                return {"status": "down", "error": str(e)}

    return mcp


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--rag_url", default=os.getenv("RAG_URL", "http://localhost:8100"))
    parser.add_argument("--host", default="0.0.0.0")
    parser.add_argument("--port", type=int, default=8200)
    parser.add_argument("--timeout", type=float, default=60.0)
    args = parser.parse_args()

    mcp = build_server(args.rag_url, args.timeout)
    print(f"[mcp] serve at http://{args.host}:{args.port}/mcp  →  RAG {args.rag_url}")

    # FastMCP 2025-03 streamable transport
    try:
        import uvicorn
        uvicorn.run(mcp.streamable_http_app(), host=args.host, port=args.port)
    except AttributeError:
        # 旧版 API 兼容
        asyncio.run(mcp.run_streamable_http_async(host=args.host, port=args.port))


if __name__ == "__main__":
    main()
