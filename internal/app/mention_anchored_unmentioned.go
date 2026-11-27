package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page hanging on a line, a table row, a captioned block, a page's title
// or a database page's cell (mention_anchored_pages.go,
// mention_anchored_fields.go) whose mention there an integration took out
// through the Public API stays there unmentioned, as Notion's does (seen
// 2026-11-18 in two tabs of a Notion test page): it is no page in the trash,
// Notion's API still gives that block or page as its parent, and Notion's
// sidebar leaves it out until a mention of it is back there, put in through
// the API or typed. Later edits of the block or field leave it so; the
// block deleted takes it to the trash, and the block back, by Undo or the
// API's restore, brings it back still unmentioned. One whose mention a
// person took out went to the trash instead, and stays there when its block
// goes and comes back. anchor_unmentioned keeps it so beside anchor_lost: in
// the trash with both, a page went with its block while unmentioned there.

const (
	pageAnchorUnmentionedField = "anchor_unmentioned"
	// pageAnchorsUnmentionedSeenStoreKey marks an app whose pages can hang
	// unmentioned yet.
	pageAnchorsUnmentionedSeenStoreKey = "leafenPageAnchorsUnmentionedSeen"
)

// pageAnchorsUnmentionedKnown reports whether pages can hang unmentioned
// yet: the sidebar and the anchors' settling read it, which migrations
// before 825 run too.
func pageAnchorsUnmentionedKnown(app core.App) (bool, error) {
	if app.Store().Has(pageAnchorsUnmentionedSeenStoreKey) {
		return true, nil
	}
	var columns int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM pragma_table_info('leafen_pages') WHERE [[name]] = {:name}`).
		Bind(dbx.Params{"name": pageAnchorUnmentionedField}).Row(&columns); err != nil || columns == 0 {
		return false, err
	}
	app.Store().Set(pageAnchorsUnmentionedSeenStoreKey, true)
	return true, nil
}

// pageAnchorUnmentionedColumn is the column to read whether a page hangs
// unmentioned by, or none yet.
func pageAnchorUnmentionedColumn(app core.App) (string, error) {
	known, err := pageAnchorsUnmentionedKnown(app)
	if err != nil || !known {
		return "FALSE", err
	}
	return "[[" + pageAnchorUnmentionedField + "]]", nil
}

// setPageAnchorUnmentioned marks a page as hanging where its mention is no
// longer, or as mentioned there again, and tells the screens showing its
// sidebar, which lists it only while mentioned.
func setPageAnchorUnmentioned(app core.App, pageID string, unmentioned bool) error {
	if known, err := pageAnchorsUnmentionedKnown(app); err != nil || !known {
		return err
	}
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_pages}} SET [[anchor_unmentioned]] = {:unmentioned}
		WHERE [[id]] = {:id} AND [[anchor_unmentioned]] != {:unmentioned}
	`).Bind(dbx.Params{"id": pageID, "unmentioned": unmentioned}).Execute()
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed == 0 {
		return err
	}
	return notePageListChange(app, pageID)
}

// notePageListChange keeps a change of a page's sidebar record for a column
// the sidebar's triggers don't follow (page_list_changes_schema.go,
// schema_baseline.json), as they keep one for its title or
// place: screens following the list read the page again.
func notePageListChange(app core.App, pageID string) error {
	params := dbx.Params{"id": pageID}
	for _, statement := range []string{
		`UPDATE {{leafen_page_list_change_sequence}} SET [[seq]] = [[seq]] + 1 WHERE [[singleton]] = 'pages'`,
		`INSERT INTO {{leafen_page_list_changes}} ([[workspace_id]], [[page_id]], [[seq]], [[subtree_seq]])
		SELECT [[page]].[[workspace]], [[page]].[[id]], [[sequence]].[[seq]], 0
		FROM {{leafen_pages}} [[page]]
		JOIN {{leafen_page_list_change_sequence}} [[sequence]] ON [[sequence]].[[singleton]] = 'pages'
		JOIN {{leafen_workspaces}} [[changed_workspace]] ON [[changed_workspace]].[[id]] = [[page]].[[workspace]]
			AND [[changed_workspace]].[[deletion_job_id]] = ''
		WHERE [[page]].[[id]] = {:id}
		ON CONFLICT ([[workspace_id]], [[page_id]]) DO UPDATE SET [[seq]] = excluded.[[seq]]`,
		`UPDATE {{leafen_workspace_content_revisions}} SET [[page_list_revision]] = [[page_list_revision]] + 1
		WHERE [[workspace_id]] = (SELECT [[workspace]] FROM {{leafen_pages}} WHERE [[id]] = {:id})`,
	} {
		if _, err := app.DB().NewQuery(statement).Bind(params).Execute(); err != nil {
			return err
		}
	}
	return nil
}

// loadUnmentionedAnchoredPageSet says which of the pages hang unmentioned,
// which the sidebar leaves out as Notion's does.
func loadUnmentionedAnchoredPageSet(app core.App, pageIDs []string) (map[string]bool, error) {
	result := map[string]bool{}
	if len(pageIDs) == 0 {
		return result, nil
	}
	if known, err := pageAnchorsUnmentionedKnown(app); err != nil || !known {
		return result, err
	}
	encoded, _ := json.Marshal(pageIDs)
	ids := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_pages}}
		WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids})) AND [[anchor_unmentioned]] = TRUE
	`).Bind(dbx.Params{"ids": string(encoded)}).Column(&ids); err != nil {
		return nil, err
	}
	for _, id := range ids {
		result[id] = true
	}
	return result, nil
}

// loadPageSidebarAnchorSets says which of the pages the sidebar leaves out
// for where they hang: on a table row's cells or a caption
// (loadFieldAnchoredPageSet), or unmentioned.
func loadPageSidebarAnchorSets(app core.App, pageIDs []string) (field, unmentioned map[string]bool, err error) {
	if field, err = loadFieldAnchoredPageSet(app, pageIDs); err != nil {
		return nil, nil, err
	}
	unmentioned, err = loadUnmentionedAnchoredPageSet(app, pageIDs)
	return field, unmentioned, err
}

// sidebarTreeLeftOut says which of the pages the sidebar's tree leaves out
// where they stand, beside those in the trash or archived, which their own
// columns say: a button's template pages (loadButtonTemplatePageSet) and
// those hanging on a table row's cells or a caption, or unmentioned
// (loadPageSidebarAnchorSets) — the rule the sidebar draws by
// (sidebarSectionHeadsOf, the app's pageTreeModel.ts), for a reader listing
// pages as it does (page_children_routes.go).
func sidebarTreeLeftOut(app core.App, pageIDs []string) (map[string]bool, error) {
	leftOut, err := loadButtonTemplatePageSet(app, pageIDs)
	if err != nil {
		return nil, err
	}
	field, unmentioned, err := loadPageSidebarAnchorSets(app, pageIDs)
	if err != nil {
		return nil, err
	}
	for _, set := range []map[string]bool{field, unmentioned} {
		for id := range set {
			leftOut[id] = true
		}
	}
	return leftOut, nil
}

// setCopiedPageAnchor hangs a copied sub-page on its anchor's copy as its
// source hangs, unmentioned there or not.
func setCopiedPageAnchor(target, source *core.Record, anchor string) {
	target.Set(pageAnchorBlockField, anchor)
	if anchor != "" && source.GetBool(pageAnchorUnmentionedField) {
		target.Set(pageAnchorUnmentionedField, true)
	}
}
