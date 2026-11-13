package r9

import (
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/text/unicode/norm"
)

const sentenceBreak = `(?<=[.!?])\s+(?=[A-Z])`
const clauseStart = `^[)\]]?([.,;:]?)\s*([A-Za-z][A-Za-z-]*)\b`

func normalizeReference(text string) string {
	text = rxReplace(`^([ \t]*)[Il](\d{1,3})(?=\.)`, "${1}1${2}", text)
	text = rxReplace("(?<=\\w)[´˛`ˆ~¸]+(?=\\w|\\s+[A-Z])", "", text)
	return strings.TrimSpace(punctuation.Replace(norm.NFKC.String(text)))
}

func payload(text string) string {
	s := normalizeReference(text)
	if m := rxFind(entryLabel, s); m != nil {
		return tailFrom(s, m.End)
	}
	return s
}

func authorShape(text string) bool {
	return rxHas(`(?i)^(?:[A-Z][\w’\x27-]+\s+)?(?:van|von|de|der|den|del|du|la|le)\s+(?:(?:van|von|de|der|den|del|du|la|le)\s+)?[A-Z][\w’\x27-]+,\s*[A-Z]`, text) ||
		rxHas(authorPattern, text) || rxHas(initialAuthor, text) ||
		rxHas(`^[A-ZÀ-ÖØ-Þ][\w&/.-]+(?:\s+[A-ZÀ-ÖØ-Þ][\w&/.-]+){1,7}[.,]`, text) ||
		rxHas(`^[A-Z]{2,12}[.,]`, text) || rxHas(`^[A-Z][a-z]+[A-Z][\w-]*[.,]`, text) ||
		rxHas(`^[A-ZÀ-ÖØ-Þ][\w&/.-]+,\s*[A-ZÀ-ÖØ-Þ][\w&/.-]+\s*[,;.]`, text) ||
		rxHas(`^[^\W\d_][\w -]{1,80},\s*[1Il]\.\s*[,;]`, text)
}

func publicationCue(text string) bool {
	return rxHas(`(?i)\bNo\.\s*[A-Za-z0-9-]+`, text) ||
		rxHas(`(?i)\b(?:pp?|vol|ed)\.\s*\d|\b\d+\s*pp\.|\b\d{1,4}\s*\([A-Za-z]{1,5}\d{2,8}\)\.?\s*$`, text) ||
		rxHas(locatorPattern, text) || rxHas(doiPattern, text) ||
		rxHas(`(?i)\b(?:press|publishing|publisher|journal|proceedings|ISBN|DOI|pp?\.|vol\.|ed\.|in press|to be published)\b`, text) ||
		rxHas(`;\s*`+ocrYear, text) || rxHas(`\b[A-Z][\w -]{1,55}:\s*[A-Z][\w -]{2,65}[;,]`, text)
}

func namedYearRecord(text string) bool {
	s := payload(text)
	year := rxFind(ocrYear, s)
	return year != nil && year.Start < 650 && authorShape(s)
}

func formalIdentifierRecord(text string) bool {
	s := payload(text)
	return rxHas(`^[A-Z]{2,12}(?:/[A-Z]{1,8})?\s+\d{2,7}(?:[-./:]\d+)*\s*[:,;]\s*\S`, s) && len(strings.Fields(s)) >= 5 && !rxHas(`[=±<>]`, s)
}

func accessedURLRecord(text string) bool {
	s := payload(text)
	return rxHas(`(?i)^(?:https?://|h[tp]+s?://)`, s) && rxHas(`(?i)\b(?:accessed|retrieved|cited)\b`, s) && rxHas(ocrYear, s)
}

