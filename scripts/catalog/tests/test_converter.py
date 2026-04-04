"""Tests for ebook format detection and conversion."""

from pathlib import Path
from unittest.mock import patch

import pytest

from catalog.converter import (
    ALL_BOOK_FORMATS,
    CONVERTIBLE_FORMATS,
    NATIVE_FORMATS,
    convert_to_pdf,
    detect_format,
    get_converter_info,
    needs_conversion,
)


class TestDetectFormat:
    def test_pdf(self, tmp_path):
        p = tmp_path / "book.pdf"
        p.touch()
        assert detect_format(p) == ".pdf"

    def test_epub(self, tmp_path):
        p = tmp_path / "book.epub"
        p.touch()
        assert detect_format(p) == ".epub"

    def test_mobi(self, tmp_path):
        p = tmp_path / "book.mobi"
        p.touch()
        assert detect_format(p) == ".mobi"

    def test_uppercase_extension(self, tmp_path):
        p = tmp_path / "book.EPUB"
        p.touch()
        assert detect_format(p) == ".epub"

    def test_azw3(self, tmp_path):
        p = tmp_path / "book.azw3"
        p.touch()
        assert detect_format(p) == ".azw3"


class TestNeedsConversion:
    def test_pdf_no_conversion(self, tmp_path):
        p = tmp_path / "book.pdf"
        p.touch()
        assert not needs_conversion(p)

    def test_epub_needs_conversion(self, tmp_path):
        p = tmp_path / "book.epub"
        p.touch()
        assert needs_conversion(p)

    def test_mobi_needs_conversion(self, tmp_path):
        p = tmp_path / "book.mobi"
        p.touch()
        assert needs_conversion(p)

    def test_txt_not_convertible(self, tmp_path):
        p = tmp_path / "notes.txt"
        p.touch()
        assert not needs_conversion(p)


class TestConvertToPdf:
    def test_pdf_returns_unchanged(self, tmp_path):
        pdf = tmp_path / "book.pdf"
        pdf.write_bytes(b"%PDF-1.4 fake")
        result = convert_to_pdf(pdf)
        assert result == pdf.resolve()

    def test_nonexistent_file_raises(self):
        with pytest.raises(FileNotFoundError):
            convert_to_pdf("/nonexistent/book.epub")

    def test_unsupported_format_raises(self, tmp_path):
        txt = tmp_path / "notes.txt"
        txt.write_text("hello")
        with pytest.raises(ValueError, match="Unsupported format"):
            convert_to_pdf(txt)

    def test_cached_conversion_reused(self, tmp_path):
        """If a cached PDF exists, conversion is skipped."""
        epub = tmp_path / "book.epub"
        epub.write_bytes(b"fake epub content")
        cache_dir = tmp_path / "cache"

        # Simulate a cached conversion by pre-creating the output
        from catalog.converter import _cached_pdf_path

        cached = _cached_pdf_path(epub, cache_dir)
        cached.parent.mkdir(parents=True, exist_ok=True)
        cached.write_bytes(b"%PDF-1.4 cached")

        # Should return cached file without calling any converter
        with patch("catalog.converter._find_converter", return_value=None):
            result = convert_to_pdf(epub, cache_dir=cache_dir)

        assert result == cached
        assert result.read_bytes() == b"%PDF-1.4 cached"

    def test_no_converter_raises(self, tmp_path):
        epub = tmp_path / "book.epub"
        epub.write_bytes(b"fake epub")
        cache_dir = tmp_path / "cache"

        with (
            patch("catalog.converter._find_converter", return_value=None),
            pytest.raises(RuntimeError, match="No ebook converter found"),
        ):
            convert_to_pdf(epub, cache_dir=cache_dir)

    def test_successful_conversion(self, tmp_path):
        """Test the full conversion flow with a mocked converter."""
        epub = tmp_path / "book.epub"
        epub.write_bytes(b"fake epub")
        cache_dir = tmp_path / "cache"

        from catalog.converter import _cached_pdf_path

        expected_output = _cached_pdf_path(epub, cache_dir)

        def mock_run(cmd, **kwargs):
            # Simulate converter creating the output file
            Path(cmd[-1] if "ebook-convert" in cmd[0] else cmd[cmd.index("-o") + 1]).parent.mkdir(
                parents=True, exist_ok=True
            )
            Path(cmd[-1] if "ebook-convert" in cmd[0] else cmd[cmd.index("-o") + 1]).write_bytes(
                b"%PDF-1.4 converted"
            )

            class Result:
                returncode = 0
                stderr = ""

            return Result()

        with (
            patch(
                "catalog.converter._find_converter",
                return_value=("ebook-convert", ["/usr/bin/ebook-convert"]),
            ),
            patch("catalog.converter.subprocess.run", side_effect=mock_run),
        ):
            result = convert_to_pdf(epub, cache_dir=cache_dir)

        assert result == expected_output
        assert result.exists()


class TestFormatSets:
    def test_pdf_is_native(self):
        assert ".pdf" in NATIVE_FORMATS

    def test_epub_is_convertible(self):
        assert ".epub" in CONVERTIBLE_FORMATS

    def test_no_overlap(self):
        assert not NATIVE_FORMATS & CONVERTIBLE_FORMATS

    def test_all_formats_is_union(self):
        assert ALL_BOOK_FORMATS == NATIVE_FORMATS | CONVERTIBLE_FORMATS


class TestGetConverterInfo:
    def test_no_converter(self):
        with patch("catalog.converter.shutil.which", return_value=None):
            info = get_converter_info()
        assert info["available"] is False
        assert info["name"] is None

    def test_ebook_convert_found(self):
        def which(name):
            if name == "ebook-convert":
                return "/usr/bin/ebook-convert"
            return None

        with patch("catalog.converter.shutil.which", side_effect=which):
            info = get_converter_info()
        assert info["available"] is True
        assert info["name"] == "ebook-convert"

    def test_pandoc_fallback(self):
        def which(name):
            if name == "pandoc":
                return "/usr/bin/pandoc"
            return None

        with patch("catalog.converter.shutil.which", side_effect=which):
            info = get_converter_info()
        assert info["available"] is True
        assert info["name"] == "pandoc"
