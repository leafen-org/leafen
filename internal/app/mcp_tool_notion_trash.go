package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's trash tools (seen 2026-11-13): pages and database pages go to the
// trash and come back, a database goes with all its rows, as the page's and
// the row's menus and a database's block put them there (trashMCPPage,
// changeDatabaseRowTrash, the deleted databases of deleted_databases.go).
// Without user_confirmed nothing moves: the refusal lists what would, as
// Notion's does, to show the person first.

const mcpTrashMaximumItems = 20

// mcpTrashMovedNote is what a move to the trash answers beside what moved.
const mcpTrashMovedNote = "Moved to the trash; notion-restore-pages, or the trash in Leafen, brings them back."

type notionMCPDeletePagesInput struct {
	PageIDs       []string `json:"page_ids" jsonschema:"IDs of the pages to move to the trash, 1 to 20; database pages are pages. A repeated ID counts once"`
	UserConfirmed bool     `json:"user_confirmed,omitempty" jsonschema:"true only after the person asked to delete these exact pages, or confirmed the list a call without it gave. Without it nothing moves to the trash and the refusal lists what would"`
}

type notionMCPRestorePagesInput struct {
	PageIDs []string `json:"page_ids" jsonschema:"IDs of 1 to 20 pages or databases in the trash; database pages are pages. A repeated ID is restored once"`
}

type notionMCPDeleteDatabasesInput struct {
	DatabaseIDs   []string `json:"database_ids" jsonschema:"IDs of the databases to move to the trash, 1 to 20: full-page or inline databases that own their data, not linked views. A repeated ID counts once"`
	UserConfirmed bool     `json:"user_confirmed,omitempty" jsonschema:"true only after the person asked to delete these exact databases, or confirmed the list a call without it gave. Without it nothing moves to the trash and the refusal lists what would"`
}

type notionMCPTrashResult struct {
	ID    string `json:"id"`
	URL   string `json:"url"`
	Type  string `json:"type,omitempty"`
	Title string `json:"title"`
}

type notionMCPTrashOutput struct {
	Results []notionMCPTrashResult `json:"results"`
	Note    string                 `json:"note,omitempty"`
}

// mcpTrashItem is a page or database a trash tool was given, found and
// checked for the connection's person.
type mcpTrashItem struct {
	ID, Title, WorkspaceID string
	InTrash                bool
	// Database is a database's location; PageID its full-page database's page.
	Database *mcpDatabaseContainerLocation
	PageID   string
	Rows     int
}

func registerMCPNotionTrashTools(server *mcp.Server, app core.App) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-delete-pages", Title: "Move Leafen pages to the trash",
		Description: "Move pages to the trash, with their sub-pages and databases; notion-restore-pages or the trash in Leafen brings them back. A database's rows are pages and go here; a database goes with notion-delete-databases, and content inside a page with notion-update-page. Use it only when the person asks to delete these pages, never on your own to clean up. Without user_confirmed: true nothing moves and the refusal lists what would: show it to the person and call again only when they confirm.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPointer(true), IdempotentHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input notionMCPDeletePagesInput) (*mcp.CallToolResult, notionMCPTrashOutput, error) {
		return trashMCPNotionItems(ctx, app, input.PageIDs, input.UserConfirmed, false)
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-restore-pages", Title: "Restore Leafen pages from the trash",
		Description: "Bring pages or databases back from the trash to where they were, as the trash's restore does. A database's rows are pages and come back here. One already out of the trash is answered and left as it is. A page whose parent is in the trash comes back with its parent: restore the parent. GET /workspaces/{workspaceId}/trash lists what the trash holds, by the IDs this takes.",
		Annotations: &mcp.ToolAnnotations{IdempotentHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input notionMCPRestorePagesInput) (*mcp.CallToolResult, notionMCPTrashOutput, error) {
		return restoreMCPNotionItems(ctx, app, input.PageIDs)
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-delete-databases", Title: "Move Leafen databases to the trash",
		Description: "Move databases to the trash with all their rows; notion-restore-pages or the trash in Leafen brings them back. Name a full-page or inline database that owns its data, not a linked view; a page or database page goes with notion-delete-pages. Use it only when the person asks to delete these databases, never on your own to clean up. Without user_confirmed: true nothing moves and the refusal lists what would: show it to the person and call again only when they confirm.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: boolPointer(true), IdempotentHint: true},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input notionMCPDeleteDatabasesInput) (*mcp.CallToolResult, notionMCPTrashOutput, error) {
		if result, output, err := trashMCPNotionItems(ctx, app, input.DatabaseIDs, input.UserConfirmed, true); err != nil || !input.UserConfirmed {
			return result, output, err
		}
		return trashMCPNotionDatabases(ctx, app, input.DatabaseIDs)
	})
}

