package app

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

var (
	errCommentInvalid          = errors.New(errorCodeCommentInvalid)
	errCommentParentScope      = errors.New(errorCodeCommentParentScope)
	errCommentBlockScope       = errors.New(errorCodeCommentBlockScope)
	errCommentNotFound         = errors.New(errorCodeCommentNotFound)
	errCommentIDConflict       = errors.New(errorCodeCommentIdConflict)
	errCommentRequestConflict  = errors.New(errorCodeCommentRequestConflict)
	errCommentRevisionConflict = errors.New(errorCodeCommentRevisionConflict)
	errCommentSnapshotChanged  = errors.New(errorCodeCommentSnapshotChanged)
	errCommentDeleteBusy       = errors.New(errorCodeCommentDeleteBusy)
	errCommentFailed           = errors.New(errorCodeCommentFailed)
)

type commentAuthorization struct {
	Page   pageAuthorization
	Target nativeCommentTarget
}

type nativeCommentTarget struct {
	Kind          string
	ID            string
	StoragePageID string
}

func nativePageCommentTarget(pageID string) nativeCommentTarget {
	return nativeCommentTarget{Kind: commentTargetPage, ID: pageID, StoragePageID: pageID}
}

func authorizeDatabaseRowCommentTarget(
	app core.App,
	actorID, rowID string,
	requirement permissionRequirement,
) (commentAuthorization, databaseRowPageAuthority, error) {
	// A template takes comments in its window as a page does
	// (database_template_row_page.go). The page of a data source in the
	// trash shows its comments as a page in the trash does, which change
	// once it is restored.
	row, sourceInTrash, err := authorizeDatabaseRowPageRead(app, actorID, strings.TrimSpace(rowID))
	if err == nil && sourceInTrash && requirement != permissionWorkspaceRead {
		err = errDatabaseRowPageNotFound
	}
	if err != nil {
		return commentAuthorization{}, databaseRowPageAuthority{}, err
	}
	pending, err := notionDatabaseRowPageDeletionPending(app, row)
	if err != nil {
		return commentAuthorization{}, databaseRowPageAuthority{}, err
	}
	if pending {
		return commentAuthorization{}, databaseRowPageAuthority{}, errDatabaseRowPageNotFound
	}
	page, err := authorizeCommentPage(app, actorID, row.ContainerPageID, requirement, "")
	if isPermissionDenied(err) {
		// A database page shared on its own takes comments by its share.
		page, err = authorizeDatabaseRowCommentGrant(app, actorID, row, requirement)
	}
	if err != nil {
		return commentAuthorization{}, databaseRowPageAuthority{}, err
	}
	if page.Page.Workspace.WorkspaceID != row.WorkspaceID ||
		page.Page.Workspace.OwnerID != row.OwnerID {
		return commentAuthorization{}, databaseRowPageAuthority{}, errDatabaseRowPageNotFound
	}
	return commentAuthorization{
		Page: page.Page,
		Target: nativeCommentTarget{
			Kind: commentTargetDatabaseRow, ID: row.RowID, StoragePageID: row.ContainerPageID,
		},
	}, row, nil
}

// authorizeDatabaseRowCommentGrant authorizes comment work on a database
// page by its own share: reading needs view, writing needs comment.
func authorizeDatabaseRowCommentGrant(
	app core.App, actorID string, row databaseRowPageAuthority, requirement permissionRequirement,
) (commentAuthorization, error) {
	minimum := pageAccessRoleView
	if requirement != permissionWorkspaceRead {
		minimum = pageAccessRoleComment
	}
	grant, err := authorizeDatabaseRowPageGrant(app, actorID, row, minimum)
	if err != nil {
		return commentAuthorization{}, err
	}
	grant.Page, err = findRecordRemembered(app, pagesCollection, row.ContainerPageID)
	if errors.Is(err, sql.ErrNoRows) {
		return commentAuthorization{}, permissionDenied()
	}
	if err != nil {
		return commentAuthorization{}, err
	}
	if err := refuseCommentsDuringDeletion(app, row.ContainerPageID, ""); err != nil {
		return commentAuthorization{}, err
	}
	return commentAuthorization{Page: grant, Target: nativePageCommentTarget(row.ContainerPageID)}, nil
}

