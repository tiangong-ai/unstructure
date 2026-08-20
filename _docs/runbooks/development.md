---
docType: runbook
scope: repo
status: current
authoritative: true
owner: unstructure
language: en
whenToUse: "When developing, validating, or operating unstructure processing workflows."
whenToUpdate: "When setup commands, dependencies, long-running job commands, or validation steps change."
checkPaths:
  - README.md
  - requirements.txt
  - src/**
  - docker/**
lastReviewedAt: 2026-08-20
lastReviewedCommit: 715aef9c4f063990e083165bc860a04acd9ca45b
---

# Unstructure Development Runbook

## Setup

1. Create and activate a Python virtual environment, typically Python 3.12.
2. Install dependencies from `requirements.txt`.
3. Install native tooling required by the target workflow, such as
   `libmagic-dev`, `poppler-utils`, `libreoffice`, `pandoc`, and OCR
   dependencies.
4. Configure any cloud, vector database, or model provider credentials through
   local environment files; do not commit secrets.

## Validation

Run:

```bash
docpact validate-config --root . --strict
```

For Python script changes, run the smallest representative command for the
touched domain, or at minimum a syntax/import smoke check against the changed
script in the active virtual environment.

For the KB parse worker, run a syntax smoke check before connecting to live
queues:

```bash
python -m compileall src/kb_parse_worker
```

Run one queue message:

```bash
python -m src.kb_parse_worker.cli once
python -m src.kb_parse_worker.cli once --worker parse-submitter
python -m src.kb_parse_worker.cli once --worker parse-finalizer
python -m src.kb_parse_worker.cli once --worker s3-ready
python -m src.kb_parse_worker.cli once --worker parse-finalization-reconciler --dry-run
python -m src.kb_parse_worker.cli once --worker parse-finalization-reconciler
```

Run continuously:

```bash
python -m src.kb_parse_worker.cli run
python -m src.kb_parse_worker.cli run --worker parse-submitter
python -m src.kb_parse_worker.cli run --worker parse-finalizer
python -m src.kb_parse_worker.cli run --worker s3-ready
python -m src.kb_parse_worker.cli run --worker parse-finalization-reconciler
```

Run continuously under PM2:

```bash
pm2 start ecosystem.kb_parse_worker.json
pm2 start ecosystem.kb_parse_worker.two_stage_async.json
pm2 save
pm2 resurrect
pm2 logs kb-parse-worker
pm2 logs kb-parse-submitter
pm2 logs kb-parse-finalizer
pm2 logs kb-s3-ready-worker
pm2 restart kb-parse-worker
pm2 restart kb-parse-submitter
pm2 restart kb-parse-finalizer
pm2 restart kb-s3-ready-worker
pm2 stop kb-parse-worker
pm2 stop kb-parse-submitter
pm2 stop kb-parse-finalizer
pm2 stop kb-s3-ready-worker
pm2 delete kb-parse-worker
pm2 delete kb-parse-submitter
pm2 delete kb-parse-finalizer
pm2 delete kb-s3-ready-worker
```

The KB parse worker explicitly loads the repository-local `.env` file before
falling back to the default `.env` lookup, so it can be started from either the
repository root or the workspace root.

Current workspace live F03 workers run on the self-hosted parse worker host,
not on the local operator machine. The worker host is the machine that can read
`NAS_RAW_ROOT`, write `NAS_PROCESSED_ROOT`, reach Supabase Postgres, and call
Unstructure-Serve. Operator-side variables such as `PARSE_WORKER_HOST`,
`PARSE_WORKER_SSH_PORT`, `PARSE_WORKER_USER`, `PARSE_WORKER_SSH_HOST_ALIAS`,
`SUPABASE_DB_*`, `NAS_*`, `UNSTRUCTURE_SERVE_*`, `KB_PROCESSED_S3_*`, and AWS
credentials, plus `KB_EMBEDDING_*`, are loaded from the root workspace
`.env.ops.local` only for manual operations, SSH login, and smoke-test
preparation. Do not treat root
`.env.ops.local` as worker runtime configuration and do not copy secret values
into docs, issues, PRs, or chat.

The remote worker runtime must receive the required values from the remote
`TianGong-AI-Unstructure/.env` file or host process environment. Run live
`once`, `run`, PM2 restart, and S3-ready checks on that remote host unless the
local machine has an equivalent NAS mount and the same runtime variables.

Required runtime variables:

```text
DATABASE_URL or SUPABASE_DB_URL or KB_DATABASE_URL
or SUPABASE_DB_HOST / SUPABASE_DB_PORT / SUPABASE_DB_NAME /
SUPABASE_DB_USER / SUPABASE_DB_PASSWORD
NAS_RAW_ROOT
NAS_PROCESSED_ROOT
UNSTRUCTURE_SERVE_URL
UNSTRUCTURE_SERVE_BEARER_TOKEN
KB_EMBEDDING_BASE_URL
KB_EMBEDDING_MODEL
KB_PROCESSED_S3_BUCKET when overriding the default processed bucket
KB_S3_READY_QUEUE when overriding the default s3-ready queue
```

Current workspace worker deployment points `UNSTRUCTURE_SERVE_URL` at:

```text
UNSTRUCTURE_SERVE_URL=http://192.168.1.140:7770/mineru_with_images
```

The parse worker can keep using that synchronous endpoint, or switch to
Unstructure-Serve's internal two-stage Celery pipeline by setting:

```text
KB_PARSE_USE_TWO_STAGE=true
UNSTRUCTURE_SERVE_TWO_STAGE_BASE_URL=http://192.168.1.140:7770
KB_PARSE_TWO_STAGE_SUBMIT_TIMEOUT_SECONDS=120
KB_PARSE_TWO_STAGE_STATUS_TIMEOUT_SECONDS=30
KB_PARSE_TWO_STAGE_POLL_INTERVAL_SECONDS=3
KB_PARSE_TWO_STAGE_PRIORITY=normal
KB_PARSE_TWO_STAGE_CHUNK_TYPE=true
```

If `UNSTRUCTURE_SERVE_TWO_STAGE_BASE_URL` is omitted, the worker derives it from
`UNSTRUCTURE_SERVE_URL` by removing a trailing `/mineru_with_images`, `/mineru`,
or `/two_stage/task`. `UNSTRUCTURE_SERVE_BEARER_TOKEN` is reused for both
submit and status polling. Optional `KB_PARSE_TWO_STAGE_PROVIDER`,
`KB_PARSE_TWO_STAGE_MODEL`, and `KB_PARSE_TWO_STAGE_PROMPT` can override the
parser-side vision defaults; leave them unset for normal deployment.

The default `parse` worker keeps the historical synchronous behavior: it submits
one `/two_stage/task`, polls it to completion, writes processed artifacts, and
then claims the next KB parse job. To let Unstructure-Serve's Celery queues hold
a controlled parse backlog instead, run the split async modes:

```text
KB_PARSE_USE_TWO_STAGE=true
KB_PARSE_TWO_STAGE_PARSE_QUEUE=queue_parse_gpu
KB_PARSE_TWO_STAGE_MAX_PARSE_BACKLOG=5
KB_PARSE_TWO_STAGE_QUEUE_STATUS_TIMEOUT_SECONDS=10
KB_PARSE_TWO_STAGE_FINALIZER_LIMIT=1
```

For PM2 deployment of the split mode, stop or delete the historical
`kb-parse-worker` process first, then start
`ecosystem.kb_parse_worker.two_stage_async.json`. Keep `kb-s3-ready-worker`
running from `ecosystem.kb_parse_worker.json`; do not run the historical
`kb-parse-worker` and split submitter/finalizer against the same parse queue at
the same time.

`parse-submitter` calls `/two_stage/queue_status` and submits another KB parse
job only when `queues[KB_PARSE_TWO_STAGE_PARSE_QUEUE] +
unacked[KB_PARSE_TWO_STAGE_PARSE_QUEUE]` is below
`KB_PARSE_TWO_STAGE_MAX_PARSE_BACKLOG`. It persists the returned Celery
`task_id` in `kb_jobs.payload_json.two_stage` through the KB control-plane RPC
and archives the original parse PGMQ message. `parse-finalizer` claims those
persisted task ids, polls `/two_stage/task/{task_id}`, extends short polling
locks for pending tasks, and performs the existing processed-artifact and
`s3_ready` handoff when a task succeeds.

Both modes request `return_txt=true`. The worker uses the returned `result` JSON
for chunk embeddings and pickle generation, drops any returned chunk whose
`text` is empty before embedding, and writes the returned whole-document `txt`
payload as `{artifact_uuid}.txt` beside `{artifact_uuid}.pkl`.

After MinerU returns chunks, the parse worker writes the raw parser chunks to a
JSON artifact, splits chunks above the embedding token cap, and then calls the
OpenAI-compatible embedding endpoint before writing the pickle artifact. Text
chunks split on sentence or newline boundaries, table-like HTML chunks split on
`<tr>` row boundaries, and oversized single sentences or rows fall back to a
hard token split. The pickle artifact stores each embedding child chunk with an
`embedding` key. Parser chunks that have no type omit the `type` key instead of
writing `type: null`. The worker requests provider-default
Qwen3-Embedding-8B vectors, then locally truncates and normalizes them to the
configured dimension:

```text
KB_EMBEDDING_BASE_URL=http://192.168.1.140:7710/v1
KB_EMBEDDING_MODEL=Qwen/Qwen3-Embedding-8B
KB_EMBEDDING_API_KEY=EMPTY
KB_EMBEDDING_DIMENSIONS=1536
KB_EMBEDDING_BATCH_SIZE=32
KB_EMBEDDING_CHUNK_MAX_TOKENS=8000
KB_EMBEDDING_TIMEOUT_SECONDS=600
```

Current workspace design documents point the processed S3 location at bucket
`tiangong` with prefix `processed_docs`. The worker defaults to those values and
keeps both overridable through runtime configuration:

```text
KB_PROCESSED_S3_BUCKET=tiangong
KB_PROCESSED_S3_PREFIX=processed_docs
```

Long-running parse jobs keep both the KB job lock and PGMQ visibility timeout
fresh through `heartbeat_job(...)`. The worker defaults to:

```text
KB_PARSE_HEARTBEAT_INTERVAL_SECONDS=60
KB_PARSE_JOB_TIMEOUT_SECONDS=7200
```

Raw and processed artifact paths are derived from the collection storage path.
For example, collection path `/course/thu_humanities` resolves raw files under
`course/thu_humanities/{document_id}{file_ext}` and processed artifacts under
`course_pickle/thu_humanities_pickle/{document_id}`.

The workers do not upload artifacts to S3 directly. The parse worker writes raw
inputs and processed artifacts to NAS paths, including the manifest-declared
parser JSON, pickle, and optional full-text TXT artifacts, calls
`complete_parse_local_ready_and_enqueue_s3_check(...)`, archives the parse queue
message, and exits without waiting for NAS-to-S3 sync. The local-ready RPC and
message archive, S3-ready completion RPC and message archive, and `fail_job_v2`
failure writes are retried up to three times for transient Postgres connection
errors; retries reconnect before replaying the idempotent DB handoff. The
S3-ready worker then waits for the NAS sync layer to publish processed artifacts
to S3 before calling `complete_s3_ready_check(...)`. Tune the S3-ready worker
check wait window with:

```text
KB_PARSE_S3_READY_TIMEOUT_SECONDS=900
KB_PARSE_S3_READY_POLL_INTERVAL_SECONDS=15
KB_PARSE_S3_READY_JOB_TIMEOUT_SECONDS=1200
```

For deployments where NAS-to-S3 sync can take a long time, keep the parse worker
and S3-ready worker as separate PM2 processes. Retry attempts for the S3-ready
stage reuse `processed_manifest_local_uri` and only re-check processed S3
readiness; they do not parse the document again.

If a parse job generated deterministic processed artifacts but the final DB
handoff never committed, run the parse finalization reconciler on the worker
host. It scans stale `failed`, `dead`, or expired `running` parse jobs whose
documents do not yet have processed manifest fields, checks the deterministic
`NAS_PROCESSED_ROOT/{processed_storage_path}/{document_id}/manifest.json`, and
calls `replay_parse_local_ready_from_artifact(...)` to mark local processed
artifacts ready and enqueue `s3_ready`. Use `--dry-run` before live replay, and
use `--document-id <uuid>` to limit a repair to one document.

Worker failures are classified before calling `fail_job_v2(...)`. Terminal parse
failures such as `RAW_HASH_MISMATCH`, `RAW_STORAGE_PATH_MISMATCH`, malformed
parser responses, empty parse results, and embedding schema/dimension errors are
reported with `retryable=false`. Transient parser, embedding, DB, timeout, and
network failures remain retryable. Terminal S3-ready failures include missing
local manifests, manifest identity mismatches, and strict sha256 artifact
mismatches; ordinary S3 sync delays remain retryable. Retryable failure calls
schedule delayed retry wake-up messages in the KB control plane, so the worker
archives the current PGMQ transport message by queue/message id after
`fail_job_v2(...)` returns. Claim dispositions such as `backoff_not_due`,
`cancelled`, `dead`, and duplicate wake-ups are archived only when
`archive_current_message` is true.

Use `KB_PARSE_S3_READY_MODE=skip` only for local smoke runs where processed S3
sync is intentionally unavailable.

## Long-Running Jobs

Use current script paths confirmed with `rg --files src` before starting
long-running jobs. Treat README background commands as legacy notes unless the
referenced path exists.

## Documentation Updates

Update `_docs/architecture/repo-architecture.md` when a source domain,
processing stage, output target, or dependency class changes. Update this
runbook when setup, validation, or job operation steps change.
