"""Fuzzy topic matching for book search."""

from __future__ import annotations

import re

from catalog.models import Book


def _tokenize(text: str) -> set[str]:
    """Split text into lowercase word tokens, stripping non-alphanumeric chars."""
    return {w for w in re.findall(r"[a-z0-9]+", text.lower()) if len(w) > 1}


def score_book(book: Book, query: str) -> int:
    """Score a book against a search query.

    Scoring breakdown:
        - Exact title match (case-insensitive): +10
        - Topic match (query appears in any topic): +5 per matching topic
        - Word overlap (query words found in title): +3 per matching word
        - Category match (query appears in category): +1

    Returns:
        Integer score (0 means no relevance).
    """
    score = 0
    query_lower = query.lower().strip()
    title_lower = book.title.lower()
    category_lower = book.category.lower()

    # Exact title match
    if query_lower == title_lower:
        score += 10

    # Topic matches — check if query (or any query word) appears in topic names
    query_words = _tokenize(query)
    for topic in book.topics:
        topic_lower = topic.lower()
        # Full query substring match in topic
        if query_lower in topic_lower or topic_lower in query_lower:
            score += 5

    # Word overlap with title
    title_words = _tokenize(book.title)
    overlap = query_words & title_words
    score += 3 * len(overlap)

    # Category match
    if query_lower in category_lower or any(w in category_lower for w in query_words):
        score += 1

    return score


def rank_books(
    books: list[Book],
    query: str,
    top_n: int = 10,
) -> list[dict]:
    """Score and rank books against a query.

    Returns top N results (excluding zero-score books) as dicts
    with the book data plus a 'score' field, sorted by descending score.
    """
    scored = []
    for book in books:
        s = score_book(book, query)
        if s > 0:
            entry = book.model_dump()
            entry["score"] = s
            scored.append(entry)

    scored.sort(key=lambda x: x["score"], reverse=True)
    return scored[:top_n]
