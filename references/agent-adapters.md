# Agent Adapters

Read this file when: installing or running the study skill outside Claude Code,
or when checking which optional tools are available in the current agent client.

## Portable Core

The portable unit is the skill folder:

```
study/
├── SKILL.md
├── references/
├── templates/
└── scripts/
```

All clients should preserve the folder layout. Do not copy only `SKILL.md`; the
FSRS scheduler, catalog tool, templates, and references are required for the full
workflow.

Every client must be able to:

1. Read and write files in the generated study workspace.
2. Run shell commands for git, the FSRS binary, and optional catalog tooling.
3. Ask the user short questions.
4. Persist `.study-config.json` exactly as described in
   `references/workspace-lifecycle.md`.

Subagents, MCP tools, browser tools, and live docs are optional accelerators. If
missing, continue with local files and normal web or model knowledge.

## Claude Code

Install as a Claude Code skill or plugin and invoke with `/study ...` if exposed
as a slash command. Claude Code-specific conveniences may include `Agent(...)`,
`AskUserQuestion`, `WebSearch`, and plugin slash commands.

Use this install shape for a local skill copy:

```bash
mkdir -p ~/.claude/skills
cp -R /path/to/study ~/.claude/skills/study
cd ~/.claude/skills/study/scripts/fsrs
go build -o fsrs ./cmd/fsrs/
```

## Codex CLI, IDE, And App

Codex supports Agent Skills in CLI, IDE extension, and app surfaces. For a
repo-scoped install, place this folder at `.agents/skills/study`. For a personal
install, place it at `~/.agents/skills/study`. Codex can invoke skills explicitly
with `$study` or choose them implicitly from the `description`.

Recommended prompt:

```text
$study study init "Go concurrency"
```

Codex-specific guidance:

- Keep repo conventions in `AGENTS.md`; keep study workflow in this skill.
- Use `references/agent-adapters.md` only for client setup, and
  `references/workspace-lifecycle.md` for state transitions.
- Resolve `<skill-dir>` from the loaded skill path, not from `~/.claude`.
- For `study init`, ask learning-approach and catalog-source questions as plain
  text and wait for the user's reply. Codex CLI does not need a special
  `AskUserQuestion` tool for these checkpoints.

## Hermes Agent

Hermes ships skills under `~/.hermes/skills/` and has first-class skill and
resume-oriented CLI behavior. Install this skill as:

```bash
mkdir -p ~/.hermes/skills
cp -R /path/to/study ~/.hermes/skills/study
cd ~/.hermes/skills/study/scripts/fsrs
go build -o fsrs ./cmd/fsrs/
```

Hermes-specific guidance:

- Hermes sessions can be resumed with its own session flags, but study resume
  must still start by reading the workspace `.study-config.json`.
- For scheduled or messaging-gateway use, ensure the job `workdir` is the study
  workspace so `AGENTS.md`/workspace files and `.study-config.json` are visible.
- Do not rely on Hermes memory for study state. Memory can help personalize
  teaching, but `.study-config.json`, `lessons/`, `practice/`, `notes/`, and
  `.fsrs/cards.json` are the source of truth.

## Cline

Cline skills live in `.cline/skills/` for a workspace or `~/.cline/skills/` for
global use. Copy the full folder and enable Skills in Cline settings.

```bash
mkdir -p ~/.cline/skills
cp -R /path/to/study ~/.cline/skills/study
```

Use `/study` if Cline exposes the skill as a slash command, or ask Cline to use
the `study` skill by name.

## OpenCode

OpenCode has first-party Agent Skills support in current releases. Copy the
skill folder into the skill location supported by your OpenCode installation or
use its native skill discovery. Older OpenCode setups may use the maintained
`opencode-agent-skills` plugin, but prefer built-in skill support when available.

## Generic Agent Skills Clients

If a client supports the open Agent Skills standard, install the complete folder
where that client discovers skills. If it does not support skills directly, add
an instruction in the client's project guidance:

```text
When the user asks for study workflows, read /path/to/study/SKILL.md and follow
its referenced files. Treat /path/to/study as <skill-dir>.
```

## Optional Tool Mapping

| Capability | Claude Code | Codex | Hermes | Generic fallback |
|---|---|---|---|---|
| Skill invocation | `/study` or skill trigger | `$study` or implicit skill trigger | skill command or loaded skill | Ask agent to read `SKILL.md` |
| Durable repo guidance | `CLAUDE.md`/plugin docs | `AGENTS.md` | `AGENTS.md`, `HERMES.md`, memory | Project instructions |
| Subagents/background research | `Agent(...)` | subagents when available | Hermes subagents/skills | Do research inline |
| Live library docs | context7 MCP/plugin | MCP if configured | MCP/tool gateway if configured | official docs/web |
| Source notebooks | NotebookLM MCP | MCP if configured | MCP/tool gateway if configured | local `sources/` grep |
| Browser checks | Playwright plugin/MCP | browser or Playwright MCP if configured | browser/tool gateway | skip or manual |

Never make optional integrations required for `study init`, `study start`,
`study break`, or `study review`.
