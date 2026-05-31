#!/usr/bin/env python3
"""
Shared helpers for marketplace-backed skill discovery and installation.
"""

from __future__ import annotations

import json
import os
import re
import shutil
import tempfile
import urllib.error
import urllib.request
import zipfile
from dataclasses import asdict, dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Mapping

DEFAULT_TIMEOUT_SECONDS = 30
SKILL_DOC_FILE_NAME = "SKILL.md"
PACKAGE_METADATA_FILE_NAME = "_meta.json"
REGISTRY_METADATA_FILE_NAME = "_registry.json"
TRPC_CLAW_STATE_DIR_ENV_NAME = "TRPC_CLAW_STATE_DIR"
DEFAULT_HOME_STATE_DIR = (
    Path.home() / ".trpc-agent-go" / "openclaw"
)
LOCAL_SKILLS_RELATIVE_DIR = Path("skills") / "local"
SKILL_NAME_PATTERN = re.compile(
    r"^name:\s*['\"]?([A-Za-z0-9_-]+(?:-[A-Za-z0-9_-]+)*)['\"]?\s*$",
    re.MULTILINE,
)


class RegistryError(RuntimeError):
    """Raised when registry search or installation fails."""


@dataclass
class SearchResult:
    """Normalized search result across multiple marketplaces."""

    provider: str
    remote_id: str
    name: str
    description: str
    version: str = ""
    homepage: str = ""
    downloads: int = 0
    install_command: str = ""

    def to_dict(self) -> dict[str, Any]:
        """Return a JSON-friendly representation."""
        return asdict(self)


def read_env(
    name: str,
    env: Mapping[str, str] | None = None,
) -> str:
    """Return a trimmed environment variable value."""
    source = os.environ if env is None else env
    return source.get(name, "").strip()


def resolve_install_root(
    install_root: str = "",
    env: Mapping[str, str] | None = None,
) -> Path:
    """Resolve the managed local skill root."""
    explicit_root = install_root.strip()
    if explicit_root:
        return Path(explicit_root).expanduser().resolve()

    state_dir = read_env(TRPC_CLAW_STATE_DIR_ENV_NAME, env)
    if state_dir:
        return (
            Path(state_dir).expanduser().resolve() /
            LOCAL_SKILLS_RELATIVE_DIR
        )

    return (DEFAULT_HOME_STATE_DIR / LOCAL_SKILLS_RELATIVE_DIR).resolve()


def json_request(
    url: str,
    payload: Any,
    headers: Mapping[str, str] | None = None,
    timeout: int = DEFAULT_TIMEOUT_SECONDS,
) -> Any:
    """Send a JSON POST request and decode the JSON response."""
    request_headers = {
        "Content-Type": "application/json",
    }
    if headers:
        request_headers.update(headers)

    request = urllib.request.Request(
        url,
        data=json.dumps(payload).encode("utf-8"),
        method="POST",
        headers=request_headers,
    )

    try:
        with urllib.request.urlopen(request, timeout=timeout) as response:
            raw = response.read().decode("utf-8")
    except urllib.error.HTTPError as err:
        body = err.read().decode("utf-8", errors="replace")
        raise RegistryError(
            f"HTTP {err.code} from {url}: {body}",
        ) from err
    except urllib.error.URLError as err:
        raise RegistryError(
            f"failed to reach {url}: {err.reason}",
        ) from err

    try:
        return json.loads(raw)
    except json.JSONDecodeError as err:
        raise RegistryError(
            f"failed to parse JSON response from {url}: {err}",
        ) from err


def read_json_url(
    url: str,
    timeout: int = DEFAULT_TIMEOUT_SECONDS,
) -> Any:
    """Read a JSON document from a URL."""
    try:
        with urllib.request.urlopen(url, timeout=timeout) as response:
            raw = response.read().decode("utf-8")
    except urllib.error.HTTPError as err:
        body = err.read().decode("utf-8", errors="replace")
        raise RegistryError(
            f"HTTP {err.code} from {url}: {body}",
        ) from err
    except urllib.error.URLError as err:
        raise RegistryError(
            f"failed to reach {url}: {err.reason}",
        ) from err

    try:
        return json.loads(raw)
    except json.JSONDecodeError as err:
        raise RegistryError(
            f"failed to parse JSON response from {url}: {err}",
        ) from err


def download_archive(
    url: str,
    timeout: int = DEFAULT_TIMEOUT_SECONDS,
) -> Path:
    """Download a zip archive to a temporary file."""
    file_handle = tempfile.NamedTemporaryFile(
        prefix="registry_skill_",
        suffix=".zip",
        delete=False,
    )
    file_handle.close()
    archive_path = Path(file_handle.name)

    try:
        with urllib.request.urlopen(url, timeout=timeout) as response:
            archive_path.write_bytes(response.read())
    except OSError as err:
        archive_path.unlink(missing_ok=True)
        raise RegistryError(
            f"failed to download archive from {url}: {err}",
        ) from err

    return archive_path


def _safe_zip_target(
    base_dir: Path,
    archive_name: str,
) -> Path:
    relative_path = Path(archive_name)
    if relative_path.is_absolute():
        raise RegistryError("archive contains absolute paths")
    if ".." in relative_path.parts:
        raise RegistryError(
            "archive contains unsafe parent-directory paths",
        )

    target_path = (base_dir / relative_path).resolve()
    base_path = base_dir.resolve()
    if os.path.commonpath(
        [str(base_path), str(target_path)],
    ) != str(base_path):
        raise RegistryError("archive escapes extraction root")
    return target_path


