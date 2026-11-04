"""SCI-only job lifecycle; never calls the legacy KB claim/retry RPCs."""
from contextlib import contextmanager
from types import SimpleNamespace
from uuid import UUID

from psycopg2.extras import Json, RealDictCursor
from src.kb_parse_worker.control_plane import connect
from .artifacts import PIPELINE

QUEUE = 'kb_sci_parse_queue'


@contextmanager
def transaction(config):
    conn = connect(config.database_url)
    try:
        with conn, conn.cursor(cursor_factory=RealDictCursor) as cur:
            yield cur
    finally:
        conn.close()


def snapshot(config, job_id):
    with transaction(config) as cur:
        cur.execute('''select j.id as job_id,j.document_id,j.document_version,j.source_sha256,
          j.payload_json as job_payload_json,d.sha256,d.raw_uri,d.file_size,d.original_filename,
          d.metadata_json as document_metadata_json,d.deleted_at,d.document_version as current_version
          from private.sci_parse_jobs j join public.kb_documents d on d.id=j.document_id
          where j.id=%s and j.status='running' ''',(job_id,))
        row=cur.fetchone()
    if (not row or row['deleted_at'] is not None or row['current_version']!=row['document_version']
            or row['source_sha256']!=row['sha256']):
        raise ValueError('SCI document deleted, version changed or source hash changed')
    for key in ('job_id','document_id'):
        row[key]=str(row[key])
    return SimpleNamespace(**row)


def claim_submission(config):
    with transaction(config) as cur:
        cur.execute('select * from pgmq.read(%s,%s,1)',(QUEUE,config.queue_vt_seconds))
        message=cur.fetchone()
        if not message:
            return None
        try:
            job_id=str(UUID(message['message']['job_id']))
        except (ValueError,KeyError,TypeError,AttributeError):
            cur.execute('select pgmq.archive(%s,%s)',(QUEUE,message['msg_id']))
            return None
        cur.execute('''select *,locked_until>clock_timestamp() as leased,
          next_retry_at>clock_timestamp() as waiting from private.sci_parse_jobs where id=%s for update''',(job_id,))
        row=cur.fetchone()
        if not row or row['status'] in ('succeeded','dead','cancelled') or row['phase']=='receive':
            cur.execute('select pgmq.archive(%s,%s)',(QUEUE,message['msg_id']))
            return None
        if row['pgmq_msg_id'] != message['msg_id']:
            cur.execute('select pgmq.archive(%s,%s)',(QUEUE,message['msg_id']))
            return None
        if row['leased'] or row['waiting']:
            return None
        # A process may have died between remote acceptance and task-id persistence.
        # Do not blindly submit the PDF again when that outcome is unknown.
        if row['status']=='running' and row['payload_json'].get('submission_started'):
            cur.execute("update private.sci_parse_jobs set status='dead',last_error='submission outcome unknown; inspect parser task',finished_at=now(),locked_by=null,locked_until=null,updated_at=now() where id=%s",(job_id,))
            cur.execute("update public.kb_documents set status='failed' where id=%s and document_version=%s and deleted_at is null",(row['document_id'],row['document_version']))
            cur.execute('select pgmq.archive(%s,%s)',(QUEUE,message['msg_id']))
            return None
        cur.execute('''update private.sci_parse_jobs set status='running',phase='submit',
          attempts=attempts+1,locked_by=%s,locked_until=now()+make_interval(secs=>%s),
          pgmq_msg_id=%s,updated_at=now() where id=%s''',
          (config.worker_id,config.lock_seconds,message['msg_id'],job_id))
        cur.execute("update public.kb_documents set status='parsing' where id=%s and document_version=%s and deleted_at is null",(row['document_id'],row['document_version']))
        return SimpleNamespace(job_id=job_id,msg_id=message['msg_id'])


def start_submission(config,job_id):
    with transaction(config) as cur:
        cur.execute('''update private.sci_parse_jobs set payload_json=payload_json || '{"submission_started":true}'::jsonb,
          updated_at=now() where id=%s and status='running' and phase='submit'
          and locked_by=%s and locked_until>clock_timestamp() returning id''',(job_id,config.worker_id))
        if not cur.fetchone():
            raise RuntimeError('SCI submission lease lost')


