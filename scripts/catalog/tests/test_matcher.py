"""Tests for fuzzy topic matching."""

from catalog.matcher import rank_books, score_book
from catalog.models import Book


def _book(title: str, category: str = "General", topics: list[str] | None = None) -> Book:
    """Helper to create a Book with minimal required fields."""
    return Book(
        title=title,
        path=f"{category}/{title.replace(' ', '_')}.pdf",
        category=category,
        filename=f"{title.replace(' ', '_')}.pdf",
        topics=topics or [],
    )


class TestScoreBook:
    def test_exact_title_match(self):
        book = _book("Quantum Mechanics", topics=["quantum mechanics"])
        score = score_book(book, "Quantum Mechanics")
        # Exact title (10) + topic match (5) + word overlap "quantum"+"mechanics" (6)
        assert score >= 10

    def test_topic_match(self):
        book = _book("Introduction to QM", topics=["quantum mechanics", "physics"])
        score = score_book(book, "quantum")
        # Topic "quantum mechanics" contains "quantum" -> 5 pts
        assert score >= 5

    def test_word_overlap(self):
        book = _book("Advanced Calculus Methods", topics=["calculus"])
        score = score_book(book, "calculus")
        # Topic match (5) + word overlap "calculus" (3)
        assert score >= 3

    def test_category_match(self):
        book = _book("Some Book", category="Physics", topics=[])
        score = score_book(book, "physics")
        assert score >= 1

    def test_no_match(self):
        book = _book("Cooking Recipes", category="Food", topics=["cooking"])
        score = score_book(book, "quantum field theory")
        assert score == 0

    def test_partial_word_overlap(self):
        book = _book("Linear Algebra Done Right", topics=["linear algebra", "algebra"])
        score = score_book(book, "algebra")
        # Topic match for "algebra" (5) + topic match for "linear algebra" (5)
        # + word overlap "algebra" (3)
        assert score >= 8

    def test_case_insensitive(self):
        book = _book("QUANTUM MECHANICS", topics=["quantum mechanics"])
        score_upper = score_book(book, "QUANTUM MECHANICS")
        score_lower = score_book(book, "quantum mechanics")
        assert score_upper == score_lower


class TestRankBooks:
    def _library(self) -> list[Book]:
        return [
            _book("Quantum Mechanics", category="Physics", topics=["quantum mechanics", "physics"]),
            _book("Classical Mechanics", category="Physics", topics=["mechanics", "physics"]),
            _book(
                "Organic Chemistry", category="Chemistry",
                topics=["chemistry", "organic chemistry"],
            ),
            _book("Python Programming", category="CS", topics=["programming", "python"]),
            _book("Cooking with Fire", category="Lifestyle", topics=["cooking"]),
        ]

    def test_top_results_ordered_by_score(self):
        results = rank_books(self._library(), "quantum mechanics")
        assert len(results) > 0
        # "Quantum Mechanics" should be the top result
        assert results[0]["title"] == "Quantum Mechanics"
        # Scores should be descending
        scores = [r["score"] for r in results]
        assert scores == sorted(scores, reverse=True)

    def test_zero_score_excluded(self):
        results = rank_books(self._library(), "quantum mechanics")
        for r in results:
            assert r["score"] > 0
        # "Cooking with Fire" should NOT appear
        titles = {r["title"] for r in results}
        assert "Cooking with Fire" not in titles

    def test_top_n_limit(self):
        results = rank_books(self._library(), "physics", top_n=2)
        assert len(results) <= 2

    def test_no_results(self):
        results = rank_books(self._library(), "underwater basket weaving")
        assert results == []

    def test_result_contains_score_field(self):
        results = rank_books(self._library(), "python")
        assert len(results) > 0
        assert "score" in results[0]
        assert isinstance(results[0]["score"], int)

    def test_result_contains_book_fields(self):
        results = rank_books(self._library(), "python")
        r = results[0]
        assert "title" in r
        assert "path" in r
        assert "category" in r
        assert "topics" in r
