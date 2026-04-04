"""Tests for Pydantic models."""

from datetime import UTC, datetime

from catalog.models import Book, Catalog


def _make_book(**overrides) -> Book:
    """Helper to create a Book with sensible defaults."""
    defaults = {
        "title": "Quantum Mechanics",
        "path": "Physics/Quantum_Mechanics.pdf",
        "category": "Physics",
        "filename": "Quantum_Mechanics.pdf",
        "topics": ["quantum mechanics", "physics"],
        "pages": 450,
        "author": "J. Griffiths",
        "size_bytes": 12_000_000,
    }
    defaults.update(overrides)
    return Book(**defaults)


class TestBook:
    def test_create_minimal(self):
        book = Book(
            title="Test",
            path="test.pdf",
            category="General",
            filename="test.pdf",
            topics=[],
        )
        assert book.title == "Test"
        assert book.pages is None
        assert book.author is None
        assert book.size_bytes is None

    def test_create_full(self):
        book = _make_book()
        assert book.title == "Quantum Mechanics"
        assert book.pages == 450
        assert book.author == "J. Griffiths"
        assert len(book.topics) == 2

    def test_serialization_roundtrip(self):
        book = _make_book()
        data = book.model_dump()
        restored = Book.model_validate(data)
        assert restored == book

    def test_json_roundtrip(self):
        book = _make_book()
        json_str = book.model_dump_json()
        restored = Book.model_validate_json(json_str)
        assert restored == book


class TestCatalog:
    def test_create_empty(self):
        catalog = Catalog(library_path="/tmp/books")
        assert catalog.books == []
        assert catalog.library_path == "/tmp/books"
        assert isinstance(catalog.built_at, datetime)

    def test_create_with_books(self):
        books = [_make_book(title="Book A"), _make_book(title="Book B")]
        catalog = Catalog(library_path="/tmp/books", books=books)
        assert len(catalog.books) == 2

    def test_serialization_roundtrip(self):
        books = [_make_book()]
        catalog = Catalog(
            library_path="/tmp/books",
            built_at=datetime(2025, 1, 1, tzinfo=UTC),
            books=books,
        )
        data = catalog.model_dump()
        restored = Catalog.model_validate(data)
        assert restored.library_path == catalog.library_path
        assert len(restored.books) == 1
        assert restored.books[0].title == "Quantum Mechanics"

    def test_save_and_load(self, tmp_path):
        book = _make_book()
        catalog = Catalog(
            library_path="/tmp/books",
            built_at=datetime(2025, 6, 15, 12, 0, 0, tzinfo=UTC),
            books=[book],
        )

        out_file = tmp_path / "catalog.json"
        catalog.save(out_file)
        assert out_file.exists()

        loaded = Catalog.load(out_file)
        assert loaded.library_path == catalog.library_path
        assert len(loaded.books) == 1
        assert loaded.books[0].title == "Quantum Mechanics"
        assert loaded.built_at == catalog.built_at

    def test_save_creates_parent_dirs(self, tmp_path):
        catalog = Catalog(library_path="/tmp/books")
        out_file = tmp_path / "nested" / "dir" / "catalog.json"
        catalog.save(out_file)
        assert out_file.exists()
