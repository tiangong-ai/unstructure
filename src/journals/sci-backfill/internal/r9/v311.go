package r9

// v3.11 reference-container and disclosure rules, ported from the verified
// Python v3.11 candidate. All offsets are rune offsets and all reason strings
// are preserved verbatim. This file intentionally contains no CLI or test
// harness: the caller owns region assembly and the fixed-point loop.
//
// Shared contract used here: Block/Removal/rx*/rune* helpers from types.go;
// filter* and regex constants from patterns.go; normalizedTitle, lineKind,
// protected, hasStructure, anchor and referenceSeed from the base rules.
// v311RegionLines is a local adapter because the shared contract does not yet
// expose regionLines/RefLine; swap it for the shared region assembler when it
// lands, otherwise the v3.11 row view and the shared reference_keeps can drift.

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const (
	v311CoordPattern = `\b\d{1,4}\s*\(\s*(?:18|19|20)\d{2}[a-z]?\s*\)\s*[,.:]?\s*\d{1,6}\b|\b\d{1,4}\s*\(\s*(?:18|19|20)\d{2}[a-z]?\s*\)\s*[,.:]?\s*\d{1,6}\s*[-–−]\s*\d{1,6}|\b\d{1,4}\s*\(\s*\d{1,3}\s*\)\s*[,.:]\s*\d{1,6}\s*[-–−]\s*\d{1,6}`
	v311EndPattern   = `(?i)^(?:biosketch(?:es)?|biograph(?:y|ies)|author biographies|how to cite(?: this article)?|citation information|correction|corrigendum|erratum)\b`
	v311FootPattern  = `(?i)^(?:journal pre-proof|accepted manuscript|accepted article|uncorrected proof|author manuscript|\d{1,5})\s*$`

	v311DiscHeadPattern = `(?i)^\s*(?:#{1,6}\s*)?(acknowledg(?:e)?ments?|funding(?: information| sources?)?|statement of author contributions|authors?[’\x27]? contributions?(?: statement)?|conflicts? of interests?(?: statement)?|declaration of competing interests?)\b\s*(?:[.:—–-]\s*)?`
	v311SocialPattern   = `(?i)\b(?:thank\w*|gratitude|grateful\w*|acknowledg\w*|appreciat\w*|indebted|benefit(?:ed|ted)? from.{0,100}(?:comments|discussions|referees)|(?:^|(?-i:\b[A-Z][A-Z.]{0,10})\s+(?:was|were)\s+)funded\s+by|(?:this|our)\s+(?:work|study|research|project|paper).{0,75}(?:supported|funded|sponsored)\s+by|funding\s+(?:provided|for this|from)|financial support|conceived|conducted the analysis|contributed to writing|declare\w*|competing financial interests)\b`
	v311OriginPattern   = `(?i)\b(?:paper|article|study|work)\b.{0,100}\b(?:first presented|previously presented|originally presented|presented at|presented to|based on|adapted from|drawn from)\b|\b(?:earlier|previous|first|original)\s+version\b.{0,100}\b(?:was|has been)\s+presented\b`

	v311InlineMathPattern = `\$[^$\n]*(?:=|\\frac|\\sum|\\int|\\partial|\\propto)[^$\n]*\$`
	v311BulletPattern     = `^\s*[■▪•●]\s*`
	v311CopyrightPattern  = `(?i)\bcopyright\b|©|\ball rights reserved\b|\bmanuscript received\b`
	v311AppendicesPattern = `(?i)^\s*(?:appendi(?:x|ces)|annex|supporting information|supplementary)\b`
	v311BookReviewPattern = `(?i)\b(?:book\s*sh(?:elf|elves)|reading list|recommended reading|book reviews?)\b|\b(?:books?|volumes?)\b.{0,65}\b(?:reviewed|recommended|discussed)\b`

	v311NormLabelPattern = `^([ \t]*)[Il](\d{1,3})(?=\.)`
	v311NormNoisePattern = `(?<=\w)[´˛\x60ˆ~¸]+(?=\w|\s+[A-Z])`

	v311YearTailPattern    = `^[a-z]?[)\].,;:]*\s*`
	v311LowerPrefixPattern = `^[a-z](?=[).,;:\s])`
	v311SentenceSplit      = `(?<=[.!?])\s+(?=[A-Z])`
	v311SentenceBoundary   = `[.!?](?=\s+[A-Z])`
	v311TokenAtEnd         = `([A-Za-z]+)[.]$`
	v311DisclosureParts    = `.+?(?:[.!?](?=\s+[A-Z]|\s*$)|$)(?:\s*)`
	v311WholeText          = `[\s\S]+`

	v311EthicsPattern      = `(?i)\bethic(?:s|al)?\b`
	v311FundingListPattern = `(?i)\b(?:service|bureau|foundation|council|ministry|university|institute)\b`
	v311ForthcomingPattern = `(?i)\b(?:will be (?:expanded|published)|forthcoming book)\b`

	v311FirstPersonPattern = `^(?:I|My|We|Our|They|Their|He|His|She|Her)\s+[a-z][a-z-]*\b`
	v311SamplesPattern     = `(?i)^(?:Samples?|Results?|Data|The (?:samples?|results?|data|authors?|study))\s+(?:[A-Za-z-]+\s+){0,2}(?:` + finitePattern + `|[a-z]{3,}ed)\b`
	v311LetMathPattern     = `\$[^$]*(?:=|\\frac|\\sum)[^$]*\$`

	v311AuthorYearParen    = `^[A-ZÀ-ÖØ-Þ][\w’'-]{1,50},?\s*(?:[A-Z][.]\s*){1,6}(?=\W|\d|$)`
	v311AuthorComma        = `^[A-ZÀ-ÖØ-Þ][\w’'-]{1,50},\s*[A-ZÀ-ÖØ-Þ][a-zà-öø-ÿ'-]{1,35}(?:\s+[A-Z][.])?[, ]`
	v311AuthorInitials     = `^(?:[A-ZÀ-ÖØ-Þ][\w’\x27-]+[, ]+)?(?:[A-Za-z][.]\s*){1,5}[A-ZÀ-ÖØ-Þ][\w’\x27-]+`
	v311AuthorParticle     = `(?i)^(?:[A-Z][\w’\x27-]+\s+)?(?:van|von|de|der|den|del|du|la|le)\s+(?:(?:van|von|de|der|den|del|du|la|le)\s+)?[A-Z][\w’\x27-]+,\s*[A-Z]`
	v311AuthorMulti        = `^[A-ZÀ-ÖØ-Þ][\w&/.-]+(?:\s+[A-ZÀ-ÖØ-Þ][\w&/.-]+){1,7}[.,]`
	v311AuthorShortOrg     = `^[A-Z]{2,12}[.,]`
	v311AuthorCamel        = `^[A-Z][a-z]+[A-Z][\w-]*[.,]`
	v311AuthorCommaPair    = `^[A-ZÀ-ÖØ-Þ][\w&/.-]+,\s*[A-ZÀ-ÖØ-Þ][\w&/.-]+\s*[,;.]`
	v311AuthorInitialComma = `^[^\W\d_][\w -]{1,80},\s*[1Il]\.\s*[,;]`

	v311ISBNPattern        = `(?i)\bISBN(?:-1[03])?\s*[: ]*\d|\b(?:press|publishing|proceedings)\b`
	v311FormalIDPattern    = `^[A-Z]{2,12}(?:/[A-Z]{1,8})?\s+\d{2,7}(?:[-./:]\d+)*\s*[:,;]\s*\S`
	v311AccessedURLPattern = `(?i)^(?:https?://|h[tp]+s?://)`
	v311AccessCuePattern   = `(?i)\b(?:accessed|retrieved|cited)\b`
	v311AccessYearPattern  = `(?i)\b(?:accessed|retrieved|cited)\b`

	v311RepeatedYearPattern = `^(?:[—–-]{1,5}[.,]?\s*)?(?:18|19|20)\d{2}[a-z]?[.,:]\s*\S`
	v311InstitutionPattern  = `^[A-Z][A-Za-z.]*(?:\s+[A-Z][A-Za-z.]+){1,5}\s*\(`
	v311ContinuationLower   = `^[a-z]`
	v311ContinuationURL     = `^https?://`
	v311ContinuationDOI     = `^10[.]`
	v311ContinuationFields  = `\b(?:ed|eds)\.|\bPress\b`
	v311PubTailStart        = `^\s*(?:\(\s*(?:18|19|20)\d{2}\s*\)|\d{1,4}\s*\()`
	v311PageTailPattern     = `^\s*\d{1,8}\s*[-–−]\s*\d{1,8}\s*$`
	v311BracketListItem     = `^(?:\[\s*\]\s*)?[a-z]\)\s*`
	v311FigTablePattern     = `(?i)^\s*(?:fig(?:ure)?[.]?|table)\s*[A-Z]?\d`
	v311NumberedPrefix      = `^\s*(\d{3,5})\s+`
	v311AccessTailPattern   = `(?i)^(?:https?://|h[tp]+s?://|doi\s*:|10[.]\d|\d|[)\]])`
	v311ClippedAuthorWord   = `[A-Za-zÀ-ÖØ-öø-ÿ][\w’'-]*`
	v311ClippedInitial      = `\b[A-Z][.]`
	v311BookReviewText      = `(?i)\b(?:book reviews?|recommended reading|reading list|book\s*sh(?:elf|elves))\b`
	v311UnknownTitleEnd     = `[.,;:]$|\b(?:18|19|20)\d{2}\b|[,;]`
	v311AuthorYearSuffix    = `(?i)[a-z]?[)\].,;:]*\s*$`
)

