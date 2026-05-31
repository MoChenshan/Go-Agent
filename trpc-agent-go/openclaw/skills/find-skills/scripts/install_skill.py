#!/usr/bin/env python3
"""
Install a selected marketplace skill into the local skills directory.
"""

from __future__ import annotations

import argparse
import json
import os
import urllib.error
import urllib.parse
import urllib.request
from pathlib import Path

from providers import (
    KNOT_PROVIDER,
    PROVIDER_PRIORITY,
    SKILLHUB_PROVIDER,
    download_provider_archive,
)
from registry_common import (
    DEFAULT_TIMEOUT_SECONDS,
    RegistryError,
    install_skill_archive,
    read_json_url,
    resolve_install_root,
)

OPENCLAW_CONFIG_ENV_NAME = "OPENCLAW_CONFIG"
OPENCLAW_APP_NAME = "openclaw"
DEFAULT_CONFIG_FILE_NAME = "openclaw.yaml"
DEFAULT_ADMIN_HOST = "127.0.0.1"
DEFAULT_ADMIN_PORT = 19789
DEFAULT_ADMIN_AUTO_PORT_SPAN = 32
ADMIN_DISCOVERY_TIMEOUT_SECONDS = 1
ADMIN_STATUS_PATH = "/api/status"
ADMIN_SKILLS_REFRESH_PATH = "/api/skills/refresh"
SKILLS_DIR_NAME = "skills"
LOCAL_SKILLS_DIR_NAME = "local"
REFRESH_REASON_SKIPPED_INSTALL = "installation was skipped"
REFRESH_REASON_UNMANAGED_ROOT = (
    "install root is outside the managed local skills directory"
)
REFRESH_REASON_ADMIN_NOT_FOUND = (
    "no running OpenClaw admin instance was found for this state_dir"
)


def parse_provider(raw_value: str) -> str:
    """Parse a supported provider name."""
    provider = raw_value.strip().lower()
    if provider in PROVIDER_PRIORITY:
        return provider
    raise argparse.ArgumentTypeError(
        "provider must be one of: "
        + ", ".join(PROVIDER_PRIORITY),
    )


def parse_bool(raw_value: str) -> bool | None:
    """Parse a YAML-like boolean scalar."""
    value = raw_value.strip().strip("'\"").lower()
    if value in {"true", "yes", "on"}:
        return True
    if value in {"false", "no", "off"}:
        return False
    return None


def parse_admin_runtime_config(config_path: Path) -> dict[str, object]:
    """Read the local admin runtime config from an OpenClaw YAML file."""
    config = {
        "enabled": True,
        "addr": f"{DEFAULT_ADMIN_HOST}:{DEFAULT_ADMIN_PORT}",
        "auto_port": True,
    }
    if not config_path.is_file():
        return config

    lines = config_path.read_text(encoding="utf-8").splitlines()
    in_admin = False
    admin_indent = 0
    for raw_line in lines:
        stripped_line = raw_line.split("#", 1)[0].rstrip()
        if not stripped_line:
            continue
        indent = len(raw_line) - len(raw_line.lstrip(" "))
        if not in_admin:
            if stripped_line.lstrip().startswith("admin:"):
                in_admin = True
                admin_indent = indent
            continue
        if indent <= admin_indent:
            break

        key, _, value = stripped_line.strip().partition(":")
        key = key.strip()
        value = value.strip()
        if key == "enabled":
            parsed = parse_bool(value)
            if parsed is not None:
                config["enabled"] = parsed
        elif key == "addr" and value:
            config["addr"] = value.strip("'\"")
        elif key == "auto_port":
            parsed = parse_bool(value)
            if parsed is not None:
                config["auto_port"] = parsed
    return config


def admin_config_path_candidates(state_dir: Path | None) -> list[Path]:
    """Return likely OpenClaw config paths for the current runtime."""
    paths: list[Path] = []

    raw_path = os.environ.get(OPENCLAW_CONFIG_ENV_NAME, "").strip()
    if raw_path:
        paths.append(Path(raw_path).expanduser())
    if state_dir is not None:
        paths.append(state_dir / DEFAULT_CONFIG_FILE_NAME)

    unique_paths: list[Path] = []
    seen: set[Path] = set()
    for path in paths:
        resolved = path.expanduser()
        if resolved in seen:
            continue
        seen.add(resolved)
        unique_paths.append(resolved)
    return unique_paths


