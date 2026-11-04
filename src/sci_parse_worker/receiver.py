"""Local receipt for the independent SCI worker; cloud indexing is separate."""
from __future__ import annotations

import tempfile
import time
from contextlib import closing
from pathlib import Path
from uuid import UUID

import psycopg2
from psycopg2.extras import Json, RealDictCursor

from src.kb_parse_worker import control_plane
from .artifacts import PIPELINE, digest, document_path, promote, publish, verify_run


class SCIContractError(ValueError):
    """Invalid registration or result: never fall back to legacy embedding."""


class SCILeaseLost(RuntimeError):
    """The current document/task no longer belongs to this finalizer."""


def is_sci(snapshot) -> bool:
    meta, payload = snapshot.document_metadata_json, snapshot.job_payload_json
    return (str(meta.get('pipeline', '')).startswith('sci-')
            or str(payload.get('pipeline', '')).startswith('sci-'))


def validate_contract(snapshot) -> None:
    meta, payload = snapshot.document_metadata_json, snapshot.job_payload_json
    if (meta.get('pipeline') != PIPELINE or payload.get('pipeline') != PIPELINE
            or meta.get('source_table') != 'journals'
            or meta.get('journal_id') != snapshot.document_id
            or payload.get('source_sha256') != snapshot.sha256):
        raise SCIContractError('SCI requires matching pipeline, journals UUID and registered source hash')
    UUID(snapshot.document_id)
    UUID(snapshot.job_id)


def receive(conn, config, claimed, snapshot, staged: Path) -> Path:
    """Fence publication and receipt with current document/job locks.

    The submitter already archived the PGMQ message. This transaction neither
    sends nor archives messages and never updates journals. A lost DB response
    can replay the same receipt; stale tasks cannot overwrite current files.
    """
    validate_contract(snapshot)
    destination = document_path(config.nas_raw_root / 'processed' / 'sci', snapshot.document_id)
    manifest = verify_run(staged, snapshot.document_id, claimed.job_id, snapshot.sha256)
    if manifest.get('document_version') != snapshot.document_version:
        raise SCIContractError('SCI manifest document version mismatch')
    manifest_hash = digest(staged / 'manifest.json')
    with conn, conn.cursor(cursor_factory=RealDictCursor) as cur:
        cur.execute('''select j.document_id,j.document_version as job_version,'parse' as stage,j.status,j.phase,j.source_sha256,
            j.locked_by,j.locked_until>clock_timestamp() as lease_valid,j.payload_json,
            d.document_version,d.deleted_at,d.sha256,d.metadata_json,d.status as document_status,
            d.processed_manifest_local_uri,d.processed_manifest_hash
            from private.sci_parse_jobs j join public.kb_documents d on d.id=j.document_id
            where j.id=%s for update of j,d''', (claimed.job_id,))
        row = cur.fetchone()
        if (not row or str(row['document_id']) != snapshot.document_id
                or claimed.document_id != snapshot.document_id
                or row['job_version'] != snapshot.document_version
                or claimed.document_version != snapshot.document_version
                or row['document_version'] != snapshot.document_version
                or row['deleted_at'] is not None or row['sha256'] != snapshot.sha256
                or row['stage'] != 'parse' or row['phase'] != 'receive'
                or row['source_sha256'] != snapshot.sha256
                or not isinstance(row['payload_json'].get('parser_task'), dict)
                or row['payload_json']['parser_task'].get('task_id') != claimed.task_id):
            raise SCILeaseLost('SCI document/task identity changed before publication')
        if (row['metadata_json'].get('pipeline') != PIPELINE
                or row['metadata_json'].get('source_table') != 'journals'
                or row['metadata_json'].get('journal_id') != snapshot.document_id
                or row['payload_json'].get('pipeline') != PIPELINE
                or row['payload_json'].get('source_sha256') != snapshot.sha256):
            raise SCIContractError('SCI registration changed before publication')
        cur.execute('select id from public.journals where id=%s for key share', (snapshot.document_id,))
        if not cur.fetchone():
            raise SCIContractError('SCI UUID does not exist in journals')
        if row['status'] == 'succeeded':
            if (row['document_status'] == 'parsed_local'
                    and row['processed_manifest_local_uri'] == str(destination / 'manifest.json')):
                existing = verify_run(destination, snapshot.document_id, claimed.job_id, snapshot.sha256)
                if (digest(destination / 'manifest.json') == row['processed_manifest_hash']
                        and existing.get('document_version') == snapshot.document_version
                        and existing['files'] == manifest['files']
                        and existing['parser'] == manifest['parser']):
                    return destination
            raise SCILeaseLost('SCI completed receipt differs; do not overwrite')
        if (row['status'] != 'running' or row['locked_by'] != config.worker_id
                or not row['lease_valid']):
            raise SCILeaseLost('SCI job lease lost before publication')
        promote(staged, destination)
        verify_run(destination, snapshot.document_id, claimed.job_id, snapshot.sha256)
        cur.execute('''update private.sci_parse_jobs set status='succeeded',finished_at=now(),updated_at=now(),
            locked_by=null,locked_until=null,next_retry_at=null,last_error=null,
            payload_json=payload_json || %s where id=%s
            and locked_by=%s and locked_until>clock_timestamp() returning id''',
            (Json({'local_parse': {'pipeline': PIPELINE, 'manifest_sha256': manifest_hash,
                                  'awaiting': 'cloud_embedding'}}), claimed.job_id, config.worker_id))
        if cur.fetchone() is None:
            raise SCILeaseLost('SCI lease expired during publication; receipt rolled back')
        cur.execute('''update public.kb_documents set status='parsed_local',parse_time=%s,
            processed_manifest_local_uri=%s,processed_manifest_hash=%s,processed_artifact_uuid=%s,
            last_error=null,last_error_stage=null,
            metadata_json=metadata_json || %s where id=%s and document_version=%s and deleted_at is null''',
            (manifest['created_at'], str(destination / 'manifest.json'), manifest_hash,
             claimed.job_id, Json({'local_parse': {'pipeline': PIPELINE,
                'parsed_item_count': manifest['parsed_item_count'], 'awaiting': 'cloud_embedding'}}),
             snapshot.document_id,snapshot.document_version))
    return destination


def finalize(config, claimed, snapshot, parsed) -> Path:
    validate_contract(snapshot)
    root = config.nas_raw_root / 'processed' / 'sci'
    root.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='.staging-', dir=root) as staging:
        try:
            staged = publish(Path(staging), snapshot.document_id, claimed.job_id, snapshot.sha256,
                             parsed, {'profile': 'mineru-task-advanced', 'version': config.parser_version,
                                      'endpoint': '/mineru/task',
                                      'task_id': claimed.task_id},
                             document_version=snapshot.document_version)
        except ValueError as exc:
            raise SCIContractError(str(exc)) from exc
        for attempt in range(3):
            try:
                with closing(control_plane.connect(config.database_url)) as conn:
                    return receive(conn, config, claimed, snapshot, staged)
            except (psycopg2.InterfaceError, psycopg2.OperationalError):
                if attempt == 2:
                    raise
                time.sleep(2 ** attempt)
    raise RuntimeError('SCI receipt retry exhausted')
