# FSRS Spaced Repetition System

Read this file when: creating review items for completed lessons, presenting review warm-ups at session start, or handling the `study review` command.

## How FSRS Works (Brief)

FSRS tracks three values per concept (card):

- **Difficulty (D)**: How hard this concept is to remember [1-10]
- **Stability (S)**: Days until recall probability drops to 90%
- **Retrievability (R)**: Current probability of successful recall [0-1]

After each review, the user rates their recall: 1=Again (forgot), 2=Hard, 3=Good, 4=Easy. The algorithm updates D, S, R and schedules the next review.

Higher stability = longer intervals between reviews. Difficulty affects how quickly stability grows. The system adapts to each concept's actual difficulty based on the user's performance.

## Invoking the Go Binary

The FSRS scheduler is a Go binary at `scripts/fsrs/cmd/fsrs/`. All commands output JSON to stdout.

Set the card store path via environment variable:
```bash
FSRS_STORE=.fsrs/cards.json
```

### Commands

**Add a card when a lesson is completed:**
```bash
FSRS_STORE=.fsrs/cards.json scripts/fsrs/fsrs add "lesson-03" "Goroutine Channels" 3
```
Output: the new card as JSON.

**Check what's due for review:**
```bash
FSRS_STORE=.fsrs/cards.json scripts/fsrs/fsrs schedule
```
Output:
```json
{
  "due_count": 2,
  "cards": [
    {"id": "lesson-01", "topic": "Goroutines", "lesson_num": 1, "due": "2026-04-04T...", "state": 2, "difficulty": 5.5, "stability": 12.3, "reviews": 4, "lapses": 0, "elapsed_days": 3}
  ]
}
```

**Record a review result:**
```bash
FSRS_STORE=.fsrs/cards.json scripts/fsrs/fsrs review "lesson-01" 3
```
Rating scale: 1=Again, 2=Hard, 3=Good, 4=Easy. Output: updated card with new due date.

**Get overview of all cards:**
```bash
FSRS_STORE=.fsrs/cards.json scripts/fsrs/fsrs status
```

## Hybrid Review Model

### Integrated Review (at session start)

1. Run `fsrs schedule` to check for due items
2. If items are due:
   - Present **maximum 3 items** as a warm-up (cap at ~5 minutes)
   - For each item, present a recall prompt based on the lesson topic
   - Ask the user to recall the concept (don't show the answer yet)
   - After they respond, show the correct answer from the lesson notes
   - Ask: "How was that? 1=Forgot completely, 2=Hard to recall, 3=Remembered, 4=Easy"
   - Run `fsrs review <id> <rating>` to record
3. If nothing is due, skip straight to the lesson

### Standalone Review (`study review`)

1. Run `fsrs schedule` to get all due items (no cap)
2. Present items one at a time using the same recall → rate flow
3. After all items reviewed, show summary: how many reviewed, average rating
4. If no items due: "Nothing due for review right now. Your next review is in N days."

### Review Prompt Format

For each card, create a recall prompt from the lesson notes:

```
Review: Lesson 3 — Goroutine Channels

Can you explain: [key concept from lesson]
- What is it?
- When would you use it?
- What's a common pitfall?

Take a moment to think, then tell me what you remember.
```

After the user responds, show the reference answer from `lessons/03-*.md` and ask for their self-rating.

### No Guilt Rules

- Never show streak counts or "days since last review"
- Never guilt the user for missed reviews ("you have 15 overdue items!")
- If items are overdue, just present them normally — the algorithm handles the scheduling
- If the user skips review at session start, silently move on
- Skipped items stay in the queue for next time
