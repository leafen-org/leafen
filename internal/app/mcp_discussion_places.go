package app

import (
	"encoding/json"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A discussion left on words stands where its marks stand now, as Notion's
// MCP and Markdown place one (seen 2026-11-22 through Notion's MCP and its
// public API, worker w2339, after update_content moved marks: a span added
// in another block naming a discussion, its first one taken away):
//
//   - In a page's Markdown each <span discussion-urls> names the block it
//     stands in ("mike <span …/<mike's block>/…>november</span>" for a
//     discussion first left on "echo" in another block), one discussion in
//     two blocks by each block in its own (notionMarkdownMentions.Block).
//   - get-comments' list and fetch's examples order it by the first block
//     marked with it, and name it by the last ("whiskey", marked first on
//     line 8 and then also on line 1, listed after line 1's "bravo" by line
//     8's block); fetch's discussion-count counts it once for each block it
//     marks (8 discussions, two in two blocks and one in none: "9"), and
//     both leave out one whose marks are all gone (get-comments 7, its
//     total-count too, include_resolved alike).
//   - get-comments by a discussion_id finds any of them, by either block's
//     address, and names it by the block it was left on, without its
//     context when that block no longer holds its mark (text-context
//     stays).
//
// A discussion on a page or on a block, which marks no words, stands where
// it was left, as before; so does a suggested edit's.

// mcpDiscussionPlaces are where a page's discussions stand: the blocks each
// one left on words marks now, in the order the page reads them, by its
// first comment's ID, and the page's blocks in that order.
type mcpDiscussionPlaces struct {
	marked map[string][]string
	words  map[string]bool
	order  map[string]int
}

// mcpPlaceDiscussions reads where a page's or a database page's (room)
// discussions stand.
func mcpPlaceDiscussions(app core.App, room string, roots []commentRecord) mcpDiscussionPlaces {
	places := mcpDiscussionPlaces{marked: map[string][]string{}, words: map[string]bool{}, order: map[string]int{"": 0}}
	captions := []commentTextPlace{}
	owners := map[commentTextPlace]string{}
	for _, root := range roots {
		anchor := commentQuoteAnchor{}
		if root.BlockID == "" || json.Unmarshal(root.Body, &anchor) != nil || !anchor.onWords() {
			continue
		}
		if body := (suggestionThreadBody{}); json.Unmarshal(root.Body, &body) == nil && body.Suggestion.Kind != "" {
			continue
		}
		places.words[root.ID] = true
		for _, place := range anchor.textPlaces(root.BlockID) {
			if place.caption || place.cell > 0 {
				captions, owners[place] = append(captions, place), root.ID
			}
		}
	}
	ids := []string{}
	if len(places.words) > 0 {
		if err := app.DB().NewQuery(`
			SELECT [[id]] FROM {{leafen_blocks}} INDEXED BY [[idx_leafen_blocks_search_projection]]
			WHERE [[page_id]]={:pageId} AND ` + blockStandsSQL("") + `
		`).Bind(dbx.Params{"pageId": room}).Column(&ids); err != nil {
			ids = nil
		}
		if document, found, err := loadYgoDoc(app, room); err == nil && found {
			for _, id := range ids {
				for _, part := range document.GetText(ygoBlockTextRootPrefix + id).ToDelta() {
					threads, _ := part.Attributes["commentId"].(string)
					for _, thread := range strings.Fields(threads) {
						if places.words[thread] && !places.marks(thread, id) {
							places.marked[thread] = append(places.marked[thread], id)
						}
					}
				}
			}
		}
		// A table cell's marks stand where they are now, its row's words moved
		// or the row written again (comment_table_cell_anchors.go).
		for id, threads := range commentTableCellThreads(app, room, ids) {
			for _, thread := range threads {
				if places.words[thread] && !places.marks(thread, id) {
					places.marked[thread] = append(places.marked[thread], id)
				}
			}
		}
		// A caption's marks stand where they were made.
		originals := &notionCommentOriginals{}
		for _, place := range captions {
			if marked, err := originals.marked(app, room, place, owners[place]); err == nil && marked && !places.marks(owners[place], place.blockID) {
				places.marked[owners[place]] = append(places.marked[owners[place]], place.blockID)
			}
		}
	}
	for _, root := range roots {
		if root.BlockID != "" {
			ids = append(ids, root.BlockID)
		}
	}
	for index, id := range mcpBlocksInReadingOrder(app, room, uniqueStrings(ids)) {
		places.order[id] = index + 1
	}
	for thread, blocks := range places.marked {
		ordered := make([]string, len(blocks))
		copy(ordered, blocks)
		for left := 1; left < len(ordered); left++ {
			for right := left; right > 0 && places.at(ordered[right]) < places.at(ordered[right-1]); right-- {
				ordered[right], ordered[right-1] = ordered[right-1], ordered[right]
			}
		}
		places.marked[thread] = ordered
	}
	return places
}

// at is where a block stands in the page's reading order, one the page's
// structure doesn't reach last.
func (places mcpDiscussionPlaces) at(blockID string) int {
	if index, found := places.order[blockID]; found {
		return index
	}
	return len(places.order) + 1
}

// marks reports whether a discussion marks a block's words.
func (places mcpDiscussionPlaces) marks(thread, blockID string) bool {
	for _, marked := range places.marked[thread] {
		if marked == blockID {
			return true
		}
	}
	return false
}

// listed reports whether a list names a discussion: not one left on words
// whose marks are all gone.
func (places mcpDiscussionPlaces) listed(root commentRecord) bool {
	return !places.words[root.ID] || len(places.marked[root.ID]) > 0
}

// first is where a list places a discussion: by the first block it marks,
// or the one it was left on.
func (places mcpDiscussionPlaces) first(root commentRecord) int {
	if marked := places.marked[root.ID]; places.words[root.ID] && len(marked) > 0 {
		return places.at(marked[0])
	}
	return places.at(root.BlockID)
}

// block is the block a list names a discussion by: the last it marks, or
// the one it was left on.
func (places mcpDiscussionPlaces) block(root commentRecord) string {
	if marked := places.marked[root.ID]; places.words[root.ID] && len(marked) > 0 {
		return marked[len(marked)-1]
	}
	return root.BlockID
}

// blocks is how many times fetch's discussion-count counts a discussion:
// once for each block it marks, or once.
func (places mcpDiscussionPlaces) blocks(thread string) int {
	return max(len(places.marked[thread]), 1)
}

// mcpDiscussionWhere is a discussion element's context attributes: its
// context, and for one left on words the words; no context for one asked
// for by its ID whose block no longer holds its mark.
func mcpDiscussionWhere(discussion notionMCPDiscussion) string {
	where := ""
	if discussion.Context != "" {
		where = `context="` + discussion.Context + `"`
	}
	if discussion.Context == "inline" || discussion.Context != "page" && discussion.TextContext != "" {
		where = strings.TrimSpace(where + ` text-context="` + mcpFetchAttribute(discussion.TextContext) + `"`)
	}
	return where
}

// mcpDiscussionAddressAt is a discussion's address naming the block a run
// of words marked with it stands in, its page and its own ID as they are;
// as it is for none.
func mcpDiscussionAddressAt(address, blockID string) string {
	parts := strings.Split(strings.TrimPrefix(address, "discussion://"), "/")
	if blockID == "" || len(parts) != 3 {
		return address
	}
	return mcpDiscussionAddress(parts[0], blockID, parts[2])
}

// kept are the discussions a list names, or the one asked for by its ID.
func (places mcpDiscussionPlaces) kept(roots []commentRecord, asked bool) []commentRecord {
	if asked {
		return roots
	}
	kept := roots[:0]
	for _, root := range roots {
		if places.listed(root) {
			kept = append(kept, root)
		}
	}
	return kept
}

// named is the block a discussion's address names: the one it was left on
// when asked for by its ID, and in a list the last it marks.
func (places mcpDiscussionPlaces) named(root commentRecord, asked bool) string {
	if asked {
		return root.BlockID
	}
	return places.block(root)
}

// context is a discussion left on words' context: inline, or none when
// asked for by its ID with its block no longer holding its mark.
func (places mcpDiscussionPlaces) context(root commentRecord, asked bool) string {
	if asked && places.words[root.ID] && !places.marks(root.ID, root.BlockID) {
		return ""
	}
	return "inline"
}
