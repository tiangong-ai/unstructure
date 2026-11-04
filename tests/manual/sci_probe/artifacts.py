"""Compatibility wrapper for the bounded manual probe."""
from src.sci_parse_worker.artifacts import (
    PIPELINE, digest, document_path, publish as publish_artifacts, reject_vectors, verify_run,
)
from src.sci_parse_worker.parser_adapter import ParsedDocument


def publish(root, document_id, run_id, source_hash, parsed, parser, **kwargs):
    if not isinstance(parsed, ParsedDocument):
        parsed = ParsedDocument(parsed.result, {
            "state": "SUCCESS", "result": {"result": parsed.result, "txt": parsed.txt},
        })
    return publish_artifacts(root, document_id, run_id, source_hash, parsed, parser, **kwargs)