def load_admin_runtime_config(state_dir: Path | None) -> dict[str, object]:
    """Load admin discovery settings from the current OpenClaw config."""
    for candidate in admin_config_path_candidates(state_dir):
        if candidate.is_file():
            return parse_admin_runtime_config(candidate)
    return {
        "enabled": True,
        "addr": f"{DEFAULT_ADMIN_HOST}:{DEFAULT_ADMIN_PORT}",
        "auto_port": True,
    }


def normalize_admin_host(raw_host: str) -> str:
    """Normalize listen hosts for loopback probing."""
    host = raw_host.strip().strip("[]")
    if not host or host in {"0.0.0.0", "::"}:
        return DEFAULT_ADMIN_HOST
    return host


def parse_admin_addr(raw_addr: str) -> tuple[str, int]:
    """Parse the configured admin listen address."""
    value = raw_addr.strip().strip("'\"")
    if not value:
        return DEFAULT_ADMIN_HOST, DEFAULT_ADMIN_PORT
    if value.startswith(":"):
        port_text = value[1:]
        host = DEFAULT_ADMIN_HOST
    else:
        host_part, sep, port_text = value.rpartition(":")
        if not sep:
            return DEFAULT_ADMIN_HOST, DEFAULT_ADMIN_PORT
        host = normalize_admin_host(host_part)
    try:
        port = int(port_text.strip())
    except ValueError:
        port = DEFAULT_ADMIN_PORT
    if port <= 0:
        port = DEFAULT_ADMIN_PORT
    return host, port


def candidate_admin_urls(
    raw_addr: str,
    auto_port: bool,
) -> list[str]:
    """Build candidate admin base URLs for discovery."""
    host, port = parse_admin_addr(raw_addr)
    max_port = port
    if auto_port:
        max_port += DEFAULT_ADMIN_AUTO_PORT_SPAN

    urls = []
    for candidate_port in range(port, max_port + 1):
        urls.append(f"http://{host}:{candidate_port}")
    return urls


def managed_state_dir_for_install_root(
    install_root: Path,
) -> Path | None:
    """Infer the managed state_dir when install_root is the local skill root."""
    resolved = install_root.expanduser().resolve()
    if resolved.name != LOCAL_SKILLS_DIR_NAME:
        return None
    if resolved.parent.name != SKILLS_DIR_NAME:
        return None
    return resolved.parent.parent


def same_resolved_path(left: str, right: Path) -> bool:
    """Return whether a JSON path value matches the expected path."""
    raw_value = left.strip()
    if not raw_value:
        return False
    try:
        resolved = Path(raw_value).expanduser().resolve()
    except OSError:
        return False
    return resolved == right.expanduser().resolve()


def discover_admin_url(state_dir: Path | None) -> str:
    """Find a running OpenClaw admin URL for the given state_dir."""
    config = load_admin_runtime_config(state_dir)
    if not config.get("enabled", True):
        return ""

    raw_addr = str(config.get("addr", ""))
    auto_port = bool(config.get("auto_port", True))
    for base_url in candidate_admin_urls(raw_addr, auto_port):
        status_url = base_url + ADMIN_STATUS_PATH
        try:
            status = read_json_url(
                status_url,
                timeout=ADMIN_DISCOVERY_TIMEOUT_SECONDS,
            )
        except RegistryError:
            continue
        if not isinstance(status, dict):
            continue
        if str(status.get("app_name", "")).strip() != OPENCLAW_APP_NAME:
            continue
        if (
            state_dir is not None
            and not same_resolved_path(
                str(status.get("state_dir", "")),
                state_dir,
            )
        ):
            continue
        admin_url = str(status.get("admin_url", "")).strip()
        if admin_url:
            return admin_url.rstrip("/")
        return base_url
    return ""


def post_refresh_request(admin_url: str) -> tuple[bool, str]:
    """Call the live skills refresh endpoint."""
    request = urllib.request.Request(
        admin_url.rstrip("/") + ADMIN_SKILLS_REFRESH_PATH,
        data=b"",
        method="POST",
        headers={
            "Content-Type": "application/x-www-form-urlencoded",
        },
    )
    try:
        with urllib.request.urlopen(
            request,
            timeout=DEFAULT_TIMEOUT_SECONDS,
        ) as response:
            final_url = response.geturl()
    except urllib.error.HTTPError as err:
        body = err.read().decode("utf-8", errors="replace")
        return False, f"HTTP {err.code}: {body}"
    except urllib.error.URLError as err:
        return False, f"failed to reach admin: {err.reason}"

    parsed = urllib.parse.urlparse(final_url)
    query = urllib.parse.parse_qs(parsed.query)
    if "error" in query and query["error"]:
        return False, query["error"][0]
    if "notice" in query and query["notice"]:
        return True, query["notice"][0]
    return True, "skills refresh completed"