func bodyYearConflict(text string) bool {
	s := payload(text)
	year := rxFind(ocrYear, s)
	if !authorShape(s) || year == nil || year.Start > 180 {
		return false
	}
	tail := rxReplace(`^[a-z](?=[).,;:\s])`, "", tailFrom(s, year.End))
	m := rxFind(clauseStart, tail)
	if m == nil {
		return false
	}
	word := m.Group(2)
	if m.Group(1) != "." && word == strings.ToLower(word) && rxFull(finitePattern, word) {
		return true
	}
	if rxHas(`(?i)^[)\]]?[.,;:]?\s*(?:i|me|my|mine|we|us|our|ours|you|your|yours|he|him|his|she|her|hers|it|its|they|them|their|theirs|this|these|those|the authors?|researchers?|participants?|patients?|results?|data|samples?)\b`, tail) {
		return true
	}
	if publicationCue(s) {
		return false
	}
	finite := strings.TrimPrefix(finitePattern, "(?i)")
	if rxHas(`(?i)^[)\]]?[.,;:]?\s*(?:the|a|an)\s+(?:[A-Za-z-]+\s+){1,6}(?:`+finite+`|[a-z]{3,}ed)\b`, tail) {
		return true
	}
	if rxHas(`[.!?]\s*$`, tail) && rxHas(`^[)\]]?[.,;:]?\s*[A-Z][a-z-]+(?:\s+[a-z-]+){0,3}\s+[a-z]{3,}(?:ed)\b`, tail) {
		return true
	}
	return rxHas(`(?i)\b(?:is|are|was|were|has|have|had|must|should|can|could)\b`, tail)
}

func referenceSeed(text string) bool {
	s := payload(text)
	y := rxFind(ocrYear, s)
	if !authorShape(s) || y == nil || bodyYearConflict(s) || y.Start > 650 {
		return false
	}
	if rxHas(`(?i)\b(?:we|our|was|were|found|showed|reported|argued|demonstrated)\b`, runeSlice(s, 0, y.Start)) {
		return false
	}
	return rxHas(locatorPattern, s) || rxHas(doiPattern, s) ||
		rxHas(`\b[A-Z][\w -]{1,55}:\s*[A-Z][\w -]{2,65}[;,]`, s) ||
		rxHas(`(?i)\b(?:press|publishing|publisher|journal|proceedings|ISBN|DOI)\b`, s) ||
		rxHas(`;\s*`+ocrYear, s)
}

func completePublicationTail(text string) bool {
	s := strings.TrimSpace(text)
	finite := strings.TrimPrefix(finitePattern, "(?i)")
	if s == "" || rxHas(`^(?i:the|this|these|those)\s+(?:[a-z-]+\s+){0,5}`+finite+`\b`, s) ||
		rxHas(`(?i)^(?:we|our|this study|the results|see)\b`, s) {
		return false
	}
	if rxHas(`(?i)\b(?:accessed|retrieved|cited)\b`, s) && rxHas(`(?i)https?://`, s) && rxHas(`\d`, s) {
		return true
	}
	return rxHas(locatorPattern, text) || rxHas(`(?i)\bISBN(?:-1[03])?\s*[: ]*\d|\b(?:working|research|technical|conference)\s+(?:paper|report)\b|\b(?:paper|report)\s+(?:no\.?\s*\d|[A-Z]{2,}[-/]|(?:commissioned|prepared|published)\s+(?:by|for)\b)|\b(?:pages?|pp?\.)\s+\d{1,6}\s*[-–−]\s*\d{1,6}`, text)
}

