#!/usr/bin/env python3
"""
Unit tests for find-skills helpers.
"""

from __future__ import annotations

import json
import sys
import tempfile
import unittest
import zipfile
from pathlib import Path

SCRIPT_DIR = Path(__file__).resolve().parent
if str(SCRIPT_DIR) not in sys.path:
    sys.path.insert(0, str(SCRIPT_DIR))

from providers import (
    KNOT_PROVIDER,
    SKILLHUB_PROVIDER,
    normalize_knot_result,
    normalize_skillhub_result,
)
from registry_common import (
    PACKAGE_METADATA_FILE_NAME,
    REGISTRY_METADATA_FILE_NAME,
    find_skill_root,
    install_skill_archive,
    read_package_meta,
    read_installed_registry_meta,
    read_skill_name_from_doc,
    resolve_install_root,
)


class RegistryCommonTest(unittest.TestCase):
    def make_zip(
        self,
        root: Path,
        entries: dict[str, str],
    ) -> Path:
        """Create a temporary zip file."""
        archive_path = root / "skill.zip"
        with zipfile.ZipFile(archive_path, "w") as archive:
            for name, content in entries.items():
                archive.writestr(name, content)
        return archive_path

    def test_resolve_install_root_prefers_explicit_root(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            root = resolve_install_root(
                install_root=temp_dir,
            )
            self.assertEqual(root, Path(temp_dir).resolve())

    def test_resolve_install_root_uses_state_dir(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            root = resolve_install_root(
                env={"TRPC_CLAW_STATE_DIR": temp_dir},
            )
            self.assertEqual(
                root,
                Path(temp_dir).resolve() / "skills" / "local",
            )

    def test_find_skill_root_supports_flat_archive(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            root = Path(temp_dir)
            (root / "SKILL.md").write_text(
                "---\nname: demo\ndescription: x\n---\n",
                encoding="utf-8",
            )
            self.assertEqual(find_skill_root(root), root.resolve())

    def test_read_skill_name_from_doc(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            root = Path(temp_dir)
            (root / "SKILL.md").write_text(
                "---\nname: demo-skill\ndescription: x\n---\n",
                encoding="utf-8",
            )
            self.assertEqual(
                read_skill_name_from_doc(root),
                "demo-skill",
            )

    def test_read_package_meta(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            root = Path(temp_dir)
            (root / PACKAGE_METADATA_FILE_NAME).write_text(
                '{"version":"1.2.3","slug":"demo"}',
                encoding="utf-8",
            )
            self.assertEqual(
                read_package_meta(root).get("version"),
                "1.2.3",
            )

    def test_install_skill_archive_replaces_existing(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            root = Path(temp_dir)
            archive_path = self.make_zip(
                root,
                {
                    "SKILL.md": (
                        "---\nname: demo\ndescription: hello\n---\n"
                    ),
                    "_meta.json": '{"version":"1.0.0"}',
                },
            )
            install_root = root / "local"
            install_root.mkdir()
            target = install_root / "demo"
            target.mkdir()
            (target / "SKILL.md").write_text(
                "---\nname: demo\ndescription: old\n---\n",
                encoding="utf-8",
            )
            (target / REGISTRY_METADATA_FILE_NAME).write_text(
                json.dumps(
                    {
                        "provider": KNOT_PROVIDER,
                        "remote_id": "16",
                    },
                ),
                encoding="utf-8",
            )

            result = install_skill_archive(
                archive_path=archive_path,
                install_root=install_root,
                provider=SKILLHUB_PROVIDER,
                remote_id="demo",
                replace=True,
            )

            self.assertTrue(result["installed"])
            self.assertTrue(result["replaced"])
            self.assertEqual(result["skill_name"], "demo")
            registry_meta = read_installed_registry_meta(target)
            self.assertEqual(
                registry_meta.get("provider"),
                SKILLHUB_PROVIDER,
            )
            self.assertEqual(
                registry_meta.get("remote_id"),
                "demo",
            )
            self.assertEqual(
                registry_meta.get("version"),
                "1.0.0",
            )

    def test_install_skill_archive_keeps_existing(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            root = Path(temp_dir)
            archive_path = self.make_zip(
                root,
                {
                    "skill/SKILL.md": (
                        "---\nname: demo\ndescription: hello\n---\n"
                    ),
                },
            )
            install_root = root / "local"
            install_root.mkdir()
            target = install_root / "demo"
            target.mkdir()
            (target / "SKILL.md").write_text(
                "---\nname: demo\ndescription: old\n---\n",
                encoding="utf-8",
            )

            result = install_skill_archive(
                archive_path=archive_path,
                install_root=install_root,
                provider=SKILLHUB_PROVIDER,
                remote_id="demo",
                replace=False,
            )

            self.assertFalse(result["installed"])
            self.assertFalse(result["replaced"])
            self.assertEqual(result["skill_name"], "demo")

    def test_install_skill_archive_returns_previous_meta_when_skipped(
        self,
    ) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            root = Path(temp_dir)
            archive_path = self.make_zip(
                root,
                {
                    "skill/SKILL.md": (
                        "---\nname: demo\ndescription: hello\n---\n"
                    ),
                },
            )
            install_root = root / "local"
            install_root.mkdir()
            target = install_root / "demo"
            target.mkdir()
            (target / "SKILL.md").write_text(
                "---\nname: demo\ndescription: old\n---\n",
                encoding="utf-8",
            )
            (target / REGISTRY_METADATA_FILE_NAME).write_text(
                json.dumps(
                    {
                        "provider": KNOT_PROVIDER,
                        "remote_id": "16",
                        "version": "9.9.9",
                    },
                ),
                encoding="utf-8",
            )

            result = install_skill_archive(
                archive_path=archive_path,
                install_root=install_root,
                provider=SKILLHUB_PROVIDER,
                remote_id="demo",
                replace=False,
            )

            self.assertFalse(result["installed"])
            self.assertEqual(
                result["previous_meta"].get("provider"),
                KNOT_PROVIDER,
            )

    def test_normalize_skillhub_result(self) -> None:
        result = normalize_skillhub_result(
            {
                "slug": "pdf",
                "displayName": "Pdf",
                "summary": "A PDF skill",
                "version": "1.0.0",
            },
        )
        self.assertEqual(result.provider, SKILLHUB_PROVIDER)
        self.assertEqual(result.remote_id, "pdf")
        self.assertEqual(result.name, "Pdf")

    def test_normalize_knot_result(self) -> None:
        result = normalize_knot_result(
            {
                "id": 16,
                "display_name": "Pdf",
                "description": "A Knot PDF skill",
                "download_count": 12,
            },
        )
        self.assertEqual(result.provider, KNOT_PROVIDER)
        self.assertEqual(result.remote_id, "16")
        self.assertEqual(result.name, "Pdf")


if __name__ == "__main__":
    unittest.main()
