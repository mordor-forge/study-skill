"""Directory walker and book metadata extraction."""

from __future__ import annotations

import re
import sys
from pathlib import Path

from catalog.converter import ALL_BOOK_FORMATS
from catalog.models import Book, Catalog

# Map of keyword stems to canonical topic names.
# Order doesn't matter — each PDF path/title is checked against all keys.
TOPIC_KEYWORDS: dict[str, str] = {
    "physics": "physics",
    "quantum": "quantum mechanics",
    "mechanic": "mechanics",
    "relativity": "relativity",
    "thermo": "thermodynamics",
    "electro": "electromagnetism",
    "optic": "optics",
    "nuclear": "nuclear physics",
    "particle": "particle physics",
    "astro": "astrophysics",
    "cosmo": "cosmology",
    "chemistry": "chemistry",
    "organic": "organic chemistry",
    "inorganic": "inorganic chemistry",
    "biochem": "biochemistry",
    "math": "mathematics",
    "algebra": "algebra",
    "calculus": "calculus",
    "geometry": "geometry",
    "topology": "topology",
    "statistic": "statistics",
    "probabilit": "probability",
    "linear": "linear algebra",
    "differential": "differential equations",
    "analysis": "analysis",
    "number theory": "number theory",
    "programming": "programming",
    "algorithm": "algorithms",
    "data structure": "data structures",
    "machine learning": "machine learning",
    "deep learning": "deep learning",
    "neural": "neural networks",
    "artificial intelligence": "artificial intelligence",
    "computer science": "computer science",
    "software": "software engineering",
    "operating system": "operating systems",
    "network": "networking",
    "database": "databases",
    "crypto": "cryptography",
    "biology": "biology",
    "genetics": "genetics",
    "evolution": "evolution",
    "ecology": "ecology",
    "engineer": "engineering",
    "circuit": "circuits",
    "signal": "signal processing",
    "control": "control systems",
    "fluid": "fluid dynamics",
    "material": "materials science",
    "python": "python",
    "java": "java",
    "rust": "rust",
    "golang": "go",
    " go ": "go",
    "c++": "c++",
    "javascript": "javascript",
    "linux": "linux",
}


def dirname_to_title(name: str) -> str:
    """Convert a directory/file name to a human-readable title.

    Examples:
        Advanced_Quantum_Mechanics -> Advanced Quantum Mechanics
        intro-to-physics -> Intro To Physics
        some.book.name -> Some Book Name
    """
    # Replace underscores, hyphens, and dots (except file extension dot) with spaces
    cleaned = re.sub(r"[_\-]", " ", name)
    # Collapse multiple spaces
    cleaned = re.sub(r"\s+", " ", cleaned).strip()
    # Title-case each word
    return cleaned.title()


def derive_topics(path_parts: list[str], title: str) -> list[str]:
    """Derive topic tags from path components and title.

    Checks each path component and the title against the keyword map.
    Returns deduplicated list of matching topics.
    """
    # Build a single searchable string from all path parts + title
    searchable = " ".join(path_parts).lower() + " " + title.lower() + " "

    topics: list[str] = []
    seen: set[str] = set()
    for keyword, topic in TOPIC_KEYWORDS.items():
        if keyword in searchable and topic not in seen:
            topics.append(topic)
            seen.add(topic)

    return sorted(topics)


def _extract_pdf_metadata(pdf_path: Path) -> dict:
    """Extract metadata from a PDF file using pdfplumber.

    Returns dict with 'pages' and 'author' keys (values may be None).
    """
    try:
        import pdfplumber

        with pdfplumber.open(pdf_path) as pdf:
            pages = len(pdf.pages)
            author = pdf.metadata.get("Author") if pdf.metadata else None
            return {"pages": pages, "author": author}
    except Exception:
        # Silently skip metadata extraction failures — corrupt PDFs, etc.
        return {"pages": None, "author": None}


def scan_directory(
    library_path: str | Path,
    extract_metadata: bool = False,
) -> Catalog:
    """Walk a directory tree and build a Catalog of all PDF files found.

    Args:
        library_path: Root directory of the book library.
        extract_metadata: If True, open each PDF to extract page count and author.
                          This is slow for large libraries.

    Returns:
        A Catalog instance with all discovered books.
    """
    root = Path(library_path).resolve()
    if not root.is_dir():
        msg = f"Library path is not a directory: {root}"
        raise FileNotFoundError(msg)

    books: list[Book] = []

    # Collect all supported book formats
    all_files: list[Path] = []
    for fmt in ALL_BOOK_FORMATS:
        all_files.extend(root.rglob(f"*{fmt}"))
    all_files.sort()

    for i, book_path in enumerate(all_files, 1):
        rel = book_path.relative_to(root)
        parts = list(rel.parts)

        # Category = first directory component, or "uncategorized"
        category = dirname_to_title(parts[0]) if len(parts) > 1 else "Uncategorized"

        # Title = filename without extension, humanized
        stem = book_path.stem
        title = dirname_to_title(stem)

        # Derive topics from all path components
        topics = derive_topics(parts, title)

        # File format without dot
        file_format = book_path.suffix.lower().lstrip(".")

        book_kwargs: dict = {
            "title": title,
            "path": str(rel),
            "category": category,
            "filename": book_path.name,
            "format": file_format,
            "topics": topics,
            "size_bytes": book_path.stat().st_size,
        }

        if extract_metadata and file_format == "pdf":
            print(f"  [{i}/{len(all_files)}] {rel}", file=sys.stderr)
            meta = _extract_pdf_metadata(book_path)
            book_kwargs["pages"] = meta["pages"]
            book_kwargs["author"] = meta["author"]

        books.append(Book(**book_kwargs))

    return Catalog(library_path=str(root), books=books)
