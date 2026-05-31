#!/usr/bin/env python3

from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import shlex
import shutil
import subprocess
import sys
import tarfile
import tempfile
import urllib.request

toolchainDirName = "toolchain"
pythonRootDirName = "python"
binDirName = "bin"
defaultVersion = "latest"
pythonBinaryName = "python3"
trpcClawBinaryName = "trpc-claw"
ffmpegBinaryName = "ffmpeg"
playwrightBrowserName = "chromium"
codexDirName = ".codex"
geminiDirName = ".gemini"


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Install Linux-only bundled skill dependencies.",
    )
    parser.add_argument(
        "--state-dir",
        required=True,
        help="OpenClaw state directory.",
    )
    parser.add_argument(
        "--manifest",
        required=True,
        help="Path to the structured install manifest.",
    )
    return parser.parse_args()


def run_command(
    command: list[str],
    env: dict[str, str] | None = None,
    cwd: Path | None = None,
) -> None:
    print(f"+ {shlex.join(command)}", flush=True)
    subprocess.run(
        command,
        check=True,
        cwd=None if cwd is None else str(cwd),
        env=env,
    )


def read_command(
    command: list[str],
    env: dict[str, str] | None = None,
    cwd: Path | None = None,
) -> str:
    print(f"+ {shlex.join(command)}", flush=True)
    output = subprocess.check_output(
        command,
        cwd=None if cwd is None else str(cwd),
        env=env,
        text=True,
    )
    return output.strip()


def ensure_dir(path: Path) -> None:
    path.mkdir(parents=True, exist_ok=True)


def ensure_dirs(paths: list[str]) -> None:
    for path in paths:
        ensure_dir(Path(path))


def ensure_bins(
    bins: list[str],
    toolchain_bin_dir: Path,
) -> None:
    missing = []
    for binary_name in bins:
        candidate = toolchain_bin_dir / binary_name
        if candidate.exists():
            continue
        if shutil.which(binary_name):
            continue
        missing.append(binary_name)
    if missing:
        raise RuntimeError(
            f"missing expected binaries after install: {', '.join(missing)}",
        )


def bootstrap_plan(state_dir: Path) -> dict[str, object]:
    output = read_command(
        [
            trpcClawBinaryName,
            "bootstrap",
            "deps",
            "--state-dir",
            str(state_dir),
            "--bundled",
            "-json",
        ],
    )
    return json.loads(output)


def run_bootstrap_steps(plan: dict[str, object]) -> None:
    for step in plan.get("steps", []):
        kind = step["kind"]
        if kind == "venv" or kind == "python":
            run_command(list(step["command"]))
            continue
        if kind == "command":
            ensure_dirs(normalize_string_list(step.get("ensure_dirs")))
            env = os.environ.copy()
            env.update(normalize_env(step.get("env")))
            run_command(list(step["command"]), env=env)
            continue
        if kind == "download":
            run_download_step(step)
            continue
        if kind == "system":
            run_system_step(step)
            continue
        raise RuntimeError(f"unsupported bootstrap step kind: {kind}")


def run_download_step(step: dict[str, object]) -> None:
    target_path = Path(str(step["target_path"]))
    ensure_dir(target_path.parent if not step.get("extract") else target_path)
    download_artifact(
        url=str(step["url"]),
        target_path=target_path,
        extract=bool(step.get("extract")),
        strip_components=normalize_strip_components(
            step.get("strip_components"),
        ),
    )


def download_artifact(
    url: str,
    target_path: Path,
    extract: bool,
    strip_components: int,
) -> None:
    with tempfile.TemporaryDirectory() as temp_dir:
        archive_path = Path(temp_dir) / "download.bin"
        with urllib.request.urlopen(url) as response:
            archive_path.write_bytes(response.read())
        if not extract:
            shutil.move(str(archive_path), str(target_path))
            return
        extract_archive(
            archive_path=archive_path,
            target_dir=target_path,
            strip_components=strip_components,
        )


