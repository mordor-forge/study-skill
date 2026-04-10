# study

A Claude Code skill for structured, interactive learning with spaced repetition.

Claude teaches concepts through notes and guides you through exercises you implement yourself — with FSRS-based review scheduling, research agent integration, PDF source material support, and ADHD-aware session management.

## Quick Start

1. Install the skill into your Claude Code skills directory:
   ```bash
   cp -r study/ ~/.claude/skills/study/
   ```

2. Build the FSRS scheduler:
   ```bash
   cd ~/.claude/skills/study/scripts/fsrs && go build -o fsrs ./cmd/fsrs/
   ```

3. Start learning:
   ```
   /study init "Go concurrency"
   /study start
   ```

## Prerequisites

### Required

- **Claude Code** with Agent tool access
- **Go 1.22+** — to build the FSRS spaced repetition binary
- **Python 3.11+** and **uv** — for the book catalog builder (only needed if you use `study catalog`)

### Recommended

These enhance the experience but aren't required:

| Plugin/Skill | What it enables | Install |
|---|---|---|
| [SciAgent-Skills](https://github.com/jaechang-hits/SciAgent-Skills) | 197 curated bioinformatics & life science skills — domain-aware lessons, parameter tables, troubleshooting, exercise verification for scientific topics | `/plugin marketplace add jaechang-hits/SciAgent-Skills && /plugin install sciagent-skills` |
| context7 | Live framework/library documentation in lessons | `/install context7@claude-plugins-official` |
| Playwright | Browser preview for visual diagrams | `/install playwright@claude-plugins-official` |
| [visual-explainer](https://github.com/nicobailon/visual-explainer) | Self-contained HTML diagrams and visualizations for lesson content | Copy to `~/.claude/skills/visual-explainer/` |

### Optional

| Plugin/MCP | What it enables |
|---|---|
| [NotebookLM MCP](https://github.com/jacob-bd/notebooklm-mcp-cli) | PDF textbook ingestion + semantic querying via Google NotebookLM (see below) |
| LSP plugins (gopls, pyright, etc.) | Real-time code validation during exercise review |
| pdfkb-mcp or rag-cli | Local PDF RAG (alternative to NotebookLM) |
| calibre or pandoc | Ebook format conversion (epub/mobi → PDF for ingestion) |

#### NotebookLM MCP Setup

NotebookLM is **free for any Google account** — no paid subscription, API key, or Google Cloud project required. The free tier (100 notebooks, 50 sources/notebook, 50 queries/day) is more than enough for studying.

Built by [Jacob Ben-David](https://github.com/jacob-bd). To set up:
1. Install the MCP server: `uv tool install notebooklm-mcp-cli`
2. Authenticate: `nlm login` (opens browser for one-time Google sign-in)
3. Connect to Claude Code: `nlm setup add claude-code`

Google Workspace accounts (business/education) get NotebookLM Plus automatically with higher limits.

> **Note:** The NotebookLM MCP uses undocumented browser APIs (cookie-based auth), not an official Google API. It works reliably but could break if Google changes their internal endpoints. This is why the skill includes fallback strategies (local RAG, chunked text) for source material.

## Graceful Degradation

The skill adapts to what's available. Nothing crashes if a plugin is missing:

| Feature | With plugin | Without plugin |
|---|---|---|
| Scientific domains | Curated workflows, parameter tables, troubleshooting via SciAgent-Skills | Falls through to web search |
| Lesson research | Live docs via context7 | Claude's training knowledge |
| Source material | Semantic search via NotebookLM | Grep over extracted text, or skipped |
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

## Learning Approaches

Choose at init:

- **Concept** (default) — focused lessons with standalone exercises
- **Project** — build toward a working end product, lesson by lesson
- **Challenge** — progressive difficulty, minimal notes, maximum practice

## Book Catalog

If you have a collection of PDF books (textbooks, Springer open access, etc.):

```bash
# Build the catalog (one-time)
cd ~/.claude/skills/study/scripts/catalog
uv sync
uv run study-catalog build /path/to/your/books/

# The skill auto-suggests relevant books when you init a workspace
/study init "Quantum Mechanics"
# → "Found 3 matching books in your library. Add as source?"
```

## Source Material

Add a textbook as source material and the skill queries it during lessons:

```
/study init "Operating Systems" --source ~/Books/tanenbaum-os.pdf
```

Backend priority:
1. **NotebookLM** — creates a notebook, adds PDF, queries semantically
2. **Local RAG** — uses pdfkb-mcp or rag-cli for local indexing
3. **Chunked text** — extracts text, saves as searchable markdown files

## SciAgent-Skills Integration

When the [SciAgent-Skills](https://github.com/jaechang-hits/SciAgent-Skills) plugin is installed (built by [Jaechang Hits](https://github.com/jaechang-hits)), the study skill becomes domain-aware for 197 scientific topics across bioinformatics, cheminformatics, biostatistics, proteomics, drug discovery, scientific computing, and more.

**What it enables:**

- **Automatic domain detection** — `study init "scRNA-seq analysis"` detects the genomics domain and attaches the `scanpy-scrna-seq` skill. Multi-tool pipelines (e.g., STAR → featureCounts → DESeq2) attach multiple skills in execution order.
- **Curated lesson content** — lessons are enriched with validated workflows, key parameter tables, and common recipes from the matched skill instead of relying on generic web search.
- **Parameter-based difficulty scaling** — beginner exercises use default parameters, intermediate exercises require tuning, advanced exercises demand justification, and expert exercises give datasets where defaults deliberately fail.
- **Exercise verification** — during review, the skill cross-references your implementation against sciagent's Common Recipes as a private structural check (never shown to you).
- **Troubleshooting as hints** — when stuck, the skill checks the matched sciagent skill's troubleshooting table before spawning a research agent.

**Install:**

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

## License

MIT
