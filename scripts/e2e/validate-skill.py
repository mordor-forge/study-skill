"""Validate Agent Skills metadata used by Codex and compatible clients."""

from __future__ import annotations

import sys
from pathlib import Path
from typing import Any

import yaml


def _frontmatter(text: str) -> str:
    delimiter = "---\n"
    if not text.startswith(delimiter):
        msg = "SKILL.md must start with YAML frontmatter delimiter '---'"
        raise ValueError(msg)

    body = text[len(delimiter) :]
    end = body.find(delimiter)
    if end == -1:
        msg = "SKILL.md must close YAML frontmatter with '---'"
        raise ValueError(msg)

    return body[:end]


def validate(path: Path) -> None:
    """Validate the required skill frontmatter fields."""
    data = yaml.safe_load(_frontmatter(path.read_text(encoding="utf-8")))
    if not isinstance(data, dict):
        msg = "SKILL.md frontmatter must parse to a mapping"
        raise ValueError(msg)

    allowed = {"name", "description", "metadata"}
    extra = set(data) - allowed
    if extra:
        msg = f"Unsupported frontmatter fields for portable skills: {sorted(extra)}"
        raise ValueError(msg)

    name = data.get("name")
    description = data.get("description")
    if not isinstance(name, str) or not name.strip():
        msg = "SKILL.md frontmatter requires non-empty string field: name"
        raise ValueError(msg)
    if not isinstance(description, str) or len(description.strip()) < 50:
        msg = "SKILL.md frontmatter requires descriptive string field: description"
        raise ValueError(msg)

    metadata: Any = data.get("metadata", {})
    if metadata and not isinstance(metadata, dict):
        msg = "SKILL.md metadata field must be a mapping when present"
        raise ValueError(msg)


def main() -> int:
    path = Path(sys.argv[1]) if len(sys.argv) > 1 else Path("SKILL.md")
    try:
        validate(path)
    except Exception as exc:
        print(f"{path}: {exc}", file=sys.stderr)
        return 1

    print(f"{path}: valid skill metadata")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
