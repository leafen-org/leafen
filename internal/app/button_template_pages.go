package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page made inside a button's draft, such as a database added to the
// blocks the button inserts, belongs to the button's template: Notion keeps
// it out of the sidebar and says it is a template being edited when it is
// opened (seen 2026-10-10). Its sub-page block sits, at any depth, in the
// draft's template block under the button.

// loadButtonTemplatePageSet says which of the pages sit in a button's draft.
func loadButtonTemplatePageSet(app core.App, pageIDs []string) (map[string]bool, error) {
	result := map[string]bool{}
	if len(pageIDs) == 0 {
		return result, nil
	}
	encoded, err := json.Marshal(pageIDs)
	if err != nil {
		return nil, err
	}
	found := []string{}
	// UNION, not UNION ALL: a damaged tree that loops ends instead of
	// climbing for ever. CROSS JOIN keeps SQLite starting from the wanted
	// pages and climbing by primary key, so the cost follows the page's
	// depth: with the statistics the nightly optimize wrote then, a plain JOIN
	// scanned every block for templates on each page read (2026-10-18).
	if err := app.DB().NewQuery(`
		WITH RECURSIVE [[ancestor]]([[page_id]], [[block_id]]) AS (
			SELECT [[block]].[[id]], [[block]].[[parent_id]]
			FROM json_each({:ids}) AS [[wanted]]
			CROSS JOIN {{leafen_blocks}} AS [[block]] ON [[block]].[[id]] = [[wanted]].[[value]]
			WHERE [[block]].[[type]] = {:childPage}
			UNION
			SELECT [[ancestor]].[[page_id]], [[block]].[[parent_id]]
			FROM [[ancestor]]
			CROSS JOIN {{leafen_blocks}} AS [[block]] ON [[block]].[[id]] = [[ancestor]].[[block_id]]
			WHERE [[ancestor]].[[block_id]] != ''
		)
		SELECT DISTINCT [[ancestor]].[[page_id]] FROM [[ancestor]]
		CROSS JOIN {{leafen_blocks}} AS [[template]] ON [[template]].[[id]] = [[ancestor]].[[block_id]]
			AND [[template]].[[type]] = {:template} AND json_extract([[template]].[[content]], '$.buttonDraft') = 1
		CROSS JOIN {{leafen_blocks}} AS [[button]] ON [[button]].[[id]] = [[template]].[[parent_id]]
			AND [[button]].[[page_id]] = [[template]].[[page_id]] AND [[button]].[[type]] = {:button}
	`).Bind(dbx.Params{
		"ids": string(encoded), "childPage": graphBlockTypeChildPage,
		"template": graphBlockTypeTemplate, "button": graphBlockTypeButton,
	}).Column(&found); err != nil {
		return nil, err
	}
	for _, id := range found {
		result[id] = true
	}
	return result, nil
}

// isButtonTemplatePage says whether one page sits in a button's draft.
func isButtonTemplatePage(app core.App, pageID string) (bool, error) {
	set, err := loadButtonTemplatePageSet(app, []string{pageID})
	return set[pageID], err
}
