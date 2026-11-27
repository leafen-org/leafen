package app

import (
	"github.com/pocketbase/pocketbase/core"
)

// Backlinks show only the pages the viewer can read, as Notion's do: a
// backlink from a page the viewer cannot read counts as a private page with
// nothing of it shown, and a page being deleted, duplicated or moved is left
// out until it settles. The path above a source shows only readable pages.

type pageBacklinkSourceAccess struct {
	readable map[string]bool
	private  map[string]bool
}

// loadPageBacklinkSourceAccess judges each page a window reads through.
func loadPageBacklinkSourceAccess(
	app core.App, principalID, workspaceID string, pageIDs []string,
) (pageBacklinkSourceAccess, error) {
	access := pageBacklinkSourceAccess{readable: map[string]bool{}, private: map[string]bool{}}
	for _, pageID := range pageIDs {
		page, err := authorizePageAccessRole(app, principalID, pageID, pageAccessRoleView)
		if isPermissionDenied(err) || err == nil && page.Workspace.WorkspaceID != workspaceID {
			access.private[pageID] = true
			continue
		}
		if err != nil {
			return pageBacklinkSourceAccess{}, err
		}
		if _, err := authorizePageSet(app, principalID, workspaceID, []string{pageID}, permissionWorkspaceRead); err != nil {
			if isPermissionDenied(err) {
				continue
			}
			return pageBacklinkSourceAccess{}, err
		}
		access.readable[pageID] = true
	}
	return access, nil
}

// split keeps the rows of readable sources and counts the private source
// pages among the rest.
func (access pageBacklinkSourceAccess) split(rows []storedPageBacklink) ([]storedPageBacklink, int) {
	kept := make([]storedPageBacklink, 0, len(rows))
	private := map[string]struct{}{}
	for _, row := range rows {
		switch {
		case access.readable[row.SourcePageID]:
			kept = append(kept, row)
		case access.private[row.SourcePageID]:
			private[row.SourcePageID] = struct{}{}
		}
	}
	return kept, len(private)
}

// readablePages keeps the pages of a backlink window's paths the viewer can
// read.
func (access pageBacklinkSourceAccess) readablePages(
	app core.App, principalID string, pages []pageResponse,
) ([]pageResponse, error) {
	kept := make([]pageResponse, 0, len(pages))
	for _, page := range pages {
		if !access.readable[page.ID] {
			_, err := authorizePageAccessRole(app, principalID, page.ID, pageAccessRoleView)
			if isPermissionDenied(err) {
				continue
			}
			if err != nil {
				return nil, err
			}
			access.readable[page.ID] = true
		}
		kept = append(kept, page)
	}
	return kept, nil
}
