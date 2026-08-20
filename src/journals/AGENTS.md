---
docType: agent-note
scope: module
status: current
authoritative: true
owner: unstructure
language: en
whenToUse: "Before editing journal-specific unstructure scripts."
whenToUpdate: "When journal processing scripts, environment variables, queue behavior, or output paths change."
checkPaths:
  - src/journals/**
  - _docs/architecture/repo-architecture.md
  - _docs/runbooks/development.md
lastReviewedAt: 2026-08-20
lastReviewedCommit: 715aef9c4f063990e083165bc860a04acd9ca45b
---

# Journals Agent Notes

- Read the target script before changing journal processing behavior.
- Legacy pickle scripts: `src/journals/file_to_pickle.py` and
  `src/journals/file_to_pickle1.py` through `file_to_pickle4.py`.
- Two-stage queue scripts: `src/journals/two_stage_enqueue.py` and
  `src/journals/two_stage_enqueue_urgent.py`.
- Legacy pickle scripts use `TOKEN` plus PostgreSQL env vars. Two-stage scripts
  use `FASTAPI_BEARER_TOKEN` plus optional `TWO_STAGE_*` env vars.
- Outputs include `docs/processed_docs/journal_new_pickle`,
  `journal_pickle_queue`, and script-specific log/error files.