// mcpTrashIDs are the IDs a trash tool was given, once each, as the tools
// name them.
func mcpTrashIDs(app core.App, raw []string, field string) ([]string, error) {
	ids := []string{}
	for _, id := range raw {
		if local := mcpLocalID(app, strings.TrimSpace(id)); local != "" && !slices.Contains(ids, local) {
			ids = append(ids, local)
		}
	}
	if len(ids) == 0 || len(ids) > mcpTrashMaximumItems {
		return nil, mcpInvalid("%s names 1 to %d IDs", field, mcpTrashMaximumItems)
	}
	return ids, nil
}

// trashMCPNotionItems checks what a delete tool was given and, confirmed,
// moves the pages to the trash; databases are moved by
// trashMCPNotionDatabases once checked here.
func trashMCPNotionItems(ctx context.Context, app core.App, raw []string, confirmed, databases bool) (*mcp.CallToolResult, notionMCPTrashOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPTrashOutput{}, err
	}
	field, kind := "page_ids", "page"
	if databases {
		field, kind = "database_ids", "database"
	}
	ids, err := mcpTrashIDs(app, raw, field)
	if err != nil {
		return nil, notionMCPTrashOutput{}, err
	}
	items := make([]mcpTrashItem, 0, len(ids))
	for _, id := range ids {
		item, err := mcpFindTrashItem(app, authority, id, databases, false)
		if err != nil {
			return nil, notionMCPTrashOutput{}, err
		}
		items = append(items, item)
	}
	if !confirmed {
		lines := []string{}
		for _, item := range items {
			// An untitled page as fetch calls it, as its person reads it; the answer's title stays "", as Notion's.
			name := mcpNamedOrCalled(item.Title, func() string {
				page, _ := app.FindRecordById(pagesCollection, item.ID)
				return mcpUntitledPageName(app, authority.PrincipalID, page)
			})
			line := fmt.Sprintf("- %s: %q (id: %s)", kind, name, item.ID)
			if databases {
				line += fmt.Sprintf(", %d row(s)", item.Rows)
			}
			lines = append(lines, line)
		}
		what := "with their sub-pages and databases"
		if databases {
			what = "with all their rows"
		}
		return nil, notionMCPTrashOutput{}, mcpConfirmationNeeded("Not moved yet: with user_confirmed: true, this call moves %d %s(s) to the trash, %s:\n%s\n\n"+
			mcpConfirmFirst, len(items), kind, what, strings.Join(lines, "\n"))
	}
	if databases {
		return nil, notionMCPTrashOutput{}, nil
	}
	output := notionMCPTrashOutput{Note: mcpTrashMovedNote}
	for _, item := range items {
		if !item.InTrash {
			if _, _, err := trashMCPPage(ctx, app, "", item.ID, true, "", nil); err != nil {
				return nil, notionMCPTrashOutput{}, err
			}
		}
		output.Results = append(output.Results, notionMCPTrashResult{ID: item.ID, URL: mcpPageAddress(app, authority, item.ID), Title: item.Title})
	}
	return nil, output, nil
}

