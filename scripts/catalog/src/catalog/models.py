"""Pydantic v2 models for book catalog."""

from __future__ import annotations

import json
from datetime import UTC, datetime
from pathlib import Path

from pydantic import BaseModel, Field


class Book(BaseModel):
    """A single book entry in the catalog."""

    title: str
    path: str  # relative to library root
    category: str
    filename: str
    format: str = "pdf"  # file extension without dot: pdf, epub, mobi, etc.
    topics: list[str]
    pages: int | None = None
    author: str | None = None
    size_bytes: int | None = None


class Catalog(BaseModel):
    """The full book catalog, built from a library directory."""

    library_path: str
    built_at: datetime = Field(default_factory=lambda: datetime.now(UTC))
    books: list[Book] = Field(default_factory=list)

    def search(self, query: str, top_n: int = 10) -> list[dict]:
        """Search the catalog using fuzzy matching.

        Delegates to matcher module to avoid circular imports.
        Returns top N results as dicts with score included.
        """
        from catalog.matcher import rank_books

        return rank_books(self.books, query, top_n=top_n)

    def save(self, path: str | Path) -> None:
        """Serialize catalog to JSON file."""
        output = Path(path)
        output.parent.mkdir(parents=True, exist_ok=True)
        output.write_text(self.model_dump_json(indent=2), encoding="utf-8")

    @classmethod
    def load(cls, path: str | Path) -> Catalog:
        """Deserialize catalog from JSON file."""
        data = json.loads(Path(path).read_text(encoding="utf-8"))
        return cls.model_validate(data)
