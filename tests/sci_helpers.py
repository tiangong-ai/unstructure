from src.sci_parse_worker.parser_adapter import ParsedDocument as CapturedDocument

def ParsedDocument(result, txt, original_chunk_count, dropped_empty_text_count):
    return CapturedDocument(result,
        {'task_id': '11111111-1111-4111-8111-111111111111', 'state': 'SUCCESS',
         'result': {'result': result, 'txt': txt}})
