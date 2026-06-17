"""Tests for the directory scanner."""

from pathlib import Path

import pytest

from catalog.scanner import (
    derive_topics,
    detect_book_format,
    dirname_to_title,
    scan_directory,
)


class TestDirnameToTitle:
    def test_underscores(self):
        assert dirname_to_title("Advanced_Quantum_Mechanics") == "Advanced Quantum Mechanics"

    def test_hyphens(self):
        assert dirname_to_title("intro-to-physics") == "Intro To Physics"

    def test_mixed_separators(self):
        assert dirname_to_title("my_book-name") == "My Book Name"

    def test_dots(self):
        assert dirname_to_title("some.book.name") == "Some Book Name"

    def test_multiple_spaces_collapsed(self):
        assert dirname_to_title("too__many___underscores") == "Too Many Underscores"

    def test_single_word(self):
        assert dirname_to_title("Physics") == "Physics"

    def test_already_clean(self):
        assert dirname_to_title("Already Clean") == "Already Clean"


class TestDetectBookFormat:
    def test_supported_suffix(self, tmp_path):
        path = tmp_path / "Book.EPUB"
        path.write_bytes(b"fake book content")
        assert detect_book_format(path) == "epub"

    def test_pdf_signature_without_supported_suffix(self, tmp_path):
        path = tmp_path / "10 - Unknown.1007%2f978-1-4614-6227-9"
        path.write_bytes(b"%PDF-1.4 fake")
        assert detect_book_format(path) == "pdf"

    def test_unsupported_file(self, tmp_path):
        path = tmp_path / "metadata.opf"
        path.write_text("<package />")
        assert detect_book_format(path) is None


class TestDeriveTopics:
    def test_physics_keyword(self):
        topics = derive_topics(["Physics"], "Classical Mechanics")
        assert "physics" in topics
        assert "mechanics" in topics

    def test_quantum_from_title(self):
        topics = derive_topics(["Science"], "Quantum Field Theory")
        assert "quantum mechanics" in topics

    def test_programming(self):
        topics = derive_topics(["CS", "Programming"], "Python Algorithms")
        assert "programming" in topics
        assert "algorithms" in topics
        assert "python" in topics

    def test_no_match(self):
        topics = derive_topics(["misc"], "Random Thoughts")
        assert topics == []

    def test_math_topics(self):
        topics = derive_topics(["Mathematics"], "Linear Algebra")
        assert "mathematics" in topics
        assert "linear algebra" in topics
        assert "algebra" in topics

    def test_deduplication(self):
        # "algebra" keyword and "linear" keyword both in the searchable text,
        # but each topic should appear only once
        topics = derive_topics(["Algebra"], "Linear Algebra Textbook")
        count_algebra = topics.count("algebra")
        assert count_algebra == 1


def _create_fake_pdf(path: Path) -> None:
    """Create a minimal fake PDF file."""
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(b"%PDF-1.4 fake")


def _create_fake_book(path: Path) -> None:
    """Create a minimal fake ebook file (any format)."""
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(b"fake book content")


