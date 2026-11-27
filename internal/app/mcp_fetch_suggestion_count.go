package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Fetch's discussion-count counts an open suggested edit once for each block
// it changes, as Notion's does: a suggestion is one discussion that each block
// it changes lists (seen 2026-11-22 in Notion's records and its MCP fetch, a
// page with suggestions made on its screen: a replacement in a block 1, Enter
// at a line's end with or without words typed on the new line 2 — the line
// with the break and the new line — a break in a line's middle 1, the line
// before it only, two blocks suggested away 2; discussion-count 10 for them
// all with one word suggested away and one typed apart). Leafen's suggestion
// is one thread too: a suggestion of new blocks changes the block whose end
// takes the break and each new block (a block a menu picked takes no break,
// so its block alone), a removal each block it takes away, any other the
// block it is in.

// mcpFetchSuggestionBlocks is how many blocks each open suggestion of a page
// changes, by its thread's ID.
func mcpFetchSuggestionBlocks(app core.App, pageID string, records []commentRecord) (map[string]int, error) {
	counts := map[string]int{}
	for _, record := range records {
		body := suggestionThreadBody{}
		if record.ParentID != "" || record.Resolved || json.Unmarshal(record.Body, &body) != nil ||
			body.Suggestion.Kind == "" || body.Suggestion.State != "" {
			continue
		}
		switch {
		case len(body.Suggestion.Removed) > 0:
			counts[record.ID] = len(body.Suggestion.Removed)
		case body.Suggestion.Picked:
			counts[record.ID] = 0
		default:
			counts[record.ID] = 1
		}
	}
	if len(counts) == 0 {
		return counts, nil
	}
	suggested := []string{}
	if err := app.DB().NewQuery(`
		SELECT json_extract([[content]], '$.` + graphBlockSuggestionKey + `') FROM {{leafen_blocks}}
		WHERE [[page_id]]={:pageId} AND json_valid([[content]])
			AND json_type([[content]], '$.` + graphBlockSuggestionKey + `') = 'text'
	`).Bind(dbx.Params{"pageId": pageID}).Column(&suggested); err != nil {
		return nil, err
	}
	for _, id := range suggested {
		if count, open := counts[id]; open {
			counts[id] = count + 1
		}
	}
	for id, count := range counts {
		counts[id] = max(count, 1)
	}
	return counts, nil
}
