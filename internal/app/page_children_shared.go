package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The pages a person opens only through shares (bootstrap_shared_pages.go)
// that page-children lists, read without walking down every page shared
// with them (bootstrap_shared_climb.go): under a page, its pages the walk
// reaches from it or starts from, which needs only whether it reaches the
// page itself, read by climbing from it; at the workspace's top, the pages
// the walk starts from whose parent it doesn't reach. Walking them all read
// every page shared with the person for each listing (2026-11-18: a member
// with 10,000 shared pages waited about a second at the NAS for a page's
// two hundred).

// loadSharedPageChildren reads up to limit of the pages a person opens only
// through shares, past after when it names one of them: under a page of
// them the pages directly under it, at the workspace's top each one whose
// parent the person doesn't open.
func loadSharedPageChildren(app core.App, principalID, workspaceID, parentID string, after pageChildrenCursor, limit int) ([]pageChildWire, error) {
	rows := []pageChildWire{}
	if limit < 1 {
		return rows, nil
	}
	// Nothing the walk starts from in the workspace, nothing in it opens
	// through shares: the short question spares the long statement, which
	// is compiled afresh each time, the person's own pages.
	params := bootstrapSharedParams(dbx.Params{
		"principalId": principalID, "workspaceId": workspaceID, "parentId": parentID, "limit": limit,
		"childWindow": pageChildWindow,
	})
	started := false
	if err := app.DB().NewQuery(sharedPagesStartInWorkspaceSQL).Bind(params).Row(&started); err != nil || !started {
		return rows, err
	}
	kept, err := pageChildKeptSQL(app, sharedPageChildKeptSQL("[[listed]].[[in_shared_walk]]"))
	if err != nil {
		return nil, err
	}
	past := pageChildrenAfterSQL(after, params)
	query := sharedPageChildrenUnderSQL(past)
	if parentID == "" {
		query = sharedPageChildrenTopSQL(past)
	}
	err = app.DB().NewQuery(query + `
		SELECT [[listed]].*, [[listed]].[[list_position]] AS [[position]],
			` + pageChildWindowListSQL("[[listed]].[[id]]", kept) + ` AS [[child_ids]]
		FROM [[listed]] ORDER BY [[listed]].[[list_position]], [[listed]].[[id]]
	`).Bind(params).All(&rows)
	for index := range rows {
		rows[index].Shared = true
	}
	return rows, err
}

// sharedPagesStartInWorkspaceSQL says whether the walk starts from any page
// of workspace {:workspaceId}.
var sharedPagesStartInWorkspaceSQL = bootstrapSharedBasesSQL + `
		SELECT EXISTS (SELECT 1 FROM [[shared_base]] WHERE [[shared_base]].[[workspace]] = {:workspaceId})`

// sharedPageChildColumnsSQL are a listed page's columns ([[page]]), its place
// among its siblings and, as inShared (an SQL expression) says, whether the
// walk reaches it.
func sharedPageChildColumnsSQL(inShared string) string {
	return `[[page]].[[id]], [[page]].[[title]], [[page]].[[icon]], [[page]].[[icon_type]], [[page]].[[kind]],
			[[page]].[[created]], [[page]].[[updated]], [[page]].[[position]] AS [[list_position]],
			(` + inShared + `) AS [[in_shared_walk]]`
}

// sharedPageChildConditionsSQL are what else a page the walk reaches
// ([[page]], its workspace [[workspace]]) needs to be listed: a live page of
// the workspace's owner, not one the person's sidebar shows already.
var sharedPageChildConditionsSQL = `[[workspace]].[[id]] = [[page]].[[workspace]]
			AND [[page]].[[owner]] = [[workspace]].[[owner]]
			AND [[page]].[[in_trash]] = FALSE
			AND [[page]].[[archived_at]] = ''
			AND [[page]].[[notion_import_staging]] = FALSE
			AND [[page]].[[deletion_job_id]] = ''
			AND ` + pageNotHeldForDeletionSQL("[[page]]", "[[page]].[[workspace]]") + `
			AND ` + bootstrapPageCompleteSQL + `
			AND NOT ` + bootstrapPageSeenSQL("[[page]]", "{:principalId}")

// sharedPageChildrenUnderSQL names, as listed, the pages under the page
// {:parentId} that the walk reaches, and that past (pageChildrenAfterSQL)
// keeps: one it starts from, or one it reaches from it when it reaches the
// page, read by climbing from it.
func sharedPageChildrenUnderSQL(past string) string {
	return bootstrapSharedBasesSQL +
		bootstrapSharedClimbSQL(`SELECT {:parentId}, {:parentId}`) + `,
		[[listed]] AS (
		SELECT ` + sharedPageChildColumnsSQL(
		`[[page]].[[id]] IN (SELECT [[id]] FROM [[shared_base]])
				OR (`+bootstrapSharedReachedSQL("shared_up", "shared_base", "{:parentId}")+` AND `+bootstrapSharedStepSQL("[[page]]")+`)`) + `
		FROM {{leafen_pages}} [[page]] INDEXED BY [[` + pagePositionIndex + `]]
		CROSS JOIN {{leafen_workspaces}} [[workspace]]
		WHERE [[page]].[[workspace]] = {:workspaceId} AND [[page]].[[parent_id]] = {:parentId}` + past + `
			AND ` + sharedPageChildConditionsSQL + `
			AND [[in_shared_walk]]
		ORDER BY [[page]].[[position]], [[page]].[[id]]
		LIMIT {:limit})`
}

// sharedPageChildrenTopSQL names, as listed, the pages at the workspace's
// top among those the walk reaches, and that past keeps: a page it reaches
// whose parent it doesn't reach is one it starts from, so these are the
// pages it starts from whose parent, climbed from, it doesn't reach.
func sharedPageChildrenTopSQL(past string) string {
	return bootstrapSharedBasesSQL + `,
		[[top_candidate]] ([[id]]) AS (
			SELECT [[id]] FROM [[shared_base]])` +
		bootstrapSharedClimbSQL(`SELECT DISTINCT [[candidate_page]].[[parent_id]], [[candidate_page]].[[parent_id]]
			FROM [[top_candidate]] JOIN {{leafen_pages}} [[candidate_page]] ON [[candidate_page]].[[id]] = [[top_candidate]].[[id]]
			WHERE [[candidate_page]].[[parent_id]] != ''`) + `,
		[[listed]] AS (
		SELECT ` + sharedPageChildColumnsSQL(
		`[[page]].[[id]] IN (SELECT [[id]] FROM [[shared_base]])`) + `
		FROM [[top_candidate]]
		CROSS JOIN {{leafen_pages}} [[page]]
		CROSS JOIN {{leafen_workspaces}} [[workspace]]
		WHERE [[page]].[[id]] = [[top_candidate]].[[id]] AND [[page]].[[workspace]] = {:workspaceId}` + past + `
			AND ` + sharedPageChildConditionsSQL + `
			AND NOT ` + bootstrapSharedReachedSQL("shared_up", "shared_base", "[[page]].[[parent_id]]") + `
		ORDER BY [[page]].[[position]], [[page]].[[id]]
		LIMIT {:limit})`
}
