package app

import (
	"context"
	"errors"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type readableProductFileReference struct {
	Kind       string `db:"owner_kind"`
	OwnerID    string `db:"owner_id"`
	PageID     string `db:"page_id"`
	TargetKind string `db:"target_kind"`
	TargetID   string `db:"target_id"`
}

// A page-only guest may read a file while an active reference displays it
// on a page they can still read. This grants no workspace writes and is
// rechecked when a previously issued download capability is used.
func authorizeReferencedProductFileRead(app core.App, principalID, workspaceID, fileID string) (workspaceAuthorization, error) {
	var authorization workspaceAuthorization
	err := runReadSnapshot(context.Background(), app, func(txApp core.App) error {
		var err error
		authorization, err = authorizeReferencedProductFileReadAt(txApp, principalID, workspaceID, fileID, false)
		return err
	})
	return authorization, err
}

// authorizeReferencedProductFileReadAt finds a page showing the file that the
// person may read; wide, for a workspace's member (product_file_member_read.
// go), its pages' history, trash and templates count too. A page in the trash
// still shows its files to whoever can open it, as Notion's.
func authorizeReferencedProductFileReadAt(app core.App, principalID, workspaceID, fileID string, wide bool) (workspaceAuthorization, error) {
	if _, err := authorizeWorkspaceReader(app, principalID, workspaceID); err != nil {
		return workspaceAuthorization{}, err
	}
	const window = 64
	cursor := readableProductFileReference{}
	for {
		rows := []readableProductFileReference{}
		err := app.DB().NewQuery(`
			SELECT DISTINCT [[reference]].[[owner_kind]], [[reference]].[[owner_id]], [[reference]].[[page_id]],
				COALESCE([[comment]].[[target_kind]], '') AS [[target_kind]],
				COALESCE([[comment]].[[target_id]], '') AS [[target_id]]
			FROM {{leafen_file_references}} [[reference]]
			LEFT JOIN {{leafen_comments}} [[comment]] ON [[reference]].[[owner_kind]] = {:commentKind}
				AND [[comment]].[[id]] = [[reference]].[[owner_id]]
				AND [[comment]].[[workspace_id]] = [[reference]].[[workspace_id]]
				AND [[comment]].[[page_id]] = [[reference]].[[page_id]]
			WHERE [[reference]].[[file_id]] = {:fileId} AND [[reference]].[[workspace_id]] = {:workspaceId}
				AND ([[reference]].[[owner_kind]], [[reference]].[[owner_id]], [[reference]].[[page_id]])
					> ({:kind}, {:ownerId}, {:pageId})
			ORDER BY [[reference]].[[owner_kind]], [[reference]].[[owner_id]], [[reference]].[[page_id]]
			LIMIT {:window}
		`).Bind(dbx.Params{
			"fileId": fileID, "workspaceId": workspaceID, "commentKind": productFileReferenceOwnerComment,
			"kind": cursor.Kind, "ownerId": cursor.OwnerID, "pageId": cursor.PageID, "window": window,
		}).All(&rows)
		if err != nil {
			return workspaceAuthorization{}, err
		}
		deniedPages := map[string]bool{}
		for _, reference := range rows {
			pageID := referencedProductFileReadPage(reference, wide)
			if pageID == "" || deniedPages[pageID] {
				continue
			}
			if reference.Kind == productFileReferenceOwnerComment {
				if err := refuseCommentsDuringDeletion(app, reference.PageID, ""); err == errCommentDeleteBusy {
					continue
				} else if err != nil {
					return workspaceAuthorization{}, err
				}
			}
			authorization, err := authorizeReferencedFilePage(app, principalID, pageID)
			if isPermissionDenied(err) {
				deniedPages[pageID] = true
				continue
			}
			if err != nil {
				return workspaceAuthorization{}, err
			}
			if authorization.Workspace.WorkspaceID == workspaceID {
				return authorization.Workspace, nil
			}
		}
		if len(rows) < window {
			return workspaceAuthorization{}, permissionDenied()
		}
		cursor = rows[len(rows)-1]
	}
}

func authorizeReferencedFilePage(app core.App, principalID, pageID string) (pageAuthorization, error) {
	authorization, err := authorizePage(app, principalID, pageID, permissionWorkspaceRead)
	if err != nil || !isDatabaseRowPageRecord(authorization.Page) {
		return authorization, err
	}
	// A template's body reads as its database's page, as the template editor
	// opens it (2026-11-26: a file in a template's body opened for no one,
	// its uploader neither — a template is no row —, and a copy into another
	// workspace couldn't take it).
	if isDatabaseTemplatePageRecord(authorization.Page) {
		return authorizeDatabaseTemplateBody(app, principalID, pageID, pageAccessRoleView)
	}
	row, err := loadDatabaseRowPageAuthority(app, pageID)
	if errors.Is(err, errDatabaseRowPageNotFound) {
		return pageAuthorization{}, permissionDenied()
	}
	if err != nil {
		return pageAuthorization{}, err
	}
	pending, err := notionDatabaseRowPageDeletionPending(app, row)
	if err != nil {
		return pageAuthorization{}, err
	}
	if pending {
		return pageAuthorization{}, permissionDenied()
	}
	return authorizeDatabaseRowPageRecord(app, principalID, row, pageAccessRoleView)
}

func referencedProductFileReadPage(reference readableProductFileReference, wide bool) string {
	switch reference.Kind {
	case productFileReferenceOwnerBlockTrash, productFileReferenceOwnerPageHistory, productFileReferenceOwnerTemplate:
		if wide {
			return reference.PageID
		}
	case productFileReferenceOwnerBlock, productFileReferenceOwnerPageIcon, productFileReferenceOwnerPageCover,
		productFileReferenceOwnerDataSourceIcon, productFileReferenceOwnerDataSourceCover:
		return reference.PageID
	case productFileReferenceOwnerRow, productFileReferenceOwnerDatabaseRowPageIcon:
		return reference.OwnerID
	case productFileReferenceOwnerComment:
		if reference.TargetKind == commentTargetDatabaseRow {
			return reference.TargetID
		}
		if reference.TargetKind == commentTargetPage {
			return reference.PageID
		}
	}
	return ""
}
