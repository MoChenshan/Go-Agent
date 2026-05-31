#!/usr/bin/env python3
"""Fetch live Gongfeng MCP tool schema from the configured endpoint."""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
import urllib.error
import urllib.request
from pathlib import Path
from typing import Any


DEFAULT_SERVER_NAME = "gongfeng"
DEFAULT_PROTOCOL_VERSION = "2025-03-26"
DEFAULT_CLIENT_NAME = "trpc-claw-gongfeng-schema"
DEFAULT_CLIENT_VERSION = "1.0"
DEFAULT_TIMEOUT_SECONDS = 30
EVENT_STREAM_TYPE = "text/event-stream"
JSON_CONTENT_TYPE = "application/json"
CONFIG_RELATIVE_PATH = "../mcp.json"
ENV_PATTERN = re.compile(r"\$\{([A-Z0-9_]+)\}")
INITIALIZE_METHOD = "initialize"
TOOLS_LIST_METHOD = "tools/list"


class SchemaLookupError(RuntimeError):
    """Raised when live schema lookup fails."""


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description=(
            "Fetch live tool schema from the Gongfeng MCP endpoint "
            "declared in mcp.json."
        )
    )
    parser.add_argument(
        "--config",
        default=str(default_config_path()),
        help="Path to mcp.json (default: skill-local mcp.json).",
    )
    parser.add_argument(
        "--server",
        default=DEFAULT_SERVER_NAME,
        help="Server name inside mcp.json.",
    )
    parser.add_argument(
        "--tool",
        help=(
            "Print the full schema for one tool. Without this flag, "
            "the script lists tool names and descriptions."
        ),
    )
    parser.add_argument(
        "--json",
        action="store_true",
        help="Print raw JSON instead of summary text.",
    )
    return parser.parse_args()


def default_config_path() -> Path:
    return (Path(__file__).resolve().parent / CONFIG_RELATIVE_PATH).resolve()


def load_server_config(
    config_path: str,
    server_name: str,
) -> dict[str, Any]:
    with open(config_path, "r", encoding="utf-8") as f:
        config = json.load(f)
    servers = config.get("mcpServers")
    if not isinstance(servers, dict):
        raise SchemaLookupError("mcpServers is missing from config")
    server = servers.get(server_name)
    if not isinstance(server, dict):
        raise SchemaLookupError(
            f"server {server_name!r} not found in config"
        )
    return server


def expand_env_placeholders(value: str) -> str:
    def replace(match: re.Match[str]) -> str:
        env_name = match.group(1)
        env_value = os.environ.get(env_name)
        if env_value is None:
            raise SchemaLookupError(
                f"required environment variable {env_name!r} is not set"
            )
        return env_value

    return ENV_PATTERN.sub(replace, value)


def build_headers(server_cfg: dict[str, Any]) -> dict[str, str]:
    headers = {
        "Accept": f"{JSON_CONTENT_TYPE}, {EVENT_STREAM_TYPE}",
        "Content-Type": JSON_CONTENT_TYPE,
    }
    configured = server_cfg.get("headers", {})
    if not isinstance(configured, dict):
        raise SchemaLookupError("headers must be an object in mcp config")
    for key, value in configured.items():
        if not isinstance(value, str):
            raise SchemaLookupError(
                f"header {key!r} must be a string in mcp config"
            )
        headers[key] = expand_env_placeholders(value)
    return headers


def post_json(
    url: str,
    headers: dict[str, str],
    payload: dict[str, Any],
) -> dict[str, Any]:
    body = json.dumps(payload).encode("utf-8")
    request = urllib.request.Request(
        url,
        data=body,
        headers=headers,
        method="POST",
    )
    try:
        with urllib.request.urlopen(
            request,
            timeout=DEFAULT_TIMEOUT_SECONDS,
        ) as response:
            content_type = response.headers.get("content-type", "")
            raw = response.read().decode("utf-8")
    except urllib.error.HTTPError as err:
        detail = err.read().decode("utf-8", errors="replace")
        raise SchemaLookupError(
            f"HTTP {err.code} from MCP endpoint: {detail}"
        ) from err
    except urllib.error.URLError as err:
        raise SchemaLookupError(f"failed to reach MCP endpoint: {err}") from err

    if EVENT_STREAM_TYPE in content_type:
        return parse_event_stream(raw)
    if JSON_CONTENT_TYPE in content_type:
        return json.loads(raw)
    raise SchemaLookupError(f"unsupported content type: {content_type}")


def parse_event_stream(raw: str) -> dict[str, Any]:
    for line in raw.splitlines():
        if line.startswith("data: "):
            return json.loads(line[len("data: ") :])
    raise SchemaLookupError("event stream did not contain a data payload")


def initialize(
    url: str,
    headers: dict[str, str],
) -> None:
    payload = {
        "jsonrpc": "2.0",
        "id": 1,
        "method": INITIALIZE_METHOD,
        "params": {
            "protocolVersion": DEFAULT_PROTOCOL_VERSION,
            "capabilities": {},
            "clientInfo": {
                "name": DEFAULT_CLIENT_NAME,
                "version": DEFAULT_CLIENT_VERSION,
            },
        },
    }
    response = post_json(url, headers, payload)
    if "error" in response:
        raise SchemaLookupError(
            f"initialize failed: {json.dumps(response['error'])}"
        )


def list_tools(
    url: str,
    headers: dict[str, str],
) -> list[dict[str, Any]]:
    payload = {
        "jsonrpc": "2.0",
        "id": 2,
        "method": TOOLS_LIST_METHOD,
        "params": {},
    }
    response = post_json(url, headers, payload)
    if "error" in response:
        raise SchemaLookupError(
            f"tools/list failed: {json.dumps(response['error'])}"
        )
    result = response.get("result", {})
    tools = result.get("tools")
    if not isinstance(tools, list):
        raise SchemaLookupError("tools/list response did not include tools")
    return tools


def format_tool_summary(tool: dict[str, Any]) -> str:
    name = tool.get("name", "<unknown>")
    description = tool.get("description", "")
    return f"{name}: {description}"


def main() -> int:
    args = parse_args()
    try:
        server_cfg = load_server_config(args.config, args.server)
        url = server_cfg.get("url")
        if not isinstance(url, str) or not url:
            raise SchemaLookupError("server url is missing from mcp config")
        headers = build_headers(server_cfg)
        initialize(url, headers)
        tools = list_tools(url, headers)
        if args.tool:
            tool = next(
                (item for item in tools if item.get("name") == args.tool),
                None,
            )
            if tool is None:
                raise SchemaLookupError(
                    f"tool {args.tool!r} not found in live schema"
                )
            print(json.dumps(tool, ensure_ascii=False, indent=2))
            return 0
        if args.json:
            print(json.dumps(tools, ensure_ascii=False, indent=2))
            return 0
        for tool in tools:
            print(format_tool_summary(tool))
        return 0
    except SchemaLookupError as err:
        print(f"error: {err}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
