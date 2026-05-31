#!/usr/bin/env python3
"""
Marketplace providers for find-skills.
"""

from __future__ import annotations

import json
import urllib.parse
from typing import Any, Mapping

from registry_common import (
    DEFAULT_TIMEOUT_SECONDS,
    RegistryError,
    SearchResult,
    download_archive,
    json_request,
    read_env,
    read_json_url,
)

SKILLHUB_PROVIDER = "skillhub"
KNOT_PROVIDER = "knot"

PROVIDER_PRIORITY = (
    SKILLHUB_PROVIDER,
    KNOT_PROVIDER,
)

SKILLHUB_TOP_URL = "https://lightmake.site/api/skills/top"
SKILLHUB_SEARCH_URL = "https://lightmake.site/api/v1/search"
SKILLHUB_DOWNLOAD_URL_FMT = (
    "https://lightmake.site/api/v1/download?slug={remote_id}"
)

KNOT_USERNAME_ENV_NAME = "KNOT_USERNAME"
KNOT_API_TOKEN_ENV_NAME = "KNOT_API_TOKEN"
KNOT_JWT_TOKEN_ENV_NAME = "KNOT_JWT_TOKEN"
KNOT_MANAGER_BASE_URL = (
    "https://knot.woa.com/rag/trpc.rag_flow.mcp_manager.MCPManager"
)
KNOT_SEARCH_URL = "https://knot.woa.com/apigw/openapi/v1/skills/get"
KNOT_TOKEN_EXCHANGE_URL = (
    "https://knot.woa.com/apigw/api/v1/mcpport/get_config"
)
KNOT_API_TOKEN_HEADER = "x-knot-api-token"
KNOT_USERNAME_HEADER = "X-Username"
KNOT_DOWNLOAD_METHOD = "/DownloadSkill"
KNOT_DEFAULT_PAGE_SIZE = 20
KNOT_JWT_SEGMENT_COUNT = 3


class ProviderUnavailableError(RegistryError):
    """Raised when a provider cannot be used in the current env."""


def build_install_command(
    provider: str,
    remote_id: str,
) -> str:
    """Return the install command for a search result."""
    return (
        "python3 scripts/install_skill.py "
        f"--provider {provider} --remote-id {remote_id}"
    )


def normalize_skillhub_result(
    item: Mapping[str, Any],
) -> SearchResult:
    """Normalize a SkillHub search/top item."""
    remote_id = str(item.get("slug", "")).strip()
    name = str(
        item.get("displayName") or item.get("name") or remote_id,
    ).strip()
    description = str(
        item.get("summary") or item.get("description") or "",
    ).strip()
    version = str(item.get("version", "")).strip()
    homepage = str(item.get("homepage", "")).strip()
    downloads = int(item.get("downloads") or 0)
    return SearchResult(
        provider=SKILLHUB_PROVIDER,
        remote_id=remote_id,
        name=name,
        description=description,
        version=version,
        homepage=homepage,
        downloads=downloads,
        install_command=build_install_command(
            SKILLHUB_PROVIDER,
            remote_id,
        ),
    )


def normalize_knot_result(
    item: Mapping[str, Any],
) -> SearchResult:
    """Normalize a Knot marketplace item."""
    remote_id = str(item.get("id", "")).strip()
    name = str(
        item.get("display_name") or
        item.get("skill_name") or
        remote_id
    ).strip()
    description = str(item.get("description", "")).strip()
    version = str(item.get("version", "")).strip()
    homepage = str(item.get("homepage", "")).strip()
    downloads = int(item.get("download_count") or 0)
    return SearchResult(
        provider=KNOT_PROVIDER,
        remote_id=remote_id,
        name=name,
        description=description,
        version=version,
        homepage=homepage,
        downloads=downloads,
        install_command=build_install_command(
            KNOT_PROVIDER,
            remote_id,
        ),
    )


def search_skillhub(
    query: str,
    limit: int,
) -> list[SearchResult]:
    """Search SkillHub, or list top skills when query is empty."""
    query = query.strip()
    if query:
        params = urllib.parse.urlencode(
            {
                "q": query,
                "limit": str(limit),
            },
        )
        payload = read_json_url(
            f"{SKILLHUB_SEARCH_URL}?{params}",
        )
        results = payload.get("results", [])
        if not isinstance(results, list):
            return []
        return [
            normalize_skillhub_result(item)
            for item in results[:limit]
            if isinstance(item, Mapping)
        ]

    payload = read_json_url(SKILLHUB_TOP_URL)
    data = payload.get("data", {})
    skills = data.get("skills", []) if isinstance(data, dict) else []
    if not isinstance(skills, list):
        return []
    return [
        normalize_skillhub_result(item)
        for item in skills[:limit]
        if isinstance(item, Mapping)
    ]


def looks_like_jwt(token: str) -> bool:
    """Return whether a token has a JWT-like shape."""
    segments = [part for part in token.strip().split(".") if part]
    return len(segments) == KNOT_JWT_SEGMENT_COUNT


def resolve_knot_username(
    env: Mapping[str, str] | None = None,
) -> str:
    """Resolve the Knot username or raise ProviderUnavailableError."""
    username = read_env(KNOT_USERNAME_ENV_NAME, env)
    if username:
        return username
    raise ProviderUnavailableError(
        "missing KNOT_USERNAME",
    )


