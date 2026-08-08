---
docType: contract
scope: repo
status: current
authoritative: true
owner: unstructure
language: en
whenToUse: "When deciding whether a change belongs in the unstructure repository."
whenToUpdate: "When ownership, processing boundaries, dependency expectations, or completion criteria change."
checkPaths:
  - AGENTS.md
  - README.md
  - .docpact/config.yaml
  - src/**
  - requirements.txt
lastReviewedAt: 2026-08-09
lastReviewedCommit: 949c61064f2db2fdc1041265b7b983aa689938eb
---

# Unstructure Repository Contract

## Ownership

This repository owns document parsing, chunking, and unstructured processing
scripts for TianGong AI source domains, including ESG, education, journals,
patents, reports, standards, textbooks, and related vector/index preparation
flows.

## Boundaries

- API serving belongs in the edge-function repository unless the change is a
  local processing script here.
- MCP client-facing tool schemas belong in the MCP repository.
- Root workspace governance, branch policy, and submodule integration remain in
  the workspace repository.
- Large input documents, processed outputs, and job logs are operational data,
  not source documentation.

## Processing Surface

Scripts under `src/**`, dependency facts in `requirements.txt`, PM2 ecosystem
files, and local stack files under `docker/**` define processing behavior and
runtime operation. Changes to input formats, output locations, chunking logic,
embedding/index targets, OCR dependencies, process-manager definitions, or
long-running job commands require review of:

- `README.md`
- `_docs/architecture/repo-architecture.md`
- `_docs/runbooks/development.md`

## Completion Criteria

- Run `docpact route` before editing governed files.
- Run `docpact validate-config --root . --strict` after governance changes.
- For script changes, run the smallest representative script or import/syntax
  smoke check available for the touched domain.
- Do not leave dependency, data-flow, or validation facts only in chat.
