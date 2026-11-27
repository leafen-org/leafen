package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Which native comment target a Notion comment request names, and whether
// the caller may read or write it: a page, a block or a row page, a stored
// comment, or a discussion thread.

type notionCommentTarget struct {
	Identity      notionOAuthBearerIdentity
	Page          pageAuthorization
	PageID        string
	StoragePageID string
	TargetKind    string
	BlockID       string
}

func (target notionCommentTarget) nativeTarget() nativeCommentTarget {
	return nativeCommentTarget{
		Kind: target.TargetKind, ID: target.PageID, StoragePageID: target.StoragePageID,
	}
}

func (target notionCommentTarget) authorizeCurrent(
	app core.App, requirement permissionRequirement,
) (commentAuthorization, error) {
	if target.TargetKind == commentTargetDatabaseRow {
		authorization, _, err := authorizeDatabaseRowCommentTarget(
			app, target.Identity.PrincipalID, target.PageID, requirement,
		)
		if err != nil || authorization.Target.StoragePageID != target.StoragePageID ||
			authorization.Page.Workspace.WorkspaceID != target.Identity.WorkspaceID ||
			!notionOAuthIdentityAllowsPage(app, target.Identity, authorization.Page) {
			if err != nil {
				return commentAuthorization{}, err
			}
			return commentAuthorization{}, errCommentNotFound
		}
		return authorization, nil
	}
	authorization, err := authorizeCommentPage(
		app, target.Identity.PrincipalID, target.PageID, requirement, "",
	)
	if err != nil {
		return commentAuthorization{}, err
	}
	if target.StoragePageID != target.PageID ||
		!notionOAuthIdentityAllowsPage(app, target.Identity, authorization.Page) {
		return commentAuthorization{}, errCommentNotFound
	}
	return authorization, nil
}

func serveNotionCommentCompatibility(event *core.RequestEvent) (bool, error) {
	if event.Request.URL.Path == notionAPICommentsPath {
		switch event.Request.Method {
		case http.MethodGet:
			return true, serveNotionCommentList(event)
		case http.MethodPost:
			return true, serveNotionCommentCreate(event)
		}
		return false, nil
	}
	if !strings.HasPrefix(event.Request.URL.Path, notionAPICommentsPath+"/") {
		return false, nil
	}
	commentID := strings.TrimPrefix(event.Request.URL.Path, notionAPICommentsPath+"/")
	if commentID == "" || strings.Contains(commentID, "/") {
		return false, nil
	}
	switch event.Request.Method {
	case http.MethodGet:
		return true, serveNotionCommentRetrieve(event, commentID)
	case http.MethodPatch:
		return true, serveNotionCommentUpdate(event, commentID)
	case http.MethodDelete:
		return true, serveNotionCommentDelete(event, commentID)
	default:
		return false, nil
	}
}

