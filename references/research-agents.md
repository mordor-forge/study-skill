# Research Agent Composition

Read this file when: creating a new lesson (Tier 1 proactive research) or when the user asks for help, deeper context, or is stuck (Tier 2 on-demand research).

## Tier 1: Proactive Research (Lesson Creation)

When writing a new lesson, use background research agents if the current client
supports them. Otherwise, do the research inline before finalizing the lesson.
The lesson outline can be written immediately; enrich it when research completes.

### Dispatch Protocol

Detect the topic category and use the appropriate research path:

**Programming language or framework:**

Claude Code-style background task example:
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

Claude Code-style background task example:
```
Agent(subagent_type="nlm-researcher", run_in_background=true, prompt="
  Query NotebookLM for [concept].
  Mode: answer
  Look in notebooks tagged with [relevant category].
  Return: authoritative content with source citations.
")
```

**Scientific domain (covered by sciagent-skills plugin):**

When the topic falls within a scientific domain — bioinformatics, cheminformatics, biostatistics,
proteomics, genomics, drug discovery, scientific computing, cell biology, lab automation, or
scientific writing — check whether a matching sciagent-skills skill exists. These provide curated
workflows, parameter tables, and validated code that are far more reliable than generic web results.

Detection: check the topic against sciagent domain keywords:

| Domain | Keywords (any match) |
|---|---|
| Genomics / bioinformatics | RNA-seq, ChIP-seq, GWAS, single-cell, scRNA-seq, variant calling, differential expression, genome assembly, sequence alignment, FASTQ, BAM, VCF, BED, FASTA, gene expression, phylogenetics |
| Structural biology / drug discovery | molecular docking, SMILES, cheminformatics, virtual screening, drug-likeness, protein structure, fingerprint, pharmacogenomics, ADMET, binding affinity |
| Biostatistics | survival analysis, Bayesian modeling, statistical test, regression, mixed effects, hypothesis testing, power analysis, effect size, p-value |
| Cell biology / imaging | microscopy, flow cytometry, cell segmentation, histopathology, whole-slide image, DICOM, fluorescence |
| Proteomics | mass spectrometry, protein identification, Western blot, LC-MS, proteomics pipeline |
| Scientific computing | network analysis, graph neural network, optimization, simulation, dimensionality reduction, time series, geospatial |
| Lab automation | liquid handling, Opentrons, protocol, plate reader, LIMS |
| Scientific writing | manuscript, peer review, figure preparation, citation, journal submission |

If a keyword match is found, resolve the best-matching sciagent skill name(s).
Claude Code-style background task example:

```
Agent(subagent_type="general-purpose", run_in_background=true, prompt="
  Invoke the sciagent-skills skill: /sciagent-skills:[skill-name]
  Extract and return these sections:
  1. Workflow (step-by-step pipeline with code blocks)
  2. Key Parameters table (parameter, default, range, effect)
  3. Common Recipes (self-contained code snippets for variants)
  4. Troubleshooting table (problem, cause, solution)
  5. When to Use section (for routing to alternative tools)
  Do NOT return the full skill verbatim — summarize into lesson-appropriate content.
  Note which sections had the most relevant material for the concept being taught.
")
```

If the topic spans multiple skills (e.g., an RNA-seq pipeline touching STAR, featureCounts, and DESeq2), resolve all matching skills and note the pipeline ordering. This informs multi-lesson arc planning.

If no sciagent skill matches or the plugin is not installed, fall through to general web search.

**General or emerging topic:**

Claude Code-style background task example:
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
- Flag any discrepancies between the model's built-in knowledge and current docs

## Tier 2: On-Demand Research (During Exercises)

Triggered when the user says "I'm stuck", "go deeper", "I need more context", or asks a specific technical question.

### Dispatch Rules

- **API syntax question** → context7 (fast, specific, returns exact signatures)
- **Scientific tool/pipeline question** → sciagent-skills (curated workflows, parameter guidance, troubleshooting). Check the workspace config for `sciagent_skills` — if a skill is already attached, query its Troubleshooting and Key Parameters sections first before spawning any agent. This is the fastest path for scientific domains.
- **Conceptual depth** → NLM researcher or web research (broader context)
- **"How does X work in practice?"** → web research (articles, blog posts, examples)
- **Source material question** → NotebookLM query against the workspace's source PDF

Use a **single** targeted research path, not all three. Match the question to the right tool.

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
| context7 plugin | Live framework/library docs | Model's built-in knowledge | Check if `mcp__plugin_context7_context7__resolve-library-id` tool exists |
| NotebookLM MCP | Deep research from notebooks | Skipped, no impact | Check if `mcp__notebooklm-mcp__notebook_query` tool exists |
| sciagent-skills plugin | Curated scientific workflows, parameter tables, troubleshooting | Falls through to web search for scientific topics | Check if `sciagent-skills:*` skills appear in available skills list |
| WebSearch | Current articles and examples | Training knowledge only | Check if `WebSearch` tool exists |
| visual-explainer skill | HTML diagrams for concepts | ASCII diagrams in notes | Check if visual-explainer is in available skills |
| LSP plugins | Real-time code validation | User tests manually | Check for language-specific LSP tool |

Never fail loudly if a tool is missing. Silently use the best available option.