def extract_archive(
    archive_path: Path,
    target_dir: Path,
    strip_components: int,
) -> None:
    with tarfile.open(archive_path, "r:*") as archive:
        for member in archive.getmembers():
            stripped_name = strip_member_name(member.name, strip_components)
            if stripped_name == "":
                continue
            destination = (target_dir / stripped_name).resolve()
            target_root = target_dir.resolve()
            if not str(destination).startswith(f"{target_root}{os.sep}"):
                raise RuntimeError(
                    f"refusing to extract outside target dir: {member.name}",
                )
            if member.isdir():
                ensure_dir(destination)
                continue
            ensure_dir(destination.parent)
            extracted = archive.extractfile(member)
            if extracted is None:
                continue
            with destination.open("wb") as output:
                shutil.copyfileobj(extracted, output)
            if member.mode != 0:
                destination.chmod(member.mode)


def strip_member_name(name: str, strip_components: int) -> str:
    parts = [part for part in name.split("/") if part not in ("", ".")]
    if len(parts) <= strip_components:
        return ""
    return "/".join(parts[strip_components:])


def normalize_strip_components(value: object) -> int:
    if value is None:
        return 0
    return int(value)


def normalize_string_list(value: object) -> list[str]:
    if value is None:
        return []
    return [str(item) for item in value]


def normalize_env(value: object) -> dict[str, str]:
    if value is None:
        return {}
    return {str(key): str(item) for key, item in value.items()}


def run_system_step(step: dict[str, object]) -> None:
    command = list(step["command"])
    packages = packages_from_system_command(command)
    unavailable = [
        package
        for package in packages
        if not system_package_available(command[0], package)
    ]
    if not unavailable:
        run_command(command)
        return
    unresolved = [
        package
        for package in unavailable
        if shutil.which(package) is None
    ]
    if unresolved:
        joined = ", ".join(unresolved)
        raise RuntimeError(
            f"system packages are unavailable and unresolved: {joined}",
        )
    joined = ", ".join(unavailable)
    print(
        f"skip unavailable system packages already satisfied on PATH: {joined}",
        flush=True,
    )


def packages_from_system_command(command: list[str]) -> list[str]:
    packages = []
    package_mode = False
    for token in command[1:]:
        if token == "install":
            package_mode = True
            continue
        if not package_mode:
            continue
        if token.startswith("-"):
            continue
        packages.append(token)
    return packages


