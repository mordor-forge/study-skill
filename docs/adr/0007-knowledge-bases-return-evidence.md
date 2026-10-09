# 0007. Knowledge Bases Return Evidence Behind a Per-Topic Seam

## Status

Accepted. On the `v2` branch, ADR-0011 replaces its v2.0 part: Lamplight does not use
NotebookLM. This copy on `main` predates that.

## Context

v1 extracted PDF text with pdfplumber, which is fragile, or used NotebookLM through a
community MCP server. NotebookLM has no consumer API: the official Gemini Notebook API is
enterprise-only, and community tools use undocumented endpoints and browser cookies.
NotebookLM's citations carry no page numbers. Some Topics learn from web pages rather than
books. Retrieval quality comparable to NotebookLM needs layout-aware conversion (such as
Docling), hybrid keyword and embedding search, and reranking.

## Decision

A Topic has at most one Knowledge base. Searching it returns Evidence: an exact quote from a
Source, plus a location when one is known and where that location came from. The agent
writes answers from Evidence; the core never does, and the core never parses documents.

- **v2.0**: two kinds. `notebooklm`: the core records the notebook and each Source's
  NotebookLM ID, and the agent queries the NotebookLM MCP server directly. `none`: the agent
  reads Sources itself and records Evidence through the core. Sources are files or URLs. The
  NotebookLM login is checked when a Session opens, and Lessons without Evidence are marked,
  never blocked.
- **Later**: Knowledge base plugins are MCP servers that implement Lamplight's own fixed
  contract (add a Source, search for Evidence, list Sources). The first planned plugin is a
  generic local RAG backend.

Considered and rejected: separate executables called once per request (models such as
Docling's would reload on every call, with no progress reporting or cancellation for
hours-long ingestion); HashiCorp go-plugin (gRPC, Go-centric); NotebookLM as the only store
(unofficial, needs a Google account, cannot work offline).

## Consequences

NotebookLM is the recommended Knowledge base without being a core dependency. Plugins can be
written in any language, which matters because Docling is Python. Because plugins implement
our contract rather than their own tool names, the core talks to all of them the same way.
