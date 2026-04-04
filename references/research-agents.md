# Research Agent Composition

Read this file when: creating a new lesson (Tier 1 proactive research) or when the user asks for help, deeper context, or is stuck (Tier 2 on-demand research).

## Tier 1: Proactive Research (Lesson Creation)

When writing a new lesson, spawn research agents in the background to ensure content is current and authoritative. The lesson outline can be written immediately — enrich it when research completes.

### Dispatch Protocol

Detect the topic category and spawn the appropriate agent:

**Programming language or framework:**
```
Agent(subagent_type="general-purpose", run_in_background=true, prompt="
  Use the context7 plugin to research [specific concept].
  1. Resolve the library ID for [language/framework]
  2. Query docs for [concept being taught]
  3. Return: current API signatures, best practices, common patterns, gotchas
  4. Note any recent changes or deprecations
")
```

**Topic covered by NotebookLM notebooks:**
```
Agent(subagent_type="nlm-researcher", run_in_background=true, prompt="
  Query NotebookLM for [concept].
  Mode: answer
  Look in notebooks tagged with [relevant category].
  Return: authoritative content with source citations.
")
```

**General or emerging topic:**
```
Agent(subagent_type="general-purpose", run_in_background=true, prompt="
  Research [concept] using WebSearch and WebFetch.
  Find: current best practices, authoritative references, recent developments.
  Return: summary with source URLs.
")
```

### Incorporating Research

When research completes, weave findings into the lesson notes:
- Add source attribution: "According to the official Go docs..."
- Use researched examples as reference material (not as exercise solutions)
- Flag any discrepancies between Claude's training knowledge and current docs

## Tier 2: On-Demand Research (During Exercises)

Triggered when the user says "I'm stuck", "go deeper", "I need more context", or asks a specific technical question.

### Dispatch Rules

- **API syntax question** → context7 (fast, specific, returns exact signatures)
- **Conceptual depth** → NLM researcher or web research (broader context)
- **"How does X work in practice?"** → web research (articles, blog posts, examples)
- **Source material question** → NotebookLM query against the workspace's source PDF

Spawn a **single** targeted agent, not all three. Match the question to the right tool.

### Presenting Results

- Don't dump raw research output at the user
- Synthesize findings into a conversational explanation
- Point to specific docs/sections for further reading
- Don't give away exercise solutions — guide understanding

## Source Material Queries

If the workspace has a PDF source configured (`.study-config.json` → `sources[]`):

**NotebookLM backend:**
```
Agent(subagent_type="nlm-researcher", prompt="
  Query notebook [notebook_id] about [concept].
  Use conversation_id [prev_id] for follow-up context if available.
  Return cited answer.
")
```

This enables Socratic drilling — multi-turn Q&A against the textbook.

**Chunked fallback (no NLM):**
Use Grep to search `sources/` directory for relevant passages, then synthesize.

## Graceful Degradation

The skill detects available tools at runtime. If a tool is missing, it falls back:

| Integration | If available | If missing | Detection |
|---|---|---|---|
| context7 plugin | Live framework/library docs | Claude's training knowledge | Check if `mcp__plugin_context7_context7__resolve-library-id` tool exists |
| NotebookLM MCP | Deep research from notebooks | Skipped, no impact | Check if `mcp__notebooklm-mcp__notebook_query` tool exists |
| WebSearch | Current articles and examples | Training knowledge only | Check if `WebSearch` tool exists |
| visual-explainer skill | HTML diagrams for concepts | ASCII diagrams in notes | Check if visual-explainer is in available skills |
| LSP plugins | Real-time code validation | User tests manually | Check for language-specific LSP tool |

Never fail loudly if a tool is missing. Silently use the best available option.
