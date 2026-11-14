package r9

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
)

func Clean(blocks []Block) (Result, error) {
	for _, b := range blocks {
		if _, ok := b["text"].(string); !ok {
			return Result{}, errors.New("expected raw blocks with string text")
		}
		if _, embedded := b["embedding"]; embedded {
			return Result{}, errors.New("embedded blocks cannot be cleaned")
		}
	}
	if !pageOrder(blocks) {
		return retainAll(blocks, "unreliable_page_order_document_retained"), nil
	}
	return fixedPoint(blocks)
}

func mergeAdjustments(blocks []Block, baseline Result, removals []adjustment, keeps []Removal, referenceKeeps []Removal, emptyReason string) Result {
	byBlock := map[int][]adjustment{}
	for _, r := range removals {
		byBlock[r.Block] = append(byBlock[r.Block], r)
	}
	keepByBlock := map[int][]Removal{}
	refByBlock := map[int][]Removal{}
	for _, item := range keeps {
		keepByBlock[item.Block] = append(keepByBlock[item.Block], item)
	}
	for _, item := range referenceKeeps {
		refByBlock[item.Block] = append(refByBlock[item.Block], item)
	}
	out := Result{Cleaned: []Block{}, Mapping: []Mapping{}, Reviews: append([]map[string]any{}, baseline.Reviews...)}
	for bi, b := range blocks {
		text := textOf(b)
		original := baseline.Mapping[bi]
		extra, force, ref := byBlock[bi], keepByBlock[bi], refByBlock[bi]
		boundaries := map[int]bool{0: true, runeLen(text): true}
		for _, s := range original.Spans {
			boundaries[s.Start], boundaries[s.End] = true, true
		}
		for _, item := range extra {
			boundaries[item.Start], boundaries[item.End] = true, true
		}
		for _, list := range [][]Removal{force, ref} {
			for _, item := range list {
				boundaries[item.Start], boundaries[item.End] = true, true
			}
		}
		bounds := sortedBounds(boundaries)
		m := outputMapping(b, bi)
		cursor, priorIndex := 0, 0
		for i := 1; i < len(bounds); i++ {
			a, z := bounds[i-1], bounds[i]
			prior := Span{Action: "keep"}
			for priorIndex+1 < len(original.Spans) && original.Spans[priorIndex].End <= a {
				priorIndex++
			}
			if priorIndex < len(original.Spans) {
				candidate := original.Spans[priorIndex]
				if candidate.Start <= a && candidate.End >= z {
					prior = candidate
				}
			}
			var add *adjustment
			for _, item := range extra {
				if item.Start <= a && item.End >= z {
					candidate := item
					add = &candidate
					break
				}
			}
			protected := covered(force, a, z) || ((prior.Kind == "" || prior.Kind == "references") && covered(ref, a, z))
			if (prior.Action == "exclude" || add != nil) && !protected {
				kind, reason, section := prior.Kind, prior.Reason, prior.Section
				if prior.Action != "exclude" {
					kind, reason, section = add.Kind, add.Reason, add.section
				}
				m.Spans = append(m.Spans, exclude(a, z, kind, reason, section, runeSlice(text, a, z)))
			} else {
				m.Spans = append(m.Spans, keep(a, z, cursor))
				cursor += z - a
			}
		}
		appendOutput(&out, b, m)
	}
	if hasBody(blocks) && !hasBody(out.Cleaned) {
		baseline.Reviews = append(baseline.Reviews, map[string]any{"reason": emptyReason})
		return baseline
	}
	return out
}

func covered(items []Removal, a, z int) bool {
	for _, item := range items {
		if item.Start <= a && item.End >= z {
			return true
		}
	}
	return false
}

func once(blocks []Block) Result {
	base := baseClean(blocks)
	add, reviews := v39Adjustments(blocks, base)
	nine := mergeAdjustments(blocks, base, add, nil, nil, "v39_empty_output_guard_supplement_cancelled")
	if !hasV39Guard(nine.Reviews) {
		nine.Reviews = append(nine.Reviews, reviews...)
	}
	rows := regionLines(blocks)
	var preserve []Removal
	for _, row := range rows {
		if lineBody(row.text) || bodyAfterCitation(row.text) {
			preserve = append(preserve, Removal{Block: row.block, Start: row.start, End: row.end})
		}
	}
	ten := mergeAdjustments(blocks, nine, guardedRegions(blocks, rows), nil, preserve, "v310_empty_output_guard_supplement_cancelled")
	extra := v311ReferenceAdditions(blocks)
	disclosures, keepDisclosure := v311DisclosureChanges(blocks)
	extra = append(extra, disclosures...)
	eleven := make([]adjustment, 0, len(extra))
	for _, item := range extra {
		eleven = append(eleven, adjustment{Removal: item, section: item.Section})
	}
	var keepReferences []Removal
	for _, row := range rows {
		if v311ReferenceBody(row.text) {
			keepReferences = append(keepReferences, Removal{Block: row.block, Start: row.start, End: row.end})
		}
	}
	return mergeAdjustments(blocks, ten, eleven, keepDisclosure, keepReferences, "v311_empty_output_guard_supplement_cancelled")
}

