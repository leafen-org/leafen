package app

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A found page's passage, as Notion's search shows it (seen 2026-11-09): the
// page's blocks holding the keywords, each as one line, a block's own lines
// joined by a space ("First Trelliqua line Second Qorvant line"). A result's
// line is the block weighing most (workspace_search_weight.go), the first of
// those in the page's order ("Closing Qorvant words and more Trelliqua" for
// "more trelliqua", over the page's first block holding "Trelliqua"), its
// keywords found as the highlights beside it find them
// (workspaceSearchKeywordsFound), so the line is the first of them, as
// Notion's row shows its first highlight (seen 2026-11-24: "grindle line
// one" over "grindles line three" for "grindle line", "zz vexlorran zz"
// over "zz 71234 zz" for "7123 zz"). A block holding a keyword only as the
// start of a word or inside one, which the search finds a page by and its
// highlights don't (workspace_search_keywords.go), comes after them all. An
// agent's search highlight is the blocks holding any, the heaviest first,
// joined by " --- ", each keyword
// wrapped in "**" and keywords side by side in one pair ("**Bramblix** one
// **Bramblix two** third plain"). A long block shows the part of it a
// highlight beside the results shows, by its first keyword
// (workspaceSearchHighlightWindow), without the highlight's "...", as
// Notion's rows do (seen 2026-11-11 on eleven blocks of 199 to 569
// characters, the keyword from their start to their end: "d024 … d057" of
// one of 409 with the keyword at 250, "zorquelin b001 … b044" with it at 50,
// "e040 … e078 zorquelin" with it at its end).

const (
	// searchSoftBreak stands for a line break inside a block in a page's
	// search text, so the text's lines are its blocks.
	searchSoftBreak = ' '
	// workspaceSearchPassageApart is what an agent's highlight puts between blocks.
	workspaceSearchPassageApart = " --- "
)

// searchBlockText is a block's text as a page's search text keeps it: one line.
func searchBlockText(text string) string {
	return strings.ReplaceAll(text, "\n", string(searchSoftBreak))
}

// searchLineShown is a line of a page's search text as it is shown, a
// block's own line breaks as spaces.
func searchLineShown(line string) string {
	return strings.ReplaceAll(line, string(searchSoftBreak), " ")
}

// workspaceSearchPassageBlock is a block holding keywords, as shown: its
// text, the keywords' places in it (in characters, side by side ones as one)
// and its weight.
type workspaceSearchPassageBlock struct {
	text   string
	marks  [][2]int
	weight workspaceSearchWeight
	// at is its line's place in the text it was read from.
	at int
}

// workspaceSearchPassage is a page's blocks holding the keywords, the
// heaviest first, then in the page's order.
type workspaceSearchPassage []workspaceSearchPassageBlock

// workspaceSearchPassageOf is the passage of a page's search text, or of the
// part of it a search read, for a search's folded keywords.
func workspaceSearchPassageOf(text, plainQuery string) workspaceSearchPassage {
	terms := workspaceSearchHighlightWords(plainQuery)
	if len(terms) == 0 {
		return nil
	}
	passage := workspaceSearchPassage{}
	for at, line := range strings.Split(text, "\n") {
		if block, held := workspaceSearchPassageBlockOf(line, terms); held {
			block.at = at
			passage = append(passage, block)
		}
	}
	slices.SortStableFunc(passage, func(a, b workspaceSearchPassageBlock) int { return a.weight.compare(b.weight) })
	return passage
}

// line is a result's line: the heaviest block.
func (passage workspaceSearchPassage) line() string {
	if len(passage) == 0 {
		return ""
	}
	return passage[0].text
}

// highlight is an agent's search highlight, as long as the longest one asked
// for may be (maxNotionMCPSearchHighlight); notionMCPSearchHighlight cuts it.
func (passage workspaceSearchPassage) highlight() string {
	var written strings.Builder
	for index, block := range passage {
		if index > 0 {
			if utf8.RuneCountInString(written.String()) > maxNotionMCPSearchHighlight {
				break
			}
			written.WriteString(workspaceSearchPassageApart)
		}
		runes := []rune(block.text)
		at := 0
		for _, mark := range block.marks {
			written.WriteString(string(runes[at:mark[0]]))
			written.WriteString("**" + string(runes[mark[0]:mark[1]]) + "**")
			at = mark[1]
		}
		written.WriteString(string(runes[at:]))
	}
	return written.String()
}

