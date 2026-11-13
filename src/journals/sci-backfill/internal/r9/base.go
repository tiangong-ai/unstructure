package r9

import (
	"strconv"
	"strings"
	"unicode"
)

const inlineMath = `\$[^$\n]*(?:=|\\frac|\\sum|\\int|\\partial|\\propto)[^$\n]*\$`
const numberedStart = `(?m)^\s*(?:\[(\d{1,4})\]|\((\d{1,4})\)|(\d{1,4})[.]?\s*)(?=\s*[A-ZÀ-ÖØ-Þ])`

var punctuation = strings.NewReplacer("（", "(", "）", ")", "，", ",", "；", ";")

func protected(t string) bool {
	return rxHas(nonTargetPattern, t) || rxHas(narrativePattern, t) || rxHas(extraProtected, t)
}

func normalizedTitle(t string) string {
	t = rxReplace(`^\s*#{1,6}\s*`, "", strings.TrimSpace(t))
	t = rxReplace(`^(?:\d+(?:\.\d+)*[.)]?|[IVXLCDM]+[.)])\s*`, "", t)
	t = rxReplace(`^\d+Q\d+\s*`, "", t)
	return strings.ToLower(strings.Trim(rxReplace(`\s+`, " ", t), " :."))
}

func lineKind(t string) string {
	s := strings.TrimSpace(t)
	n := normalizedTitle(s)
	if runeLen(s) <= 180 {
		for _, kind := range filterKinds {
			if rxFull(filterPatterns[kind], n) {
				return kind
			}
		}
		if rxFull(bodyPattern, n) {
			return "body"
		}
	}
	if protected(s) || rxHas(`^\.?\s*[A-Z][\w-]+ et al\.:`, s) || rxHas(`(?i)^(?:fig(?:ure)?\.?|table)\s*[A-Z]?\d`, s) {
		return "body"
	}
	if rxHas(`(?i)^\s*(?:\$\$|\\\[|\\begin\{|<table\b|\|.*\|\s*$)`, s) {
		return "structure"
	}
	social := rxHas(`(?i)\b(?:thank\w*|gratitude|grateful\w*|acknowledg\w*|appreciat\w*|indebted|assistance|declare\w*|conflicts?|competing)\b`, s)
	funding := rxHas(`(?i)^this (?:work|study|research) .{0,75}\b(?:support(?:ed)?|funded|sponsored)\b`, s)
	if (rxHas(prosePattern, s) && !social && !funding) ||
		rxHas(`(?i)\b(?:received (?:the|his|her) (?:b\.?s|m\.?s|ph\.?d)|was born|is (?:an? )?(?:associate |assistant |full )?professor)\b`, s) {
		return "body"
	}
	return ""
}

func inlineHeading(line string) (string, int) {
	for _, kind := range filterKinds {
		pattern := filterPatterns[kind]
		m := rxFind(`(?i)^\s*(?:#{1,6}\s*)?(`+pattern+`)(?:[.:—–-]|\x01/)?\s*`, line)
		if m == nil {
			continue
		}
		rest := []rune(tailFrom(line, m.End))
		if len(rest) == 0 || !unicode.IsUpper(rest[0]) {
			continue
		}
		if kind == "references" {
			if anchor(string(rest)) && rxHas(yearPattern, string(rest)) {
				return kind, m.End
			}
		} else if disclosureReason(string(rest), kind, "") != "" {
			return kind, m.End
		}
	}
	return "", 0
}

type piece struct {
	block, start, end int
	text, kind        string
}