def system_package_available(package_manager: str, package: str) -> bool:
    if package_manager not in {"dnf", "yum"}:
        return True
    result = subprocess.run(
        [package_manager, "info", package],
        check=False,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    return result.returncode == 0


def install_pip_packages(
    packages: list[dict[str, object]],
    python_binary: str,
    toolchain_bin_dir: Path,
) -> None:
    for package in packages:
        command = [python_binary, "-m", "pip", "install"]
        command.extend(normalize_string_list(package.get("install_args")))
        command.append(str(package["package"]))
        run_command(
            command,
        )
        ensure_bins(list(package.get("bins", [])), toolchain_bin_dir)


def install_downloaded_binaries(
    items: list[dict[str, object]],
    toolchain_bin_dir: Path,
) -> None:
    for item in items:
        with tempfile.TemporaryDirectory() as temp_dir:
            extract_dir = Path(temp_dir) / "extract"
            ensure_dir(extract_dir)
            download_artifact(
                url=str(item["url"]),
                target_path=extract_dir,
                extract=bool(item.get("extract", True)),
                strip_components=normalize_strip_components(
                    item.get("strip_components"),
                ),
            )
            for file_item in item.get("files") or []:
                source_path = extract_dir / str(file_item["source"])
                if not source_path.exists():
                    raise RuntimeError(
                        f"downloaded file not found: {source_path}",
                    )
                target_name = str(file_item.get("target") or file_item["source"])
                target_path = toolchain_bin_dir / target_name
                shutil.copy2(source_path, target_path)
                target_path.chmod(int(str(file_item.get("mode", "0755")), 8))
            ensure_bins(
                [str(file_item.get("target") or file_item["source"])
                 for file_item in item.get("files") or []],
                toolchain_bin_dir,
            )


def install_npm_packages(
    packages: list[dict[str, object]],
    toolchain_root: Path,
    toolchain_bin_dir: Path,
) -> None:
    for package in packages:
        run_command(
            [
                "npm",
                "install",
                "-g",
                "--prefix",
                str(toolchain_root),
                str(package["package"]),
            ],
        )
        ensure_bins(list(package.get("bins", [])), toolchain_bin_dir)


def install_go_packages(
    packages: list[dict[str, object]],
    toolchain_bin_dir: Path,
) -> None:
    for package in packages:
        git_repo = package.get("git_repo")
        if git_repo is not None:
            install_go_package_from_git(
                package,
                toolchain_bin_dir,
            )
            continue
        version = str(package.get("version", defaultVersion))
        install_target = f"{package['package']}@{version}"
        env = os.environ.copy()
        env["GOBIN"] = str(toolchain_bin_dir)
        run_command(["go", "install", install_target], env=env)
        ensure_bins(list(package.get("bins", [])), toolchain_bin_dir)
        ensure_aliases(
            list(package.get("aliases", [])),
            toolchain_bin_dir,
        )


def install_go_package_from_git(
    package: dict[str, object],
    toolchain_bin_dir: Path,
) -> None:
    git_repo = str(package["git_repo"])
    git_ref = package.get("git_ref")
    install_path = str(package.get("install_path") or ".")

    with tempfile.TemporaryDirectory() as temp_dir:
        repo_dir = Path(temp_dir) / "src"
        clone_command = ["git", "clone"]
        if git_ref is None:
            clone_command.extend(["--depth", "1"])
        clone_command.extend([git_repo, str(repo_dir)])
        run_command(clone_command)
        if git_ref is not None:
            run_command(["git", "checkout", str(git_ref)], cwd=repo_dir)
        env = os.environ.copy()
        env["GOBIN"] = str(toolchain_bin_dir)
        run_command(["go", "install", install_path], env=env, cwd=repo_dir)
        ensure_bins(list(package.get("bins", [])), toolchain_bin_dir)
        ensure_aliases(
            list(package.get("aliases", [])),
            toolchain_bin_dir,
        )


def install_cargo_packages(
    packages: list[dict[str, object]],
    toolchain_root: Path,
    toolchain_bin_dir: Path,
) -> None:
    for package in packages:
        command = [
            "cargo",
            "install",
            "--locked",
            "--root",
            str(toolchain_root),
            str(package["package"]),
        ]
        run_command(command)
        ensure_bins(list(package.get("bins", [])), toolchain_bin_dir)
        ensure_aliases(
            list(package.get("aliases", [])),
            toolchain_bin_dir,
        )


def ensure_aliases(
    aliases: list[dict[str, str]],
    toolchain_bin_dir: Path,
) -> None:
    for alias in aliases:
        alias_path = toolchain_bin_dir / alias["name"]
        target_name = alias["target"]
        target_path = toolchain_bin_dir / target_name
        if not target_path.exists():
            raise RuntimeError(
                f"alias target does not exist: {target_path}",
            )
        if alias_path.is_symlink():
            current_target = os.readlink(alias_path)
            if current_target == target_name:
                continue
            alias_path.unlink()
        elif alias_path.exists():
            alias_path.unlink()
        alias_path.symlink_to(target_name)


def install_playwright_runtime(python_binary: str) -> None:
    run_command(
        [python_binary, "-m", "playwright", "install", playwrightBrowserName],
    )


def install_ffmpeg_wrapper(
    python_binary: str,
    toolchain_bin_dir: Path,
) -> None:
    ffmpeg_link = toolchain_bin_dir / ffmpegBinaryName
    if ffmpeg_link.is_symlink() or ffmpeg_link.exists():
        ffmpeg_link.unlink()
    script = "\n".join(
        [
            "#!/usr/bin/env bash",
            "set -euo pipefail",
            (
                "FFMPEG_BIN=\"$("
                + shlex.quote(python_binary)
                + " -c 'import imageio_ffmpeg;"
                "print(imageio_ffmpeg.get_ffmpeg_exe())')\""
            ),
            "exec \"$FFMPEG_BIN\" \"$@\"",
            "",
        ],
    )
    ffmpeg_link.write_text(script, encoding="utf-8")
    ffmpeg_link.chmod(0o755)


def verify_bundled_deps(state_dir: Path) -> None:
    output = read_command(
        [
            trpcClawBinaryName,
            "inspect",
            "deps",
            "--state-dir",
            str(state_dir),
            "--bundled",
            "-json",
        ],
    )
    payload = json.loads(output)
    failures = collect_missing_failures(payload.get("missing"))
    if failures:
        joined = "\n".join(failures)
        raise RuntimeError(
            f"bundled skill dependencies are still unresolved:\n{joined}",
        )


def collect_missing_failures(missing: object) -> list[str]:
    if missing is None:
        return []
    if isinstance(missing, dict):
        return collect_aggregate_missing_failures(missing)
    if isinstance(missing, list):
        return collect_skill_missing_failures(missing)
    raise RuntimeError(
        f"unsupported inspect deps missing payload type: {type(missing)!r}",
    )


def collect_aggregate_missing_failures(
    missing: dict[str, object],
) -> list[str]:
    failures: list[str] = []
    missing_bins = normalize_string_list(missing.get("bins"))
    if missing_bins:
        joined = ", ".join(missing_bins)
        failures.append(f"missing bins: {joined}")
    missing_python = []
    for item in missing.get("python") or []:
        module_name = item.get("module") or item.get("package")
        missing_python.append(str(module_name or "python-package"))
    if missing_python:
        joined = ", ".join(missing_python)
        failures.append(f"missing python: {joined}")
    for group in missing.get("any_bins") or []:
        names = normalize_string_list(group)
        if not names:
            continue
        joined = ", ".join(names)
        failures.append(f"missing any-of binaries: {joined}")
    return failures


def collect_skill_missing_failures(
    missing: list[dict[str, object]],
) -> list[str]:
    failures: list[str] = []
    for skill in missing:
        skill_name = str(skill.get("name") or "unknown-skill")
        missing_bins = [
            item["name"]
            for item in skill.get("bins", [])
            if not item.get("found")
        ]
        if missing_bins:
            joined = ", ".join(missing_bins)
            failures.append(f"{skill_name}: missing bins {joined}")
        missing_python = [
            item.get("module") or item.get("package") or "python-package"
            for item in skill.get("python", [])
            if not item.get("found")
        ]
        if missing_python:
            joined = ", ".join(str(item) for item in missing_python)
            failures.append(f"{skill_name}: missing python {joined}")
        for group in skill.get("any_bins", []):
            if any(item.get("found") for item in group):
                continue
            joined = ", ".join(item["name"] for item in group)
            failures.append(
                f"{skill_name}: missing any-of binaries {joined}",
            )
    return failures


def main() -> int:
    args = parse_args()
    state_dir = Path(args.state_dir).resolve()
    manifest_path = Path(args.manifest).resolve()
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))

    toolchain_root = state_dir / toolchainDirName / pythonRootDirName
    toolchain_bin_dir = toolchain_root / binDirName
    python_binary = str(toolchain_bin_dir / pythonBinaryName)

    ensure_dir(toolchain_root)
    ensure_dir(toolchain_bin_dir)
    ensure_dir(Path.home() / codexDirName)
    ensure_dir(Path.home() / geminiDirName)

    install_downloaded_binaries(
        list(manifest.get("downloaded_binaries", [])),
        toolchain_bin_dir,
    )
    run_bootstrap_steps(bootstrap_plan(state_dir))
    install_pip_packages(
        list(manifest.get("pip_packages", [])),
        python_binary,
        toolchain_bin_dir,
    )
    install_npm_packages(
        list(manifest.get("npm_packages", [])),
        toolchain_root,
        toolchain_bin_dir,
    )
    install_go_packages(
        list(manifest.get("go_packages", [])),
        toolchain_bin_dir,
    )
    install_cargo_packages(
        list(manifest.get("cargo_packages", [])),
        toolchain_root,
        toolchain_bin_dir,
    )
    install_playwright_runtime(python_binary)
    install_ffmpeg_wrapper(python_binary, toolchain_bin_dir)
    verify_bundled_deps(state_dir)
    return 0


if __name__ == "__main__":
    sys.exit(main())
