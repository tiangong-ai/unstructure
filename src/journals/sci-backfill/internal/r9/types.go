package r9

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/dlclark/regexp2/v2"
)

type Block map[string]any

type Removal struct {
	Block, Start, End     int
	Kind, Reason, Section string
}

type Span struct {
	Start       int     `json:"start"`
	End         int     `json:"end"`
	Action      string  `json:"action"`
	Kind        string  `json:"kind,omitempty"`
	Reason      string  `json:"reason,omitempty"`
	Section     any     `json:"section,omitempty"`
	Text        *string `json:"text,omitempty"`
	OutputStart *int    `json:"output_start,omitempty"`
}

type Mapping struct {
	SourceIndex int    `json:"source_index"`
	Metadata    Block  `json:"source_metadata"`
	SHA256      string `json:"source_text_sha256"`
	Spans       []Span `json:"spans"`
	OutputIndex *int   `json:"output_index"`
}

type Result struct {
	Cleaned []Block          `json:"cleaned"`
	Mapping []Mapping        `json:"mapping"`
	Reviews []map[string]any `json:"reviews"`
}

type rxMatch struct {
	Start, End int
	groups     []string
}

func (m *rxMatch) Group(i int) string {
	if i < 0 || i >= len(m.groups) {
		return ""
	}
	return m.groups[i]
}

var rxCache sync.Map

func rx(pattern string) *regexp2.Regexp {
	if v, ok := rxCache.Load(pattern); ok {
		return v.(*regexp2.Regexp)
	}
	p := strings.ReplaceAll(pattern, "(?P<", "(?<")
	re := regexp2.MustCompile(p, regexp2.None)
	re.MatchTimeout = 3 * time.Second
	v, _ := rxCache.LoadOrStore(pattern, re)
	return v.(*regexp2.Regexp)
}

func matchOf(m *regexp2.Match) *rxMatch {
	if m == nil {
		return nil
	}
	out := &rxMatch{Start: m.RuneIndex, End: m.RuneIndex + m.RuneLength}
	for _, g := range m.Groups() {
		value := ""
		if len(g.Captures) != 0 {
			value = g.String()
		}
		out.groups = append(out.groups, value)
	}
	return out
}

func rxFind(pattern, text string) *rxMatch {
	m, err := rx(pattern).FindStringMatch(text)
	if err != nil {
		panic(err)
	}
	return matchOf(m)
}

func rxAll(pattern, text string) []*rxMatch {
	p := rx(pattern)
	var out []*rxMatch
	m, err := p.FindStringMatch(text)
	for m != nil && err == nil {
		out = append(out, matchOf(m))
		m, err = p.FindNextMatch(m)
	}
	if err != nil {
		panic(err)
	}
	return out
}

func rxHas(pattern, text string) bool  { return rxFind(pattern, text) != nil }
func rxFull(pattern, text string) bool { return rxHas("\\A(?:"+pattern+")\\z", text) }
func rxReplace(pattern, replacement, text string) string {
	out, err := rx(pattern).Replace(text, replacement, -1, -1)
	if err != nil {
		panic(err)
	}
	return out
}

func runeLen(s string) int                { return utf8.RuneCountInString(s) }
func runeSlice(s string, a, z int) string { return string([]rune(s)[a:z]) }
func tailFrom(s string, a int) string     { return string([]rune(s)[a:]) }
func splitLines(s string, keep bool) []string {
	var lines []string
	runes := []rune(s)
	start := 0
	for i := 0; i < len(runes); i++ {
		switch runes[i] {
		case '\n', '\r', '\v', '\f', '\u001c', '\u001d', '\u001e', '\u0085', '\u2028', '\u2029':
			end := i
			if runes[i] == '\r' && i+1 < len(runes) && runes[i+1] == '\n' {
				i++
			}
			if keep {
				end = i + 1
			}
			lines = append(lines, string(runes[start:end]))
			start = i + 1
		}
	}
	if start < len(runes) {
		lines = append(lines, string(runes[start:]))
	}
	return lines
}

func textOf(b Block) string { s, _ := b["text"].(string); return s }
func pageOf(b Block) (int, bool) {
	switch v := b["page_number"].(type) {
	case json.Number:
		n, e := strconv.Atoi(v.String())
		return n, e == nil
	case int:
		return v, true
	default:
		return 0, false
	}
}

func pageOrder(blocks []Block) bool {
	last := 0
	for _, b := range blocks {
		p, ok := pageOf(b)
		if !ok || p < 1 || p < last {
			return false
		}
		last = p
	}
	return true
}

func sha(s string) string { digest := sha256.Sum256([]byte(s)); return hex.EncodeToString(digest[:]) }
func clone(b Block) Block {
	x := make(Block, len(b))
	for k, v := range b {
		x[k] = v
	}
	return x
}
func outputMapping(b Block, i int) Mapping {
	meta := clone(b)
	delete(meta, "text")
	return Mapping{SourceIndex: i, Metadata: meta, SHA256: sha(textOf(b)), Spans: []Span{}}
}
func keep(start, end, cursor int) Span {
	return Span{Start: start, End: end, Action: "keep", OutputStart: &cursor}
}
func exclude(start, end int, kind, reason string, section any, text string) Span {
	return Span{Start: start, End: end, Action: "exclude", Kind: kind, Reason: reason, Section: section, Text: &text}
}

func hasBody(blocks []Block) bool {
	for _, b := range blocks {
		if strings.TrimSpace(textOf(b)) != "" {
			return true
		}
	}
	return false
}

func review(b, start int, kind, reason string) map[string]any {
	return map[string]any{"block": b, "start": start, "kind": kind, "reason": reason}
}

func retainAll(blocks []Block, reason string) Result {
	r := Result{Cleaned: []Block{}, Mapping: []Mapping{}, Reviews: []map[string]any{{"reason": reason}}}
	for i, b := range blocks {
		m := outputMapping(b, i)
		m.Spans = append(m.Spans, keep(0, runeLen(textOf(b)), 0))
		index := i
		m.OutputIndex = &index
		r.Mapping = append(r.Mapping, m)
		r.Cleaned = append(r.Cleaned, clone(b))
	}
	return r
}

func appendOutput(r *Result, b Block, m Mapping) {
	var buf strings.Builder
	hasExclusions := false
	for _, sp := range m.Spans {
		if sp.Action == "exclude" {
			hasExclusions = true
			continue
		}
		buf.WriteString(runeSlice(textOf(b), sp.Start, sp.End))
	}
	text := buf.String()
	if len(m.Spans) == 0 {
		text = textOf(b)
	}
	if !hasExclusions || strings.TrimSpace(text) != "" {
		index := len(r.Cleaned)
		m.OutputIndex = &index
		copy := clone(b)
		copy["text"] = text
		r.Cleaned = append(r.Cleaned, copy)
	}
	r.Mapping = append(r.Mapping, m)
}

func sortedBounds(bounds map[int]bool) []int {
	out := make([]int, 0, len(bounds))
	for p := range bounds {
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}

func isFilter(kind string) bool { return filterPatterns[kind] != "" }
