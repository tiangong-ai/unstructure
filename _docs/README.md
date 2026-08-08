---
docType: index
scope: repo
status: current
authoritative: true
owner: unstructure
language: en
whenToUse: "When navigating unstructure repository documentation."
whenToUpdate: "When repository documentation layers, key docs, or governance routing change."
checkPaths:
  - AGENTS.md
  - .docpact/config.yaml
  - .github/workflows/docpact.yml
  - _docs/**
lastReviewedAt: 2026-08-09
lastReviewedCommit: 949c61064f2db2fdc1041265b7b983aa689938eb
---

# Unstructure Documentation

This directory contains the repo-local source documents governed by docpact.

## Layers

- Layer 0: `AGENTS.md` for mandatory agent entry guidance.
- Layer 1: `.docpact/config.yaml` for machine-readable governance.
- CI: `.github/workflows/docpact.yml` for config validation and PR
  documentation lint.
- Layer 2: current contracts, architecture, standards, and runbooks under
  `_docs/**`.

## Current Documents

- `_docs/contracts/repo-contract.md`: repository ownership, boundaries, and
  completion rules.
- `_docs/architecture/repo-architecture.md`: document processing topology.
- `_docs/runbooks/development.md`: setup, validation, and operation workflow.
- `_docs/standards/documentation-standards.md`: repo-local documentation rules.
