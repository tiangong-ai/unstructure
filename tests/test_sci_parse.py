from __future__ import annotations

import json
import pickle
import tempfile
import unittest
from dataclasses import replace
from types import SimpleNamespace
from pathlib import Path
from unittest.mock import patch, MagicMock

from tests.sci_helpers import ParsedDocument
from tests.manual.sci_probe.artifacts import digest, publish, verify_run, document_path
from tests.manual.sci_probe.cli import source_path, prior_artifacts, main
from src.kb_parse_worker.queue import QueueMessage
from tests.test_kb_parse_worker_artifacts import snapshot
from tests.test_kb_parse_worker_reliability import NoopLease

DOC = "2e03c71a-0429-4706-990e-44af753d2c1b"
RUN = "38a40cb8-e19a-43bf-9f15-8ed4fc0bf066"
OWNER = "98f3b1ea-118b-4b04-8fc8-d7cee502bb5e"


class ArtifactTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.parsed = ParsedDocument([{"text":"A title", "page_number":1,"type":"title"},
                                      {"text":"正文", "page_number":2}], "A title\n正文", 2, 0)

    def test_roundtrip_and_tampering(self):
        p = publish(self.root, DOC, RUN, "source", self.parsed, {"version":"test"})
        self.assertEqual(p, self.root / DOC)
        self.assertEqual(set(x.name for x in p.iterdir()), {f"{DOC}.pkl",f"{DOC}.txt",f"{DOC}.json","manifest.json"})
        self.assertEqual(pickle.loads((p/f"{DOC}.pkl").read_bytes()), self.parsed.result)
        self.assertEqual((p/f"{DOC}.txt").read_text(), "\n\n".join(x["text"] for x in self.parsed.result))
        manifest = verify_run(p,DOC,RUN,"source")
        self.assertEqual(manifest["parsed_item_count"], 2)
        self.assertEqual(set(manifest["files"]), {f"{DOC}.pkl", f"{DOC}.txt", f"{DOC}.json"})
        with self.assertRaises(ValueError):
            verify_run(p,DOC,RUN,"another source")
        (p/f"{DOC}.txt").write_text("tampered")
        with self.assertRaises(ValueError):
            verify_run(p,DOC,RUN,"source")

    def test_manifest_rejects_job_uuid_filenames(self):
        p = publish(self.root, DOC, RUN, "source", self.parsed, {})
        manifest = json.loads((p/'manifest.json').read_text())
        for ext in ('pkl', 'txt'):
            (p/f'{DOC}.{ext}').rename(p/f'{RUN}.{ext}')
            manifest['files'][f'{RUN}.{ext}'] = manifest['files'].pop(f'{DOC}.{ext}')
        (p/'manifest.json').write_text(json.dumps(manifest))
        with self.assertRaisesRegex(ValueError, 'document UUID'):
            verify_run(p, DOC, RUN, 'source')

    def test_reference_text_only_preserved_in_json_and_fulltext(self):
        parsed = ParsedDocument([{'text': 'Body', 'page_number': 1}, {'text': 'References', 'type': 'title', 'page_number': 4},
                                 {'text': 'Author, 2024. Journal reference.', 'page_number': 4}],
                                'References\nAuthor, 2024. Journal reference.', 2, 0)
        p = publish(self.root, DOC, RUN, 'source', parsed, {})
        self.assertEqual(pickle.loads((p/f'{DOC}.pkl').read_bytes()), [parsed.result[0]])
        self.assertEqual(json.loads((p/f'{DOC}.json').read_text()), parsed.raw_response)
        self.assertIn('Author, 2024.', (p/f'{DOC}.txt').read_text())

    def test_vectors_and_empty_results_never_publish(self):
        for bad in [ParsedDocument([],"txt",0,0), ParsedDocument([{"text":"References", "type":"title"}],"x",1,0),
                    ParsedDocument([{"text":"x","extra":{"embedding":[1]}}],"x",1,0)]:
            with self.assertRaises(ValueError):
                publish(self.root,DOC,RUN,"source",bad,{})
            self.assertFalse(document_path(self.root,DOC).exists())

    def test_existing_run_never_overwritten(self):
        p=publish(self.root,DOC,RUN,"source",self.parsed,{})
        before=digest(p/"manifest.json")
        with self.assertRaises(FileExistsError):
            publish(self.root,DOC,RUN,"source",self.parsed,{})
        self.assertEqual(digest(p/"manifest.json"),before)

    def test_failure_before_manifest_leaves_no_ready_output(self):
        with patch("src.sci_parse_worker.artifacts.json.dump",side_effect=OSError("disk full")):
            with self.assertRaises(OSError):
                publish(self.root,DOC,RUN,"source",self.parsed,{})
        self.assertFalse(document_path(self.root,DOC).exists())
        self.assertEqual(list(self.root.iterdir()),[])

    def test_source_path_and_legacy_aliases(self):
        (self.root/"10.1").mkdir()
        pdf=self.root/"10.1"/"a%2528b%2529.pdf"
        pdf.write_bytes(b"%PDF-1.7 test")
        self.assertEqual(source_path(self.root,"10.1/a(b)"),pdf.resolve())
        (self.root/(DOC+"_old.pkl")).write_bytes(b"old")
        self.assertEqual(len(prior_artifacts(self.root,DOC,"10.1/a(b)")),1)
        (self.root/"escape.pdf").symlink_to("/etc/hosts")
        with self.assertRaises(ValueError):
            source_path(self.root,"escape")

    def test_existing_worker_reaches_parser_and_stops_before_embedding(self):
        pdf_root=self.root/'journals'; (pdf_root/'10.1').mkdir(parents=True)
        pdf=pdf_root/'10.1/a.pdf';pdf.write_bytes(b'%PDF-1.7 test')
        legacy=self.root/'legacy';legacy.mkdir()
        env=self.root/'env';env.write_text('')
        raw_root=self.root/'raw';out=self.root/'out'
        args=['--env-file',str(env),'--document-id',DOC,'--run-id',RUN,'--owner-id',OWNER,
              '--pdf-root',str(pdf_root),'--legacy-root',str(legacy),'--output-root',str(out),
              '--parser-version','test']
        test_snapshot=replace(snapshot(),document_id=DOC,raw_uri=str(raw_root/'sci_parse_probe'/f'{DOC}.pdf'),
                              collection_path='/sci_parse_probe',collection_storage_path='sci_parse_probe',
                              sha256=digest(pdf),file_size=pdf.stat().st_size,
                              document_metadata_json={'source_table':'journals'})
        values={'DATABASE_URL':'postgresql://test','NAS_RAW_ROOT':str(raw_root),
                'NAS_PROCESSED_ROOT':str(out),'UNSTRUCTURE_SERVE_URL':'http://parser/mineru_with_images',
                'UNSTRUCTURE_SERVE_BEARER_TOKEN':'test'}
        with patch.dict('os.environ',values,clear=True), \
             patch('src.kb_parse_worker.config.load_worker_env'), \
             patch('tests.manual.sci_probe.cli.psycopg2.connect'), \
             patch('tests.manual.sci_probe.cli.journal',return_value={'id':DOC,'doi':'10.1/a'}), \
             patch('tests.manual.sci_probe.registry.register_and_reserve',return_value=QueueMessage(123,RUN,{'job_id':RUN})) as register, \
             patch('tests.manual.sci_probe.cli.complete') as complete, \
             patch('src.kb_parse_worker.worker.claim_queue_message',return_value=SimpleNamespace(claimed=True,stage='parse',job_id=RUN)), \
             patch('src.kb_parse_worker.worker.LeaseMaintainer',NoopLease), \
             patch('src.kb_parse_worker.worker.load_parse_snapshot_with_short_connection',return_value=test_snapshot), \
             patch('src.kb_parse_worker.worker.parse_with_unstructure_serve',return_value=self.parsed) as parse:
            self.assertEqual(main(args),0)
            register.assert_not_called();complete.assert_not_called();parse.assert_not_called()
            self.assertFalse(raw_root.exists())
            self.assertEqual(main(args+['--execute']),0)
            register.assert_called_once();parse.assert_called_once();complete.assert_called_once()
            self.assertEqual(pickle.loads((out/DOC/f'{DOC}.pkl').read_bytes()),self.parsed.result)
            self.assertTrue((raw_root/'sci_parse_probe'/f'{DOC}.pdf').is_symlink())


if __name__ == "__main__":
    unittest.main()
