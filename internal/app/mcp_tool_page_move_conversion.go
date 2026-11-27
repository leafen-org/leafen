package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page moved into a database becomes one of its pages, and a database page
// moved under a page becomes a sub-page, keeping its ID; the database page
// loses its values. Each such move is its own change, made after every page
// in the request is admitted.

func mcpPageMovePersistence(app core.App) (*pocketBaseYgoPersistence, error) {
	persistence, ok := app.Store().Get(ygoPersistenceStoreKey).(*pocketBaseYgoPersistence)
	if !ok || persistence == nil {
		return nil, errMCPToolFailed
	}
	return persistence, nil
}

// moveMCPPagesIntoDataSource moves the pages into the data source's database.
func moveMCPPagesIntoDataSource(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	app core.App,
	authority mcpAuthority,
	rawDataSource string,
	rawPageIDs, written []string,
	dropValues bool,
) (*mcp.CallToolResult, notionMCPMovePagesOutput, error) {
	dataSourceID, ok := mcpLocalDataSourceID(app, rawDataSource)
	pageIDs, valid := normalizeMCPMovePageIDs(rawPageIDs, "")
	if !ok || !valid {
		return nil, notionMCPMovePagesOutput{}, mcpInvalid("a data_source_id as notion-fetch gives it (collection://…), and the pages to move in page_or_database_ids")
	}
	var containerPageID string
	err := app.DB().NewQuery(`
		SELECT [[page_id]] FROM {{leafen_data_sources}} WHERE [[id]] = {:id} AND ` + databaseSourceNotTrashedSQL("{:id}") + `
	`).Bind(dbx.Params{"id": dataSourceID}).Row(&containerPageID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notionMCPMovePagesOutput{}, mcpDenied("that data source is gone, or isn't one this connection reaches")
	}
	if err != nil {
		return nil, notionMCPMovePagesOutput{}, failedBecause(errMCPToolFailed, err)
	}
	container, err := authorizePageBody(app, authority.PrincipalID, containerPageID, permissionWorkspaceWrite)
	if err != nil {
		return nil, notionMCPMovePagesOutput{}, mcpPageMoveError(err, pageMetadataApplied, nil)
	}
	workspace, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{
		WorkspaceID: container.Workspace.WorkspaceID,
	}, permissionWorkspaceWrite)
	if err != nil || workspace.WorkspaceID != container.Workspace.WorkspaceID {
		return nil, notionMCPMovePagesOutput{}, mcpDenied("that data source is outside the workspaces this connection was given, or this account can't change it")
	}
	seed := mcpCallSeed()
	titles := make([]string, len(pageIDs))
	// A database's item moves as a row into another database
	// (mcp_tool_page_move_rows.go), each admitted before anything moves.
	rows := map[int]*mcpRowMove{}
	for index, pageID := range pageIDs {
		// A page or an item of another workspace is refused as such; an item
		// of this one moves as a row into another database
		// (mcp_tool_page_move_rows.go).
		if from := pageInOtherWorkspace(app, authority.PrincipalID, pageID, workspace.WorkspaceID); from != "" {
			return nil, notionMCPMovePagesOutput{}, mcpMovedAcross(app, written[index], pageID, from, workspace.WorkspaceID, rowDatabasePage(app, pageID) != "")
		}
		if rowDatabasePage(app, pageID) != "" {
			record, err := app.FindRecordById(pagesCollection, pageID)
			if err != nil {
				return nil, notionMCPMovePagesOutput{}, mcpDenied("each page to move is one this account may change, in the data source's workspace")
			}
			titles[index] = record.GetString("title")
			rows[index] = &mcpRowMove{index: index, rowID: pageID, title: titles[index],
				requestID: mcpPageMoveMutationID(authority.ConnectionID, seed, index)}
			if err := admitMCPRowMove(ctx, app, authority, rows[index], dataSourceID, dropValues); err != nil {
				return nil, notionMCPMovePagesOutput{}, err
			}
			continue
		}
		page, err := authorizePage(app, authority.PrincipalID, pageID, permissionWorkspaceWrite)
		if err != nil {
			return nil, notionMCPMovePagesOutput{}, mcpDenied("each page to move is one this account may change, in the data source's workspace")
		}
		titles[index] = page.Page.GetString("title")
	}
	persistence, err := mcpPageMovePersistence(app)
	if err != nil {
		return nil, notionMCPMovePagesOutput{}, err
	}
	batchHash := mcpPageMoveBatchHash(mcpDataSourceURLPrefix+dataSourceID, pageIDs)
	output := notionMCPMovePagesOutput{Moved: make([]notionMCPMovedPage, 0, len(pageIDs))}
	for index, pageID := range pageIDs {
		if row, isRow := rows[index]; isRow {
			moved, err := moveMCPRow(ctx, authority, *row, dataSourceID)
			if err != nil {
				return nil, notionMCPMovePagesOutput{}, err
			}
			output.Moved = append(output.Moved, moved)
			continue
		}
		receipt, err := persistence.movePageIntoDatabase(ctx, pageToDatabaseRowRequest{
			PrincipalID: authority.PrincipalID, PageID: pageID, DataSourceID: dataSourceID,
			ContainerPageID: containerPageID,
			MutationID:      mcpPageMoveMutationID(authority.ConnectionID, seed, index), RequestHash: batchHash,
		})
		if err != nil {
			return nil, notionMCPMovePagesOutput{}, mcpPageConversionError(err)
		}
		output.Moved = append(output.Moved, notionMCPMovedPage{
			ID: pageID, Title: titles[index], URL: mcpPageURL(authority, pageID),
			Parent:    notionMCPMovedPageParent{Type: "data_source_id", DataSourceID: dataSourceID},
			Duplicate: receipt.Duplicate,
		})
	}
	return nil, output, nil
}

