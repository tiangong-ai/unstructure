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
lastReviewedAt: 2026-11-14
lastReviewedCommit: 9f3347d233319199825b3dd6d6425788b9572f4f
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

- [SCI 工作日记](../SCI_DOCUMENT_WORKLOG.md): SCI 问题台账、历史基线、当前焦点与修改记录；后续 SCI 工作先读此处。
- [历史证据索引](reference/sci-evidence-index.md): 本地保留范围、远端历史定位、未批准提案与提交边界。
- [本机与 141 操作说明](runbooks/sci-local-server.md): 两端路径、NAS 别名、证据位置与交接规则。
- `_docs/contracts/repo-contract.md`: repository ownership, boundaries, and
  completion rules.
- `_docs/architecture/repo-architecture.md`: document processing topology.
- `_docs/runbooks/development.md`: setup, validation, and operation workflow.
- `_docs/standards/documentation-standards.md`: repo-local documentation rules.
