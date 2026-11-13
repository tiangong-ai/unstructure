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
lastReviewedAt: 2026-11-14
lastReviewedCommit: 9f3347d233319199825b3dd6d6425788b9572f4f
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

## SCI work diary

`SCI_DOCUMENT_WORKLOG.md` is a dated operational ledger, not a live inventory or
a product-wide processing contract. Preserve historical audit counts and diary
entries; append corrections with their evidence. Current focus and issue status
may change, with the corresponding dated diary entry.

At the user's request, `_docs/runbooks/sci-local-server.md` records the minimum
host and path information needed to resume SCI work. These are dated environment
observations, not portable defaults. Keep credentials and raw inventories out of
Git; keep remote evidence pointers and selective file hashes for traceability.
SCI-only rules must not impose this ledger on other source domains.

## Historical evidence and proposals

Keep raw audit responses, inventories, operational scripts, screenshots and
historical design drafts under Git-ignored `analysis/`; retain the original bytes.
The [evidence index](../reference/sci-evidence-index.md) is the tracked locator
and publication boundary. An ignored directory is not a backup: preserve a
verified local copy and record any unverified remote-only source explicitly.

Historical designs stay proposals until reviewed under a separate tracked task;
do not promote them into current architecture as part of documentation cleanup.
Historical locators are plain text paths, not links promising files in a fresh
Git checkout. Formal repository links must resolve. Link-markup normalization
is allowed with the original labels/paths retained, an exact pre-curation copy,
and a preservation comparison; it must not rewrite dated facts or decisions.
