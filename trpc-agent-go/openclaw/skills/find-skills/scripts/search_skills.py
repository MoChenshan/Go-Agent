#!/usr/bin/env python3
"""
Search skills across SkillHub and Knot.
"""

from __future__ import annotations

import argparse
import json

from providers import search_all_providers

DEFAULT_LIMIT = 10
MAX_LIMIT = 50
TABLE_NAME_WIDTH = 24
TABLE_DESC_WIDTH = 64


def parse_limit(raw_value: str) -> int:
    """Parse a positive limit within the supported range."""
    value = int(raw_value)
    if value <= 0:
        raise argparse.ArgumentTypeError(
            "limit must be greater than zero",
        )
    if value > MAX_LIMIT:
        raise argparse.ArgumentTypeError(
            f"limit must be <= {MAX_LIMIT}",
        )
    return value


def truncate_text(text: str, width: int) -> str:
    """Return a compact single-line preview."""
    compact = " ".join(text.split())
    if len(compact) <= width:
        return compact
    if width <= 1:
        return compact[:width]
    return compact[: width - 1] + "…"


def print_results(payload: dict) -> None:
    """Render search results as a table plus JSON."""
    query = str(payload.get("query", "")).strip()
    used = payload.get("providers_used", [])
    skipped = payload.get("providers_skipped", [])
    results = payload.get("results", [])

    if query:
        print(f'Query: "{query}"')
    else:
        print("Query: <top skills>")

    if used:
        print("Providers used: " + ", ".join(used))
    if skipped:
        print("Providers skipped:")
        for item in skipped:
            provider = str(item.get("provider", "-")).strip()
            reason = str(item.get("reason", "-")).strip()
            print(f"- {provider}: {reason}")

    if not results:
        print("\nNo matching skills were found.")
        print("\n=== JSON_OUTPUT_START ===")
        print(json.dumps(payload, ensure_ascii=False, indent=2))
        print("=== JSON_OUTPUT_END ===")
        return

    print(f"\nFound {len(results)} skill candidate(s):\n")
    print(
        f"{'Provider':<10} {'Remote ID':<12} "
        f"{'Version':<10} {'Name':<{TABLE_NAME_WIDTH}} "
        "Description"
    )
    print("-" * 132)
    for item in results:
        provider = truncate_text(
            str(item.get("provider", "")),
            10,
        )
        remote_id = truncate_text(
            str(item.get("remote_id", "")),
            12,
        )
        version = truncate_text(
            str(item.get("version", "")),
            10,
        )
        name = truncate_text(
            str(item.get("name", "")),
            TABLE_NAME_WIDTH,
        )
        description = truncate_text(
            str(item.get("description", "")),
            TABLE_DESC_WIDTH,
        )
        print(
            f"{provider:<10} {remote_id:<12} {version:<10} "
            f"{name:<{TABLE_NAME_WIDTH}} {description}"
        )

    print("\n=== JSON_OUTPUT_START ===")
    print(json.dumps(payload, ensure_ascii=False, indent=2))
    print("=== JSON_OUTPUT_END ===")


def main() -> int:
    """CLI entry point."""
    parser = argparse.ArgumentParser(
        description="Search skills across SkillHub and Knot",
    )
    parser.add_argument(
        "--query",
        "-q",
        default="",
        help="Search query; empty means top skills",
    )
    parser.add_argument(
        "--limit",
        "-n",
        type=parse_limit,
        default=DEFAULT_LIMIT,
        help=f"Maximum results per provider (default: {DEFAULT_LIMIT})",
    )
    args = parser.parse_args()

    payload = search_all_providers(
        query=args.query,
        limit=args.limit,
    )
    print_results(payload)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