func piecesFor(blocks []Block) []piece {
	pieces := []piece{}
	inReferences := false
	tables, dollars, brackets, environments := 0, 0, 0, 0
	for i, block := range blocks {
		text := textOf(block)
		if tables > 0 || dollars > 0 || brackets > 0 || environments > 0 || hasStructure(text) || rxHas(`(?i)</table\s*>|\\end\{|\\\]`, text) {
			pieces = append(pieces, piece{i, 0, runeLen(text), text, "structure"})
			inReferences = false
			tables = max(0, tables+len(rxAll(`(?i)<table\b`, text))-len(rxAll(`(?i)</table\s*>`, text)))
			dollars = (dollars + strings.Count(text, "$$")) % 2
			brackets = max(0, brackets+strings.Count(text, "\\[")-strings.Count(text, "\\]"))
			environments = max(0, environments+strings.Count(text, "\\begin{")-strings.Count(text, "\\end{"))
			continue
		}
		cuts := map[int]bool{0: true}
		kinds := map[int]string{}
		pos := 0
		for _, line := range splitLines(text, true) {
			kind := lineKind(line)
			inlineKind, inlineEnd := "", 0
			if !isFilter(kind) {
				inlineKind, inlineEnd = inlineHeading(line)
			}
			heading := kind
			if inlineKind != "" {
				heading = inlineKind
			}
			if heading == "references" {
				inReferences = true
			} else if heading != "" {
				inReferences = false
			}
			if heading == "" && inReferences && len(numberedLines(line)) > 0 && !anchor(line) {
				kind, inReferences = "body", false
			}
			if inlineKind != "" {
				cuts[pos], cuts[pos+inlineEnd], kinds[pos] = true, true, inlineKind
			} else if kind != "" {
				cuts[pos], kinds[pos] = true, kind
				if isFilter(kind) {
					cuts[pos+runeLen(line)] = true
				}
			}
			pos += runeLen(line)
		}
		cuts[runeLen(text)] = true
		bounds := sortedBounds(cuts)
		for j := 1; j < len(bounds); j++ {
			a, z := bounds[j-1], bounds[j]
			pieces = append(pieces, piece{i, a, z, runeSlice(text, a, z), kinds[a]})
		}
	}
	return pieces
}

func anchor(t string) bool {
	s := punctuation.Replace(strings.TrimSpace(t))
	if protected(s) {
		return false
	}
	if label := rxFind(`^(?:\[\s*[A-Z]?\d{1,4}\s*\]|\(\d{1,4}\)|\d{1,4}[.]?|[A-Z][.])\s*`, s); label != nil {
		rest := tailFrom(s, label.End)
		surname := `[A-ZÀ-ÖØ-Þ][\w’\x27-]{1,45}`
		initials := `(?:[A-Z][.](?:-[A-Z][.])?\s*){1,5}`
		particles := `(?:(?:van|von|de|der|den|dos|da|di|la|le)\s+){0,3}`
		return rxHas("^"+initials+particles+surname, rest) ||
			rxHas("^"+surname+`(?:,\s*[A-Z]|\s+[A-Z]{1,5}\b|\s+et al\b|\s*\((?:18|19|20)\d{2})`, rest) ||
			rxHas("^"+surname+`(?:\s+`+surname+`){1,3}(?:,\s*[A-Z]|\s+[A-Z]{1,5}\b)`, rest) ||
			rxHas(`^(?:[A-Z][\w-]+\s+){1,7}[A-Z][\w-]+\s*\((?:18|19|20)\d{2}`, rest) ||
			(rxHas(`^[A-Z]{2,12}\b`, rest) && rxHas(yearPattern, rest) &&
				rxHas(`(?i)\b(?:publication|report|working group|council|commission|team|institute|university|assessment|roadmap)\b`, rest))
	}
	if rxHas(`^(?:[A-Z][.]\s*){1,4}[A-ZÀ-ÖØ-Þ][\w’\x27-]+\s*[,;]`, s) ||
		rxHas(`^[A-ZÀ-ÖØ-Þ][\w’\x27-]{1,45},\s*[A-Z]`, s) ||
		rxHas(`^[A-ZÀ-ÖØ-Þ][\w’\x27-]{1,45}\s+[A-Z]{1,4},`, s) {
		return true
	}
	m := rxFind(`^([A-Z][^\n]{0,90}?)(?:,\s*|\.\s*)\(?(?:18|19|20)\d{2}\b`, s)
	if m == nil {
		return false
	}
	prefix := m.Group(1)
	return rxFull(`[A-Z.\s&-]{2,45}`, prefix) ||
		rxHas(`\b(?:Bureau|Agency|Organization|Organisation|Commission|Council|Society|Committee|Office|Institute)\b`, prefix) || prefix == "Anonymous"
}

