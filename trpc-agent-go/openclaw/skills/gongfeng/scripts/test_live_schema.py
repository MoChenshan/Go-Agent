#!/usr/bin/env python3
"""Tests for Gongfeng live schema helper."""

from __future__ import annotations

import importlib.util
import json
import os
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler
from http.server import HTTPServer
from pathlib import Path


SCRIPT_PATH = (
    Path(__file__).resolve().parent / "live_schema.py"
)


def load_module():
    spec = importlib.util.spec_from_file_location(
        "gongfeng_live_schema",
        SCRIPT_PATH,
    )
    module = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    spec.loader.exec_module(module)
    return module


class MockMCPHandler(BaseHTTPRequestHandler):
    tools = [
        {
            "name": "create_merge_request_note",
            "description": "Create a comment for a merge request",
            "inputSchema": {
                "type": "object",
                "properties": {
                    "project_id": {"type": "string"},
                    "merge_request_id": {"type": "number"},
                    "path": {"type": "string"},
                    "line": {"type": "number"},
                    "line_type": {
                        "type": "string",
                        "enum": ["old", "new"],
                    },
                },
            },
        }
    ]

    def log_message(self, format, *args):
        return

    def do_POST(self):
        size = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(size).decode("utf-8")
        request = json.loads(body)
        if request["method"] == "initialize":
            payload = {
                "jsonrpc": "2.0",
                "id": request["id"],
                "result": {
                    "protocolVersion": "2025-03-26",
                    "capabilities": {"tools": {}},
                    "serverInfo": {
                        "name": "mock-gongfeng",
                        "version": "0.1",
                    },
                },
            }
        elif request["method"] == "tools/list":
            payload = {
                "jsonrpc": "2.0",
                "id": request["id"],
                "result": {"tools": self.tools},
            }
        else:
            payload = {
                "jsonrpc": "2.0",
                "id": request["id"],
                "error": {"code": -32601, "message": "method not found"},
            }
        content = "event: message\n" + "data: " + json.dumps(payload)
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.end_headers()
        self.wfile.write(content.encode("utf-8"))


class LiveSchemaTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.module = load_module()

    def setUp(self):
        self.server = HTTPServer(("127.0.0.1", 0), MockMCPHandler)
        self.thread = threading.Thread(
            target=self.server.serve_forever,
            daemon=True,
        )
        self.thread.start()
        self.temp_dir = tempfile.TemporaryDirectory()
        self.config_path = Path(self.temp_dir.name) / "mcp.json"
        self.token_env = "MCP_GONGFENG_ACCESS_TOKEN"
        os.environ[self.token_env] = "test-token"
        self.config_path.write_text(
            json.dumps(
                {
                    "mcpServers": {
                        "gongfeng": {
                            "url": (
                                "http://127.0.0.1:"
                                f"{self.server.server_port}/mcp"
                            ),
                            "headers": {
                                "Authorization": (
                                    "Bearer ${MCP_GONGFENG_ACCESS_TOKEN}"
                                )
                            },
                        }
                    }
                }
            ),
            encoding="utf-8",
        )

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=5)
        self.temp_dir.cleanup()
        os.environ.pop(self.token_env, None)

    def test_build_headers_expands_env(self):
        cfg = self.module.load_server_config(
            str(self.config_path),
            "gongfeng",
        )
        headers = self.module.build_headers(cfg)
        self.assertEqual(
            headers["Authorization"],
            "Bearer test-token",
        )

    def test_list_tools_parses_event_stream(self):
        cfg = self.module.load_server_config(
            str(self.config_path),
            "gongfeng",
        )
        headers = self.module.build_headers(cfg)
        self.module.initialize(cfg["url"], headers)
        tools = self.module.list_tools(cfg["url"], headers)
        self.assertEqual(tools[0]["name"], "create_merge_request_note")
        self.assertIn("path", tools[0]["inputSchema"]["properties"])
        self.assertIn("line_type", tools[0]["inputSchema"]["properties"])


if __name__ == "__main__":
    unittest.main()
