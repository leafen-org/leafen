package app

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// notion-fetch lists a page's blocks when asked: Markdown names none of them,
// and what a person does to one block by its menu — copying its link, moving
// it — an agent does by its address and ID.

type notionMCPFetchedBlock struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	ParentID string `json:"parent_id,omitempty"`
	URL      string `json:"url"`
	Text     string `json:"text,omitempty"`
}

const (
	mcpFetchedBlockTextRunes = 200
	// mcpFetchedBlockWindow is how many blocks a fetch lists at a time; its
	// blocks_next_cursor lists on from there to the last.
	mcpFetchedBlockWindow = 1000
)

// mcpPageBlockList lists a window of an authorized page's blocks in their
// tree's order, after the block cursor names ("" from the first); next names
// the last one listed while more follow.
func mcpPageBlockList(ctx context.Context, app core.App, pageID, cursor string) ([]notionMCPFetchedBlock, string, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, "", err
	}
	if _, err := authorizePageBody(app, authority.PrincipalID, pageID, permissionWorkspaceRead); err != nil {
		return nil, "", mcpRefusedUnlessStorage(errMCPCantRead, err)
	}
	// Every block's place, however many: the tree's order is read from all of
	// them, and only the window's text below.
	rows := []struct {
		ID        string `db:"id"`
		ParentID  string `db:"parent_id"`
		Type      string `db:"type"`
		Suggested bool   `db:"suggested"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], COALESCE([[parent_id]], '') AS [[parent_id]], [[type]],
			CASE WHEN json_valid([[content]]) THEN
				CASE WHEN json_type([[content]], '$.` + graphBlockSuggestionKey + `') = 'text'
					AND json_extract([[content]], '$.` + graphBlockSuggestionKey + `') != '' THEN 1 ELSE 0 END
			ELSE 0 END AS [[suggested]]
		FROM {{leafen_blocks}} WHERE [[page_id]] = {:pageId} ORDER BY [[position]], [[id]]
	`).Bind(dbx.Params{"pageId": pageID}).All(&rows); err != nil {
		return nil, "", failedBecause(errMCPToolFailed, err)
	}
	children := map[string][]int{}
	for index, row := range rows {
		children[row.ParentID] = append(children[row.ParentID], index)
	}
	// The tree's order; a suggested block and what it holds aren't there yet.
	order := make([]int, 0, len(rows))
	var walk func(parent string)
	walk = func(parent string) {
		for _, index := range children[parent] {
			if !rows[index].Suggested {
				order = append(order, index)
				walk(rows[index].ID)
			}
		}
	}
	walk("")
	start := 0
	if cursor = strings.TrimSpace(cursor); cursor != "" {
		start = -1
		for at, index := range order {
			if rows[index].ID == cursor {
				start = at + 1
			}
		}
		if start < 0 {
			return nil, "", mcpInvalid("blocks_cursor is the blocks_next_cursor of the last fetch, and the block it names is gone: fetch the blocks from the start")
		}
	}
	end, next := min(start+mcpFetchedBlockWindow, len(order)), ""
	if end < len(order) {
		next = rows[order[end-1]].ID
	}
	document, _, err := loadYgoDoc(app, pageID)
	if err != nil {
		return nil, "", failedBecause(errMCPToolFailed, err)
	}
	listed := make([]notionMCPFetchedBlock, 0, end-start)
	for _, index := range order[start:end] {
		row := rows[index]
		text := ""
		if !graphBlockWordless(row.Type) {
			_, text = rowPageBlockTextAsItStands(document, row.ID, false)
		}
		if utf8.RuneCountInString(text) > mcpFetchedBlockTextRunes {
			text = string([]rune(text)[:mcpFetchedBlockTextRunes]) + "…"
		}
		listed = append(listed, notionMCPFetchedBlock{
			ID: row.ID, Type: row.Type, ParentID: row.ParentID, Text: text,
			URL: mcpPageURL(authority, pageID) + mcpDatabaseLinkFragment + row.ID,
		})
	}
	return listed, next, nil
}
