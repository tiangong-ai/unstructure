package r9

const (
	yearPattern        = "\\b(?:18|19|20)\\d{2}[a-z]?\\b"
	publicationPattern = "(?i)https?://|\\bdoi\\b|\\b(?:journal|presss?|proceedings)\\b|\\b(?:vol|pp?)\\.?\\s*\\d|\\b\\d+\\s*(?:\\(\\s*\\d+\\s*\\))?\\s*[,;:]\\s*\\d+\\s*[-–]\\s*\\d+"
	prosePattern       = "(?i)^(?:we\\b|our\\b|here we\\b|in (?:this|the present) (?:paper|study|work)\\b|this (?:paper|study|work|model|result|method)\\b|the (?:results|proposed method|present study)\\b)|^(?:the (?:instrument|apparatus|equipment|sample|calibration|measurement|material|cable))\\b"
	bodyPattern        = "(?i)(?:abstract|introduction|results?(?: and discussion)?|discussion|conclusions?|summary|methods?|materials(?: and methods)?|experimental(?: methods| results| section)?|nomenclature|notation|abbreviations|appendi(?:x|ces)\\b.*|annex\\b.*|supplementary\\b.*|supporting information\\b.*|data availability\\b.*)"
	nonTargetPattern   = "(?i)(?:^|\\n|(?<=[.!?])\\s+)(?:editor[’']?s? (?:note|reply)|publisher[’']?s? note|dear (?:sir|editor)|(?:manuscript )?(?:received|accepted|revised|published)(?:\\s|:)|copyright\\b|©|author (?:biograph|details|information)|.*?\\b(?:animal care|iacuc|research permit|ethics approval|ethical approval)\\b)"
	narrativePattern   = "(?i)(?:^|\\n|(?<=[.!?])\\s+)(?:our (?:results?|findings?|model|analysis)|we (?:found|find|show|observed|demonstrate|measured|investigated|calculated)|both researchers|these (?:results|observations|findings)|the (?:results|model|proposed method) (?:shows?|supports?|indicates?|suggests?)|as researchers)\\b"
	v39Label           = "^\\s*(?:\\[(?P<br>[A-Z]?\\d{1,4})\\]|\\((?P<paren>\\d{1,4})\\)|(?P<plain>\\d{1,4})\\.\\s+)\\s*(?=\\S)"
	v39Sub             = "^\\s*\\(([a-z])\\)\\s+(?=\\S)"
	v39Locator         = "(?i)\\b\\d{1,4}\\s*(?:\\(\\s*\\d{1,3}\\s*\\))?\\s*[,;:]\\s*\\d{1,5}(?:\\s*[-–−]\\s*\\d{1,5})?|\\bpp?\\.\\s*\\d|\\b\\d{1,4}\\s*\\(\\s*(?:18|19|20)\\d{2}\\s*\\)\\s*\\d{1,6}\\b"
	v39Tech            = "(?i)\\b(?:we|our|was|were|is|are|used|measured|calibrated|calibrating|manufactur\\w*|supplied|must|should)\\b"
	ocrYear            = "(?<!\\d)(?:1[89]|2[0oO])[0-9oOlIi]{2}(?!\\d)"
	entryLabel         = "^\\s*(?:\\[\\s*[A-Z]?\\d{1,4}\\s*\\]|\\(\\d{1,4}\\)|\\d{1,4}\\.\\s*(?=[A-ZÀ-ÖØ-Þ]))\\s*"
	locatorPattern     = "\\d{1,4}\\s*(?:[A-Za-z]|\\(\\s*[\\w .–−/-]{1,15}\\s*\\))?\\s*[,;:]\\s*[A-Za-z]{0,4}\\d{1,8}(?:\\s*[-–−]\\s*\\d{1,8})?"
	doiPattern         = "(?i)\\b10\\s*[.]\\s*\\d{4,9}\\s*/\\S+"
	clauseNarrative    = "(?i)^(?:we\\b|our\\b|here\\b|the\\b|this\\b|these\\b|those\\b|both\\b|however\\b|therefore\\b|for (?:the|this)\\b|in (?:the|this|our)\\b|according to\\b|it\\s+(?:is|was|can)\\b)"
	verbPattern        = "(?i)\\b(?:was|were|is|are|must|should|supplied|manufactured|found that|showed that|reported that|results show|we measured|our results|we found)\\b"
	bioPattern         = "(?i)\\b(?:graduated|research interests?|corresponding author|e-mail)\\b|\\b(?:he|she)\\s+(?:is|was|has|received|obtained)|\\b(?:received|obtained|earned|holds?)\\b.{0,60}\\b(?:degree|dipl|ph\\.?\\s*d|b\\.?\\s*sc|m\\.?\\s*sc|bachelor|master)\\b|\\b(?:ethics|ethical)\\s+(?:approval|committee|review)|\\b(?:informed consent|patient consent|research permit|iacuc)\\b"
	metaPattern        = "(?i)^(?:received\\b|accepted\\b|revised\\b|published\\b|copyright\\b|©|publisher.?s note\\b|editor.?s (?:note|reply)\\b|dear\\b|author (?:biograph|details|information)|authors?.? information|data availability\\b|supporting information\\b|supplementary\\b)"
	finitePattern      = "(?i)(?:is|are|was|were|has|have|had|found|saw|show(?:s|ed)?|report(?:s|ed)?|observ(?:es|ed)?|suggest(?:s|ed)?|conclud(?:es|ed)?|argu(?:es|ed)?|demonstrat(?:es|ed)?|collect(?:s|ed)?|measur(?:es|ed)?|used|present(?:s|ed)?)"
	authorPattern      = "^[^\\W\\d_][\\w’'´˛.-]{1,50}(?:,\\s*|\\s+)(?:[A-ZÀ-ÖØ-Þ]{1,5}\\.?[\\s,.;]|et\\s+al\\b)"
	initialAuthor      = "^(?:[A-Z]\\.\\s*){1,4}[^\\W\\d_][\\w’\\x27´˛-]{1,50}"
)

