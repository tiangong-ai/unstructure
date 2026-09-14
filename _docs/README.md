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
lastReviewedAt: 2026-09-14
lastReviewedCommit: 58f7612bb02891f9db5c153f0cc324f1d3f95f9b
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
