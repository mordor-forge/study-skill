# 0003. Catalog Extension Normalization

## Status

Accepted

## Context

Book collections often contain mixed-case ebook extensions and separator-heavy
filenames such as `Classical.Mechanics.PDF`. The scanner should produce complete
catalogs on case-sensitive filesystems and readable titles for search.

## Decision

Discover candidate files by walking the tree once and comparing
`Path.suffix.lower()` against the supported format set. Normalize dots,
underscores, and hyphens in filename stems before title-casing.

## Consequences

The scanner handles common ebook library naming schemes consistently. Format
detection remains centralized around lowercase suffixes, and title search gets
cleaner word boundaries.
