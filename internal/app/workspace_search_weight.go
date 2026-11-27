package app

// How Notion's search weighs a found page's blocks — which one its result's
// line shows (workspace_search_passage.go), the order of the highlights
// beside it (workspace_search_highlights.go) and of an agent's
// (mcp_search_highlight_marks.go), its search's highlights in the order
// its server ranks them — seen 2026-11-14 on twenty-one synthetic pages
// under the test page: first by its marks, each keyword it holds a mark and
// all the query's words side by side in their order one ("vorquil first line
// tamesh second line" before "vorquil tamesh together", "lurvane x tospik x
// lurvane" before "lurvane tospik and lurvane tospik"; "kettravin
// fozzlemir" for "fozzlemir kettravin", "wembrody wembrody", and "belsquo
// trimadel" for "corvanith belsquo trimadel" are two), then by how many
// keywords it holds ("hostrevin quellamy x hostrevin" before "hostrevin x
// quellamy"), then by the page's order. How many of the query's words it
// holds is not weighed ("bratimor x bratimor x bratimor" before "bratimor x
// selvanq x bratimor", "glomberth x glomberth" before "glomberth
// yarrimol"). Between blocks weighing the same, one holding more keywords as
// words of their own comes first (seen 2026-11-16: for "kestrum", "kestrum
// trelbin line three" before "kestrums line one", and for "kestrums"
// "kestrums line one" before it). In a query of several words a mark of a
// keyword's English stem alone weighs less than one holding a keyword as
// written — two such less, three more (seen 2026-11-24 in the business
// trial workspace, the server's ranks of a page's highlights, each pair
// both ways round in the page: for "jump zzqq" and "jump trevo" "jump
// trevo" before "jumping x jumping", for "run zzqq" "running x running x
// running" before "run alone"; for "jump" and "run" alone the stems' lines
// first; for "grindle line" and "walk plonk" "grindle line one" and "walk
// plonk one" before "grindles line three" and "walks plonk three").
type workspaceSearchWeight struct {
	// marks is the worth of its marks (workspaceSearchMarkWritten).
	marks, keywords, whole int
}

// workspaceSearchKeywordAt is where a line holds one of the query's words,
// in its characters, and which of them (its place in the query); loose when
// the word only starts or holds it, or has its English stem.
type workspaceSearchKeywordAt struct {
	place [2]int
	word  int
	loose bool
}

// The worth of a mark: one holding a keyword as written, or any in a query
// of one word; one of a keyword's other forms alone in a query of several.
const (
	workspaceSearchMarkWritten = 5
	workspaceSearchMarkForm    = 2
)

// workspaceSearchWeigh weighs a line by the query's words it holds, in the
// line's order, of a query of `words` words.
func workspaceSearchWeigh(shown []rune, found []workspaceSearchKeywordAt, words int) workspaceSearchWeight {
	weight := workspaceSearchWeight{keywords: len(found)}
	for _, at := range found {
		if !at.loose {
			weight.whole++
		}
	}
	for index := 0; index < len(found); {
		run := 1
		if words > 1 && workspaceSearchWholeQueryAt(shown, found[index:], words) {
			run = words
		}
		worth := workspaceSearchMarkForm
		for _, at := range found[index : index+run] {
			if !at.loose || words == 1 {
				worth = workspaceSearchMarkWritten
			}
		}
		weight.marks += worth
		index += run
	}
	return weight
}

// workspaceSearchWholeQueryAt is whether the query's words stand at the
// start of `found` in their order, each after the one before with only
// spaces or marks between them — "11 23", "11/23", "2026-11-23" — as Notion
// marks them one (seen 2026-11-24 for "11 23", "11/23" and "2026-11-23"; an
// agent's highlight the same, mcp_search_highlight_marks.go).
func workspaceSearchWholeQueryAt(shown []rune, found []workspaceSearchKeywordAt, words int) bool {
	if len(found) < words {
		return false
	}
	for index := range words {
		if found[index].word != index {
			return false
		}
		if index == 0 {
			continue
		}
		between := shown[min(found[index-1].place[1], found[index].place[0]):found[index].place[0]]
		if len(between) == 0 {
			return false
		}
		for _, r := range between {
			if workspaceSearchWordRune(r) {
				return false
			}
		}
	}
	return true
}

// compare orders the heavier first, for a stable sort that keeps the page's
// order between blocks of the same weight.
func (weight workspaceSearchWeight) compare(other workspaceSearchWeight) int {
	if weight.marks != other.marks {
		return other.marks - weight.marks
	}
	if weight.keywords != other.keywords {
		return other.keywords - weight.keywords
	}
	return other.whole - weight.whole
}
