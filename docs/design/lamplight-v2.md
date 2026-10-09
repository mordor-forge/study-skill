# Lamplight v2 design

Lamplight is an interactive tutor for one learner. v2 turns the v1 `study` skill into a Go
program that owns all study state, with a thin skill that teaches through it. Terms in
**bold** are defined in [GLOSSARY.md](../../GLOSSARY.md). Lasting decisions are recorded in
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
                                    Library index, where Source files are, removed
                                    Topics, caches
  .templates/<name>/                optional learner-provided Workbench starters
  llm-data-engineering/             a Topic: its own git repository
    topic.toml                      Goal and deadline, Pace periods, Level, Approach,
                                    Workbench, Knowledge base, daily cap on new Cards
    tasks.jsonl                     Tasks: steps toward the Goal that are not study
    syllabus.toml                   Milestones (priority, target date) and Lessons
                                    (id, title, hour estimate), in order
    lessons/<lesson-id>.md          Lesson text; YAML header holds the Check and Break points
    cards.jsonl                     Card content, in the order written
    sources.jsonl                   Sources: files (Topic path or name, content hash) and URLs
    history.jsonl                   Events, append only
    learner.md                      optional per-Topic additions to the Learner profile
    notes/                          Session notes, Assessments, research briefs
    teacher/                        Teacher's notes: answer keys, expected scores
    practice/<lesson-id>/           exercise work on the Workbench
    .heldout/<lesson-id>/           Held-out data: synthetic or public only, committed
    .gitattributes                  history.jsonl and sources.jsonl merge by union
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
  is idempotent. Checkpoints take the same lock and finish an interrupted write first, so a
  Checkpoint never carries an Event without its content to another machine. Recovery is
  described below.
- **Items** are named relative to the Topic with forward slashes: `<path>` for a whole file
  (`topic.toml`, `syllabus.toml`), `<path>#<key>` for one entity inside a file
  (`cards.jsonl#<card-id>`, `lessons/<id>.md#check`). An entity's hash covers only that
  entity, in a canonical form its file's codec defines, so two machines adding different
  Cards to one file never conflict, and reformatting a file is not an edit.