var v311ClauseStops = map[string]bool{
	"of": true, "in": true, "on": true, "from": true, "for": true,
	"with": true, "at": true, "to": true, "and": true, "under": true,
	"between": true, "by": true, "after": true, "before": true,
	"during": true, "among": true, "across": true, "through": true,
	"into": true, "without": true, "a": true, "an": true, "the": true,
	"as": true, "than": true, "or": true, "towards": true, "toward": true,
	"via": true, "within": true, "beyond": true, "against": true,
	"over": true, "about": true, "along": true,
}

var v311Abbreviations = map[string]bool{
	"dr": true, "drs": true, "prof": true, "mr": true, "mrs": true,
	"ms": true, "jr": true, "sr": true, "vs": true, "etc": true,
	"al": true, "no": true, "fig": true, "eq": true, "vol": true,
	"pp": true,
}

var v311Particles = map[string]bool{
	"van": true, "von": true, "de": true, "der": true, "den": true,
	"del": true, "du": true, "da": true, "di": true, "la": true,
	"le": true, "et": true, "al": true,
}

// v311RefLine mirrors the fields of the Python RefLine that v3.11 consumes.
type v311RefLine struct {
	BI              int
	A, Z            int
	Text            string
	Seed            bool
	Header          bool
	RefHeader       bool
	Structure       bool
	Prose           bool
	Island          bool
	PrintedLineView bool
}

type v311DisclosureSentence struct {
	Start, End int
	Text       string
}

type v311Class struct {
	strong, weak, metadata, hard bool
}

// v311Norm reproduces _norm: OCR noise cleanup, NFKC, full-width punctuation
// translation and trimming.
func v311Norm(text string) string {
	if m := rxFind(v311NormLabelPattern, text); m != nil {
		text = m.Group(1) + "1" + m.Group(2) + tailFrom(text, m.End)
	}
	if matches := rxAll(v311NormNoisePattern, text); len(matches) > 0 {
		runes := []rune(text)
		for i := len(matches) - 1; i >= 0; i-- {
			runes = append(runes[:matches[i].Start], runes[matches[i].End:]...)
		}
		text = string(runes)
	}
	text = norm.NFKC.String(text)
	text = strings.NewReplacer("，", ",", "；", ";", "（", "(", "）", ")").Replace(text)
	return strings.TrimSpace(text)
}