def extract_archive(
    archive_path: str | Path,
    extract_root: str | Path,
) -> Path:
    """Extract a zip archive and return the extracted root."""
    extract_path = Path(extract_root).expanduser().resolve()
    extract_path.mkdir(parents=True, exist_ok=True)

    with zipfile.ZipFile(archive_path) as archive:
        entries = archive.infolist()
        if not entries:
            raise RegistryError("downloaded archive was empty")

        for entry in entries:
            target = _safe_zip_target(extract_path, entry.filename)
            if entry.is_dir():
                target.mkdir(parents=True, exist_ok=True)
                continue

            target.parent.mkdir(parents=True, exist_ok=True)
            with archive.open(entry) as source, open(
                target,
                "wb",
            ) as destination:
                shutil.copyfileobj(source, destination)

            mode = entry.external_attr >> 16
            if mode:
                os.chmod(target, mode)

    return extract_path


def find_skill_root(extract_root: str | Path) -> Path:
    """Locate the directory that contains SKILL.md."""
    root = Path(extract_root).expanduser().resolve()
    direct_doc = root / SKILL_DOC_FILE_NAME
    if direct_doc.is_file():
        return root

    candidates = sorted(
        {
            doc.parent.resolve()
            for doc in root.rglob(SKILL_DOC_FILE_NAME)
            if doc.is_file()
        },
    )
    if not candidates:
        raise RegistryError(
            "downloaded archive did not include SKILL.md",
        )
    if len(candidates) > 1:
        raise RegistryError(
            "downloaded archive contained multiple skill directories",
        )
    return candidates[0]


def read_skill_name_from_doc(skill_root: str | Path) -> str:
    """Read the runtime skill name from SKILL.md frontmatter."""
    doc_path = Path(skill_root) / SKILL_DOC_FILE_NAME
    try:
        content = doc_path.read_text(encoding="utf-8")
    except OSError as err:
        raise RegistryError(
            f"failed to read {doc_path}: {err}",
        ) from err

    sections = content.split("---", 2)
    if len(sections) < 3:
        raise RegistryError(
            f"{doc_path} does not contain valid frontmatter",
        )

    match = SKILL_NAME_PATTERN.search(sections[1])
    if not match:
        raise RegistryError(
            f"{doc_path} does not define a skill name",
        )
    return match.group(1).strip()


def read_package_meta(skill_root: str | Path) -> dict[str, Any]:
    """Read optional package metadata from the extracted skill root."""
    meta_path = Path(skill_root) / PACKAGE_METADATA_FILE_NAME
    if not meta_path.exists():
        return {}
    try:
        raw = json.loads(meta_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return {}
    return raw if isinstance(raw, dict) else {}


def read_installed_registry_meta(
    target_dir: str | Path,
) -> dict[str, Any]:
    """Read the installed registry metadata, if any."""
    meta_path = Path(target_dir) / REGISTRY_METADATA_FILE_NAME
    if not meta_path.exists():
        return {}
    try:
        raw = json.loads(meta_path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return {}
    return raw if isinstance(raw, dict) else {}


def remove_existing_path(path: Path) -> None:
    """Delete an existing file or directory path."""
    if not path.exists():
        return
    if path.is_dir() and not path.is_symlink():
        shutil.rmtree(path)
        return
    path.unlink()


def write_registry_metadata(
    target_dir: str | Path,
    provider: str,
    remote_id: str,
    version: str,
) -> dict[str, Any]:
    """Write provider provenance for an installed local skill."""
    payload = {
        "provider": provider,
        "remote_id": remote_id,
        "version": version,
        "installed_at": datetime.now(
            timezone.utc,
        ).isoformat(),
    }
    target_path = Path(target_dir) / REGISTRY_METADATA_FILE_NAME
    target_path.write_text(
        json.dumps(payload, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )
    return payload


def install_skill_archive(
    archive_path: str | Path,
    install_root: str | Path,
    provider: str,
    remote_id: str,
    replace: bool = True,
) -> dict[str, Any]:
    """Install an archive into the flat local skill directory."""
    install_root_path = Path(install_root).expanduser().resolve()
    install_root_path.mkdir(parents=True, exist_ok=True)

    staging_root = Path(
        tempfile.mkdtemp(prefix="marketplace_skill_install_"),
    )
    try:
        extracted_root = extract_archive(archive_path, staging_root)
        skill_root = find_skill_root(extracted_root)
        skill_name = read_skill_name_from_doc(skill_root)
        package_meta = read_package_meta(skill_root)
        target_dir = install_root_path / skill_name
        previous_meta = read_installed_registry_meta(target_dir)

        replaced = False
        if target_dir.exists():
            if not replace:
                return {
                    "installed": False,
                    "replaced": False,
                    "target_dir": str(target_dir),
                    "skill_name": skill_name,
                    "previous_meta": previous_meta,
                }
            remove_existing_path(target_dir)
            replaced = True

        shutil.copytree(skill_root, target_dir)
        registry_meta = write_registry_metadata(
            target_dir=target_dir,
            provider=provider,
            remote_id=remote_id,
            version=str(package_meta.get("version", "")).strip(),
        )
        return {
            "installed": True,
            "replaced": replaced,
            "target_dir": str(target_dir),
            "skill_name": skill_name,
            "package_meta": package_meta,
            "registry_meta": registry_meta,
            "previous_meta": previous_meta,
        }
    finally:
        shutil.rmtree(staging_root, ignore_errors=True)
