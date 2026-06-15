#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

WORKSPACE="$WORKDIR/go-concurrency"
FSRS_BIN="$ROOT/scripts/fsrs/fsrs"

if [[ ! -x "$FSRS_BIN" ]]; then
  (cd "$ROOT/scripts/fsrs" && go build -o fsrs ./cmd/fsrs/)
fi

mkdir -p "$WORKSPACE"
cp -R "$ROOT/templates/go-idiomatic/." "$WORKSPACE/"
mkdir -p "$WORKSPACE/lessons" "$WORKSPACE/practice/lesson-01" "$WORKSPACE/notes" "$WORKSPACE/.fsrs"

cd "$WORKSPACE"
git init -q
git config user.email "study-smoke@example.invalid"
git config user.name "Study Smoke Test"

cat > .study-config.json <<'JSON'
{
  "version": 3,
  "topic": "Go Concurrency",
  "template": "go-idiomatic",
  "template_mode": "code-as-subject",
  "approach": "concept",
  "end_goal": null,
  "difficulty": "beginner",
  "difficulty_override": null,
  "next_calibration_at_lesson": 4,
  "mode": "tutorial",
  "created": "2026-06-14T00:00:00Z",
  "sciagent_skills": [],
  "sciagent_primary": null,
  "progress": {
    "current_lesson": 1,
    "lessons_completed": 0,
    "session_count": 1,
    "last_session": "2026-06-14T00:00:00Z"
  },
  "lessons": [
    {
      "num": 1,
      "title": "Goroutines",
      "file": "lessons/01-goroutines.md",
      "status": "in_progress"
    }
  ],
  "session_state": {
    "phase": "teaching",
    "pending_action": "finish Lesson 1 exercise prompt",
    "context": "Lesson 1 introduces goroutines and asks the learner to write a small concurrent program.",
    "energy": "half",
    "time_budget_minutes": 25
  },
  "sources": [],
  "review": {
    "fsrs_data_path": ".fsrs/cards.json",
    "items_due": 0,
    "last_review": null
  },
  "catalog_path": "~/.config/study/book-catalog.json"
}
JSON

cat > lessons/01-goroutines.md <<'MD'
# Lesson 1: Goroutines

## Concept
Goroutines let Go run functions concurrently.

## Exercise
Create your implementation in `practice/lesson-01/`.
MD

git add .
git commit -q -m "[agent] init study workspace"

FSRS_STORE=.fsrs/cards.json "$FSRS_BIN" add "lesson-01" "Goroutines" 1 >/tmp/study-smoke-card.json
FSRS_STORE=.fsrs/cards.json "$FSRS_BIN" schedule >/tmp/study-smoke-schedule.json

python - <<'PY'
import json
from pathlib import Path

config_path = Path(".study-config.json")
config = json.loads(config_path.read_text())
config["session_state"] = {
    "phase": "practicing",
    "pending_action": "review practice/lesson-01 implementation",
    "context": "User was about to implement the goroutine exercise.",
    "energy": "half",
    "time_budget_minutes": 25,
}
Path("notes/session-2026-06-14.md").write_text(
    "# Session 1\n\nPaused during Lesson 1 practice. Next: review practice/lesson-01 implementation.\n"
)
config_path.write_text(json.dumps(config, indent=2) + "\n")
PY

git add .
git commit -q -m "[session] end session 1"

python - <<'PY'
import json
from pathlib import Path

config = json.loads(Path(".study-config.json").read_text())
assert config["session_state"]["phase"] == "practicing"
assert config["session_state"]["pending_action"] == "review practice/lesson-01 implementation"
assert Path("lessons/01-goroutines.md").is_file()
assert Path("notes/session-2026-06-14.md").is_file()

cards_data = json.loads(Path(".fsrs/cards.json").read_text())
cards = cards_data["cards"] if isinstance(cards_data, dict) else cards_data
assert any(card["id"] == "lesson-01" for card in cards)
PY

echo "study lifecycle smoke test passed: $WORKSPACE"