// v311Payload reproduces _payload: normalized text with an entry label removed.
func v311Payload(text string) string {
	s := v311Norm(text)
	if m := rxFind(entryLabel, s); m != nil {
		return tailFrom(s, m.End)
	}
	return s
}

func v311AuthorShape(text string) bool {
	return rxHas(v311AuthorParticle, text) ||
		rxHas(`\A(?:`+authorPattern+`)`, text) ||
		rxHas(`\A(?:`+initialAuthor+`)`, text) ||
		rxHas(v311AuthorMulti, text) ||
		rxHas(v311AuthorShortOrg, text) ||
		rxHas(v311AuthorCamel, text) ||
		rxHas(v311AuthorCommaPair, text) ||
		rxHas(v311AuthorInitialComma, text)
}

func v311Author(s string) bool {
	return v311AuthorShape(s) ||
		rxHas(v311AuthorYearParen, s) ||
		rxHas(v311AuthorComma, s) ||
		rxHas(v311AuthorInitials, s)
}

func v311CompletePublicationTail(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	if rxHas(`^(?i:the|this|these|those)\s+(?:[a-z-]+\s+){0,5}`+finitePattern+`\b`, trimmed) {
		return false
	}
	if rxHas(`(?i)^(?:we|our|this study|the results|see)\b`, trimmed) {
		return false
	}
	if rxHas(`(?i)\b(?:accessed|retrieved|cited)\b`, text) &&
		rxHas(`(?i)https?://`, text) && rxHas(`\d`, text) {
		return true
	}
	return rxHas(locatorPattern, text) ||
		rxHas(`(?i)\bISBN(?:-1[03])?\s*[: ]*\d|\b(?:working|research|technical|conference)\s+(?:paper|report)\b|\b(?:paper|report)\s+(?:no\.?\s*\d|[A-Z]{2,}[-/]|(?:commissioned|prepared|published)\s+(?:by|for)\b)|\b(?:pages?|pp?\.)\s+\d{1,6}\s*[-–−]\s*\d{1,6}`, text)
}

func v311Fields(s string) bool {
	return v311CompletePublicationTail(s) ||
		rxHas(locatorPattern, s) ||
		rxHas(v311CoordPattern, s) ||
		rxHas(doiPattern, s) ||
		rxHas(v311ISBNPattern, s)
}

func v311Clause(tail string) bool {
	for _, m := range rxAll(`\b([a-z][a-z'-]{2,})\s+(a|an|the|that|this|these|those|our|their|my|your|his|her|its)\b`, tail) {
		word := m.Group(1)
		if m.Start > 0 && !v311ClauseStops[word] && !strings.HasSuffix(word, "ing") {
			return true
		}
	}
	return rxHas(`(?i)\b(?:is|are|was|were|has|have|had|must|should|can|could)\b`, tail)
}

// v311ReferenceBody reports whether a reference line is actually a prose body
// sentence with a cited author-year prefix.
func v311ReferenceBody(text string) bool {
	p := v311Payload(text)
	y := rxFind(ocrYear, p)
	if y == nil || y.Start > 180 || !v311Author(p) || v311Fields(p) {
		return false
	}
	tail := rxReplace(v311YearTailPattern, "", tailFrom(p, y.End))
	return v311Clause(tail)
}

func v311Narrative(s string) bool {
	if v311ReferenceBody(s) {
		return true
	}
	p := v311Payload(s)
	y := rxFind(ocrYear, p)
	if y != nil && v311Author(p) {
		tail := rxReplace(v311YearTailPattern, "", tailFrom(p, y.End))
		if rxHas(`(?i:^(?:I|we|our|my|you|your|he|she|they|their|the authors?|researchers?|participants?|patients?|results?|samples?|data)\b)`, tail) {
			return true
		}
		if rxHas(`(?i:^(?:the|a|an)\s+(?:[a-z-]+\s+){1,6}(?:is|are|was|were|has|have|had)\b)`, tail) {
			return true
		}
		if rxHas(`(?i:^(?:is|are|was|were|has|have|had|found|showed|reported|observed|argued|demonstrated)\b)`, tail) {
			return true
		}
	}
	return rxHas(prosePattern, p) || rxHas(narrativePattern, p)
}

func v311LineBodyText(text string) bool {
	s := v311Payload(text)
	y := rxFind(ocrYear, s)
	if y == nil || y.Start > 180 || !v311AuthorShape(s) {
		return false
	}
	tail := rxReplace(v311LowerPrefixPattern, "", tailFrom(s, y.End))
	initial := rxFind(`^[)\]]?([.,;:]?)\s*([A-Za-z][A-Za-z-]*)\b`, tail)
	if initial == nil {
		return false
	}
	body := rxReplace(`^[)\]]?[.,;:]?\s*`, "", tail)
	parts := rxFind(v311SentenceSplit, body)
	if parts != nil {
		second := tailFrom(body, parts.End)
		if v311CompletePublicationTail(second) {
			return false
		}
	}
	if initial.Group(1) != "." && v311IsLowerWord(initial.Group(2)) && rxFull(finitePattern, initial.Group(2)) {
		return true
	}
	if rxHas(`(?i:^(?:I|me|we|us|you|he|she|they|the authors?|researchers?|participants?|patients?)\s+[a-z][a-z-]*\b)`, body) {
		return true
	}
	if rxHas(`(?i:^(?:my|our|your|his|her|their)\s+(?:[a-z-]+\s+){1,5}(?:`+finitePattern+`|[a-z]{3,}ed)\b)`, body) {
		return true
	}
	if rxHas(`(?i:^(?:samples?|results?|data)\s+(?:[a-z-]+\s+){0,2}(?:`+finitePattern+`|[a-z]{3,}ed)\b)`, body) {
		return true
	}
	if rxHas(`(?i:^(?:the|a|an)\s+(?:[a-z-]+\s+){1,6}`+finitePattern+`\b)`, body) {
		return true
	}
	return !rxHas(`(?i:^(?:the|a|an|towards?|with|from|in|on)\b)`, body) &&
		rxHas(`[.!?]\s*$`, body) &&
		rxHas(`^[A-Z][a-z-]+\s+[a-z]{3,}ed(?=\s)`, body)
}

