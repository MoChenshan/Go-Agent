#!/usr/bin/env python3
from __future__ import annotations

import argparse
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import urllib.error
import urllib.request


STATE_DIR_ENV = "TRPC_CLAW_STATE_DIR"
TOOLCHAIN_PYTHON_ENV = "OPENCLAW_TOOLCHAIN_PYTHON"
PATH_ENV = "PATH"
PIP_DISABLE_ENV = "PIP_DISABLE_PIP_VERSION_CHECK"
PIP_DISABLE_VALUE = "1"

TOOLCHAIN_DIR = "toolchain"
PYTHON_ENV_DIR = "python"
BIN_DIR = "bin"
FONTS_DIR = "fonts"
TESSDATA_DIR = "tessdata"
PREVIEW_PREFIX = "preview"
TEXT_SAMPLE_LIMIT = 400
UTF8_ENCODING = "utf-8"
DOWNLOAD_TIMEOUT_SECS = 90
MIN_DOWNLOAD_BYTES = 1024
OCR_PAGE_SEGMENT_MODE = "6"
TESSDATA_FAST_PROVIDER = "fast"
TESSDATA_BEST_PROVIDER = "best"
TESSDATA_STANDARD_PROVIDER = "standard"
TESSDATA_PROVIDERS = (
    TESSDATA_FAST_PROVIDER,
    TESSDATA_BEST_PROVIDER,
    TESSDATA_STANDARD_PROVIDER,
)
TESSERACT_LANG_HEADER = "List of available languages"

DOC_COMMANDS = (
    "pdftotext",
    "pdfinfo",
    "pdftoppm",
    "tesseract",
    "pandoc",
    "soffice",
    "fc-list",
)

DOC_MODULES = (
    ("pypdf", "pypdf"),
    ("pdfplumber", "pdfplumber"),
    ("pdf2image", "pdf2image"),
    ("pytesseract", "pytesseract"),
    ("PIL", "pillow"),
    ("reportlab", "reportlab"),
    ("defusedxml", "defusedxml"),
    ("lxml", "lxml"),
)

CJK_FONT_HINTS = (
    "noto sans cjk",
    "noto serif cjk",
    "source han",
    "wenquanyi",
    "sarasa",
    "simsun",
    "simhei",
    "microsoft yahei",
    "pingfang",
)

FONT_DIRS = (
    "/usr/share/fonts",
    "/usr/local/share/fonts",
    str(Path.home() / ".fonts"),
    str(Path.home() / ".local" / "share" / "fonts"),
)

CJK_RE = re.compile(r"[\u3400-\u4dbf\u4e00-\u9fff\uf900-\ufaff]")
PAGES_RE = re.compile(r"^Pages:\s+(\d+)$", re.MULTILINE)

CJK_FONT_DOWNLOADS = (
    {
        "family": "sans",
        "filename": "NotoSansCJKsc-Regular.ttf",
        "urls": (
            "https://github.com/life888888/cjk-fonts-ttf/releases/"
            "download/v0.1.0/NotoSansCJKsc-Regular.ttf",
        ),
    },
    {
        "family": "serif",
        "filename": "NotoSerifCJKsc-Regular.ttf",
        "urls": (
            "https://github.com/life888888/cjk-fonts-ttf/releases/"
            "download/v0.1.0/NotoSerifCJKsc-Regular.ttf",
        ),
    },
)

TESSDATA_PROVIDER_BASE_URLS = {
    TESSDATA_FAST_PROVIDER: (
        "https://raw.githubusercontent.com/tesseract-ocr/"
        "tessdata_fast/main",
        "https://github.com/tesseract-ocr/tessdata_fast/raw/main",
    ),
    TESSDATA_BEST_PROVIDER: (
        "https://raw.githubusercontent.com/tesseract-ocr/"
        "tessdata_best/main",
        "https://github.com/tesseract-ocr/tessdata_best/raw/main",
    ),
    TESSDATA_STANDARD_PROVIDER: (
        "https://raw.githubusercontent.com/tesseract-ocr/"
        "tessdata/main",
        "https://github.com/tesseract-ocr/tessdata/raw/main",
    ),
}


