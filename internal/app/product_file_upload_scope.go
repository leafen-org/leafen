package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A file goes up for a page as well as for its workspace (2026-10-17: a guest
// who may comment on a page couldn't attach a file to a comment, nor one who
// may edit it add an image, as an upload asked for a workspace writer, which
// Notion doesn't). An upload may name the page it is for and what for: the
// page's content, or a comment on it. Someone who isn't a workspace writer
// may send it when they may edit that page, or comment on it for a comment's
// file. The page and purpose stay with the upload, so each later step asks
// the same, and the person who sent it may read the file back while they can
// still open that page, before anything there shows it.

type productFileUploadScope struct {
	PageID  string
	Purpose string
}

func (input fileUploadPrepareInput) uploadScope() productFileUploadScope {
	return productFileUploadScope{PageID: input.PageID, Purpose: input.Purpose}
}

func (upload storedProductFileUpload) uploadScope() productFileUploadScope {
	return productFileUploadScope{PageID: upload.PageID, Purpose: upload.Purpose}
}

// normalizeProductFileUploadScope accepts no page, or a page with a known
// purpose (the page's content when none is named).
func normalizeProductFileUploadScope(input *fileUploadPrepareInput) bool {
	if input.PageID == "" {
		return input.Purpose == ""
	}
	if input.Purpose == "" {
		input.Purpose = fileUploadPurposePage
	}
	return len(input.PageID) <= maxWorkspacePermissionIdentifierLength &&
		(input.Purpose == fileUploadPurposePage || input.Purpose == fileUploadPurposeComment)
}

func authorizeProductFileUpload(
	app core.App,
	actorID, workspaceID string,
	scope productFileUploadScope,
) (workspaceAuthorization, error) {
	authorization, err := authorizeWorkspace(app, actorID, workspaceID, permissionWorkspaceWrite)
	if !isPermissionDenied(err) || scope.PageID == "" {
		return authorization, err
	}
	role := pageAccessRoleEdit
	if scope.Purpose == fileUploadPurposeComment {
		role = pageAccessRoleComment
	}
	page, pageErr := authorizePageAccessRole(app, actorID, scope.PageID, role)
	if isPermissionDenied(pageErr) {
		return workspaceAuthorization{}, err
	}
	if pageErr != nil {
		return workspaceAuthorization{}, pageErr
	}
	if page.Workspace.WorkspaceID != workspaceID {
		return workspaceAuthorization{}, permissionDenied()
	}
	return page.Workspace, nil
}

// authorizeUploadedProductFileRead lets the person who sent a file for a
// page read it while they can still open that page. Every page they sent it
// for is looked at, a window at a time, until one lets them: eight read and
// then those in the trash or out of reach left out, a file sent for a ninth
// page they could open was refused (2026-11-23).
func authorizeUploadedProductFileRead(app core.App, principalID, workspaceID, fileID string) (workspaceAuthorization, error) {
	const window = 64
	after := ""
	for {
		pageIDs := []string{}
		if err := app.DB().NewQuery(`
			SELECT DISTINCT [[page_id]] FROM {{leafen_file_uploads}}
			WHERE [[file_id]] = {:fileId} AND [[actor_id]] = {:actorId} AND [[workspace_id]] = {:workspaceId}
				AND [[status]] = {:completed} AND [[page_id]] != '' AND [[page_id]] > {:after}
			ORDER BY [[page_id]] LIMIT {:window}
		`).Bind(dbx.Params{
			"fileId": fileID, "actorId": principalID, "workspaceId": workspaceID,
			"completed": fileUploadStatusCompleted, "after": after, "window": window,
		}).Column(&pageIDs); err != nil {
			return workspaceAuthorization{}, err
		}
		trash, err := loadPageTrashEffects(app, pageIDs)
		if err != nil {
			return workspaceAuthorization{}, err
		}
		for _, pageID := range pageIDs {
			if _, trashed := trash[pageID]; trashed {
				continue
			}
			page, err := authorizeReferencedFilePage(app, principalID, pageID)
			if isPermissionDenied(err) {
				continue
			}
			if err != nil {
				return workspaceAuthorization{}, err
			}
			if page.Workspace.WorkspaceID == workspaceID {
				return page.Workspace, nil
			}
		}
		if len(pageIDs) < window {
			return workspaceAuthorization{}, permissionDenied()
		}
		after = pageIDs[len(pageIDs)-1]
	}
}
