package app

import (
	"context"
	"database/sql"
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// Notion's MCP reads what is in the trash and what is under it, as a reader
// who may read it (seen 2026-11-11, synthetic pages, databases and data
// sources sent to the trash through the API):
//
//   - a page in the trash itself fetches as `<page … deleted>` with an empty
//     <ancestor-path> and no content (<blank-page>), its path still given;
//     a page under one in the trash fetches as any, with an empty
//     <ancestor-path> too;
//   - a database page is `deleted` while it or its database is in the trash
//     — its data source kept in the trash where it stands, its database in
//     the trash, or a page above it — and its ancestors are named as any's;
//     only one in the trash itself loses its content;
//   - a database is `deleted` likewise: in the trash itself, its data
//     sources all kept in the trash where they stand, or under a page in it;
//   - a data source in the trash fetches as any, and rows, sql and a view's
//     query read its rows as any's, the rows in the trash themselves left
//     out; fetch finds no view of one in the trash (its own or its
//     database's), though it does under a page in the trash.
//
// The app's trash rules hold for every change: nothing here writes.

// mcpPageTrashMarks is how a page's fetch shows where the page stands toward
// the trash.
type mcpPageTrashMarks struct {
	// Deleted marks its <page> `deleted`.
	Deleted bool
	// Blank leaves its content out, as a page with none.
	Blank bool
	// NoAncestors empties its <ancestor-path>; its path is still given.
	NoAncestors bool
}

// mcpPageTrash reads where a page stands toward the trash, as fetch shows
// it (notionPageTrashPlace): a database page is under the trash with its
// data source or the page holding its database.
func mcpPageTrash(app core.App, page *core.Record) (mcpPageTrashMarks, error) {
	place, err := notionPageTrashPlace(app, page.Id)
	if err != nil {
		return mcpPageTrashMarks{}, err
	}
	itself := place == notionInTrashItself
	if isDatabaseRowPageRecord(page) {
		return mcpPageTrashMarks{Deleted: place.inTrash(), Blank: itself}, nil
	}
	return mcpPageTrashMarks{Deleted: itself, Blank: itself, NoAncestors: place.inTrash()}, nil
}

// mcpFetchedPageBlocks lists a fetched page's blocks as its content reads:
// none for a page in the trash itself, whose content fetch leaves out.
func mcpFetchedPageBlocks(ctx context.Context, app core.App, pageID, cursor string) ([]notionMCPFetchedBlock, string, error) {
	if page, err := app.FindRecordById(pagesCollection, pageID); err == nil {
		marks, err := mcpPageTrash(app, page)
		if err != nil {
			return nil, "", err
		}
		if marks.Blank {
			return nil, "", nil
		}
	}
	return mcpPageBlockList(ctx, app, pageID, cursor)
}

// mcpDatabaseDeleted is whether a database's fetch marks it deleted: in the
// trash itself, its sources all kept in the trash where they stand, or on a
// page in the trash or under one.
func mcpDatabaseDeleted(app core.App, containerID, holderPageID string) (bool, error) {
	if trashed, err := databaseContainerIsTrashed(app, containerID); err != nil || trashed {
		return trashed, err
	}
	if kept, err := databaseKeepsOnlySourcesTrashedInPlace(app, containerID); err != nil || kept {
		return kept, err
	}
	return pageInTrashPath(app, holderPageID)
}

// authorizeMCPRowInTrashedSource opens a database page whose data source is
// in the trash, which the app reaches through no page, for reading through
// the page holding its database (authorizeRowPageInTrashedSource); found is
// false for any other page.
func authorizeMCPRowInTrashedSource(
	app core.App, principalID, pageID string,
) (pageAuthorization, bool, error) {
	row, err := findRecordRemembered(app, pagesCollection, pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return pageAuthorization{}, false, nil
	}
	if err != nil {
		return pageAuthorization{}, false, err
	}
	trashed, found, err := authorizeRowPageInTrashedSource(app, principalID, row, permissionWorkspaceRead)
	if !found || err != nil {
		return pageAuthorization{}, found, err
	}
	return trashed.Row, true, nil
}

// mcpRowPageAbove is the page above a database page, its data source in the
// trash or not, which fetch names its ancestors from.
func mcpRowPageAbove(app core.App, row *core.Record) (string, error) {
	above, err := databaseRowPageAbove(app, row)
	if !isPermissionDenied(err) {
		return above, err
	}
	authority, readErr := readDatabaseRowPageAuthorityWhere(app, row.Id, "TRUE")
	if readErr != nil || authority.ContainerPageID == "" {
		return "", err
	}
	return authority.ContainerPageID, nil
}
