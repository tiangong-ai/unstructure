package r9

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

func bibliographyType(text string) string {
	s := strings.TrimSpace(text)
	if m := rxFind(v39Label, s); m != nil {
		s = tailFrom(s, m.End)
	}
	if m := rxFind(v39Sub, s); m != nil {
		s = tailFrom(s, m.End)
	}
	if s == "" || protected(s) || hasStructure(s) || rxHas(prosePattern, s) || rxHas(`(?i)^(?:model|instrument|apparatus|equipment|the\b|these\b|this\b|see\b)`, s) || rxHas(v39Tech, s) {
		return ""
	}
	author := anchor("[1] "+s) || anchor(s)
	year := rxHas(yearPattern, s)
	standard := rxFind(`^(?:ISO(?:/IEC)?|IEC|IEEE|ASTM|ANSI|EN|DIN|BS|JIS|GB(?:/T)?)\s+\d{2,7}(?:[-.:/]\d{1,7})*\s*[:;,]\s*\S`, s)
	if standard != nil && year && len(strings.Fields(tailFrom(s, standard.End))) >= 3 {
		return "standard"
	}
	if year && rxHas(`(?i)\b(?:patent|pat\.?(?:\s+no\.?)?|PCT\s+Int\.?)\b`, s) && rxHas(`\b(?:US|EP|WO|CN|JP|GB|DE)[-\s]?\d{5,}[A-Z]?\d*\b`, s) {
		return "patent"
	}
	if author && year && (rxHas(`\bISBN(?:-1[03])?\s*:?\s*[0-9Xx][0-9Xx\s-]{8,20}`, s) || rxHas(`(?i)\b(?:press|publisher|publishing|springer|wiley|elsevier|routledge)\b`, s)) {
		return "book_or_chapter"
	}
	if author && year && rxHas(`(?i)\b(?:ph\.?d\.?|doctoral|master.?s?|m\.?sc\.?)\s+(?:thesis|dissertation)\b`, s) && rxHas(`(?i)\b(?:university|institute|college)\b`, s) {
		return "thesis"
	}
	organization := author || rxHas(`^[A-Z][\w -]{3,80}[.,]`, s)
	if year && organization && rxHas(`(?i)\b(?:technical|research|working|final)\s+(?:report|paper)\b`, s) && rxHas(`\b(?:no\.?\s*)?[A-Z]{2,12}[- ]\d{2,6}(?:[-/]\d{1,6})*`, s) {
		return "report"
	}
	if year && organization && rxHas(`(?i)https?://\S+`, s) && rxHas(`(?i)\b(?:accessed|retrieved|last visited|cited)\b`, s) {
		return "web"
	}
	if author && year && (rxHas(v39Locator, s) || rxHas(`\b10\.\d{4,9}/\S+`, s) || rxHas(`(?i)\b(?:journal|proceedings)\b`, s)) {
		return "journal_or_proceedings"
	}
	return ""
}

func authorListFragment(text string) bool {
	s := strings.TrimSpace(text)
	if m := rxFind(v39Label, s); m != nil {
		s = tailFrom(s, m.End)
	}
	if runeLen(s) > 350 || !strings.Contains(s, ";") || rxHas(yearPattern, s) || rxHas(publicationPattern, s) || protected(s) || !anchor("[1] "+s) {
		return false
	}
	words := rxAll(`[A-Za-zÀ-ÖØ-öø-ÿ][\w’'-]*`, s)
	if len(words) == 0 {
		return false
	}
	for _, m := range words {
		w := m.Group(0)
		first := []rune(w)[0]
		if !unicode.IsUpper(first) && !strings.Contains("|van|von|de|der|den|dos|da|di|la|le|et|al|", "|"+w+"|") {
			return false
		}
	}
	return true
}

type regionLine struct {
	block, start, end                                 int
	text                                              string
	seed, header, refHeader, structure, prose, island bool
}

type adjustment struct {
	Removal
	section any
}

func structuralBlocks(blocks []Block) map[int]bool {
	structural := map[int]bool{}
	for _, p := range piecesFor(blocks) {
		if p.kind == "structure" {
			structural[p.block] = true
		}
	}
	return structural
}

func joinedLines(rows []regionLine) string {
	var buf strings.Builder
	for _, r := range rows {
		buf.WriteString(r.text)
	}
	return buf.String()
}