func v311BodyAfterCitation(text string) bool {
	s := v311Payload(text)
	locators := append(rxAll(locatorPattern, s), rxAll(doiPattern, s)...)
	if len(locators) == 0 {
		return false
	}
	for _, boundary := range rxAll(v311SentenceBoundary, s) {
		var lastEnd int
		found := false
		for _, locator := range locators {
			if locator.End <= boundary.Start {
				if !found || locator.End > lastEnd {
					lastEnd = locator.End
				}
				found = true
			}
		}
		if !found {
			continue
		}
		between := runeSlice(s, lastEnd, boundary.Start)
		newEntry := false
		fragment := ""
		for _, start := range rxAll(`(?<!\w)[A-ZÀ-ÖØ-Þ]`, between) {
			fragment = tailFrom(between, start.Start)
			if v311NamedYearRecord(fragment) && rxHas(ocrYear+v311AuthorYearSuffix, fragment) {
				newEntry = true
				break
			}
		}
		tail := strings.TrimSpace(tailFrom(s, boundary.End))
		if newEntry {
			entryTail := tail
			for _, cut := range rxAll(v311SentenceBoundary, tail) {
				following := tailFrom(tail, cut.End)
				if v311NamedYearRecord(following) {
					entryTail = runeSlice(tail, 0, cut.End)
					break
				}
			}
			candidate := fragment + ". " + entryTail
			if !v311LineBodyText(candidate) {
				continue
			}
		}
		if tail == "" {
			continue
		}
		if v311AuthorShape(v311Payload(tail)) && rxHas(ocrYear, tail) {
			continue
		}
		sentence := tail
		if split := rxFind(v311SentenceSplit, tail); split != nil {
			sentence = runeSlice(tail, 0, split.Start)
		}
		if v311CompletePublicationTail(sentence) {
			continue
		}
		if rxHas(prosePattern, sentence) {
			return true
		}
		if rxHas(v311FirstPersonPattern, sentence) {
			return true
		}
		if rxHas(v311SamplesPattern, sentence) {
			return true
		}
		if rxHas(`^Let\b`, tail) && rxHas(v311LetMathPattern, tail) {
			return true
		}
	}
	return false
}

func v311NamedYearRecord(text string) bool {
	s := v311Payload(text)
	y := rxFind(ocrYear, s)
	return y != nil && y.Start < 650 && v311AuthorShape(s)
}

func v311FormalIdentifierRecord(text string) bool {
	s := v311Payload(text)
	return rxHas(v311FormalIDPattern, s) &&
		len(strings.Fields(s)) >= 5 &&
		!rxHas(`[=±<>]`, s)
}

func v311AccessedURLRecord(text string) bool {
	s := v311Payload(text)
	return rxHas(v311AccessedURLPattern, s) &&
		rxHas(v311AccessCuePattern, s) &&
		rxHas(ocrYear, s)
}

func v311HardProse(text string, seed bool) bool {
	s := v311Payload(text)
	if s == "" {
		return false
	}
	if rxHas(bioPattern, s) || rxHas(metaPattern, s) {
		return true
	}
	if seed {
		return false
	}
	return rxHas(`\A(?:`+narrativePattern+`)`, s) ||
		rxHas(verbPattern, s) ||
		rxHas(`(?i)^(?:model|instrument|apparatus|equipment|device|calibration)\b`, s)
}

func v311ReviewListContext(blocks []Block, bi int) bool {
	start := bi - 2
	if start < 0 {
		start = 0
	}
	var parts []string
	for _, b := range blocks[start:bi] {
		parts = append(parts, textOf(b))
	}
	return rxHas(v311BookReviewPattern, strings.Join(parts, "\n"))
}

func v311UnknownTitle(text string) bool {
	s := strings.TrimSpace(text)
	return runeLen(s) < 150 &&
		len(strings.Fields(s)) < 20 &&
		!strings.Contains(s, "\n") &&
		!rxHas(v311UnknownTitleEnd, s) &&
		!anchor(s)
}

// v311IsLowerWord mirrors Python str.islower(): at least one cased rune and
// no uppercase/titlecase rune. Uncased characters are ignored.
func v311IsLowerWord(s string) bool {
	hasCased := false
	for _, r := range s {
		if unicode.IsUpper(r) || unicode.IsTitle(r) {
			return false
		}
		if unicode.IsLower(r) {
			hasCased = true
		}
	}
	return hasCased
}

func v311UpperFirst(s string) bool {
	for _, r := range s {
		return unicode.IsUpper(r)
	}
	return false
}

func v311AlphaFirst(s string) bool {
	for _, r := range s {
		return unicode.IsLetter(r)
	}
	return false
}

func v311Contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func v311StructureBlocks(blocks []Block) map[int]bool {
	out := map[int]bool{}
	for bi, b := range blocks {
		for _, line := range splitLines(textOf(b), false) {
			if hasStructure(line) || lineKind(line) == "structure" {
				out[bi] = true
				break
			}
		}
	}
	return out
}

