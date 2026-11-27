package app

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The tools read a database in the trash by one rule (2026-11-24: an agent
// picked the trashed one of two databases of one name from leafen_catalog,
// which listed it with no mark while notion-fetch said deleted, a write said
// not found and SQL read it): a data source is in the trash when it is
// itself — kept in its database's trash where it stands, or with its
// database deleted — or when the database it is in is, as notion-fetch marks
// that database deleted (mcpDatabaseDeleted): in the trash itself, or on a
// page in the trash or under one. A list leaves it out, as notion-search
// does and as Notion's search does; a read that names it says so: notion-fetch
// marks its database `deleted` and says it of a data source, and a query
// reads its rows as they stood, as Notion's MCP does (mcp_fetch_trash.go),
// with a warning.

// mcpDataSourceInTrash is whether the tools read a data source as in the
// trash.
func mcpDataSourceInTrash(app core.App, location mcpDataSourceLocation) (bool, error) {
	itself := false
	if err := app.DB().NewQuery(`SELECT NOT ` + databaseSourceNotTrashedSQL("{:id}") + ` OR ` + databaseSourceDeletedSQL("{:id}")).
		Bind(dbx.Params{"id": location.ID}).Row(&itself); err != nil || itself {
		return itself, err
	}
	containerID, err := loadDatabaseSQLSourceContainerID(app, location)
	if errors.Is(err, errMCPScopeDenied) {
		// No database shows it here: the page it is on says.
		return pageInTrashPath(app, location.PageID)
	}
	if err != nil {
		return false, err
	}
	return mcpDatabaseDeleted(app, containerID, location.PageID)
}

// mcpQueriedSourceContainerID is the database a query reads a data source
// in: its own (loadDatabaseSQLSourceContainerID), or the deleted database
// the trash keeps it in, whose block left its page — read as any in the
// trash (2026-11-24: rows mode read such a source, SQL said it was gone).
func mcpQueriedSourceContainerID(app core.App, location mcpDataSourceLocation) (string, error) {
	containerID, err := loadDatabaseSQLSourceContainerID(app, location)
	if !errors.Is(err, errMCPScopeDenied) {
		return containerID, err
	}
	deleted := ""
	readErr := app.DB().NewQuery(`
		SELECT [[deleted]].[[block_id]]
		FROM {{leafen_deleted_databases}} AS [[deleted]], json_each([[deleted]].[[data_source_ids]]) AS [[member]]
		WHERE [[member]].[[value]] = {:id} AND [[deleted]].[[owner]] = {:owner}
			AND [[deleted]].[[workspace_id]] = {:workspaceId} AND [[deleted]].[[page_id]] = {:pageId}
		ORDER BY [[deleted]].[[block_id]] LIMIT 1
	`).Bind(dbx.Params{
		"id": location.ID, "owner": location.OwnerID, "workspaceId": location.WorkspaceID, "pageId": location.PageID,
	}).Row(&deleted)
	if errors.Is(readErr, sql.ErrNoRows) {
		return "", err
	}
	return deleted, readErr
}

// mcpDataSourceTrashSaid is what a read that names a data source in the
// trash says of it.
const mcpDataSourceTrashSaid = " is in the trash, itself or with its database or the page holding it: " +
	"its rows read as they stood, it changes only once restored (notion-restore-pages on its database, or the trash in Leafen), " +
	"and leafen_catalog and notion-search leave it out."

// withMCPQueryNotices adds to a query's answer what notices its data
// sources have: one still being imported (withNotionImportFilling), and one
// in the trash.
func withMCPQueryNotices(app core.App, output notionMCPQueryDataSourcesOutput, dataSourceIDs ...string) notionMCPQueryDataSourcesOutput {
	output = withNotionImportFilling(app, output, dataSourceIDs...)
	for _, id := range dataSourceIDs {
		location, err := loadMCPDataSourceLocationToRead(app, id)
		if err != nil {
			continue
		}
		if trashed, err := mcpDataSourceInTrash(app, location); err == nil && trashed {
			output.Warnings = append(output.Warnings, mcpDataSourceURL(app, id)+mcpDataSourceTrashSaid)
		}
	}
	return output
}

// mcpDataSourceTextInTrash is a data source's fetched text saying, after its
// title, that it is in the trash, where it is.
func mcpDataSourceTextInTrash(app core.App, location mcpDataSourceLocation, text string) (string, error) {
	trashed, err := mcpDataSourceInTrash(app, location)
	if err != nil || !trashed {
		return text, err
	}
	line := "This data source" + mcpDataSourceTrashSaid + "\n"
	head, rest, found := strings.Cut(text, "\n")
	if !found {
		return text, nil
	}
	if title, after, found := strings.Cut(rest, "\n"); found && strings.HasPrefix(title, "Data source title:") {
		return head + "\n" + title + "\n" + line + after, nil
	}
	return head + "\n" + line + rest, nil
}