func lineBody(line string) bool {
	s := payload(line)
	year := rxFind(ocrYear, s)
	if year == nil || year.Start > 180 || !authorShape(s) {
		return false
	}
	tail := rxReplace(`^[a-z](?=[).,;:\s])`, "", tailFrom(s, year.End))
	initial := rxFind(clauseStart, tail)
	if initial == nil {
		return false
	}
	text := rxReplace(`^[)\]]?[.,;:]?\s*`, "", tail)
	if m := rxFind(sentenceBreak, text); m != nil && completePublicationTail(tailFrom(text, m.End)) {
		return false
	}
	word := initial.Group(2)
	if initial.Group(1) != "." && word == strings.ToLower(word) && rxFull(finitePattern, word) {
		return true
	}
	finite := strings.TrimPrefix(finitePattern, "(?i)")
	if rxHas(`^(?i:I|me|we|us|you|he|she|they|the authors?|researchers?|participants?|patients?)\s+[a-z][a-z-]*\b`, text) ||
		rxHas(`^(?i:my|our|your|his|her|their)\s+(?:[a-z-]+\s+){1,5}(?:`+finite+`|[a-z]{3,}ed)\b`, text) ||
		rxHas(`^(?i:samples?|results?|data)\s+(?:[a-z-]+\s+){0,2}(?:`+finite+`|[a-z]{3,}ed)\b`, text) ||
		rxHas(`^(?i:the|a|an)\s+(?:[a-z-]+\s+){1,6}`+finite+`\b`, text) {
		return true
	}
	return !rxHas(`^(?i:the|a|an|towards?|with|from|in|on)\b`, text) && rxHas(`[.!?]\s*$`, text) && rxHas(`^[A-Z][a-z-]+\s+[a-z]{3,}ed(?=\s)`, text)
}

func bodyAfterCitation(text string) bool {
	s := payload(text)
	locators := append(rxAll(locatorPattern, s), rxAll(doiPattern, s)...)
	if len(locators) == 0 {
		return false
	}
	finite := strings.TrimPrefix(finitePattern, "(?i)")
	for _, boundary := range rxAll(`[.!?]\s+(?=[A-Z])`, s) {
		lastEnd := -1
		for _, loc := range locators {
			if loc.End <= boundary.Start {
				lastEnd = max(lastEnd, loc.End)
			}
		}
		if lastEnd < 0 {
			continue
		}
		between := runeSlice(s, lastEnd, boundary.Start)
		fragment := ""
		for _, start := range rxAll(`(?<!\w)[A-ZÀ-ÖØ-Þ]`, between) {
			f := tailFrom(between, start.Start)
			if namedYearRecord(f) && rxHas(ocrYear+`[a-z]?[)\].,;:]*\s*$`, f) {
				fragment = f
				break
			}
		}
		tail := strings.TrimSpace(tailFrom(s, boundary.End))
		if fragment != "" {
			entryTail := tail
			for _, cut := range rxAll(`[.!?]\s+(?=[A-Z])`, tail) {
				if namedYearRecord(tailFrom(tail, cut.End)) {
					entryTail = runeSlice(tail, 0, cut.End)
					break
				}
			}
			if !lineBody(fragment + ". " + entryTail) {
				continue
			}
		}
		if tail == "" || (authorShape(payload(tail)) && rxHas(ocrYear, tail)) {
			continue
		}
		sentence := tail
		if m := rxFind(sentenceBreak, tail); m != nil {
			sentence = runeSlice(tail, 0, m.Start)
		}
		if completePublicationTail(sentence) {
			continue
		}
		if rxHas(prosePattern, sentence) || rxHas(`^(?:I|My|We|Our|They|Their|He|His|She|Her)\s+[a-z][a-z-]*\b`, sentence) ||
			rxHas(`(?i)^(?:Samples?|Results?|Data|The (?:samples?|results?|data|authors?|study))\s+(?:[A-Za-z-]+\s+){0,2}(?:`+finite+`|[a-z]{3,}ed)\b`, sentence) ||
			(rxHas(`^Let\b`, tail) && rxHas(`\$[^$]*(?:=|\\frac|\\sum)[^$]*\$`, tail)) {
			return true
		}
	}
	return false
}

func hardProse(text string, seed bool) bool {
	s := payload(text)
	return s != "" && (rxHas(bioPattern, s) || rxHas(metaPattern, s) ||
		(!seed && (rxHas(clauseNarrative, s) || rxHas(verbPattern, s) || rxHas(`(?i)^(?:model|instrument|apparatus|equipment|device|calibration)\b`, s))))
}

