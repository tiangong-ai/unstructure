"""Exercise the real SCI loop without sleeping, contacting services or claiming jobs."""
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import Mock, patch

from src.sci_parse_worker.artifacts import digest
from src.sci_parse_worker.worker import Finalizer, Submitter


class StopLoop(BaseException):
    pass


class PollingTests(unittest.TestCase):
    def config(self, root):
        return SimpleNamespace(
            worker_id='test', lock_seconds=1800, poll_interval_seconds=5,
            two_stage_submit_timeout_seconds=120, two_stage_status_timeout_seconds=30,
            unstructure_serve_two_stage_base_url='http://parser.invalid',
            unstructure_serve_bearer_token='test', two_stage_queue_status_timeout_seconds=10,
            two_stage_parse_queue_name='parse', two_stage_max_parse_backlog=5,
            nas_raw_root=root, two_stage_priority='normal', two_stage_provider=None,
            two_stage_model=None, two_stage_prompt=None,
        )

    def submitter_waits(self, outcomes):
        waits = []
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            pdf = root / 'input.pdf'
            pdf.write_bytes(b'%PDF-test')
            events = iter(outcomes)
            current = None

            def backlog(*args, **kwargs):
                nonlocal current
                current = next(events)
                if current == 'parser-error':
                    raise RuntimeError('parser unavailable')
                return 5 if current == 'busy' else 0

            def claim(*args):
                if current == 'db-error':
                    raise RuntimeError('database unavailable')
                return SimpleNamespace(job_id='job') if current == 'work' else None

            def sleep(seconds):
                waits.append(seconds)
                if len(waits) == len(outcomes):
                    raise StopLoop()

            with patch('src.sci_parse_worker.worker.control') as ctl, \
                 patch('src.sci_parse_worker.worker.receiver.validate_contract'), \
                 patch('src.sci_parse_worker.worker.control.parser_inflight', side_effect=backlog), \
                 patch('src.sci_parse_worker.worker.submit_mineru_task') as submit, \
                 patch('src.sci_parse_worker.worker.time.sleep', side_effect=sleep):
                ctl.claim_submission.side_effect = claim
                ctl.snapshot.return_value = SimpleNamespace(
                    raw_uri=str(pdf), file_size=pdf.stat().st_size,
                    sha256=digest(pdf), document_id='document')
                submit.return_value = SimpleNamespace(task_id='task')
                with self.assertRaises(StopLoop):
                    Submitter(self.config(root)).run_forever()
                self.assertEqual(ctl.claim_submission.call_count,
                                 sum(x not in ('busy', 'parser-error') for x in outcomes))
                self.assertEqual(submit.call_count, outcomes.count('work'))
                ctl.fail.assert_not_called()
        return waits

    def test_empty_queue_backs_off_and_caps_at_sixty(self):
        self.assertEqual(self.submitter_waits(['empty'] * 5), [30, 30, 60, 60, 60])

    def test_claimed_work_resets_the_idle_streak(self):
        self.assertEqual(self.submitter_waits(['empty'] * 3 + ['work', 'empty', 'empty', 'empty']),
                         [30, 30, 60, 5, 30, 30, 60])

    def test_capacity_limit_does_not_count_as_empty(self):
        self.assertEqual(self.submitter_waits(['empty', 'empty', 'busy', 'empty']),
                         [30, 30, 5, 30])

    def test_errors_do_not_inherit_idle_delay(self):
        for error in ('parser-error', 'db-error'):
            with self.subTest(error=error):
                self.assertEqual(self.submitter_waits(['empty'] * 3 + [error, 'empty']),
                                 [30, 30, 60, 5, 30])

    def test_finalizer_keeps_five_second_interval(self):
        worker = Finalizer(self.config(Path('/unused')))
        worker.run_once = Mock(side_effect=[False, True, RuntimeError('temporary')])
        waits = []

        def sleep(seconds):
            waits.append(seconds)
            if len(waits) == 3:
                raise StopLoop()

        with patch('src.sci_parse_worker.worker.time.sleep', side_effect=sleep):
            with self.assertRaises(StopLoop):
                worker.run_forever()
        self.assertEqual(waits, [5, 5, 5])


if __name__ == '__main__':
    unittest.main()
