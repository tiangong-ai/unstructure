-- Apply only to the selected SCI ledger database, before registering a run.
begin;
create schema if not exists private;
alter table public.journals add column if not exists unstructure_time timestamptz;
create table if not exists private.sci_backfill_runs (
  run_id text primary key,
  sealed boolean not null default false
);
create table if not exists private.sci_ingest_tasks (
  id uuid primary key default gen_random_uuid(),
  journal_id uuid not null references public.journals(id),
  doi text not null,
  journal text not null,
  published_date text not null,
  parser_node int not null check (parser_node in (140,141)),
  mineru_task_id uuid,
  local_state text not null default 'queued',
  local_attempts int not null default 0,
  local_published_at timestamptz,
  cloud_state text not null default 'queued',
  cloud_attempts int not null default 0,
  lease_owner text,
  lease_until timestamptz,
  opensearch_done_at timestamptz,
  indexed_chunks int,
  last_error text,
  parquet_at timestamptz
);
alter table private.sci_ingest_tasks
  add column if not exists run_id text references private.sci_backfill_runs(run_id),
  add column if not exists scan_at timestamptz not null default now(),
  add column if not exists local_lease timestamptz,
  add column if not exists pgmq_msg_id bigint,
  add column if not exists parse_submitted_at timestamptz,
  add column if not exists local_failed_at timestamptz,
  add column if not exists parquet_at timestamptz,
  add column if not exists staged_at timestamptz,
  add column if not exists stage_bytes bigint,
  add column if not exists stage_sha text,
  add column if not exists shard_id bigint;
alter table private.sci_ingest_tasks
  alter column mineru_task_id drop not null,
  alter column mineru_task_id drop default,
  alter column local_state set default 'queued',
  alter column local_attempts set default 0,
  drop constraint if exists sci_ingest_tasks_local_attempts_check;
alter table private.sci_ingest_tasks
  add constraint sci_ingest_tasks_local_attempts_check check (local_attempts between 0 and 3);
create unique index if not exists sci_run_journal on private.sci_ingest_tasks(run_id,journal_id);
create unique index if not exists sci_run_doi on private.sci_ingest_tasks(run_id,doi);
create index if not exists sci_cloud_scan on private.sci_ingest_tasks(run_id,scan_at,id)
  where local_state='done' and cloud_state in ('queued','running');
create index if not exists sci_local_scan on private.sci_ingest_tasks(run_id,parser_node,local_state,local_lease,id);
create index if not exists sci_final_lease
  on private.sci_ingest_tasks(run_id,lease_until)
  where local_state='done' and cloud_state='running' and cloud_attempts>=3;
create index if not exists sci_run_pending on private.sci_ingest_tasks(run_id)
  where local_state not in ('done','failed') or
    (local_state='done' and cloud_state in ('queued','running'));
create index if not exists sci_stage_pending on private.sci_ingest_tasks(run_id,staged_at,id)
  where cloud_state='staged' and shard_id is null;
create index if not exists sci_shard_members on private.sci_ingest_tasks(run_id,shard_id,doi);
create table if not exists private.sci_backfill_shards (
  id bigint generated always as identity primary key,
  run_id text not null references private.sci_backfill_runs(run_id),
  bytes bigint,
  sha text,
  vector_count bigint,
  uploaded_at timestamptz
);
alter table private.sci_ingest_tasks enable row level security;
alter table private.sci_backfill_runs enable row level security;
alter table private.sci_backfill_shards enable row level security;
revoke all on private.sci_backfill_runs, private.sci_backfill_shards from public, anon, authenticated;
commit;
