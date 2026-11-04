"""SCI ordinary MinerU HTTP contract; preserve the successful response before cleaning."""
from copy import deepcopy
from dataclasses import dataclass
from pathlib import Path
from types import SimpleNamespace
from uuid import UUID
import requests

from src.kb_parse_worker.parser_adapter import ParserError, TwoStageTaskSubmission


@dataclass(frozen=True)
class ParsedDocument:
    result: list
    raw_response: dict


def submit_mineru_task(raw_path: Path, base_url: str, bearer_token: str, *,
                       timeout_seconds: int, priority: str = 'normal'):
    url = f"{base_url.rstrip('/')}/mineru/task"
    with raw_path.open('rb') as stream:
        response = requests.post(url, files={'file': (raw_path.name, stream)},
            params={'chunk_type': 'true', 'return_txt': 'true'},
            data={'tier': 'advanced', 'priority': priority},
            headers={'Authorization': f'Bearer {bearer_token}'}, timeout=timeout_seconds)
    response.raise_for_status()
    payload = response.json()
    task_id = payload.get('task_id')
    if not isinstance(task_id, str) or not task_id:
        raise ParserError('parser response missing task_id')
    UUID(task_id)
    return TwoStageTaskSubmission(task_id, payload.get('state'), url, f'{url}/{task_id}')


def fetch_mineru_task_status(base_url: str, bearer_token: str, task_id: str, *,
                            timeout_seconds: int):
    UUID(task_id)
    response = requests.get(f"{base_url.rstrip('/')}/mineru/task/{task_id}",
        headers={'Authorization': f'Bearer {bearer_token}'}, timeout=timeout_seconds)
    # Ordinary API uses HTTP500 for a terminal job failure; recognize only a
    # well-formed response for this exact task, never a generic server error.
    if response.status_code == 500:
        try:
            failed = response.json()
        except ValueError:
            response.raise_for_status()
        if isinstance(failed, dict) and failed.get('task_id') == task_id and failed.get('state') in {'FAILURE', 'REVOKED'}:
            return SimpleNamespace(state=failed['state'], error=failed.get('error'), parsed=None)
    response.raise_for_status()
    payload = response.json()
    if payload.get('task_id') != task_id or not isinstance(payload.get('state'), str):
        raise ParserError('parser task identity/state mismatch')
    state = payload['state']
    if state != 'SUCCESS':
        return SimpleNamespace(state=state, error=payload.get('error'), parsed=None)
    data = payload.get('result')
    if not isinstance(data, dict) or not isinstance(data.get('result'), list):
        raise ParserError('parser response missing chunk list')
    chunks = data['result']
    if not chunks or any(not isinstance(x, dict) or not isinstance(x.get('text'), str)
                         for x in chunks):
        raise ParserError('invalid/empty parser chunks')
    # Do not drop empty chunks or otherwise mutate the archived JSON.
    parsed = ParsedDocument(deepcopy(chunks), deepcopy(payload))
    return SimpleNamespace(state=state, parsed=parsed, error=None)
