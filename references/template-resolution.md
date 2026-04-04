# Template Resolution

Read this file when: handling `study init` to determine which template to apply for the workspace.

## Resolution Order

1. **Explicit flag**: `--template=go-idiomatic` → use that template directly
2. **Auto-detect from topic**: parse the topic string for language/framework cues
3. **Ask user**: if ambiguous, present options
4. **Default**: `plain` template

## Auto-Detection Logic

### Step 1: Identify if a programming language is mentioned

Check the topic string (case-insensitive) against known languages:

| Pattern | Language |
|---|---|
| `go`, `golang` | Go |
| `python`, `py` | Python |
| `typescript`, `ts` | TypeScript |
| `rust` | Rust |
| `c programming`, `c language`, `learn c` | C |

Avoid false positives: "go" in "going further" is not Go. Look for whole word matches or language-specific qualifiers (e.g., "go concurrency", "in Go", "golang").

### Step 2: Determine template mode

If a language is detected, decide between code-as-subject and code-as-tool:

**Code-as-subject** (idiomatic template): The topic IS about learning the language itself.
- "Learn Go", "Go concurrency patterns", "Rust ownership"
- "TypeScript generics", "Python decorators"

**Code-as-tool** (flat template): The topic uses the language as a means to explore something else.
- "Physics simulations in Go", "Linear algebra with Python"
- "Data structures (implement in Rust)"

Heuristic: if the topic starts with the language name or the language is the primary noun, it's code-as-subject. If the language appears after "in", "with", "using", or is parenthetical, it's code-as-tool.

### Step 3: Select template

| Language | Code-as-subject | Code-as-tool |
|---|---|---|
| Go | `go-idiomatic` | `go-flat` |
| Python | `python` | `python` (same — Python structure is already flat) |
| TypeScript | `typescript` | `typescript` (same) |
| Rust | `rust` | `rust` (same — Cargo.toml is always needed) |
| C | `c` | `c` (same) |
| None detected | — | `plain` |

Only Go has separate idiomatic/flat templates because its project structure conventions (cmd/, internal/) are substantial enough to affect learning.

## Template Path Resolution

Templates are searched in this order:

1. **User custom**: `~/.config/study/templates/<name>/`
2. **Skill built-in**: `<skill-directory>/templates/<name>/`

The skill's own directory can be found relative to the SKILL.md file. Use the Bash tool to resolve:
```bash
SKILL_DIR=$(dirname "$(find ~/.claude/skills/study -name SKILL.md 2>/dev/null | head -1)")
```

If a user-custom template exists with the same name as a built-in, the user-custom takes precedence. This lets users override templates without modifying the skill.

## Applying a Template

When `study init` creates the workspace:

1. Copy all files from the selected template into the workspace root
2. Run `git init` and commit the template files
3. Create the standard study directories: `lessons/`, `practice/`, `notes/`, `.fsrs/`
4. Create `.study-config.json` with `template` and `template_mode` fields

Do NOT run `npm install`, `go mod tidy`, `cargo build`, or any dependency installation during init. The user may not have the toolchain installed yet — let them handle it.

## Shipped Templates

| Name | Mode | Contents | Best for |
|---|---|---|---|
| `go-idiomatic` | code-as-subject | cmd/, internal/, go.mod, Makefile | Learning Go itself |
| `go-flat` | code-as-tool | main.go, go.mod | Physics/math via Go |
| `python` | both | src/, requirements.txt | Python topics or Python as tool |
| `typescript` | both | src/, package.json, tsconfig.json | TypeScript or JS topics |
| `rust` | both | src/main.rs, Cargo.toml | Rust or systems topics |
| `c` | both | src/main.c, Makefile, .clang-format | C or low-level topics |
| `plain` | n/a | README.md | Non-code topics (theory, concepts) |

## Creating Custom Templates

Users can create templates at `~/.config/study/templates/<name>/`:

1. Create a directory with the template name
2. Add whatever scaffold files are needed
3. Use it: `study init "my topic" --template=<name>`

Templates are just directories of files — they're copied verbatim into new workspaces.