def main() -> int:
    parser = build_parser()
    args = parser.parse_args()
    try:
        if args.command == "probe":
            payload = probe(args.state_dir)
        elif args.command == "ensure-python":
            payload = ensure_python(args.state_dir, args.packages)
        elif args.command == "ensure-fonts":
            payload = ensure_fonts(args.state_dir)
        elif args.command == "ensure-tessdata":
            payload = ensure_tessdata(
                state_dir=args.state_dir,
                langs=args.langs,
                provider=args.provider,
            )
        elif args.command == "verify-pdf":
            payload = verify_pdf(
                pdf_path=args.path,
                expect_cjk=args.expect_cjk,
                preview_dir=args.preview_dir,
                state_dir=args.state_dir,
            )
        else:
            raise ValueError(f"unsupported command: {args.command}")
    except Exception as err:
        print(
            json.dumps(
                {
                    "ok": False,
                    "error": str(err),
                },
                ensure_ascii=False,
                indent=2,
            )
        )
        return 1

    print(json.dumps(payload, ensure_ascii=False, indent=2))
    return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Managed runtime helper for document workflows.",
    )
    parser.add_argument(
        "--state-dir",
        default=os.getenv(STATE_DIR_ENV, "").strip(),
        help="Override TRPC_CLAW_STATE_DIR.",
    )
    sub = parser.add_subparsers(dest="command", required=True)

    sub.add_parser(
        "probe",
        help="Inspect PDF/OCR/CJK capabilities.",
    )

    ensure = sub.add_parser(
        "ensure-python",
        help="Install managed Python packages in the runtime toolchain.",
    )
    ensure.add_argument(
        "packages",
        nargs="+",
        help="Python packages to install.",
    )

    sub.add_parser(
        "ensure-fonts",
        help="Download managed CJK font files.",
    )

    tessdata = sub.add_parser(
        "ensure-tessdata",
        help="Download managed OCR language data.",
    )
    tessdata.add_argument(
        "langs",
        nargs="+",
        help="OCR languages such as chi_sim or eng.",
    )
    tessdata.add_argument(
        "--provider",
        choices=TESSDATA_PROVIDERS,
        default=TESSDATA_FAST_PROVIDER,
        help="Preferred official tessdata provider.",
    )

    verify = sub.add_parser(
        "verify-pdf",
        help="Verify a generated PDF before sending it back.",
    )
    verify.add_argument("--path", required=True, help="PDF path.")
    verify.add_argument(
        "--expect-cjk",
        action="store_true",
        help="Expect Chinese or other CJK text in the output.",
    )
    verify.add_argument(
        "--preview-dir",
        default="",
        help="Optional directory for a rendered page preview.",
    )
    return parser


def probe(state_dir: str) -> dict[str, object]:
    clean_state_dir = state_dir.strip()
    commands = {
        name: command_status(name) for name in DOC_COMMANDS
    }
    modules = {
        module: module_status(module, package)
        for module, package in DOC_MODULES
    }
    managed_fonts = managed_cjk_fonts(clean_state_dir)
    fonts = detect_cjk_fonts(commands, managed_fonts)
    langs = tesseract_languages(commands, clean_state_dir)
    return {
        "ok": True,
        "state_dir": clean_state_dir,
        "commands": commands,
        "python": python_probe(clean_state_dir),
        "modules": modules,
        "tesseract_langs": langs,
        "cjk_fonts": fonts,
        "managed_assets": {
            "toolchain_dir": str(toolchain_root(clean_state_dir)),
            "fonts_dir": str(managed_fonts_dir(clean_state_dir)),
            "tessdata_dir": str(managed_tessdata_dir(clean_state_dir)),
            "managed_python": str(managed_python_path(clean_state_dir)),
            "preferred_cjk_font": preferred_cjk_font(clean_state_dir),
        },
        "capabilities": {
            "has_cjk_fonts": len(fonts) > 0,
            "has_chi_sim": "chi_sim" in langs,
            "has_eng": "eng" in langs,
        },
    }


