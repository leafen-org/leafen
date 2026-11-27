package app

import (
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's Markdown mentions a data source as <mention-data-source
// url="collection://…"/>, or "v2"'s <mention url="collection://…"/>. Leafen
// keeps it the data source's mention, by the source's own ID
// (data_source_mentions.go), which reads back by the source's public API ID
// as it was written; before 2026-11-22 it became its database's. A data
// source that is not there, or that the writer may not read, is kept in
// Markdown by the ID written, as Notion's is
// (notion_markdown_kept_data_sources.go), and fails a block write's mention
// of it.

const notionMarkdownDataSourceAddress = "collection://"

// notionMarkdownDataSource is the data source ID a mention written as
// <mention-data-source> names, as written.
func notionMarkdownDataSource(span graphTextSpan) (string, bool) {
	if span.Mention != "database" {
		return "", false
	}
	return strings.CutPrefix(span.DatabaseID, notionMarkdownDataSourceAddress)
}

// dataSourceDatabase is the database a workspace's data source belongs to,
// a live one before one in the trash (notionMentionedDatabase), and the page
// holding it. A mention of a data source in the trash is its database's as
// any (seen 2026-11-13: Notion's keeps a data source mention of one in the
// trash, written before it went there or after;
// authorizeNotionDatabaseMentionTarget).
func dataSourceDatabase(app core.App, workspaceID, dataSourceID string) (string, string, bool, error) {
	found := []struct {
		ID      string `db:"id"`
		PageID  string `db:"page_id"`
		Trashed bool   `db:"trashed"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[container]].[[id]], [[container]].[[page_id]], EXISTS (
			SELECT 1 FROM {{leafen_database_container_trash}} AS [[trash]]
			WHERE [[trash]].[[container_id]] = [[container]].[[id]]
		) AS [[trashed]]
		FROM {{leafen_database_containers}} AS [[container]]
		JOIN {{leafen_database_container_sources}} AS [[membership]]
			ON [[membership]].[[container_id]] = [[container]].[[id]]
		-- The workspace is read after the containers' ids (+), or SQLite,
		-- which keeps no statistics, reads the workspace's every container.
		WHERE +[[container]].[[workspace_id]] = {:workspaceId} AND [[container]].[[kind]] = 'source'
			AND [[membership]].[[source_container_id]] = '' AND [[membership]].[[data_source_id]] = {:dataSourceId}
		UNION ALL
		SELECT [[deleted]].[[block_id]], [[deleted]].[[page_id]], 1 AS [[trashed]]
		FROM {{leafen_deleted_databases}} AS [[deleted]], json_each([[deleted]].[[data_source_ids]]) AS [[member]]
		WHERE [[deleted]].[[workspace_id]] = {:workspaceId} AND [[member]].[[value]] = {:dataSourceId}
		ORDER BY 3, 1
		LIMIT 1
	`).Bind(dbx.Params{"workspaceId": workspaceID, "dataSourceId": dataSourceID}).All(&found); err != nil {
		return "", "", false, err
	}
	if len(found) == 0 {
		return "", "", false, nil
	}
	return found[0].ID, found[0].PageID, true, nil
}

// dataSourceMention names a Public API connection's data source mention,
// which names the source by its Public API ID — or by its own, as a Notion
// move's boundary names one it made (notionMovedDataSource) — as the
// source's own mention in Markdown (dataSourceMentionHere); in JSON rich text
// as a mention of its database by the database's Public API ID, for the
// database mention's checks to follow.
func (preparation *notionMentionPreparation) dataSourceMention(span *graphTextSpan) error {
	publicID, named := notionMarkdownDataSource(*span)
	if !named {
		return nil
	}
	workspaceID := preparation.current.Workspace.WorkspaceID
	dataSourceID, err := resolveNotionDataSourceInternalID(preparation.app, workspaceID, publicID)
	if errors.Is(err, errInvalidNotionObjectIdentity) {
		dataSourceID, err = publicID, nil
	}
	if err != nil {
		return err
	}
	containerID, _, found, err := dataSourceDatabase(preparation.app, workspaceID, dataSourceID)
	if err != nil {
		return err
	}
	if found && preparation.markdown {
		// A source the writer may not read is kept as one that is not there.
		_, allowed, err := notionDatabaseMentionTitle(preparation.app, preparation.identity, preparation.current, containerID)
		if err != nil {
			return err
		}
		found = allowed
	}
	if !found {
		if preparation.markdown {
			return keepOrRefuseUnresolvedDataSource(span, publicID)
		}
		return invalidNotionRichTextMention()
	}
	if preparation.markdown {
		return dataSourceMentionHere(preparation.app, span, dataSourceID)
	}
	databaseID, err := ensureNotionObjectIdentity(preparation.app, notionObjectIdentityDatabase, workspaceID, containerID)
	if err != nil {
		return err
	}
	span.DatabaseID = databaseID
	return nil
}

// principalDataSourceMention names the data source mention of Markdown
// someone writes through the MCP tools, which names the source by its own ID
// or by the public API's, as the source's own mention showing its name
// (dataSourceMentionHere), when they may read its database's page.
func principalDataSourceMention(
	app core.App, principalID string, current pageAuthorization, span *graphTextSpan, allows func(pageAuthorization) bool,
) error {
	// A teamspace's or a group's stays as read (notionMarkdownTeamOrGroupMention).
	written, named := notionMarkdownDataSource(*span)
	if !named {
		return nil
	}
	dataSourceID, reached, err := principalDataSourceNamed(app, principalID, current, written, allows)
	if err != nil {
		return err
	}
	if !reached {
		// As Notion's Markdown keeps it, a source the writer can't reach
		// is kept by the ID written (notion_markdown_kept_data_sources.go).
		return keepOrRefuseUnresolvedDataSource(span, written)
	}
	return dataSourceMentionHere(app, span, dataSourceID)
}

// principalDataSourceNamed is the data source of current's workspace an ID
// written names, by its own ID or the public API's, and whether the writer
// reaches it: may read its database's page.
func principalDataSourceNamed(
	app core.App, principalID string, current pageAuthorization, written string, allows func(pageAuthorization) bool,
) (string, bool, error) {
	workspaceID, dataSourceID := current.Workspace.WorkspaceID, written
	_, pageID, found, err := dataSourceDatabase(app, workspaceID, dataSourceID)
	if err == nil && !found {
		// The public API's ID names the workspace's source too.
		internalID, resolveErr := resolveNotionDataSourceInternalID(app, workspaceID, strings.TrimSpace(written))
		switch {
		case resolveErr == nil && internalID != written:
			dataSourceID = internalID
			_, pageID, found, err = dataSourceDatabase(app, workspaceID, internalID)
		case resolveErr != nil && !errors.Is(resolveErr, errInvalidNotionObjectIdentity):
			err = resolveErr
		}
	}
	if err != nil || !found {
		return dataSourceID, false, err
	}
	_, allowed, err := authorizePageMentionTarget(
		app, principalID, current.Workspace.OwnerID, current.Workspace.WorkspaceID, pageID, allows,
	)
	return dataSourceID, allowed && err == nil, err
}

// principalDatabaseMention names the database mention of Markdown someone
// writes through the MCP tools as its page mentions are named
// (narrowedMarkdownMentionInput): one of the workspace's databases they may
// read, by its own ID or by the public API's, by its name now; one they may
// not, or in the trash, linked to it all the same with its name, which a
// reader who may not open it reads unnamed; one nothing in the workspace
// answers to as the word "Untitled". A page mention naming a database, as a
// page's Markdown gives a database its reader can't open, is the database's.
func principalDatabaseMention(
	app core.App, principalID string, current pageAuthorization, span *graphTextSpan, allows func(pageAuthorization) bool,
) error {
	databaseID := span.DatabaseID
	switch span.Mention {
	case "database":
	case "page":
		// A database's address mentions it, as Notion's reads one of
		// type="page" (seen 2026-11-11): one in a page's is its ID without
		// dashes, which also names its block.
		if dashed := markdownMentionedDatabaseID(app, span.PageID); dashed != "" {
			databaseID = dashed
			break
		}
		// So does a full-page one's page's, written with no type or of
		// type="page", as the editor mentions it (its database), and the
		// page's one left as it turned inline.
		if containerID := mcpPageAddressDatabaseContainer(app, span.PageID); containerID != "" {
			databaseID = containerID
			break
		}
		if _, _, there := pageMentionTargetNow(app, span.PageID); there || span.PageID == "" {
			return nil
		}
		databaseID = span.PageID
	default:
		return nil
	}
	// The public API's ID names the workspace's database too.
	identity, err := resolveNotionObjectIdentity(app, notionObjectIdentityDatabase, databaseID)
	switch {
	case err == nil && identity.Workspace == current.Workspace.WorkspaceID:
		databaseID = identity.InternalID
	case err != nil && !errors.Is(err, errInvalidNotionObjectIdentity):
		return err
	}
	// A database by an address by Leafen's IDs: its ID without dashes
	// (databaseAppAddress), or a full-page one's page's ID, as the app's
	// address bar names it.
	if span.Mention == "database" {
		if containerID := mcpPageAddressDatabaseContainer(app, databaseID); containerID != "" {
			databaseID = containerID
		} else if dashed := markdownMentionedDatabaseID(app, databaseID); dashed != "" {
			databaseID = dashed
		}
	}
	database, found, err := loadNotionMentionedDatabase(app, databaseID)
	if err != nil {
		return err
	}
	if !found || database.WorkspaceID != current.Workspace.WorkspaceID {
		// A mention of nothing, or of a page by a database's tag, is the
		// page rule's to name, as Notion's Markdown reads an address by its
		// target (pageDatabaseMention).
		if span.Mention == "database" {
			span.Mention, span.PageID, span.DatabaseID = "page", databaseID, ""
		}
		return nil
	}
	allowed := false
	if !database.Trashed {
		if _, allowed, err = authorizePageMentionTarget(
			app, principalID, current.Workspace.OwnerID, current.Workspace.WorkspaceID, database.PageID, allows,
		); err != nil {
			return err
		}
	}
	title, _, err := databaseContainerTitle(app, databaseID)
	if err != nil {
		return err
	}
	// A database with no name is named as the editor names one.
	if strings.TrimSpace(title) == "" {
		title = notionUnresolvedMentionTitle
		if allowed {
			title = mcpDatabaseShownTitle(app, principalID, "")
		}
	}
	span.Mention, span.PageID, span.DatabaseID, span.Text = "database", "", databaseID, title
	return nil
}

// markdownMentionedDatabaseID is the database an ID without its dashes
// names, "" for none.
func markdownMentionedDatabaseID(app core.App, id string) string {
	dashed, uuid := normalizeNotionUUIDRequest(id)
	if !uuid || dashed == id {
		return ""
	}
	if _, found, err := loadNotionMentionedDatabase(app, dashed); err != nil || !found {
		return ""
	}
	return dashed
}
