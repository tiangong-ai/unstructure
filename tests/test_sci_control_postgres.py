"""SCI lifecycle integration tests with real PGMQ on an owned disposable DB."""
import os
import unittest
from types import SimpleNamespace
from src.sci_parse_worker import control,receiver
from tests import test_sci_finalizer_postgres as receipt_tests
from tests.test_sci_finalizer import DOC,JOB


@unittest.skipUnless(os.getenv('SCI_TEST_DATABASE_URL'),'requires disposable PostgreSQL with PGMQ')
class ControlTests(receipt_tests.PostgresReceiptTests):
    def setUp(self):
        super().setUp()
        self.fixture.config.database_url=os.environ['SCI_TEST_DATABASE_URL']
        self.fixture.config.lock_seconds=1800
        self.fixture.config.queue_vt_seconds=1800
        self.fixture.config.poll_interval_seconds=5
        with self.conn,self.conn.cursor() as cur:
            cur.execute('create extension if not exists pgmq')
            cur.execute("do $$ begin create type public.kb_document_status as enum ('parse_queued','parsing','failed'); exception when duplicate_object then null; end $$")
            cur.execute("select pgmq.create(%s)",(control.QUEUE,))
            cur.execute("select pgmq.create('kb_parse_queue')")
            cur.execute("select pgmq.send('kb_parse_queue','{\"legacy_sentinel\":true}')")
            cur.execute('alter table kb_documents add column raw_uri text,add column file_size bigint,add column original_filename text')
            cur.execute('''alter table private.sci_parse_jobs add column attempts int default 0,
                add column max_attempts int default 3,add column pgmq_msg_id bigint,
                add column created_at timestamptz default now()''')
            cur.execute("update private.sci_parse_jobs set phase='submit',status='queued',locked_by=null,locked_until=null,payload_json=payload_json-'parser_task'")
            cur.execute('select pgmq.send(%s,%s::jsonb)',(control.QUEUE,'{"job_id":"'+JOB+'"}'))
            msg=cur.fetchone()[0]
            cur.execute('update private.sci_parse_jobs set pgmq_msg_id=%s',(msg,))
        self.addCleanup(self.clean_queues)

    def clean_queues(self):
        self.conn.rollback()
        with self.conn,self.conn.cursor() as cur:
            cur.execute("select pgmq.metrics('kb_parse_queue')")
            # No SCI operation may read/archive the legacy sentinel.
            cur.execute("select read_ct from pgmq.q_kb_parse_queue")
            self.assertEqual(cur.fetchall(),[(0,)])
            cur.execute('select pgmq.drop_queue(%s)',(control.QUEUE,))
            cur.execute("select pgmq.drop_queue('kb_parse_queue')")

    def submit(self):
        claim=control.claim_submission(self.fixture.config)
        self.assertEqual(claim.job_id,JOB)
        control.start_submission(self.fixture.config,JOB)
        control.record_submission(self.fixture.config,JOB,
            SimpleNamespace(task_id='celery-1',task_url='http://parser/task',status_url='http://parser/task/celery-1'))
        return control.claim_result(self.fixture.config)

    def test_separate_submission_receipt_and_duplicate_message(self):
        claim=self.submit()
        snap=control.snapshot(self.fixture.config,JOB)
        receiver.receive(self.conn,self.fixture.config,claim,snap,self.fixture.staged)
        self.assertEqual(self.state()[0],('succeeded',None))
        with self.conn,self.conn.cursor() as cur:
            cur.execute('select pgmq.send(%s,%s::jsonb)',(control.QUEUE,'{"job_id":"'+JOB+'"}'))
        self.assertIsNone(control.claim_submission(self.fixture.config))
        self.assertIsNone(control.claim_result(self.fixture.config))

    def test_retry_only_goes_to_sci_queue(self):
        self.submit()
        control.fail(self.fixture.config,JOB,'parser failed',True)
        with self.conn,self.conn.cursor() as cur:
            cur.execute('select status,phase,attempts,locked_by,payload_json from private.sci_parse_jobs')
            row=cur.fetchone()
            self.assertEqual(row[:4],('queued','submit',1,None))
            self.assertNotIn('parser_task',row[4])
            cur.execute('select count(*) from pgmq.q_kb_sci_parse_queue')
            self.assertEqual(cur.fetchone()[0],1)

    def test_stale_duplicate_cannot_kill_current_submission(self):
        claim=control.claim_submission(self.fixture.config)
        control.start_submission(self.fixture.config,JOB)
        with self.conn,self.conn.cursor() as cur:
            cur.execute("update private.sci_parse_jobs set locked_until=now()-interval '1 second'")
            cur.execute('select pgmq.send(%s,%s::jsonb)',(control.QUEUE,'{"job_id":"'+JOB+'"}'))
        self.assertIsNone(control.claim_submission(self.fixture.config))
        self.assertEqual(self.state()[0][0],'running')

    def test_defer_cannot_release_another_worker_claim(self):
        self.submit()
        with self.conn,self.conn.cursor() as cur:
            cur.execute("update private.sci_parse_jobs set locked_by='replacement'")
        control.defer_result(self.fixture.config,JOB,'old worker completed')
        self.assertEqual(self.state()[0],('running','replacement'))

    def test_uncertain_submission_is_not_submitted_again(self):
        claim=control.claim_submission(self.fixture.config)
        control.start_submission(self.fixture.config,JOB)
        with self.conn,self.conn.cursor() as cur:
            cur.execute("update private.sci_parse_jobs set locked_until=now()-interval '1 second'")
            cur.execute('select pgmq.set_vt(%s,%s,0)',(control.QUEUE,claim.msg_id))
        self.assertIsNone(control.claim_submission(self.fixture.config))
        self.assertEqual(self.state()[0],('dead',None))

    # Parent tests expect an already-receivable job; retain those independent
    # receipt cases in test_sci_finalizer_postgres instead of inheriting them.
    test_real_sql_receipt_replay_and_sci_unchanged=None
    test_expired_or_replaced_task_does_not_write=None
    test_receipt_failure_rolls_back_db_and_replay_repairs=None