def ensure_python(
    state_dir: str,
    packages: list[str],
) -> dict[str, object]:
    clean_state_dir = state_dir.strip()
    if not clean_state_dir:
        raise ValueError("state dir is required for ensure-python")

    packages = [pkg.strip() for pkg in packages if pkg.strip()]
    if not packages:
        raise ValueError("at least one package is required")

    managed_root = managed_python_root(clean_state_dir)
    managed_python = managed_python_path(clean_state_dir)
    bootstrap_python = find_bootstrap_python(clean_state_dir)
    if not bootstrap_python:
        raise RuntimeError("python3 or python is required")

    if not managed_python.exists():
        run_command(
            [
                bootstrap_python,
                "-m",
                "venv",
                str(managed_root),
            ],
            env=plan_env(clean_state_dir),
        )

    pip_ok = run_command(
        [str(managed_python), "-m", "pip", "--version"],
        env=plan_env(clean_state_dir),
        check=False,
    )
    if pip_ok.returncode != 0:
        run_command(
            [str(managed_python), "-m", "ensurepip", "--upgrade"],
            env=plan_env(clean_state_dir),
        )

    run_command(
        [
            str(managed_python),
            "-m",
            "pip",
            "install",
            "--upgrade",
            "pip",
            "setuptools",
            "wheel",
        ],
        env=plan_env(clean_state_dir),
    )
    run_command(
        [str(managed_python), "-m", "pip", "install", *packages],
        env=plan_env(clean_state_dir),
    )

    return {
        "ok": True,
        "state_dir": clean_state_dir,
        "managed_python": str(managed_python),
        "packages": packages,
    }


def ensure_fonts(state_dir: str) -> dict[str, object]:
    clean_state_dir = state_dir.strip()
    if not clean_state_dir:
        raise ValueError("state dir is required for ensure-fonts")

    fonts_dir = managed_fonts_dir(clean_state_dir)
    fonts_dir.mkdir(parents=True, exist_ok=True)

    downloaded = []
    for spec in CJK_FONT_DOWNLOADS:
        target = fonts_dir / spec["filename"]
        if target.exists() and target.stat().st_size >= MIN_DOWNLOAD_BYTES:
            continue
        download_file(spec["urls"], target)
        downloaded.append(str(target))

    return {
        "ok": True,
        "state_dir": clean_state_dir,
        "fonts_dir": str(fonts_dir),
        "downloaded": downloaded,
        "fonts": managed_cjk_fonts(clean_state_dir),
        "preferred_font": preferred_cjk_font(clean_state_dir),
    }


def ensure_tessdata(
    state_dir: str,
    langs: list[str],
    provider: str,
) -> dict[str, object]:
    clean_state_dir = state_dir.strip()
    if not clean_state_dir:
        raise ValueError("state dir is required for ensure-tessdata")

    requested = normalize_langs(langs)
    if not requested:
        raise ValueError("at least one OCR language is required")

    tessdata_dir = managed_tessdata_dir(clean_state_dir)
    tessdata_dir.mkdir(parents=True, exist_ok=True)

    downloaded = []
    for lang in requested:
        target = tessdata_dir / f"{lang}.traineddata"
        if target.exists() and target.stat().st_size >= MIN_DOWNLOAD_BYTES:
            continue
        download_file(tessdata_urls(lang, provider), target)
        downloaded.append(str(target))

    return {
        "ok": True,
        "state_dir": clean_state_dir,
        "provider": provider,
        "langs": requested,
        "downloaded": downloaded,
        "tessdata_dir": str(tessdata_dir),
    }