// workspaceSearchPassageBlockOf is a line of a page's search text as a
// passage shows it, if it holds a keyword.
func workspaceSearchPassageBlockOf(line string, terms []string) (workspaceSearchPassageBlock, bool) {
	shown := []rune(searchLineShown(line))
	found := workspaceSearchKeywordsFound(shown, terms)
	highlighted := len(found) > 0
	if !highlighted {
		found = workspaceSearchPassageKeywords(shown, terms)
	}
	if len(found) == 0 {
		return workspaceSearchPassageBlock{}, false
	}
	marks := make([][2]int, 0, len(found))
	for _, at := range found {
		marks = append(marks, at.place)
	}
	start, end := workspaceSearchHighlightWindow(shown, marks[0])
	// The keywords' places in the window, those side by side as one.
	kept := [][2]int{}
	for _, mark := range marks {
		mark[0], mark[1] = max(mark[0], start)-start, min(mark[1], end)-start
		if mark[0] >= mark[1] {
			continue
		}
		if last := len(kept) - 1; last >= 0 && (mark[0] <= kept[last][1] || workspaceSearchOnlySpaces(shown[start+kept[last][1]:start+mark[0]])) {
			kept[last][1] = max(kept[last][1], mark[1])
			continue
		}
		kept = append(kept, mark)
	}
	weight := workspaceSearchWeigh(shown, found, len(terms))
	if !highlighted {
		weight.marks = 0
	}
	return workspaceSearchPassageBlock{text: string(shown[start:end]), marks: kept, weight: weight}, true
}

// workspaceSearchPassageKeywords are where a line, as shown, holds the
// keywords a passage looks for, in its order: a keyword anywhere in it, and
// an English keyword's stem as a whole word.
func workspaceSearchPassageKeywords(shown []rune, terms []string) []workspaceSearchKeywordAt {
	folded, of := workspaceSearchFoldedRunes(shown)
	found := []workspaceSearchKeywordAt{}
	shownAt := func(at int) int {
		if at < len(folded) {
			return of[at]
		}
		return len(shown)
	}
	for word, term := range terms {
		want := []rune(term)
		for at := 0; at+len(want) <= len(folded); at++ {
			if !slices.Equal(folded[at:at+len(want)], want) {
				continue
			}
			// A keyword standing as a word of its own outweighs one starting
			// or inside a word (workspace_search_weight.go).
			loose := at > 0 && workspaceSearchWordRune(folded[at-1]) ||
				at+len(want) < len(folded) && workspaceSearchWordRune(folded[at+len(want)])
			found = append(found, workspaceSearchKeywordAt{place: [2]int{of[at], shownAt(at + len(want))}, word: word, loose: loose})
		}
		// An English keyword holds a word of its stem, the whole word.
		if searchEnglishWord(term) {
			stem := englishStem(term)
			workspaceSearchEachWord(folded, func(start, end int) {
				held := string(folded[start:end])
				if !strings.Contains(held, term) && searchEnglishStemEqual(held, term, stem) {
					found = append(found, workspaceSearchKeywordAt{place: [2]int{of[start], shownAt(end)}, word: word, loose: true})
				}
			})
		}
	}
	slices.SortStableFunc(found, func(a, b workspaceSearchKeywordAt) int { return a.place[0] - b.place[0] })
	return found
}

// workspaceSearchEachWord calls on each word of folded text — letters, digits
// and their marks — with where it starts and ends.
func workspaceSearchEachWord(folded []rune, each func(start, end int)) {
	for start := 0; start < len(folded); {
		if !workspaceSearchWordRune(folded[start]) {
			start++
			continue
		}
		end := start
		for end < len(folded) && workspaceSearchWordRune(folded[end]) {
			end++
		}
		each(start, end)
		start = end
	}
}

// workspaceSearchOnlySpaces is whether what stands between two keywords is
// only spaces, so they are marked as one.
func workspaceSearchOnlySpaces(between []rune) bool {
	for _, r := range between {
		if !unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

// workspaceSearchFoldedRunes folds a line as search folds its text
// (foldSearchText), with the character of the line each folded one comes
// from: folding a character can change how many it is ("ß" two, a mark
// none), so a line as long folded may still have moved.
func workspaceSearchFoldedRunes(shown []rune) ([]rune, []int) {
	folded := make([]rune, 0, len(shown))
	of := make([]int, 0, len(shown))
	for index, r := range shown {
		switch {
		case r < utf8.RuneSelf:
			folded = append(folded, unicode.ToLower(r))
			of = append(of, index)
		case r >= 0xAC00 && r <= 0xD7A3:
			// A Hangul syllable folds to itself.
			folded = append(folded, r)
			of = append(of, index)
		default:
			for _, f := range foldSearchText(string(r)) {
				folded = append(folded, f)
				of = append(of, index)
			}
		}
	}
	return folded, of
}