// trashMCPNotionDatabases moves checked databases to the trash: a full-page
// database with its page, an inline one by taking its block off its page as
// the screen does, which keeps it in the trash with its rows.
func trashMCPNotionDatabases(ctx context.Context, app core.App, raw []string) (*mcp.CallToolResult, notionMCPTrashOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPTrashOutput{}, err
	}
	ids, err := mcpTrashIDs(app, raw, "database_ids")
	if err != nil {
		return nil, notionMCPTrashOutput{}, err
	}
	output := notionMCPTrashOutput{Note: mcpTrashMovedNote}
	for _, id := range ids {
		item, err := mcpFindTrashItem(app, authority, id, true, false)
		if err != nil {
			return nil, notionMCPTrashOutput{}, err
		}
		if item.PageID != "" {
			if _, _, err := trashMCPPage(ctx, app, "", item.PageID, true, "", nil); err != nil {
				return nil, notionMCPTrashOutput{}, err
			}
		} else {
			result, err := mcpNativeCall(ctx, "POST", "/pages/"+item.Database.PageID+"/content-graph/mutations", "", graphMutationInput{
				MutationID: "mcp-database-trash:" + newDiagnosticReference(),
				Operations: []graphOperation{{Kind: graphOperationKindDeleteBlock, Deletion: &graphBlockDelete{BlockID: item.Database.BlockID}}},
			})
			if err != nil {
				return nil, notionMCPTrashOutput{}, err
			}
			if err := mcpNativeRefusal(result); err != nil {
				return nil, notionMCPTrashOutput{}, err
			}
		}
		output.Results = append(output.Results, notionMCPTrashResult{
			ID: item.ID, URL: mcpDatabaseAddress(authority, item.ID), Title: item.Title,
		})
	}
	return nil, output, nil
}

// restoreMCPNotionItems brings pages and databases back from the trash.
func restoreMCPNotionItems(ctx context.Context, app core.App, raw []string) (*mcp.CallToolResult, notionMCPTrashOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPTrashOutput{}, err
	}
	ids, err := mcpTrashIDs(app, raw, "page_ids")
	if err != nil {
		return nil, notionMCPTrashOutput{}, err
	}
	output := notionMCPTrashOutput{Results: []notionMCPTrashResult{}}
	for _, id := range ids {
		// An inline database whose block left its page is in the trash as a
		// deleted database, its block's ID its own.
		if deleted, found, err := mcpFindDeletedDatabase(app, authority, id); err != nil {
			return nil, notionMCPTrashOutput{}, err
		} else if found {
			result, err := mcpNativeCall(ctx, "POST",
				"/workspaces/"+deleted.WorkspaceID+"/deleted-databases/"+deleted.BlockID+"/restore", "", map[string]any{})
			if err != nil {
				return nil, notionMCPTrashOutput{}, err
			}
			if err := mcpNativeRefusal(result); err != nil {
				return nil, notionMCPTrashOutput{}, err
			}
			output.Results = append(output.Results, notionMCPTrashResult{
				ID: deleted.BlockID, URL: mcpDatabaseAddress(authority, deleted.BlockID), Type: "database",
				Title: mcpDatabaseShownTitle(app, authority.PrincipalID, deleted.Title),
			})
			continue
		}
		item, err := mcpFindTrashItem(app, authority, id, mcpTrashNamesDatabase(app, id), true)
		if err != nil {
			return nil, notionMCPTrashOutput{}, err
		}
		result := notionMCPTrashResult{ID: item.ID, URL: mcpPageAddress(app, authority, item.ID), Type: "page", Title: item.Title}
		switch {
		case item.Database != nil:
			result.Type, result.URL = "database", mcpDatabaseAddress(authority, item.ID)
			if item.PageID != "" && item.InTrash {
				_, _, err = trashMCPPage(ctx, app, "", item.PageID, false, "", nil)
			} else if item.InTrash {
				err = restoreMCPInlineDatabase(ctx, item)
			}
		case item.InTrash:
			_, _, err = trashMCPPage(ctx, app, "", item.ID, false, "", nil)
		}
		if err != nil {
			if strings.Contains(err.Error(), "can't be put back where it was") {
				err = mcpInvalid("%s's parent is in the trash: restore the parent, which brings it back too", item.ID)
			}
			return nil, notionMCPTrashOutput{}, err
		}
		output.Results = append(output.Results, result)
	}
	return nil, output, nil
}