def verify_pdf(
    pdf_path: str,
    expect_cjk: bool,
    preview_dir: str,
    state_dir: str,
) -> dict[str, object]:
    path = Path(pdf_path).expanduser().resolve()
    if not path.is_file():
        raise FileNotFoundError(f"pdf not found: {path}")

    page_count = extract_page_count(path)
    extracted_text, extractor = extract_pdf_text(path, state_dir)
    warnings = []

    if not extracted_text.strip():
        warnings.append("no text could be extracted from the PDF")
    if contains_replacement_chars(extracted_text):
        warnings.append("extracted text contains replacement characters")
    preview_path = ""
    preview_ocr = ""
    clean_preview_dir = preview_dir.strip()
    render_preview_for_check = bool(clean_preview_dir)
    if expect_cjk or warnings:
        render_preview_for_check = True

    if render_preview_for_check:
        preview_path, preview_ocr = render_pdf_preview_ocr(
            pdf_path=path,
            preview_dir=clean_preview_dir,
            state_dir=state_dir,
            expect_cjk=expect_cjk,
        )

    if expect_cjk:
        has_cjk = bool(CJK_RE.search(extracted_text))
        has_preview_cjk = bool(CJK_RE.search(preview_ocr))
        if not has_cjk and not has_preview_cjk:
            warnings.append(
                "expected CJK text but neither extraction nor "
                "preview OCR found it"
            )

    return {
        "ok": len(warnings) == 0,
        "path": str(path),
        "page_count": page_count,
        "extractor": extractor,
        "text_sample": extracted_text[:TEXT_SAMPLE_LIMIT],
        "ocr_text_sample": preview_ocr[:TEXT_SAMPLE_LIMIT],
        "warnings": warnings,
        "preview_path": preview_path,
    }


def command_status(name: str) -> dict[str, object]:
    resolved = shutil.which(name)
    return {
        "found": resolved is not None,
        "path": resolved or "",
    }


def module_status(
    module: str,
    package: str,
) -> dict[str, object]:
    found = importlib.util.find_spec(module) is not None
    return {
        "found": found,
        "package": package,
    }


def python_probe(state_dir: str) -> dict[str, object]:
    managed = managed_python_path(state_dir)
    bootstrap = find_bootstrap_python(state_dir)
    return {
        "managed_python": str(managed) if managed.exists() else "",
        "bootstrap_python": bootstrap,
        "current_python": sys.executable,
    }


def tesseract_languages(
    commands: dict[str, dict[str, object]],
    state_dir: str,
) -> list[str]:
    tesseract = commands.get("tesseract", {})
    path = str(tesseract.get("path", "")).strip()
    langs = set(managed_tessdata_langs(state_dir))
    if not path:
        return sorted(langs)

    result = run_command(
        tesseract_list_langs_command(path, state_dir),
        check=False,
    )
    if result.returncode == 0:
        langs.update(parse_tesseract_langs(result.stdout))
    return sorted(langs)


def tesseract_list_langs_command(
    tesseract_path: str,
    state_dir: str,
) -> list[str]:
    command = [tesseract_path]
    tessdata_dir = managed_tessdata_dir(state_dir)
    if tessdata_dir.is_dir():
        command.extend(["--tessdata-dir", str(tessdata_dir)])
    command.append("--list-langs")
    return command


def detect_cjk_fonts(
    commands: dict[str, dict[str, object]],
    managed_fonts: list[dict[str, str]],
) -> list[dict[str, str]]:
    if managed_fonts:
        return managed_fonts

    fc_list = str(
        commands.get("fc-list", {}).get("path", "")
    ).strip()
    if fc_list:
        result = run_command(
            [fc_list, ":lang=zh", "family", "file"],
            check=False,
        )
        fonts = parse_fc_list_output(result.stdout)
        if fonts:
            return fonts

    found = []
    for root in FONT_DIRS:
        path = Path(root)
        if not path.is_dir():
            continue
        for candidate in path.rglob("*"):
            if not candidate.is_file():
                continue
            lower_name = candidate.name.lower()
            if not lower_name.endswith((".ttf", ".otf", ".ttc")):
                continue
            if not any(hint in lower_name for hint in CJK_FONT_HINTS):
                continue
            found.append(
                {
                    "family": candidate.stem,
                    "file": str(candidate),
                }
            )
            if len(found) >= 20:
                return found
    return found


