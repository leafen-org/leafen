package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func notionInlineDatabaseTitle(
	app core.App,
	authorization notionBlockAuthorization,
	block storedNotionBlock,
) (string, bool, error) {
	return notionInlineDatabaseTitleWith(app, authorization, block, false)
}

// notionInlineDatabaseTitleWith with trashed also names a database in the
// trash, for a read of its block there (notion_trash_reads.go).
func notionInlineDatabaseTitleWith(
	app core.App,
	authorization notionBlockAuthorization,
	block storedNotionBlock,
	trashed bool,
) (string, bool, error) {
	rows := []struct {
		Title        string `db:"title"`
		SourcePageID string `db:"source_page_id"`
		Kind         string `db:"kind"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[source]].[[title]], [[source_container]].[[page_id]] AS [[source_page_id]], [[container]].[[kind]]
		FROM {{leafen_database_containers}} AS [[container]]
		JOIN {{leafen_database_container_sources}} AS [[membership]]
			ON [[membership]].[[container_id]] = [[container]].[[id]]
			AND [[membership]].[[owner]] = [[container]].[[owner]]
			AND [[membership]].[[workspace_id]] = [[container]].[[workspace_id]]
			AND [[membership]].[[page_id]] = [[container]].[[page_id]]
		JOIN {{leafen_data_sources}} AS [[source]]
			ON [[source]].[[id]] = [[membership]].[[data_source_id]]
			AND [[source]].[[owner]] = [[container]].[[owner]]
			AND [[source]].[[workspace_id]] = [[container]].[[workspace_id]]
		JOIN {{leafen_database_containers}} AS [[source_container]]
			ON [[source_container]].[[id]] = CASE
				WHEN [[container]].[[kind]] = 'source' THEN [[container]].[[id]]
				ELSE [[container]].[[source_container_id]] END
			AND [[source_container]].[[owner]] = [[container]].[[owner]]
			AND [[source_container]].[[workspace_id]] = [[container]].[[workspace_id]]
			AND [[source_container]].[[kind]] = 'source'
			AND [[source_container]].[[page_id]] = [[source]].[[page_id]]
		WHERE [[container]].[[id]] = {:blockId}
			AND [[container]].[[block_id]] = {:blockId}
			AND [[container]].[[owner]] = {:owner}
			AND [[container]].[[workspace_id]] = {:workspaceId}
			AND [[container]].[[page_id]] = {:pageId}
			AND [[container]].[[kind]] IN ('source','linked')
			AND ({:trashed} OR NOT EXISTS (
				SELECT 1 FROM {{leafen_database_container_trash}} AS [[container_trash]]
				WHERE [[container_trash]].[[container_id]] = [[container]].[[id]]
			))
			AND ({:trashed} OR NOT EXISTS (
				SELECT 1 FROM {{leafen_database_container_trash}} AS [[source_trash]]
				WHERE [[source_trash]].[[container_id]] = [[source_container]].[[id]]
			))
		ORDER BY [[membership]].[[position]], [[source]].[[id]] LIMIT 1
	`).Bind(map[string]any{
		"blockId": block.ID, "owner": authorization.Page.Workspace.OwnerID,
		"workspaceId": authorization.Page.Workspace.WorkspaceID, "pageId": block.PageID, "trashed": trashed,
	}).All(&rows)
	if err != nil || len(rows) == 0 {
		return "", false, err
	}
	if rows[0].SourcePageID != block.PageID {
		sourcePage, err := authorizePageBody(
			app, authorization.Identity.PrincipalID,
			rows[0].SourcePageID, permissionWorkspaceRead,
		)
		if err != nil || sourcePage.Workspace.OwnerID != authorization.Page.Workspace.OwnerID ||
			sourcePage.Workspace.WorkspaceID != authorization.Page.Workspace.WorkspaceID ||
			sourcePage.Page.GetBool("in_trash") ||
			!notionOAuthIdentityAllowsPage(app, authorization.Identity, sourcePage) {
			return "", false, nil
		}
	}
	// A linked database is named by its own name, as the database object is.
	if rows[0].Kind == "linked" {
		return linkedDatabaseNameOf(block.Content).Title, true, nil
	}
	// One of its own sources, inline, by the one rule its database object's
	// title follows (notionDatabaseBlockTitle): of one source by the first
	// that names it, of two or more by its name of its own or its sources'
	// names listed.
	if authorization.Page.Page == nil || authorization.Page.Page.GetString("kind") != pageKindDatabase {
		if title, found, err := notionDatabaseBlockTitle(app, block.ID, notionNamesReadBy(app, authorization.Identity)); err != nil || found {
			return title, found, err
		}
	}
	return notionDatabaseShownTitle(rows[0].Title), true, nil
}

// inlineDatabaseOwnName is an inline database's name kept on its block as
// it takes a second source: its one source's name, with its formatting, or
// its words as one plain piece.
func inlineDatabaseOwnName(app core.App, sourceID string) []graphTextSpan {
	stored := struct {
		Title string `db:"title"`
		Spans string `db:"title_spans"`
	}{}
	if app.DB().NewQuery(`SELECT [[title]], [[title_spans]] FROM {{leafen_data_sources}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": sourceID}).One(&stored) != nil || stored.Title == "" {
		return nil
	}
	if spans := decodeStoredTextSpans(stored.Spans); len(spans) > 0 && spelledName(spans) == stored.Title {
		return spans
	}
	return []graphTextSpan{{Text: stored.Title}}
}