func unknownTitle(t string) bool {
	s := strings.TrimSpace(t)
	return runeLen(s) < 150 && len(strings.Fields(s)) < 20 && !strings.Contains(s, "\n") &&
		!rxHas(`[.,;:]$|\b(?:18|19|20)\d{2}\b|[,;]`, s) && !anchor(s)
}

func hasStructure(t string) bool {
	return rxHas(`(?i)\$\$|\\(?:begin\{|tag\{|\[)|<table\b|(?m:^\s*\|.*\|\s*$)`, t)
}

func numberedLines(t string) []string {
	var lines []string
	for _, line := range splitLines(t, false) {
		if rxHas(`^\s*(?:\[\s*[A-Z]?\d{1,4}\s*\]|\(\d{1,4}\)|\d{1,4}[.]?\s*)\s*[A-ZÀ-ÖØ-Þ]`, line) {
			lines = append(lines, line)
		}
	}
	return lines
}

func referenceReason(text, previous string) string {
	s := punctuation.Replace(strings.TrimSpace(text))
	if s == "" || hasStructure(s) || rxHas(prosePattern, s) || protected(s) {
		return ""
	}
	if numbered := numberedLines(s); len(numbered) >= 2 {
		for _, line := range numbered {
			if !anchor(line) {
				return ""
			}
		}
	}
	years := len(rxAll(yearPattern, s))
	pub := rxHas(publicationPattern, s)
	if anchor(s) && (years > 0 || pub) {
		return "reference_entry"
	}
	later := 0
	lines := splitLines(s, false)
	for _, line := range lines[1:] {
		if anchor(line) {
			later++
		}
	}
	if previous == "" {
		if later >= 2 && years >= 2 && pub {
			return "reference_clipped_start"
		}
		return ""
	}
	if rxFull(`\d{1,4}[.]?`, s) && rxHas(`(?:[-–]|pp?\.)\s*$`, previous) {
		return "reference_page_range_continuation"
	}
	if unknownTitle(s) {
		return ""
	}
	prev := strings.TrimRightFunc(previous, unicode.IsSpace)
	if (pub && later > 0) ||
		(!rxHas(`[.!?]\s*$`, prev) && rxHas(`^[a-z(]`, s) && pub && (years > 0 || rxHas(`\d+\s*[,;:]\s*\d+`, s))) ||
		((strings.HasSuffix(prev, "-") || strings.HasSuffix(prev, "–") || strings.HasSuffix(prev, ",")) && (years > 0 || pub)) {
		return "reference_continuation"
	}
	return ""
}

func disclosureReason(text, kind, previous string) string {
	s := strings.TrimSpace(text)
	if s == "" || hasStructure(s) || protected(s) || len(numberedLines(s)) > 0 || rxHas(inlineMath, s) {
		return ""
	}
	if kind == "acknowledgements" && previous != "" && strings.HasSuffix(strings.TrimRightFunc(previous, unicode.IsSpace), "-") &&
		rxHas(`^[a-z]`, s) && runeLen(s) < 1000 {
		return "acknowledgement_word_continuation"
	}
	opening := s
	for _, b := range rxAll(`[.!?]\s+(?=[A-Z])`, s) {
		m := rxFind(`[A-Za-z.]+$`, runeSlice(s, 0, b.Start))
		word := ""
		if m != nil {
			word = m.Group(0)
		}
		if rxFull(`(?:[A-Z]\.)*[A-Z]`, word) || strings.Contains("|Dr|Prof|Mr|Mrs|Ms|No|Vol|pp|", "|"+word+"|") {
			continue
		}
		opening = runeSlice(s, 0, b.End)
		break
	}
	if p := disclosureCues[kind]; p != "" && rxHas(p, opening) {
		if kind == "acknowledgements" {
			return "acknowledgement_continuation"
		}
		return "section_body"
	}
	return ""
}

