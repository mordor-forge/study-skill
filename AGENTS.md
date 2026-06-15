# Agent Guide

## Project Overview

This repository contains the `study` Agent Skills-compatible tutor. It defines an
interactive learning workflow in `SKILL.md`, reusable reference instructions in
`references/`, starter workspaces in `templates/`, a Go FSRS scheduler in
`scripts/fsrs`, and a Python book catalog tool in `scripts/catalog`.

## Development Commands

Use the root `Makefile` as the stable entrypoint:

```bash
make setup        # install Python catalog dev dependencies
make test         # run Python catalog and Go FSRS tests
make lint         # run ruff and gofmt verification
make typecheck    # run Python type checks
make coverage     # run tests with coverage output
make build        # build scripts/fsrs/fsrs
```

Single-file or narrow checks:

```bash
cd scripts/catalog && uv run ruff check src/catalog/scanner.py
cd scripts/catalog && uv run pyright src/catalog/scanner.py
cd scripts/catalog && uv run pytest tests/test_scanner.py
cd scripts/fsrs && go test ./internal
cd scripts/fsrs && go test ./cmd/fsrs -run TestCLIAddCard
```

## Code Conventions

- Keep skill behavior in Markdown instructions unless executable code is needed.
- Keep generated study workspaces independent from the installed skill directory.
- Python catalog code targets Python 3.11+, uses Pydantic models, and is linted by
  Ruff.
- Go FSRS code targets Go 1.22+ and should remain deterministic and JSON-oriented.
- Add focused regression tests for scanner, converter, matcher, CLI, and scheduler
  changes.
- Prefer typed public function signatures in `scripts/catalog/src`.
- Use conventional commit prefixes such as `fix:`, `feat:`, `docs:`, `test:`,
  and `ci:`.
- Do not commit generated binaries, local study workspaces, or agent assessment
  reports.

## Important Paths

- `SKILL.md`: command orchestration and user-facing study workflow.
- `references/fsrs-spaced-repetition.md`: FSRS invocation and review protocol.
- `references/agent-adapters.md`: Claude Code, Codex, Hermes, Cline, OpenCode,
  and generic client setup.
- `references/workspace-lifecycle.md`: init/start/break/resume state contract.
- `scripts/fsrs`: Go scheduler, store, and CLI.
- `scripts/catalog`: Python catalog scanner, matcher, converter, and CLI.
- `templates`: files copied into newly initialized study workspaces.

## Patterns

- Scanner changes: update `scripts/catalog/src/catalog/scanner.py` and add
  focused cases in `scripts/catalog/tests/test_scanner.py`.
- Catalog CLI changes: exercise `main()` through argv in
  `scripts/catalog/tests/test_cli.py`.
- FSRS changes: keep CLI behavior JSON-compatible and add Go tests under
  `scripts/fsrs/internal` or `scripts/fsrs/cmd/fsrs`.
- Skill workflow changes: update `SKILL.md` and any deeper protocol file under
  `references/` together.