var filterKinds = []string{"references", "acknowledgements", "contributions", "declarations"}
var filterPatterns = map[string]string{
	"references":       "(?:references?(?: and notes)?|notes and references|references cited only in the online enhancements|(?:general )?bibliography(?: \\d{4}\\s*[-–]\\s*\\d{4})?|literature\\s*cited|r e f e r e n c e s|kirjallisuutta|uncited references)(?: (?:from|for) appendix [a-z0-9]+)?",
	"acknowledgements": "(?:acknowledg(?:e)?ments?|funding(?: information| sources?)?)",
	"contributions":    "(?:authors?[’\\x27]?\\s*contributions?|authorship contribution|credit authorship contribution statement|statement of authorship)",
	"declarations":     "(?:conflicts? of interests?|declaration of competing interests?|declaration of interests?|declarations?)",
}

const extraProtected = "(?i)this publication has been subjected|peer and administrative review|approved for publication|(?:mention|use) of trade names.{0,100}endorse|makes any warranty|legal liability|views (?:and opinions )?(?:expressed|of authors)|\\bethic(?:s|al)?\\b|\\b(?:informed consent|patient consent|reviewer information|peer.review information)\\b|(?:^|\\n)\\s*(?:supporting information|supplementary information|supplementary material|data availability|author information)\\b|\\b(?:his|her) (?:main |focal )?research (?:interest|includes)|\\bis (?:an? |the )?(?:ph\\.?d\\.? student|senior researcher|post.doctoral researcher)|\\b(?:he|she) (?:obtained|received|graduated|has done)\\b|\\b(?:received|obtained|earned|completed|holds?)\\b.{0,80}\\b(?:degree|dipl|m\\.?sc|b\\.?sc|ph\\.?d|doctorate|bachelor|master)"

var disclosureCues = map[string]string{
	"acknowledgements": "(?i)\\b(?:thank\\w*|gratitude|grateful\\w*|acknowledg\\w*|assistance|indebted|appreciat\\w*)\\b|\\b(?:was|were|is|are|been)\\s+(?:(?:partly|partially|financially|also|in part)\\s+)*(?:supported|funded|sponsored)\\b|\\b(?:funding|financial support|research grant)\\s+from|\\binstrumental in assisting\\b|grant/award number|\\b(?:no (?:specific|external|additional) (?:funding|funds)|not (?:received|got) any funds|not available)\\b|\\bwith the support of\\b|\\bfinancial support (?:provided|received)\\b",
	"contributions":    "(?i)\\b(?:conceptualization|methodology|software|validation|investigation|writing|supervision|visualization|curation|administration|conceived|designed|wrote|conducted|contributed equally)\\b",
	"declarations":     "(?i)\\b(?:declare\\w*|conflicts?|competing|financial|interests?|relationships?|personal)\\b",
}
