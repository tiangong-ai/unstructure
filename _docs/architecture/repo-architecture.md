---
docType: architecture
scope: repo
status: current
authoritative: true
owner: unstructure
language: en
whenToUse: "When changing document processing scripts, dependencies, or local service stack files."
whenToUpdate: "When source domains, data flow, index targets, OCR/model dependencies, or runtime assumptions change."
checkPaths:
  - src/**
  - docker/**
  - requirements.txt
lastReviewedAt: 2026-11-14
lastReviewedCommit: 390f33b7ea0a11a31ab834fafb05c919a0ed6b5b
---

# Unstructure Architecture

## Overview

The repository contains Python workflows for converting heterogeneous source
documents into structured chunks and index-ready artifacts for TianGong AI.
Workflows are organized by source domain under `src/**`.

## Key Paths

- `src/{ali,education,edu_textbooks,esg,journals,patents,pptx,reports,standards}/**`:
  source-domain processing scripts.
- `src/kb_parse_worker/**`: KB F03 parse and S3-ready workers. The parse
  worker consumes `kb_parse_queue`, claims jobs through the KB control plane,
  calls Unstructure-Serve either through the legacy synchronous
  `/mineru_with_images` endpoint or, when `KB_PARSE_USE_TWO_STAGE=true`,
  submits `/two_stage/task` and polls `/two_stage/task/{task_id}` for the same
  `result`/`txt` payload contract. It publishes processed artifacts to NAS and
  enqueues the S3-ready check. For deployments that want Unstructure-Serve's
  Celery queue to hold parse backlog, the worker also supports split
  `parse-submitter` and `parse-finalizer` modes: the submitter applies
  `/two_stage/queue_status` backpressure, submits `/two_stage/task`, persists
  the returned task id in the KB job payload, and archives the parse PGMQ
  message; the finalizer claims those persisted task ids, polls task status,
  writes processed artifacts, and completes the parse handoff. The S3-ready
  worker consumes
  `kb_s3_ready_queue` and marks processed artifacts ready after S3 verification.
- `ecosystem.kb_parse_worker.json`: PM2 process definitions for the KB parse
  worker and S3-ready worker.
- `src/journals/**`: journal workflows; also read `src/journals/AGENTS.md`.
- `src/tools/**` and per-domain `tools/**`: helper modules.
- `src/weaviate/**`: local Weaviate utility scripts.
- `src/legacy/**`: legacy experiments and migration references; verify before reuse.
- `docker/weaviate/*.yml`: local Weaviate stacks.
- `requirements.txt`: Python dependency facts.

## Runtime Shape

The repository expects Python virtual environments and native document tooling
such as poppler, libreoffice, pandoc, tesseract, and CUDA-capable optional
components for some workloads. Treat README command dumps as legacy notes unless
the referenced files still exist.

## Integration Points

- Output artifacts feed downstream knowledge-base and search/index services.
- The KB parse worker reads raw document locations from `kb_documents.raw_uri`,
  requires raw files to live under the collection-derived storage path using
  the renamed canonical filename `{document_id}{file_ext}`, writes processed
  `json`/`pkl`/`txt`/`manifest.json` artifacts under the configured NAS
  processed root using a `_pickle` suffixed path derived from the collection
  storage path, and calls `complete_parse_local_ready_and_enqueue_s3_check(...)`.
  The worker requests `return_txt=true` from Unstructure-Serve in both sync and
  two-stage modes, drops parser chunks whose `text` is empty before embedding,
  and writes the returned whole-document text as `{artifact_uuid}.txt` beside
  the pickle artifact.
  Before artifact writes, the worker stores the parser result as a JSON artifact,
  splits chunks above the embedding token cap into child chunks, embeds every
  child chunk `text` field through the OpenAI-compatible Qwen3-Embedding-8B
  endpoint, locally truncates and normalizes vectors to 1536 dimensions, and
  stores vectors in the pickle chunks under `embedding`.
  That RPC completes the parse job, leaves the document in `s3_sync_pending`,
  and enqueues a durable `s3_ready` job. The parse worker treats that final
  local-ready RPC plus parse-message archive as one finalization step. A parse
  finalization reconciler can repair historical splits where deterministic
  processed artifacts already exist on NAS but the final DB handoff did not
  commit; it validates the local manifest identity and replays the local-ready
  DB transition without re-running document parsing. A
  separate S3-ready worker then verifies the processed manifest/json/pkl/txt
  objects after NAS-to-S3 sync and calls `complete_s3_ready_check(...)` to mark
  `processed_s3_ready`. Final DB transitions and failure writes retry transient
  Postgres connection failures with short reconnecting backoff. PM2 keeps
  both long-running worker processes resident; each worker's heartbeat loop
  keeps its active job lock and PGMQ visibility timeout fresh. Parse and
  S3-ready jobs also enforce worker-local hard timeouts, consume typed
  `claim_job_from_pgmq_message(...)` dispositions, classify failures as
  retryable or terminal before calling `fail_job_v2(...)`, and archive the
  current PGMQ message by queue/message id only when the control plane contract
  says the current wake-up should be removed. Retryable failures archive the
  current transport message after `fail_job_v2(...)` schedules the delayed
  retry wake-up.
- Edge functions query indexes or storage populated by these workflows, but API
  serving remains outside this repository.


## Independent SCI local parsing

SCI runs `src.sci_parse_worker.cli` as two independently managed processes,
`sci-parse-submitter` and `sci-parse-finalizer`. It uses the private
`private.sci_parse_jobs` ledger and `kb_sci_parse_queue`. Neither process calls
legacy KB claim, retry or finalization RPCs. No new SCI jobs enter `kb_jobs`, so
unchanged humanities workers and reconcilers cannot claim them. The existing
KB worker implementation/CLI/ecosystem files remain unchanged.

The submitter reads an already existing SCI PDF, validates its registered hash,
and submits to the ordinary `/mineru/task` Celery endpoint on 140. SCI bounds
its own outstanding tasks using the private ledger. SCI records the returned task ID in its
private ledger and archives its own PGMQ message. The finalizer polls those
registered tasks; it does not require a second PGMQ queue while the parser runs.
Retries of a failed parser task go only to the SCI queue. Ambiguous submission
outcomes stop for inspection rather than silently sending the PDF again.

Registration reuses the journals UUID as the KB document UUID, atomically writes
both pipeline markers (`sci-ingestion-contract-v1`) and the pinned source hash,
and enqueues the SCI job. It never generates or changes journals metadata.
An explicit reprocess accepts only compatible SCI/manual-probe documents with
no active legacy job, and clears current KB parse/index readiness. Historical
search data itself is not deleted by registration.

The receiver writes `NAS_RAW_ROOT/processed/sci/<document UUID>/<document UUID>.pkl`,
`<document UUID>.txt`, `<document UUID>.json` and `manifest.json`. There is no run UUID directory and no local
embedding/chunk splitting. The job UUID remains in the manifest for correlation.
The receiver verifies current document/task/version/hash/lease under row locks,
then records `parsed_local` and a succeeded private SCI job. It does not enqueue
legacy S3/index jobs or change SCI upload, embedding or fulltext times.

Document UUID filenames are prepared in staging and published using hard links/renames,
manifest last. Filesystem publication and database commit are not atomic. The
future AWS consumer must check both payload hashes and the committed current
document manifest hash/version/source and succeeded **private SCI job** before
processing. A complete file set alone is not a completed receipt. NAS-to-S3
verification, Lambda/OpenAI embedding and indexing are not implemented here.

The manifest file list names the same document UUID artifacts. During an explicit
SCI reprocess, only the prior `parsed.pkl` / `fulltext.txt` aliases are retired
before the new manifest is published. Mixed old/new names after interruption can
be retried; unexpected files, symlinks or a different document directory stop
publication. Previously completed documents are not swept or renamed on startup.

The JSON stores the complete successful HTTP response. TXT joins every original
chunk in reading order and converts inline HTML to text. PKL preserves chunk
metadata after removing headers and sections matched by the mineru_sci heading
rules, including references, acknowledgements and data availability. The
manifest v2 records the rules, counts and all three hashes.

SCI submitter no-claim polling backs off from 30 seconds to 60 seconds after
three consecutive idle checks. Work, capacity backpressure and exceptions reset
the idle streak; the existing active delay is retained. The finalizer continues
its own configured interval (5 seconds on 142), so idle submission polling does
not delay collection of running parser results. No extra queue/table is needed.

### SCI historical journal backfill candidate

The separate [Go backfill command](../../src/journals/sci-backfill/README.md)
uses journal UUIDs in an SCI task ledger, the existing two MinerU nodes, the
raw JSON publication path, and the S3 notification queue. The EC2 consumer
can recover missing notifications by checking due ledger rows against S3.
It writes new fulltext documents to `sci_v1`, keeps immutable per-paper
vectors in an S3 staging prefix, and commits Parquet shard receipts from
durable member sets. The import root contains only final shards. Query
cutover and manual Pinecone import are separate operations.