// v311RegionLines is the local row view required by the v3.11 detection rows.
// It mirrors the Python region_lines fields; the shared region assembler may
// replace it later without changing the v3.11 rules.
func v311RegionLines(blocks []Block) []v311RefLine {
	structureBlocks := v311StructureBlocks(blocks)
	rows := make([]v311RefLine, 0)
	for bi, b := range blocks {
		text := textOf(b)
		singleLine := len(splitLines(text, false)) == 1
		blockType, _ := b["type"].(string)
		pos := 0
		for _, line := range splitLines(text, true) {
			a := pos
			pos += runeLen(line)
			s := strings.TrimSpace(line)
			title := normalizedTitle(rxReplace(v311BulletPattern, "", s))
			refHeader := false
			if pattern, ok := filterPatterns["references"]; ok {
				refHeader = rxFull(pattern, title)
			}
			seed := referenceSeed(line)
			header := !refHeader && !seed &&
				((runeLen(s) < 180 && rxFull(bodyPattern, title)) ||
					v311UnknownTitle(s) ||
					(singleLine && (blockType == "title" || blockType == "heading")))
			inlineMath := !seed && rxHas(v311InlineMathPattern, line)
			metadata := rxHas(v311CopyrightPattern, line)
			technical := !seed &&
				(rxHas(`(?i)^(?:model|instrument|apparatus|equipment|device|calibration)\b`, v311Payload(line)) ||
					rxHas(`(?i)\b(?:supplied|manufactured)\b`, line))
			neutral := rxFull(`^\d{1,5}[.]?$`, s) || rxFull(initialAuthor, s)
			probe := line
			if seed {
				probe = rxReplace(v311EthicsPattern, "", line)
			}
			stop := v311HardProse(line, seed) ||
				protected(probe) ||
				v311BodyAfterCitation(line) ||
				(!seed && (lineKind(line) == "body" || lineKind(line) == "structure"))
			rows = append(rows, v311RefLine{
				BI:              bi,
				A:               a,
				Z:               pos,
				Text:            line,
				Seed:            seed,
				Header:          header,
				RefHeader:       refHeader && !structureBlocks[bi],
				Structure:       structureBlocks[bi] || inlineMath,
				Prose:           stop,
				Island:          metadata || technical || neutral,
				PrintedLineView: false,
			})
		}
	}
	return rows
}

// v311DetectionRows reproduces _v311_detection_rows, including the printed
// line-number "shadow view" that joins numbered lines into a single row.
func v311DetectionRows(blocks []Block) []v311RefLine {
	raw := v311RegionLines(blocks)
	order := make([]int, 0)
	groups := map[int][]int{}
	for i, row := range raw {
		if _, ok := groups[row.BI]; !ok {
			order = append(order, row.BI)
		}
		groups[row.BI] = append(groups[row.BI], i)
	}
	out := make([]v311RefLine, 0)

	type rowText struct {
		row  v311RefLine
		text string
	}
	for _, bi := range order {
		indices := groups[bi]
		rows := make([]v311RefLine, 0, len(indices))
		for _, i := range indices {
			rows = append(rows, raw[i])
		}
		labels := make([]*rxMatch, len(rows))
		numbered := make([]int, 0)
		nonblank := 0
		for i, row := range rows {
			labels[i] = rxFind(v311NumberedPrefix, row.Text)
			if labels[i] != nil {
				numbered = append(numbered, v311AtoiGroup(labels[i], 1))
			}
			if strings.TrimSpace(row.Text) != "" {
				nonblank++
			}
		}
		consecutive := 0
		for i := 1; i < len(numbered); i++ {
			if numbered[i] == numbered[i-1]+1 {
				consecutive++
			}
		}
		shadow := len(numbered) >= 6 &&
			float64(len(numbered))/float64(v311MaxInt(1, nonblank)) >= .80 &&
			float64(consecutive)/float64(v311MaxInt(1, len(numbered)-1)) >= .90
		if !shadow {
			out = append(out, rows...)
			continue
		}
		pending := make([]rowText, 0)
		emit := func(part []rowText) {
			if len(part) == 0 {
				return
			}
			texts := make([]string, 0, len(part))
			for _, item := range part {
				texts = append(texts, strings.TrimSpace(item.text))
			}
			text := strings.Join(texts, "\n") + "\n"
			seed := referenceSeed(text)
			anyHeader, anyRefHeader, anyStructure, anyIsland := false, false, false, false
			for _, item := range part {
				anyHeader = anyHeader || item.row.Header
				anyRefHeader = anyRefHeader || item.row.RefHeader
				anyStructure = anyStructure || item.row.Structure
				anyIsland = anyIsland || item.row.Island
			}
			out = append(out, v311RefLine{
				BI:              part[0].row.BI,
				A:               part[0].row.A,
				Z:               part[len(part)-1].row.Z,
				Text:            text,
				Seed:            seed,
				Header:          anyHeader,
				RefHeader:       anyRefHeader,
				Structure:       anyStructure,
				Prose:           v311HardProse(text, seed) || v311BodyAfterCitation(text),
				Island:          anyIsland || v311Clause(text),
				PrintedLineView: true,
			})
		}
		for i, row := range rows {
			text := row.Text
			if labels[i] != nil {
				text = tailFrom(row.Text, labels[i].End)
			}
			s := v311Norm(text)
			labelled := rxHas(entryLabel, s)
			stop := row.Structure || row.Island || row.RefHeader ||
				(row.Header && s != "" && v311UpperFirst(s) && len(strings.Fields(s)) <= 8 && !labelled && !v311Fields(s)) ||
				rxFull(bodyPattern, normalizedTitle(s)) ||
				v311Narrative(s) ||
				(!v311Author(v311Payload(s)) && rxHas(`[.!?]\s*$`, s) && v311Clause(s))
			if stop {
				emit(pending)
				pending = nil
				emit([]rowText{{row: row, text: text}})
				continue
			}
			if labelled {
				emit(pending)
				pending = []rowText{{row: row, text: text}}
				continue
			}
			if len(pending) > 0 {
				currentParts := make([]string, 0, len(pending))
				for _, item := range pending {
					currentParts = append(currentParts, strings.TrimSpace(item.text))
				}
				current := strings.Join(currentParts, " ")
				complete := rxHas(ocrYear, current) && v311Author(v311Payload(current)) && v311Fields(current)
				tail := rxHas(v311AccessTailPattern, s)
				if complete && !tail {
					emit(pending)
					pending = nil
					emit([]rowText{{row: row, text: text}})
				} else {
					pending = append(pending, rowText{row: row, text: text})
				}
			} else {
				emit([]rowText{{row: row, text: text}})
			}
		}
		emit(pending)
	}
	return out
}

