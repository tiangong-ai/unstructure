import unittest
from datetime import datetime,timedelta,timezone
from types import SimpleNamespace
from unittest.mock import patch,Mock
from src.sci_parse_worker.worker import Finalizer
from src.sci_parse_worker.receiver import SCILeaseLost


class FinalizerRecoveryTests(unittest.TestCase):
    def run_worker(self, age, *, status_error=None, receipt_error=None):
        config=SimpleNamespace(lock_seconds=1800,two_stage_submit_timeout_seconds=120,
            two_stage_status_timeout_seconds=30,parse_job_timeout_seconds=7200,
            unstructure_serve_two_stage_base_url='http://parser.invalid',
            unstructure_serve_bearer_token='test')
        claim=SimpleNamespace(job_id='job',task_id='task')
        snapshot=SimpleNamespace(job_payload_json={'submitted_at':
            (datetime.now(timezone.utc)-timedelta(seconds=age)).isoformat()})
        with patch('src.sci_parse_worker.worker.control') as ctl, \
             patch('src.sci_parse_worker.worker.receiver') as receiver, \
             patch('src.sci_parse_worker.worker.fetch_mineru_task_status') as fetch:
            ctl.claim_result.return_value=claim;ctl.snapshot.return_value=snapshot
            receiver.SCILeaseLost=SCILeaseLost
            fetch.side_effect=status_error
            fetch.return_value=SimpleNamespace(state='SUCCESS',parsed=object())
            receiver.finalize.side_effect=receipt_error
            Finalizer(config).run_once()
            return ctl

    def test_unavailable_parser_eventually_reaches_terminal_state(self):
        ctl=self.run_worker(7201,status_error=RuntimeError('404 task absent'))
        ctl.fail.assert_called_once()
        self.assertFalse(ctl.fail.call_args.args[-1])
        self.assertIn('deadline',ctl.fail.call_args.args[2])
        ctl.defer_result.assert_not_called()

    def test_temporary_status_error_retains_parser_task(self):
        ctl=self.run_worker(5,status_error=RuntimeError('temporary 503'))
        ctl.defer_result.assert_called_once();ctl.fail.assert_not_called()

    def test_receipt_lease_change_uses_guarded_release(self):
        ctl=self.run_worker(5,receipt_error=SCILeaseLost('changed'))
        ctl.defer_result.assert_called_once();ctl.fail.assert_not_called()


if __name__=='__main__': unittest.main()
