"""Independent SCI processes using the existing local PDF parser service."""
import logging
import time
from datetime import datetime, timezone
from pathlib import Path

import psycopg2

from .parser_adapter import fetch_mineru_task_status, submit_mineru_task
from . import control, receiver
from .artifacts import digest

LOG=logging.getLogger(__name__)


class Worker:
    def __init__(self,config):
        self.config=config
        if config.lock_seconds <= max(config.two_stage_submit_timeout_seconds,
                                      config.two_stage_status_timeout_seconds)+30:
            raise ValueError('SCI lease must exceed parser HTTP timeout by at least30seconds')

    def run_forever(self):
        LOG.info('SCI %s online queue=%s worker=%s',type(self).__name__,control.QUEUE,self.config.worker_id)
        while True:
            processed = None
            try:
                processed = self.run_once()
            except Exception:
                LOG.exception('SCI worker iteration failed; retry after poll interval')
            time.sleep(self.next_poll_interval(processed))

    def next_poll_interval(self, processed):
        return self.config.poll_interval_seconds


class Submitter(Worker):
    def __init__(self, config):
        super().__init__(config)
        self._no_claim = False
        self._empty_polls = 0
        self._last_poll_interval = config.poll_interval_seconds

    def next_poll_interval(self, processed):
        # No eligible claim is distinct from parser backpressure or a failed query.
        if processed is False and self._no_claim:
            self._empty_polls = min(self._empty_polls + 1, 3)
            interval = 60 if self._empty_polls >= 3 else 30
        else:
            self._empty_polls = 0
            interval = self.config.poll_interval_seconds
        if interval != self._last_poll_interval:
            LOG.info('SCI submitter polling wait_seconds=%s consecutive_empty=%s',
                     interval, self._empty_polls)
            self._last_poll_interval = interval
        return interval

    def run_once(self):
        self._no_claim = False
        c=self.config
        if control.parser_inflight(c)>=c.two_stage_max_parse_backlog:
            return False
        claim=control.claim_submission(c)
        if claim is None:
            self._no_claim = True
            return False
        started=False
        submitted=None
        try:
            snap=control.snapshot(c,claim.job_id)
            receiver.validate_contract(snap)
            raw=Path(snap.raw_uri).resolve(strict=True)
            raw.relative_to(c.nas_raw_root.resolve())
            if not raw.is_file() or raw.suffix.lower()!='.pdf':
                raise ValueError('SCI raw URI must be an existing PDF within NAS_RAW_ROOT')
            if raw.stat().st_size!=snap.file_size or digest(raw)!=snap.sha256:
                raise ValueError('SCI raw PDF differs from registered source')
            control.start_submission(c,claim.job_id)
            started=True
            submitted=submit_mineru_task(raw,c.unstructure_serve_two_stage_base_url,
                c.unstructure_serve_bearer_token,timeout_seconds=c.two_stage_submit_timeout_seconds,
                priority=c.two_stage_priority)
            for attempt in range(3):
                try:
                    control.record_submission(c,claim.job_id,submitted)
                    break
                except (psycopg2.InterfaceError,psycopg2.OperationalError):
                    if attempt==2:
                        raise
                    time.sleep(2**attempt)
            LOG.info('SCI submitted document=%s job=%s task=%s',snap.document_id,claim.job_id,submitted.task_id)
        except Exception as exc:
            LOG.exception('SCI submission failed job=%s task=%s',claim.job_id,submitted.task_id if submitted else None)
            if submitted is not None:
                # The result may already be committed; retry read/registration on operator recovery.
                # Never resubmit when remote acceptance is known or uncertain.
                return True
            control.fail(c,claim.job_id,('submission outcome uncertain: ' if started else '')+str(exc),False)
        return True


class Finalizer(Worker):
    def expired(self, snapshot):
        submitted_at = snapshot.job_payload_json.get('submitted_at')
        return bool(submitted_at and (datetime.now(timezone.utc) -
            datetime.fromisoformat(submitted_at)).total_seconds() > self.config.parse_job_timeout_seconds)

    def run_once(self):
        c=self.config
        claim=control.claim_result(c)
        if claim is None:
            return False
        try:
            snap=control.snapshot(c,claim.job_id)
            receiver.validate_contract(snap)
            try:
                status=fetch_mineru_task_status(c.unstructure_serve_two_stage_base_url,
                    c.unstructure_serve_bearer_token,claim.task_id,
                    timeout_seconds=c.two_stage_status_timeout_seconds)
            except Exception as exc:
                if self.expired(snap):
                    control.fail(c,claim.job_id,'SCI parser status deadline exceeded: '+str(exc),False)
                else:
                    control.defer_result(c,claim.job_id,str(exc))
                return True
            if status.state in {'FAILURE','REVOKED'}:
                control.fail(c,claim.job_id,status.error or status.state,True)
            elif status.state=='SUCCESS' and status.parsed is not None:
                output=receiver.finalize(c,claim,snap,status.parsed)
                LOG.info('SCI parsed_local document=%s job=%s output=%s',snap.document_id,claim.job_id,output)
            else:
                if self.expired(snap):
                    control.fail(c,claim.job_id,'SCI parser result deadline exceeded',False)
                else:
                    control.defer_result(c,claim.job_id)
        except receiver.SCILeaseLost:
            LOG.warning('SCI receipt lease/task changed job=%s; no stale write',claim.job_id)
            # The owner predicate cannot release a replacement worker's claim.
            control.defer_result(c,claim.job_id,'SCI receipt lease/task changed')
        except (psycopg2.InterfaceError,psycopg2.OperationalError,OSError) as exc:
            # Keep the known parser task, not a second PDF submission.
            LOG.exception('SCI receipt temporarily unavailable job=%s',claim.job_id)
            control.defer_result(c,claim.job_id,str(exc))
        except Exception as exc:
            LOG.exception('SCI result rejected job=%s',claim.job_id)
            control.fail(c,claim.job_id,str(exc),False)
        return True