def record_submission(config,job_id,submitted):
    with transaction(config) as cur:
        cur.execute('select *,locked_until>clock_timestamp() as leased from private.sci_parse_jobs where id=%s for update',(job_id,))
        row=cur.fetchone()
        if row and row['phase']=='receive' and row['payload_json'].get('parser_task',{}).get('task_id')==submitted.task_id:
            return
        if not row or row['status']!='running' or row['locked_by']!=config.worker_id or not row['leased']:
            raise RuntimeError('SCI submission lease lost before task registration')
        task={'task_id':submitted.task_id,'task_url':submitted.task_url,'status_url':submitted.status_url}
        cur.execute('''update private.sci_parse_jobs set phase='receive',payload_json=payload_json || %s || jsonb_build_object('submitted_at',now()),
          locked_by=null,locked_until=null,updated_at=now() where id=%s''',(Json({'parser_task':task}),job_id))
        cur.execute('select pgmq.archive(%s,%s) as archived',(QUEUE,row['pgmq_msg_id']))
        if not cur.fetchone()['archived']:
            raise RuntimeError('SCI submission message archive failed')


def claim_result(config):
    with transaction(config) as cur:
        cur.execute('''select * from private.sci_parse_jobs where status='running' and phase='receive'
          and (locked_until is null or locked_until<=clock_timestamp())
          order by updated_at limit 1 for update skip locked''')
        row=cur.fetchone()
        if not row:
            return None
        cur.execute('''update private.sci_parse_jobs set locked_by=%s,
          locked_until=now()+make_interval(secs=>%s),updated_at=now() where id=%s''',
          (config.worker_id,config.lock_seconds,row['id']))
        return SimpleNamespace(job_id=str(row['id']),document_id=str(row['document_id']),
                               document_version=row['document_version'],
                               task_id=row['payload_json']['parser_task']['task_id'])


def defer_result(config,job_id,error=None):
    with transaction(config) as cur:
        cur.execute('''update private.sci_parse_jobs set locked_by=null,
          locked_until=now()+make_interval(secs=>%s),last_error=%s,updated_at=now()
          where id=%s and status='running' and phase='receive' and locked_by=%s''',
          (config.poll_interval_seconds,error[:2000] if error else None,job_id,config.worker_id))


def fail(config,job_id,error,retryable):
    with transaction(config) as cur:
        cur.execute('''select * from private.sci_parse_jobs where id=%s and status='running'
          and locked_by=%s and locked_until>clock_timestamp() for update''',(job_id,config.worker_id))
        row=cur.fetchone()
        if not row:
            return
        retry=retryable and row['attempts']<row['max_attempts']
        delay=min(300,15*row['attempts'])
        cur.execute('select pgmq.archive(%s,%s)',(QUEUE,row['pgmq_msg_id']))
        msg_id=None
        if retry:
            cur.execute('select pgmq.send(%s,%s,%s) as msg_id',(QUEUE,Json({'job_id':job_id,'pipeline':PIPELINE}),delay))
            msg_id=cur.fetchone()['msg_id']
        cur.execute('''update private.sci_parse_jobs set status=%s,phase='submit',locked_by=null,locked_until=null,
          next_retry_at=case when %s then now()+make_interval(secs=>%s) else null end,
          pgmq_msg_id=%s,payload_json=payload_json-'parser_task'-'submission_started',last_error=%s,
          finished_at=case when %s then null else now() end,updated_at=now() where id=%s''',
          ('queued' if retry else 'dead',retry,delay,msg_id,str(error)[:2000],retry,job_id))
        cur.execute('''update public.kb_documents set status=%s::public.kb_document_status,
          last_error=%s,last_error_stage='parse' where id=%s and document_version=%s and deleted_at is null''',
          ('parse_queued' if retry else 'failed',str(error)[:2000],row['document_id'],row['document_version']))


def parser_inflight(config):
    """Bound this pipeline's outstanding ordinary tasks without a two-stage queue API."""
    with transaction(config) as cur:
        cur.execute("select count(*) as n from private.sci_parse_jobs where status='running' and phase='receive'")
        return cur.fetchone()['n']
