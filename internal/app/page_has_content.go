package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// withPageContents says of each page listed whether its body holds
// anything, so an empty page or database page shows the empty document as
// the sidebar does (Notion's, seen 2026-11-10: backlinks; 2026-11-11: an
// empty row found by search): a list that brings pages apart from the
// sidebar's — backlinks, search results, the library's rows — says it with
// them.
func withPageContents(app core.App, pages []*pageResponse) error {
	ids := make([]string, 0, len(pages))
	for _, page := range pages {
		ids = append(ids, page.ID)
	}
	contents, err := pageContentsByID(app, ids)
	if err != nil {
		return err
	}
	for _, page := range pages {
		if has, found := contents[page.ID]; found {
			page.HasContent = boolPointer(has)
		}
	}
	return nil
}

// pageContentsByID is, of the pages named, whether each one's body holds
// anything (pageHasContentSQL), by id; a page not found isn't in it.
func pageContentsByID(app core.App, ids []string) (map[string]bool, error) {
	contents := make(map[string]bool, len(ids))
	if len(ids) == 0 {
		return contents, nil
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows := []struct {
		ID         string `db:"id"`
		HasContent bool   `db:"has_content"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[page]].[[id]], ` + pageHasContentSQL + ` AS [[has_content]]
		FROM {{leafen_pages}} [[page]] WHERE [[page]].[[id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"ids": string(encoded)}).All(&rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		contents[row.ID] = row.HasContent
	}
	return contents, nil
}