func regionLines(blocks []Block) []regionLine {
	structural := structuralBlocks(blocks)
	var rows []regionLine
	for bi, b := range blocks {
		lines := splitLines(textOf(b), true)
		pos := 0
		for _, line := range lines {
			a, z := pos, pos+runeLen(line)
			pos = z
			s := strings.TrimSpace(line)
			title := normalizedTitle(rxReplace(`^\s*[■▪•●]\s*`, "", s))
			rh, seed := rxFull(filterPatterns["references"], title), referenceSeed(line)
			typ, _ := b["type"].(string)
			header := !rh && !seed && ((runeLen(s) < 180 && rxFull(bodyPattern, title)) || unknownTitle(s) || (len(lines) == 1 && (typ == "title" || typ == "heading")))
			math := !seed && rxHas(inlineMath, line)
			metadata := rxHas(`(?i)\bcopyright\b|©|\ball rights reserved\b|\bmanuscript received\b`, line)
			technical := !seed && (rxHas(`(?i)^(?:model|instrument|apparatus|equipment|device|calibration)\b`, payload(line)) || rxHas(`(?i)\b(?:supplied|manufactured)\b`, line))
			neutral := rxFull(`\d{1,5}[.]?`, s) || rxFull(initialAuthor, s)
			probe := line
			if seed {
				probe = rxReplace(`(?i)\bethic(?:s|al)?\b`, "", line)
			}
			kind := lineKind(line)
			stop := hardProse(line, seed) || protected(probe) || bodyAfterCitation(line) || (!seed && (kind == "body" || kind == "structure"))
			rows = append(rows, regionLine{bi, a, z, line, seed, header, rh && !structural[bi], structural[bi] || math, stop, metadata || technical || neutral})
		}
	}
	return rows
}

func numberedBlockEvidence(rows []regionLine) bool {
	active, seeds, last := 0, 0, -1
	for _, r := range rows {
		if strings.TrimSpace(r.text) == "" {
			continue
		}
		if r.structure || r.header || r.prose || r.island {
			return false
		}
		m := rxFind(entryLabel, normalizeReference(r.text))
		if m == nil || !authorShape(payload(r.text)) || !publicationCue(r.text) {
			return false
		}
		d := rxFind(`\d+`, m.Group(0))
		if d == nil {
			return false
		}
		n, _ := strconv.Atoi(d.Group(0))
		if last >= 0 && n != last+1 {
			return false
		}
		last = n
		active++
		if r.seed {
			seeds++
		}
	}
	return active >= 5 && seeds*5 >= active*3
}

func reviewListContext(blocks []Block, bi int) bool {
	var prev strings.Builder
	for _, b := range blocks[max(0, bi-2):bi] {
		prev.WriteString(textOf(b))
		prev.WriteByte('\n')
	}
	return rxHas(`(?i)\b(?:book\s*sh(?:elf|elves)|reading list|recommended reading|book reviews?)\b|\b(?:books?|volumes?)\b.{0,65}\b(?:reviewed|recommended|discussed)\b`, prev.String())
}