def managed_cjk_fonts(state_dir: str) -> list[dict[str, str]]:
    fonts_dir = managed_fonts_dir(state_dir)
    if not fonts_dir.is_dir():
        return []
    found = []
    for candidate in sorted(fonts_dir.iterdir()):
        if not candidate.is_file():
            continue
        lower_name = candidate.name.lower()
        if not lower_name.endswith((".ttf", ".otf", ".ttc")):
            continue
        found.append(
            {
                "family": candidate.stem,
                "file": str(candidate),
            }
        )
    return found


def preferred_cjk_font(state_dir: str) -> str:
    fonts = managed_cjk_fonts(state_dir)
    if not fonts:
        return ""
    for font in fonts:
        family = font.get("family", "").lower()
        if "sans" in family:
            return font.get("file", "")
    return fonts[0].get("file", "")


def parse_fc_list_output(text: str) -> list[dict[str, str]]:
    fonts = []
    seen = set()
    for raw in text.splitlines():
        line = raw.strip()
        if not line:
            continue
        parts = [part.strip() for part in line.split(":", 1)]
        if len(parts) != 2:
            continue
        file_path, family = parts
        key = (family, file_path)
        if key in seen:
            continue
        seen.add(key)
        fonts.append(
            {
                "family": family,
                "file": file_path,
            }
        )
    return fonts


def extract_page_count(pdf_path: Path) -> int:
    pdfinfo = shutil.which("pdfinfo")
    if not pdfinfo:
        return 0
    result = run_command(
        [pdfinfo, str(pdf_path)],
        check=False,
    )
    if result.returncode != 0:
        return 0
    match = PAGES_RE.search(result.stdout)
    if not match:
        return 0
    return int(match.group(1))


def extract_pdf_text(
    pdf_path: Path,
    state_dir: str,
) -> tuple[str, str]:
    pdftotext = shutil.which("pdftotext")
    if pdftotext:
        with tempfile.NamedTemporaryFile(
            suffix=".txt",
            delete=False,
        ) as handle:
            temp_path = Path(handle.name)
        try:
            result = run_command(
                [
                    pdftotext,
                    "-enc",
                    "UTF-8",
                    "-nopgbrk",
                    str(pdf_path),
                    str(temp_path),
                ],
                check=False,
            )
            if result.returncode == 0 and temp_path.exists():
                return (
                    temp_path.read_text(
                        encoding=UTF8_ENCODING,
                        errors="replace",
                    ),
                    "pdftotext",
                )
        finally:
            temp_path.unlink(missing_ok=True)

    if importlib.util.find_spec("pypdf") is not None:
        from pypdf import PdfReader  # type: ignore

        reader = PdfReader(str(pdf_path))
        text = []
        for page in reader.pages:
            text.append(page.extract_text() or "")
        return "\n".join(text), "pypdf"

    raise RuntimeError(
        "no PDF text extractor is available; install poppler or pypdf"
    )


def render_preview(pdf_path: Path, preview_dir: str) -> str:
    pdftoppm = shutil.which("pdftoppm")
    if not pdftoppm:
        return ""

    target_dir = Path(preview_dir).expanduser().resolve()
    target_dir.mkdir(parents=True, exist_ok=True)
    prefix = target_dir / PREVIEW_PREFIX
    result = run_command(
        [
            pdftoppm,
            "-png",
            "-f",
            "1",
            "-l",
            "1",
            str(pdf_path),
            str(prefix),
        ],
        check=False,
    )
    if result.returncode != 0:
        return ""
    preview = target_dir / f"{PREVIEW_PREFIX}-1.png"
    return str(preview) if preview.exists() else ""