def refresh_runtime_skills(
    install_root: Path,
    installed: bool,
) -> dict[str, object]:
    """Refresh the running OpenClaw skill index when possible."""
    if not installed:
        return {
            "attempted": False,
            "refreshed": False,
            "reason": REFRESH_REASON_SKIPPED_INSTALL,
        }

    state_dir = managed_state_dir_for_install_root(install_root)
    if state_dir is None:
        return {
            "attempted": False,
            "refreshed": False,
            "reason": REFRESH_REASON_UNMANAGED_ROOT,
        }

    admin_url = discover_admin_url(state_dir)
    if not admin_url:
        return {
            "attempted": False,
            "refreshed": False,
            "reason": REFRESH_REASON_ADMIN_NOT_FOUND,
        }

    refreshed, message = post_refresh_request(admin_url)
    return {
        "attempted": True,
        "refreshed": refreshed,
        "admin_url": admin_url,
        "message": message,
    }


def print_install_result(result: dict) -> None:
    """Render the installation result as text and JSON."""
    if not result.get("installed"):
        print(
            "[skipped] An installed skill with the same name already "
            "exists and --keep-existing was set.",
        )
    else:
        print(
            "[success] Installed "
            f"{result.get('skill_name', '-')}"
            " into local skills.",
        )

    print(f"Provider: {result.get('provider', '-')}")
    print(f"Remote ID: {result.get('remote_id', '-')}")
    print(f"Installed dir: {result.get('target_dir', '-')}")
    print(f"Replaced existing: {bool(result.get('replaced'))}")

    refresh = result.get("refresh", {})
    if isinstance(refresh, dict):
        message = str(refresh.get("message") or refresh.get("reason") or "-")
        if refresh.get("refreshed"):
            admin_url = refresh.get("admin_url", "-")
            print(f"Runtime refresh: success via {admin_url}")
            print(f"Refresh detail: {message}")
        elif refresh.get("attempted"):
            print(f"Runtime refresh: failed ({message})")
        else:
            print(f"Runtime refresh: skipped ({message})")

    previous_meta = result.get("previous_meta", {})
    if isinstance(previous_meta, dict) and previous_meta:
        provider = previous_meta.get("provider", "-")
        remote_id = previous_meta.get("remote_id", "-")
        print(
            "Previous source: "
            f"{provider} / {remote_id}",
        )

    print("\n=== JSON_OUTPUT_START ===")
    print(json.dumps(result, ensure_ascii=False, indent=2))
    print("=== JSON_OUTPUT_END ===")


def main() -> int:
    """CLI entry point."""
    parser = argparse.ArgumentParser(
        description="Install a marketplace skill into local skills",
    )
    parser.add_argument(
        "--provider",
        required=True,
        type=parse_provider,
        help="Marketplace provider name",
    )
    parser.add_argument(
        "--remote-id",
        required=True,
        help="Provider-specific remote skill ID",
    )
    parser.add_argument(
        "--install-root",
        default="",
        help="Explicit local skills root",
    )
    parser.add_argument(
        "--keep-existing",
        action="store_true",
        help="Do not overwrite an existing local skill with the same name",
    )
    args = parser.parse_args()

    archive_path = None
    install_root = resolve_install_root(args.install_root)
    try:
        archive_path, canonical_id = download_provider_archive(
            provider=args.provider,
            remote_id=args.remote_id,
        )
        install_result = install_skill_archive(
            archive_path=archive_path,
            install_root=install_root,
            provider=args.provider,
            remote_id=canonical_id,
            replace=not args.keep_existing,
        )
    except RegistryError as err:
        print(f"[error] {err}")
        return 1
    finally:
        if archive_path is not None:
            Path(archive_path).unlink(missing_ok=True)

    install_result["provider"] = args.provider
    install_result["remote_id"] = canonical_id
    install_result["refresh"] = refresh_runtime_skills(
        install_root=install_root,
        installed=bool(install_result.get("installed")),
    )
    print_install_result(install_result)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