// mcpFindDeletedDatabase finds a deleted database by its ID, on a page the
// person may change and the connection reaches.
func mcpFindDeletedDatabase(app core.App, authority mcpAuthority, id string) (storedDeletedDatabase, bool, error) {
	blockID, ok := parseMCPDatabaseIdentifier(id)
	if !ok {
		return storedDeletedDatabase{}, false, nil
	}
	if dashed, ok := normalizeNotionUUIDRequest(blockID); ok {
		blockID = dashed
	}
	deleted := storedDeletedDatabase{}
	if err := app.DB().NewQuery(`
		SELECT [[block_id]], [[workspace_id]], [[page_id]], [[title]] FROM {{leafen_deleted_databases}} WHERE [[block_id]] = {:id} LIMIT 1
	`).Bind(dbx.Params{"id": blockID}).One(&deleted); errors.Is(err, sql.ErrNoRows) {
		return storedDeletedDatabase{}, false, nil
	} else if err != nil {
		return storedDeletedDatabase{}, false, failedBecause(errMCPToolFailed, err)
	}
	page, err := authorizePage(app, authority.PrincipalID, deleted.PageID, permissionWorkspaceWrite)
	if err != nil {
		return storedDeletedDatabase{}, false, mcpTrashAccessError(app, authority, deleted.PageID, err, mcpNotFound("database", id))
	}
	if !mcpAuthorityReachesPage(app, authority, page.Page) {
		return storedDeletedDatabase{}, false, errMCPOutsideTokenPages
	}
	if workspace, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{WorkspaceID: deleted.WorkspaceID},
		permissionWorkspaceWrite); err != nil || workspace.WorkspaceID != deleted.WorkspaceID {
		return storedDeletedDatabase{}, false, mcpRefusedUnlessStorage(errMCPOutsideConnection, err)
	}
	// By the name it went by on its page (deletedDatabaseNames), as Notion's
	// search keeps it (mcpDatabaseNameAsIndexed).
	names, err := deletedDatabaseNames(app, []string{deleted.BlockID})
	if err != nil {
		return storedDeletedDatabase{}, false, failedBecause(errMCPToolFailed, err)
	}
	if name, found := names[deleted.BlockID]; found {
		deleted.Title = mcpDatabaseNameAsIndexed(app, authority.PrincipalID, name)
	}
	return deleted, true, nil
}

// restoreMCPInlineDatabase puts an inline database back on its page, as the
// trash's restore does: by its block, or by its database where its block
// went with the page's text.
func restoreMCPInlineDatabase(ctx context.Context, item mcpTrashItem) error {
	var refusal error
	for _, blockID := range slices.Compact([]string{item.Database.BlockID, item.ID}) {
		result, err := mcpNativeCall(ctx, "POST",
			"/workspaces/"+item.WorkspaceID+"/deleted-databases/"+blockID+"/restore", "", map[string]any{})
		if err != nil {
			return err
		}
		if refusal = mcpNativeRefusal(result); refusal == nil {
			return nil
		}
	}
	return refusal
}

// mcpTrashNamesDatabase says whether an ID names a database, in the trash
// or out of it, rather than a page.
func mcpTrashNamesDatabase(app core.App, id string) bool {
	if _, found := resolveMCPDatabaseID(app, id); found {
		return true
	}
	databaseID, ok := parseMCPDatabaseIdentifier(id)
	return ok && mcpDatabaseContainerExistsToRead(app, databaseID)
}

