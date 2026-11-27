package app

import "unicode"

// A highlight shows a long line's middle, 179 characters and the rest of a
// word they cut, as Notion's do (seen 2026-11-11 on lines of 162 to 562
// characters, a keyword placed from their start to their end): of 262
// characters with the keyword at 50, 200 or 250, the same 178 from the 40th,
// whatever the keyword's place as long as it stands in that middle. A
// keyword before the middle starts the highlight ("talquiren e001 …
// e034..."), and one past it ends it, from the first word starting at most
// 199 characters before it ("...p012 … p050 parvolin", the line's " q001 …
// end" left out; 2026-11-14: "...sit amet lorem … sit amet korvan", 198
// before). A line of 179 characters or fewer shows whole.
const (
	workspaceSearchHighlightRunes = 179
	workspaceSearchHighlightLead  = 199
	// workspaceSearchHighlightWordRunes is the longest word a highlight's
	// edge moves to keep whole; a longer one, as in a script written without
	// spaces, is cut where the edge falls.
	workspaceSearchHighlightWordRunes = 40
)

// workspaceSearchHighlightWindow is the part of a line a highlight shows, by
// its first keyword's place.
func workspaceSearchHighlightWindow(runes []rune, first [2]int) (int, int) {
	start := workspaceSearchHighlightWordStart(runes, (len(runes)-workspaceSearchHighlightRunes)/2)
	if first[0] < start {
		start = first[0]
	}
	limit := start + workspaceSearchHighlightRunes
	end := len(runes)
	if first[1] > limit {
		start, end = workspaceSearchHighlightNextWord(runes, first[0]-workspaceSearchHighlightLead), first[1]
	} else if limit < len(runes) {
		end = workspaceSearchHighlightWordEnd(runes, limit)
	}
	for start < end && unicode.IsSpace(runes[start]) {
		start++
	}
	for end > start && unicode.IsSpace(runes[end-1]) {
		end--
	}
	return start, end
}

// workspaceSearchHighlightWordStart is where a highlight starting at a
// character starts: the start of the word holding it, or of the next word.
func workspaceSearchHighlightWordStart(runes []rune, at int) int {
	if at <= 0 {
		return 0
	}
	if at >= len(runes) {
		return len(runes)
	}
	if unicode.IsSpace(runes[at]) {
		for at < len(runes) && unicode.IsSpace(runes[at]) {
			at++
		}
		return at
	}
	from := at
	for from > 0 && !unicode.IsSpace(runes[from-1]) && at-from < workspaceSearchHighlightWordRunes {
		from--
	}
	if from > 0 && !unicode.IsSpace(runes[from-1]) {
		return at
	}
	return from
}

// workspaceSearchHighlightNextWord is where the first word starting at a
// character or after it starts.
func workspaceSearchHighlightNextWord(runes []rune, at int) int {
	if at <= 0 {
		return 0
	}
	for at < len(runes) && !unicode.IsSpace(runes[at-1]) {
		at++
	}
	for at < len(runes) && unicode.IsSpace(runes[at]) {
		at++
	}
	return at
}

// workspaceSearchHighlightWordEnd is where a highlight running to a
// character ends: there, or at the end of the word it cuts.
func workspaceSearchHighlightWordEnd(runes []rune, at int) int {
	if at == 0 || unicode.IsSpace(runes[at-1]) {
		return at
	}
	to := at
	for to < len(runes) && !unicode.IsSpace(runes[to]) && to-at < workspaceSearchHighlightWordRunes {
		to++
	}
	if to < len(runes) && !unicode.IsSpace(runes[to]) {
		return at
	}
	return to
}
