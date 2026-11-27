package app

import (
	"reflect"
	"strings"
)

func graphRichString(spans []graphTextSpan) string {
	var text strings.Builder
	for _, span := range spans {
		text.WriteString(span.Text)
	}
	return text.String()
}

func cutGraphRich(spans []graphTextSpan, from, to int) []graphTextSpan {
	result := []graphTextSpan{}
	position := 0
	for _, span := range spans {
		text := []rune(span.Text)
		start, end := max(0, from-position), min(len(text), to-position)
		if start < end {
			span.Text = string(text[start:end])
			result = append(result, span)
		}
		position += len(text)
		if position >= to {
			break
		}
	}
	return result
}

func appendGraphRich(result []graphTextSpan, spans ...[]graphTextSpan) []graphTextSpan {
	for _, part := range spans {
		for _, span := range part {
			if span.Text == "" {
				continue
			}
			last := len(result) - 1
			marks := span
			marks.Text = ""
			if last >= 0 {
				previous := result[last]
				previous.Text = ""
				if previous == marks && span.Mention == "" && span.Equation == "" && span.Template == "" {
					result[last].Text += span.Text
					continue
				}
			}
			result = append(result, span)
		}
	}
	return result
}

// Apply only formatting keys changed by this writer; unrelated peer marks
// survive. Equal text is aligned by run boundaries, not expanded into per-char
// copies of every metadata field.
func mergeGraphRichMarks(base, next, current []graphTextSpan) []graphTextSpan {
	b, n, c := append([]graphTextSpan(nil), base...), append([]graphTextSpan(nil), next...), append([]graphTextSpan(nil), current...)
	result := []graphTextSpan{}
	for len(b) > 0 && len(n) > 0 && len(c) > 0 {
		br, nr, cr := []rune(b[0].Text), []rune(n[0].Text), []rune(c[0].Text)
		length := min(len(br), len(nr), len(cr))
		span := c[0]
		span.Text = string(cr[:length])
		before, wanted, value := reflect.ValueOf(b[0]), reflect.ValueOf(n[0]), reflect.ValueOf(&span).Elem()
		for index := 1; index < value.NumField(); index++ {
			if !reflect.DeepEqual(before.Field(index).Interface(), wanted.Field(index).Interface()) {
				value.Field(index).Set(wanted.Field(index))
			}
		}
		result = appendGraphRich(result, []graphTextSpan{span})
		for _, part := range []*[]graphTextSpan{&b, &n, &c} {
			first := &(*part)[0]
			remaining := []rune(first.Text)[length:]
			if len(remaining) == 0 {
				*part = (*part)[1:]
			} else {
				first.Text = string(remaining)
			}
		}
	}
	return result
}

// Others' writing is the one run where the stored text differs from the base:
// the base's letters before it and after it are still there.
func graphRichPeerRun(before, stored []rune) (same, sameEnd int) {
	for same < len(before) && same < len(stored) && before[same] == stored[same] {
		same++
	}
	for sameEnd < len(before)-same && sameEnd < len(stored)-same && before[len(before)-sameEnd-1] == stored[len(stored)-sameEnd-1] {
		sameEnd++
	}
	return same, sameEnd
}

// A change clear of others' run keeps its place, past the run when after it.
// The text around a change can't always find it: a field's edge has none on one
// side, and a peer typing inside the window breaks the other (2026-11-05: a
// cell's start was refused, and the letters lost, with a peer 4 letters away).
func graphRichStartClearOfPeers(before []rune, prefix, suffix int, stored []rune) (int, bool) {
	same, sameEnd := graphRichPeerRun(before, stored)
	if prefix >= len(before)-sameEnd {
		return prefix + len(stored) - len(before), true
	}
	if len(before)-suffix <= same {
		return prefix, true
	}
	return 0, false
}

