#!/usr/bin/env python3
"""
Unit tests for install_skill helpers.
"""

from __future__ import annotations

import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

SCRIPT_DIR = Path(__file__).resolve().parent
if str(SCRIPT_DIR) not in sys.path:
    sys.path.insert(0, str(SCRIPT_DIR))

import install_skill


class _FakeHTTPResponse:
    """Minimal urlopen response stub."""

    def __init__(self, url: str) -> None:
        self._url = url

    def __enter__(self) -> "_FakeHTTPResponse":
        return self

    def __exit__(self, *args: object) -> bool:
        return False

    def geturl(self) -> str:
        return self._url


class InstallSkillTest(unittest.TestCase):
    def test_parse_admin_runtime_config_reads_admin_block(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            config_path = Path(temp_dir) / "openclaw.yaml"
            config_path.write_text(
                "admin:\n"
                "  enabled: false\n"
                "  addr: ':21000'\n"
                "  auto_port: false\n",
                encoding="utf-8",
            )

            parsed = install_skill.parse_admin_runtime_config(config_path)

            self.assertFalse(parsed["enabled"])
            self.assertEqual(parsed["addr"], ":21000")
            self.assertFalse(parsed["auto_port"])

    def test_discover_admin_url_matches_state_dir(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            state_dir = Path(temp_dir) / "state"
            state_dir.mkdir()
            config_path = state_dir / "openclaw.yaml"
            config_path.write_text(
                "admin:\n"
                "  enabled: true\n"
                "  addr: '127.0.0.1:21000'\n"
                "  auto_port: true\n",
                encoding="utf-8",
            )

            def fake_read_json_url(url: str, timeout: int) -> object:
                del timeout
                if url == (
                    "http://127.0.0.1:21000"
                    + install_skill.ADMIN_STATUS_PATH
                ):
                    return {
                        "app_name": install_skill.OPENCLAW_APP_NAME,
                        "state_dir": str(Path(temp_dir) / "other"),
                    }
                if url == (
                    "http://127.0.0.1:21001"
                    + install_skill.ADMIN_STATUS_PATH
                ):
                    return {
                        "app_name": install_skill.OPENCLAW_APP_NAME,
                        "state_dir": str(state_dir),
                        "admin_url": "http://127.0.0.1:21001",
                    }
                raise install_skill.RegistryError("missing")

            with mock.patch.dict(
                "os.environ",
                {
                    install_skill.OPENCLAW_CONFIG_ENV_NAME: str(
                        config_path,
                    ),
                },
                clear=False,
            ):
                with mock.patch.object(
                    install_skill,
                    "read_json_url",
                    side_effect=fake_read_json_url,
                ):
                    admin_url = install_skill.discover_admin_url(state_dir)

            self.assertEqual(admin_url, "http://127.0.0.1:21001")

    def test_refresh_runtime_skills_posts_refresh_when_admin_found(
        self,
    ) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            state_dir = Path(temp_dir) / "state"
            install_root = state_dir / "skills" / "local"
            install_root.mkdir(parents=True)

            config_path = state_dir / "openclaw.yaml"
            config_path.write_text(
                "admin:\n"
                "  enabled: true\n"
                "  addr: '127.0.0.1:21002'\n"
                "  auto_port: false\n",
                encoding="utf-8",
            )

            status_doc = {
                "app_name": install_skill.OPENCLAW_APP_NAME,
                "state_dir": str(state_dir),
                "admin_url": "http://127.0.0.1:21002",
            }

            with mock.patch.dict(
                "os.environ",
                {
                    install_skill.OPENCLAW_CONFIG_ENV_NAME: str(
                        config_path,
                    ),
                },
                clear=False,
            ):
                with mock.patch.object(
                    install_skill,
                    "read_json_url",
                    return_value=status_doc,
                ):
                    with mock.patch(
                        "install_skill.urllib.request.urlopen",
                        return_value=_FakeHTTPResponse(
                            "http://127.0.0.1:21002/skills"
                            "?notice=Refreshed+skills",
                        ),
                    ) as mocked_urlopen:
                        result = install_skill.refresh_runtime_skills(
                            install_root=install_root,
                            installed=True,
                        )

            self.assertTrue(result["attempted"])
            self.assertTrue(result["refreshed"])
            self.assertEqual(
                result["admin_url"],
                "http://127.0.0.1:21002",
            )
            mocked_urlopen.assert_called_once()

    def test_refresh_runtime_skills_skips_unmanaged_root(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            install_root = Path(temp_dir) / "custom-root"
            install_root.mkdir()

            result = install_skill.refresh_runtime_skills(
                install_root=install_root,
                installed=True,
            )

            self.assertFalse(result["attempted"])
            self.assertFalse(result["refreshed"])
            self.assertEqual(
                result["reason"],
                install_skill.REFRESH_REASON_UNMANAGED_ROOT,
            )


if __name__ == "__main__":
    unittest.main()