func v39Adjustments(blocks []Block, baseline Result) ([]adjustment, []map[string]any) {
	additions := []adjustment{}
	reviews := []map[string]any{}
	active, lastConfirmed := false, false
	var heading regionLine
	var buf []regionLine
	lastNumber, lastPage, lastSub := -1, -1, rune(0)
	structural := structuralBlocks(blocks)
	emitted := map[[2]int]bool{}
	flush := func() {
		if len(buf) == 0 {
			return
		}
		kind := bibliographyType(joinedLines(buf))
		lastConfirmed = kind != ""
		if kind != "" {
			var section any = fmt.Sprintf("v39:%d:%d", heading.block, heading.start)
			for _, sp := range baseline.Mapping[heading.block].Spans {
				if sp.Action == "exclude" && sp.Kind == "references" && sp.Reason == "section_heading" && sp.Start <= heading.start && heading.start < sp.End {
					section = sp.Section
					break
				}
			}
			for _, row := range buf {
				additions = append(additions, adjustment{Removal: Removal{Block: row.block, Start: row.start, End: row.end, Kind: "references", Reason: "v39_" + kind}, section: section})
			}
			key := [2]int{heading.block, heading.start}
			if !emitted[key] {
				emitted[key] = true
				additions = append(additions, adjustment{Removal: Removal{Block: heading.block, Start: heading.start, End: heading.end, Kind: "references", Reason: "v39_confirmed_reference_heading"}, section: section})
			}
		} else {
			reviews = append(reviews, review(buf[0].block, buf[0].start, "references", "unconfirmed_reference_entry_retained_v39"))
		}
		buf = nil
	}
	for bi, b := range blocks {
		page, _ := pageOf(b)
		if structural[bi] || (active && lastPage >= 0 && page-lastPage > 1) {
			flush()
			active, lastNumber, lastSub = false, -1, 0
		}
		lastPage = page
		if structural[bi] {
			continue
		}
		pos := 0
		for _, line := range splitLines(textOf(b), true) {
			row := regionLine{block: bi, start: pos, end: pos + runeLen(line), text: line}
			pos = row.end
			s := strings.TrimSpace(line)
			title := normalizedTitle(rxReplace(`^\s*[■▪•●]\s*`, "", s))
			if rxFull(filterPatterns["references"], title) {
				flush()
				active, heading, lastNumber, lastSub, lastConfirmed = true, row, -1, 0, false
				continue
			}
			if !active {
				continue
			}
			if s == "" {
				flush()
				continue
			}
			if k := lineKind(line); k == "body" || k == "structure" || isFilter(k) {
				flush()
				active, lastNumber, lastSub = false, -1, 0
				continue
			}
			if label := rxFind(v39Label, line); label != nil {
				flush()
				num, _ := strconv.Atoi(rxReplace(`\D`, "", label.Group(0)))
				if lastNumber >= 0 && num != lastNumber+1 {
					reviews = append(reviews, review(bi, row.start, "references", "nonconsecutive_reference_number_retained_v39"))
					lastNumber, lastSub, lastConfirmed = num, 0, false
					continue
				}
				lastNumber, lastSub = num, 0
				if rxHas(`^\s*\(a\)`, tailFrom(line, label.End)) {
					lastSub = 'a'
				}
				buf = []regionLine{row}
				continue
			}
			if sub := rxFind(v39Sub, line); sub != nil {
				flush()
				letter := []rune(sub.Group(1))[0]
				if lastNumber >= 0 && lastConfirmed && lastSub != 0 && letter == lastSub+1 {
					lastSub = letter
					buf = []regionLine{row}
					continue
				}
				reviews = append(reviews, review(bi, row.start, "references", "unconfirmed_reference_subentry_retained_v39"))
				lastSub, lastConfirmed = letter, false
				continue
			}
			if rxFull(`\d{1,5}`, s) {
				flush()
				reviews = append(reviews, review(bi, row.start, "references", "line_number_noise_retained_v39"))
				continue
			}
			if len(buf) > 0 {
				prev := strings.TrimRightFunc(buf[len(buf)-1].text, unicode.IsSpace)
				if !protected(s) && ((!rxHas(`[.!?]\s*$`, prev) && (rxHas(publicationPattern, s) || rxHas(yearPattern, s) || rxHas(v39Locator, s))) ||
					(authorListFragment(joinedLines(buf)) && rxHas(yearPattern, s) && rxHas(v39Locator, s))) {
					buf = append(buf, row)
					continue
				}
			}
			flush()
			active, lastNumber, lastSub = false, -1, 0
		}
	}
	flush()
	return additions, reviews
}
