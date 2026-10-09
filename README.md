# study

> **Lamplight v2 is in development** on the [`v2`](https://github.com/mordor-forge/study-skill/tree/v2) branch. See the [design doc](https://github.com/mordor-forge/study-skill/blob/v2/docs/design/lamplight-v2.md) and the [tracking issue](https://github.com/mordor-forge/study-skill/issues/37).
> `main` stays v1 until v2.0 ships. v1 is also preserved at the [`v1.0.0`](https://github.com/mordor-forge/study-skill/tree/v1.0.0) tag.

An Agent Skills-compatible study tutor for structured, interactive learning with spaced repetition.

The active agent teaches concepts through notes and guides you through exercises you implement yourself, with FSRS-based review scheduling, optional research integrations, PDF source material support, and restart-safe session state.

## Quick Start

1. Install the full skill folder for your agent:

   Claude Code:
   ```bash
   mkdir -p ~/.claude/skills
   cp -R study/ ~/.claude/skills/study/
   ```

   Codex:
   ```bash
   mkdir -p ~/.agents/skills
   cp -R study/ ~/.agents/skills/study/
   ```

   Hermes:
   ```bash
   mkdir -p ~/.hermes/skills
   cp -R study/ ~/.hermes/skills/study/
   ```

2. Build the FSRS scheduler:
   ```bash
   cd <installed-study-skill>/scripts/fsrs
   go build -o fsrs ./cmd/fsrs/
   ```

3. Start learning:
   ```text
   /study init "Go concurrency"
   /study start
   ```

   In Codex, explicitly mention the skill if needed:
   ```text
   $study study init "Go concurrency"
   ```

## Prerequisites

### Required

- **An Agent Skills-compatible client** such as Claude Code, Codex, Hermes, Cline, or OpenCode
- **Go 1.22+** — to build the FSRS spaced repetition binary
- **Python 3.11+** and **uv** — for the book catalog builder (only needed if you use `study catalog`)

### Recommended

These enhance the experience but aren't required:

| Plugin/Skill | What it enables | Install |
|---|---|---|
| [SciAgent-Skills](https://github.com/jaechang-hits/SciAgent-Skills) | Curated bioinformatics and life science skills — domain-aware lessons, parameter tables, troubleshooting, exercise verification for scientific topics | Install from that project's current agent-specific instructions |
| context7 | Live framework/library documentation in lessons | Install through your agent's MCP/plugin mechanism |
| Playwright | Browser preview for visual diagrams | Install through your agent's MCP/plugin mechanism |
| [visual-explainer](https://github.com/nicobailon/visual-explainer) | Self-contained HTML diagrams and visualizations for lesson content | Install from that project's current agent-specific instructions |

### Optional

| Plugin/MCP | What it enables |
|---|---|
| LSP plugins (gopls, pyright, etc.) | Real-time code validation during exercise review |
| calibre or pandoc | Ebook format conversion (epub/mobi → PDF for ingestion) |

#### NotebookLM is no longer supported

Earlier versions of this README recommended a community NotebookLM MCP server for source
material. That path is unsupported now, and Lamplight v2 removes it
([ADR-0011](https://github.com/mordor-forge/study-skill/blob/v2/docs/adr/0011-lamplight-does-not-use-notebooklm.md)).

NotebookLM has no official API for personal Google accounts. The community server reaches it
through undocumented endpoints, signed in with your browser's Google session cookies. That is
unofficial access, which Google neither documents nor supports, and it may not be allowed by
Google's terms. If it goes wrong, the account at risk is yours, so this project no longer
recommends installing it.

v1 still uses a NotebookLM MCP server when it finds one in your agent. To keep v1 away from
it, remove the server from your agent's configuration. If you installed the tool only for
this skill, uninstall it too (`uv tool uninstall notebooklm-mcp-cli`) and delete the Google
sign-in it saved, as that project's documentation describes. Without the server, v1 falls
back to searching extracted text, as [Source Material](#source-material) describes.

## Graceful Degradation

The skill adapts to what's available. Nothing crashes if a plugin is missing:

| Feature | With plugin | Without plugin |
|---|---|---|
| Scientific domains | Curated workflows, parameter tables, troubleshooting via SciAgent-Skills | Falls through to web search |
| Lesson research | Live docs via context7 | Model's built-in knowledge |
| Source material | Semantic search via a NotebookLM MCP server ([unsupported](#notebooklm-is-no-longer-supported)) | Grep over extracted text, or skipped |
| Concept diagrams | HTML via visual-explainer | ASCII diagrams in lesson notes |
| Code validation | LSP real-time checking | User runs tests manually |
| Book catalog | Fuzzy search across library | Manual `--source` path |

## Commands

```
study init <topic> [--template=<name>] [--source <path>]
    Initialize a study workspace for a topic.
    Auto-detects language template from topic.

study start
    Start or resume a learning session.
    Checks energy, presents due reviews, enters lesson loop.

study status
    Show progress, difficulty level, review queue.

study review
    Standalone review session for all due FSRS items.

study add-source <path>
    Add a PDF/ebook as source material.

study catalog build <library-path>
    Index a directory of books for topic matching.

study catalog search <query>
    Search the catalog for books matching a topic.

study break
    Save session state and exit cleanly.
```

## Agent Support

The skill is intentionally portable: `SKILL.md` contains the workflow, `references/`
contains deeper protocols, `templates/` contains workspace scaffolds, and `scripts/`
contains deterministic support tools.

| Client | Install/use |
|---|---|
| Claude Code | Copy to `~/.claude/skills/study`; use `/study ...` if exposed as a command |
| Codex CLI/IDE/App | Copy to `.agents/skills/study` or `~/.agents/skills/study`; invoke with `$study` or rely on implicit skill matching |
| Hermes Agent | Copy to `~/.hermes/skills/study`; run from the study workspace and resume via Hermes session flags plus `.study-config.json` |
| Cline | Copy to `.cline/skills/study` or `~/.cline/skills/study` |
| OpenCode | Use built-in Agent Skills support when available; older setups may use `opencode-agent-skills` |

For details, read `references/agent-adapters.md`.

## Stop And Resume

Stop/resume is workspace-driven, not agent-memory-driven. The authoritative state
lives in `.study-config.json`, `lessons/`, `practice/`, `notes/`, and
`.fsrs/cards.json`. Any agent can resume a workspace by reading
`references/workspace-lifecycle.md`, then following `session_state.phase`,
`pending_action`, and `context`.

This is especially important for Hermes and other agents with their own memory or
session resume systems: those systems help recover chat context, but the study
workspace remains the source of truth.

## Usage

Use the skill after installation:

```text
/study init "Quantum Mechanics"
/study start
/study status
/study review
```

For catalog-backed source discovery:

```bash
cd <installed-study-skill>/scripts/catalog
uv sync
uv run study-catalog build /path/to/books --output ~/.config/study/book-catalog.json
uv run study-catalog search "quantum mechanics"
```

## Learning Approaches

Choose at init:

- **Concept** (default) — focused lessons with standalone exercises
- **Project** — build toward a working end product, lesson by lesson
- **Challenge** — progressive difficulty, minimal notes, maximum practice

## Book Catalog

If you have a collection of PDF books or ebooks, build the catalog before
`study init` if you want automatic book suggestions. The skill looks for the
default catalog at:

```text
~/.config/study/book-catalog.json
```

Build it once, then rebuild whenever your library changes:

```bash
# Build the catalog (one-time)
cd <installed-study-skill>/scripts/catalog
uv sync
uv run study-catalog build /path/to/your/books/ --output ~/.config/study/book-catalog.json

# The skill auto-suggests relevant books when you init a workspace
/study init "Quantum Mechanics"
# → "Found 3 matching books in your library. Add as source?"
```

For a local library, replace the source path with the directory that contains
your books:

```bash
cd <installed-study-skill>/scripts/catalog
uv sync
uv run study-catalog build /path/to/your/books --output ~/.config/study/book-catalog.json
```

## Source Material

Add a textbook as source material and the skill queries it during lessons:

```
/study init "Operating Systems" --source ~/Books/tanenbaum-os.pdf
```

The skill extracts the text, saves it as searchable markdown files under `sources/`, and
searches those during lessons.

A NotebookLM MCP server, when one is installed in your agent, is used instead in v1. That
path is unsupported: see [NotebookLM is no longer supported](#notebooklm-is-no-longer-supported).

## SciAgent-Skills Integration

When the [SciAgent-Skills](https://github.com/jaechang-hits/SciAgent-Skills) plugin is installed (built by [Jaechang Hits](https://github.com/jaechang-hits)), the study skill becomes domain-aware across bioinformatics, cheminformatics, biostatistics, proteomics, drug discovery, scientific computing, and more.

**What it enables:**

- **Automatic domain detection** — `study init "scRNA-seq analysis"` detects the genomics domain and attaches the `scanpy-scrna-seq` skill. Multi-tool pipelines (e.g., STAR → featureCounts → DESeq2) attach multiple skills in execution order.
- **Curated lesson content** — lessons are enriched with validated workflows, key parameter tables, and common recipes from the matched skill instead of relying on generic web search.
- **Parameter-based difficulty scaling** — beginner exercises use default parameters, intermediate exercises require tuning, advanced exercises demand justification, and expert exercises give datasets where defaults deliberately fail.
- **Exercise verification** — during review, the skill cross-references your implementation against sciagent's Common Recipes as a private structural check (never shown to you).
- **Troubleshooting as hints** — when stuck, the skill checks the matched sciagent skill's troubleshooting table before spawning a research agent.

Install SciAgent-Skills from its current instructions. For Claude Code, the
plugin commands are:

```bash
/plugin marketplace add jaechang-hits/SciAgent-Skills
/plugin install sciagent-skills
```

The skill works without SciAgent-Skills installed — scientific topics fall back to web search.

## Spaced Repetition (FSRS)

Completed lessons become review cards tracked by the [FSRS-6 algorithm](https://github.com/open-spaced-repetition/free-spaced-repetition-scheduler). The system uses a hybrid review model:

- **Integrated** — due items surface automatically as a warm-up at session start (max 3, capped at 5 min)
- **Standalone** — `study review` for dedicated review sessions

No streak counters, no guilt for missed reviews. The algorithm silently reschedules.

## Difficulty Adaptation

The skill tracks exercise performance (hints requested, review rounds, recall ratings) and adjusts lesson depth across four levels:

```
beginner → intermediate → advanced → expert
```

Adjustments are automatic with periodic calibration checks where you can override.

## Templates

| Template | Use case |
|---|---|
| `go-idiomatic` | Learning Go — cmd/, internal/, Makefile |
| `go-flat` | Using Go for physics/math — flat main.go |
| `python` | Python topics or Python as tool |
| `typescript` | TypeScript/JS topics |
| `rust` | Rust or systems programming |
| `c` | C or low-level programming |
| `plain` | Non-code topics (theory, math, science) |

Custom templates: create at `~/.config/study/templates/<name>/`.

## ADHD-Aware Design

- **Energy gating** — asks battery level before suggesting tasks
- **Capped review warm-ups** — max 3 items, no marathon review sessions
- **Detailed break state** — captures exactly where you stopped for seamless resumption
- **No guilt mechanisms** — no streaks, no "you missed N days", no judgment
- **Time budget awareness** — optional session length, paces lessons accordingly

## Project Structure

```
study/
├── README.md
├── SKILL.md                    # Core orchestrator (~345 lines)
├── references/
│   ├── fsrs-spaced-repetition.md
│   ├── research-agents.md
│   ├── difficulty-adaptation.md
│   ├── template-resolution.md
│   └── visual-libraries.md
├── templates/
│   ├── go-idiomatic/
│   ├── go-flat/
│   ├── python/
│   ├── typescript/
│   ├── rust/
│   ├── c/
│   └── plain/
└── scripts/
    ├── fsrs/                   # Go — FSRS-6 scheduler
    └── catalog/                # Python — book catalog builder
```

## Development

Run checks from the repository root:

```bash
make setup
make test
make lint
make typecheck
make coverage
```

The Python catalog is a nested uv project with its lockfile at
`scripts/catalog/uv.lock`. The Go FSRS scheduler is a nested Go module at
`scripts/fsrs`.

The deterministic lifecycle smoke test creates a temporary study workspace,
writes a lesson, adds an FSRS card, records a break state, and verifies another
agent could resume from `.study-config.json`:

```bash
scripts/e2e/study-lifecycle-smoke.sh
```

## License

MIT
