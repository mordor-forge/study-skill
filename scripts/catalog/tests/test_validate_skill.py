"""Tests for portable skill metadata validation."""

from __future__ import annotations

import importlib.util
from pathlib import Path
from types import ModuleType

import pytest


def _load_validator() -> ModuleType:
    repo_root = Path(__file__).resolve().parents[3]
    validator_path = repo_root / "scripts" / "e2e" / "validate-skill.py"
    spec = importlib.util.spec_from_file_location("validate_skill", validator_path)
    assert spec is not None
    assert spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def test_frontmatter_requires_closing_delimiter() -> None:
    validator = _load_validator()
    text = "---\nname: study\ndescription: missing closing delimiter\n"

    with pytest.raises(ValueError, match="close YAML frontmatter"):
        validator._frontmatter(text)


def test_frontmatter_returns_yaml_body() -> None:
    validator = _load_validator()
    text = "---\nname: study\ndescription: valid portable skill metadata\n---\n# Body\n"

    expected = "name: study\ndescription: valid portable skill metadata\n"
    assert validator._frontmatter(text) == expected


@pytest.mark.parametrize("metadata", ["[]", "false", "''"])
def test_validate_rejects_falsy_non_mapping_metadata(tmp_path: Path, metadata: str) -> None:
    validator = _load_validator()
    path = tmp_path / "SKILL.md"
    path.write_text(
        "---\n"
        "name: study\n"
        "description: Valid portable skill metadata with enough detail for clients.\n"
        f"metadata: {metadata}\n"
        "---\n"
        "# Body\n",
        encoding="utf-8",
    )

    with pytest.raises(ValueError, match="metadata field must be a mapping"):
        validator.validate(path)
