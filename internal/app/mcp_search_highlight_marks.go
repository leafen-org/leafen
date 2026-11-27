package app

import (
	"slices"
	"strings"
)

// An agent's search highlight marks a line's keywords as Notion's search
// marks them (seen 2026-11-22 twice, in bodies and in titles of synthetic
// pages): each keyword on its own, even beside another ("**vornelk**
// **quistap** 끝" and "**quistap** **drumbel** 끝" for "vornelk quistap
// drumbel", "**vornelk** **vornelk**" for "vornelk", "**drumbel**
// **quistap** **vornelk**" in reverse), but the whole query standing in its
// order as one, with only spaces or punctuation between its words
// ("**vornelk quistap drumbel** 끝", "**vornelk, quistap; drumbel**", "2:30
// **PM (GMT**+9)" for "PM GMT"); with a phrase in quotes, each phrase
// standing in its order as one and every other keyword on its own
// ("**vornelk quistap** **drumbel** 끝" for `"vornelk quistap" drumbel`).
// A keyword is marked where a word holds it whole: as the word, a word of
// its stem, or a part of a word of letters and digits, as the app's
// highlights take it (workspace_search_highlight_places.go). A keyword of
// letters and digits is marked by its parts too, a word's parts side by side
// as one mark ("**kravnox77** 앞", "**77zt** 뒤", "**kravnox** 글자" and
// "**kravnox77**ztq 더 김" for "kravnox77zt"; "j**7**" for "kravnox7"; seen
// 2026-11-22 twice), but not by the start of a longer word — the search
// still finds such a page (workspace_search_order.go), and shows it unmarked.
// Its lines are weighed as the app's (workspace_search_weight.go), the whole
// query one mark and each part a keyword.

// mcpSearchHighlightQuery is a search's keywords as a highlight marks them:
// its words in order, quotes left out, and the runs of them marked as one
// where they stand in order.
type mcpSearchHighlightQuery struct {
	words  []string
	groups [][]int
	// units are what a line is looked through for: the words, then the
	// parts of the words of letters and digits; of is the word each is.
	units []string
	of    []int
}

// newMCPSearchHighlightQuery reads a search's keywords for its highlights.
func newMCPSearchHighlightQuery(query string) mcpSearchHighlightQuery {
	read := mcpSearchHighlightQuery{}
	phrased := false
	for _, part := range workspaceSearchQueryParts(foldSearchText(query)) {
		words := workspaceSearchWords(part.text)
		if len(words) == 0 {
			continue
		}
		if part.phrase {
			phrased = true
			read.groups = append(read.groups, mcpSearchHighlightRun(len(read.words), len(words)))
		}
		read.words = append(read.words, words...)
	}
	if !phrased && len(read.words) > 1 {
		read.groups = [][]int{mcpSearchHighlightRun(0, len(read.words))}
	}
	for index, word := range read.words {
		read.unit(word, index)
	}
	for index, word := range read.words {
		for _, part := range searchWordParts(word) {
			read.unit(part, index)
		}
	}
	return read
}

func (query *mcpSearchHighlightQuery) unit(unit string, word int) {
	if !slices.Contains(query.units, unit) {
		query.units = append(query.units, unit)
		query.of = append(query.of, word)
	}
}

// plain is what a page's lines are looked through for, as a search's plain
// keywords (workspaceSearchPassageOf).
func (query mcpSearchHighlightQuery) plain() string {
	return strings.Join(query.units, " ")
}

// keywords are where a line holds the keywords as a highlight marks them, in
// its order, and the places a word's part holds alone.
func (query mcpSearchHighlightQuery) keywords(shown []rune) ([]workspaceSearchKeywordAt, map[[2]int]bool) {
	found := workspaceSearchKeywordsFound(shown, query.units)
	parts := map[[2]int]bool{}
	for index := range found {
		unit := found[index].word
		found[index].word = query.of[unit]
		if unit >= len(query.words) {
			parts[found[index].place] = true
		}
	}
	// A word holding a keyword whole is marked once, not by its parts too.
	found = slices.DeleteFunc(found, func(at workspaceSearchKeywordAt) bool {
		if !parts[at.place] {
			return false
		}
		for _, other := range found {
			if other.word == at.word && !parts[other.place] && other.place[0] <= at.place[0] && at.place[1] <= other.place[1] {
				return true
			}
		}
		return false
	})
	return slices.CompactFunc(found, func(a, b workspaceSearchKeywordAt) bool { return a.place == b.place }), parts
}

func mcpSearchHighlightRun(from, count int) []int {
	run := make([]int, count)
	for index := range run {
		run[index] = from + index
	}
	return run
}

// blockOf is a line as a highlight shows it, if it holds a keyword: the part
// of it a highlight shows (workspaceSearchHighlightWindow), its marks and its
// weight.
func (query mcpSearchHighlightQuery) blockOf(line string) (workspaceSearchPassageBlock, bool) {
	shown := []rune(searchLineShown(line))
	found, parts := query.keywords(shown)
	if len(found) == 0 {
		return workspaceSearchPassageBlock{}, false
	}
	marks := query.marks(shown, found, parts)
	// Weighed as the app's lines and highlights are (workspace_search_weight.go).
	weight := workspaceSearchWeigh(shown, found, len(query.words))
	start, end := workspaceSearchHighlightWindow(shown, marks[0])
	kept := [][2]int{}
	for _, mark := range marks {
		mark[0], mark[1] = max(mark[0], start)-start, min(mark[1], end)-start
		if mark[0] >= mark[1] {
			continue
		}
		// Marks touching are one.
		if last := len(kept) - 1; last >= 0 && mark[0] <= kept[last][1] {
			kept[last][1] = max(kept[last][1], mark[1])
			continue
		}
		kept = append(kept, mark)
	}
	return workspaceSearchPassageBlock{text: string(shown[start:end]), marks: kept, weight: weight}, true
}

// marks are a line's keywords' places in order, each run of a group standing
// in order as one.
func (query mcpSearchHighlightQuery) marks(shown []rune, found []workspaceSearchKeywordAt, parts map[[2]int]bool) [][2]int {
	marks := make([][2]int, 0, len(found))
	for index := 0; index < len(found); {
		run := 1
		for _, group := range query.groups {
			if query.standsInOrder(shown, found[index:], group, parts) {
				run = len(group)
				break
			}
		}
		marks = append(marks, [2]int{found[index].place[0], found[index+run-1].place[1]})
		index += run
	}
	return marks
}

// standsInOrder is whether a group's words stand at the start of found, each
// whole after the one before, with only spaces or punctuation between them.
func (query mcpSearchHighlightQuery) standsInOrder(shown []rune, found []workspaceSearchKeywordAt, group []int, parts map[[2]int]bool) bool {
	if len(found) < len(group) {
		return false
	}
	for index, word := range group {
		if found[index].word != word || found[index].loose || parts[found[index].place] {
			return false
		}
		if index == 0 {
			continue
		}
		from, to := found[index-1].place[1], found[index].place[0]
		if from >= to {
			return false
		}
		for _, r := range shown[from:to] {
			if workspaceSearchWordRune(r) {
				return false
			}
		}
	}
	return true
}
