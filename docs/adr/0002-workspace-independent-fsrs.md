# 0002. Workspace-Independent FSRS Invocation

## Status

Accepted

## Context

Generated study workspaces contain lesson, practice, notes, and review data. They
do not contain the skill's support scripts. Invoking FSRS through a relative
`scripts/fsrs/fsrs` path from a workspace therefore depends on files that are not
created during `study init`.

## Decision

Resolve the installed study skill directory first, then invoke
`<skill-dir>/scripts/fsrs/fsrs` while keeping `FSRS_STORE` pointed at the current
workspace's `.fsrs/cards.json`.

## Consequences

Each workspace owns only its review data. The scheduler implementation remains
centralized with the installed skill, so upgrades and fixes apply to every
workspace without copying binaries.
