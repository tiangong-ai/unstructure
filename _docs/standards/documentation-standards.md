---
docType: standard
scope: repo
status: current
authoritative: true
owner: unstructure
language: en
whenToUse: "When creating, moving, or reviewing unstructure repository documentation."
whenToUpdate: "When documentation layers, metadata rules, or source-of-truth boundaries change."
checkPaths:
  - AGENTS.md
  - .docpact/config.yaml
  - .github/workflows/docpact.yml
  - _docs/**
lastReviewedAt: 2026-09-19
lastReviewedCommit: 07f65ef100300d7cb44996476af8a033a9ae8bbc
---

# Unstructure Documentation Standards

## Layers

- `AGENTS.md`: mandatory repo entry guidance for agents.
- `.docpact/config.yaml`: machine-readable governance, routing, coverage, and
  document inventory.
- `.github/workflows/docpact.yml`: CI enforcement for config validation and PR
  documentation lint.
- `_docs/contracts/**`: current constraints and ownership rules.
- `_docs/architecture/**`: current processing topology and integration facts.
- `_docs/runbooks/**`: executable procedures.
- `_docs/standards/**`: repo-local documentation and engineering standards.

## Rules

- Keep deterministic governance facts in `.docpact/config.yaml`.
- Keep explanatory processing, dependency, and workflow details in `_docs/**`.
- Update docs when source domains, processing stages, output locations, index
  targets, native dependencies, or long-running job commands change.
- Do not document secrets, local data paths containing private data, or
  generated output inventories as governed source facts.
- Do not duplicate root workspace branch policy or submodule integration policy
  in this repository.