func hasV39Guard(reviews []map[string]any) bool {
	return len(reviews) > 0 && reviews[len(reviews)-1]["reason"] == "v39_empty_output_guard_supplement_cancelled"
}

type interval struct {
	start, end, originalStart int
}

type origin struct {
	block int
	parts []interval
}

func fixedPoint(blocks []Block) (Result, error) {
	current := blocks
	origins := make([]origin, len(blocks))
	for i, b := range blocks {
		origins[i] = origin{block: i, parts: []interval{{0, runeLen(textOf(b)), 0}}}
	}
	removed := make([][]Span, len(blocks))
	events := []map[string]any{}
	settled := false
	for pass := 0; pass < 8; pass++ {
		step := once(current)
		for _, e := range step.Reviews {
			event := make(map[string]any, len(e))
			for k, v := range e {
				event[k] = v
			}
			if bi, ok := event["block"].(int); ok && bi >= 0 && bi < len(origins) {
				source := origins[bi]
				event["block"] = source.block
				if point, ok := event["start"].(int); ok {
					delete(event, "start")
					for _, in := range source.parts {
						if in.start <= point && point < in.end {
							event["start"] = in.originalStart + point - in.start
							break
						}
					}
				}
			}
			events = append(events, event)
		}
		if reflect.DeepEqual(step.Cleaned, current) {
			settled = true
			break
		}
		oldLength, nextLength := length(current), length(step.Cleaned)
		if nextLength > oldLength || (nextLength == oldLength && len(step.Cleaned) >= len(current)) {
			return Result{}, errors.New("R9 transformation did not decrease")
		}
		next := make([]origin, 0, len(step.Cleaned))
		for _, mapping := range step.Mapping {
			o := origins[mapping.SourceIndex]
			kept := origin{block: o.block}
			for _, span := range mapping.Spans {
				for _, segment := range o.parts {
					left, right := max(segment.start, span.Start), min(segment.end, span.End)
					if left >= right {
						continue
					}
					a := segment.originalStart + left - segment.start
					z := a + right - left
					if span.Action == "exclude" {
						section := span.Section
						if pass > 0 && section != nil {
							section = fmt.Sprintf("v310:pass%d:%v", pass, section)
						}
						removed[o.block] = append(removed[o.block], exclude(a, z, span.Kind, span.Reason, section, runeSlice(textOf(blocks[o.block]), a, z)))
					} else {
						start := left - span.Start + *span.OutputStart
						kept.parts = append(kept.parts, interval{start, start + right - left, a})
					}
				}
			}
			if mapping.OutputIndex != nil {
				if *mapping.OutputIndex != len(next) {
					return Result{}, errors.New("R9 mapping is out of order")
				}
				next = append(next, kept)
			}
		}
		current, origins = step.Cleaned, next
	}
	if !settled {
		return retainAll(blocks, "v310_fixedpoint_limit_document_retained"), nil
	}
	result := Result{Cleaned: []Block{}, Mapping: []Mapping{}, Reviews: []map[string]any{}}
	for i, block := range blocks {
		text := textOf(block)
		cuts := removed[i]
		sort.SliceStable(cuts, func(a, b int) bool { return cuts[a].Start < cuts[b].Start })
		m := outputMapping(block, i)
		pos, cursor := 0, 0
		for _, cut := range cuts {
			if cut.Start < pos {
				return Result{}, errors.New("overlapping R9 source spans")
			}
			if cut.Start > pos {
				m.Spans = append(m.Spans, keep(pos, cut.Start, cursor))
				cursor += cut.Start - pos
			}
			m.Spans = append(m.Spans, cut)
			pos = cut.End
		}
		if pos < runeLen(text) || len(m.Spans) == 0 {
			m.Spans = append(m.Spans, keep(pos, runeLen(text), cursor))
		}
		appendOutput(&result, block, m)
	}
	if !reflect.DeepEqual(result.Cleaned, current) {
		return Result{}, errors.New("R9 fixed point does not match the source mapping")
	}
	seen := map[string]bool{}
	for _, event := range events {
		data, err := json.Marshal(event)
		if err != nil {
			return Result{}, err
		}
		key := string(data)
		if !seen[key] {
			result.Reviews = append(result.Reviews, event)
			seen[key] = true
		}
	}
	return result, nil
}

func length(blocks []Block) int {
	total := 0
	for _, b := range blocks {
		total += runeLen(textOf(b))
	}
	return total
}