def render_pdf_preview_ocr(
    pdf_path: Path,
    preview_dir: str,
    state_dir: str,
    expect_cjk: bool,
) -> tuple[str, str]:
    if preview_dir:
        preview_path = render_preview(pdf_path, preview_dir)
        if not preview_path:
            return "", ""
        return preview_path, ocr_image(
            Path(preview_path),
            state_dir,
            expect_cjk,
        )

    with tempfile.TemporaryDirectory(prefix="trpc-claw-preview-") as temp_dir:
        preview_path = render_preview(pdf_path, temp_dir)
        if not preview_path:
            return "", ""
        preview_ocr = ocr_image(
            Path(preview_path),
            state_dir,
            expect_cjk,
        )
    return "", preview_ocr


def ocr_image(
    image_path: Path,
    state_dir: str,
    expect_cjk: bool,
) -> str:
    tesseract = shutil.which("tesseract")
    if not tesseract or not image_path.is_file():
        return ""

    for command in ocr_commands(
        tesseract_path=tesseract,
        image_path=image_path,
        state_dir=state_dir,
        expect_cjk=expect_cjk,
    ):
        result = run_command(
            command,
            env=plan_env(state_dir),
            check=False,
        )
        if result.returncode == 0 and result.stdout.strip():
            return result.stdout
    return ""


def ocr_commands(
    tesseract_path: str,
    image_path: Path,
    state_dir: str,
    expect_cjk: bool,
) -> list[list[str]]:
    langs = ocr_language_candidates(state_dir, expect_cjk)
    tessdata_dir = managed_tessdata_dir(state_dir)
    commands = []
    for lang in langs:
        base = [
            tesseract_path,
            str(image_path),
            "stdout",
            "--psm",
            OCR_PAGE_SEGMENT_MODE,
        ]
        if tessdata_dir.is_dir() and managed_tessdata_langs(state_dir):
            commands.append(
                [
                    *base,
                    "--tessdata-dir",
                    str(tessdata_dir),
                    "-l",
                    lang,
                ]
            )
        commands.append([*base, "-l", lang])
    if not commands:
        commands.append(
            [
                tesseract_path,
                str(image_path),
                "stdout",
                "--psm",
                OCR_PAGE_SEGMENT_MODE,
            ]
        )
    return commands


def ocr_language_candidates(
    state_dir: str,
    expect_cjk: bool,
) -> list[str]:
    langs = set(managed_tessdata_langs(state_dir))
    system_langs = set()
    tesseract = shutil.which("tesseract")
    if tesseract:
        result = run_command([tesseract, "--list-langs"], check=False)
        if result.returncode == 0:
            system_langs.update(parse_tesseract_langs(result.stdout))
    langs.update(system_langs)

    candidates = []
    if expect_cjk and "chi_sim" in langs and "eng" in langs:
        candidates.append("chi_sim+eng")
    if expect_cjk and "chi_sim" in langs:
        candidates.append("chi_sim")
    if "eng" in langs:
        candidates.append("eng")
    return dedupe(candidates)


def contains_replacement_chars(text: str) -> bool:
    return "\ufffd" in text or "\x00" in text


def toolchain_root(state_dir: str) -> Path:
    return Path(state_dir) / TOOLCHAIN_DIR


def managed_fonts_dir(state_dir: str) -> Path:
    return toolchain_root(state_dir) / FONTS_DIR


def managed_tessdata_dir(state_dir: str) -> Path:
    return toolchain_root(state_dir) / TESSDATA_DIR


def managed_tessdata_langs(state_dir: str) -> list[str]:
    tessdata_dir = managed_tessdata_dir(state_dir)
    if not tessdata_dir.is_dir():
        return []
    langs = []
    for candidate in sorted(tessdata_dir.glob("*.traineddata")):
        name = candidate.stem.strip()
        if name:
            langs.append(name)
    return langs


def managed_python_root(state_dir: str) -> Path:
    return toolchain_root(state_dir) / PYTHON_ENV_DIR


def managed_python_path(state_dir: str) -> Path:
    return managed_python_root(state_dir) / BIN_DIR / "python3"


