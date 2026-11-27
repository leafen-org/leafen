package app

import (
	"context"

	"github.com/pocketbase/pocketbase/core"
)

// readerBootstrapPagesWindow is how many pages each read takes.
const readerBootstrapPagesWindow = 500

// readReaderBootstrapPages reads every page the app's start-up read gives a
// reader, in each workspace they're in — their own and their teamspaces'
// pages, then those shared with them — by the same reads GET /bootstrap
// takes a window at a time (bootstrap.go), with which pages are shared with
// them and which of those everyone in the workspace opens. It is for a reader
// without the screen, who takes the whole list at once: an agent's sidebar
// sections (sidebar_section_pages.go) and the trash's lines
// (page_trash_list.go).
func readReaderBootstrapPages(ctx context.Context, app core.App, principalID string) (bootstrapResponse, error) {
	response := bootstrapResponse{}
	cursor := bootstrapCursor{PrincipalID: principalID, WorkspacesDone: true}
	err := runReadSnapshot(ctx, app, func(txApp core.App) error {
		for !cursor.PagesDone {
			rows, workspaceCreated, done, err := loadBootstrapPages(txApp, principalID, cursor, readerBootstrapPagesWindow)
			if err != nil {
				return err
			}
			visible, err := withoutHiddenBootstrapPages(txApp, principalID, rows)
			if err != nil {
				return err
			}
			if err := appendBootstrapPages(txApp, principalID, &response, visible); err != nil {
				return err
			}
			shared, err := sharedPrivatePageIDs(txApp, visible)
			if err != nil {
				return err
			}
			response.SharedPageIDs = append(response.SharedPageIDs, shared...)
			cursor.PagesDone = done || len(rows) == 0
			if len(rows) > 0 {
				last := rows[len(rows)-1]
				cursor.PageWorkspaceCreated, cursor.PageWorkspace = workspaceCreated[last.WorkspaceID], last.WorkspaceID
				cursor.PageTeamspace, cursor.PageCreated, cursor.PageID = last.TeamspaceID, last.Created, last.ID
			}
		}
		for !cursor.SharedDone {
			rows, done, err := loadBootstrapSharedPages(txApp, principalID, cursor, readerBootstrapPagesWindow)
			if err != nil {
				return err
			}
			if err := appendBootstrapPages(txApp, principalID, &response, rows); err != nil {
				return err
			}
			// A page the person keeps privately is theirs, not shared with them.
			owned, err := privatelyOwnedPageIDs(txApp, principalID, rows)
			if err != nil {
				return err
			}
			wide, err := workspaceWidePageIDs(txApp, principalID, rows)
			if err != nil {
				return err
			}
			for _, row := range rows {
				if !owned[row.ID] {
					response.SharedPageIDs = append(response.SharedPageIDs, row.ID)
					if wide[row.ID] {
						response.WorkspaceWidePageIDs = append(response.WorkspaceWidePageIDs, row.ID)
					}
				}
			}
			cursor.SharedDone = done || len(rows) == 0
			if len(rows) > 0 {
				cursor.SharedCreated, cursor.SharedPageID = rows[len(rows)-1].Created, rows[len(rows)-1].ID
			}
		}
		return nil
	})
	return response, err
}