// moveMCPDatabasePagesOut moves the listed database pages under the parent
// page, before the request's pages move together, and reports each by its
// place in the request.
func moveMCPDatabasePagesOut(
	ctx context.Context,
	app core.App,
	authority mcpAuthority,
	workspaceID, parentID, seed, batchHash string,
	pageIDs, written []string,
	leaveDatabase bool,
) (map[int]notionMCPMovedPage, error) {
	rows := map[int]databaseRowPageAuthority{}
	for index, pageID := range pageIDs {
		var dataSourceID string
		err := app.DB().NewQuery(`SELECT [[data_source_id]] FROM {{leafen_pages}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": pageID}).Row(&dataSourceID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, failedBecause(errMCPToolFailed, err)
		}
		if dataSourceID == "" {
			// A page the principal can't read fails the whole request here,
			// before any database page has moved.
			page, err := authorizePage(app, authority.PrincipalID, pageID, permissionWorkspaceRead)
			if err != nil {
				return nil, mcpDenied("each page to move is one this account can read")
			}
			if page.Workspace.WorkspaceID != workspaceID {
				return nil, mcpMovedAcross(app, written[index], pageID, page.Workspace.WorkspaceID, workspaceID, false)
			}
			continue
		}
		row, err := authorizeDatabaseRowPage(app, authority.PrincipalID, pageID, permissionWorkspaceWrite)
		if err != nil {
			return nil, mcpRowAuthorityRefusal(app, authority.PrincipalID, pageID, mcpDenied("each database item to move is one this account may change"))
		}
		if row.WorkspaceID != workspaceID {
			return nil, mcpMovedAcross(app, written[index], pageID, row.WorkspaceID, workspaceID, true)
		}
		rows[index] = row
	}
	moved := map[int]notionMCPMovedPage{}
	if len(rows) == 0 {
		return moved, nil
	}
	// A database's item leaves its database only when the call says so, before anything moves.
	if !leaveDatabase {
		return nil, mcpInvalid("the pages to move hold %d database item(s): under a page each leaves its database as a page of its own; "+
			"send leave_database true to do it, or new_parent data_source_id to move them to another database", len(rows))
	}
	persistence, err := mcpPageMovePersistence(app)
	if err != nil {
		return nil, err
	}
	for index, pageID := range pageIDs {
		row, isRow := rows[index]
		if !isRow {
			continue
		}
		title := databaseRowPageTitle(app, pageID)
		receipt, err := persistence.convertDatabaseRowToPage(ctx, databaseRowToPageRequest{
			PrincipalID: authority.PrincipalID, RowID: pageID, ContainerPageID: row.ContainerPageID, ParentID: parentID,
			MutationID: mcpPageMoveMutationID(authority.ConnectionID, seed, index), RequestHash: batchHash,
		})
		if err != nil {
			return nil, mcpPageConversionError(err)
		}
		// A wiki's page moved within its wiki stays its page and leaves nothing.
		warning, now := databaseRowLeftWarning(pageID, false), ""
		if err := app.DB().NewQuery(`SELECT [[data_source_id]] FROM {{leafen_pages}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": pageID}).Row(&now); err == nil && now == row.DataSourceID {
			warning = ""
		}
		moved[index] = notionMCPMovedPage{
			ID: pageID, Title: title, URL: mcpPageURL(authority, pageID),
			Parent:    notionMCPMovedPageParent{Type: "page_id", PageID: parentID},
			Duplicate: receipt.Duplicate, Warning: warning,
		}
	}
	return moved, nil
}

// mcpRowAuthorityRefusal is why a database's item this account can't reach
// as a live one can't move: in the Trash, which it can be restored from
// (rowInTrashRefused); otherwise refusal.
func mcpRowAuthorityRefusal(app core.App, principalID, rowID string, refusal error) error {
	if row, err := authorizeDatabaseRowPageInTrash(app, principalID, rowID, permissionWorkspaceWrite); err == nil && row.TrashedAt != 0 {
		return mcpPageConversionError(rowInTrashRefused())
	}
	return refusal
}

func mcpPageConversionError(err error) error {
	// A move refused for its reason says it, in the words Leafen's routes
	// give it (page_row_conversion_refusals.go, destination_refusals.go),
	// the fields it names by the tool's inputs (mcp_tool_field_names.go);
	// one for access this account lacks as such.
	var refusal *refusedForReason
	if errors.As(err, &refusal) && strings.TrimSpace(refusal.message) != "" {
		words := mcpMovePagesFieldNames.words(refusal.message)
		if refusal.status == http.StatusForbidden {
			return mcpDenied("can't move it there: %s", words)
		}
		return mcpInvalid("can't move it there: %s", words)
	}
	// A page published to the web and a parent in the trash are said as what
	// to do first; told "it is shared on its own, has a conflicting change, or
	// is being converted — fetch it and try again", an agent fetched and sent
	// the same move again.
	var requestError *graphRequestError
	switch {
	case isPermissionDenied(err), errors.Is(err, errDatabaseRowPageNotFound):
		return mcpDenied("this account can't change that page or item, or it is gone")
	case errors.Is(err, errPageRowConversionShared):
		return mcpInvalid("that page is published to the web: unpublish it before it goes into a database")
	case errors.As(err, &requestError) && requestError.code == errorCodePageInTrash:
		return mcpInvalid("the new parent is in the trash, or under a page that is: restore it first (notion-restore-pages), then move the page")
	// What is left is a change met meanwhile.
	case errors.Is(err, errDatabaseRowPageConflict), errors.Is(err, errPageRowConversionConflict):
		return mcpInvalid("that page or item, or where it goes, changed while this call ran or is being converted: fetch them and try again")
	default:
		return failedBecause(errMCPToolFailed, err)
	}
}