- **Events** carry a unique ID and a time from a hybrid logical clock: the later of the wall
  clock and the latest time already in the History, plus one tick, so an Event written after
  another was read always sorts after it, whichever machine's clock is ahead. That time only
  orders Events; each Event also records `wall`, its writer's real clock, which domain logic
  such as Card scheduling uses. A History dated more than a day ahead of the current clock is
  flagged in `status`. Replay orders Events by time, then by ID, never by position in the
  file, and applies each ID once. An Event that refers to an item not yet known (a Review of
  a Card whose creation hasn't arrived) is held, and reported in `status` if it never
  resolves. Each Event is one append ending in a newline. A last line without one is either
  a complete Event whose newline an editor dropped, which is kept and repaired, or an
  interrupted append, which replay ignores and the next write or Checkpoint truncates
  (logging the fragment). A line anywhere else that is not an Event is skipped and flagged,
  never deleted.
- **Formats**: every file has a `format` number, and one without it is damaged. A binary
  reads a History with Events newer than it understands, holding them, but refuses to write
  to it or to any file newer than it understands. Settings it does not know in a file it
  rewrites are kept. An Event type's payload never changes shape: a new shape is a new type
  name. File schemas are Lamplight's own types, never go-fsrs structs. Files the core
  rewrites say in a header comment that comments are not preserved.

  ```json
  {"format":1,"id":"k3…","time":"2026-10-01T09:30:00.000001Z","wall":"2026-10-01T09:30:00Z",
   "type":"topic.updated","data":{"title":"C"},
   "items":[{"item":"topic.toml","before":"sha256:…","after":"sha256:…"}]}
  ```
- **Learning files.** `syllabus.toml` is rewritten only from approved Revisions; `cards.jsonl`
  holds one Card per line, `{"format":1,"id":"<lesson-id>.<suffix>","lesson":"…","prompt":"…",
  "answer":"…"}`, content only; a Lesson's Check is the YAML header of `lessons/<id>.md`,
  which the agent writes, and is the item `lessons/<id>.md#check`, versioned by its
  canonical JSON so editing the Lesson's text never changes it. Sessions, Phases, Attempts,
  Next steps, draft status and Card schedules have no files: they are Events, replayed.

  | Event | Payload | Items |
  |---|---|---|
  | `revision.proposed` | summary, the Syllabus version the History recorded, the whole new Syllabus; for a hand edit adopted, `from_file` and the file's version | none |
  | `revision.applied` | Revision, approval (how: `chat`, `elicitation` or `terminal`; the learner's words; the question shown when Lamplight asked), the whole Syllabus | `syllabus.toml` |
  | `revision.declined` | Revision, the learner's answer as for an approval | none |
  | `session.opened` | Energy, Focus | none |
  | `session.focused` | Session, the Focus the learner chose after the suggestion | none |
  | `session.closed` | Session, Next step, context, the Lesson it is about, the turn it ended | none |
  | `break_point.reached` | Lesson, Break point, Next step, context, the turn it ended | none |
  | `phase.set` | Lesson, Phase, optional Next step, the Check version shown when practicing starts, the turn it ended | none |
  | `attempt.recorded` | Lesson, Check version, snapshot, outcome, per criterion its kind, outcome, results file and, for `held_out` criteria, whether it was counted when run and why not (never output) | none |
  | `rubric.graded` | Lesson, rubric item, grade, note, the Check version and snapshot graded, the files of the work it looked at (path and hash), the grade it replaces | none |
  | `lesson.completed` | Lesson, the Attempt and the rubric grades it relied on, its Check version and the one shown, snapshot, the turn it ended, draft Cards in full | `cards.jsonl#<id>` each |
  | `review.recorded` | Card, rating, for a draft keep, edit (new content) or drop, and the client's optional request id | `cards.jsonl#<id>` on edit or drop |
  | `card.added` | the new Card in full, Evidence ids included | `cards.jsonl#<id>` |
  | `card.edited` | Card, the new prompt, answer or Evidence | `cards.jsonl#<id>` |
  | `card.suspended`, `card.unsuspended` | Card | none |
  | `card.deleted` | Card, how many of its Reviews the deleting machine knew | `cards.jsonl#<id>` (removed) |
  | `card.flagged` | Card, the learner's note | none |
  | `checkpoint.taken` | the Event whose Checkpoint it settles, role, commit | none |
  | `deadline.set` | the Goal's deadline, empty to remove it | `topic.toml` |
  | `pace.set` | the Pace periods (`from`, `hours_per_week`), replacing the old ones | `topic.toml` |
  | `new_cards_per_day.set` | the daily cap on new Cards | `topic.toml` |
  | `task.added`, `task.removed` | the Tasks in full, or their ids | `tasks.jsonl#<id>` each |
  | `topic_state.set` | `active`, `paused` or `finished`, and the state the writer saw | none |
  | `task.done`, `task.reopened` | Task id | none |

  `syllabus.toml` keeps settings Lamplight does not know at every level, as `topic.toml`
  does, and refuses a newer format. A Revision is based on the version the History
  recorded; while the file differs from it, Revisions are refused except one that adopts
  the hand edit (`from_file`), so a learner's edit is approved rather than overwritten.
  `cards.jsonl` merges by union like the History: a Card repeated with different content,
  a Lesson completed on two machines (whose Cards are all kept), a Review or an edit of a
  dropped or deleted Card, and a Revision removing a done Lesson are flagged.
- **Sync**: v2.0 supports using a Topic on one machine at a time, synced through git between
  sessions. History files merge by union, which can leave lines in any order; because
  replay sorts Events, a merge in either direction gives the same state. Conflicting changes
  made on two machines anyway (two edits of one Card, a delete and a Review, a Revision whose
  base no longer matches) are flagged in `status`, never resolved automatically, and textual
  conflicts in content files such as `cards.jsonl` are resolved by hand. Each flag has a
  stable ID; once the learner has looked at a conflict, a held Event, a damaged line or a
  clock warning, they can dismiss it, which records a `flag.dismissed` Event and changes no
  content.

**Recovering an interrupted write.** Because of the lock, at most one Event can be unapplied
after a crash, and recovery inspects only the one named by the intent marker, comparing each
item it edits:

| The item matches | Meaning | Recovery |
|---|---|---|
| the `before` hash, and no later Event changed it | the write never happened | apply the Event |
| the `before` hash, but a later Event changed it since | another machine superseded it | leave it, log it |
| the `after` hash | the write finished | clear the marker |
| neither | edited by hand since | keep the edit, log it, never overwrite |
| nothing (missing or unparseable) | the file is corrupt | stop with a clear error, keep the Event |

When the item matches `before` but this binary's applier writes different bytes from the
`after` hash (a newer binary formats a file differently), nobody edited the item, so the
Event is applied with a warning rather than blocking the Topic. Recovery also removes the
interrupted write's temporary files, which are hidden and covered by the Topic's default
`.gitignore`. A dry run plans against the Topic as recovery would leave it, without writing.

Hand edits to text need no acknowledgement. For content that approves or gates something
(the Syllabus and each Lesson's Check), the version recorded by the last Event is compared on
load, and a mismatch is flagged in `status`.

## Domain behaviour

### Creating a Topic

Topic creation can stop and resume at any step, because a first session that runs for
hours before any learning happens is exactly what v1 produced.

1. Energy check. At fumes, suggest coming back later.
2. Brainstorm the Goal (deadline optional), Pace periods, Approach and Workbench kind. The
   Approach is how the Lessons relate: `concepts` (standalone concepts, each with its own
   exercises), `project` (one project built step by step) or `challenges` (a run of
   challenges of growing difficulty). It lives in `topic.toml` as `approach`, set through
   `topic_update` (`approach.set`).
3. Add Sources: files from the Library or URLs. The Knowledge base is `none`, the only
   kind until Knowledge base plugins are built (ADR-0012).
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
  - The core computes both, exactly and deterministically: work is the hour estimates of
    the Lessons neither done nor skipped (one in progress in full, any estimate at least a
    minute), taken through the Syllabus in order and counted in minutes; the Pace is minutes
    per week, and each calendar day on the learner's clock, from today, adds its period's
    share. Days are civil dates, so daylight saving changes never add or lose one. A Lesson
    without an estimate stops the Forecast at its Milestone and names it, rather than
    guessing. A Milestone's deadline is its target date, or the Goal's deadline for a
    must-Milestone without one.
  - The Triage for the first must-Milestone forecast to end after its deadline offers only
    what can still finish it in time: the Pace that does (hours a week from today to the
    deadline, rounded up to a half hour, never above 168; hours today when the deadline is
    today), the fewest Lessons not started that fit it in time if moved past the deadline
    (optional Milestones' Lessons before it first, then its own from the last; never all its
    own work), and the hours of Stretch goals to trim (never more than half its own work,
    since Stretch goals are optional extras). When none can, it says so and suggests the
    date the Milestone is forecast to end. A Triage is something to consider, never the one
    recommended action. The core never changes the Syllabus or the Pace itself: the learner
    chooses, and the agent proposes the Revision or the Pace change.
  - Forecasts appear in `status` and `syllabus`; a paused or finished Topic has none.
- **Revisions**: the learner asks in plain words; the agent proposes a before/after change
  that names Lessons by title and shows any renumbering; the core applies it after approval.
  The core computes that change from the current Syllabus and the proposed one, so the
  agent never describes it from memory. Done and skipped Lessons are never rewritten: they
  keep their title, hours and Milestone, and a done Lesson cannot be skipped. A Lesson is
  skipped by marking it (`skipped = true`), never by removing it; it keeps its number, and a
  later Revision can take the skip back. Skipping a Lesson in progress offers Cards for what
  was already covered. Target dates are written as text, so no time zone can shift them.
- **Approvals are tamper-evident, not tamper-proof.** A Revision stores its exact change and
  the Syllabus version it was based on. Where the client supports MCP elicitation, or on a
  terminal, the core asks the learner directly; the Event records how approval was given and
  the question shown. Under the 2026-07-28 MCP protocol the question goes back in the tool's
  result (SEP-2322) with a signed request state that ties the answer to that exact question;
  under older protocols the server sends an elicitation request. Without either, the agent
  relays the learner's words (`chat`). A no is recorded too (`revision.declined`), and a
  declined Revision is never applied. The learner is asked only about a Revision that can
  be applied as it stands. `status` flags Syllabus edits made outside Lamplight, naming
  precisely what keeps one from being adopted. Two machines that change the Syllabus from
  one version are flagged, whether by Revisions or by an adopted hand edit, as is a Lesson
  completed on one machine and removed or skipped on the other.
- An **Assessment** at the end of a Milestone never blocks progress; weak results lead to a
  proposed Revision (for example a review Lesson). When a Milestone's last Lesson is done,
  `lesson_complete` returns `next` with the code `assess_milestone`, and `status`
  recommends `assess` for that Milestone, ahead of the Next step, until its Assessment is
  recorded or the learner deliberately moves on by starting a Lesson of another Milestone.
  Stopping does not end it: the Next step written then names the next Lesson's first step
  (or, when no Lesson follows, taking that Assessment), since the Assessment is cued on
  its own, and at the next Session the agent offers the Assessment first. Only an active Topic has one. It is the next thing to do, never
  something late.

### Sessions

1. `status` comes first. It shows the Active topic and why it was chosen, the Resume point
   and its Next step word for word, one recommended action, Cards sized to the Energy
   (never the total due), Forecasts, and any relevant Tasks. The recommendation is the
   Assessment of a Milestone just finished (`assess`), then the Next step when there is one,
   otherwise the next move in the Syllabus (plan it, start, continue,
   practice or go over feedback on the current Lesson), otherwise Reviews or exploring once
   every Lesson is done. A paused Topic's recommendation is instead to resume it or pick
   another Topic (`resume_topic`), an imported Topic's is to adopt it (`adopt`, see
   Migration from v1) until it has a Syllabus, and a finished Topic's is its Reviews, or `stop` when no
   Card is ready. A Triage is never the recommendation. For Cards, `status` says only whether
   Reviews are possible now, under the Topic's daily cap on new Cards, or when the next Card
   falls due; a paused Topic's Cards are never ready. How many is decided when a Session's
   Energy is known.
2. Energy check (full, half, fumes) suggests a Focus, and the learner chooses:
   - **Learn**: start the next Lesson.
   - **Practice**: continue the current exercise from the last Break point.
   - **Reviews**: due Cards only, capped by Energy.
   - **Explore**: free questions; useful answers can become Cards or a Revision proposal.
   With nothing due at fumes, the offer is "write tomorrow's first step"; without a
   Syllabus, it is to plan one. A paused Topic is never suggested for study, and a finished
   one only for its Reviews. `session_open` returns the suggestion when it gets an Energy
   and no Focus yet, as one value: a Focus, `plan`, `adopt`, `stop`, `resume_topic` or
   `assess`, the same words `status` recommends. `assess` is suggested at full and half
   Energy; at fumes the Assessment waits, and the suggestion is Reviews or `stop`. The
   suggestion is never recorded, only the Focus the learner chooses: `session_open` again
   with the open Session's id and that Focus records it on the same Session
   (`session.focused`) and opens nothing new.
3. The Learner profile and the Topic's additions are read at the start of every Session;
   `status` gives their paths when the files exist.
4. A Lesson moves through its Phases: teaching → practicing → feedback. The Check's criteria
   are shown before practicing starts, and the Check version shown is recorded. A Checkpoint
   is taken at every turn switch, with `[agent]` or `[learner]` authorship: practicing is
   the learner's turn, teaching and feedback the agent's, whatever the Lesson, so
   `phase_set` takes the Checkpoint of whoever's turn just ended. The History records that
   it is owed, and a `checkpoint.taken` Event that it was taken; one that failed or was cut
   off by a crash is taken by the next `phase_set`, `lesson_complete` or `checkpoint`.
5. Reaching a Break point, or ending a Session, saves the work with a Checkpoint of the turn
   it ends (owed like a turn switch's, never blocking the stop) and records a Next step
   (starting with a verb)
   and free-text context. Break points are declared in order under `break_points:` in the
   Lesson's YAML header and read apart from the Check, so a wrong type or value there never
   makes the Check unreadable (a YAML syntax error breaks the whole header, and `status`
   flags it). They are reached in the current Lesson only, so the Resume point's Lesson,
   Break point and Next step belong together. A Next step is checked without a language
   model: after any opening punctuation it starts with a letter, says what to act on (two
   words, or four characters in scripts written without spaces), carries no invisible
   formatting, and does not open with a word that introduces a description ("The parser
   is…", "I'm on…", "Done with…"); that word list is English, so other languages pass on the
   other rules. Going back to practicing after a failed Attempt can record one too, naming
   the fix. The Resume point is the first Lesson not done, its Phase, the last Break point
   reached in it and the latest Next step; a Lesson's completion clears a Next step that
   belonged to it, and a Next step that a merge brings in for a Lesson already done,
   skipped or removed never leads and is flagged. If the learner simply closes the
   terminal, the next Session sees every Session left unclosed, shows what changed since
   the last Checkpoint, and asks for the missing notes. The changes are listed the way a
   Checkpoint would see them, from hashes that are computed but not written, so listing
   them writes nothing to `.git` and runs no program the repository names (ADR-0009);
   Lamplight's own state files are left out. `session_close` naming an old Session records
   its note, which never replaces a Next step recorded in a newer Session.
6. Completing a Lesson is one idempotent operation: mark it done, save its draft Cards,
   record the Event, take a Checkpoint in the role of the turn it ends (the learner's when
   completing straight from practicing). It is allowed only when the completion rule under
   Checks holds, and its Event records the Attempt and Check version it relied on, so later
   changes to shared code or to the Lesson never reopen it.
7. After a long gap: a short recap of where the Topic stands and a two-minute warm-up, never
   the size of the backlog. `session_open` sets `long_gap` when the Topic was last worked
   on, by any Event, more than a week ago.

### Checks

- A Check is a list of criteria, each of one kind:
  - **run**: a command that can be repeated (tests, a build, a script);
  - **rubric**: an item graded by the agent, shown before the work starts; the learner
    checks themselves first;
  - **held_out**: an evaluation on Held-out data. In v2.0 it is diagnostic: it never blocks
    completion, and its scores feed the Level signals.
- Each criterion in the Lesson's YAML header has exactly one of `run` (an argument list),
  `rubric` (what to grade) or `held_out` (an argument list); the Check's version is the
  hash of its criteria, kinds included. `held_out` is the one spelling of the kind: the
  YAML key, the `kind` value and the JSON field, where both command kinds show their
  argument list as `command`. A Check needs a run criterion or a rubric item, since held-out
  results alone could never complete a Lesson.
- Checks run only through `study check <lesson>` in the agent's own shell (ADR-0009).
  Commands are argument lists, not shell strings, and run criteria run first. The core
  removes inherited `STUDY_` variables, then passes `STUDY_TOPIC`, `STUDY_LESSON` and
  `STUDY_RESULTS` to every command, and `STUDY_HELDOUT_DIR` to `held_out` commands only, so
  a run criterion, whose output is shown, never sees the Held-out data. A command may write
  a JSON results file to `STUDY_RESULTS`, in a folder of its own outside the practice
  folder so writing it never changes the work: `passed`, `score` out of `max`, named
  `metrics` and a `summary`, at most 64 KiB, validated, and recorded without any other
  field; `held_out` commands must write one with a `score`. The file is opened without
  following links or blocking, checked to be a regular file, and read through the size
  bound; reasons quote at most 80 characters of it. Each command runs in its own process
  group, which is stopped as a whole on a timeout or SIGTERM; a command that leaves
  programs running is errored. A program that leaves the group (`setsid`) escapes this,
  which the agent's sandbox must contain. Long runs report progress on stderr.
- Each run is an **Attempt**, recorded with the Lesson, a hash of the Check's criteria (its
  version), a snapshot hash of `practice/<lesson-id>/` computed with the same filter-free git
  commands as Checkpoints, per-criterion scores, and an outcome: `passed`, `failed` (a
  command that exits with another status, or a valid results file that says so) or
  `errored` (no or invalid results where they are needed, or the command crashed). The
  Attempt's outcome comes from its run criteria alone; held-out results never change it. A
  Check without run criteria (rubric items and `held_out` criteria) takes its Attempt's
  outcome from the held-out results, which still never decide completion. The snapshot
  writes nothing to `.git`, covers the ignore rules that apply as well as the files, and
  refuses work with a blind spot (files the index hides, a nested repository, a link
  leading outside the folder, a folder whose files are all ignored) with an `errored`
  Attempt. It is taken before the run, after the run criteria, which error the Attempt if
  they changed the work, and after each `held_out` command, which errors only that
  criterion if it did. A Check with only rubric items has nothing to run.
- **Completion rule**: a Lesson can be completed when, for the Check version shown to the
  learner when practicing last started, which must still be the current one, every run
  criterion passed on an Attempt whose snapshot matches the current work, and every rubric
  item has a grade for that Check and that work, whatever the grade (`not_met` counts as
  graded), and the files each grade looked at still have the hashes it recorded. Only
  showing a Check moves its recorded version, so a Check edited afterwards stays flagged
  until it is shown again. Changing the work or the criteria after a pass means running the
  Check, and grading, again. `lesson.completed` records the Attempt and the grades it
  relied on.
- **Rubric grades** are `met`, `partly` or `not_met`, with a note, recorded by
  `rubric_record` (or `study rubric grade`) after the learner checks their own work against
  the item, on a practice folder that holds work. Grading an item again replaces its grade;
  two grades that replace the same one were given on two machines and are flagged. Replay
  validates a grade's payload and holds a grade it cannot trust.
- **Held-out runs**: the counted measurement is the first run that produces results, on the
  Check shown to the learner, in an Attempt that is not errored; every other run is
  recorded as "not counted", with the reason. The count is kept per Lesson and criterion,
  so editing the Check can't create a fresh first run, and an `errored` run never uses it
  up. A criterion is known by its id, so renaming a `held_out` criterion starts a new
  count; the agent's instructions forbid renaming one for a fresh first run. Only the
  results file is shown for held-out criteria, never raw output, so the test data doesn't
  leak. Each run records whether it was counted when it ran; replay validates the Attempt,
  decides, and flags a run that claimed the count but sorts after another, as when two
  machines measured first.
- **Held-out data** lives in `.heldout/<lesson-id>/`, committed with the Topic so every
  machine measures on the same data; it is synthetic or public, never personal data. The
  default `.gitignore` ends with `!/.heldout/` and `!.heldout/**` (git never looks inside
  a folder a line above leaves out), so data formats it ignores elsewhere
  (Parquet, `build/`) are committed there. "Out of the learner's sight" is a convention the
  agent keeps, not a lock: the agent writes the data before practicing starts and never
  shows it, and Lamplight never prints it, never lists its file names among an unclosed
  Session's changes or a Checkpoint's large files, and never records a held-out command's
  output.
- An Attempt that fails on a run criterion of the Check shown to the learner asks for a
  Next step that names the fix: `phase_set practicing` refuses without one, whatever
  Phases the Lesson went through since, and `check_results` says so in `next` (code
  `name_the_fix`). Once that Next step is recorded, none is asked for until another Attempt
  fails. Held-out results never ask for one.
- Per-Lesson criteria allow Lessons with special needs (a GPU, a container) and project
  Topics where one codebase grows across Lessons.
- Written work can be submitted as typed final answers or a photo of paper work: files in
  the practice folder, part of the work's snapshot and its Checkpoints. A rubric grade names
  the files it looked at by path and content hash; their bytes never reach the History.
  Each must be a file the snapshot sees, never an ignored file or a link, and is opened
  inside the practice folder, so a grade can't look at anything else.

### Cards and Reviews

- Cards are single concepts with a prompt and an expected answer, written from a Lesson or
  an Explore Session, linked to Evidence where possible.
- **Drafts**: a new Card is a draft until its first Review, where the learner keeps, edits or
  drops it. A daily cap limits how many new Cards appear.
- The card-writing rules, in the server's instructions: one fact per Card, no lists, no
  answer in the prompt, no trivia, at least one Card from the learner's own mistakes.
- Cards can be added, edited, suspended and deleted; `study review` has a key to flag one.
  A flagged Card shows as a `card_flagged` flag in `status` until it is edited or deleted,
  or the flag is dismissed; flagging it again after a dismissal is a new flag. Adding a Card
  with the same Lesson and content twice, or deleting one already gone, records nothing.
- Prompts and answers may span lines and hold tabs, for code Topics. A Card cites Evidence
  by id, checked against the History; retracted Evidence is refused.
- Explore Cards have IDs `explore.<random suffix>`, so `explore` is reserved and no Lesson
  may use it. Adding one needs no open Explore Session: a useful answer comes up in any
  Session, and the learner may add Cards from the command line with no Session at all.
  New Cards are appended to `cards.jsonl` rather than kept sorted by ID: a union merge keeps
  two machines' changes apart only when they touch different parts of the file, and
  inserting in ID order makes them overlap. Display numbers ("Card 4") come from the order
  Cards were written.
- **Lines a merge leaves**: every reader and write of a `path#key` item, for Cards and
  Sources alike, picks among the lines a union merge left by one rule. A line at the version
  the History recorded last is the entity, and lines at versions an earlier Event recorded
  are debris. A line at a version the History never recorded is an edit made outside
  Lamplight: where the file is authoritative for text (`cards.jsonl`), one such edit wins and
  several are a conflict; where the History holds the text (`sources.jsonl`), the recorded
  version wins and any such edit is a conflict. A write leaves a single line.
- **Retries and conflicts**: a Review may carry the client's request id; a retry with it, or
  a repeated first decision on a draft, records nothing and returns what was recorded. A
  delete records how many Reviews it had seen, so a delete and a Review made on two
  machines are flagged whichever replays first, as is a draft decided on both.
- **Sizing**: the Cards offered are those due, earliest first, then drafts, as many as the
  daily cap allows (10 decided a day). Without an explicit limit, the list is sized to the
  Energy, given or taken from the open Session: 20 at full, 10 at half, 3 at fumes, 10
  without one. Suspended Cards are never offered, and no count of what is due is shown.
- **Scheduling** replays each Card's Reviews through FSRS-6 (go-fsrs v4, which needs Go 1.26;
  fuzz off) from their `wall` times, each clamped to the Card's previous Review so time never
  runs backwards. Short-term learning steps are off: Lamplight works in sessions and offers a
  session's Cards once, so every Review, the first included, schedules in days, and "again"
  means the Card comes back next time. A Card starts at its first Review, never at the current time, so replay
  does not depend on when it runs. Only `schedule()` knows go-fsrs; a later version changes
  every replayed schedule, so it comes with a migration once learner data depends on it.
- Reviews work with the agent (conversational recall) or without it (`study review` in the
  terminal). Paused Topics hide their Cards; finished Topics keep reviewing at growing
  intervals. The daily cap on new Cards is a Topic setting, `new_cards_per_day` in
  `topic.toml`, 10 unless set.

### Level, Goal, Pace and Tasks

- The Level is set at the Assessment and can be changed by the learner at any time; an
  override holds until the next Assessment.
  - The Level lives in `topic.toml` (`level`), written by `assessment.recorded` when the
    Assessment sets one and by `level.set` when the learner chooses; the latest writer wins,
    which is the override rule. Where it came from is replayed from the History; a Level
    edited into the file by hand counts as the learner's choice.
  - A Level changed on two machines is flagged as a dismissible conflict naming both
    Events. That includes a later Assessment on one machine that kept the Level the file
    already held there while the learner chose another on the other: the merged file keeps
    the learner's choice, although the History's last writer is the Assessment. When the
    file is a version an Event recorded, the Level is reported with the Event that set it,
    never as a hand edit; choosing a Level, even the one the file holds, records it and
    settles the conflict.
  - An Assessment records its kind (placement or milestone, naming the Milestone), each item
    asked with its area and outcome (correct, partly, incorrect, or not reached when time ran
    out), a summary, the minutes it took and its time box, the Level it suggests, and the
    path and hash of the notes the agent saved in `notes/`. Weak and unreached areas are
    derived for the agent; after a milestone Assessment with weak areas the result's
    `next` is `propose_revision`, and nothing blocks. A client's `request` id makes a retry
    record nothing; without one, the same Assessment as the latest is a retry only while
    nothing changed the Level since, so a retake after an override sets the Level again.
- v2.0 records every signal a future Level suggestion needs: Checks passed on the first try,
  hints requested, feedback rounds, the gap between dev and Held-out scores, and Review
  results. Suggestions come later, once there is data to tune them.
  - Only hints (`hint.recorded`: nudge, explanation or step, for a Lesson being studied,
    with `requested_by` the learner who asked or the agent who offered it unasked) and
    Assessments have Events of their own; the other signals are derived by replay. The first
    try is the first Attempt measured on the Check shown to the learner (run criteria, not
    errored, so an agent's try before showing the Check never counts, and a Check with only
    rubric items or `held_out` criteria has none); feedback rounds count
    the times a Lesson went to feedback; the dev and Held-out gap compares a `held_out`
    criterion's counted score with the run criteria's mean score in the same Attempt.
  - The `signals` view is for the agent, to adapt depth, scaffolding and how soon it offers
    a hint. Its counts and scores are never shown to the learner: the `study signals`
    command is hidden from help and answers in JSON only.
- Tasks are non-study steps toward the Goal. They appear in `status` when relevant and are
  marked done through the core.
  - The Goal's optional deadline, the Pace as dated periods (`[[pace]]`, each with `from` and
    `hours_per_week`; the first may start "from now on") and the daily cap on new Cards live
    in `topic.toml`, set through `topic_update` and kept with any keys Lamplight does not
    know. A setting a hand edit broke is reported in `status` and left out; the rest stands,
    and setting or removing it fixes it. A `topic.toml` left with git conflict markers is
    reported with how to resolve it.
  - Tasks live in `tasks.jsonl`, one per line (`id`, `title`, optional `by` date and
    `after` Milestone), merged by union like `cards.jsonl`, so two machines adding Tasks
    never conflict, and changing Tasks never puts `topic.toml`, and with it the Topic, at
    risk of a git conflict. A line that is not a Task is reported and kept; the others work.
    A Task is relevant while it is open and, if it names a Milestone with `after`, once that
    Milestone is done, or with a note once a Revision removed it. Its `by` date is shown as
    written, never counted as late. Whether it is done comes from the History (`task.done`,
    `task.reopened`; the last mark wins), so a Task the learner wrote by hand, with any id,
    can be marked done too.
  - A Topic's state (`active`, `paused`, `finished`) is recorded by `topic_state.set`, with
    the state the writer saw, and replayed, never stored in a file; two machines changing it
    from one state to different ones are flagged. Paused hides its Cards and Forecasts and
    lasts until the learner resumes the Topic: a Session can still open on it, saying it is
    paused. Finished hides its Forecasts and keeps its Cards coming back at growing intervals.

## Knowledge

- **Built** (ADR-0007, ADR-0011): the Knowledge base kind is `none`, with which the agent
  reads the Sources itself. Evidence is an exact quote plus a location when known, with
  where the location came from. Sources are files (by path plus content hash) or URLs.
  Lessons without Evidence are marked, never blocked.
  - The Knowledge base lives in `topic.toml`'s `[knowledge_base]` table and is set by a
    `knowledge_base.set` Event, which writes the table anew with the kind alone. With one
    kind, choosing it again writes nothing, so the table is left as it is, with any keys a
    newer version added. A kind this version does not know, one a newer version added or
    the `notebooklm` of earlier builds, is shown as recorded and treated as `none`.
  - Lamplight does not use NotebookLM (ADR-0011): its community MCP server works through
    undocumented endpoints and the learner's browser session, at the risk of the learner's
    Google account. Earlier builds of v2 recorded a notebook and each Source's id in it;
    their Topics still load, with those fields ignored in Events and kept, unread, in
    `sources.jsonl`. A write they left interrupted is finished by this version; ADR-0011
    says how, and the one case in which the next change to a Source is then flagged as a
    conflict.
  - The History is the single source of truth for Sources: `source.added` and
    `source.updated` record them, and `sources.jsonl` is the readable copy, one line per
    Source and each its own item (`sources.jsonl#<id>`). A line added by hand is
    `untracked` until recorded. Synced data holds nothing machine-specific: a file inside
    the Topic is kept by its Topic path, one outside by its name, content hash and size.
    Where each file is on a computer is local state in `.lamplight/sources/<topic>.json`,
    found inside the Topic, where it was last found, or in the Library by its content, and
    updated without recording Events, so switching machines never writes to the History.
  - `history.jsonl` and `sources.jsonl` merge by union. Two machines adding Sources do not
    conflict; one Source edited on both is flagged, and duplicate lines a union merge
    leaves are flagged too, with the History's version used until the next change leaves
    one line. Duplicate Sources or Evidence added on two machines before syncing are
    accepted under the one-machine-at-a-time contract.
  - Evidence lives only in the History: `evidence.recorded`, held until its Source is known,
    and `evidence.retracted`, which takes it back without deleting it.
  - Location origins are `source` (read in the Source itself), `knowledge_base` (a citation
    as the Knowledge base gave it), `learner` and `estimate`.
  - The core reads a file's bytes only to hash it, never blocking on a FIFO or device, and
    stops when the request is cancelled.
  - `status` marks the Lessons started or done that cite no Evidence, once the Topic has
    Sources (`lessons_without_evidence`); they are never blocked.
- **Built** (ADR-0012): the registry of the Knowledge base plugins a machine has, and
  `study knowledge-base add`, `list` and `remove` to change it. Nothing starts or contacts
  a plugin yet, and no Topic names one.
  - The registry is `knowledge-base-plugins.json`, with a `format` number, in Lamplight's
    configuration folder beside `config.toml`. An entry is a name and either a command, as
    an argument list, or an http or https URL. A name follows the rule of a Topic's id and
    is neither `none` nor `plugin`, the kinds of Knowledge base a Topic records.
  - A command's program is resolved when it is registered, on the absolute folders of
    `PATH` or from the folder `study` started in, and stored as an absolute path with its
    symbolic links unresolved. A path with `..` in it is refused: by name it leads
    elsewhere than through a link.
  - Nothing the registry depends on may be in the Study home or be reached through it: the
    agent writes there, and a plugin is started outside its sandbox. A path is followed one
    name at a time, each folder opened from the one before it and each symbolic link
    followed from the folder that really holds it, and is refused as soon as it enters the
    Study home, wherever it ends. The Study home is known by file identity, not by name,
    so another letter case, link or mount of it is still the Study home. One that is not
    there yet is known by where it will be, and looked for again at every folder, so a
    Study home another `study` makes meanwhile is refused like any other. That holds for a
    plugin's program and for Lamplight's configuration folder, which is then read, locked
    and written through the folder as it was opened, never through its name again. A
    configuration folder that is not an absolute path is refused too. A program must be
    one the user `study` runs as may run, which the system is asked.
  - An argument that names something in the Study home is refused unless the learner
    allows it (`--allow-study-home-arguments`), which the entry records. An argument that
    names a file relative to where the command was typed is refused, since a plugin is
    started from the registry's folder. This catches the honest mistake and is no
    guarantee: `sh -c` and the like resolve more at start than any reading of arguments
    sees, and no following of paths sees a hard link.
  - The reader takes exactly what the writer writes. A newer format is refused before
    anything else is judged. Anything else is `corrupt`: a key it does not know, in
    another letter case or given twice, a `null`, bytes that are no text, a path or a URL
    not written as `study` writes one. A registry that is a symbolic link is not followed.
    A change never writes what the reader would refuse, one over the size it reads
    included.
  - The registry is the machine's and no Topic's, so changing it records no Event. A change
    takes the lock of a file beside the registry, reads the registry again under it and
    replaces the file whole. Registering a plugin as it is registered changes nothing;
    anything else under a registered name needs `--replace`. A dry run looks at everything
    the change needs and fails where the real run would, making nothing.
  - No MCP tool registers, changes or removes a plugin or takes a command line or a URL
    for one, and the build keeps it so. `internal/pluginregistry` reads the registry, and
    the core and the MCP server link it. `internal/pluginregistry/registrar` changes it,
    and only `internal/cli` imports that: a test lists what the server is built from and
    fails when the registrar is in it, so no wrapper in the core can register a plugin
    for a tool. What the two share, the opened configuration folder included, is in a
    package neither the core nor the server can import. Tests that look at names, the
    server's source and every tool's inputs, are a second line.
  - `pluginregistry.OpenProgram` is what starting a plugin will use: it checks the program
    and the arguments at that moment and returns the program open. The file is what is
    started, not its path, which must not be resolved again after the check.
- **Still to build for v2.0** (ADR-0012): the rest of Knowledge base plugins, so the agent
  can search a Topic's Sources with no account anywhere.
  - The agent talks to `study`, and `study` is the MCP client of the Topic's plugin. `study`
    ships no retrieval and still never parses a document.
  - The contract is six tools with fixed names: `contract` (the version, and what the
    plugin can do), `index` (one Source of a Topic's collection; it returns at once),
    `status` (each Source indexed, indexing or failed, and the hash of the content
    indexed), `cancel` (stop indexing a Source), `search` (Passages for a query, each with
    its score) and `passage` (one Passage again). A Passage is the Source's own text as
    extracted, with a location when the plugin knows one. A location says its kind: `pages`
    now, others such as a time range later, without a new contract version; a Passage from
    a web page has none. A file is indexed by its content hash; a URL gets one when the
    plugin fetches it.
  - A plugin is registered per machine with `study knowledge-base add` (built, above). A
    Topic records only its name (`kind = "plugin"`, `plugin = "<name>"`), so nothing a Topic
    holds decides which program runs (ADR-0009). A Topic whose plugin this machine lacks is
    treated as `none`, and `status` says how to fix it. A command is started by the
    absolute path recorded at registration, from the registry's folder and never from the
    Topic. A plugin reached by URL is a service outside any sandbox, so it must itself be
    told what it may read and fetch.
  - `study source index` is the one thing that asks a plugin to index, and it is CLI-only,
    like `study check`: run in the agent's shell or by the learner, so the sandbox and
    approval prompts apply to what a command plugin reads and fetches. No MCP tool starts
    indexing, or the MCP server would read any file the agent named and hand its text back
    through a search. Adding a Source records it and indexes nothing. Whether a Source is
    indexed is the plugin's to say on this machine: it records no Event, and `sources` and
    `status` show it and give the command. A command plugin keeps its index in a folder
    `study` gives it under the Study home's `.lamplight`. Nothing waits for a plugin.
  - `evidence_search` returns Passages. Evidence recorded with a Passage's id is checked:
    `study` reads the Passage again, refuses a quote that is not in it, and takes the
    location from it. That is a new Event type. Evidence without a Passage stays possible,
    unchecked.
  - Shelf (`study-shelf`) is the plugin that ships with Lamplight: a second program in this
    repository and release, of which `study` links nothing. Go without cgo, one SQLite file
    per collection for keyword search and vectors, PDFs read page by page, embeddings from
    any endpoint that speaks the OpenAI embeddings API, keyword and embedding results
    merged by rank, and keyword alone when there is no endpoint. When a reranking endpoint
    is given, it reorders the best Passages before they are returned. A Recipe names
    Shelf's settings for one embedding model; the first is for EmbeddingGemma 2.
- **Later**: a context written by a language model for each Passage, used for indexing
  only; layout-aware conversion for formulas, tables and scans; recordings, video and
  images as Sources.

## Library

The Python catalog is ported to Go. One normalizer is shared by indexing and searching,
topics match as whole words (fixing the "C" and "algorithms" ranking bugs), and results are
typed. The index is rebuildable data in the Study home; ADR-0003's scanning rules still
apply. Search results carry an absolute path. Conversion leaves the Library.

## Interfaces

### MCP tools

Every write names its Topic. Tools are named after things that happen in the domain.

- **Read**: `status`, `syllabus`, `lesson`, `due_cards` (sized to Energy), `cards`, `history`,
  `check_results`, `library_search`, `sources`, `evidence`, `tasks`, `signals` (for the
  agent only).
- **Topics**: `topic_create`, `topic_update` (Goal, Pace, Level, Approach, Knowledge base,
  Tasks, pause, finish), `task_done`, `assessment_record`, `source_add`, `source_update`,
  `evidence_record`, `evidence_retract`. Still to build (ADR-0012): `evidence_search`.
  Indexing has no tool: `study source index` is CLI-only.
- **Syllabus**: `revision_propose`, `revision_apply`, `revision_decline`.
- **Sessions**: `session_open`, `session_close`, `phase_set`, `break_point_reached`,
  `checkpoint`, `hint_record`, `rubric_record`, `lesson_complete`.
- **Cards**: `card_add`, `card_edit`, `card_suspend`, `card_flag`, `card_delete`,
  `review_record` (including keep, edit or drop for drafts, and a required request id).

Opening a Session on a Topic also makes it the most recent Topic; there is no separate
switch tool.

### CLI

- The same operations, plus `study check`, following the `cli-creator` conventions: nouns
  then verbs, `--json` everywhere (JSON on stdout only, diagnostics on stderr), documented
  success and error shapes, exit 0 on empty results, `--dry-run` on writes, bounded
  `--limit`.
- `study` alone prints `status`. `study review` runs terminal Reviews. `study doctor --json`
  works even when setup is broken.
- `study topic remove` moves a Topic's folder, whole, into `.lamplight/removed`, deleting
  nothing, and `study topic restore` moves it back: it takes the lock of the Topic id,
  refuses while anything in the Study home has that id, and moves the folder into place
  with a rename. The lock keeps it apart from writes, removals, other restores and
  `study import`, which holds the lock of its Topic's id while it stages the Topic and
  moves it into place. Creating a Topic takes no lock, and there the rename does the work:
  it is refused when a folder that holds anything is under the id. So a Topic created or
  imported under the id meanwhile is never touched, and the removed one never ends up
  inside it. Both commands are for the learner alone, with no MCP tool (ADR-0013).
  - Neither records an Event. They move a folder on one computer and change nothing
    inside the Topic, so they are that computer's state, like where a Source's file is.
    Writes are Events because the History is what every computer shares, and a Topic
    removed here is unchanged everywhere else, on its git remote too. It is not a general
    exemption: anything that changes what a Topic holds records an Event.
  - A record beside each removed folder says which Topic it was and when, since a Topic
    does not hold its own id. It is written before the folder moves out and deleted after
    it moves back, so an interrupted removal or restore leaves at worst a record, or the
    temporary file of one, without a folder; both are ignored.
  - A folder whose record is missing or cannot be used has only its name, which gives the
    time to the second and an id when it fits one (`<time>-<id>` and `<time>-<id>-<suffix>`
    look alike). It is restored by id alone only when that is enough to tell that it is the
    Topic's newest removal; otherwise the learner names the folder, and the id too when
    the name fits two. A folder that holds no Topic is never restored.
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
configuration is trusted. Sources and caches stay local.

## Checkpoints

Each Checkpoint is a git commit made by the core with low-level commands that cannot run
programs named in the repository's configuration: `hash-object -w --no-filters`,
`update-index --index-info`, `write-tree`, `commit-tree --no-gpg-sign` and `update-ref`, run
with no inherited `GIT_*` variables, `GIT_CONFIG_GLOBAL=/dev/null`, `GIT_CONFIG_NOSYSTEM=1`,
`core.hooksPath=/dev/null` and `core.fsmonitor=false`. Diffs for the large-file warning use
`--no-textconv --no-ext-diff`. This rules out hooks, clean and smudge filters, fsmonitor,
textconv, external diff drivers and signing programs; ADR-0009 lists the further routes
(hooks defined in configuration, `core.worktree`, partial-clone fetches) that are closed as
well. The commit author is read from the learner's git configuration beforehand and passed
explicitly.

Checkpoints stage into a copy of git's index while holding `.git/index.lock`, then replace
the index, so `git status` is clean afterwards. A refused or failed Checkpoint leaves the
learner's index untouched; if the branch cannot be moved, the learner's index is put back. A
Checkpoint never runs `git init`: Topic creation does, and writes the default `.gitignore`.
The cost is that Checkpoints store raw bytes, so line-ending conversion and Git LFS are not
applied, which is acceptable for study Topics.

The agent can change the Topic while a Checkpoint runs, so a Checkpoint pins the Topic
folder and its `.git` when it starts, works through the open folders (on Linux, git does
too), and checks the names again before git writes; ADR-0009 describes this and the race
that remains.

A Checkpoint skips empty commits, refuses to commit during a merge, a rebase or on a
detached HEAD, waits briefly when an editor holds git's index lock, and warns before
committing large files. Files an editor replaces while they are being saved are saved once
more; files that vanish count as deleted. A dry run reports whether a Checkpoint would be
made, and its large files, without writing anything to `.git` or waiting for the lock. The
default `.gitignore` covers data and model artefacts (Parquet, DuckDB, GGUF, safetensors,
PyTorch checkpoints), build outputs, test coverage files (`.coverage`, `coverage.out`) and
caches, and ends with `!/.heldout/` and `!.heldout/**`, so Held-out data is always committed. Held-out files
never appear among the large files a Checkpoint reports.

## Distribution and setup

ADR-0008 and ADR-0010. `study` ships for Linux and macOS on the GitHub release alone.
`install.sh` downloads the archive for the machine, checks it against the release's
checksums, puts `study` in `~/.local/bin` and installs completions; running it again
upgrades. The release also holds deb and rpm packages, and `go install` works. There is no
Homebrew cask and no AUR package. `study setup` installs the `lamplight` skill and registers
the MCP server at user scope for Claude Code and Codex, never overwrites a folder it did not
create, and can be reversed exactly with `--remove`. Claude Code can instead use the
marketplace plugin, which includes a session-start hook that prints `status` when the agent
starts inside the Study home. Other agents use `npx skills add` and a documented MCP snippet.

- `study setup` records each file it writes with its SHA-256, each folder and link it
  creates (with where it led, symlinks resolved), and each registration, in
  `$XDG_STATE_HOME/lamplight/setup.json`. It saves the record before or as it takes each
  step, under a lock that `--remove` and `study mcp`'s refresh share, so a run that stops
  part-way can still be undone exactly. It replaces or removes only what still matches the
  record: a regular file with the content it wrote, a real folder that still leads where it
  led, a link with the target it gave it. Files changed by hand, and anything moved behind a
  symlink (a dotfiles repository), are kept. A record that names anything setup never
  creates is refused as `corrupt`. `--check` reports a stale skill, a missing registration
  or a `study` that moved, and `--dry-run` takes every decision the real run would on a copy
  of the record. `study mcp` refreshes the skill files setup wrote when it starts.
- Agents run `study` by an absolute path: the `PATH` entry when it is the running binary
  (it survives upgrades, unlike Homebrew's versioned folders), a version manager's shim when
  that is what the shell runs, else the running binary's real path, with a note. A binary in
  a temporary build folder (`go run`) is refused without `--force`.
- Claude Code's user-scope registration is read from `~/.claude.json`; Codex's through
  `codex mcp get --json`, whose stdout only is parsed. Neither check starts the server.
  Agent commands run in their own process group, stopped after 10 seconds for reads and 60
  for changes.
- The plugin is generated, not stored: `study claude-plugin-path` (the marketplace entry's
  `command` source) writes the skill, the MCP server and the session-start hook from the
  binary (`internal/claudeplugin`) into a folder named after its content, which is never
  replaced or removed, and prints the folder. Setup leaves Claude Code to the plugin when it
  is enabled in the user's settings or the current project's, and the command refuses while
  setup provides Lamplight to Claude Code, so the two never both register where setup can
  see.

## Migration from v1

v1 stays installed as the `study` skill and keeps working. The LLM data engineering Topic
stays on v1 until after 13 October. Until the learner switches, v2's skill is installed only
for testing, against a separate Study home, so "let's study" keeps reaching v1.

1. `study import <v1-dir> [--not-done lesson-NN]... [--dry-run]` copies the workspace, git
   history included, into the Study home and leaves the original untouched. It keeps v1
   folder and Lesson names as IDs (`lesson-01`), so paths quoted in Lesson text and in
   `.gitignore` keep working. It moves `lessons/plan.md` to `notes/v1-plan.md`, converts
   `.study-config.json` into `topic.toml`, and maps `sources` to Sources. v1's `notebooklm`
   setting and each source's ids in a notebook are dropped, and the import chooses no
   Knowledge base (ADR-0011). It records one `topic.imported` Event, whose payload is the
   import's report and carries the Lesson completions it can prove, after the settings and
   Sources it converts. v1's lesson-level cards are dropped. `--dry-run` lists everything
   that will be copied, converted, moved and dropped, with each proof.
   - Every move is planned before anything is copied, and the dry run and the import share
     the plan: a Lesson's file under `lessons/` moves to `lessons/lesson-NN.md`, the Lesson
     file v2 reads, once, never over another file; v1's config is kept as
     `notes/v1-config.json`, and v1 files named like Lamplight's state files move to
     `notes/v1-<name>`. Nothing under `.git` moves.
   - A Lesson v1 calls completed counts as done only with its file under `lessons/` and v1's
     own record of it: the commit v1's Lesson Completion Contract makes, `[agent] complete
     lesson NN`, matched whole and not reverted since, or the card `lesson-NN` the contract
     adds. Anything else stays open, and the report says it was not proven; the learner keeps
     any Lesson open with `--not-done`. A completion the import proves has no Attempt; a
     Revision cannot remove or skip it.
   - Everything is copied, git-ignored or not, except links leading outside the workspace
     (resolved through `os.Root`, so a second link cannot lead out either), special files,
     v1's cards, and folders the language's tools rebuild (`node_modules`, virtual
     environments, caches, Rust and Maven `target`), unless the history tracks them. Each
     is listed with why; nothing refuses the import.
   - The learner's `.gitignore` and `.gitattributes` lines come first and Lamplight's last,
     so they win: negations keep the state files in Checkpoints, and the state files'
     attributes are reset to Lamplight's. The Event records the merged `.gitattributes`.
   - The v1 difficulty becomes the Level with `source: import`, v1's estimate, until an
     Assessment or the learner sets it.
   - The workspace is copied under `.lamplight/tmp` and moved into place when complete, so
     an interrupted import leaves no Topic; the next import removes staging folders over an
     hour old whose Topic is not locked, and `study doctor` reports them. Imports run one at
     a time under a lock, and a workspace is known by its real path, so the same one cannot
     be imported twice. Its history is read only through the hardened checkpoint package, in
     UTF-8 and bounded. A repository borrowing objects (alternates) is refused with the fix,
     `git repack -a -d` then deleting `objects/info/alternates`; one where git is writing is
     `busy`.
2. An adoption Session works through a checklist: Goal and deadline, Pace periods, Syllabus
   from `notes/v1-plan.md` (the three tiers become three Milestone priorities), or, without
   one (only v1's project approach wrote it), from v1's lesson list and the learner's notes,
   a Check for each open Lesson, the Sources, Cards for completed Lessons, and the
   Next step from v1's `pending_action` and `context`. The learner approves the result as a
   Revision. A proof the learner disputes is fixed before adoption, by importing again with
   `--not-done`. Until the Topic has a Syllabus, `status` recommends `adopt` and carries the
   import's report, and `session_open` suggests it (Instruction 13); the skill's adoption
   reference is the checklist.
3. Acceptance test: `~/study-workspaces/c` and `~/study-workspaces/llm-data-engineering`
   import and resume exactly where they stopped. Automated tests use sanitised copies,
   because the real workspaces contain work-related content.

## The skill

`skills/lamplight/` holds the teaching method: `SKILL.md` carries what every Session needs
(where the learner is, Energy and Focus, stopping with a Next step, the teaching rules, the
learner's words, the files the agent keeps), and `references/` one file per branch:
starting a Topic (brainstorming and the Assessment), the Syllabus and Revisions, the lesson
loop (template, Phases, Checks, Held-out data, feedback, Levels), Cards and Reviews,
Knowledge, Goal, Pace and Forecasts, and optional companions. The skill names tools but
leaves how to call them to their descriptions, and points to the server's instructions by
number instead of restating them, so each rule is said in one place. It is embedded in the
binary (`go:embed`, package `skills/lamplight`) for `study setup` to install, and a Go test
validates it: portable frontmatter, links and anchors that resolve, no orphan reference,
every tool name and field it writes in backticks known to the real MCP server's tools and
schemas, every `study` command and flag it shows known to the CLI's command tree, and none
of v1's commands. Tools and commands still to come are written inside
`<!-- pending #N -->` blocks, which the test allows only for the names it lists under that
issue, and rejects once the server or the CLI has them. It carries v1's teaching material
over explicitly:

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

The repository is renamed to Lamplight. v2 is developed on the `v2` branch while `main`
keeps v1 for its users; at release, v1 is tagged and kept on a `v1` branch, and `v2` is
merged into `main`.

```
cmd/study/                  entry point
cmd/study-shelf/            Shelf, the Knowledge base plugin (still to build, ADR-0012)
internal/…                  core modules, and the CLI and MCP adapters
skills/lamplight/           the skill and its references (embedded in the binary)
.claude-plugin/             marketplace.json; the plugin itself is generated by
                            study claude-plugin-path (internal/claudeplugin)
docs/adr/, docs/design/, GLOSSARY.md
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
`study setup` for Claude Code and Codex; Linux and macOS releases with an install script;
`study import`; and, still to build, Knowledge base plugins (ADR-0012): the contract, `study`
as its client, the registry, `evidence_search` with the quote check, and Shelf with keyword
and embedding search and optional reranking.

**Order of work**: the tracer bullet, then the History engine (its file formats are settled
before any real data is written), then a thin learner loop through every layer, then each
module deepened.

**Later**: the dashboard (Vue 3, TypeScript, shadcn-vue; home network with a login, or
Tailscale; read-mostly first), Level suggestions, held-out results that can block completion
(with fresh, reviewed test sets for a retry), using one Topic on several machines at once,
Windows packages, the Agent Plugins 1.0 manifest, setup for more agents, reading tables of
contents from PDFs, retrieval from page images, the Journal, the FSRS optimizer, publishing
to the MCP Registry; for Knowledge bases, the context written for each Passage,
layout-aware conversion, and recordings, video and images as Sources.

## Known risks

- Until Knowledge base plugins exist (ADR-0012), the agent reads the Sources itself, which
  is slow in a long book.
- A plugin is handed a file's path, so one on another machine must see the same files.
- Formulas extract garbled from PDFs, so a formula is quoted as extracted or left unchecked.
- Approvals are tamper-evident only: an agent with a shell can still edit files.
- Agents that read both `~/.agents/skills` and `~/.claude/skills` may list the skill twice.
- The session-start `status` is automatic only where the agent supports hooks.
- Comments in files the core rewrites are lost.
- Sync assumes one machine at a time: concurrent edits are flagged, not merged.
- Checkpoints store raw bytes, so line-ending conversion and Git LFS don't apply.
