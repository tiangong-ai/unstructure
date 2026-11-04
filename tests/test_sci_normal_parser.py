import copy
import json
import pickle
import tempfile
import unittest
from pathlib import Path
from unittest.mock import Mock, patch

from src.sci_parse_worker import parser_adapter as api
from src.sci_parse_worker.artifacts import publish, verify_run
from src.sci_parse_worker.cleaning import clean_chunks, plain_text
from tests.test_sci_finalizer import DOC, JOB

TASK='11111111-1111-4111-8111-111111111111'

class NormalParserTests(unittest.TestCase):
    def response(self,payload,code=200):
        return Mock(status_code=code,json=Mock(return_value=payload))

    def test_post_uses_real_query_form_contract_and_no_vision_fields(self):
        with tempfile.TemporaryDirectory() as temp:
            path=Path(temp)/'source.pdf';path.write_bytes(b'%PDF')
            with patch.object(api.requests,'post',return_value=self.response({'task_id':TASK,'state':'PENDING'})) as post:
                result=api.submit_mineru_task(path,'http://parser','token',timeout_seconds=120)
        self.assertEqual(result.status_url,f'http://parser/mineru/task/{TASK}')
        self.assertEqual(post.call_args.args[0],'http://parser/mineru/task')
        kw=post.call_args.kwargs
        self.assertEqual(kw['params'],{'chunk_type':'true','return_txt':'true'})
        self.assertEqual(kw['data'],{'tier':'advanced','priority':'normal'})

    def test_capture_preserves_all_response_fields_and_empty_chunks(self):
        payload={'task_id':TASK,'state':'SUCCESS','extra':{'future':True},
                 'result':{'result':[{'text':'', 'page_number':1}, {'text':'Body','type':None}], 'txt':'remote txt'}}
        before=copy.deepcopy(payload)
        with patch.object(api.requests,'get',return_value=self.response(payload)):
            status=api.fetch_mineru_task_status('http://parser','token',TASK,timeout_seconds=30)
        self.assertEqual(status.parsed.raw_response,before)
        self.assertEqual(len(status.parsed.result),2)
        status.parsed.result[1]['text']='modified'
        self.assertEqual(status.parsed.raw_response,before)
        self.assertEqual(payload,before)

    def test_terminal_500_is_a_job_failure_but_other_500_raises(self):
        payload={'task_id':TASK,'state':'FAILURE','error':'parse failed'}
        with patch.object(api.requests,'get',return_value=self.response(payload,500)):
            self.assertEqual(api.fetch_mineru_task_status('http://parser','token',TASK,timeout_seconds=30).state,'FAILURE')
        response=self.response({'detail':'server error'},500)
        response.raise_for_status.side_effect=RuntimeError('HTTP500')
        with patch.object(api.requests,'get',return_value=response):
            with self.assertRaisesRegex(RuntimeError,'HTTP500'):
                api.fetch_mineru_task_status('http://parser','token',TASK,timeout_seconds=30)

    def test_mismatched_task_is_rejected(self):
        with patch.object(api.requests,'get',return_value=self.response({'task_id':JOB,'state':'SUCCESS'})):
            with self.assertRaisesRegex(api.ParserError,'identity'):
                api.fetch_mineru_task_status('http://parser','token',TASK,timeout_seconds=30)

class ContentTests(unittest.TestCase):
    def chunks(self):
        return [{'text':'Journal header','type':'header','page_number':1},
                {'text':'Paper title','type':'title','page_number':1},
                {'text':'Body mentions references and data availability.','page_number':1},
                {'text':'<table><tr><td>A</td><td>2</td></tr></table>','page_number':2},
                {'text':'Acknowledgements','type':'title','page_number':2},
                {'text':'Thanks to authors','page_number':2},
                {'text':'Page header','type':'header','page_number':3},
                {'text':'References','type':'title','page_number':3},
                {'text':'Author, 2024. Citation.','page_number':3},
                {'text':'Appendix','type':'title','page_number':4},
                {'text':'Appendix data','page_number':4}]

    def test_original_stays_complete_pickle_filters_sections_and_resumes(self):
        chunks=self.chunks();before=copy.deepcopy(chunks)
        cleaned,stats=clean_chunks(chunks)
        self.assertEqual(cleaned,[chunks[i] for i in [1,2,3,9,10]])
        self.assertEqual(stats['headers'],2)
        self.assertEqual(stats['section_chunks'],4)
        self.assertEqual(stats['section_headings'],['Acknowledgements','References'])
        self.assertEqual(chunks,before)
        self.assertIn('Citation.',plain_text(chunks))
        self.assertIn('Journal header',plain_text(chunks))
        self.assertNotIn('<table>',plain_text(chunks))
        self.assertIn('A\t2',plain_text(chunks))
        self.assertNotIn('page_number',plain_text(chunks))

    def test_all_accepted_section_names_use_the_local_sci_matcher(self):
        headings=['Acknowledgments','Author contributions','Bibliography','Conflict of interest',
            'CRediT authorship contribution statement','Data availability','Declaration of competing interest',
            'Declaration of interests','Declarations','Literature cited','Online','R E F E R E N C E S','2. References', '参考文献 (References):']
        for title in headings:
            with self.subTest(title=title):
                chunks=[{'text':'Body'},{'text':title,'type':'title'},{'text':'Skip'},{'text':'Next','type':'title'},{'text':'Keep'}]
                self.assertEqual([x['text'] for x in clean_chunks(chunks)[0]],['Body','Next','Keep'])

    def test_three_artifacts_are_distinct_and_manifest_binds_all(self):
        chunks=self.chunks()
        raw={'task_id':TASK,'state':'SUCCESS','result':{'result':chunks,'txt':'DO NOT blindly reuse remote txt'}}
        parsed=api.ParsedDocument(chunks,copy.deepcopy(raw))
        with tempfile.TemporaryDirectory() as temp:
            out=publish(Path(temp),DOC,JOB,'source',parsed,{'task_id':TASK})
            self.assertEqual(json.loads((out/f'{DOC}.json').read_text()),raw)
            self.assertEqual((out/f'{DOC}.txt').read_text(),plain_text(chunks))
            self.assertEqual(pickle.loads((out/f'{DOC}.pkl').read_bytes()),clean_chunks(chunks)[0])
            manifest=verify_run(out,DOC,JOB,'source')
            self.assertEqual(manifest['raw_item_count'],11)
            self.assertEqual(manifest['parsed_item_count'],5)
            self.assertEqual(len(manifest['files']),3)
            (out/f'{DOC}.json').write_text('{}')
            with self.assertRaisesRegex(ValueError,'checksum'):
                verify_run(out,DOC,JOB,'source')
