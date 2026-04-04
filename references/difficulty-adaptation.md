# Difficulty Adaptation

Read this file when: generating a new lesson (to set appropriate depth), providing feedback on exercises, or running the periodic calibration check.

## Performance Metrics

Track these implicitly per lesson — no extra prompts to the user:

```json
{
  "lesson": 3,
  "metrics": {
    "review_rounds": 2,
    "hints_requested": 1,
    "fsrs_ratings": [3, 4],
    "questions_asked": 3,
    "time_to_complete_minutes": null
  }
}
```

- **review_rounds**: How many times feedback required revision before passing. 1 = nailed it first try.
- **hints_requested**: Count of times user said "I'm stuck" or "need help" during the exercise.
- **fsrs_ratings**: Self-rated recall quality when the lesson concept comes up in review.
- **questions_asked**: Count of clarifying questions during the lesson (not a negative signal — curiosity is good, but combined with hints it indicates difficulty).
- **time_to_complete_minutes**: Optional, only if the user set a time budget.

Update `lessons[].metrics` in `.study-config.json` as events occur during the session.

## Difficulty Levels

```
beginner → intermediate → advanced → expert
```

Current level is stored in `.study-config.json` → `difficulty`.

### How Each Level Affects Lesson Generation

**Beginner:**
- Longer concept explanations with analogies
- Small, focused exercises (one concept at a time)
- 2-3 reference examples in notes (clearly marked as reference, not to copy)
- Explicit success criteria with checkboxes
- More scaffolding in the practice directory (maybe a starter file with comments)

**Intermediate:**
- Balanced explanation/exercise ratio
- Compound requirements (combine 2-3 concepts per exercise)
- 1 reference example max, more concise
- Success criteria without hand-holding

**Advanced:**
- Minimal notes — assume prior concepts are solid
- Complex exercises that require independent research
- May reference source material directly: "implement the algorithm described in Chapter 7"
- Open-ended requirements: "build something that demonstrates X"

**Expert:**
- Challenge-style, no notes section
- Performance constraints: "solve in O(n log n)" or "handle 10k concurrent connections"
- Deliberately ambiguous requirements to test design judgment
- Hints are disabled — if stuck, the skill suggests going back to advanced

## Automatic Adjustment Rules

After each lesson is completed (status = `completed`), evaluate the last 2 completed lessons:

**Bump UP if all true for last 2 lessons:**
- hints_requested == 0
- review_rounds <= 1
- average fsrs_ratings >= 4

**Bump DOWN if any true for last 2 lessons:**
- hints_requested >= 3 in a single lesson
- review_rounds >= 3 in a single lesson
- average fsrs_ratings <= 2

**Otherwise: hold current level.**

Never adjust after a single lesson — wait for a pattern across 2.

## Calibration Check

Every 3-4 completed lessons, ask the user:

```
Based on your recent exercises, I'm pitching lessons at [intermediate] level.
Does that feel right?
  1. Too easy — challenge me more
  2. About right
  3. Too hard — slow down
```

User override is **sticky** — it holds until the next calibration check. The automatic adjustment rules still run but won't override a user choice until the next calibration.

Store the override in `.study-config.json`:
```json
{
  "difficulty": "intermediate",
  "difficulty_override": "advanced",
  "difficulty_override_at_lesson": 5,
  "next_calibration_at_lesson": 9
}
```

## Edge Cases

- **First lesson**: Always start at `beginner` unless user explicitly says otherwise at init.
- **User jumps from beginner to expert**: Respect the override. If they struggle, the automatic rules will suggest stepping back at next calibration.
- **Expert + stuck**: Don't bump down automatically. Instead suggest: "This is expert-level — want to step back to advanced for this concept and return to expert after?"
