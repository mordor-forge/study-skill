# 0001. Repository Structure

## Status

Accepted

## Context

The study skill combines Markdown instructions, workspace templates, a Go FSRS
scheduler, and a Python catalog tool. Generated study workspaces should not need
copies of the skill's implementation scripts.

## Decision

Keep the skill orchestration in root Markdown files, keep reusable guidance under
`references/`, keep copied starter projects under `templates/`, and keep executable
support tools under `scripts/`.

The Go FSRS binary is built inside the installed skill directory and invoked from
workspaces via a skill-relative path. The Python catalog remains a separate uv
project with its own lock file.

## Consequences

Agents and maintainers can reason about the skill instructions independently from
the support tools. Generated workspaces remain small and portable, while shared
tooling stays centralized in the installed skill.
