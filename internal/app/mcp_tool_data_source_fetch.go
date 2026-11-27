package app

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/pocketbase/core"
)

// A data source's fetch reads it through the page holding it, in a
// workspace the connection may read, and answers with its configurable state
// and SQLite table (mcp_data_source_text.go), and its workspace's id, as a
// page's fetch gives its own (2026-11-16: an agent working in two workspaces
// of the same name couldn't tell which held a data source).

func fetchMCPDataSource(
	ctx context.Context,
	app core.App,
	dataSourceID string,
) (*mcp.CallToolResult, notionMCPFetchOutput, error) {
	read, err := readMCPDataSource(ctx, app, dataSourceID)
	if err != nil {
		return nil, notionMCPFetchOutput{}, notionImportFillingRefusal(ctx, app, err, dataSourceID)
	}
	return nil, read.Fetch, nil
}

// mcpReadDataSource is a data source the connection may read: its place,
// properties and views, and its fetch answer.
type mcpReadDataSource struct {
	Location   mcpDataSourceLocation
	Properties []notionMCPDataSourceProperty
	Views      []notionMCPDataSourceView
	Fetch      notionMCPFetchOutput
}

func readMCPDataSource(ctx context.Context, app core.App, dataSourceID string) (mcpReadDataSource, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return mcpReadDataSource{}, err
	}
	read := mcpReadDataSource{}
	err = runNotionReadSnapshot(ctx, app, func(txApp core.App) error {
		location, err := loadMCPDataSourceLocationToRead(txApp, dataSourceID)
		if err != nil {
			return err
		}
		page, err := authorizePage(txApp, authority.PrincipalID, location.PageID, permissionWorkspaceRead)
		if err != nil || page.Workspace.WorkspaceID != location.WorkspaceID ||
			page.Workspace.OwnerID != location.OwnerID {
			return mcpRefusedUnlessStorage(errMCPCantRead, err)
		}
		if !mcpAuthorityReachesPage(txApp, authority, page.Page) {
			return errMCPOutsideTokenPages
		}
		workspace, err := selectMCPWorkspace(txApp, authority, mcpWorkspaceInput{
			WorkspaceID: location.WorkspaceID,
		}, permissionWorkspaceRead)
		if err != nil || workspace.WorkspaceID != location.WorkspaceID {
			return mcpRefusedUnlessStorage(errMCPOutsideConnection, err)
		}
		properties, views, err := loadMCPDataSourceSchema(txApp, location, authority.PrincipalID)
		if err != nil {
			return err
		}
		templates, err := loadMCPDataSourceTemplates(txApp, location)
		if err != nil {
			return err
		}
		// Its name as the agent reads it (page_name_mentions.go).
		location.Title = mcpSourceNameAsRead(txApp, authority, location.ID, location.Title)
		text, err := mcpDataSourceElement(txApp, authority, location, properties, templates)
		if err != nil {
			return err
		}
		// One in the trash says so (mcp_database_trash_rule.go).
		if text, err = mcpDataSourceTextInTrash(txApp, location, text); err != nil {
			return err
		}
		// A data source's URL is its database's, as Notion's fetch gives it.
		url := mcpDataSourceURL(txApp, dataSourceID)
		if containerID, err := loadDatabaseSQLSourceContainerID(txApp, location); err == nil {
			url = mcpDatabaseAddress(authority, containerID)
		}
		read = mcpReadDataSource{
			Location: location, Properties: properties, Views: views,
			Fetch: notionMCPFetchOutput{
				Metadata: notionMCPFetchMetadata{Type: "data_source"}, Title: mcpDataSourceShownTitle(txApp, authority.PrincipalID, mcpDataSourceAsShown(txApp, authority, location)), URL: url, Text: text,
				WorkspaceID: location.WorkspaceID,
			},
		}
		return nil
	})
	if err != nil {
		return mcpReadDataSource{}, mcpDataSourceQueryError(err)
	}
	return read, nil
}
