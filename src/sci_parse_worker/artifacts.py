"""Publish the current parsing output directly under the SCI document UUID."""

from __future__ import annotations

import hashlib
import json
import os
import pickle
import shutil
import tempfile
from datetime import datetime, timezone
from pathlib import Path
from uuid import UUID

from .parser_adapter import ParsedDocument
from .cleaning import clean_chunks, plain_text, RULE_VERSION

PIPELINE = "sci-ingestion-contract-v1"


def digest(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            h.update(block)
    return h.hexdigest()


def document_path(root: Path, document_id: str) -> Path:
    return root / str(UUID(document_id))


def artifact_names(document_id: str) -> tuple[str, str, str]:
    """Use the existing SCI document identity, never the processing job UUID."""
    stem = str(UUID(document_id))
    return f"{stem}.pkl", f"{stem}.txt", f"{stem}.json"


def reject_vectors(value: object) -> None:
    if isinstance(value, dict):
        for key, item in value.items():
            if str(key).lower() in {"embedding", "embeddings", "vector", "vectors"}:
                raise ValueError("SCI parsed result contains vector fields")
            reject_vectors(item)
    elif isinstance(value, list):
        for item in value:
            reject_vectors(item)


def verify_run(directory: Path, document_id: str, run_id: str, source_hash: str) -> dict:
    manifest = json.loads((directory / "manifest.json").read_text())
    expected = {"pipeline": PIPELINE, "schema_version": 2, "stage": "parsed",
                "document_id": document_id, "run_id": run_id, "source_sha256": source_hash}
    if any(manifest.get(k) != v for k, v in expected.items()):
        raise ValueError("manifest identity mismatch")
    if manifest.get('cleaning', {}).get('rule_version') != RULE_VERSION:
        raise ValueError('SCI cleaning rule version mismatch')
    files = manifest.get("files", {})
    if set(files) != set(artifact_names(document_id)):
        raise ValueError("manifest must describe the SCI document UUID .pkl, .txt and .json files")
    for name, info in files.items():
        path = directory / name
        if path.is_symlink() or not path.is_file():
            raise ValueError("artifact missing or symlinked")
        if path.stat().st_size != info["size_bytes"] or digest(path) != info["sha256"]:
            raise ValueError("artifact checksum mismatch")
    if not isinstance(manifest.get("parsed_item_count"), int) or manifest["parsed_item_count"] < 1:
        raise ValueError("empty manifest item count")
    return manifest


def publish(root: Path, document_id: str, run_id: str, source_hash: str,
            parsed: ParsedDocument, parser: dict, *, document_version: int | None = None) -> Path:
    destination = document_path(root, document_id)
    if destination.exists():
        raise FileExistsError("run already exists; verify and finalize instead of overwriting")
    raw = parsed.raw_response
    if raw.get('state') != 'SUCCESS' or raw.get('result', {}).get('result') != parsed.result:
        raise ValueError('raw parser response differs from parsed chunks')
    chunks = parsed.result
    if not chunks or any(not isinstance(x, dict) or not isinstance(x.get('text'), str) for x in chunks):
        raise ValueError('parser must return valid chunks')
    reject_vectors(chunks)
    cleaned, cleaning = clean_chunks(chunks)
    fulltext = plain_text(chunks)
    if not cleaned or not fulltext.strip():
        raise ValueError('cleaned chunks and full text must be nonempty')
    pkl_name, txt_name, json_name = artifact_names(document_id)
    destination.parent.mkdir(parents=True, exist_ok=True)
    staging = Path(tempfile.mkdtemp(prefix=".staging-", dir=destination.parent))
    try:
        with (staging / json_name).open('w', encoding='utf-8') as stream:
            json.dump(raw, stream, ensure_ascii=False, indent=2)
            stream.flush()
            os.fsync(stream.fileno())
        with (staging / pkl_name).open("wb") as stream:
            pickle.dump(cleaned, stream, protocol=4)
            stream.flush()
            os.fsync(stream.fileno())
        with (staging / txt_name).open("w", encoding="utf-8") as stream:
            stream.write(fulltext)
            stream.flush()
            os.fsync(stream.fileno())
        manifest = {
            "schema_version": 2, "pipeline": PIPELINE, "stage": "parsed",
            "document_id": document_id, "run_id": run_id, "source_sha256": source_hash,
            "created_at": datetime.now(timezone.utc).isoformat(), "parser": parser,
            "parsed_item_count": len(cleaned),
            "raw_item_count": len(chunks), "cleaning": cleaning,
            "dropped_empty_text_count": cleaning["empty_chunks"],
            "files": {name: {"size_bytes": (staging / name).stat().st_size,
                              "sha256": digest(staging / name)}
                      for name in (pkl_name, txt_name, json_name)},
        }
        if document_version is not None:
            manifest["document_version"] = document_version
            manifest["receipt_required"] = "kb_documents.processed_manifest_hash"
        with (staging / "manifest.json").open("w", encoding="utf-8") as stream:
            json.dump(manifest, stream, ensure_ascii=False, indent=2)
            stream.write("\n")
            stream.flush()
            os.fsync(stream.fileno())
        verify_run(staging, document_id, run_id, source_hash)
        staging.rename(destination)
        return destination
    finally:
        if staging.exists():
            shutil.rmtree(staging)


def promote(staged: Path, destination: Path) -> None:
    """Publish document UUID names under the caller's document/job DB locks.

    Filesystem and DB commit are not atomic. No manifest means not ready; an
    interrupted replacement is repaired by replaying this job. Cloud readers
    must validate all sizes/hashes and re-read the manifest before using files.
    """
    if destination.is_symlink():
        raise ValueError("SCI destination cannot be a symlink")
    manifest = json.loads((staged / "manifest.json").read_text())
    document_id = manifest["document_id"]
    if destination.name != str(UUID(document_id)):
        raise ValueError("SCI destination must match the manifest document UUID")
    verify_run(staged, document_id, manifest["run_id"], manifest["source_sha256"])
    names = artifact_names(document_id)
    legacy_names = ("parsed.pkl", "fulltext.txt")
    destination.mkdir(parents=True, exist_ok=True)
    if any(p.name not in {*names, *legacy_names, "manifest.json"}
           or p.is_symlink() or not p.is_file() for p in destination.iterdir()):
        raise ValueError("SCI destination has unexpected entries; inspect before replacing")
    (destination / "manifest.json").unlink(missing_ok=True)
    # Staging is on the same filesystem: hard links avoid copying large files
    # while the DB locks block heartbeats. The staged bytes remain for retries.
    with tempfile.TemporaryDirectory(prefix=".publish-", dir=destination.parent) as scratch:
        for name in names:
            temp = Path(scratch) / name
            os.link(staged / name, temp)
            os.replace(temp, destination / name)
        # A retry may see a mix of old/new names after a crash. Retire only the
        # two known SCI aliases, before making the new manifest visible.
        for name in legacy_names:
            (destination / name).unlink(missing_ok=True)
        temp = Path(scratch) / "manifest.json"
        os.link(staged / "manifest.json", temp)
        os.replace(temp, destination / "manifest.json")
