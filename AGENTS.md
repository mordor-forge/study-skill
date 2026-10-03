# Agent Guide

## Project Overview

This repository holds Lamplight v2: a Go program, `study`, whose core owns all study state
(Topics, the Syllabus, Checks and Attempts, Cards, the History) in plain files inside the
learner's Study home. A command line and an MCP server are thin adapters over the core, and
the `lamplight` skill, embedded in the binary, teaches agents to tutor with them. Until v2.0
is released it lives on the `v2` branch, and `main` keeps v1, the Markdown study skill.

Read these before changing behaviour:

- `CONTEXT.md`: the glossary. Use its terms in code, comments and docs.
- `docs/design/lamplight-v2.md`: the design.
- `docs/adr/`: the decisions behind it, 0004 onwards. 0001 and 0002 are superseded by 0004.
  0003 was written for v1's Python book catalog; the Go Library (`internal/library`), which
  replaced that catalog, keeps its walk-once and case-insensitive extension rules and
  replaces its title clean-up with one shared normalizer.
- `docs/cli.md`: the command-line contract that scripts and agents rely on. A test checks
  that it mentions every command and flag.

## Development Commands

Use the root `Makefile` as the stable entrypoint:

```bash
make test         # go test -race ./...
make lint         # gofmt check and go vet
make coverage     # tests with a coverage profile
make build        # build ./study
```

Narrow checks:

```bash
go test ./internal/core -run TestCreateTopic
go test ./internal/cli -run TestJSONOutput
go test ./internal/cli -update      # rewrite CLI golden files, then review the diff
go test ./skills/...                # the skill against the real tools and commands
go test ./internal/e2e              # the learner loop and the v2.0 acceptance walkthrough
go test -shuffle=on ./...           # no test may depend on another's order
GOOS=darwin go vet ./...            # macOS is supported too
```

## Code Conventions

- Go 1.26 or later (go-fsrs v4 needs it); the module is `github.com/mordor-forge/lamplight/v2`.
- Domain logic lives in `internal/core`. `internal/cli` and `internal/mcpserver` only
  parse input, call the core and render results.
- Core errors are `*core.Error` with a documented code (`invalid_argument`, `corrupt`,
  `busy`, …); adapters map codes to exit codes and MCP tool errors.
- Every write is an Event in the Topic's History, written first, then the content, under
  the Topic's lock. Event types are versioned by name: a new payload shape is a new type.
  Domain time is the Event's `wall`; `time` only orders Events.
- Conflicts between machines are flagged in `status`, never resolved silently.
- Files in a Topic are read and written through `os.Root`. git runs only through the
  hardened `internal/checkpoint` package or `gitCommand`, never with the caller's `GIT_*`
  environment, and never runs a program the repository's configuration names (ADR-0009).
- No MCP tool runs learner or agent code; Checks run only through `study check` in the
  agent's own shell (ADR-0009). A test enforces it.
- Every file Lamplight owns carries a `format` number, and newer formats are refused.
- Text from files or the History goes through `printable()` before it reaches a terminal;
  `--json` output never triggers terminal queries.
- The learner never sees overdue counts, "behind" or streaks: show where they are and one
  next action.
- Use conventional commit prefixes such as `fix:`, `feat:`, `docs:`, `test:` and `ci:`.
- Do not commit built binaries, Study homes or agent assessment reports.

## Tests

- Tests go through each package's public interface, with a fixed clock, injected IDs and
  a temporary Study home. CLI output is checked with golden files under `testdata/`.
- New Event types get crash-injection tests at every crash point, and a check that the dry
  run reports what the real run does.
- Use a temporary `HOME`, `XDG_*` folders and `STUDY_HOME`. Never touch the real
  `~/.agents`, `~/.claude`, `~/.codex`, `~/.config`, a real Study home, or the installed v1
  skill in `~/.agents/skills/study`.
- Tests of `study setup` and the Claude Code plugin run with a `PATH` holding only fake
  `claude` and `codex` scripts; a guard fails the test if the real ones could be reached.
  Never run the real agents with commands that change anything.
- Every git configuration a test writes includes `[maintenance]` `auto = false`: git's
  background maintenance would otherwise still be writing to `.git` while the test removes
  its folder. A test that writes a `.gitconfig` also points `GIT_CONFIG_GLOBAL` at it and
  sets `GIT_CONFIG_NOSYSTEM=1`, so git run by the test reads that file whatever the
  caller's environment says (the core strips `GIT_*` on its own).
- A package whose tests run git or the go command themselves clears the caller's `GIT_*`
  variables in `TestMain`: a commit hook that runs the tests sets `GIT_INDEX_FILE` and
  `GIT_DIR`, which point those commands at the repository being committed.
  `TestTheTestsIgnoreTheCallersGitEnvironment` checks it in each such package. Tests that
  build `study` pass `-buildvcs=false`, so the build never needs git to report on the checkout.
- Tests must pass with no git identity configured (CI has none), and in any order.

## Important Paths

- `cmd/study`: the entry point.
- `internal/core`: everything the learner's data goes through, behind one `Core`: Topics
  and `status`, the History engine (`history.go`, `replay.go`, `write.go`, `items.go`),
  the Syllabus and Revisions, Sessions and the Resume point, Checks and Attempts, Cards and
  FSRS scheduling (`schedule.go`), Sources and Evidence, Goal and Pace, Assessments and
  signals, the read views, and `study import`.
- `internal/checkpoint`: hardened git: Checkpoints, work snapshots for Attempts, changes
  since a Checkpoint, and reading history.
- `internal/library`: building and searching the Library index.
- `internal/cli`: the `study` command line (cobra and fang), including `study setup`.
- `internal/mcpserver`: the MCP server, its tools and its Instructions for agents.
- `internal/claudeplugin` and `.claude-plugin/marketplace.json`: the Claude Code plugin,
  generated by `study claude-plugin-path`.
- `internal/e2e`: end-to-end tests: the learner loop driven by a scripted agent, and the
  v2.0 acceptance walkthrough from `study setup` to an imported v1 Topic.
- `skills/lamplight`: the `lamplight` skill, embedded in the binary.
- `docs/release-checklist.md`: the maintainer's steps to ship v2.0.

## Patterns

- New operation: add it to the core with tests, then expose it in the CLI (with `--json`,
  and `--dry-run` for writes) and the MCP server, document it in `docs/cli.md`, and mention
  it in the skill if agents should use it. Some operations are CLI-only on purpose and get
  no MCP tool: `study check`, which runs code (ADR-0009), and what is for the learner or the
  installation alone, such as `study topic remove`, `study import`, `study setup`,
  `study doctor`, `study completion`, `study library build` and terminal `study review`.
- New file or Event format: bump nothing silently; add a `format` field and a test that
  newer formats are refused.
- Design changes: update `CONTEXT.md`, the design and a new ADR together.
