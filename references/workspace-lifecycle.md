# Workspace Lifecycle

Read this file when: implementing `study init`, `study start`, `study break`,
`study status`, or resuming a session in any agent client.

## Source Of Truth

The study workspace is the durable state boundary. Agent session memory is
helpful but never authoritative.

Required workspace files:

```
.study-config.json
.fsrs/cards.json
lessons/
practice/
notes/
```

`session_state` in `.study-config.json` determines resume behavior:

```json
{
  "phase": "idle",
  "pending_action": null,
  "context": null,
  "energy": null,
  "time_budget_minutes": null
}
```

## Phases

| Phase | Meaning | Resume behavior |
|---|---|---|
| `idle` | No interrupted lesson session | Start a fresh session |
| `teaching` | Agent wrote or was writing lesson notes | Read current lesson and continue teaching |
| `practicing` | User is working on an exercise | Inspect `practice/lesson-NN/` and ask whether to continue or review |
| `reviewing` | Agent was reviewing user work | Read git diff and resume feedback |
| `review` | Dedicated FSRS review session | Continue due-card review |
| `break` | Session intentionally paused | Follow `pending_action` |

Do not set `phase` to `idle` in `study break`. That loses resume context.

## Init Contract

1. Create workspace directory and run `git init`.
2. Copy the selected template into the workspace.
3. Create `lessons/`, `practice/`, `notes/`, and `.fsrs/`.
4. Create `.fsrs/cards.json` as an empty FSRS store if it does not exist.
5. Write `.study-config.json`.
6. Commit the initial state with `[agent] init study workspace`.

## Start Contract

1. Read `.study-config.json` before doing anything else.
2. If `session_state.phase != "idle"`, resume instead of starting new work.
3. If fresh, increment `progress.session_count` and update
   `progress.last_session`.
4. Run the FSRS schedule check via `references/fsrs-spaced-repetition.md`.
5. Continue with review warm-up or lesson work based on energy and due cards.

## Lesson Completion Contract

When a lesson passes final review:

1. Add an FSRS card with id `lesson-NN`.
2. Mark the lesson as `completed` in `.study-config.json`.
3. Increment `progress.lessons_completed`.
4. Set `session_state.phase` to `idle` only if there is no pending work.
5. Commit with `[agent] complete lesson NN`.

## Break Contract

When the user asks to stop, pause, or take a break:

1. Commit any meaningful uncommitted changes.
2. Set `session_state.phase` to the current non-idle phase.
3. Set `session_state.pending_action` to a concrete next step.
4. Set `session_state.context` to enough information for another agent to
   resume without chat history.
5. Preserve `session_state.energy` and `time_budget_minutes` if known.
6. Write `notes/session-YYYY-MM-DD.md`.
7. Commit with `[session] end session N`.

Good `pending_action` examples:

- `review practice/lesson-01 implementation`
- `continue explaining Lesson 2 exercise requirements`
- `ask user for FSRS rating for lesson-03`

Bad examples:

- `continue`
- `do next`
- `remember what we were doing`

## Resume Contract

When resuming:

1. Read `.study-config.json`.
2. Read the current lesson file listed in `lessons[]` or inferred from
   `progress.current_lesson`.
3. Read the latest `notes/session-*.md` if present.
4. Inspect git status and relevant practice files.
5. Tell the user exactly what will resume:
   `Resuming: <pending_action> on Lesson N: <title>.`
6. Ask for current energy if the previous energy is stale or missing.

## Cross-Agent Rules

- Never depend on prior chat history for critical state.
- Never depend on a specific agent's memory layer for lesson progress.
- Keep all paths relative to the workspace except `<skill-dir>` tool paths.
- If a different agent resumes the session, it should not need to know which
  agent created it.
