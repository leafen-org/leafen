package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
)

// The scan's search looks, as the import does
// (notion_import_unmentioned_pages.go), for pages hanging on what it read
// that its tree does not show — a line's, a table row's, a caption's, a
// title's or a cell's "@" page, mentioned there or not — and for pages and
// databases in rows' bodies. Each was made
// after what it hangs on, so a pass reads the search newest first down to
// when the oldest of what it looks under was made, less the import's margin
// (notionImportUnmentionedMargin): what the scan read before it began, or
// since the last pass began. What a pass finds is read in turn and the next
// pass looks under that, so a page hanging inside one found is counted as
// the run brings it.

// notionMigrationScanPass is a search pass's place in a scan's queue: Dated
// while the scan kept, from its start, when what it read was made; the time
// a pass under way reads down to (Bound, unix seconds); and whether it
// queued the next pass (Next).
type notionMigrationScanPass struct {
	Dated bool  `json:"dated,omitempty"`
	Bound int64 `json:"bound,omitempty"`
	Next  bool  `json:"next,omitempty"`
}

// notionMigrationScanMade is when the oldest of what a scan step read that a
// page can hang on was made (notionHangingParentMade): unix seconds, 0 for
// none; undated when Notion gave one no time. A search step that begins a
// pass takes what was kept so far (begin).
type notionMigrationScanMade struct {
	Since   int64 `json:"since,omitempty"`
	Undated bool  `json:"undated,omitzero"`
	begin   bool
}

func (made *notionMigrationScanMade) note(objects ...notionProviderObject) {
	for _, object := range objects {
		at, holds, dated := notionHangingParentMade(object)
		switch {
		case !holds:
		case !dated:
			made.Undated = true
		case made.Since == 0 || at.Unix() < made.Since:
			made.Since = at.Unix()
		}
	}
}

// notionMigrationSearchMadeKey keeps, among the scan's seen ids, when what it
// read since the last pass began was made; no Notion id reads so.
const notionMigrationSearchMadeKey = "search:made"

// searchMade is what the scan kept of when what it read was made.
func (store notionMigrationScanStore) searchMade() (notionMigrationScanMade, error) {
	made := notionMigrationScanMade{}
	value, found, err := store.find(notionMigrationItemSeen, notionMigrationSearchMadeKey)
	if err != nil || !found {
		return made, err
	}
	return made, json.Unmarshal([]byte(value), &made)
}

// keepSearchMade keeps when what a step read was made, the oldest since the
// last pass began; a step beginning a pass starts it again. One that had no
// time makes every pass after it read the search through.
func (store notionMigrationScanStore) keepSearchMade(step notionMigrationScanMade) error {
	if step.Since == 0 && !step.Undated && !step.begin {
		return nil
	}
	kept, err := store.searchMade()
	if err != nil {
		return err
	}
	if step.begin {
		kept.Since = 0
	}
	if step.Since != 0 && (kept.Since == 0 || step.Since < kept.Since) {
		kept.Since = step.Since
	}
	kept.Undated = kept.Undated || step.Undated
	encoded, err := json.Marshal(kept)
	if err != nil {
		return err
	}
	_, err = store.app.DB().NewQuery(`
		INSERT INTO {{leafen_notion_migration_scan_items}} ([[migration_id]],[[workspace_id]],[[kind]],[[item_key]],[[ordinal]],[[value]])
		VALUES ({:migrationId},{:workspaceId},{:kind},{:key},0,{:value})
		ON CONFLICT ([[migration_id]],[[kind]],[[item_key]]) DO UPDATE SET [[value]] = excluded.[[value]]
	`).Bind(dbx.Params{"migrationId": store.migrationID, "workspaceId": store.workspaceID, "kind": notionMigrationItemSeen,
		"key": notionMigrationSearchMadeKey, "value": string(encoded)}).Execute()
	return err
}