func strictUnheaded(t string) bool {
	if protected(t) || hasStructure(t) || rxHas(`(?i)\b(?:we|our|this study|found that|showed that|reported that|results show)\b`, t) {
		return false
	}
	starts := rxAll(numberedStart, t)
	if len(starts) < 2 || strings.TrimSpace(runeSlice(t, 0, starts[0].Start)) != "" {
		return false
	}
	last := -1
	for i, m := range starts {
		number := ""
		for j := 1; j <= 3; j++ {
			if number = m.Group(j); number != "" {
				break
			}
		}
		n, err := strconv.Atoi(number)
		if err != nil || (i > 0 && n != last+1) {
			return false
		}
		last = n
		end := runeLen(t)
		if i+1 < len(starts) {
			end = starts[i+1].Start
		}
		entry := runeSlice(t, m.Start, end)
		if !anchor(entry) || !rxHas(yearPattern, entry) ||
			!rxHas(`(?i)\b\d{1,4}\s*(?:\(\s*\d{1,3}\s*\))?\s*[,;:]\s*\d{1,5}(?:\s*[-–]\s*\d{1,5})?|\bpp?\.\s*\d`, entry) ||
			strings.Contains(strings.TrimSpace(entry), "\n\n") {
			return false
		}
		rest := normalizeReference(runeSlice(t, m.End, end))
		if !authorShape(rest) && !rxHas(`^(?:[A-Z][.](?:-[A-Z][.])?\s*){1,5}[^\W\d_][\w’\x27-]{1,45}`, rest) {
			return false
		}
	}
	return true
}

func baseClean(blocks []Block) Result {
	if !pageOrder(blocks) {
		return retainAll(blocks, "unreliable_page_order_document_retained")
	}
	r := Result{Cleaned: []Block{}, Mapping: []Mapping{}, Reviews: []map[string]any{}}
	pieces := piecesFor(blocks)
	removed := map[int]Span{}
	sections := 0
	for i, p := range pieces {
		if !isFilter(p.kind) {
			continue
		}
		if _, ok := removed[i]; ok {
			continue
		}
		previous := ""
		lastPage, _ := pageOf(blocks[p.block])
		members := map[int]string{}
		j := i + 1
		for ; j < len(pieces); j++ {
			next := pieces[j]
			if next.kind != "" {
				break
			}
			if strings.TrimSpace(next.text) == "" {
				continue
			}
			page, _ := pageOf(blocks[next.block])
			if page < lastPage || page-lastPage > 1 {
				break
			}
			reason := ""
			if p.kind == "references" {
				reason = referenceReason(next.text, previous)
			} else {
				reason = disclosureReason(next.text, p.kind, previous)
			}
			if reason == "" {
				break
			}
			members[j] = reason
			previous, lastPage = next.text, page
		}
		if len(members) == 0 {
			r.Reviews = append(r.Reviews, review(p.block, p.start, p.kind, "unconfirmed_section_retained"))
			continue
		}
		removed[i] = exclude(p.start, p.end, p.kind, "section_heading", sections, p.text)
		for index, reason := range members {
			next := pieces[index]
			removed[index] = exclude(next.start, next.end, p.kind, reason, sections, next.text)
		}
		sections++
		if j < len(pieces) && pieces[j].kind == "" {
			next := pieces[j]
			r.Reviews = append(r.Reviews, review(next.block, next.start, p.kind, "unconfirmed_continuation_retained"))
		}
	}
	for i, p := range pieces {
		if _, ok := removed[i]; !ok && p.kind == "" && strictUnheaded(p.text) {
			removed[i] = exclude(p.start, p.end, "references", "standalone_numbered_bibliography", sections, p.text)
			sections++
		}
	}
	next := 0
	for bi, block := range blocks {
		m := outputMapping(block, bi)
		cursor := 0
		for next < len(pieces) && pieces[next].block == bi {
			p := pieces[next]
			if span, ok := removed[next]; ok {
				m.Spans = append(m.Spans, span)
			} else {
				m.Spans = append(m.Spans, keep(p.start, p.end, cursor))
				cursor += p.end - p.start
			}
			next++
		}
		appendOutput(&r, block, m)
	}
	if hasBody(blocks) && !hasBody(r.Cleaned) {
		return retainAll(blocks, "empty_output_guard_all_exclusions_cancelled")
	}
	return r
}