def normalize_langs(langs: list[str]) -> list[str]:
    normalized = []
    for lang in langs:
        clean = lang.strip()
        if clean:
            normalized.append(clean)
    return dedupe(normalized)


def tessdata_urls(lang: str, provider: str) -> list[str]:
    ordered = [provider]
    for fallback in TESSDATA_PROVIDERS:
        if fallback not in ordered:
            ordered.append(fallback)

    urls = []
    for name in ordered:
        for base_url in TESSDATA_PROVIDER_BASE_URLS.get(name, ()):
            urls.append(f"{base_url}/{lang}.traineddata")
    return urls


def download_file(urls: list[str] | tuple[str, ...], target: Path) -> None:
    target.parent.mkdir(parents=True, exist_ok=True)
    errors = []
    temp_path = target.with_suffix(target.suffix + ".tmp")
    for url in urls:
        try:
            with urllib.request.urlopen(
                url,
                timeout=DOWNLOAD_TIMEOUT_SECS,
            ) as response, temp_path.open("wb") as handle:
                shutil.copyfileobj(response, handle)
            if temp_path.stat().st_size < MIN_DOWNLOAD_BYTES:
                raise RuntimeError(
                    f"downloaded file is too small from {url}"
                )
            temp_path.replace(target)
            return
        except (
            OSError,
            RuntimeError,
            urllib.error.URLError,
        ) as err:
            temp_path.unlink(missing_ok=True)
            errors.append(f"{url}: {err}")
    raise RuntimeError("; ".join(errors))


def dedupe(values: list[str]) -> list[str]:
    seen = set()
    out = []
    for value in values:
        if value in seen:
            continue
        seen.add(value)
        out.append(value)
    return out


def parse_tesseract_langs(text: str) -> list[str]:
    langs = []
    for raw in text.splitlines():
        line = raw.strip()
        if not line:
            continue
        if line.startswith(TESSERACT_LANG_HEADER):
            continue
        if line.isdigit():
            continue
        langs.append(line)
    return langs


def find_bootstrap_python(state_dir: str) -> str:
    env_python = os.getenv(TOOLCHAIN_PYTHON_ENV, "").strip()
    if env_python and os.path.exists(env_python):
        return env_python

    managed = managed_python_path(state_dir)
    if managed.exists():
        return str(managed)

    current = sys.executable.strip()
    if current and os.path.exists(current):
        return current

    for name in ("python3", "python"):
        resolved = shutil.which(name)
        if resolved:
            return resolved
    return ""


def plan_env(state_dir: str) -> dict[str, str]:
    env = os.environ.copy()
    env[PIP_DISABLE_ENV] = PIP_DISABLE_VALUE

    candidates = [
        str(toolchain_root(state_dir) / BIN_DIR),
        str(managed_python_root(state_dir) / BIN_DIR),
    ]
    current_path = env.get(PATH_ENV, "")
    parts = [part for part in current_path.split(os.pathsep) if part]
    for candidate in reversed(candidates):
        if candidate not in parts:
            parts.insert(0, candidate)
    env[PATH_ENV] = os.pathsep.join(parts)
    return env


def run_command(
    command: list[str],
    env: dict[str, str] | None = None,
    check: bool = True,
) -> subprocess.CompletedProcess[str]:
    result = subprocess.run(
        command,
        check=False,
        env=env,
        text=True,
        capture_output=True,
    )
    if check and result.returncode != 0:
        raise RuntimeError(
            "command failed: "
            + shell_join(command)
            + "\n"
            + result.stderr.strip()
        )
    return result


def shell_join(parts: list[str]) -> str:
    return " ".join(shlex_quote(part) for part in parts)


def shlex_quote(text: str) -> str:
    if not text:
        return "''"
    if re.fullmatch(r"[A-Za-z0-9_./:=+-]+", text):
        return text
    return "'" + text.replace("'", "'\"'\"'") + "'"


if __name__ == "__main__":
    raise SystemExit(main())
