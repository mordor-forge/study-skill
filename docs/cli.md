# The study command line

`study` is Lamplight's command line for learners and for agents with a shell. Agents
without a shell use the same operations through `study mcp`. This page is the contract
that scripts and agents can rely on. Terms follow [CONTEXT.md](../CONTEXT.md).

## Commands

| Command | What it does |
|---|---|
| `study` | Same as `study status`. |
| `study status` | Shows the Study home, the Active topic and why it was chosen, the one action recommended for it (`recommended`), the Learner profile (`learner_profile`), every Topic, and any Topic that could not be read (`problems`). A broken Topic never stops the others from being listed. Each Topic can carry `flags`, its Resume point (`resume`), whether its Cards are ready (`cards`, never a count), its additions to the Learner profile (`learner_additions`) and `lessons_without_evidence`: Lessons started or done that cite no Evidence yet, once the Topic has Sources or a NotebookLM Knowledge base (a reminder, never a block). See "Where the learner stopped" below. It also carries its plan (see [Goal, Pace and Forecasts](#goal-pace-and-forecasts)): `state`, `deadline`, `pace`, `new_cards_per_day`, the `forecast` and the open `tasks` that matter now; human output shows them under the Active topic. |
| `study session open <topic> [--energy E] [--focus F] [--session ID] [--dry-run]` | Opens a Session and shows where you stopped. With an Energy and no Focus chosen yet, it suggests one (or to plan, or to stop); it lists every Session that ended without a Next step, and what changed since the last Checkpoint. With `--session` and `--focus`, it records the Focus chosen for that open Session instead of opening another. See "Opening a Session" below. |
| `study session close <topic> --next-step S [--context C] [--session ID] [--dry-run]` | Closes the Session with a Next step that starts with a verb (see "Next steps" below), and saves the work with a Checkpoint. `--session` gives a Session left unclosed the note it never got. |
| `study session break-point <topic> <lesson> <break-point> --next-step S [--context C] [--dry-run]` | Records a Break point the Lesson's header declares, with a Next step, and saves the work with a Checkpoint. The Session stays open. Reaching the same Break point with the same Next step again changes nothing. |
| `study topic create --title T [--id ID] [--goal G] [--dry-run]` | Creates a Topic folder with its settings, History and git repository. `--dry-run` validates and shows the result without writing. |
| `study topic update <topic> [--title T] [--goal G] [--knowledge-base K [--notebook ID]] [--deadline D] [--pace H[@FROM] ... \| --clear-pace] [--new-cards-per-day N] [--level L] [--approach A] [--state S] [--dry-run]` | Changes a Topic's title, goal, Knowledge base (`notebooklm` with the notebook's id, or `none`; see [Sources and Evidence](#sources-and-evidence)), the Goal's deadline, the Pace, the daily cap on new Cards, the Level (the learner's choice, which holds until the next Assessment), the Approach (`concepts`, `project` or `challenges`; see [Assessments, Level and learning signals](#assessments-level-and-learning-signals)), or its state (`active`, `paused` or `finished`); see [Goal, Pace and Forecasts](#goal-pace-and-forecasts). Flags left out stay as they are; `--goal ""` and `--deadline ""` remove them. `--pace` replaces the Pace: `--pace 10` is 10 hours a week from now on, and a further `--pace 3@2026-11-16` starts a period of 3 hours a week that day. Settings in `topic.toml` that this version does not know are kept. The result is `{"topic": ..., "changed": bool}`: asking for the values the Topic already has changes nothing and records nothing. Each kind of change is its own Event; if a later one fails, the error says which were recorded. A dry run shows the Topic as it would be, Forecast included. |
| `study assessment record <topic> --file F [--dry-run]` | Records an Assessment read from a JSON file (`--file -` reads stdin) holding one object in the shape the `assessment_record` tool takes; anything after it is refused. With a `level`, it sets the Topic's Level. A retry records nothing (see below). A dry run shows the Assessment without an id and the Level the Topic would have. See [Assessments, Level and learning signals](#assessments-level-and-learning-signals). |
| `study assessment list [topic]` | Lists a Topic's Assessments, newest first in the text and in `--json`, and its Level. |
| `study hint record <lesson> --requested-by learner\|agent [--topic ID] [--kind K] [--note N] [--request ID] [--dry-run]` | Records a hint given for a Lesson: `nudge`, `explanation` or `step`, asked for by the `learner` or offered unasked by the `agent`. Without `--topic`, it uses the Topic whose folder it runs in. |
| `study signals [topic] --json` | For agents: a Topic's learning signals, in JSON. Hidden from help, and without `--json` it only says it is for agents: the signals are never shown to the learner. Reads only. |
| `study task add <topic> <title...> [--by D] [--after M] [--dry-run]` | Adds a Task, a step toward the Goal that is not study, to `tasks.jsonl`. `--by` is a date the learner wants it done by, shown as is; `--after` names a Milestone of the Syllabus, and the Task is shown once that Milestone is done. Adding a Task with the title of one already there returns that Task, done or not. The result lists the Tasks with their ids in `added_tasks`; a dry run gives a new Task no id yet. |
| `study task done <topic> <task> [--undo] [--dry-run]` | Marks a Task done, or not done with `--undo`; marking it as it already is changes nothing. |
| `study task remove <topic> <task>... [--dry-run]` | Removes Tasks that no longer matter. A Task removed already changes nothing; one that never existed is `not_found`. |
| `study task list <topic> [--all]` | Lists the open Tasks, or all with `--all`, and the lines of `tasks.jsonl` that are not Tasks (`problems`). |
| `study topic dismiss-flag <topic> <flag-id> [--dry-run]` | Dismisses one of the Topic's flags, by the id `status` shows, once the learner has looked at it. It records the decision in the History and never changes content. The result is `{"topic": ..., "flag": {...}, "changed": bool}`; dismissing a flag twice changes nothing. Only `held_event`, `conflict`, `damaged_line`, `clock_ahead` and `card_flagged` flags can be dismissed (see below). |
| `study topic remove <topic> [--dry-run]` | Moves the Topic's folder, whole (git history included), out of the Study home into `.lamplight/removed/<YYYYMMDD-HHMMSS>-<topic>`, the time in UTC, and records nothing in the History. Nothing is deleted: the result, `{"topic": ..., "moved_to": path, "restore": command}`, carries the exact command that restores the Topic, `test ! -e <Study home>/<topic> && mv <moved_to> <Study home>/<topic>`, each path quoted for a POSIX shell; while another Topic has that id it moves nothing and fails (it checks just before it moves, so do not create a Topic with that id while it runs). The removal is local to this computer: the Topic's git remote and other computers' copies are untouched. It waits for a write in progress (a dry run says so in `note`, without waiting), refuses (`failed_precondition`) while an interrupted write waits to be finished, and fails with `not_found` if the Topic is removed or replaced while it waits; every other write that was waiting then fails the same way and writes nothing. It is for the learner, such as before importing a v1 workspace again with `--not-done`; agents have no tool for it. |
| `study import <v1-workspace> [--topic ID] [--not-done LESSON]... [--dry-run]` | Imports a v1 study workspace as a new Topic, with its history, leaving the original untouched. `--not-done` keeps a Lesson open that v1's records prove done. See [Importing a v1 workspace](#importing-a-v1-workspace). |
| `study library build <folder>` | Indexes the books in a folder (relative to where you run it) and replaces the Library index in the Study home. |
| `study library search <query> [--limit N]` | Ranks the books in the Library against the query. `--limit` defaults to 10 and is capped at 100; no matches is a success with an empty list. |
| `study source add <topic> (--file PATH \| --url URL) [--title T] [--notebooklm-id ID] [--dry-run]` | Adds a file or a web page as a Source of the Topic. A file is hashed, never parsed. Adding a file or URL the Topic already has is `already_exists`, naming the Source. |
| `study source update <topic> <source> [--title T] [--path P] [--notebooklm-id ID] [--dry-run]` | Changes a Source's title or NotebookLM id (`""` removes it), which the History records, or says where its file is on this computer (`--path`, remembered locally only); the file at `--path` must hold the same content. |
| `study source list <topic>` | Lists the Topic's Knowledge base and Sources, and finds each file on this computer. |
| `study evidence record <topic> --lesson L --source S --quote Q [--location LOC --location-from F] [--dry-run]` | Records an exact quote from a Source that a Lesson cites. `--quote -` reads the quote from stdin. Recording the same Evidence twice changes nothing. |
| `study evidence retract <topic> <evidence> [--dry-run]` | Takes back Evidence recorded by mistake. The retraction is recorded, never deleted; retracting twice changes nothing. |
| `study evidence list <topic> [--lesson L] [--all]` | Lists the Evidence recorded in the Topic, or only what one Lesson cites. Retracted Evidence is listed only with `--all`. |
| `study syllabus [topic]` | Shows a Topic's Syllabus (the Active topic's when none is named): Milestones and Lessons with their display numbers and status, the Revisions waiting for the learner with their change, and a hand edit of `syllabus.toml` (see [The Syllabus](#the-syllabus)). |
| `study revision propose <topic> --summary S (--syllabus FILE \| --from-file) [--dry-run]` | Proposes a change to the Syllabus, the first one included. `--syllabus` names a file holding the whole Syllabus as it would be afterwards, as TOML like `syllabus.toml` or as JSON; `--from-file` proposes `syllabus.toml` as edited by hand. Nothing changes until the learner approves. |
| `study revision apply <topic> <revision> [--learner-said S] [--dry-run]` | Applies a proposed Revision once the learner approves. On a terminal, study shows the change and asks the learner directly; an agent relaying their answer from the conversation passes their words with `--learner-said`. Answering no records a decline. |
| `study revision decline <topic> <revision> [--learner-said S] [--dry-run]` | Records that the learner said no. The Syllabus is unchanged, and the Revision can no longer be applied. The result is `{"topic", "revision", "decision", "changed"}`; `apply` returns `{"topic", "revision", "syllabus", "approval", "changed"}`. Answering again changes nothing and reports the answer recorded the first time, on a terminal too. |
| `study checkpoint --topic ID --role agent\|learner [-m\|--message MESSAGE] [--dry-run]` | Saves the Topic's work as a git commit at a turn switch. Skips when nothing changed, refuses during a merge or rebase, and lists large files it saved. Never runs programs named in the Topic's git configuration. It waits for a write in progress and finishes an interrupted one first; `--dry-run` refuses (`failed_precondition`) while one is pending. |
| `study check <lesson> [--topic ID] [--timeout D]` | Runs a Lesson's Check on the current work and records the Attempt (see "Checks" below). Without `--topic`, it uses the Topic whose folder it runs in. |
| `study rubric grade <lesson> <criterion> --grade G [...]` | Grades a rubric item of a Lesson's Check (see "Checks"). |
| `study results <lesson> [--topic ID]` | Shows a Lesson's Check results and whether it can be completed (see "Checks"). |
| `study lesson <lesson> [--topic ID]` | Shows one Lesson: its number, Milestone, status and Phase, its file, and what its YAML header declares (the Check's criteria with their commands, and the Break points), with the current Check version, whether it is the one shown to the learner (`check_shown`), and whether the header can be read (`header_readable`, `header_error`). Runs nothing and never shows Held-out data. Without `--topic`, it uses the Active topic. The MCP tool is `lesson`. |
| `study history [topic] [--limit N] [--type T] [--lesson L]` | Shows a Topic's most recent Events, newest first, each as `{id, type, at, clock_behind?, summary, lesson?, item?, held?}` with `more` set when older ones match too. `--limit` defaults to 10 and is capped at 100; `--type` keeps the Events whose type starts with T (such as `card.` or `attempt`), `--lesson` those about one Lesson, its Card Events and Reviews included. Each Event appears once, as replay applied it. Entries are ordered by the History's clock (`time`) but show the writer's wall clock (`at`), so an Event from a machine whose clock was behind can look older than the one before it: `clock_behind` marks it ("clock behind" in the text). `held` marks an Event replay did not apply (`status` flags it), and Events written by a newer version of study are not shown. Entries never carry an Event's full payload: summaries are built from the kind of Event and ids only, so no Held-out results, notes, quotes, titles or Next steps. It is for understanding what happened, never a tally for the learner. The MCP tool is `history`. |
| `study review [topic] [--energy E] [--limit N]` | Reviews the due Cards in the terminal, without an agent (see "Cards and Reviews" below). Without a Topic, it reviews the Active topic. Interactive only: with `--json` it is a usage error. |
| `study card list <topic> [--lesson L]` | Lists the Topic's Cards in the order they were written, with their display numbers and state. `--lesson explore` lists the Explore Cards. |
| `study card due <topic> [--energy E] [--limit N]` | Lists the Cards to review now, sized to the Energy, never saying how many more are due. A paused Topic lists none, and its result says `"paused": true`. |
| `study card add <topic> --prompt P --answer A [--lesson L] [--evidence E,...] [--dry-run]` | Adds a draft Card from a Lesson, or without `--lesson` an Explore Card, citing Evidence by id. Adding the same Card again changes nothing. |
| `study card edit <topic> <card> [--prompt P] [--answer A] [--evidence E,... \| --clear-evidence] [--dry-run]` | Changes a Card's prompt, answer or Evidence, keeping its schedule; settles a flag on the Card. |
| `study card suspend <topic> <card> [--undo] [--dry-run]` | Stops offering a Card for Review, or with `--undo` offers it again. |
| `study card delete <topic> <card> [--dry-run]` | Deletes a Card for good; deleting it again changes nothing. |
| `study card flag <topic> <card> [--note N] [--dry-run]` | Flags a Card as wrong or unclear, so it shows in `status` until fixed. |
| `study card review <topic> <card> --rating R [--draft keep\|edit\|drop] [--prompt P --answer A] [--request ID] [--dry-run]` | Records one Review, for scripts; at a draft's first Review `--draft` is required. A retry with the same `--request`, or repeating a draft's first decision, returns the Review already recorded (`changed: false`). A dry run shows the Card after the Review. |
| `study doctor` | Diagnoses the setup and says how to fix what it finds. It works even when nothing else does. Exits 1 when a Finding failed. |
| `study completion install [--shell S] [--dir D] [--yes] [--force] [--dry-run]` | Installs completions for bash, zsh or fish (default: from `$SHELL`) for your user. |
| `study completion uninstall [--shell S] [--dry-run]` | Removes what `install` added, for every shell or only `--shell`. |
| `study completion bash`, `study completion zsh`, `study completion fish`, `study completion powershell` `[--no-descriptions]` | Prints a completion script, for packagers. `--no-descriptions` leaves out the help text shown next to each completion. |
| `study man` | Prints study's man page in roff, for packagers to install as `study.1` (hidden from help). The page's date is the one `SOURCE_DATE_EPOCH` names (seconds since 1970, UTC) when it is set, so a package built twice from one source holds the same page, and today's otherwise. It has no JSON form: with `--json` it is a usage error. |
| `study setup [--agent claude\|codex\|all] [--dry-run] [--force]` | Installs the `lamplight` skill and registers `study mcp` with Claude Code and Codex. See [Setting up agents](#setting-up-agents). |
| `study setup --check [--agent A]` | Reports what is missing or stale, changing nothing; exits 1 (`unhealthy`) when setup has something to do. |
| `study setup --remove [--agent A] [--dry-run]` | Undoes exactly what `study setup` did. |
| `study claude-plugin-path` | Writes the Claude Code plugin and prints its folder; the plugin marketplace runs it. |
| `study mcp` | Runs the MCP server over stdin and stdout. On start it refreshes a skill copy that `study setup` installed, when nobody changed it. |

The Study home is `STUDY_HOME` if set, otherwise `study_home` in
`$XDG_CONFIG_HOME/lamplight/config.toml`, otherwise `~/study`. It must be an absolute path
or start with `~/`, so every folder finds the same Study home.

Global flags: `--json` (below) and `--log-level` (see [The Log](#the-log)).

Human output is styled when stdout is a terminal. Styles are dropped when it is not, and
colours are dropped with `NO_COLOR=1`; `CLICOLOR_FORCE=1` keeps them in a pipe.

## JSON output

Every command accepts `--json`. With it, `study` prints exactly one JSON document on
stdout and nothing else; diagnostics, if any, go to stderr. `--json` is honoured even
when an earlier argument is invalid. Help (`--help`), `--version`, `study mcp` (which
speaks MCP on stdout) and the completion-script and man-page commands print text; a command
group run without a subcommand, such as `study topic --json`, is a `usage` error. With
`--json`, `study` never writes to the terminal or waits for it, even when stdout is one.

Success:

```json
{
  "ok": true,
  "data": { "...": "the command's result" }
}
```

Failure:

```json
{
  "ok": false,
  "error": {
    "code": "already_exists",
    "message": "a Topic named c already exists"
  }
}
```

Error codes:

| Code | Meaning |
|---|---|
| `usage` | Unknown flag, unexpected argument, or similar command-line mistake. |
| `invalid_argument` | The request was understood but its values are invalid, such as an empty title. |
| `already_exists` | The thing to create exists already. |
| `not_found` | The thing named does not exist. |
| `newer_format` | A file was written by a newer version of `study`; upgrade to read it. |
| `corrupt` | A file Lamplight reads is damaged or missing, for example invalid TOML after a hand edit, or a Topic's git repository is missing, was replaced during a Checkpoint, or leads outside the Topic. Fix or restore it; the message names it. |
| `failed_precondition` | The request is valid but the Topic is not ready for it, for example a git merge is in progress or git has no identity. The message says what to do. |
| `canceled` | The command was stopped, by SIGTERM or Ctrl-C, before it finished; what it would have recorded was not recorded. `study check` stops every program the Check started. |
| `busy` | Another program is using the Topic: another `study` process writing to it for too long, an editor using its git repository, or files that kept changing while they were being saved. Try again shortly; the message says what to do if it persists. |
| `unhealthy` | `study doctor` only: a Finding failed. `data` still holds the full diagnosis. |
| `internal` | Anything else, such as a file that cannot be read. |

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Success, including empty results, and `study doctor` with warnings only. |
| 1 | The command failed (`already_exists`, `not_found`, `newer_format`, `corrupt`, `failed_precondition`, `busy`, `canceled`, `unhealthy`, `internal`). |
| 2 | Usage error (`usage`, `invalid_argument`). |

## Flags in status

Each Topic in `study status --json` may carry `flags`: things replaying its History found
that need the learner's attention. Flags are reported, never resolved automatically.

```json
{ "id": "3f9c2a71b0", "kind": "conflict", "message": "...", "item": "topic.toml", "events": ["...", "..."] }
```

| Kind | Meaning | Goes away |
|---|---|---|
| `held_event` | An Event could not be applied: it refers to something the History doesn't contain (yet), or a newer version of `study` wrote it. While a Topic holds Events from a newer version, it can be read but not changed (`newer_format`). | when the Event becomes applicable, or when dismissed |
| `conflict` | Two Events changed one item from the same version, or two different Events share an ID: usually changes made on two machines without syncing in between. Also a Next step recorded for a Lesson the other machine completed, skipped or removed, and one Session closed on two machines with different notes. | when dismissed, after checking the item by hand |
| `damaged_line` | A line of `history.jsonl` is not an Event, such as a line cut off by an interrupted write that a merge moved into the middle of the file. It is skipped, never deleted. | when the line is fixed or deleted by hand, or when dismissed |
| `clock_ahead` | The History holds an Event dated more than a day after this computer's clock, so some machine's clock was wrong. New Events still sort after it. | when the clock catches up, or when dismissed |
| `edited_outside` | Content that approves or gates progress differs from the version its last Event recorded. | when Lamplight next records that item, or the content is restored |
| `interrupted_write` | A write to the Topic was interrupted; the next write to it finishes the job. | with the next write or Checkpoint |
| `card_flagged` | The learner flagged a Card during a Review as wrong or unclear, with their note if any. | when the Card is edited or deleted, or when dismissed |
| `lesson_header` | The current Lesson's YAML header cannot be read, so its Break points are unknown. | when the header is fixed |

`id` is stable across runs and machines, so a dismissal recorded on one machine applies on
every other. `item` and `events` are present when the flag concerns a particular item or
Events.

## Where the learner stopped

Once a Topic has a Syllabus or a Session, it carries `resume` in `study status --json`, and
human output shows it under the Active topic:

```json
{
  "lesson": "pointers", "lesson_title": "Pointers", "phase": "practicing",
  "break_point": { "id": "swap", "describe": "swap() works on two ints" },
  "next_step": { "step": "Fix the off-by-one in parse.c", "context": "...", "lesson": "pointers", "at": "..." },
  "open_session": { "id": "...", "opened": "..." }
}
```

`lesson` is the first Lesson in Syllabus order that is neither done nor skipped;
`syllabus_done` is `true` instead once every Lesson is. `break_point` is the last Break point
reached in that Lesson, with its description from the Lesson's header. `next_step` is the
latest Next step recorded, word for word, unless its Lesson is done, skipped or no longer in
the Syllabus. `open_session` is the newest Session not closed yet: in progress, or ended
without a Next step.

`status` also recommends one action for the Active topic, never counting anything:

```json
"recommended": { "topic": "c", "action": "next_step", "text": "Fix the off-by-one in parse.c" }
```

`action` is one of `next_step` (the text is the Next step word for word), `plan` (no
Syllabus yet), `adopt` (imported from v1 and not adopted yet: see
[Importing a v1 workspace](#importing-a-v1-workspace)), `learn`, `practice` or `feedback` (the current Lesson's Phase), `reviews`
(every Lesson done, Cards ready) or `explore` (every Lesson done); or, whatever the Resume
point says, `resume_topic` (the Topic is paused: resume it or pick another Topic), and for a
finished Topic `reviews` (Cards ready) or `stop` (no Card ready: nothing to study on it now).
Before the Next step comes `assess`, with `milestone` (`{number, id, title}`): every Lesson
of that Milestone is done or skipped, at least one done, and its end-of-Milestone Assessment
is the next thing to do. It names the Milestone finished most recently, on an active Topic,
and only while no milestone Assessment for it was recorded after its last Lesson was
completed and the learner has not deliberately moved on since by starting a Lesson of
another Milestone (`phase_set`). Stopping does not end it: a Next step recorded with an
Assessment due names the next Lesson's first step (or, when no Lesson follows, taking that
Assessment), the Resume point still shows it word for
word, and `assess` outranks it until the Assessment is recorded; then the Next step leads
again. A completion merged in from another machine for a Lesson already done does not bring
it back. A Topic carries the same Milestone as `assessment_due`. It is a next action, never
something late. The text of `study status` asks a learner without an agent to ask theirs
for it, or to record one with `study assessment record`.
The words match the Focuses and the suggestions of `session_open` wherever they mean the same
thing, and the list is fixed, so skills can rely on it; `text` is English prose a skill may
rephrase. An active Topic's recommendation always agrees with its Resume point. A Triage is
never the recommended action: it is something to consider (see
[Goal, Pace and Forecasts](#goal-pace-and-forecasts)). Each Topic's `cards` is
`{"ready": true}`, `{"ready": false, "next_due": "..."}`, or `{"ready": false, "paused": true}`
while the Topic is paused: whether Reviews are possible now, under the Topic's own daily cap
on new Cards, never how many Cards are due. Under a cap of 0, drafts alone are never ready and
give no `next_due`. `learner_profile` is the Study home's `learner.md` and a
Topic's `learner_additions` its own `learner.md`, when they exist as regular files (a
symbolic link is not followed); agents read both before teaching.

### Break points

A Lesson declares its Break points, in order, in its YAML header next to the Check:

```yaml
---
check:
  - id: tests
    run: [go, test, ./...]
break_points:
  - id: swap
    describe: swap() works on two ints
  - id: arrays
    describe: The same for arrays
---
```

Ids are slugs, unique within the Lesson. The Check and the Break points are read apart, so
a wrong type or an invalid value in `break_points` never makes the Check unreadable; a YAML
syntax error breaks the whole header, as it would any header. When the current Lesson's
header cannot be read, `status` flags it (`lesson_header`) instead of hiding its Break
points. `break_point_reached` (or `study session break-point`) records one, with a Next
step, in the current Lesson only: the Resume point's Lesson, Break point and Next step
always belong together. The next Session resumes from it. A Lesson removed by a Revision
loses its Break point, even if a later Revision adds it back.

### Next steps

A Next step is an action that starts with a verb and says what to act on: "Fix the
off-by-one in parse.go". `session_close`, `break_point_reached` and a `phase_set` that
gives one check it, without a language model, and refuse with `invalid_argument` a Next
step that:

- after any opening punctuation or symbols (quotes, `¿`, `**`, a backtick), does not start
  with a letter;
- is too short to say what to do: fewer than two words, or, in scripts written without
  spaces between words (Chinese, Japanese, Thai and the like), fewer than four characters;
- starts with a word that introduces a description rather than an action ("The parser is
  half done", "I'm on Lesson 3", "Done with the parser"); the first word is its leading run
  of letters, so contractions count by their first part. This word list is English; steps
  in other languages pass on the other rules;
- contains an invisible formatting character, such as a zero-width space.

All text Lamplight records also refuses control characters and bidirectional embedding,
override and isolate controls (U+202A–U+202E, U+2066–U+2069).

### Opening a Session

`session_open` (or `study session open`) returns, besides the Resume point:

- `suggested`, when an Energy is given and no Focus chosen yet: `suggest` is a Focus to offer
  (`learn`, `practice`, `reviews`, `explore`), `plan` (no Syllabus yet: plan it together),
  `stop` (fumes with nothing due: write tomorrow's first step and end here; or a finished
  Topic with no Card ready), `resume_topic` (the Topic is paused), `assess` (at full or
  half Energy, when `status` would recommend `assess`: offer the Milestone's Assessment
  first) or `adopt` (an imported Topic without a Syllabus: adopt it before planning). At
  fumes an Assessment waits for another day, and the suggestion is what it would be without
  one: `reviews` when Cards are ready, otherwise `stop`; `reason` is English
  prose a skill may rephrase. A paused Topic is never suggested for study, and a finished one
  only for its Reviews. A suggestion is never recorded.
- `paused`, when the Topic is paused. The Session opens anyway; the Topic stays paused until
  the learner resumes it (see [Goal, Pace and Forecasts](#goal-pace-and-forecasts)).
- `cards`, as in `status`.
- `long_gap`, when the Topic was last worked on, by any Event, more than a week ago: start
  with a short recap and a warm-up, never with the size of a backlog.
- `unclosed`, every Session that ended without a Next step, newest first, each with its id
  and when it opened; a merge can leave one from each machine. Give each its note with
  `session_close` naming it.
- `changes`, when a Session was left unclosed: what changed since the last Checkpoint
  (`since`; absent before the first Checkpoint, when every file counts as added). `files`
  lists at most 50, each `added`, `modified` or `deleted`, and `more` counts the rest.
  Lamplight's own state files (`topic.toml`, `syllabus.toml`, `history.jsonl`,
  `cards.jsonl`, `sources.jsonl`, `tasks.jsonl`) are left out. Listing them writes nothing to `.git` and
  runs nothing the repository names; when git cannot list them, such as during a merge,
  `error` says why and the Session opens anyway.

Once the learner chooses a Focus, record it on the open Session: `session_open` (or `study
session open`) with `session` (the open Session's id) and `focus` records a `session.focused`
Event and opens nothing new. Naming a closed or unknown Session is refused, and the same Focus
again records nothing.

A `phase.set` Event whose Phase is not `teaching`, `practicing` or `feedback`, or whose Lesson
is not a valid id, as a hand edit or another version could write, is held and flagged at
replay; it never reaches the Resume point.

**Stopping saves the work.** `session_close` and `break_point_reached` take a Checkpoint of the
turn they end, as `phase_set` does at a turn switch: the learner's while a Lesson is practicing,
the agent's otherwise. The result has `checkpoint`, or `checkpoint_error` and `checkpoint_role`
when it could not be taken: the stop is recorded anyway, and the Checkpoint stays owed, so
`checkpoint` with that role, or the next write that takes Checkpoints, saves it. A Checkpoint
still owed from earlier, such as a completion's, is taken instead and covers the stop. The
Checkpoint's own `checkpoint.taken` Event names the commit, so it is written after it: right
after a stop, `history.jsonl` is the only changed file, and the next Checkpoint saves it.

A note given late to an older Session never replaces a Next step recorded in a newer one. A
Next step that a merge brings in for a Lesson already done, skipped or removed never leads
the Resume point, and is flagged as a `conflict`; so is one Session closed on two machines
with different notes (the first note counts).

## Goal, Pace and Forecasts

A Topic's plan lives in `topic.toml`, next to its title and Goal, where the learner can read
and edit it; keys Lamplight does not know are kept:

```toml
deadline = "2026-12-01"      # the Goal's deadline, optional
new_cards_per_day = 10       # the daily cap on new Cards

[[pace]]                     # hours a week, in dated periods
hours_per_week = 10.0        # the first period may leave out from: from now on

[[pace]]
from = "2026-11-16"
hours_per_week = 3.0
```

Tasks live in `tasks.jsonl`, one per line, so two machines adding Tasks merge without a
conflict (the file merges by union, like the History):

```json
{"format":1,"id":"book-the-exam.k3f9a2","title":"Book the exam","by":"2026-11-01","after":"core"}
```

`by` is a date shown as is, never counted as late; `after` names a Milestone, and the Task is
shown once that Milestone is done, or with a note if a Revision removed it. The learner may
add lines by hand with any id short of 100 bytes without `#` or control characters; such a
Task can be marked done and removed like the others. A line that is not a Task is named in
`settings_problems` and kept as it is; the other Tasks still work.

Whether a Topic is `active`, `paused` or `finished`, and which Tasks are done, come from its
History, never from a file. A paused Topic offers no Cards and has no Forecasts, and stays
paused until the learner resumes it (`--state active`); `status` recommends `resume_topic`,
and opening a Session on it works and says `"paused": true`. A finished Topic has no
Forecasts and keeps offering its Cards: `status` recommends `reviews` when they are ready and
`stop` otherwise. A Topic made
paused on one machine and finished on another, from the same state, is flagged as a
`conflict`; marking a Task done on one machine and not done on another is not: the last mark
wins.

A setting that cannot be read, such as a hand-edited Pace with negative hours or without
`hours_per_week`, is left out and named in `settings_problems`; the rest of the Topic stands,
and setting it again, or removing it (`--deadline ""`, `--clear-pace`), fixes it. A
`topic.toml` that still holds git's conflict markers after a merge is `corrupt`, with the
fix: keep one side of each block and delete the marker lines.

The **Forecast** says when each Milestone ends at the Pace, never how far behind anything
is. It is in `status` (`forecast`) and in `study syllabus` and `syllabus`:

On 1 Oct 2026, with 20 hours left in Core, of which "Structs" is 6, and a target of 10 Oct:

```json
{
  "pace": "10 h/week, then 3 h/week from 16 Nov 2026",
  "milestones": [
    { "milestone": "core", "title": "Core", "priority": "must",
      "deadline": "2026-10-10", "deadline_from": "target",
      "remaining_hours": 20, "ends": "2026-10-14", "after_deadline": true,
      "text": "At 10 h/week, Core ends 14 Oct 2026; its target is 10 Oct 2026." }
  ],
  "triage": { "milestone": "core", "deadline": "2026-10-10", "deadline_from": "target",
    "ends": "2026-10-14", "raise_pace_to": 14, "move_lessons": ["structs"], "trim_hours": 5.8,
    "suggest_deadline": "2026-10-14",
    "text": "To finish Core by 10 Oct 2026: raise the Pace to about 14 h/week until then, move \"Structs\" past the deadline, or trim about 5.8 h of Stretch goals." }
}
```

(At 10 h/week, 20 h take 14 days, so Core ends 14 Oct. The ten days to 10 Oct fit about 14.3
of the 20 hours, so about 5.8 h do not; 20 h in ten days is 14 h/week.)

- Work is the hour estimates of the Lessons neither done nor skipped, a Lesson in progress
  in full, taken through the Syllabus in order. Any estimate counts as at least a minute. A
  Milestone with a Lesson that has no estimate is not given a date, and nor are the ones
  after it: `unestimated` names the Lessons that need one. Without a Pace, `needs_pace` is
  `true` and no dates are given. `remaining_hours` is rounded to a tenth of an hour.
- Days are calendar days on this computer's clock, starting today, counted as dates so a
  daylight saving change never adds or loses one. Each day adds its Pace period's hours a
  week divided by seven; the count is exact, in minutes. A period of 0 h is shown as a break.
  A Forecast looks ten years ahead at most.
- A Milestone's `deadline` is its target date (`deadline_from: target`), or the Goal's
  deadline for a `must` Milestone without one (`deadline_from: goal`). `after_deadline` is
  set when it is forecast to end later.
- A **Triage** is offered for the first `must` Milestone forecast to end after its
  deadline. It is something to consider, never the one next action, and it offers only what
  can still finish the Milestone in time:
  - the Pace that does (`raise_pace_to`: hours a week from today to the deadline, rounded up
    to a half hour; "set" rather than "raise" when it is not above today's Pace), left out
    above 168 h a week; or, when the deadline is today, the hours to work today
    (`hours_today`, up to 24);
  - the fewest Lessons not started that fit it in time if moved past the deadline
    (`move_lessons`: optional Milestones' Lessons before it first, then its own from the
    last), left out when that is not enough or would move all its own work;
  - the hours of Stretch goals to trim (`trim_hours`), left out when that is more than half
    the Milestone's own work, since Stretch goals are optional extras.

  When none of them can, the text says so and `suggest_deadline` offers the date it is
  forecast to end. Once the deadline has passed, `deadline_passed` asks for a new date first.
  Nothing changes until the learner chooses: a Revision to move Lessons, trim Stretch goals
  or change a target date, or `study topic update` for the Pace or the Goal's deadline.

## The Syllabus

`syllabus.toml` holds Milestones, each with an `outcome`, a `priority` (`must`, `if_time` or
`after_deadline`) and an optional `target` date, written as text (`"2026-12-01"`; a native
TOML date written by hand is read too), and Lessons with an `id`, a `title`, `hours` and
`skipped = true` for a skipped Lesson. Display numbers ("Lesson 2.3") come from position.
Lamplight rewrites the file only from approved Revisions, and keeps settings it does not
know at every level.

`study syllabus --json` returns:

```json
{
  "topic": "c",
  "milestones": [
    { "number": 1, "id": "basics", "title": "Basics", "priority": "must", "target": "2026-12-01",
      "lessons": [ { "number": "1.1", "id": "pointers", "title": "Pointers", "hours": 2,
                     "status": "in_progress", "phase": "practicing", "evidence": 1 } ] }
  ],
  "proposals": [ { "revision": "...", "summary": "...", "changes": { ... }, "stale": false } ],
  "edited_outside": true,
  "file_error": "syllabus.toml of c: Lesson 2.1 (maps): the title is empty"
}
```

A Lesson's `status` is `not_started`, `in_progress`, `done` or `skipped`. A proposal's
`changes` names Lessons by title: each change with its `kind`, `renumbered`
(`{lesson, title, from, to}`), `skipped_in_progress` (Lessons skipped while in progress: go
over what was already covered with the learner) and `text`, the whole change in plain
words. A `stale` proposal was based on a Syllabus that has changed since; propose it again.

| Change `kind` | Meaning |
|---|---|
| `first_syllabus` | The Topic's first Syllabus, followed by each Milestone and Lesson it adds. |
| `file_adopted` | The Revision adopts `syllabus.toml` as edited by hand (`--from-file`). |
| `milestone_added`, `milestone_removed` | A Milestone added or removed. |
| `milestone_changed` | A Milestone's title, outcome, priority, target date or position changed. |
| `lesson_added`, `lesson_removed` | A Lesson added or removed. |
| `lesson_renamed`, `lesson_moved`, `lesson_hours` | A Lesson's title, Milestone or hour estimate changed. |
| `lesson_skipped`, `lesson_unskipped` | A Lesson skipped, or its skip taken back. |
| `lessons_reordered` | Lessons that stay in a Milestone change places within it; `milestone` names it. |
| `settings_changed` | Only settings Lamplight does not know changed. |

Rules a Revision follows:

- Done and skipped Lessons keep their title, hours and Milestone, and are never removed; a
  done Lesson cannot be skipped. A Revision can take a skip back. Skipped Lessons keep
  their number, are left out of the Resume point, and cannot be studied.
- A Revision must change something; adopting the file as it is (`--from-file`) is the
  only Revision that may change nothing.
- A Revision is based on the Syllabus version the History recorded. While `syllabus.toml`
  differs from it (flagged `edited_outside`, with what keeps the edit from being adopted),
  the only Revision allowed adopts the file as it is: `--from-file`. A write interrupted
  before it rewrote `syllabus.toml` is no edit: the next write finishes it.
- Before asking the learner directly, study checks that the Revision can be applied as it
  stands, so an answer is never asked for and then thrown away.
- Approval records how the learner answered (`via`): `terminal` or `elicitation` when
  Lamplight asked them directly, with the question it showed (`shown`); `chat` when an agent
  relays their words (`learner_said`, required). What the learner adds when asked directly
  is kept on one line, without control characters, cut at 500 characters; it never makes
  their answer fail. Approvals are tamper-evident, not tamper-proof.
- A declined Revision is never applied. Changes made on two machines without syncing are
  flagged, never resolved: two Revisions (or a Revision and an adopted hand edit) approved
  from one Syllabus version, a Revision applied on one and declined on the other, and a
  Lesson completed on one and removed, skipped or rewritten on the other, in either order.
  One Revision approved on both machines is not a conflict.

## Checks

A Lesson's Check is written in the YAML header of `lessons/<lesson-id>.md`. Each criterion
has an id and exactly one of `run`, `rubric` or `held_out` (at most 30 criteria):

```yaml
---
check:
  - id: tests
    describe: The tests pass
    run: [go, test, ./...]              # a command that can be repeated
  - id: names
    rubric: Every function name says what it does   # graded by the agent
  - id: accuracy
    describe: Accuracy on the test set
    held_out: [python3, eval.py]        # evaluated on the Held-out data
---
```

The Check's version is the hash of its criteria, kinds included, so editing a rubric item's
text changes it too. A Check needs at least one run criterion or rubric item: one with only
`held_out` criteria is refused as `corrupt`, since held-out results never decide whether a
Lesson is done.

`study check <lesson>` runs each `run` and `held_out` criterion's command, an argument list
rather than a shell string, in `practice/<lesson-id>/`, run criteria first, with no
standard input and these variables set. Any other `STUDY_` variable in `study`'s own
environment is removed first:

| Variable | Set for | Meaning |
|---|---|---|
| `STUDY_TOPIC`, `STUDY_LESSON` | every command | the Topic's and the Lesson's ids |
| `STUDY_RESULTS` | every command | where the command may write its results file, in a folder of its own outside the practice folder |
| `STUDY_HELDOUT_DIR` | `held_out` commands only | the Lesson's Held-out data, `.heldout/<lesson-id>/` in the Topic |

A Check with only rubric items has nothing to run (`failed_precondition`): grade its items
instead. Progress goes to stderr: each criterion as it starts, and every 15 seconds while a
long one runs; with `--json`, only the latter, so a quick Check keeps stderr empty.

- **Outcomes.** A run criterion passes when its command exits with 0 and its results file,
  if any, does not say `"passed": false`; it fails when the command exits with another
  status or the results say so; and it errors when the command cannot start, is stopped by
  a signal, runs longer than `--timeout` (default 30 minutes), leaves programs running in
  the background, or writes a results file that is not valid. A `held_out` criterion
  follows the same rules but must write a results file with a `score`: without one, or
  without Held-out data, it is errored. **The Attempt's outcome comes from its run criteria
  alone**: held-out results are diagnostic and never change it. A Check without run
  criteria takes the outcome from its held-out results, so its Attempt passes only when
  every one passed; completion still never depends on them.
- **Processes.** Each command runs in a process group of its own. On a timeout, or when
  `study` receives SIGTERM or Ctrl-C, the whole group gets SIGTERM, then SIGKILL after
  two seconds; whatever a command leaves running when it exits is killed too. A Check
  stopped by SIGTERM records nothing and reports `canceled`. A program that leaves the
  group, with `setsid` say, escapes this and can outlive the Check; containing it is the
  sandbox's job (ADR-0009).
- **The work.** The practice folder is snapshotted before the run, after the run criteria
  and after each `held_out` command, writing nothing to `.git`. The snapshot covers every
  file that is not ignored and every ignore rule that applies inside the folder (each
  `.gitignore` on the way and inside it, `.git/info/exclude`, the global excludes file),
  so build outputs can be ignored, but ignoring a file after a pass changes the snapshot.
  If a run criterion changed the work, the Attempt is errored and the changed files are
  named; list files a Check writes in the folder's `.gitignore`. A `held_out` command must
  not write into the work either: if it does, that criterion is errored, its results are
  not kept and its run never counts, and the Attempt stands. The Attempt is also errored,
  without running anything, when the snapshot would have a blind spot: files the index
  marks skip-worktree or assume-unchanged, another git repository in the folder, a
  symbolic link leading outside it, or a folder whose files are all ignored.

**Results files.** A command may write a JSON object of at most 64 KiB to `STUDY_RESULTS`.
Every field is optional, except `score` for a `held_out` criterion; other fields are ignored
and never recorded, and an invalid file makes the criterion errored:

```json
{"format": 1, "passed": true, "score": 17, "max": 20,
 "metrics": {"precision": 0.91, "recall": 0.88}, "summary": "17 of 20 cases"}
```

`format` is 1 (a newer one asks to upgrade `study`); `score` is at least 0 and at most
`max`, which defaults to 1 and must be more than 0; `metrics` holds at most 20 numbers,
each named with 1 to 40 lowercase letters, digits, `_`, `.` and `-`, starting with a letter
or a digit; `summary` is text of at most 1,000 characters for the learner. The file must be
a regular file, not a link or a FIFO; it is opened without following links and read up to
the limit only. A reason that quotes the file, such as a bad metric name or a JSON error,
quotes at most 80 characters of it.

`study check --json` prints the Attempt with the end of each run criterion's output. The
History records the Attempt without any output, which could reveal test data, and a
`held_out` criterion's output is never kept at all. `not_counted` says why a held-out run is
not the counted measurement:

```json
{
  "id": "...", "lesson": "pointers", "check_version": "sha256:...", "snapshot": "sha256:...",
  "outcome": "failed", "at": "...",
  "criteria": [
    { "id": "tests", "kind": "run", "outcome": "failed", "exit_code": 1, "output": "the end of what it printed" },
    { "id": "accuracy", "kind": "held_out", "outcome": "passed", "exit_code": 0, "counted": false,
      "not_counted": "an earlier run is the counted measurement",
      "results": { "score": 0.87, "max": 1, "summary": "87 of 100 cases" } }
  ]
}
```

The exit code is 0 whenever the Attempt was recorded, whatever its outcome. Checks run
only through the command line, from the agent's own shell, so the agent's sandbox applies
(ADR-0009); the MCP server reads Attempts (`check_results`) but never runs a Check.

**Held-out data** lives in `.heldout/<lesson-id>/`, which is committed with the Topic so
every machine measures on the same data: the default `.gitignore` ends with `!/.heldout/`
and `!.heldout/**`, so the folder and its files are committed whatever their format, even
when a line above, such as `.*`, would leave the folder out. It is synthetic or public, never
personal data. Lamplight never shows its contents, the files' names (they are left out of
the changes an unclosed Session lists and of a Checkpoint's `large_files`) or a `held_out`
command's output, only its results.

The **counted** measurement of a `held_out` criterion is its first run that produces
results, on the Check shown to the learner when practicing last started, in an Attempt that
is not errored. Every other run is recorded as not counted, with the reason: the Check was
not shown yet, this version of it is not the one shown, the Attempt errored, or an earlier
run counted. So editing the Check never makes a fresh first run, and an errored run never
uses the count up. A criterion is known by its id, so renaming a `held_out` criterion does
start a new count: agents must never rename or re-create one to get a fresh first run. Two
machines that both measured first are flagged.

**Rubric items** are graded by the agent, after the learner checks their own work against
them, with `study rubric grade` or the MCP `rubric_record` tool: `met`, `partly` or
`not_met`, with an optional note. A grade is for the Check shown to the learner and the
work as it is now, and the practice folder must hold work: at least one file that is not
ignored. **Written work** is files in the practice folder: typed final answers in a text
file, or a photo of paper work. A grade names the files it looked at, relative to the
practice folder or to the Topic; each must be a file the work's snapshot sees, so never an
ignored file or a link, and it is opened inside the practice folder, so nothing leads out
of it. Only their paths and content hashes are recorded.

| Command | What it does |
|---|---|
| `study rubric grade <lesson> <criterion> --grade G [--note N] [--looked-at FILE]... [--topic ID] [--dry-run]` | Grades a rubric item. Without `--topic`, it uses the Topic whose folder it runs in. The same grade, note and files again record nothing. |
| `study results <lesson> [--topic ID]` | Shows the Check, the Attempts, each rubric item's grade (and whether it is for the current Check and work), each `held_out` criterion's counted measurement and latest run, whether the Lesson can be completed, and what to do next. Runs nothing. Without `--topic`, it uses the Active topic. |

With `--json`, each criterion of the Check has its `id`, `kind` (`run`, `rubric` or
`held_out`), `describe`, and either `command` (for `run` and `held_out`) or `rubric`.

**Completion.** A Lesson can be completed when, for the Check shown to the learner when
practicing last started (through `phase_set`), which must still be the current one, every
run criterion passed on an Attempt of the current work, and every rubric item has a grade
for that Check and that work, whose files it looked at are unchanged. Every rubric item
needs a grade, but which grade does not matter: `not_met` counts as graded. Held-out
results never decide it. A Check edited afterwards is flagged in `status` and must be shown
again; changing the work after a pass means running the Check, and grading, again.
`lesson_complete` records the Attempt and the grades it relied on, and a done Lesson is
never reopened. When the completion finishes its Milestone of an active Topic (every
other Lesson of it is done or skipped), the result has `next`, with the Milestone; calling
it again returns the same `next` while that Milestone's Assessment is still due:

```json
"next": {"code": "assess_milestone", "text": "Milestone 1 “Basics” is finished: assess it together with the learner, then record it with assessment_record", "milestone": {"number": 1, "id": "basics", "title": "Basics"}}
```

**After a failed Attempt**, one where a run criterion of the Check shown to the learner
failed, the agent gives feedback, then moves the Lesson to practicing with `phase_set` and
a Next step that names the fix, whatever Phases it goes through on the way: without one,
`phase_set` refuses with `invalid_argument`. Once that Next step is recorded, none is asked
for again until another Attempt fails. `study results` and `check_results` say so in
`next`, with a `code` for programs and a `text` for people:

```json
"next": {"code": "name_the_fix", "text": "give the learner feedback on the failed Attempt, then move the Lesson to practicing with phase_set and a Next step that names the fix"}
```

## Assessments, Level and learning signals

An **Assessment** is a short quiz, kept to its time box (15 minutes unless set): the
placement Assessment when a Topic is created, before its Syllabus, and one at the end of
each Milestone. It is recorded by an `assessment.recorded` Event:

```json
{
  "kind": "placement",
  "items": [
    {"area": "variables", "question": "What does x := 1 do?", "outcome": "correct"},
    {"area": "pointers", "outcome": "incorrect", "note": "confused * and &"},
    {"area": "goroutines", "outcome": "not_reached"}
  ],
  "summary": "Knows variables; pointers need work",
  "minutes": 13,
  "time_box": 15,
  "level": "beginner",
  "notes": "notes/placement.md",
  "request": "placement-go"
}
```

- `kind` is `placement` or `milestone`; a `milestone` Assessment names its `milestone`,
  which must be in the Syllabus.
- Each item has an `area`, an optional `question` and `note`, and an `outcome`: `correct`,
  `partly`, `incorrect`, or `not_reached` when time ran out before it.
- `time_box` is from 1 to 240 minutes, 15 when left out. `minutes`, how long it took, is
  optional: left out it is not known, which is not the same as 0. It is never more than the
  time box.
- The result lists the areas to work on (`weak`: `incorrect` or `partly`) and those to
  `confirm` during Lessons (`not_reached`). Weak results never block anything. After a
  `milestone` Assessment with weak areas, the result carries
  `"next": {"code": "propose_revision", "text": ...}`: the agent proposes a Revision for
  them, such as a review Lesson.
- `notes` names the file in the Topic's `notes/` folder where the agent saved the
  Assessment, with forward slashes: a regular file, not a link; only its path and hash are
  recorded. A name with a backslash is refused.
- `request` is an optional id the client chooses: a retry with the same id records
  nothing, and the same id with a different Assessment is refused. Without one, the same
  Assessment as the latest records nothing, unless the learner changed the Level since; a
  retake after that is the next Assessment, and sets the Level again.
- Replay checks each `assessment.recorded`, `level.set` and `hint.recorded` Event as a
  write would; one that fails, from a hand edit or another version, is held and flagged.

The **Level** (`beginner`, `intermediate`, `advanced` or `expert`) lives in `topic.toml` as
`level`. An Assessment with a `level` sets it; the learner can change it at any time with
`study topic update --level` (`level.set`), and that choice holds until the next Assessment
sets one. Each Topic in `status` carries `level: {level, source, assessment?, at?}`, where
`source` is `assessment`, `learner` or `import` (taken by `study import` from a v1 workspace's
difficulty: v1's estimate, until an Assessment or the learner sets the Level); a Level edited
into `topic.toml` by hand is the learner's, without `at`, and one that is not a Level is
reported in `settings_problems`.

The Level changed on two machines is flagged as a conflict that can be dismissed: "the
Level was changed on two machines", naming both Events. That covers two choices made from
the same version, and also a later Assessment on one machine that kept the Level
`topic.toml` already held there while the learner chose another on the other: after the
merge, `topic.toml` keeps the learner's choice although the History's last writer is the
Assessment. `status` then reports the Level the file holds, with the Event that set it,
never as a hand edit. Choosing a Level with `study topic update --level`, even the one the
file holds, records it and clears the flag; dismissing the flag keeps the file's Level.

The **Approach** is how a Topic's Lessons relate: `concepts` (standalone concepts, each
with its own exercises), `project` (one project built step by step, Lesson by Lesson) or
`challenges` (a run of challenges of growing difficulty). The agent chooses it with the
learner when creating the Topic, and it lives in `topic.toml` as `approach`, set by
`study topic update --approach` (`approach.set`); `status` shows it, and one that is not an
Approach is reported in `settings_problems`.

A **hint** (`hint.recorded`) is help given while the learner practises a Lesson that is not
done or skipped: a `nudge` (a question or a pointer), an `explanation` of a concept again,
or a `step` of the way to a solution. `requested_by` says who asked: the `learner`, or the
`agent`, which offered it unasked. Every call records a hint, except a retry with the same
`--request`, which returns the first hint recorded with it.

**Learning signals** are what a future Level suggestion will use; v2.0 records them and
makes no suggestion yet. `study signals --json` (and the `signals` tool) derives them from
the History. They are for the agent to adapt how it teaches, never shown to the learner as
counts or scores, so `study signals` is hidden from help and answers in JSON only.

| Signal | Derived from |
|---|---|
| `first_try` | The first Attempt measured on the Check shown to the learner passed. An Attempt is measured when it ran run criteria, was not errored, and used the Check shown; an agent's try before showing the Check is not. A Check without run criteria, with only rubric items or `held_out` criteria, has no `first_try`. |
| `attempts` | The Attempts measured. |
| `feedback_rounds` | The times the Lesson went to feedback. |
| `hints` | The hints recorded, by kind; `hints_requested` counts those the learner asked for and `hints_offered` those the agent offered unasked. |
| `held_out` | For each `held_out` criterion, its counted score against the run criteria's mean score in the same Attempt (each a results score over its max, or 1 for passed and 0 for failed), and the `gap`: dev minus Held-out, positive when the work did better on the learner's own tests. |
| `reviews` | Review ratings: per Lesson, and for the Topic with Explore Cards. |

`totals` sums them: Lessons passed on the first try out of those measured, feedback rounds,
hints and the hints the learner asked for. `assessments` lists the Topic's Assessments,
oldest first.

## Cards and Reviews

A Card is one fact: a prompt and its expected answer, one line of `cards.jsonl`, in the order
Cards were written. Prompts and answers may span lines and hold tabs, for code; other control
characters are refused. A Card may cite Evidence by id (`--evidence`, `evidence` in MCP); the
ids must be recorded in the Topic's History and not retracted. Its ID is
`<lesson-id>.<random suffix>`, or `explore.<random suffix>` for an Explore Card written
without a Lesson, which is why no Lesson may be called `explore`. Its display number
("Card 4") comes from its position among the Topic's Cards. Whether a Card is a draft,
suspended, flagged or due is replayed from the History, never stored in the file.

A new Card is a draft until its first Review, where the learner keeps, edits or drops it.
Each day, counted on this computer's clock, at most 10 drafts are decided (the Topic's
`new_cards_per_day` changes that), so new Cards never pile up: `study card due` and
`due_cards` offer the Cards due first, earliest first, then as many drafts as are left of
the day's cap. A paused Topic offers no Cards until it is active again; a finished Topic
keeps offering them, at the growing intervals FSRS gives. Without `--limit`, the list is sized to the Energy, given with `--energy` or
taken from the open Session: 20 Cards at full, 10 at half, 3 at fumes, and 10 without one.
Suspended Cards are never offered. Neither command, nor `study review`, ever says how many
more Cards are due.

Scheduling replays every Review through FSRS-6 (go-fsrs v4, with fuzz and short-term steps
off), using the time each Review was really made, never earlier than the Card's previous
Review, so the same History always gives the same schedule on every machine. Every Review,
the first included, schedules the Card in days: Lamplight works in sessions, so a Card due
"in 10 minutes" would only come back next time anyway.

Reviews are safe to retry. A Review given a request id records nothing when the same id comes
again, and returns what was recorded; repeating a draft's first decision does the same. Over
MCP, `review_record` requires a request id, since a client may retry a call it saw fail. A
Review of a Card deleted on another machine, a delete that had not seen a Review made
elsewhere, and a draft decided on two machines are flagged in `status`.

`study review` shows each Card's prompt and asks the learner to recall the answer. Enter
shows it; the learner may type their answer first, and it is shown beside the real one to
compare. A line holding just `f` flags the Card as wrong or unclear, `s` skips it and `q`
stops. The learner then rates their recall with a single key: `1` again, `2` hard, `3` good,
`4` easy. At a new Card's first Review, `k` keeps it, `e` edits it (an empty line keeps the
prompt or the answer; the edit is shown and saved only after `y`) and `d` drops it, after
asking `y/N`. Before each question, whatever was typed ahead is discarded, so a key only ever
answers a question already on screen; arrow keys and other escape sequences are ignored.
Ctrl-C, SIGINT, SIGTERM or SIGHUP stop the session like `q`: the terminal is restored, every
Review made so far is kept, and `study` exits with 0 after "Stopped.". In a terminal each
key is one keystroke; otherwise each answer is read from one line of standard input, so a
script can drive it.

### Lines a merge leaves

`cards.jsonl` and `sources.jsonl` merge by union, so syncing can leave several lines for one
Card or Source. Every reader and every write picks among them by one rule, using what the
History recorded:

- a line at the version the History recorded last is the entity;
- lines at versions an earlier Event recorded are debris of the merge, and are ignored;
- a line at a version the History never recorded is an edit made outside Lamplight. In
  `cards.jsonl`, which is authoritative for text, one such edit wins, as a hand edit does
  without a merge, and several are a conflict in `status`. In `sources.jsonl`, whose text the
  History holds, the recorded version wins and any such edit is a conflict.

The order of the lines never matters, so every machine reads the same entity, and the next
change to it leaves a single line.

## Importing a v1 workspace

`study import <v1-workspace>` turns a workspace of the v1 study skill (`.study-config.json`,
`lessons/`, `practice/`, `notes/`, `.fsrs/`, in a repository) into a new Topic. Run it with
`--dry-run` first: the report lists the size of the copy, everything that will be converted
and moved, the Lessons proven done with their proof, those left open, and everything left
out, and nothing is written. The workspace itself is only read, never changed, and its
history is read through the same hardened calls as Checkpoints, which run no program its
configuration names.

The import copies the workspace, `.git` included, so every commit is kept, and copies every
file, git-ignored or not, since ignored files can be the learner's data. The Topic's id is
the workspace folder's name unless `--topic` gives another, and v1's names become ids:
Lesson 1 is `lesson-01`, as v1 named its practice folder, so paths quoted in Lesson text and
in `.gitignore` keep working. The workspace is recognised by its real path, links resolved.

| v1 | v2 |
|---|---|
| `topic`, `end_goal` | the Topic's title and Goal |
| `difficulty_override`, else `difficulty` | the Level, with `source: import` |
| `approach` (`concept`, `project`, `challenge`) | the Approach (`concepts`, `project`, `challenges`) |
| `lessons[]`, numbered | Lessons `lesson-NN`; a file under `lessons/` moves to `lessons/lesson-NN.md` |
| a Lesson `completed` with proof | done, through the import's Event, with no Attempt |
| `lessons/plan.md` (v1's project approach only) | `notes/v1-plan.md`, for the adoption Session |
| `.study-config.json` | `topic.toml`; the original is kept as `notes/v1-config.json` |
| `sources[]` (paths, or objects with `path`, `url`, `title`, `notebook_id`, `source_id`) | Sources: a file inside the workspace by its path there, one outside by its content, URLs as they are |
| `notebooklm` (an id, a NotebookLM address, an object with either, or `{"notebooks": [...]}`), or a source's `notebook_id` | a NotebookLM Knowledge base (the first notebook; others are dropped). A Source keeps the notebook it declared |
| `session_state` (`pending_action`, `context`, `phase`) | `v1_next_step`, for the adoption Session's Next step |
| `syllabus.toml`, `cards.jsonl`, `sources.jsonl`, `tasks.jsonl` at the top | `notes/v1-<name>`, so they are not taken for Lamplight's own |
| `.gitignore`, `.gitattributes` | merged: the learner's lines first, Lamplight's last, so they win |

**Moves** are planned before anything is copied, and the dry run and the import share the
plan. A Lesson's file moves only if it is a regular file under `lessons/`, where v1 writes
them; anything else stays where it is and is not the Lesson's file (nothing under `.git`
ever moves). A file two Lessons share moves with the first. A move whose target exists, or
whose folder is a file, does not happen; the file stays where it is, with a note.

**Proof.** A Lesson v1 calls `completed` (or `complete`, `done`) counts as done only when its
Lesson file is under `lessons/` and v1's own records show it. v1's Lesson Completion Contract
(`references/workspace-lifecycle.md` in v1) adds an FSRS card with id `lesson-NN`, then
commits `[agent] complete lesson NN`. A commit proves a Lesson only when its whole subject is
that (`completed` is accepted, and `lesson 1` never matches `lesson 10` or `lesson 4.5`), and
no newer `Revert "..."` of it exists; otherwise the card does. The report shows each proof,
the commit's hash and subject or the card, and a Lesson without one stays open, saying why.
`--not-done lesson-NN`, repeatable, keeps a Lesson open whatever its proof: check the dry run
first, since a Topic imported already must be removed (`study topic remove <topic>`) before
importing it again.

**The git files.** In a `.gitignore` and a `.gitattributes`, the last matching line wins, so
the learner's lines come first and Lamplight's last. The `.gitignore` gets Lamplight's
default lines it lacks, then `!/topic.toml`, `!/history.jsonl`, `!/syllabus.toml`,
`!/cards.jsonl`, `!/sources.jsonl`, `!/tasks.jsonl`, `!/.gitattributes` and `!/.gitignore`,
so a v1 line such as `*.jsonl` cannot leave the state out of Checkpoints. The
`.gitattributes` ends with Lamplight's union merges, and resets `filter`, `text`, `eol`,
`ident` and `working-tree-encoding` on the state files, so a v1 line such as
`*.jsonl filter=lfs merge=lfs -text` leaves them alone. The `topic.imported` Event records
the merged `.gitattributes`.

**Left out of the copy**, each listed under `dropped` with its reason: links leading outside
the workspace, directly or through another link (links inside it are copied as links);
entries that are neither files, folders nor links (FIFOs, sockets); v1's lesson-level cards
(`.fsrs/`, kept in the history); and folders the language's tools rebuild, unless the
history tracks them: `node_modules`, `.venv`, `venv` and any folder holding `pyvenv.cfg`,
`__pycache__`, `.pytest_cache`, `.mypy_cache`, `.ruff_cache`, `.tox`, and `target` next to a
`Cargo.toml` or `pom.xml`.

**Dropped from the config**, each with its reason: templates, the mode, calibration rounds
(`next_calibration_at_lesson`, `difficulty_override_at_lesson`), progress counters, the
review queue, the catalog path, companions' settings, Energy and time budget, v1's creation
date, each Lesson's `metrics`, keys the importer does not know (they stay in
`notes/v1-config.json`), and Lesson entries it cannot read. Lessons are read one at a time:
a `num` written as `"1"` or `1.0` is read as 1, and an entry without a whole number, or that
is not an object, is dropped alone.

**Refused:** a folder without `.study-config.json`; one inside the Study home, or holding it;
one that is a Lamplight Topic already; a config version newer than 3, or more than 500
Lessons or 200 sources; a config that is not a regular file or is over 1 MiB; a linked
worktree or submodule (`.git` is a file); a repository that borrows objects
(`.git/objects/info/alternates`, `failed_precondition`; run `git repack -a -d` in it, then
delete that file); one where git is writing (`index.lock`, `HEAD.lock`, `packed-refs.lock`
or a lock under `refs/`: `busy`, naming the file to delete if no git command is running); a
`.gitignore` or `.gitattributes` that is a folder; a file named like Lamplight's whose
`notes/v1-<name>` is taken; a `--not-done` Lesson the config does not list; an id that is
taken (`already_exists`); and a workspace imported already with the same config and HEAD
(`already_exists`, naming the Topic). An import stopped by SIGTERM or Ctrl-C reports
`canceled`, and leaves nothing.

Imports run one at a time, under a lock in `.lamplight/locks/`, so two imports of the same
workspace cannot both succeed. The Topic is assembled under `.lamplight/tmp` and moved into
place only when complete, so an interrupted import leaves no Topic, and importing again
starts over; each import first removes staging folders over an hour old whose Topic no
process holds the lock of. It records `topic.created`, then the settings (`level.set` with
`source: import`, `approach.set`, `knowledge_base.set`), one `source.added` per Source, and
one `topic.imported` Event, which writes the `.gitattributes` and whose payload is the
report: where it came from (`from`, `head`, `config_version`, `config_hash`), where the plan
and the config now are (`plan`, `config`), the Lessons kept open with `--not-done`
(`not_done`), what was converted, moved and dropped, the Lessons proven done with their
proof, the open ones, and where v1 stopped. A Checkpoint then saves the import; without an
identity for commits the result carries `checkpoint_error`, and the next Checkpoint saves it.

An imported Topic shows `imported` in `status`: `from`, `at`, `adopted`, `plan` (absent when
v1 wrote none), `config`, `v1_next_step`, and the report the adoption works from:
`completed` with their proofs, `open` and `dropped`, each cut to its first 100 entries, with
`more` counting what was cut (the `topic.imported` Event holds everything). It carries the
recommendation `adopt` until it has a Syllabus. The adoption, with the agent, is a
checklist: the Goal and deadline, the Pace, the Syllabus from `notes/v1-plan.md` (v1's three
tiers become the priorities `must`, `if_time` and `after_deadline`), or, without a plan,
from v1's lesson list and the learner's notes, approved as a Revision that keeps every
Lesson proven done, Checks for the open Lessons, the Knowledge base, Cards for the completed
Lessons, and a Next step from where v1 stopped.

## Writes

Commands that write accept `--dry-run`, which validates the request and reports what would
change without writing anything.

Every write to a Topic takes the Topic's lock, so the CLI and the MCP server can run at
the same time. Once it holds the lock, it checks that the Topic's folder is still the one it
opened; if the Topic was removed or replaced meanwhile, it fails with `not_found` and
writes nothing. `study check` and `study checkpoint` open the folder once, before the Check
runs or the commit is made, so a Topic removed or replaced while its Check ran gets no
Attempt, and a Checkpoint never commits in a folder that another program put in the
Topic's place. A write records its Event in the History before it changes any content, so one
interrupted by a crash is finished by the next write or Checkpoint. A dry run reports
what the real run would do after finishing such a write, and writes nothing. Lock and
intent-marker files live in the Study home's `.lamplight/` folder, which holds this
computer's local state (removed Topics too). Lamplight never syncs it; if a file-sync tool
syncs the Study home, exclude `.lamplight/` from it. Topics sync through git.

## Sources and Evidence

The Knowledge seam ([ADR-0007](adr/0007-knowledge-bases-return-evidence.md)) records where
a Topic's material comes from. `study` never parses a document and never calls a knowledge
service: the agent searches the Knowledge base itself, such as a NotebookLM notebook through
the NotebookLM MCP server, or reads the Sources when there is none, and records the quotes
it relied on.

- **Knowledge base**: `notebooklm`, with the notebook's id, or `none`; `--notebook` alone
  means `notebooklm`. It is stored in the `[knowledge_base]` table of `topic.toml`, whose
  other keys stay while the kind stays, and shown on the Topic in `status`. A Topic without
  one behaves as `none`.
- **Sources**: the History records which Sources exist and what they are;
  `sources.jsonl` is the readable copy Lamplight writes, one line per Source. Edit Sources
  with `study source`, not by hand: a line added by hand is listed as `untracked` and is not
  a Source until `study source add` records it. A Source's id is a slug of its title plus a
  random suffix, such as `strang-linear-algebra.k3f9a2`. Each line holds only what is the
  same on every computer:

  ```json
  {"id":"strang-linear-algebra.k3f9a2","kind":"file","title":"Linear Algebra","file_name":"strang.pdf",
   "hash":"sha256:…","size_bytes":15,"notebooklm_id":"7b1e","notebooklm_notebook":"nb-42"}
  {"id":"notes-paper.p2x7q1","kind":"file","title":"Paper","topic_path":"notes/paper.pdf","hash":"sha256:…","size_bytes":9}
  {"id":"go-dev-blog-context.m4k8s3","kind":"url","title":"Go Concurrency Patterns: Context","url":"https://go.dev/blog/context"}
  ```

  A file inside the Topic is kept by its `topic_path`, so every clone has it; a file
  outside, by its original `file_name`, `hash` and `size_bytes`. A `notebooklm_id` belongs to
  the `notebooklm_notebook` it was recorded for; after the Topic moves to another notebook,
  `source list` marks it `notebooklm_stale`. URLs are normalised: the scheme and host are
  lowercased, a default port is dropped, an international host stays readable (Unicode,
  NFC), and addresses with a user name or password are refused.
- **Where files are** differs from one computer to the next, so it is local state, never
  synced: `.lamplight/sources/<topic>.json` in the Study home.

  ```json
  {"format": 1, "files": {"strang-linear-algebra.k3f9a2": {"path": "/home/ada/Books/strang.pdf",
   "size_bytes": 15, "mtime": "2026-10-01T09:30:00Z"}}}
  ```

  `source list` finds each file Source inside the Topic, where it was last found, or in the
  Library by its content (rebuild the Library with `study library build` after reorganising
  your books), and updates this file as it goes, recording nothing in the History. A file is
  hashed again only when its size or modification time changed. Each file Source gets a
  `state`: `ok`, `changed` (the file where it was last found has other content now, and the
  recorded content is nowhere in the Library) or `missing` (say where it is with
  `source update --path`). Files are opened without blocking and must be regular files, so a
  FIFO or a device is refused, and hashing stops when the command is cancelled.
- **Evidence** is an exact quote, cited by a Lesson, with an optional `location` and
  `location_from`: `source` (read in the Source itself, such as a printed page number),
  `knowledge_base` (a citation as the Knowledge base gave it; NotebookLM citations carry no
  page numbers), `learner`, or `estimate`. Evidence lives in the History only, and Evidence
  whose Source has not arrived from another machine yet is held until it does. Retracted
  Evidence no longer counts for its Lesson. Lessons without Evidence are marked, never
  blocked.

  ```json
  { "id": "k3f9a2b7qd", "lesson": "elimination", "source": "strang-linear-algebra.k3f9a2",
    "quote": "Elimination produces an upper triangular system.", "location": "p. 46",
    "location_from": "source", "recorded": "2026-10-01T09:30:00Z" }
  ```
- **Two machines**: Sources and Evidence added on two machines merge: each Source is its own
  item in the History, and `history.jsonl` and `sources.jsonl` both merge by union in git.
  One Source edited on both machines is flagged as a `conflict`, and the union merge may
  leave two lines for it in `sources.jsonl`; the History's version is used, and the next
  change to the Source leaves one line. Under the one-machine-at-a-time contract, adding the
  same file or URL, or the same Evidence, on both machines before syncing gives two of them;
  that is not flagged.

## study doctor

`study doctor --json` reports a list of Findings. (A Finding is not a Check: Checks belong to
Lessons.) Each has a `status` of `ok`, `warn` or `fail`, and a `fix` for anything not `ok`.
Only a failure makes the setup unhealthy; then `ok` is `false`, the error code is
`unhealthy`, the exit code is 1, and `data` is still present:

```json
{
  "ok": false,
  "data": {
    "study_home": "/home/ada/study",
    "healthy": false,
    "findings": [
      { "name": "git", "status": "fail", "message": "git is not installed, or not on PATH",
        "fix": "install git 2.28 or newer" },
      { "name": "topic:physics", "status": "warn",
        "message": "physics is not a git repository, so Checkpoints cannot be saved",
        "fix": "git -C /home/ada/study/physics init --initial-branch=main" }
    ]
  },
  "error": { "code": "unhealthy", "message": "1 finding failed: git" }
}
```

`study_home` is absent when it cannot be resolved. Finding names, in order: `config`,
`study_home`, `git`, `git_identity` (only when git works), and, when the Study home
resolves, `local_state`, `topics`, one `topic:<id>` per Topic with a problem, `staging` (a
warning, only when an interrupted import or Topic creation left folders in `.lamplight/tmp`
over an hour ago; the next `study import` removes them), and `library`; then `log`,
`completion` and `setup`. A Topic folder study cannot read is a failed `topic:<id>`; a
Topic whose `.git` is missing, or is a file or a symlink (which Checkpoints refuse), is a
warning.

`study doctor` writes nothing: it checks permissions instead of writing test files, and the
Log file is not created by checking it.

## Completions

`study completion install` writes completions for one shell, for your user only, and
records what it did in `$XDG_STATE_HOME/lamplight/completions.json` so `uninstall` can undo
exactly that:

- fish: `$XDG_CONFIG_HOME/fish/completions/study.fish`. No configuration changes.
- bash: `completions/study` in the first folder of `$BASH_COMPLETION_USER_DIR` (a
  `:`-separated list), else `$XDG_DATA_HOME/bash-completion/completions/study`. Loaded by the
  bash-completion package; no configuration changes.
- zsh: `_study` in a writable folder already on `$fpath`. `study` cannot read zsh's `fpath`,
  so it uses `--dir` if given, else the first existing, writable folder in an exported
  `FPATH`, Oh My Zsh's completion cache (`$ZSH_CACHE_DIR/completions`, when `$ZSH` is set),
  or Homebrew's `$HOMEBREW_PREFIX/share/zsh/site-functions`. When none works, `study`
  installs to `$XDG_DATA_HOME/lamplight/completions/_study` and adds one line to
  `${ZDOTDIR:-~}/.zshrc` that sources it, but only with `--yes` or after asking at a
  terminal. Without consent it stops with a `usage` error and writes nothing.

What `study` will and won't touch:

- It never replaces a completion file it did not write: `install` stops with
  `already_exists` unless you pass `--force`. When a package already provides completions
  for the shell, `install` reports it in `provided_by` and installs nothing, again unless
  `--force`.
- The record keeps a SHA-256 of each script. `uninstall` deletes a script only while it is
  unchanged, and lists changed ones under `kept`.
- A `.zshrc` that is a symlink (stow, chezmoi) is edited where it points, so the link stays.
  One that is read-only or has other hard links is never rewritten: `install` still installs
  the script and lists the line to add by hand under `manual`, and `uninstall` lists the line
  to remove.
- `uninstall` finds its line even after an editor changed line endings. A line you edited is
  left alone, reported as `not_removed` with the line to remove by hand, and kept in the
  record so a later `uninstall` checks again. `.zshrc` is replaced atomically; if another
  program keeps changing it meanwhile, `uninstall` stops with `busy`.
- Installing to a different place first undoes the previous install. A failed install takes
  back what it wrote.
- `uninstall` removes a `.zshrc` that `install` created, once it is empty again.
- A damaged record is `corrupt` (delete it and reinstall); a record from a newer `study` is
  `newer_format` and is never rewritten.

`uninstall` reports `removed`, `kept` and `already_gone` scripts, `rc_lines` with a
`status` of `removed`, `already_gone` or `not_removed`, and any `manual` steps.

Packages install the scripts from `study completion <shell>` system-wide instead.

## Setting up agents

`study setup` makes Lamplight available to Claude Code and Codex in one step (ADR-0008):

- It installs the `lamplight` skill, embedded in `study`, into `~/.agents/skills/lamplight`,
  where Codex reads skills, and links `~/.claude/skills/lamplight` to it for Claude Code
  (`$CLAUDE_CONFIG_DIR/skills/lamplight` when that is set).
- It registers the MCP server at user scope with each agent's own command:
  `claude mcp add --scope user lamplight -- <study> mcp` and
  `codex mcp add lamplight -- <study> mcp`. An agent whose command is not on `PATH` is
  skipped with a note; run `study setup` again once it is installed.
- `<study>` is an absolute path, because GUI editors do not inherit the shell's `PATH`. When
  the `study` on `PATH` is this same program, its `PATH` entry is used as written (Homebrew's
  `bin` link, `~/go/bin/study`, `/usr/bin/study`), because the real file behind it often
  lives in a versioned folder that the next upgrade removes. When the first `study` on
  `PATH` is a version manager's shim (a `shims` folder, as mise and asdf use), or the running
  binary sits in a version manager's `installs` folder next to one, agents run the shim, and
  `study_note` says so. Otherwise the running binary's path is used, and `study_note` says it
  may not survive an upgrade.
- A `study` in a temporary build folder (a `go-build` folder, as `go run` uses, or under
  `$TMPDIR`, `/tmp` when unset) is refused with `failed_precondition`, because it will soon
  be gone; `--force` registers it anyway.

What it will and won't touch:

- It never replaces or deletes a file or folder it did not create. A
  `~/.agents/skills/lamplight` that something else made (for example `npx skills add`) stops
  setup with `already_exists` before anything is written; the v1 skill at
  `~/.agents/skills/study` is never touched. An existing `~/.claude/skills/lamplight` that is
  not setup's link is left alone (`kept`).
- A server named `lamplight` that an agent already has, registered by hand, is left alone
  (`kept`), and `--remove` never removes it.
- It records what it does in `$XDG_STATE_HOME/lamplight/setup.json`: each skill file with
  the SHA-256 of what it wrote, the folders and the link it created with where each led
  (symlinks resolved), and each registration. It saves the record before each change (the
  folders it is about to create, a registration it is about to make) or right after it (each
  file written), and first proves it can write there, so a run that stops part-way, on a
  full disk or a failing agent, can still be undone exactly by `--remove`, and setup run
  again finishes the job. A lock (`setup.lock` next to the record) keeps setup, `--remove`
  and `study mcp`'s refresh from running at once; a setup that waits more than 30 seconds
  for it fails with `busy`.
- It acts only on what the record names, and only while it is still what setup made: a
  regular file with the content setup wrote, a real folder (not a symlink) that still leads
  where it led, a link with the target setup gave it, in the folder it made it in. Skill
  files are read and written inside the skill folder through `os.Root`, never through a
  symlinked folder. So a folder moved into a dotfiles repository and linked back, by hand or
  by a dotfiles manager, is the learner's: setup and `--remove` leave it and everything
  behind it alone (`kept`).
- Running setup again updates the skill files it wrote and nobody changed, restores ones
  that were deleted, keeps files changed by hand (listed under `kept`), and re-registers an
  agent whose registration still runs an older path of `study` (`re_registered`).
- `study mcp` refreshes the skill files setup wrote when it starts, so an upgrade reaches the
  skill without running setup again. It never touches a file changed by hand, and never
  restores one the learner deleted; `--check` reports those as missing, and `study setup`
  restores them.
- `--remove` unregisters each server while it is still the one setup registered, removes
  the link while it is still setup's, deletes skill files that are unchanged, and removes
  the folders it created once they are empty. `--agent` limits it to one agent; the skill
  folder stays while another agent still uses it.
- `--check` and `--dry-run` write nothing and run no agent command that changes anything
  (they may run `codex mcp get lamplight --json`, which only reads; Claude Code's
  registration is read from `~/.claude.json`, because `claude mcp get` may start the
  server). A dry run takes every decision the real run would, on a copy of the record, so
  it reports exactly what the real run will do. `study doctor` adds a `setup` Finding with
  the first problem `--check` finds.
- Agent commands run with stdin closed, in a process group of their own: a read
  (`codex mcp get`) is stopped after 10 seconds, a change (`mcp add`, `mcp remove`) after
  60, together with anything they started. Only Codex's stdout is parsed; output it cannot
  read is an error, never taken for a server registered by hand.
- A `setup.json` study cannot read is `corrupt`, and so is one that names anything setup
  never creates: a file outside the skill folder, another skill folder or link, a folder
  other than `~/.agents`, `~/.agents/skills` and Claude Code's folder and its `skills`, or
  a registration other than `lamplight` running an absolute `study mcp`. Nothing is changed;
  delete it, then run setup. One from a newer `study` is `newer_format` and is never
  rewritten.

`--json` data of setup and `--remove`: `study`, `study_note`, `skill` (`dir`, `status`,
`note`, `written`, `removed`, `kept`), `agents` (`agent`, `status`, `note`, and for Claude
Code `link` and `link_status`), `manual`, `dry_run`, `remove`, `note`. Statuses:
`installed`, `updated`, `current`, `registered`, `re_registered`, `kept`, `skipped`,
`plugin`, `linked`, `removed`, `already_gone`, `not_installed`, `failed`. In a dry run the
statuses say what the real run would do, and the text output says "would install", "would
link" and so on.

When setup or `--remove` fails part-way, for example because an agent's command failed,
the JSON envelope has `ok: false`, the error, and `data` with everything that was done
before and after it (the failing agent has status `failed`, its `note` says why), and the
text output prints the same before the error.

`--check --json` data is `study`, `up_to_date` and `findings` (as in `study doctor`). When
setup has something to do, `--check` exits 1 and, like `study doctor`, `ok` is `false`, the
error code is `unhealthy`, and `data` is still present:

```json
{
  "ok": false,
  "data": {
    "study": "/usr/bin/study",
    "up_to_date": false,
    "findings": [
      { "name": "setup:skill", "status": "ok", "message": "the skill is installed in /home/ada/.agents/skills/lamplight" },
      { "name": "setup:codex", "status": "warn", "message": "study is not registered with codex",
        "fix": "study setup --agent codex" }
    ]
  },
  "error": { "code": "unhealthy", "message": "study setup has something to do: setup:codex" }
}
```

### The Claude Code plugin

Instead of `study setup`, Claude Code can install Lamplight from the marketplace in the
Lamplight repository:

```bash
claude plugin marketplace add mordor-forge/lamplight
claude plugin install lamplight@lamplight
```

The marketplace's plugin uses a `command` source, `study claude-plugin-path`, which writes the
plugin from the installed `study` into `$XDG_CACHE_HOME/lamplight/claude-plugin/` and prints
its folder; Claude Code runs it at install and once per session, so the plugin always
matches `study`. The plugin holds the `lamplight` skill, the `study mcp` server by `study`'s
absolute path, and a session-start hook (`study claude-hook session-start`) that prints
`study status` when a session starts inside the Study home, and nothing anywhere else.
`study` must be on the `PATH` of the shell Claude Code runs the command in.

The plugin and `study setup` never both register for Claude Code: `study setup` leaves
Claude Code to an enabled plugin, and undoes what an earlier setup did for it;
`study claude-plugin-path` refuses (`failed_precondition`) while `study setup` provides
Lamplight to Claude Code, so installing the plugin then fails until you run
`study setup --remove --agent claude`.

Setup counts the plugin as enabled when `enabledPlugins` has `lamplight@lamplight` set to
`true` (a plugin of the same name from another marketplace does not count) in Claude Code's
user settings (`~/.claude/settings.json`, or under `$CLAUDE_CONFIG_DIR`), or in the settings
of the project setup runs in: the nearest folder above the working folder, short of `HOME`,
with a `.claude` folder, where `settings.local.json` overrides `settings.json`. A user-scope
registration reaches every project, so setup leaves Claude Code to the plugin when it is
enabled in either place. Setup cannot see other projects' settings, nor managed settings:
enable the plugin there and run `study setup --remove --agent claude` yourself.

The plugin's folder is named after a hash of its content, and is never replaced or
removed: another session, or another version of `study` during an upgrade, may have just
printed it for Claude Code to copy. Concurrent runs of the same `study` all print the same
folder. Folders of earlier versions stay in `$XDG_CACHE_HOME/lamplight/claude-plugin/`,
which can be deleted at any time; only temporary folders left by interrupted runs are
removed, after ten minutes.

### Other agents

Any agent that speaks MCP can run the server. Point it at `study mcp`, by `study`'s absolute
path when the agent is a GUI application:

```json
{
  "mcpServers": {
    "lamplight": { "command": "/usr/bin/study", "args": ["mcp"] }
  }
}
```

Install the skill for your user with `npx skills add -g mordor-forge/lamplight`, or copy
`skills/lamplight/` from the repository into the agent's skills folder. (The
`mordor-forge/lamplight` repository names here and in the plugin assume the repository
rename planned in issue #18.)

## The Log

The Log is application diagnostics, never the learner's activity. Records go to two places:

- JSON lines in `$XDG_STATE_HOME/lamplight/study.log` (default
  `~/.local/state/lamplight/study.log`), at the chosen level. The file is created on the
  first record.
- stderr, formatted for people. It shows warnings and errors, or the chosen level when one
  is chosen explicitly.

The level comes from `--log-level`, else `STUDY_LOG`, else `info`: one of `debug`, `info`,
`warn`, `error`, in any case. Anything else, including slog's offsets such as `error+8`, is
invalid: an invalid `--log-level` is a usage error; an invalid `STUDY_LOG` is ignored with a
warning. In `study mcp`, nothing but MCP messages is ever written to stdout,
and stderr shows only warnings and errors whatever the level.
