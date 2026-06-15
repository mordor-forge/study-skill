"""Tests for the catalog CLI."""

from __future__ import annotations

import json

import pytest

from catalog.cli import main
from catalog.models import Book, Catalog


def test_build_writes_catalog(tmp_path, monkeypatch, capsys):
    library = tmp_path / "library"
    output = tmp_path / "catalog.json"
    book = library / "Physics" / "Classical.Mechanics.PDF"
    book.parent.mkdir(parents=True)
    book.write_bytes(b"%PDF-1.4 fake")

    monkeypatch.setattr(
        "sys.argv",
        ["study-catalog", "build", str(library), "--output", str(output)],
    )

    main()

    captured = capsys.readouterr()
    assert "Found 1 books" in captured.err
    catalog = Catalog.load(output)
    assert catalog.books[0].title == "Classical Mechanics"


def test_build_rejects_missing_directory(tmp_path, monkeypatch, capsys):
    missing = tmp_path / "missing"
    monkeypatch.setattr("sys.argv", ["study-catalog", "build", str(missing)])

    with pytest.raises(SystemExit) as exc:
        main()

    assert exc.value.code == 1
    captured = capsys.readouterr()
    assert f"Error: {missing} is not a directory" in captured.err


def test_search_outputs_json(tmp_path, monkeypatch, capsys):
    catalog_path = tmp_path / "catalog.json"
    catalog = Catalog(
        library_path=str(tmp_path),
        books=[
            Book(
                title="Quantum Mechanics",
                path="Physics/Quantum_Mechanics.pdf",
                category="Physics",
                filename="Quantum_Mechanics.pdf",
                format="pdf",
                topics=["quantum mechanics", "physics"],
            )
        ],
    )
    catalog.save(catalog_path)

    monkeypatch.setattr(
        "sys.argv",
        ["study-catalog", "search", "quantum", "--catalog", str(catalog_path), "--top", "1"],
    )

    main()

    results = json.loads(capsys.readouterr().out)
    assert len(results) == 1
    assert results[0]["title"] == "Quantum Mechanics"


def test_search_rejects_missing_catalog(tmp_path, monkeypatch, capsys):
    missing = tmp_path / "missing.json"
    monkeypatch.setattr(
        "sys.argv",
        ["study-catalog", "search", "physics", "--catalog", str(missing)],
    )

    with pytest.raises(SystemExit) as exc:
        main()

    assert exc.value.code == 1
    captured = capsys.readouterr()
    assert f"Error: catalog not found at {missing}" in captured.err