func validCommentIdentifier(value string) bool {
	return value != "" && value == strings.TrimSpace(value) &&
		len([]rune(value)) <= commentIdentifierRunes
}

func authorizeCommentPage(
	app core.App,
	actorID, pageID string,
	requirement permissionRequirement,
	allowedDeletionJobID string,
) (commentAuthorization, error) {
	if !validCommentIdentifier(pageID) {
		return commentAuthorization{}, permissionDenied()
	}
	minimumRole := pageAccessRoleView
	if requirement != permissionWorkspaceRead {
		minimumRole = pageAccessRoleComment
	}
	page, err := authorizePageAccessRole(app, actorID, pageID, minimumRole)
	if err != nil {
		return commentAuthorization{}, err
	}
	if err := refuseCommentsDuringDeletion(app, pageID, allowedDeletionJobID); err != nil {
		return commentAuthorization{}, err
	}
	return commentAuthorization{Page: page, Target: nativePageCommentTarget(pageID)}, nil
}

// refuseCommentsDuringDeletion refuses comment work on a page whose
// comments are being deleted, but for that deletion's own job.
func refuseCommentsDuringDeletion(app core.App, pageID, allowedDeletionJobID string) error {
	var pending int
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_comment_deletion_jobs}}
		WHERE [[page_id]]={:pageId} AND [[status]]={:pending} AND [[id]] != {:allowed}
	`).Bind(dbx.Params{
		"pageId": pageID, "pending": commentDeletionStatusPending, "allowed": allowedDeletionJobID,
	}).Row(&pending)
	if err != nil {
		return err
	}
	if pending != 0 {
		return errCommentDeleteBusy
	}
	return nil
}

func validateCommentAnchor(
	app core.App,
	authorization commentAuthorization,
	blockID, parentID string,
) error {
	if blockID != "" {
		if !validCommentIdentifier(blockID) {
			return errCommentBlockScope
		}
		// A database page's blocks are its page's, under the row's ID.
		var count int
		err := app.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_blocks}}
			WHERE [[id]]={:id} AND [[workspace_id]]={:workspaceId} AND [[page_id]]={:targetId}
		`).Bind(dbx.Params{
			"id": blockID, "workspaceId": authorization.Page.Workspace.WorkspaceID,
			"targetId": authorization.Target.ID,
		}).Row(&count)
		if err != nil {
			return err
		}
		if count != 1 {
			return errCommentBlockScope
		}
	}
	if parentID == "" {
		return nil
	}
	if !validCommentIdentifier(parentID) {
		return errCommentParentScope
	}
	var parent struct {
		WorkspaceID string `db:"workspace_id"`
		TargetKind  string `db:"target_kind"`
		TargetID    string `db:"target_id"`
	}
	err := app.DB().NewQuery(`
		SELECT [[workspace_id]],[[target_kind]],[[target_id]]
		FROM {{leafen_comments}} WHERE [[id]]={:id}
	`).Bind(dbx.Params{"id": parentID}).One(&parent)
	if errors.Is(err, sql.ErrNoRows) {
		return errCommentParentScope
	}
	if err != nil {
		return err
	}
	if parent.WorkspaceID != authorization.Page.Workspace.WorkspaceID ||
		parent.TargetKind != authorization.Target.Kind || parent.TargetID != authorization.Target.ID {
		return errCommentParentScope
	}
	return nil
}

func authorizeCommentMutation(
	authorization commentAuthorization,
	actorID, authorID string,
) error {
	if actorID == authorID || authorization.Page.AccessRole >= pageAccessRoleEdit {
		return nil
	}
	return permissionDenied()
}
