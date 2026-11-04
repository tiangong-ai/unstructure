from __future__ import annotations

import tempfile
import unittest
from dataclasses import replace
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import MagicMock, patch

import psycopg2

from src.sci_parse_worker import receiver
from src.sci_parse_worker.artifacts import PIPELINE, digest, promote, publish, verify_run
from tests.sci_helpers import ParsedDocument
from src.sci_parse_worker.worker import Finalizer
from tests.test_kb_parse_worker_artifacts import snapshot
from tests.test_kb_parse_worker_reliability import NoopLease, worker_config

DOC = '2e03c71a-0429-4706-990e-44af753d2c1b'
JOB = '38a40cb8-e19a-43bf-9f15-8ed4fc0bf066'


class ReceiverTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.config = worker_config()
        self.config.nas_raw_root = self.root
        self.config.unstructure_serve_two_stage_base_url = 'http://parser'
        self.config.unstructure_serve_bearer_token = 'test'
        self.config.two_stage_status_timeout_seconds = 30
        self.config.two_stage_submit_timeout_seconds=120
        self.config.lock_seconds=1800
        self.config.parser_profile = 'test'
        self.config.parser_version = 'test'
        self.meta = {'pipeline': PIPELINE, 'source_table': 'journals', 'journal_id': DOC}
        self.payload = {'pipeline': PIPELINE, 'source_sha256': snapshot().sha256,
                        'parser_task': {'task_id': 'celery-1'}}
        self.snapshot = replace(snapshot(), job_id=JOB, document_id=DOC,
                                document_metadata_json=self.meta, job_payload_json=self.payload)
        self.claim = SimpleNamespace(job_id=JOB, document_id=DOC, document_version=1, task_id='celery-1')
        self.parsed = ParsedDocument([{'text': 'first page', 'page_number': 1}], 'first page', 1, 0)
        self.staged = publish(self.root / 'stage', DOC, JOB, self.snapshot.sha256,
                              self.parsed, {}, document_version=1)
        self.destination = self.root / 'processed' / 'sci' / DOC
        self.row = dict(document_id=DOC,job_version=1,document_version=1,stage='parse',
                        status='running',phase='receive',source_sha256=self.snapshot.sha256,locked_by=self.config.worker_id,lease_valid=True,
                        payload_json=self.payload,metadata_json=self.meta,sha256=self.snapshot.sha256,
                        deleted_at=None,document_status="parsing",processed_manifest_local_uri=None,processed_manifest_hash=None)

    def connection(self, row=None, journal=True, update=True):
        conn = MagicMock()
        cur = conn.cursor.return_value.__enter__.return_value
        cur.fetchone.side_effect = [row or self.row, {'id':DOC} if journal else None,
                                    {'id':JOB} if update else None]
        return conn, cur

    def test_guarded_receipt_has_no_sci_write_or_queue_handoff(self):
        conn, cur = self.connection()
        receiver.receive(conn,self.config,self.claim,self.snapshot,self.staged)
        verify_run(self.destination,DOC,JOB,self.snapshot.sha256)
        sql = ' '.join(call.args[0] for call in cur.execute.call_args_list).lower()
        self.assertIn("status='parsed_local'",sql)
        self.assertIn('for update of j,d',sql)
        for forbidden in ['update public.journals','pgmq.','embedding_time','fulltext_time','chunk_count=']:
            self.assertNotIn(forbidden,sql)

    def test_stale_claim_never_publishes(self):
        for changed in [dict(job_version=2),dict(document_version=2),dict(locked_by='other'),
                        dict(lease_valid=False),dict(deleted_at='deleted'),dict(sha256='changed'),
                        dict(payload_json={'pipeline':PIPELINE,'parser_task':{'task_id':'new-task'}})]:
            row = {**self.row, **changed}
            conn,_ = self.connection(row)
            with self.assertRaises(receiver.SCILeaseLost):
                receiver.receive(conn,self.config,self.claim,self.snapshot,self.staged)
            self.assertFalse(self.destination.exists())

    def test_missing_journal_and_missing_contract_never_publish(self):
        conn,_ = self.connection(journal=False)
        with self.assertRaises(receiver.SCIContractError):
            receiver.receive(conn,self.config,self.claim,self.snapshot,self.staged)
        for bad in [{}, {'source_table':'journals'}, {**self.meta,'journal_id':JOB}]:
            with self.assertRaises(receiver.SCIContractError):
                receiver.validate_contract(replace(self.snapshot,document_metadata_json=bad))
        with self.assertRaises(receiver.SCIContractError):
            receiver.validate_contract(replace(self.snapshot,job_payload_json={**self.payload,'source_sha256':'other'}))
        self.assertFalse(self.destination.exists())

    def test_receipt_replay_is_idempotent_and_rejects_mismatch(self):
        promote(self.staged,self.destination)
        row = {**self.row,'status':'succeeded','document_status':'parsed_local','locked_by':None,'lease_valid':False,
               'processed_manifest_local_uri':str(self.destination/'manifest.json'),
               'processed_manifest_hash':digest(self.staged/'manifest.json')}
        conn,cur = self.connection(row)
        with patch.object(receiver,'promote') as write:
            receiver.receive(conn,self.config,self.claim,self.snapshot,self.staged)
            write.assert_not_called()
        self.assertFalse(any('update public.' in c.args[0] for c in cur.execute.call_args_list))
        regenerated = publish(self.root/'regenerated',DOC,JOB,self.snapshot.sha256,
                              self.parsed,{},document_version=1)
        receiver.receive(self.connection(row)[0],self.config,self.claim,self.snapshot,regenerated)
        row['processed_manifest_hash']='wrong'
        with self.assertRaises(receiver.SCILeaseLost):
            receiver.receive(self.connection(row)[0],self.config,self.claim,self.snapshot,self.staged)

    def test_replacement_and_interrupted_publication_recover_fixed_names(self):
        promote(self.staged,self.destination)
        new = publish(self.root/'new',DOC,JOB,self.snapshot.sha256,
                      ParsedDocument([{'text':'new text'}],'new text',1,0),{},document_version=1)
        import os
        original = os.replace
        def interrupt(src,dst):
            if Path(dst).name==f'{DOC}.txt':
                raise OSError('disk full')
            return original(src,dst)
        with patch('src.sci_parse_worker.artifacts.os.replace',side_effect=interrupt):
            with self.assertRaises(OSError):
                promote(new,self.destination)
        self.assertFalse((self.destination/'manifest.json').exists())
        # A hard-killed previous process can leave temporary directories.
        (self.destination.parent/'.publish-abandoned').mkdir()
        promote(new,self.destination)
        verify_run(self.destination,DOC,JOB,self.snapshot.sha256)
        self.assertEqual((self.destination/f'{DOC}.txt').read_text(),'new text')
        self.assertEqual({p.name for p in self.destination.iterdir()},
                         {f'{DOC}.pkl',f'{DOC}.txt',f'{DOC}.json','manifest.json'})

    def test_legacy_names_retired_before_new_manifest_and_replay_recovers(self):
        import os
        self.destination.mkdir(parents=True)
        for name in ('parsed.pkl', 'fulltext.txt', 'manifest.json'):
            (self.destination/name).write_text('previous output')
        original = os.replace

        def interrupt(src, dst):
            if Path(dst).name == f'{DOC}.txt':
                raise OSError('interrupted replacement')
            return original(src, dst)

        with patch('src.sci_parse_worker.artifacts.os.replace', side_effect=interrupt):
            with self.assertRaises(OSError):
                promote(self.staged, self.destination)
        self.assertFalse((self.destination/'manifest.json').exists())
        self.assertTrue((self.destination/'parsed.pkl').exists())

        def check_manifest_last(src, dst):
            if Path(dst).name == 'manifest.json':
                self.assertFalse((self.destination/'parsed.pkl').exists())
                self.assertFalse((self.destination/'fulltext.txt').exists())
                self.assertTrue((self.destination/f'{DOC}.pkl').exists())
                self.assertTrue((self.destination/f'{DOC}.txt').exists())
            return original(src, dst)

        with patch('src.sci_parse_worker.artifacts.os.replace', side_effect=check_manifest_last):
            promote(self.staged, self.destination)
        verify_run(self.destination, DOC, JOB, self.snapshot.sha256)
        self.assertEqual({p.name for p in self.destination.iterdir()},
                         {f'{DOC}.pkl', f'{DOC}.txt', f'{DOC}.json', 'manifest.json'})

    def test_wrong_document_or_unrelated_entries_preserve_existing_manifest(self):
        other = self.destination.with_name(JOB)
        with self.assertRaisesRegex(ValueError, 'destination must match'):
            promote(self.staged, other)
        self.assertFalse(other.exists())
        promote(self.staged, self.destination)
        before = (self.destination/'manifest.json').read_bytes()
        (self.destination/f'{JOB}.pkl').write_text('unrelated file')
        with self.assertRaisesRegex(ValueError, 'unexpected entries'):
            promote(self.staged, self.destination)
        self.assertEqual((self.destination/'manifest.json').read_bytes(), before)

    def test_sci_finalizer_only_uses_independent_claim_and_receipt(self):
        with patch('src.sci_parse_worker.control.claim_result',return_value=self.claim), \
             patch('src.sci_parse_worker.control.snapshot',return_value=self.snapshot), \
             patch('src.sci_parse_worker.worker.fetch_mineru_task_status',return_value=SimpleNamespace(state='SUCCESS',parsed=self.parsed)), \
             patch.object(receiver,'finalize',return_value=self.destination) as receive, \
             patch('src.sci_parse_worker.control.fail') as fail:
            self.assertTrue(Finalizer(self.config).run_once())
            receive.assert_called_once()
            fail.assert_not_called()

    def test_db_outage_does_not_requeue_pdf_parsing(self):
        with patch('src.sci_parse_worker.control.claim_result',return_value=self.claim), \
             patch('src.sci_parse_worker.control.snapshot',return_value=self.snapshot), \
             patch('src.sci_parse_worker.worker.fetch_mineru_task_status',return_value=SimpleNamespace(state='SUCCESS',parsed=self.parsed)), \
             patch.object(receiver,'finalize',side_effect=psycopg2.OperationalError('offline')), \
             patch('src.sci_parse_worker.control.defer_result') as defer, \
             patch('src.sci_parse_worker.control.fail') as fail:
            Finalizer(self.config).run_once()
            defer.assert_called_once()
            fail.assert_not_called()

    def test_reconnect_retries_use_same_staged_manifest(self):
        conn=MagicMock()
        hashes=[]
        def receive(*args):
            hashes.append(digest(args[-1]/'manifest.json'))
            if len(hashes)==1:
                raise psycopg2.OperationalError('lost commit response')
            return self.destination
        with patch.object(receiver.control_plane,'connect',return_value=conn), \
             patch.object(receiver,'receive',side_effect=receive),patch.object(receiver.time,'sleep'):
            self.assertEqual(receiver.finalize(self.config,self.claim,self.snapshot,self.parsed),self.destination)
        self.assertEqual(len(hashes),2)
        self.assertEqual(hashes[0],hashes[1])
        self.assertEqual(conn.close.call_count,2)


if __name__ == '__main__':
    unittest.main()
