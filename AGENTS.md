---
docType: agent-contract
scope: repo
status: current
authoritative: true
owner: unstructure
language: en
whenToUse: "Before editing the unstructure repository."
whenToUpdate: "When repo entry points, workflow commands, docpact config, document processing domains, or deployment boundaries change."
checkPaths:
  - AGENTS.md
  - .docpact/config.yaml
  - .github/workflows/docpact.yml
  - _docs/**
lastReviewedAt: 2026-09-19
lastReviewedCommit: 07f65ef100300d7cb44996476af8a033a9ae8bbc
---

# TianGong AI Unstructure Agent Contract

This repository owns document unstructure and chunking workflows for TianGong AI
knowledge sources. Workspace-level submodule policy remains in the root
workspace; product implementation and repo-local documentation belong here.

## Required Load Order

1. Read this file.
2. Read `.docpact/config.yaml`.
3. Run `docpact route --root . --paths <target-paths> --format json` from this
   repo root for the files you plan to change.
4. Read the relevant files under `_docs/contracts/**`, `_docs/architecture/**`,
   and `_docs/runbooks/**`.
5. If the target is under `src/journals/**`, also read
   `src/journals/AGENTS.md`.
6. Read the implementation files under `src/**` or `docker/**`.

## Source Of Truth

- `.docpact/config.yaml`: machine-readable governance rules, routing aliases,
  coverage, document inventory, and freshness policy.
- `README.md`: legacy setup notes and uncurated operational examples; verify
  command paths against `src/**` before running.
- `_docs/runbooks/development.md`: curated setup, validation, and operation
  workflow.
- `_docs/contracts/repo-contract.md`: durable ownership and boundary rules.
- `_docs/architecture/repo-architecture.md`: current processing topology and
  key paths.

## Hard Boundaries

- Do not move workspace submodule policy, branch policy, or integration
  completion rules into this repository.
- Do not commit local credentials, downloaded source documents, generated
  processed outputs, or long-running job logs.
- Treat source-specific processing scripts under `src/**` as data pipeline
  behavior; update architecture or runbook docs when inputs, outputs, indexes,
  model dependencies, or operational commands change.

## Completion Criteria

- Relevant docpact route output has been reviewed before code or docs changes.
- Docs touched by the route result are reviewed or updated.
- `docpact validate-config --root . --strict` passes after governance changes.
- For implementation changes, run the relevant script, dependency, or smoke
  validation described in `_docs/runbooks/development.md`.