func v311AtoiGroup(m *rxMatch, group int) int {
	value := 0
	for _, r := range m.Group(group) {
		if r < '0' || r > '9' {
			continue
		}
		value = value*10 + int(r-'0')
	}
	return value
}

func v311MaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// v311ReferenceAdditions implements _v311_reference_additions.
func v311ReferenceAdditions(blocks []Block) []Removal {
	rows := v311DetectionRows(blocks)
	flags := make([]v311Class, len(rows))

	classify := func(r v311RefLine) v311Class {
		s := v311Norm(r.Text)
		p := v311Payload(s)
		year := rxFind(ocrYear, p)
		named := year != nil && year.Start < 650 && v311Author(p)
		fields := v311Fields(p)
		strong := !v311Narrative(s) && ((named && fields) || r.Seed)
		label := rxHas(entryLabel, s)
		repeated := rxHas(v311RepeatedYearPattern, s) && fields
		institution := year != nil &&
			rxHas(v311InstitutionPattern, p) &&
			!v311Narrative(s)
		weak := strong || (named && !v311Narrative(s)) || repeated || institution ||
			v311FormalIdentifierRecord(p) || v311AccessedURLRecord(p)
		continuation := fields &&
			(rxHas(v311ContinuationLower, s) || rxHas(v311ContinuationURL, s) ||
				rxHas(v311ContinuationDOI, s) ||
				(year == nil && rxHas(v311ContinuationFields, s)) || repeated) &&
			!v311Narrative(s) && !rxHas(verbPattern, s)
		publicationTail := v311Fields(s) &&
			((rxHas(doiPattern, s) && rxHas(locatorPattern, s)) || rxHas(v311PubTailStart, s)) &&
			!r.Prose
		doiRecord := v311Author(p) && rxHas(doiPattern, s) && !v311Narrative(s)
		pageTail := rxFull(v311PageTailPattern, s)
		weak = weak ||
			(year == nil && v311Author(p) && fields && !r.Prose) ||
			continuation || publicationTail || doiRecord || pageTail ||
			(label && v311Author(p) && fields && !v311Narrative(s)) ||
			(rxHas(v311BracketListItem, p) &&
				v311Author(rxReplace(v311BracketListItem, "", p)) && fields && year != nil)
		bibliographicIsland := protected(s) && strong && !rxHas(bioPattern, s)
		words := rxAll(v311ClippedAuthorWord, s)
		clippedAuthors := year == nil &&
			strings.Count(s, ",") >= 3 &&
			len(rxAll(v311ClippedInitial, s)) >= 3 &&
			len(words) > 0 &&
			v311AllClippedWords(words)
		metadata := rxFull(v311FootPattern, s) || clippedAuthors || bibliographicIsland ||
			(r.Island && (rxHas(v311CopyrightPattern, s) || rxFull(v311FootPattern, s)))
		if metadata && !r.Structure {
			return v311Class{metadata: true}
		}
		if r.PrintedLineView && s != "" && !r.RefHeader {
			if !(label && named && fields) {
				return v311Class{hard: true}
			}
		}
		hard := r.Structure || r.RefHeader ||
			rxHas(bioPattern, s) || rxHas(v311EndPattern, s) ||
			rxFull(bodyPattern, normalizedTitle(s)) ||
			(v311LineBodyText(r.Text) && !fields) ||
			v311Narrative(s) || v311BodyAfterCitation(s) ||
			(protected(s) && !bibliographicIsland) ||
			(r.Island && !metadata) ||
			(r.Header && !strong && !weak) ||
			(r.Prose && !strong && !weak) ||
			(!weak && !metadata && s != "")
		return v311Class{strong: strong, weak: weak, metadata: metadata, hard: hard}
	}

	for i, row := range rows {
		flags[i] = classify(row)
	}

	runs := make([]Removal, 0)
	buf := make([]int, 0)
	headed := false

	// Dominant homogeneous bibliography blocks are evidence in their own right.
	order := make([]int, 0)
	groups := map[int][]int{}
	for i, row := range rows {
		if _, ok := groups[row.BI]; !ok {
			order = append(order, row.BI)
		}
		groups[row.BI] = append(groups[row.BI], i)
	}
	for _, bi := range order {
		indices := append([]int(nil), groups[bi]...)
		text := textOf(blocks[bi])
		if v311ReviewListContext(blocks, bi) || rxHas(v311BookReviewText, v311PrefixRunes(text, 400)) {
			continue
		}
		if v311HasAnyStructure(rows, indices) {
			continue
		}
		captions := make([]int, 0)
		for _, i := range indices {
			if rxHas(v311FigTablePattern, rows[i].Text) {
				captions = append(captions, i)
			}
		}
		if len(captions) > 0 {
			starts := make([]int, 0)
			for _, i := range indices {
				if rows[i].RefHeader && i > captions[0] {
					starts = append(starts, i)
				}
			}
			if len(starts) == 0 {
				continue
			}
			start := starts[len(starts)-1]
			end := indices[len(indices)-1] + 1
			for _, i := range captions {
				if i > start {
					end = i
					break
				}
			}
			filtered := make([]int, 0)
			for _, i := range indices {
				if i > start && i < end {
					filtered = append(filtered, i)
				}
			}
			indices = filtered
		}
		active := make([]int, 0)
		qualified := make([]int, 0)
		for _, i := range indices {
			if strings.TrimSpace(rows[i].Text) != "" && !flags[i].metadata && !rows[i].RefHeader {
				active = append(active, i)
				if flags[i].weak && !flags[i].hard {
					qualified = append(qualified, i)
				}
			}
		}
		printed := false
		for _, i := range active {
			if rows[i].PrintedLineView {
				printed = true
				break
			}
		}
		if printed {
			active = v311FilterActivePrinted(rows, active)
			allowed := make(map[int]bool, len(active))
			for _, i := range active {
				allowed[i] = true
			}
			filtered := make([]int, 0)
			for _, i := range qualified {
				if allowed[i] {
					filtered = append(filtered, i)
				}
			}
			qualified = filtered
		}
		strongCount := 0
		for _, i := range qualified {
			if flags[i].strong {
				strongCount++
			}
		}
		ratio := .75
		if printed {
			ratio = .70
		}
		if len(qualified) < 8 || strongCount < 5 ||
			float64(len(qualified))/float64(v311MaxInt(1, len(active))) < ratio {
			continue
		}
		for _, i := range qualified {
			row := rows[i]
			runs = append(runs, Removal{
				Block: row.BI, Start: row.A, End: row.Z,
				Kind: "references", Reason: "v311_dominant_bibliography_block_positive_row",
				Section: "v311:dense_block",
			})
		}
	}

	flush := func() {
		if len(buf) == 0 {
			return
		}
		strong := make([]int, 0)
		active := make([]int, 0)
		for _, i := range buf {
			if flags[i].strong {
				strong = append(strong, i)
			}
			if strings.TrimSpace(rows[i].Text) != "" && !flags[i].metadata && !rows[i].RefHeader {
				active = append(active, i)
			}
		}
		minimum := 5
		if headed {
			minimum = 2
		}
		if len(strong) < minimum ||
			(!headed && float64(len(strong))/float64(v311MaxInt(1, len(active))) < .55) {
			buf = nil
			return
		}
		first, last := strong[0], strong[len(strong)-1]
		if v311ReviewListContext(blocks, rows[first].BI) {
			buf = nil
			return
		}
		following := -1
		for j := buf[len(buf)-1] + 1; j < len(rows); j++ {
			if strings.TrimSpace(rows[j].Text) != "" {
				following = j
				break
			}
		}
		terminal := following < 0 ||
			rows[following].Structure ||
			rxHas(v311EndPattern, v311Norm(rows[following].Text)) ||
			(following >= 0 && (v311Contains(filterKinds, lineKind(rows[following].Text)) ||
				rxHas(v311AppendicesPattern, rows[following].Text)))
		numbered := true
		for _, i := range active {
			if !rxHas(entryLabel, v311Norm(rows[i].Text)) {
				numbered = false
				break
			}
		}
		longRun := len(strong) >= 12 &&
			v311Distinct(rows, strong, func(r v311RefLine) int { return r.BI }) >= 3 &&
			v311Distinct(rows, strong, func(r v311RefLine) int {
				p, _ := pageOf(blocks[r.BI])
				return p
			}) >= 2
		if !headed && !terminal && !numbered && !longRun {
			buf = nil
			return
		}
		opaque, gapchars := 0, 0
		for i := first; i <= last; i++ {
			row := rows[i]
			if flags[i].hard {
				buf = nil
				return
			}
			if flags[i].strong || flags[i].weak {
				opaque = 0
				gapchars = 0
			} else if strings.TrimSpace(row.Text) != "" && !flags[i].metadata {
				opaque++
				gapchars += runeLen(row.Text)
				if opaque > 2 || gapchars > 650 {
					buf = nil
					return
				}
			}
		}
		start := first
		end := last + 1
		for start > buf[0] &&
			(flags[start-1].weak || flags[start-1].metadata ||
				strings.TrimSpace(rows[start-1].Text) == "" || rows[start-1].RefHeader) {
			start--
		}
		for end <= buf[len(buf)-1] &&
			(flags[end].weak || flags[end].metadata || strings.TrimSpace(rows[end].Text) == "") {
			end++
		}
		for i := start; i < end; i++ {
			if flags[i].metadata || strings.TrimSpace(rows[i].Text) == "" {
				continue
			}
			row := rows[i]
			runs = append(runs, Removal{
				Block: row.BI, Start: row.A, End: row.Z,
				Kind: "references", Reason: "v311_confirmed_bibliography_container",
				Section: "v311:references",
			})
		}
		buf = nil
	}

	previous := -1
	for i, row := range rows {
		if previous >= 0 {
			p1, ok1 := pageOf(blocks[row.BI])
			p2, ok2 := pageOf(blocks[rows[previous].BI])
			if ok1 && ok2 && p1-p2 > 2 {
				flush()
				headed = false
			}
		}
		if row.RefHeader {
			flush()
			headed = true
			buf = []int{i}
			previous = i
			continue
		}
		if flags[i].hard {
			flush()
			headed = false
			previous = i
			continue
		}
		buf = append(buf, i)
		previous = i
	}
	flush()
	return runs
}