def exchange_knot_jwt(
    jwt_token: str,
    username: str,
) -> str:
    """Exchange a Knot JWT for an API token."""
    response = json_request(
        KNOT_TOKEN_EXCHANGE_URL,
        {
            "jwt_token": jwt_token,
            "for_knot_api_token": True,
        },
        {
            KNOT_USERNAME_HEADER: username,
        },
    )
    if response.get("code") != 0:
        raise RegistryError(
            "failed to exchange Knot JWT for API token: "
            f"code={response.get('code')}, "
            f"msg={response.get('msg')}",
        )

    data = response.get("data", {})
    if not isinstance(data, dict):
        raise RegistryError(
            "Knot token exchange returned invalid payload",
        )
    token = str(data.get("knot_api_token", "")).strip()
    if not token:
        raise RegistryError(
            "Knot token exchange returned an empty API token",
        )
    return token


def resolve_knot_api_token(
    username: str,
    env: Mapping[str, str] | None = None,
) -> str:
    """Resolve Knot API credentials or raise ProviderUnavailableError."""
    direct_token = read_env(KNOT_API_TOKEN_ENV_NAME, env)
    if direct_token:
        return direct_token

    legacy_token = read_env(KNOT_JWT_TOKEN_ENV_NAME, env)
    if not legacy_token:
        raise ProviderUnavailableError(
            "missing KNOT_API_TOKEN or KNOT_JWT_TOKEN",
        )

    if not looks_like_jwt(legacy_token):
        return legacy_token
    return exchange_knot_jwt(legacy_token, username)


def build_knot_headers(
    token: str,
    username: str,
) -> dict[str, str]:
    """Build Knot authentication headers."""
    return {
        KNOT_API_TOKEN_HEADER: token,
        KNOT_USERNAME_HEADER: username,
    }


def search_knot(
    query: str,
    limit: int,
    env: Mapping[str, str] | None = None,
) -> list[SearchResult]:
    """Search the Knot marketplace."""
    username = resolve_knot_username(env)
    token = resolve_knot_api_token(username, env)
    payload = {
        "keyword": query.strip(),
        "category": "",
        "page_num": 1,
        "page_size": max(1, min(limit, KNOT_DEFAULT_PAGE_SIZE)),
        "order_by": "download_count",
    }
    response = json_request(
        KNOT_SEARCH_URL,
        payload,
        build_knot_headers(token, username),
    )
    if response.get("code") != 0:
        raise RegistryError(
            "Knot search failed: "
            f"code={response.get('code')}, "
            f"msg={response.get('msg')}",
        )

    data = response.get("data", {})
    skills = data.get("list", []) if isinstance(data, dict) else []
    if not isinstance(skills, list):
        return []
    return [
        normalize_knot_result(item)
        for item in skills[:limit]
        if isinstance(item, Mapping)
    ]


def search_all_providers(
    query: str,
    limit: int,
    env: Mapping[str, str] | None = None,
) -> dict[str, Any]:
    """Search all providers in priority order."""
    results: list[SearchResult] = []
    used: list[str] = []
    skipped: list[dict[str, str]] = []

    for provider in PROVIDER_PRIORITY:
        try:
            if provider == SKILLHUB_PROVIDER:
                provider_results = search_skillhub(query, limit)
            elif provider == KNOT_PROVIDER:
                provider_results = search_knot(query, limit, env)
            else:
                continue
        except ProviderUnavailableError as err:
            skipped.append(
                {
                    "provider": provider,
                    "reason": str(err),
                },
            )
            continue

        used.append(provider)
        results.extend(provider_results)

    return {
        "query": query.strip(),
        "providers_used": used,
        "providers_skipped": skipped,
        "results": [item.to_dict() for item in results],
    }


def resolve_download_url(
    provider: str,
    remote_id: str,
    env: Mapping[str, str] | None = None,
) -> tuple[str, str]:
    """Resolve the archive download URL for a provider result."""
    clean_id = remote_id.strip()
    if provider == SKILLHUB_PROVIDER:
        url = SKILLHUB_DOWNLOAD_URL_FMT.format(remote_id=clean_id)
        return url, clean_id

    if provider != KNOT_PROVIDER:
        raise RegistryError(f"unsupported provider: {provider}")

    username = resolve_knot_username(env)
    token = resolve_knot_api_token(username, env)
    response = json_request(
        f"{KNOT_MANAGER_BASE_URL}{KNOT_DOWNLOAD_METHOD}",
        {
            "id": clean_id,
        },
        build_knot_headers(token, username),
    )
    code = response.get("code")
    if code not in (None, 0, 200):
        raise RegistryError(
            "Knot download preparation failed: "
            f"code={code}, msg={response.get('msg')}",
        )
    data = response.get("data", {})
    if not isinstance(data, dict):
        raise RegistryError(
            "Knot download payload was not a JSON object",
        )
    archive_url = str(data.get("file_url", "")).strip()
    if not archive_url:
        raise RegistryError(
            "Knot did not return a downloadable archive URL",
        )
    return archive_url, clean_id


def download_provider_archive(
    provider: str,
    remote_id: str,
    env: Mapping[str, str] | None = None,
) -> tuple[Any, str]:
    """Download a provider archive and return the temp path."""
    archive_url, canonical_id = resolve_download_url(
        provider=provider,
        remote_id=remote_id,
        env=env,
    )
    archive_path = download_archive(
        archive_url,
        timeout=DEFAULT_TIMEOUT_SECONDS,
    )
    return archive_path, canonical_id