func guardedRegions(blocks []Block, rows []regionLine) []adjustment {
	byBlock := map[int][]regionLine{}
	for _, row := range rows {
		byBlock[row.block] = append(byBlock[row.block], row)
	}
	numbered := map[int]bool{}
	for bi, lines := range byBlock {
		numbered[bi] = numberedBlockEvidence(lines)
	}
	var out []adjustment
	var buf []regionLine
	var heading *regionLine
	lastBlock := -1
	region := 0
	flush := func() {
		current := buf
		buf = nil
		var seeds, eligible []int
		nonblank := 0
		for i, r := range current {
			if r.seed {
				seeds = append(seeds, i)
			}
			if r.seed || (heading != nil && namedYearRecord(r.text)) {
				eligible = append(eligible, i)
			}
			if strings.TrimSpace(r.text) != "" {
				nonblank++
			}
		}
		if len(seeds) == 0 {
			return
		}
		headed := heading != nil
		if headed && len(eligible) < 2 {
			return
		}
		if !headed && (len(seeds) < 5 || len(seeds)*5 < nonblank*3) {
			return
		}
		if !headed {
			bi := current[seeds[len(seeds)-1]].block
			if reviewListContext(blocks, bi) {
				return
			}
			terminal := numbered[bi]
			later := ""
			for _, b := range blocks[bi+1:] {
				if strings.TrimSpace(textOf(b)) != "" {
					later = textOf(b)
					break
				}
			}
			k, _ := inlineHeading(later)
			terminal = terminal || later == "" || isFilter(lineKind(later)) || isFilter(k) || rxHas(`(?i)^\s*(?:appendi(?:x|ces)|annex|supplementary|supporting information)\b`, later)
			if !terminal {
				return
			}
		}
		first, last := eligible[0], eligible[len(eligible)-1]
		gap, gapLines := 0, 0
		for _, row := range current[first : last+1] {
			if row.seed {
				gap, gapLines = 0, 0
			} else if strings.TrimSpace(row.text) != "" && !row.island {
				gap += runeLen(row.text)
				gapLines++
			}
			if gap > 650 || gapLines > 2 {
				return
			}
		}
		end := last + 1
		for end < len(current) {
			row := current[end]
			s := strings.TrimSpace(row.text)
			label := rxHas(entryLabel, s) && namedYearRecord(s)
			url := rxHas(`(?i)^(?:https?://|h[tp]+s?://|10\.\d{4,9}/)`, s) && (rxHas(doiPattern, s) || rxHas(`(?i)\b(?:accessed|retrieved|cited)\b`, s))
			if s == "" || ((label || url || numbered[row.block]) && !row.prose && !row.island) {
				end++
			} else {
				break
			}
		}
		part := current[first:end]
		kind := "guarded_unheaded_dense"
		if headed {
			leading := current[:first]
			length := 0
			safe := true
			for _, r := range leading {
				length += runeLen(r.text)
				if r.prose || !(r.island || strings.TrimSpace(r.text) == "" || namedYearRecord(r.text) || accessedURLRecord(r.text)) {
					safe = false
				}
			}
			if length < 650 && safe {
				part = current[:end]
			}
			part = append([]regionLine{*heading}, part...)
			kind = "guarded_heading"
		}
		section := fmt.Sprintf("v310:%d", region)
		region++
		for _, row := range part {
			if !row.island {
				out = append(out, adjustment{Removal: Removal{Block: row.block, Start: row.start, End: row.end, Kind: "references", Reason: "v310_region_" + kind}, section: section})
			}
		}
	}
	for _, row := range rows {
		if row.refHeader {
			flush()
			h := row
			heading = &h
			lastBlock = row.block
			continue
		}
		if row.island && !row.structure {
			buf = append(buf, row)
			lastBlock = row.block
			continue
		}
		if row.header || row.structure || row.prose {
			flush()
			heading = nil
			lastBlock = row.block
			continue
		}
		if heading == nil && lastBlock >= 0 && lastBlock != row.block {
			flush()
		}
		if lastBlock >= 0 {
			page, _ := pageOf(blocks[row.block])
			lastPage, _ := pageOf(blocks[lastBlock])
			diff := page - lastPage
			if diff > 1 && !(heading != nil && row.seed && row.block == lastBlock+1 && diff == 2) {
				flush()
				heading = nil
			}
		}
		weak := numbered[row.block] || row.seed || strings.TrimSpace(row.text) == "" || namedYearRecord(row.text) || formalIdentifierRecord(row.text) || accessedURLRecord(row.text)
		if !weak {
			flush()
			heading = nil
			lastBlock = row.block
			continue
		}
		lastBlock = row.block
		buf = append(buf, row)
	}
	flush()
	return out
}
