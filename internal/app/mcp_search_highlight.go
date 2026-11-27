package app

import (
	"slices"
	"strings"
	"unicode"
)

// An agent's search highlight says a found page's words as Notion's search
// says them in its highlights (seen twice on 2026-11-22, by a reader of
// Korean): a page or database mention says nothing, a date mention its day
// in full in English, with its time and zone ("November 22, 2026", "November
// 22, 2026 9:30 AM (GMT+9)", "November 22, 2026 → November 24, 2026"),
// whoever reads it, as the index keeps it (workspace_search_date_words.go),
// and an inline equation its expression ("x^2"). A line of the page's body or
// a sub-page's line in it says each run of spaces as one ("w2331zq 본문 와
// November 22, 2026 끝" where a mention stood between two spaces, "공백 두칸
// 세칸" for "공백  두칸   세칸"); a title, its spaces as written
// (mcpTitleAsSearched).

// mcpSearchHighlightPassage is a found page's passage as an agent's
// highlight says and marks it (mcp_search_highlight_marks.go), the heaviest
// block first, then in the page's order.
func mcpSearchHighlightPassage(passage workspaceSearchPassage, query mcpSearchHighlightQuery) workspaceSearchPassage {
	highlighted := make(workspaceSearchPassage, 0, len(passage))
	for _, block := range passage {
		if again, held := query.blockOf(mcpSearchLineSpaced(block.text)); held {
			again.at = block.at
			highlighted = append(highlighted, again)
		}
	}
	slices.SortStableFunc(highlighted, func(a, b workspaceSearchPassageBlock) int {
		if order := a.weight.compare(b.weight); order != 0 {
			return order
		}
		return a.at - b.at
	})
	return highlighted
}

// mcpSearchLineSpaced is a line of a page's body with each run of spaces
// as one, as a highlight says it.
func mcpSearchLineSpaced(line string) string {
	var spaced strings.Builder
	space := false
	for _, r := range line {
		if unicode.IsSpace(r) {
			if !space {
				spaced.WriteRune(' ')
			}
			space = true
			continue
		}
		space = false
		spaced.WriteRune(r)
	}
	return spaced.String()
}