func authorizeNotionCommentTarget(
	event *core.RequestEvent,
	publicTargetID string,
	requiredScope string,
	requirement permissionRequirement,
) (notionCommentTarget, bool, error) {
	workspace, ok, err := authorizeNotionWorkspaceOperation(
		event, requiredScope, permissionWorkspaceRead,
	)
	if err != nil || !ok {
		return notionCommentTarget{}, false, err
	}
	identity := workspace.Identity
	publicTargetID = strings.TrimSpace(publicTargetID)
	if pageID, resolveErr := resolveNotionPageRecordID(event.App, publicTargetID); resolveErr == nil && notionPublicIDNamesPage(event.App, publicTargetID) {
		commentPage, err := authorizeCommentPage(
			event.App, identity.PrincipalID, pageID, requirement, "",
		)
		if isPermissionDenied(err) || errors.Is(err, errCommentDeleteBusy) {
			return notionCommentTarget{}, false, notionCommentNotFound(event)
		}
		if err != nil {
			return notionCommentTarget{}, false, notionCommentInternalError(event, err)
		}
		if commentPage.Page.Workspace.WorkspaceID != identity.WorkspaceID ||
			!notionOAuthIdentityAllowsPage(event.App, identity, commentPage.Page) {
			return notionCommentTarget{}, false, notionCommentNotFound(event)
		}
		return notionCommentTarget{
			Identity: identity, Page: commentPage.Page, PageID: pageID,
			StoragePageID: pageID, TargetKind: commentTargetPage,
		}, true, nil
	}
	blockID, valid := resolveNotionBlockID(event.App, publicTargetID)
	if valid {
		block := struct {
			WorkspaceID string `db:"workspace_id"`
			PageID      string `db:"page_id"`
		}{}
		err = event.App.DB().NewQuery(`
			SELECT [[workspace_id]],[[page_id]] FROM {{leafen_blocks}}
			WHERE [[id]]={:id} LIMIT 2
		`).Bind(dbx.Params{"id": blockID}).One(&block)
		if err == nil {
			// A block in a database page's body is the row's: its comments
			// are the row's comments on that block.
			var dataSourceID string
			if rowErr := event.App.DB().NewQuery(`SELECT [[data_source_id]] FROM {{leafen_pages}} WHERE [[id]]={:id}`).
				Bind(dbx.Params{"id": block.PageID}).Row(&dataSourceID); rowErr != nil && !errors.Is(rowErr, sql.ErrNoRows) {
				return notionCommentTarget{}, false, notionCommentInternalError(event, rowErr)
			}
			if dataSourceID != "" && block.WorkspaceID == identity.WorkspaceID {
				commentTarget, row, authorizeErr := authorizeDatabaseRowCommentTarget(
					event.App, identity.PrincipalID, block.PageID, requirement,
				)
				if authorizeErr == nil && notionOAuthIdentityAllowsPage(event.App, identity, commentTarget.Page) {
					return notionCommentTarget{
						Identity: identity, Page: commentTarget.Page, PageID: row.RowID,
						StoragePageID: row.ContainerPageID, TargetKind: commentTargetDatabaseRow, BlockID: blockID,
					}, true, nil
				}
				if authorizeErr != nil && !errors.Is(authorizeErr, errDatabaseRowPageNotFound) && !isPermissionDenied(authorizeErr) {
					return notionCommentTarget{}, false, notionCommentInternalError(event, authorizeErr)
				}
				return notionCommentTarget{}, false, notionCommentNotFound(event)
			}
			commentPage, authorizeErr := authorizeCommentPage(
				event.App, identity.PrincipalID, block.PageID, requirement, "",
			)
			if authorizeErr == nil && block.WorkspaceID == identity.WorkspaceID &&
				commentPage.Page.Workspace.WorkspaceID == identity.WorkspaceID &&
				notionOAuthIdentityAllowsPage(event.App, identity, commentPage.Page) {
				return notionCommentTarget{
					Identity: identity, Page: commentPage.Page, PageID: block.PageID,
					StoragePageID: block.PageID, TargetKind: commentTargetPage, BlockID: blockID,
				}, true, nil
			}
			if authorizeErr != nil && !isPermissionDenied(authorizeErr) &&
				!errors.Is(authorizeErr, errCommentDeleteBusy) {
				return notionCommentTarget{}, false, notionCommentInternalError(event, authorizeErr)
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return notionCommentTarget{}, false, notionCommentInternalError(event, err)
		} else if requirement == permissionWorkspaceRead {
			// A block in the trash lists its comments as before
			// (notion_trash_reads.go).
			target, found, err := notionTrashedBlockCommentTarget(event.App, identity, blockID)
			if err != nil {
				return notionCommentTarget{}, false, notionCommentInternalError(event, err)
			}
			if found {
				return target, true, nil
			}
		}
	}
	if rowIdentity, resolveErr := resolveNotionObjectIdentity(
		event.App, notionObjectIdentityRowPage, publicTargetID,
	); resolveErr == nil && rowIdentity.Workspace == identity.WorkspaceID {
		commentTarget, row, authorizeErr := authorizeDatabaseRowCommentTarget(
			event.App, identity.PrincipalID, rowIdentity.InternalID, requirement,
		)
		if authorizeErr == nil &&
			notionOAuthIdentityAllowsPage(event.App, identity, commentTarget.Page) {
			return notionCommentTarget{
				Identity: identity, Page: commentTarget.Page, PageID: row.RowID,
				StoragePageID: row.ContainerPageID, TargetKind: commentTargetDatabaseRow,
			}, true, nil
		}
		if !errors.Is(authorizeErr, errDatabaseRowPageNotFound) && !isPermissionDenied(authorizeErr) {
			return notionCommentTarget{}, false, notionCommentInternalError(event, authorizeErr)
		}
		if requirement == permissionWorkspaceRead {
			// One with its database in the trash lists its comments too
			// (notion_trash_reads.go).
			target, found, err := notionTrashedSourceRowCommentTarget(event.App, identity, rowIdentity.InternalID)
			if err != nil {
				return notionCommentTarget{}, false, notionCommentInternalError(event, err)
			}
			if found {
				return target, true, nil
			}
		}
	} else if resolveErr != nil && !errors.Is(resolveErr, errInvalidNotionObjectIdentity) {
		return notionCommentTarget{}, false, notionCommentInternalError(event, resolveErr)
	}
	if rowBlockIdentity, resolveErr := resolveNotionObjectIdentity(
		event.App, notionObjectIdentityRowBlock, publicTargetID,
	); resolveErr == nil && rowBlockIdentity.Workspace == identity.WorkspaceID {
		blockRowID, blockWorkspaceID, found, loadErr := loadRowPageBlockOwner(
			event.App, rowBlockIdentity.InternalID,
		)
		if loadErr != nil {
			return notionCommentTarget{}, false, notionCommentInternalError(event, loadErr)
		}
		if found && blockWorkspaceID == identity.WorkspaceID {
			commentTarget, row, authorizeErr := authorizeDatabaseRowCommentTarget(
				event.App, identity.PrincipalID, blockRowID, requirement,
			)
			if authorizeErr == nil &&
				notionOAuthIdentityAllowsPage(event.App, identity, commentTarget.Page) {
				return notionCommentTarget{
					Identity: identity, Page: commentTarget.Page, PageID: row.RowID,
					StoragePageID: row.ContainerPageID, TargetKind: commentTargetDatabaseRow,
					BlockID: rowBlockIdentity.InternalID,
				}, true, nil
			}
			if !errors.Is(authorizeErr, errDatabaseRowPageNotFound) &&
				!isPermissionDenied(authorizeErr) {
				return notionCommentTarget{}, false, notionCommentInternalError(event, authorizeErr)
			}
		}
	} else if resolveErr != nil && !errors.Is(resolveErr, errInvalidNotionObjectIdentity) {
		return notionCommentTarget{}, false, notionCommentInternalError(event, resolveErr)
	}
	return notionCommentTarget{}, false, notionCommentNotFound(event)
}

func authorizeNotionComment(
	event *core.RequestEvent,
	publicCommentID string,
	requiredScope string,
	requirement permissionRequirement,
) (notionCommentTarget, storedComment, bool, error) {
	workspace, ok, err := authorizeNotionWorkspaceOperation(
		event, requiredScope, permissionWorkspaceRead,
	)
	if err != nil || !ok {
		return notionCommentTarget{}, storedComment{}, false, err
	}
	identity := workspace.Identity
	resolved, err := resolveNotionObjectIdentity(
		event.App, notionObjectIdentityComment, strings.TrimSpace(publicCommentID),
	)
	if errors.Is(err, errInvalidNotionObjectIdentity) ||
		err == nil && resolved.Workspace != identity.WorkspaceID {
		return notionCommentTarget{}, storedComment{}, false, notionCommentNotFound(event)
	}
	if err != nil {
		return notionCommentTarget{}, storedComment{}, false, notionCommentInternalError(event, err)
	}
	row, err := loadCommentByID(event.App, resolved.InternalID)
	if errors.Is(err, errCommentNotFound) {
		return notionCommentTarget{}, storedComment{}, false, notionCommentNotFound(event)
	}
	if err != nil {
		return notionCommentTarget{}, storedComment{}, false, notionCommentInternalError(event, err)
	}
	target, err := authorizeNotionStoredCommentTarget(event.App, identity, row, requirement)
	if errors.Is(err, errCommentNotFound) || isPermissionDenied(err) ||
		errors.Is(err, errCommentDeleteBusy) || errors.Is(err, errDatabaseRowPageNotFound) {
		return notionCommentTarget{}, storedComment{}, false, notionCommentNotFound(event)
	}
	if err != nil {
		return notionCommentTarget{}, storedComment{}, false, notionCommentInternalError(event, err)
	}
	return target, row, true, nil
}

func authorizeNotionStoredCommentTarget(
	app core.App,
	identity notionOAuthBearerIdentity,
	row storedComment,
	requirement permissionRequirement,
) (notionCommentTarget, error) {
	if row.WorkspaceID != identity.WorkspaceID {
		return notionCommentTarget{}, errCommentNotFound
	}
	if row.TargetKind == commentTargetDatabaseRow {
		commentTarget, authority, err := authorizeDatabaseRowCommentTarget(
			app, identity.PrincipalID, row.TargetID, requirement,
		)
		if err != nil || authority.ContainerPageID != row.PageID ||
			commentTarget.Page.Workspace.WorkspaceID != identity.WorkspaceID ||
			!notionOAuthIdentityAllowsPage(app, identity, commentTarget.Page) {
			if err != nil {
				return notionCommentTarget{}, err
			}
			return notionCommentTarget{}, errCommentNotFound
		}
		return notionCommentTarget{
			Identity: identity, Page: commentTarget.Page, PageID: row.TargetID,
			StoragePageID: row.PageID, TargetKind: row.TargetKind, BlockID: row.BlockID,
		}, nil
	}
	if row.TargetKind != commentTargetPage || row.TargetID != row.PageID {
		return notionCommentTarget{}, errCommentNotFound
	}
	authorization, err := authorizeCommentPage(
		app, identity.PrincipalID, row.PageID, requirement, "",
	)
	if err != nil {
		return notionCommentTarget{}, err
	}
	if !notionOAuthIdentityAllowsPage(app, identity, authorization.Page) {
		return notionCommentTarget{}, errCommentNotFound
	}
	return notionCommentTarget{
		Identity: identity, Page: authorization.Page, PageID: row.PageID,
		StoragePageID: row.PageID, TargetKind: row.TargetKind, BlockID: row.BlockID,
	}, nil
}

func loadCommentByID(app core.App, commentID string) (storedComment, error) {
	row := storedComment{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[workspace_id]],[[page_id]],[[target_kind]],[[target_id]],
			[[block_id]],[[parent_id]],[[author_id]],
			[[body_json]],[[notion_discussion_id]],[[resolved]],[[revision]],
			[[created_at]],[[updated_at]],[[edited_at]]
		FROM {{leafen_comments}} WHERE [[id]]={:id} LIMIT 2
	`).Bind(dbx.Params{"id": strings.TrimSpace(commentID)}).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return storedComment{}, errCommentNotFound
	}
	return row, err
}

func authorizeNotionDiscussion(
	event *core.RequestEvent,
	publicDiscussionID string,
	requiredScope string,
) (notionCommentTarget, storedComment, bool, error) {
	discussionID, ok := normalizeNotionUUIDRequest(strings.TrimSpace(publicDiscussionID))
	if !ok {
		return notionCommentTarget{}, storedComment{}, false, notionCommentNotFound(event)
	}
	workspace, authorized, err := authorizeNotionWorkspaceOperation(
		event, requiredScope, permissionWorkspaceRead,
	)
	if err != nil || !authorized {
		return notionCommentTarget{}, storedComment{}, false, err
	}
	root := storedComment{}
	err = event.App.DB().NewQuery(`
		SELECT [[id]],[[workspace_id]],[[page_id]],[[target_kind]],[[target_id]],
			[[block_id]],[[parent_id]],[[author_id]],
			[[body_json]],[[notion_discussion_id]],[[resolved]],[[revision]],
			[[created_at]],[[updated_at]],[[edited_at]]
		FROM {{leafen_comments}} INDEXED BY [[idx_leafen_comments_discussion_lookup]]
		WHERE [[workspace_id]]={:workspaceId} AND [[notion_discussion_id]]={:discussionId}
			AND [[parent_id]]=''
		ORDER BY [[created_at]],[[id]] LIMIT 1
	`).Bind(dbx.Params{
		"workspaceId": workspace.Identity.WorkspaceID, "discussionId": discussionID,
	}).One(&root)
	if errors.Is(err, sql.ErrNoRows) {
		return notionCommentTarget{}, storedComment{}, false, notionCommentNotFound(event)
	}
	if err != nil {
		return notionCommentTarget{}, storedComment{}, false, notionCommentInternalError(event, err)
	}
	target, err := authorizeNotionStoredCommentTarget(
		event.App, workspace.Identity, root, permissionWorkspaceWrite,
	)
	if isPermissionDenied(err) || errors.Is(err, errCommentDeleteBusy) ||
		errors.Is(err, errDatabaseRowPageNotFound) || errors.Is(err, errCommentNotFound) {
		return notionCommentTarget{}, storedComment{}, false, notionCommentNotFound(event)
	}
	if err != nil {
		return notionCommentTarget{}, storedComment{}, false, notionCommentInternalError(event, err)
	}
	return target, root, true, nil
}