func v311AllClippedWords(words []*rxMatch) bool {
	for _, m := range words {
		w := m.Group(0)
		if w == "" {
			continue
		}
		if v311UpperFirst(w) {
			continue
		}
		if v311Particles[strings.ToLower(w)] {
			continue
		}
		return false
	}
	return true
}

func v311HasAnyStructure(rows []v311RefLine, indices []int) bool {
	for _, i := range indices {
		if rows[i].Structure {
			return true
		}
	}
	return false
}

func v311FilterActivePrinted(rows []v311RefLine, active []int) []int {
	out := make([]int, 0, len(active))
	for _, i := range active {
		s := v311Norm(rows[i].Text)
		p := v311Payload(rows[i].Text)
		if rxHas(entryLabel, s) && v311AlphaFirst(p) {
			out = append(out, i)
		}
	}
	return out
}

func v311Distinct(rows []v311RefLine, indices []int, key func(v311RefLine) int) int {
	seen := map[int]bool{}
	for _, i := range indices {
		seen[key(rows[i])] = true
	}
	return len(seen)
}

func v311PrefixRunes(s string, n int) string {
	length := runeLen(s)
	if length > n {
		length = n
	}
	return runeSlice(s, 0, length)
}

// v311DisclosureSentences reproduces _v311_disclosure_sentences.
func v311DisclosureSentences(text string) []v311DisclosureSentence {
	out := make([]v311DisclosureSentence, 0)
	start := 0
	for _, m := range rxAll(v311SentenceBoundary, text) {
		token := rxFind(v311TokenAtEnd, runeSlice(text, 0, m.End))
		if token != nil {
			word := token.Group(1)
			if runeLen(word) == 1 || v311Abbreviations[strings.ToLower(word)] {
				continue
			}
		}
		out = append(out, v311DisclosureSentence{
			Start: start, End: m.End, Text: runeSlice(text, start, m.End),
		})
		start = m.End
	}
	if start < runeLen(text) {
		out = append(out, v311DisclosureSentence{
			Start: start, End: runeLen(text), Text: tailFrom(text, start),
		})
	}
	return out
}

