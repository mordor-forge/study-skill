"""Ebook format detection and conversion to PDF.

Supports epub, mobi, azw3, and other ebook formats.
Converts via ebook-convert (calibre) or pandoc, whichever is available.
Caches converted PDFs in ~/.cache/study/converted/ to avoid re-converting.
"""

from __future__ import annotations

import hashlib
import shutil
import subprocess
from pathlib import Path

# Formats that NLM and most RAG backends accept natively
NATIVE_FORMATS = {".pdf"}

# Formats we can convert to PDF
CONVERTIBLE_FORMATS = {".epub", ".mobi", ".azw3", ".azw", ".fb2", ".djvu", ".cbz"}

# All supported book formats (for the scanner)
ALL_BOOK_FORMATS = NATIVE_FORMATS | CONVERTIBLE_FORMATS

DEFAULT_CACHE_DIR = Path.home() / ".cache" / "study" / "converted"


def detect_format(path: Path) -> str:
    """Return the lowercase file extension."""
    return path.suffix.lower()


def needs_conversion(path: Path) -> bool:
    """Check if a file needs conversion before ingestion."""
    return detect_format(path) in CONVERTIBLE_FORMATS


def _cache_key(source_path: Path) -> str:
    """Generate a stable cache key from the source file path and modification time.

    Uses path + mtime so re-conversion happens if the source file changes.
    """
    stat = source_path.stat()
    key_input = f"{source_path.resolve()}:{stat.st_mtime_ns}"
    return hashlib.sha256(key_input.encode()).hexdigest()[:16]


def _cached_pdf_path(source_path: Path, cache_dir: Path) -> Path:
    """Return the expected path for a cached PDF conversion."""
    key = _cache_key(source_path)
    stem = source_path.stem
    return cache_dir / f"{stem}-{key}.pdf"


def _find_converter() -> tuple[str, list[str]] | None:
    """Find an available ebook-to-PDF converter.

    Returns (converter_name, base_command) or None if nothing is available.
    Prefers ebook-convert (calibre) for quality, falls back to pandoc.
    """
    ebook_convert = shutil.which("ebook-convert")
    if ebook_convert:
        return ("ebook-convert", [ebook_convert])

    pandoc = shutil.which("pandoc")
    if pandoc:
        return ("pandoc", [pandoc])

    return None


def convert_to_pdf(
    source_path: Path | str,
    cache_dir: Path | str = DEFAULT_CACHE_DIR,
) -> Path:
    """Convert an ebook file to PDF, returning the path to the PDF.

    If the source is already a PDF, returns it unchanged.
    Converted files are cached — repeated calls for the same source are free.

    Args:
        source_path: Path to the ebook file.
        cache_dir: Directory for cached conversions.

    Returns:
        Path to the PDF file (original if already PDF, cached conversion otherwise).

    Raises:
        FileNotFoundError: If the source file doesn't exist.
        ValueError: If the format is not supported.
        RuntimeError: If no converter is available or conversion fails.
    """
    source = Path(source_path).resolve()
    cache = Path(cache_dir)

    if not source.exists():
        msg = f"Source file not found: {source}"
        raise FileNotFoundError(msg)

    fmt = detect_format(source)

    if fmt in NATIVE_FORMATS:
        return source

    if fmt not in CONVERTIBLE_FORMATS:
        supported = sorted(NATIVE_FORMATS | CONVERTIBLE_FORMATS)
        msg = f"Unsupported format: {fmt}. Supported: {supported}"
        raise ValueError(msg)

    # Check cache
    output = _cached_pdf_path(source, cache)
    if output.exists():
        return output

    # Find converter
    converter = _find_converter()
    if converter is None:
        msg = (
            "No ebook converter found. Install one of:\n"
            "  - calibre: dnf install calibre  (or flatpak)\n"
            "  - pandoc:  dnf install pandoc"
        )
        raise RuntimeError(msg)

    converter_name, base_cmd = converter

    # Ensure cache directory exists
    cache.mkdir(parents=True, exist_ok=True)

    # Convert
    if converter_name == "ebook-convert":
        cmd = [*base_cmd, str(source), str(output)]
    else:
        # pandoc: epub → pdf requires a PDF engine (pdflatex, xelatex, etc.)
        cmd = [*base_cmd, str(source), "-o", str(output)]

    result = subprocess.run(cmd, capture_output=True, text=True, timeout=300)

    if result.returncode != 0:
        # Clean up partial output
        output.unlink(missing_ok=True)
        msg = f"{converter_name} failed (exit {result.returncode}): {result.stderr[:500]}"
        raise RuntimeError(msg)

    if not output.exists():
        msg = f"{converter_name} completed but output file not found: {output}"
        raise RuntimeError(msg)

    return output


def get_converter_info() -> dict:
    """Return information about the available converter.

    Useful for the skill to report in status output.
    """
    converter = _find_converter()
    if converter is None:
        return {"available": False, "name": None, "path": None}

    name, cmd = converter
    return {"available": True, "name": name, "path": cmd[0]}
