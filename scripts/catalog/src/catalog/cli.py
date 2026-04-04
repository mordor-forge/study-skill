"""CLI entry point for the book catalog builder."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

DEFAULT_CATALOG_PATH = Path.home() / ".config" / "study" / "book-catalog.json"


def _build(args: argparse.Namespace) -> None:
    """Build a catalog from a library directory."""
    from catalog.scanner import scan_directory

    library_path = Path(args.library_path)
    output_path = Path(args.output) if args.output else DEFAULT_CATALOG_PATH

    if not library_path.is_dir():
        print(f"Error: {library_path} is not a directory", file=sys.stderr)
        sys.exit(1)

    print(f"Scanning {library_path} ...", file=sys.stderr)
    catalog = scan_directory(library_path, extract_metadata=args.metadata)
    print(f"Found {len(catalog.books)} books", file=sys.stderr)

    catalog.save(output_path)
    print(f"Catalog saved to {output_path}", file=sys.stderr)


def _search(args: argparse.Namespace) -> None:
    """Search an existing catalog."""
    from catalog.models import Catalog

    catalog_path = Path(args.catalog) if args.catalog else DEFAULT_CATALOG_PATH

    if not catalog_path.is_file():
        print(f"Error: catalog not found at {catalog_path}", file=sys.stderr)
        print("Run 'study-catalog build' first.", file=sys.stderr)
        sys.exit(1)

    catalog = Catalog.load(catalog_path)
    results = catalog.search(args.query, top_n=args.top)

    # Output JSON array to stdout for the skill to parse
    json.dump(results, sys.stdout, indent=2, default=str)
    print(file=sys.stdout)  # trailing newline


def main() -> None:
    """Main CLI entry point."""
    parser = argparse.ArgumentParser(
        prog="study-catalog",
        description="Build and search a book catalog for the study skill",
    )
    subparsers = parser.add_subparsers(dest="command", required=True)

    # build subcommand
    build_parser = subparsers.add_parser("build", help="Build catalog from a library directory")
    build_parser.add_argument("library_path", help="Path to the book library root directory")
    build_parser.add_argument(
        "--output", "-o", default=None, help=f"Output path (default: {DEFAULT_CATALOG_PATH})"
    )
    build_parser.add_argument(
        "--metadata",
        "-m",
        action="store_true",
        help="Extract PDF metadata (page count, author) — slow for large libraries",
    )
    build_parser.set_defaults(func=_build)

    # search subcommand
    search_parser = subparsers.add_parser("search", help="Search the book catalog")
    search_parser.add_argument("query", help="Search query (topic, title, or keyword)")
    search_parser.add_argument(
        "--catalog",
        "-c",
        default=None,
        help=f"Path to catalog JSON (default: {DEFAULT_CATALOG_PATH})",
    )
    search_parser.add_argument(
        "--top",
        "-n",
        type=int,
        default=10,
        help="Number of results to return (default: 10)",
    )
    search_parser.set_defaults(func=_search)

    args = parser.parse_args()
    args.func(args)


if __name__ == "__main__":
    main()