class TestScanDirectory:
    def test_empty_directory(self, tmp_path):
        catalog = scan_directory(tmp_path)
        assert len(catalog.books) == 0
        assert catalog.library_path == str(tmp_path.resolve())

    def test_single_pdf(self, tmp_path):
        _create_fake_pdf(tmp_path / "test_book.pdf")
        catalog = scan_directory(tmp_path)
        assert len(catalog.books) == 1
        book = catalog.books[0]
        assert book.title == "Test Book"
        assert book.filename == "test_book.pdf"
        assert book.category == "Uncategorized"
        assert book.path == "test_book.pdf"

    def test_nested_pdf(self, tmp_path):
        _create_fake_pdf(tmp_path / "Physics" / "Quantum_Mechanics.pdf")
        catalog = scan_directory(tmp_path)
        assert len(catalog.books) == 1
        book = catalog.books[0]
        assert book.title == "Quantum Mechanics"
        assert book.category == "Physics"
        assert book.path == "Physics/Quantum_Mechanics.pdf"
        assert "physics" in book.topics
        assert "quantum mechanics" in book.topics

    def test_multiple_categories(self, tmp_path):
        _create_fake_pdf(tmp_path / "Physics" / "mechanics.pdf")
        _create_fake_pdf(tmp_path / "Math" / "calculus.pdf")
        _create_fake_pdf(tmp_path / "CS" / "Programming" / "python_intro.pdf")
        catalog = scan_directory(tmp_path)
        assert len(catalog.books) == 3

        categories = {b.category for b in catalog.books}
        assert "Physics" in categories
        assert "Math" in categories
        assert "Cs" in categories  # title-cased from "CS"

    def test_non_pdf_files_ignored(self, tmp_path):
        _create_fake_pdf(tmp_path / "real.pdf")
        (tmp_path / "readme.txt").write_text("not a pdf")
        (tmp_path / "notes.md").write_text("# notes")
        catalog = scan_directory(tmp_path)
        assert len(catalog.books) == 1

    def test_nonexistent_directory(self):
        with pytest.raises(FileNotFoundError):
            scan_directory("/nonexistent/path/to/library")

    def test_size_bytes_populated(self, tmp_path):
        _create_fake_pdf(tmp_path / "small.pdf")
        catalog = scan_directory(tmp_path)
        assert catalog.books[0].size_bytes is not None
        assert catalog.books[0].size_bytes > 0

    def test_deeply_nested(self, tmp_path):
        _create_fake_pdf(tmp_path / "Science" / "Physics" / "Advanced" / "relativity.pdf")
        catalog = scan_directory(tmp_path)
        assert len(catalog.books) == 1
        book = catalog.books[0]
        # Category is first directory component
        assert book.category == "Science"
        assert "relativity" in book.topics
        assert "physics" in book.topics

    def test_epub_indexed(self, tmp_path):
        _create_fake_book(tmp_path / "Fiction" / "novel.epub")
        catalog = scan_directory(tmp_path)
        assert len(catalog.books) == 1
        assert catalog.books[0].format == "epub"
        assert catalog.books[0].filename == "novel.epub"

    def test_uppercase_extensions_indexed(self, tmp_path):
        _create_fake_pdf(tmp_path / "Physics" / "Classical.Mechanics.PDF")
        _create_fake_book(tmp_path / "Fiction" / "Novel.EPUB")
        catalog = scan_directory(tmp_path)
        assert len(catalog.books) == 2

        by_filename = {book.filename: book for book in catalog.books}
        assert by_filename["Classical.Mechanics.PDF"].format == "pdf"
        assert by_filename["Classical.Mechanics.PDF"].title == "Classical Mechanics"
        assert by_filename["Novel.EPUB"].format == "epub"

    def test_pdf_signature_without_supported_suffix_indexed(self, tmp_path):
        book_path = tmp_path / "Unknown" / "10 (335)" / "10 - Unknown.1007%2f978-1-4614-6227-9"
        _create_fake_pdf(book_path)
        catalog = scan_directory(tmp_path)
        assert len(catalog.books) == 1
        assert catalog.books[0].format == "pdf"
        assert catalog.books[0].filename == "10 - Unknown.1007%2f978-1-4614-6227-9"
        assert "1007%2F978" in catalog.books[0].title
        assert "4614" in catalog.books[0].title

    def test_mobi_indexed(self, tmp_path):
        _create_fake_book(tmp_path / "Fiction" / "novel.mobi")
        catalog = scan_directory(tmp_path)
        assert len(catalog.books) == 1
        assert catalog.books[0].format == "mobi"

    def test_mixed_formats(self, tmp_path):
        _create_fake_pdf(tmp_path / "Science" / "physics.pdf")
        _create_fake_book(tmp_path / "Science" / "chemistry.epub")
        _create_fake_book(tmp_path / "Fiction" / "novel.mobi")
        catalog = scan_directory(tmp_path)
        assert len(catalog.books) == 3
        formats = {b.format for b in catalog.books}
        assert formats == {"pdf", "epub", "mobi"}

    def test_pdf_format_field(self, tmp_path):
        _create_fake_pdf(tmp_path / "test.pdf")
        catalog = scan_directory(tmp_path)
        assert catalog.books[0].format == "pdf"

    def test_unsupported_formats_ignored(self, tmp_path):
        _create_fake_pdf(tmp_path / "real.pdf")
        (tmp_path / "readme.txt").write_text("not a book")
        (tmp_path / "doc.docx").write_bytes(b"fake docx")
        catalog = scan_directory(tmp_path)
        assert len(catalog.books) == 1