func v311DisclosureBody(text string) bool {
	for _, sentence := range v311DisclosureSentences(text) {
		s := strings.TrimSpace(sentence.Text)
		if rxHas(v311SocialPattern, s) || rxHas(v311OriginPattern, s) {
			continue
		}
		if len(rxAll(`[^\W\d_]+`, s)) >= 4 ||
			rxHas(prosePattern, s) ||
			v311HardProse(s, false) {
			return true
		}
	}
	return false
}

// v311DisclosureChanges implements _v311_disclosure_changes.
func v311DisclosureChanges(blocks []Block) ([]Removal, []Removal) {
	additions := make([]Removal, 0)
	keeps := make([]Removal, 0)
	active := ""
	pending := make([]Removal, 0)
	lastBI := -1
	inRefs := false

	for bi, b := range blocks {
		pos := 0
		for _, line := range splitLines(textOf(b), true) {
			a := pos
			pos += runeLen(line)

			head := rxFind(v311DiscHeadPattern, line)
			headEnd := 0
			headName := ""
			if head != nil {
				headEnd = head.End
				headName = strings.ToLower(head.Group(1))
			}

			if lineKind(line) == "references" {
				inRefs = true
			} else if rxFull(bodyPattern, normalizedTitle(line)) {
				inRefs = false
			}
			if inRefs && head != nil && strings.HasPrefix(headName, "funding") &&
				strings.TrimSpace(tailFrom(line, headEnd)) != "" {
				head = nil
				headEnd = 0
				active = ""
				pending = nil
			}
			if lastBI >= 0 {
				p1, ok1 := pageOf(b)
				p2, ok2 := pageOf(blocks[lastBI])
				if ok1 && ok2 && p1-p2 > 1 {
					active = ""
					pending = nil
				}
			}
			lastBI = bi
			if head != nil && active != "" && headName == "funding" &&
				!rxHas(`(?i)^(?:[.:—–-]|$|Open access|Funded)`, tailFrom(line, headEnd)) {
				head = nil
				headEnd = 0
			}

			lineBody := line
			offset := a
			if head != nil {
				active = headName
				pending = []Removal{{Block: bi, Start: a, End: a + headEnd}}
				lineBody = tailFrom(line, headEnd)
				offset = a + headEnd
			}
			if active == "" {
				continue
			}
			if v311DisclosureBody(lineBody) {
				keeps = append(keeps, Removal{Block: bi, Start: a, End: pos})
				active = ""
				pending = nil
				continue
			}
			if protected(lineBody) || rxHas(narrativePattern, lineBody) {
				active = ""
				pending = nil
				continue
			}
			if strings.TrimSpace(lineBody) == "" {
				continue
			}

			partsOverride := false
			if !rxHas(v311OriginPattern, lineBody) &&
				!protected(lineBody) &&
				!rxHas(narrativePattern, lineBody) &&
				rxHas(v311SocialPattern, lineBody) &&
				!rxHas(v311ForthcomingPattern, lineBody) {
				partsOverride = true
			}

			if rxFull(bodyPattern, normalizedTitle(lineBody)) ||
				lineKind(lineBody) == "structure" ||
				rxHas(v311EndPattern, strings.TrimSpace(lineBody)) ||
				(head == nil && (v311Contains(filterKinds, lineKind(lineBody)) || v311UnknownTitle(lineBody))) {
				active = ""
				pending = nil
				continue
			}

			var parts []*rxMatch
			if partsOverride {
				parts = rxAll(v311WholeText, lineBody)
			} else {
				parts = rxAll(v311DisclosureParts, lineBody)
			}
			accepted := false
			for _, m := range parts {
				t := m.Group(0)
				left := offset + m.Start
				right := offset + m.End
				if strings.TrimSpace(t) == "" {
					continue
				}
				origin := rxHas(v311OriginPattern, t)
				if origin {
					keeps = append(keeps, Removal{Block: bi, Start: left, End: right})
					continue
				}
				if protected(t) || hasStructure(t) {
					continue
				}
				cue := rxHas(v311SocialPattern, t)
				fundingList := strings.Contains(active, "funding information") &&
					head != nil &&
					headEnd < runeLen(line) &&
					rxHas(v311FundingListPattern, t)
				prefix := strings.TrimSpace(runeSlice(lineBody, 0, m.Start))
				if (cue || fundingList) &&
					!(!accepted && m.Start > 0 && head == nil && !rxHas(v311SocialPattern, prefix)) {
					accepted = true
					additions = append(additions, Removal{
						Block: bi, Start: left, End: right,
						Kind: "acknowledgements", Reason: "v311_explicit_disclosure_positive_cue",
						Section: "v311:disclosure",
					})
				}
			}
			if accepted {
				for _, p := range pending {
					additions = append(additions, Removal{
						Block: p.Block, Start: p.Start, End: p.End,
						Kind: "acknowledgements", Reason: "v311_explicit_disclosure_heading",
						Section: "v311:disclosure",
					})
				}
				pending = nil
			} else if head == nil {
				anyOrigin := false
				for _, m := range parts {
					if rxHas(v311OriginPattern, m.Group(0)) {
						anyOrigin = true
						break
					}
				}
				if !anyOrigin {
					active = ""
					pending = nil
				}
			}
		}
	}
	return additions, keeps
}
