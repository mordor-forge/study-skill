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

## Scientific Domain Adaptation (sciagent-skills)

When a workspace has `sciagent_skills` configured (see `references/template-resolution.md`), the attached skill's **Key Parameters** table provides an additional axis for difficulty scaling beyond general code complexity.

### Parameter-Based Exercise Scaling

**Beginner:**
- Exercise instructions: "Use the default parameters shown in the lesson notes"
- Key Parameters table is included in the lesson notes with defaults highlighted
- Success criteria reference specific output shapes/sizes (from sciagent's Expected Outputs)
- Troubleshooting table excerpts included in Common Pitfalls section

**Intermediate:**
- Exercise instructions: "Tune parameter X to achieve Y" (e.g., "adjust clustering resolution to find 8-12 clusters")
- Key Parameters table included but defaults are NOT highlighted — user must reason about ranges
- Exercise requirements combine 2-3 pipeline steps from the sciagent workflow
- Success criteria are outcome-based, not parameter-based

**Advanced:**
- Exercise instructions: "Justify your parameter choices for this dataset"
- Key Parameters table is NOT included — user must look it up or remember from earlier lessons
- Exercise may reference the sciagent skill's Common Recipes: "implement the batch correction variant"
- Open-ended: "your pipeline should handle [edge case from Troubleshooting table]"

**Expert:**
- Exercise provides a dataset with characteristics that make default parameters fail
- No parameter guidance — user must diagnose why defaults produce poor results
- May require chaining multiple sciagent skills (e.g., alignment → quantification → DE analysis)
- Success criteria include quality metrics (e.g., "achieve silhouette score > 0.3")

### Review Verification with sciagent-skills

During the Review & Feedback phase (main loop step 3), when a sciagent skill is attached:

1. Read the relevant sciagent skill's Common Recipe for the exercise topic
2. Compare the user's implementation *structurally* against the recipe (not exact match — pattern match)
3. Check: correct function calls, parameter values within reasonable range, proper error handling for known failure modes (from Troubleshooting table)
4. Flag anti-patterns listed in the skill's Troubleshooting section

The sciagent recipe is a **private verification reference** — never show it to the user. Use it to inform feedback quality.

## Edge Cases

- **First lesson**: Always start at `beginner` unless user explicitly says otherwise at init.
- **User jumps from beginner to expert**: Respect the override. If they struggle, the automatic rules will suggest stepping back at next calibration.
- **Expert + stuck**: Don't bump down automatically. Instead suggest: "This is expert-level — want to step back to advanced for this concept and return to expert after?"
