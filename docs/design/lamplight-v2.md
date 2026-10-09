# Lamplight v2 design

> This copy is from 1 October 2026. The design has changed since, and the current one is on
> the [`v2` branch](https://github.com/mordor-forge/study-skill/blob/v2/docs/design/lamplight-v2.md).
> One change: Lamplight v2 does not use NotebookLM (ADR-0011 there).

Lamplight is an interactive tutor for one learner. v2 turns the v1 `study` skill into a Go
program that owns all study state, with a thin skill that teaches through it. Terms in
**bold** are defined in [CONTEXT.md](../../CONTEXT.md). Lasting decisions are recorded in
ADR-0004 to ADR-0009.

This version incorporates two design reviews (architecture, and the learner's journey) and
the maintainer's answers to the questions they raised.

## Principles

- The core owns state and rules; the agent owns judgment and teaching.
- Content lives in plain files; the History records what happened; everything else is
  computed or rebuildable.
- The learner always sees where they are, the Next step, and what "done" means.
- Nothing silently changes the learner's Syllabus: changes are proposed, then approved.
- Show forecasts and one next action, never counts of what is late. No streaks, no guilt.
- Agent input is untrusted: agent-written code runs only inside the agent's sandbox.
- The core has no dependency on any single agent, knowledge service or companion skill.

## Architecture

```
            learner                         agent (any harness)
               │                             │               │
        CLI (study …)               lamplight skill     MCP (study mcp)
               │                   (teaching only)           │
               └──────────────┬──────────────────────────────┘
                              ▼
                     Lamplight core (Go)
   topic · syllabus · lesson · history (event log) · card (go-fsrs) · check
   workbench · forecast · checkpoint (git) · library · knowledge seam
                              │
             plain files in the Study home + rebuildable indexes
```

- **Core**: one Go module of deep modules with small interfaces. The adapters are thin.
- **Adapters**: the CLI and the MCP server expose the same domain operations, except that
  Checks run only through the CLI (ADR-0009). An HTTP API for the dashboard comes in a later
  point release.
- **MCP server instructions**: the rules that must never drift from the binary (write
  tools name their Topic, never edit state files, record a Next step, run Checks through the
  CLI, never show overdue counts) are sent as the server's instructions, so even an outdated
  skill gets them.
- **Skill**: one skill, `lamplight`, which only teaches (see "The skill" below). It writes
  teaching material directly but never edits Lamplight's state files.
- **Knowledge**: a per-Topic seam that returns Evidence (ADR-0007).

## The Study home on disk

```
~/study/                            Study home (STUDY_HOME); not a git repository
  learner.md                        Learner profile
  .lamplight/                       local state: most recent Topic, write markers,
                                    Library index, caches
  .templates/<name>/                optional learner-provided Workbench starters
  llm-data-engineering/             a Topic: its own git repository
    topic.toml                      Goal, Pace periods, Level, Approach, Workbench,
                                    Knowledge base, Sources, Tasks
    syllabus.toml                   Milestones (priority, target date) and Lessons
                                    (id, title, hour estimate), in order
    lessons/<lesson-id>.md          Lesson text; YAML header holds the Check and Break points
    cards.jsonl                     Card content, sorted by ID
    history.jsonl                   Events, append only
    learner.md                      optional per-Topic additions to the Learner profile
    notes/                          Session notes, Assessments, research briefs
    teacher/                        Teacher's notes: answer keys, expected scores
    practice/<lesson-id>/           exercise work on the Workbench
    .heldout/<lesson-id>/           Held-out data: synthetic or public only
    .gitattributes                  history.jsonl merges by union
```

- Global config: `$XDG_CONFIG_HOME/lamplight/config.toml`; environment variables such as
  `STUDY_HOME` override it.
- **IDs** are short slugs that never change (`pii-in-cli-logs`, or `lesson-01` for imported
  v1 Lessons). Display numbers ("Lesson 2.3", "Card 4") come from position, so a Revision
  never renames files. Card IDs are `<lesson-id>.<random suffix>`, and Explore Cards use
  `explore.<random suffix>`, so two machines adding Cards never pick the same ID.
- **Status is never stored.** Lesson status, Topic status (active, paused, finished), the
  Resume point, completed Tasks and Card scheduling are computed by replaying the History
  (ADR-0005). go-fsrs is deterministic, so every replay gives the same schedule. Content
  files are authoritative for text, so a hand edit always wins; the History is authoritative
  for status, scheduling and approvals.
- **Writes** take a per-Topic lock and leave an intent marker in the Study home's
  `.lamplight/`. The Event is written first, with everything needed to apply it and, for each
  item it edits (one Card, one Lesson file, the Syllabus), a hash of the item before and
  after. Content files are then replaced atomically and the marker cleared. Every operation
  is idempotent. Recovery is described below.
- **Events** carry a unique ID and a time from a hybrid logical clock: the later of the wall
  clock and the latest time already in the History, plus one tick, so an Event written after
  another was read always sorts after it, whichever machine's clock is ahead. Replay orders
  Events by time, then by ID, never by position in the file, and applies each ID once. An
  Event that refers to an item not yet known (a Review of a Card whose creation hasn't
  arrived) is held, and reported in `status` if it never resolves. Each Event is one append
  ending in a newline; a last line without one is an interrupted write, which replay
  ignores and the next write truncates (logging the fragment).
- **Formats**: every file has a `format` number, and a binary refuses to write a file newer
  than it understands. File schemas are Lamplight's own types, never go-fsrs structs. Files
  the core rewrites say in a header comment that comments are not preserved.
- **Sync**: v2.0 supports using a Topic on one machine at a time, synced through git between
  sessions. History files merge by union, which can leave lines in any order; because
  replay sorts Events, a merge in either direction gives the same state. Conflicting changes
  made on two machines anyway (two edits of one Card, a delete and a Review, a Revision whose
  base no longer matches) are flagged in `status`, never resolved automatically, and textual
  conflicts in content files such as `cards.jsonl` are resolved by hand.

**Recovering an interrupted write.** Because of the lock, at most one Event can be unapplied
after a crash, and recovery inspects only the one named by the intent marker, comparing each
item it edits:

| The item matches | Meaning | Recovery |
|---|---|---|
| the `before` hash | the write never happened | apply the Event |
| the `after` hash | the write finished | clear the marker |
| neither | edited by hand since | keep the edit, log it, never overwrite |
| nothing (missing or unparseable) | the file is corrupt | stop with a clear error, keep the Event |

Hand edits to text need no acknowledgement. For content that approves or gates something
(the Syllabus and each Lesson's Check), the version recorded by the last Event is compared on
load, and a mismatch is flagged in `status`.

## Domain behaviour

### Creating a Topic

Topic creation can stop and resume at any step, because a first session that runs for
hours before any learning happens is exactly what v1 produced.

1. Energy check. At fumes, suggest coming back later.
2. Brainstorm the Goal (deadline optional), Pace periods, Approach and Workbench kind.
3. Add Sources: files from the Library or URLs. Choose the Knowledge base: `notebooklm` or
   `none`.
4. Assessment, time-boxed to about 15 minutes. Areas not reached are marked "confirm during
   Lessons". The result is saved in `notes/` and sets the Level.
5. The agent drafts the Syllabus: Milestone 1 in detail, later Milestones as outlines with
   their outcomes and priorities. The learner approves a short summary.
6. The agent sets up the Workbench with the ecosystem's own tools (`go mod init`,
   `cargo new`, `uv init`, `npm create`, or a folder for written work). It asks before
   installing any toolchain.
7. Session 1 ends at a Break point of Lesson 1, or with a written Next step.

### Syllabus, Forecasts and Revisions

- Syllabus → Milestones → Lessons. Each Milestone has an outcome, a priority (must, if time
  allows, after the deadline) and an optional target date. Each Lesson has an hour estimate
  and cites Evidence where available.
- The **Forecast** projects when each Milestone ends at the current Pace ("at 10 h/week,
  Core ends Oct 16"). If a must-Milestone misses its deadline, the agent offers a **Triage**
  Revision: trim Stretch goals, move Lessons past the deadline, or raise the Pace. A
  Pace change produces a new Forecast.
- **Revisions**: the learner asks in plain words; the agent proposes a before/after change
  that names Lessons by title and shows any renumbering; the core applies it after approval.
  Done and skipped Lessons are never rewritten. Skipping a Lesson in progress offers Cards
  for what was already covered.
- **Approvals are tamper-evident, not tamper-proof.** A Revision stores its exact change and
  the Syllabus version it was based on. Where the client supports MCP elicitation, or on a
  terminal, the core asks the learner directly; the Event records how approval was given.
  `status` flags Syllabus edits made outside Lamplight.
- An **Assessment** at the end of a Milestone never blocks progress; weak results lead to a
  proposed Revision (for example a review Lesson).

### Sessions

1. `status` comes first. It shows the Active topic and why it was chosen, the Resume point
   and its Next step word for word, one recommended action, Cards sized to the Energy
   (never the total due), Forecasts, and any relevant Tasks.
2. Energy check (full, half, fumes) suggests a Focus, and the learner chooses:
   - **Learn**: start the next Lesson.
   - **Practice**: continue the current exercise from the last Break point.
   - **Reviews**: due Cards only, capped by Energy.
   - **Explore**: free questions; useful answers can become Cards or a Revision proposal.
   With nothing due at fumes, the offer is "write tomorrow's first step".
3. The Learner profile and the Topic's additions are read at the start of every Session.
4. A Lesson moves through its Phases: teaching → practicing → feedback. The Check's criteria
   are shown before practicing starts. A Checkpoint is taken at every turn switch, with
   `[agent]` or `[learner]` authorship.
5. Reaching a Break point, or ending a Session, records a Next step (starting with a verb)
   and free-text context. If the learner simply closes the terminal, the next Session sees
   the unclosed Session, shows what changed since the last Checkpoint, and asks for the
   missing note.
6. Completing a Lesson is one idempotent operation: mark it done, save its draft Cards,
   record the Event, take a Checkpoint. It is allowed only when the completion rule under
   Checks holds, and its Event records the Attempt and Check version it relied on, so later
   changes to shared code or to the Lesson never reopen it.
7. After a long gap: a short recap of where the Topic stands and a two-minute warm-up, never
   the size of the backlog.

### Checks

- A Check is a list of criteria, each of one kind:
  - **run**: a command that can be repeated (tests, a build, a script);
  - **rubric**: an item graded by the agent, shown before the work starts; the learner
    checks themselves first;
  - **held-out**: an evaluation on Held-out data. In v2.0 it is diagnostic: it never blocks
    completion, and its scores feed the Level signals.
- Checks run only through `study check <lesson>` in the agent's own shell (ADR-0009).
  Commands are argument lists, not shell strings. The core passes `STUDY_LESSON` and
  `STUDY_HELDOUT_DIR`, and reads per-criterion scores from a JSON results file.
- Each run is an **Attempt**, recorded with the Lesson, a hash of the Check's criteria (its
  version), a snapshot hash of `practice/<lesson-id>/` computed with the same filter-free git
  commands as Checkpoints, per-criterion scores, and an outcome: `passed`, `failed` (a valid
  results file) or `errored` (no or invalid results, or the command crashed).
- **Completion rule**: a Lesson can be completed when, for the current Check version, every
  run criterion passed on an Attempt whose snapshot matches the current work, and every
  rubric item has a grade. Changing the work or the criteria after a pass means running the
  Check again.
- **Held-out runs**: the first run that produces results is the counted measurement; later
  runs are recorded as "not counted". The count is kept per Lesson and criterion, so editing
  the Check can't create a fresh first run, and an `errored` run never uses it up. Only the
  results file is shown for held-out criteria, never raw output, so the test data doesn't
  leak.
- A failed Attempt sends the Lesson from feedback back to practicing, with a Next step that
  names the fix.
- Per-Lesson criteria allow Lessons with special needs (a GPU, a container) and project
  Topics where one codebase grows across Lessons.
- Written work can be submitted as typed final answers or a photo of paper work.

### Cards and Reviews

- Cards are single concepts with a prompt and an expected answer, written from a Lesson or
  an Explore Session, linked to Evidence where possible.
- **Drafts**: a new Card is a draft until its first Review, where the learner keeps, edits or
  drops it. A daily cap limits how many new Cards appear.
- The skill's card-writing rules: one fact per Card, no lists, no answer in the prompt, no
  trivia, at least one Card from the learner's own mistakes.
- Cards can be added, edited, suspended and deleted; `study review` has a key to flag one.
- Reviews work with the agent (conversational recall) or without it (`study review` in the
  terminal). Paused Topics hide their Cards; finished Topics keep reviewing at growing
  intervals.

### Level, Goal, Pace and Tasks

- The Level is set at the Assessment and can be changed by the learner at any time; an
  override holds until the next Assessment.
- v2.0 records every signal a future Level suggestion needs: Checks passed on the first try,
  hints requested, feedback rounds, the gap between dev and Held-out scores, and Review
  results. Suggestions come later, once there is data to tune them.
- Tasks are non-study steps toward the Goal. They appear in `status` when relevant and are
  marked done through the core.

## Knowledge

- **v2.0** (ADR-0007): Knowledge base kind `notebooklm` or `none`. Evidence is an exact quote
  plus a location when known, with where the location came from. Sources are files (by path
  plus content hash) or URLs. The NotebookLM login is checked when a Session opens; Lessons
  without Evidence are marked, never blocked.
- **Later**: Knowledge base plugins are MCP servers implementing Lamplight's fixed contract
  (add a Source, search for Evidence, list Sources). The first is a generic local RAG
  plugin: layout-aware conversion (Docling), hybrid keyword and embedding search, reranking.

## Library

The Python catalog is ported to Go. One normalizer is shared by indexing and searching,
topics match as whole words (fixing the "C" and "algorithms" ranking bugs), and results are
typed. The index is rebuildable data in the Study home; ADR-0003's scanning rules still
apply. Search results carry an absolute path. Conversion leaves the Library.

## Interfaces

### MCP tools

Every write names its Topic. Tools are named after things that happen in the domain.

- **Read**: `status`, `syllabus`, `lesson`, `due_cards`, `history`, `check_results`,
  `library_search`.
- **Topics**: `topic_create`, `topic_update` (Goal, Pace, Level, Approach, Knowledge base,
  Tasks, pause, finish), `task_done`, `assessment_record`, `source_add`, `evidence_record`.
- **Syllabus**: `revision_propose`, `revision_apply`.
- **Sessions**: `session_open`, `session_close`, `phase_set`, `break_point_reached`,
  `checkpoint`, `hint_record`, `rubric_record`, `lesson_complete`.
- **Cards**: `card_add`, `card_edit`, `card_suspend`, `card_delete`, `review_record`
  (including keep, edit or drop for drafts).

Opening a Session on a Topic also makes it the most recent Topic; there is no separate
switch tool.

### CLI

- The same operations, plus `study check`, following the `cli-creator` conventions: nouns
  then verbs, `--json` everywhere (JSON on stdout only, diagnostics on stderr), documented
  success and error shapes, exit 0 on empty results, `--dry-run` on writes, bounded
  `--limit`.
- `study` alone prints `status`. `study review` runs terminal Reviews. `study doctor --json`
  works even when setup is broken.
- Human output via cobra, Charm's fang and lipgloss; colour turns off with `NO_COLOR` or when
  piped. `study completion install` writes bash, zsh and fish completions; packages ship them
  system-wide.

### Wording

Glossary terms are used everywhere, except in learner-facing text for other users, where
"test set", "saved" and "citation" replace Held-out data, Checkpoint and Evidence. A learner
asking to "review my code" means feedback, not Reviews.

### Logging

`log/slog` everywhere; `charmbracelet/log` for terminal output; JSON lines under
`$XDG_STATE_HOME/lamplight/`; level from `--log-level` or `STUDY_LOG`. In MCP mode nothing is
logged to stdout.

## Security

ADR-0009: Checks run only inside the agent's sandbox, and the core never runs a program that
repository configuration names (see Checkpoints). The core treats agent input as untrusted:
Topic files are accessed through Go's `os.Root` (Go 1.24+), IDs are validated, and child
processes never inherit the MCP server's stdin. `.git/config` lives inside the Topic and the
agent can edit it, so the guarantee is that the core executes nothing it names, not that the
configuration is trusted. Sources and caches stay local, except what the learner sends to
NotebookLM.

## Checkpoints

Each Checkpoint is a git commit made by the core with low-level commands that cannot run
programs named in the repository's configuration: `hash-object -w --no-filters`,
`update-index --cacheinfo`, `write-tree`, `commit-tree --no-gpg-sign` and `update-ref`, run
with `GIT_CONFIG_GLOBAL=/dev/null`, `GIT_CONFIG_NOSYSTEM=1`, `core.hooksPath=/dev/null` and
`core.fsmonitor=false`. Diffs for the large-file warning use `--no-textconv --no-ext-diff`.
This rules out hooks, clean and smudge filters, fsmonitor, textconv, external diff drivers
and signing programs. The commit author is read from the learner's git configuration
beforehand and passed explicitly. The cost is that Checkpoints store raw bytes, so
line-ending conversion and Git LFS are not applied, which is acceptable for study Topics.

A Checkpoint skips empty commits, refuses to commit during a merge, a rebase or on a
detached HEAD, retries when an editor holds git's lock (`GIT_OPTIONAL_LOCKS=0`), and warns
before committing large files. The default `.gitignore` covers data and model artefacts
(Parquet, DuckDB, GGUF, safetensors, PyTorch checkpoints).

## Distribution and setup

ADR-0008. `study` ships for Linux and macOS through a Homebrew cask tap, the AUR, deb and
rpm packages, and `go install`. `study setup` installs the `lamplight` skill and registers
the MCP server at user scope for Claude Code and Codex, never overwrites a folder it did not
create, and can be reversed exactly with `--remove`. Claude Code can instead use the
marketplace plugin, which includes a session-start hook that prints `status` when the agent
starts inside the Study home. Other agents use `npx skills add` and a documented MCP snippet.

## Migration from v1

v1 stays installed as the `study` skill and keeps working. The LLM data engineering Topic
stays on v1 until after 13 October. Until the learner switches, v2's skill is installed only
for testing, against a separate Study home, so "let's study" keeps reaching v1.

1. `study import <v1-dir> [--dry-run]` copies the workspace, git history included, into the
   Study home and leaves the original untouched. It keeps v1 folder and Lesson names as IDs
   (`lesson-01`), so paths quoted in Lesson text and in `.gitignore` keep working. It moves
   `lessons/plan.md` to `notes/v1-plan.md`, converts `.study-config.json` into `topic.toml`,
   and maps `sources` and `notebooklm` to the Knowledge base. It records one `imported` Event
   plus the Lesson completions it can prove. v1's lesson-level cards are dropped.
   `--dry-run` lists everything that will be dropped.
2. An adoption Session works through a checklist: Goal and deadline, Pace periods, Syllabus
   from `notes/v1-plan.md` (the three tiers become three Milestone priorities), a Check for
   each open Lesson, the Knowledge base, Cards for completed Lessons, and the Next step from
   v1's `pending_action` and `context`. The learner approves the result as a Revision.
3. Acceptance test: `~/study-workspaces/c` and `~/study-workspaces/llm-data-engineering`
   import and resume exactly where they stopped. Automated tests use sanitised copies,
   because the real workspaces contain work-related content.

## The skill

`skills/lamplight/` holds the teaching method, with separate references for: brainstorming
and Assessment, drafting a Syllabus, the lesson loop and feedback, writing Cards, and Reviews.
It carries v1's teaching material over explicitly:

- teaching rules, including "never write the learner's implementation";
- the Lesson template: Concept, Key points, Reference example (don't copy), Common pitfalls,
  Exercise, Break points, Check, Stretch goals;
- numbered menus after a Lesson and after feedback;
- guidance for writing at each Level;
- the time budget question at the start of a Session;
- KaTeX for maths and physics;
- optional companions (visual explainers, domain packs, live-docs tools) suggested when
  installed, never required.

## Repository

The repository is renamed to Lamplight; v1 is tagged and kept on a `v1` branch; v2 is
written on main.

```
cmd/study/                  entry point
internal/…                  core modules, and the CLI and MCP adapters
skills/lamplight/           the skill and its references (embedded in the binary)
plugin/                     Claude Code plugin manifest and hooks
.claude-plugin/             marketplace.json
docs/adr/, docs/design/, CONTEXT.md
web/                        dashboard, in a later point release
```

`templates/`, `scripts/fsrs` and `scripts/catalog` are removed.

## Testing

- Tests go through each module's interface. Core tests run in process with a fixed clock
  and a temporary Study home.
- Replay: the same History always yields the same state and Card schedule.
- Crash injection: interrupt every write between the Event and the content update, and cut
  an Event off mid-line, then check that replay and the next write repair it. An Event that
  was applied before a hand edit leaves the edit intact on reload.
- Concurrency: the CLI and the MCP server writing to one Topic at once.
- Sync: two machines' History files, merged by union in both directions, replay to the same
  state with no learning record lost, including a Card created and reviewed on a machine
  whose clock is ahead, both machines adding Cards to one Lesson, and conflicting Revisions.
- Checkpoints: a Topic whose configuration routes files through a filter script, or sets
  `commit.gpgsign` with a `gpg.program`, is checkpointed through MCP without running either.
- Checks: passing and then changing the work or the criteria blocks completion; a completed
  Lesson stays complete when later Lessons change shared code; the held-out journey from a
  first failure through feedback to completion, and an `errored` run that leaves the counted
  run unused.
- The MCP server through the Go SDK's in-memory transport; the CLI by comparing `--json`
  output with saved expected files.
- `study setup` against fake `claude` and `codex` executables.
- **The learner loop**, built right after the tracer bullet and the History engine: a
  scripted agent drives the core over the in-memory MCP transport through teaching, an
  Attempt that fails, feedback, a fix, completion, a Card Review, a crash during completion
  and a resume, asserting on the History and the replayed state rather than on transcripts.
  The same loop is run by hand with Claude Code and Codex before release.
- The import against sanitised copies of the two v1 workspaces.

## Scope

**v2.0**: the core (Topics, Syllabus with Forecasts, Triage and Revisions, Lessons with
Break points and Next steps, Checks with Attempts and diagnostic held-out results, Cards
with drafts and Reviews, History with replay, Checkpoints, Assessment, Goal, Pace and Tasks,
recording Level signals); sync for one machine at a time; the Library in Go; the CLI with
terminal Reviews; the MCP server; the `lamplight` skill; the Claude Code plugin and
`study setup` for Claude Code and Codex; Linux and macOS packages; `study import`.

**Order of work**: the tracer bullet, then the History engine (its file formats are settled
before any real data is written), then a thin learner loop through every layer, then each
module deepened.

**Later**: the dashboard (Vue 3, TypeScript, shadcn-vue; home network with a login, or
Tailscale; read-mostly first), Knowledge base plugins and the generic RAG plugin, Level
suggestions, held-out results that can block completion (with fresh, reviewed test sets for
a retry), using one Topic on several machines at once, Windows packages, the Agent Plugins
1.0 manifest, setup for more agents, reading tables of contents from PDFs, retrieval from
page images, the Journal, the FSRS optimizer, publishing to the MCP Registry.

## Known risks

- NotebookLM access is unofficial and can break without notice.
- Approvals are tamper-evident only: an agent with a shell can still edit files.
- Agents that read both `~/.agents/skills` and `~/.claude/skills` may list the skill twice.
- The session-start `status` is automatic only where the agent supports hooks.
- Comments in files the core rewrites are lost.
- Sync assumes one machine at a time: concurrent edits are flagged, not merged.
- Checkpoints store raw bytes, so line-ending conversion and Git LFS don't apply.