// mcpFindTrashItem finds a page or database a trash tool was given, which
// the person may change and the connection reaches, as Notion's tools name
// one they can't find or that is of the other kind.
func mcpFindTrashItem(app core.App, authority mcpAuthority, id string, database, trashed bool) (mcpTrashItem, error) {
	kind := map[bool]string{false: "page", true: "database"}[database]
	missing := mcpNotFound(kind, id)
	if !database && mcpTrashNamesDatabase(app, id) {
		return mcpTrashItem{}, mcpInvalid("%s is a database, not a page: notion-delete-databases moves a database to the trash.", id)
	}
	item := mcpTrashItem{ID: id}
	pageID := id
	if database {
		containerID, found := resolveMCPDatabaseID(app, id)
		if !found {
			containerID, found = parseMCPDatabaseIdentifier(id)
			found = found && trashed && mcpDatabaseContainerExistsToRead(app, containerID)
		}
		if !found {
			if page, err := app.FindRecordById(pagesCollection, id); err == nil && page != nil {
				return mcpTrashItem{}, mcpInvalid("%s is a page, not a database: notion-delete-pages moves a page or database page to the trash.", id)
			}
			return mcpTrashItem{}, missing
		}
		location, err := loadMCPDatabaseContainerLocationWith(app, containerID, true)
		if err != nil {
			return mcpTrashItem{}, missing
		}
		if location.Kind != "source" {
			return mcpTrashItem{}, mcpInvalid("%s is a linked view of another database, not one that owns its data: take its block off the page with notion-update-page, or name the database it shows", id)
		}
		item.ID, item.Database, pageID = containerID, &location, location.PageID
		if mcpFullPageDatabaseContainer(app, location.PageID) == containerID {
			item.PageID = location.PageID
		} else {
			item.InTrash = !mcpDatabaseContainerExists(app, containerID)
		}
		// Its title as Notion's trash tools list it (mcpTitleAsIndexed), by
		// the name it goes by everywhere (mcpDatabaseNameAsIndexed): a
		// full-page one by its page's title, as fetch heads it.
		names, err := databaseContainerNames(app, []string{containerID})
		if err != nil {
			return mcpTrashItem{}, failedBecause(errMCPToolFailed, err)
		}
		item.Title = mcpDatabaseShownTitle(app, authority.PrincipalID, mcpDatabaseNameAsIndexed(app, authority.PrincipalID, names[containerID]))
		item.Rows = mcpTrashLiveRows(app, containerID)
	}
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil || page == nil {
		return mcpTrashItem{}, missing
	}
	if isDatabaseRowPageRecord(page) {
		row, err := authorizeDatabaseRowPageInTrash(app, authority.PrincipalID, page.Id, permissionWorkspaceWrite)
		if err != nil {
			return mcpTrashItem{}, mcpTrashAccessError(app, authority, page.Id, err, missing)
		}
		item.WorkspaceID, item.InTrash = row.WorkspaceID, row.TrashedAt != 0
	} else {
		current, err := authorizePage(app, authority.PrincipalID, page.Id, permissionWorkspaceWrite)
		if err != nil {
			return mcpTrashItem{}, mcpTrashAccessError(app, authority, page.Id, err, missing)
		}
		item.WorkspaceID = current.Workspace.WorkspaceID
		if item.Database == nil || item.PageID != "" {
			item.InTrash = page.GetBool("in_trash")
		}
	}
	if !mcpAuthorityReachesPage(app, authority, page) {
		return mcpTrashItem{}, errMCPOutsideTokenPages
	}
	if workspace, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{WorkspaceID: item.WorkspaceID},
		permissionWorkspaceWrite); err != nil || workspace.WorkspaceID != item.WorkspaceID {
		return mcpTrashItem{}, mcpRefusedUnlessStorage(errMCPOutsideConnection, err)
	}
	// Its title as Notion's trash tools list it (seen 2026-11-22: "w2322
	// 날짜 ‣ 와 ‣", mcpTitleAsIndexed).
	if item.Database == nil {
		item.Title = mcpTitleAsIndexed(app, page.Id, page.GetString("title"))
	}
	return item, nil
}

// mcpTrashAccessError is a trash tool's refusal of a page it may not
// change: one the account opens it can't change, and any other it found
// none of (mcp_named_target.go).
func mcpTrashAccessError(app core.App, authority mcpAuthority, pageID string, err, missing error) error {
	if isPermissionDenied(err) && mcpAccountOpens(app, authority, mcpPage(pageID)) {
		return errMCPCantChange
	}
	if isPermissionDenied(err) {
		return missing
	}
	if errors.Is(err, errDatabaseRowPageNotFound) || errors.Is(err, sql.ErrNoRows) {
		return missing
	}
	return failedBecause(errMCPToolFailed, err)
}

// mcpTrashLiveRows counts a database's rows out of the trash, which go to
// the trash with it; a source kept in the trash where it stands has its rows
// there already (database_source_trash_in_place.go).
func mcpTrashLiveRows(app core.App, containerID string) int {
	sources, err := loadMCPDatabaseContainerOwnMembers(app, containerID)
	if err != nil || len(sources) == 0 {
		return 0
	}
	total := 0
	for _, source := range sources {
		count := 0
		_ = app.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_database_rows}} AS [[row]]
			WHERE ` + databaseSourceRowsSQL("[[row]]", "{:source}") + ` AND NOT EXISTS (
				SELECT 1 FROM {{leafen_database_row_trash}} AS [[trash]] WHERE [[trash]].[[row_id]] = [[row]].[[id]]
			) AND NOT ` + databaseSourceTrashedInPlaceSQL("{:source}") + `
		`).Bind(dbx.Params{"source": source}).Row(&count)
		total += count
	}
	return total
}
