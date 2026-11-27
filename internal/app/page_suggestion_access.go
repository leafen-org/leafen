package app

import (
	"context"
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A suggestion changes a page's body, or a database page's. Someone who can
// comment on the body suggests, and a body in Trash, under a page in Trash
// or locked for the person takes no suggestion work.

// suggestionSurface is the body a suggestion changes: a page's, or a
// database page's, whose suggestions are its row's comments.
type suggestionSurface struct {
	// Room holds the body's collaborative text and names its blocks' page:
	// the page, or the row.
	Room string
	Row  bool
	// request is the context of the request that suggests or settles: the
	// versions its writes record name the connection it came through, as
	// the page's other writes do (namePageHistoryConnection) — an agent's
	// suggestion is its person's and the agent's. None for the server's own.
	request context.Context
}

// requestContext is the suggestion request's context for its writes, its
// values without its cancellation: the write goes through as it did before
// the request was gone.
func (surface suggestionSurface) requestContext() context.Context {
	if surface.request == nil {
		return context.Background()
	}
	return context.WithoutCancel(surface.request)
}

func (surface suggestionSurface) targetKey() string {
	if surface.Row {
		return commentTargetDatabaseRow + ":" + surface.Room
	}
	return commentTargetPage + ":" + surface.Room
}

// authorizeSuggestionBody authorizes suggestion work on a body for someone
// who can comment on it. A database page's suggestions are its row's
// comments, and its role is its row page's, as its body's edits are.
func authorizeSuggestionBody(app core.App, actorID string, surface suggestionSurface) (commentAuthorization, error) {
	if !surface.Row {
		return authorizeCommentPage(app, actorID, surface.Room, permissionWorkspaceWrite, "")
	}
	authorization, _, err := authorizeDatabaseRowCommentTarget(app, actorID, surface.Room, permissionWorkspaceWrite)
	if err != nil {
		return commentAuthorization{}, err
	}
	rowPage, err := findRecordRemembered(app, pagesCollection, surface.Room)
	if err != nil {
		return commentAuthorization{}, refusedUnlessStorage(errDatabaseRowPageNotFound, err)
	}
	if authorization.Page, err = authorizeRowPageRecord(app, actorID, rowPage, pageAccessRoleComment); err != nil {
		return commentAuthorization{}, err
	}
	return authorization, nil
}

// suggestionBodyInTrash reports a body in Trash or under a page in Trash.
func suggestionBodyInTrash(app core.App, authorization commentAuthorization) (bool, error) {
	if authorization.Target.Kind == commentTargetDatabaseRow {
		row, err := loadDatabaseRowPageAuthority(app, authorization.Target.ID)
		// A template's page is never in the trash on its own (database_template_row_page.go).
		if errors.Is(err, errDatabaseRowPageNotFound) {
			template, templateErr := isDatabaseTemplatePageID(app, authorization.Target.ID)
			if templateErr != nil {
				return false, templateErr
			}
			if template {
				return pageInTrashPath(app, authorization.Target.StoragePageID)
			}
		}
		if err != nil || row.TrashedAt != 0 {
			return err == nil, err
		}
	}
	return pageInTrashPath(app, authorization.Target.StoragePageID)
}

// refuseSuggestionBody refuses suggestion work on a body in Trash, or under
// a page in Trash, or locked for the person.
func refuseSuggestionBody(app core.App, authorization commentAuthorization, actorID string) error {
	if trashed, err := suggestionBodyInTrash(app, authorization); err != nil {
		return err
	} else if trashed {
		return pageInTrashError()
	}
	locked, err := pageLockedFor(app, authorization.Page.Page, actorID)
	if err != nil {
		return err
	}
	if locked {
		return pageLockedError()
	}
	return nil
}

// checkSuggestionBlock checks that a suggestion's block is a text block on
// its page, and reads the suggestion that suggested the block itself.
func checkSuggestionBlock(app core.App, authorization commentAuthorization, blockID string) (string, error) {
	var block struct {
		Type    string `db:"type"`
		Content string `db:"content"`
	}
	err := app.DB().NewQuery(`
		SELECT [[type]],[[content]] FROM {{leafen_blocks}}
		WHERE [[id]]={:id} AND [[workspace_id]]={:workspaceId} AND [[page_id]]={:pageId}
	`).Bind(dbx.Params{
		"id": blockID, "workspaceId": authorization.Page.Workspace.WorkspaceID,
		"pageId": authorization.Target.ID,
	}).One(&block)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errCommentBlockScope
	}
	if err != nil {
		return "", err
	}
	if _, ok := suggestionBlockTypes[block.Type]; !ok {
		return "", errCommentInvalid
	}
	return graphBlockSuggestion(block.Content), nil
}
