"""SCI receipt transformations; JSON is never modified by these functions."""
import re
from html.parser import HTMLParser

RULE_VERSION = 'mineru-sci-sections-and-header-v1'
# Mirrored from unstructure-serve mineru_sci_service: all accepted section names.
filter_patterns = [
    r"^\s*([ivxlcdm]+\.?|\d+\.?|\w+\.?)?\s*acknowledgements?\s*[:\.]?\s*$",
    r"^\s*([ivxlcdm]+\.?|\d+\.?|\w+\.?)?\s*acknowledgments?\s*[:\.]?\s*$",
    r"^\s*([ivxlcdm]+\.?|\d+\.?|\w+\.?)?\s*author\s+contributions?\s*[:\.]?\s*$",
    r"^\s*([ivxlcdm]+\.?|\d+\.?|\w+\.?)?\s*bibliography\s*[:\.]?\s*$",
    r"^\s*([ivxlcdm]+\.?|\d+\.?|\w+\.?)?\s*conflict\s+of\s+interest\s*[:\.]?\s*$",
    r"^\s*([ivxlcdm]+\.?|\d+\.?|\w+\.?)?\s*credit\s+authorship\s+contribution\s+statement\s*[:\.]?\s*$",
    r"^\s*([ivxlcdm]+\.?|\d+\.?|\w+\.?)?\s*data\s+availability\s*[:\.]?\s*$",
    r"^\s*([ivxlcdm]+\.?|\d+\.?|\w+\.?)?\s*declaration\s+of\s+competing\s+interest\s*[:\.]?\s*$",
    r"^\s*([ivxlcdm]+\.?|\d+\.?|\w+\.?)?\s*declaration\s+of\s+interests?\s*[:\.]?\s*$",
    r"^\s*([ivxlcdm]+\.?|\d+\.?|\w+\.?)?\s*declarations?\s*[:\.]?\s*$",
    r"^\s*([ivxlcdm]+\.?|\d+\.?|\w+\.?)?\s*literature\s+cited\s*[:\.]?\s*$",
    r"^\s*([ivxlcdm]+\.?|\d+\.?|\w+\.?)?\s*online\s*[:\.]?\s*$",
    r"^\s*([ivxlcdm]+\.?|\d+\.?|\w+\.?)?\s*r\s+e\s+f\s+e\s+r\s+e\s+n\s+c\s+e\s+s\s*[:\.]?\s*$",
    r"^\s*([ivxlcdm]+\.?|\d+\.?|\w+\.?)?\s*references?\s*[:\.]?\s*$",
]


def is_filtered_section(text):
    """Check if the text matches any of the filter patterns"""
    if not text:
        return False

    text_normalized = text.strip().lower()
    # MinerU can emit bilingual headings as a single title.
    if re.fullmatch(r'参考文献\s*(?:[（(]\s*references?\s*[）)])?\s*[:：.]?', text_normalized):
        return True
    return any(re.match(pattern, text_normalized, re.IGNORECASE) for pattern in filter_patterns)



class _PlainHTML(HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.parts = []
    def handle_data(self, data):
        self.parts.append(data)
    def handle_starttag(self, tag, attrs):
        if tag.lower() == 'br':
            self.parts.append('\n')
    def handle_endtag(self, tag):
        if tag.lower() in {'tr', 'p', 'div'}:
            self.parts.append('\n')
        elif tag.lower() in {'td', 'th'}:
            self.parts.append('\t')


def plain_text(chunks):
    parts = []
    for item in chunks:
        text = item['text']
        # Keep ordinary mathematical < / > as text; only parse actual markup.
        if re.search(r'</?(?:table|tr|td|th|thead|tbody|sup|sub|p|div|span|b|i|br)\b', text, re.I):
            parser = _PlainHTML()
            parser.feed(text)
            text = ''.join(parser.parts)
        parts.append(text)
    return '\n\n'.join(parts)


def clean_chunks(chunks):
    kept, headings = [], []
    skipping = False
    counts = {'headers': 0, 'section_chunks': 0, 'empty_chunks': 0}
    for item in chunks:
        # A header must not terminate a skipped section even if it resembles a title.
        if item.get('type') == 'header':
            counts['headers'] += 1
            continue
        title = item.get('type') == 'title' or item.get('text_level') is not None
        if title and is_filtered_section(item['text']):
            skipping = True
            headings.append(item['text'])
        elif title:
            skipping = False
        if skipping:
            counts['section_chunks'] += 1
        elif not item['text'].strip():
            counts['empty_chunks'] += 1
        else:
            kept.append(dict(item))
    return kept, {**counts, 'section_headings': headings, 'rule_version': RULE_VERSION}