// A native rich field remains a graph field. Merge an anchored local delta
// against its current value; ambiguous/overlapping replacements are explicit
// conflicts instead of silently overwriting a peer's snapshot.
func mergeGraphRichText(base, next, current []graphTextSpan) ([]graphTextSpan, error) {
	beforeText, nextText, currentText := graphRichString(base), graphRichString(next), graphRichString(current)
	if beforeText == nextText {
		if currentText == beforeText {
			return mergeGraphRichMarks(base, next, current), nil
		}
		// Formatting goes on the letters others left alone; what they wrote
		// keeps its own (2026-11-05: bold was refused while a peer typed).
		before, stored := []rune(beforeText), []rune(currentText)
		same, sameEnd := graphRichPeerRun(before, stored)
		tail := len(before) - sameEnd
		return appendGraphRich([]graphTextSpan{},
			mergeGraphRichMarks(cutGraphRich(base, 0, same), cutGraphRich(next, 0, same), cutGraphRich(current, 0, same)),
			cutGraphRich(current, same, len(stored)-sameEnd),
			mergeGraphRichMarks(cutGraphRich(base, tail, len(before)), cutGraphRich(next, tail, len(before)), cutGraphRich(current, len(stored)-sameEnd, len(stored)))), nil
	}
	before, wanted := []rune(beforeText), []rune(nextText)
	prefix, suffix := 0, 0
	for prefix < len(before) && prefix < len(wanted) && before[prefix] == wanted[prefix] {
		prefix++
	}
	for suffix < len(before)-prefix && suffix < len(wanted)-prefix && before[len(before)-suffix-1] == wanted[len(wanted)-suffix-1] {
		suffix++
	}
	deleted := string(before[prefix : len(before)-suffix])
	prefixContext := string(before[max(0, prefix-32):prefix])
	suffixContext := string(before[len(before)-suffix : min(len(before), len(before)-suffix+32)])
	start := -1
	stored := []rune(currentText)
	removed := len([]rune(deleted))
	if currentText == beforeText {
		start = prefix
	} else if beforeText == "" {
		start = 0
	} else if position, clear := graphRichStartClearOfPeers(before, prefix, suffix, stored); clear {
		start = position
	} else {
		for _, candidate := range []struct {
			text   string
			offset int
		}{
			{prefixContext + deleted + suffixContext, len([]rune(prefixContext))},
			{prefixContext, len([]rune(prefixContext))},
			{suffixContext, 0},
		} {
			if candidate.text == "" {
				continue
			}
			index := strings.Index(currentText, candidate.text)
			if index < 0 || index != strings.LastIndex(currentText, candidate.text) {
				continue
			}
			position := len([]rune(currentText[:index])) + candidate.offset
			if position+removed > len(stored) || string(stored[position:position+removed]) != deleted {
				continue
			}
			start = position
			break
		}
	}
	if start < 0 || start+removed > len(stored) || string(stored[start:start+removed]) != deleted {
		return nil, errGraphBlockContentConflict
	}
	head, tail := cutGraphRich(current, 0, start), cutGraphRich(current, start+removed, len(stored))
	// A transaction may change formatting outside its inserted/deleted range.
	// Merge those runs when their text is unchanged; otherwise refuse rather
	// than dropping that part of the writer's change.
	for _, side := range []struct {
		before, next []graphTextSpan
		current      *[]graphTextSpan
	}{
		{cutGraphRich(base, 0, prefix), cutGraphRich(next, 0, prefix), &head},
		{cutGraphRich(base, len(before)-suffix, len(before)), cutGraphRich(next, len(wanted)-suffix, len(wanted)), &tail},
	} {
		if reflect.DeepEqual(side.before, side.next) {
			continue
		}
		if graphRichString(side.before) != graphRichString(*side.current) {
			return nil, errGraphBlockContentConflict
		}
		*side.current = mergeGraphRichMarks(side.before, side.next, *side.current)
	}
	insert := cutGraphRich(next, prefix, len(wanted)-suffix)
	return appendGraphRich([]graphTextSpan{}, head, insert, tail), nil
}
