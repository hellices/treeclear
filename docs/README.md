# Treeclear Documentation

The Markdown files committed to this repository are the canonical Treeclear
documentation. External or local wikis may index or render these files, but
they are supplemental and must link back to the repository source.

## Structure

- `architecture/`: accepted, durable system boundaries, invariants, and data
  flows.
- `design/`: feature or subsystem designs that are narrower than the product
  architecture.
- `specs/`: versioned externally observable contracts such as CLI behavior,
  plan schemas, adapter manifests, and configuration semantics.
- `plans/`: ordered implementation plans with exact source paths, tests, and
  completion gates.
- `adr/`: concise records of significant architecture decisions and their
  consequences.

Agent operating instructions belong in `AGENTS.md`. Tool-specific skills and
plugins belong in their standard configuration directories, such as
`.agents/skills/`, rather than under `docs/`.

## Current documents

- [Treeclear Architecture](architecture/2026-09-12-treeclear.md)
- [Implementation Plans](plans/README.md)
- [Development Harness Design](design/development-harness.md)
- [Development and Review Workflow](development.md)
