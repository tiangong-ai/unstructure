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
lastReviewedAt: 2026-11-14
lastReviewedCommit: 390f33b7ea0a11a31ab834fafb05c919a0ed6b5b
---

# TianGong AI Unstructure Agent Contract

This repository owns document unstructure and chunking workflows for TianGong AI
knowledge sources. Workspace-level submodule policy remains in the root
workspace; product implementation and repo-local documentation belong here.

## Required Load Order

1. Read this file.
2. Read `.docpact/config.yaml`.
   For SCI / journals metadata, PDF, PKL, parsing or related index work, also
   read [SCI_DOCUMENT_WORKLOG.md](SCI_DOCUMENT_WORKLOG.md) before choosing scope.
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
- `SCI_DOCUMENT_WORKLOG.md`: SCI issue ledger, dated audit evidence, accepted
  decisions, current focus and append-only work diary; not a live data inventory.
- `_docs/reference/sci-evidence-index.md`: historical evidence locations, local
  retention, unpublished draft status and Git publication boundaries.
- `_docs/runbooks/sci-local-server.md`: local/141 execution boundaries,
  remote evidence locations and checkout handoff procedure.

## SCI 文档工作约定

这些约定适用于 SCI / journals 元数据、PDF、PKL、拆解任务及相关索引工作。

- 开始前阅读 [SCI_DOCUMENT_WORKLOG.md](SCI_DOCUMENT_WORKLOG.md)，将任务对应到已有问题编号；新问题补入台账，并写清本次目标、范围、验收和停止条件。
- 按用户当次指示推进；调整既定范围或顺序时，在工作日记中记录原因。尚未确定的方案不能写成用户已接受的决定。
- 工作结束时更新问题状态、当前焦点，并追加修改内容、处理范围、验证证据和剩余问题。分别记录代码、历史产物和索引的完成情况；只改代码不能宣称历史数据已修复。
- `analysis/` 是 Git 排除的历史证据区，不是现行规范或脚本入口；先按 [证据索引](_docs/reference/sci-evidence-index.md) 确认来源和日期，不重放历史清理脚本，不把旧授权用于新任务。
- 保留 2026-10-10 审计基线和历史日记；更正以新条目追加。新计数注明日期、执行端和口径；抽样检查不能表述为全量通过。
- 涉及服务器或 NAS 时先读 [本机与 141 操作说明](_docs/runbooks/sci-local-server.md)。日记同一时刻只有一个写入端；切换前比较两端版本和未提交修改，合并新增记录，不能直接覆盖。
- `src/journals/AGENTS.md` 中的运行说明含历史入口。执行前核对脚本、服务及路径是否仍存在，不直接照搬停止进程或批处理命令；本机与服务器同名仓库也不代表代码相同。

非 SCI 任务不受这份工作日记的范围约束。日记承载问题与证据；需要开发交付时，仍按工作区约定关联 GitHub Issue / PR，不以日记状态替代交付状态。

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
