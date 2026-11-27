package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database page's body is its page's body, so the Public API's block
// routes serve it as they serve a page's: its blocks keep the IDs the API
// gave them, and the database page answers to its row page ID.

func notionDatabaseObjectIdentityExists(
	app core.App,
	kind string,
	publicID string,
) (bool, error) {
	_, err := resolveNotionObjectIdentity(app, kind, strings.TrimSpace(publicID))
	if errors.Is(err, errInvalidNotionObjectIdentity) {
		return false, nil
	}
	return err == nil, err
}

// authorizeNotionPageOrItemBody authorizes a page's body by its public ID: an
// ordinary page's by its page ID, a database item's by its item ID.
func authorizeNotionPageOrItemBody(
	event *core.RequestEvent,
	publicID string,
	requiredScope string,
	requirement permissionRequirement,
) (notionPageAuthorization, bool, error) {
	return authorizeNotionPageOrItemBodyAs(event, publicID, requiredScope, requirement, notionTrashedEdit)
}

// authorizeNotionPageOrItemBodyAs is authorizeNotionPageOrItemBody refusing
// a write to a database item in the trash in words.
func authorizeNotionPageOrItemBodyAs(
	event *core.RequestEvent,
	publicID string,
	requiredScope string,
	requirement permissionRequirement,
	words notionTrashedEditWords,
) (notionPageAuthorization, bool, error) {
	if !notionPublicIDNamesPage(event.App, strings.TrimSpace(publicID)) {
		item, err := notionDatabaseObjectIdentityExists(event.App, notionObjectIdentityRowPage, publicID)
		if err != nil {
			return notionPageAuthorization{}, false, notionRequestNotCompleted(event, err)
		}
		if item {
			return authorizeNotionRowPageBodyAs(event, publicID, requiredScope, requirement, words)
		}
	}
	return authorizeNotionPage(event, publicID, requiredScope, requirement)
}

// authorizeNotionRowPageBody authorizes a database page's body by its row
// page ID, as authorizeNotionPage authorizes a page's.
func authorizeNotionRowPageBody(
	event *core.RequestEvent,
	publicRowID string,
	requiredScope string,
	requirement permissionRequirement,
) (notionPageAuthorization, bool, error) {
	return authorizeNotionRowPageBodyAs(event, publicRowID, requiredScope, requirement, notionTrashedEdit)
}

// authorizeNotionRowPageBodyAs is authorizeNotionRowPageBody refusing a
// write to a row in the trash, or under a page in it, in words.
func authorizeNotionRowPageBodyAs(
	event *core.RequestEvent,
	publicRowID string,
	requiredScope string,
	requirement permissionRequirement,
	words notionTrashedEditWords,
) (notionPageAuthorization, bool, error) {
	workspace, ok, err := authorizeNotionWorkspaceOperation(event, requiredScope, requirement)
	if err != nil || !ok {
		return notionPageAuthorization{}, false, err
	}
	identity, err := resolveNotionObjectIdentity(event.App, notionObjectIdentityRowPage, strings.TrimSpace(publicRowID))
	if errors.Is(err, errInvalidNotionObjectIdentity) ||
		err == nil && identity.Workspace != workspace.Identity.WorkspaceID {
		return notionPageAuthorization{}, false, notionPageNotFound(event)
	}
	if err != nil {
		return notionPageAuthorization{}, false, notionRequestNotCompleted(event, err)
	}
	if requirement != permissionWorkspaceRead && identity.Workspace == workspace.Identity.WorkspaceID {
		// A row's body in the trash takes no change (notion_trash_refusal.go).
		message, err := notionTrashedBodyEdit(event.App, workspace.Identity, identity.InternalID, true, words)
		if err != nil {
			return notionPageAuthorization{}, false, notionRequestNotCompleted(event, err)
		}
		if message != "" {
			return notionPageAuthorization{}, false, notionPageValidationError(event, message)
		}
	}
	page, err := authorizePageBody(event.App, workspace.Identity.PrincipalID, identity.InternalID, requirement)
	if isPermissionDenied(err) && requirement == permissionWorkspaceRead {
		// One with its database in the trash is read through the page
		// holding the database (notion_trash_reads.go).
		trashed, found, trashedErr := authorizeNotionRowInTrashedSource(
			event.App, workspace.Identity, identity.InternalID, requirement,
		)
		if trashedErr != nil {
			return notionPageAuthorization{}, false, notionRequestNotCompleted(event, trashedErr)
		}
		if found {
			return notionPageAuthorization{Identity: workspace.Identity, Page: trashed}, true, nil
		}
	}
	if isPermissionDenied(err) || err == nil && (page.Workspace.WorkspaceID != workspace.Identity.WorkspaceID ||
		!isDatabaseRowPageRecord(page.Page)) {
		return notionPageAuthorization{}, false, notionPageNotFound(event)
	}
	if err != nil {
		return notionPageAuthorization{}, false, notionRequestNotCompleted(event, err)
	}
	if !notionOAuthIdentityAllowsPage(event.App, workspace.Identity, page) {
		return notionPageAuthorization{}, false, notionPageNotFound(event)
	}
	return notionPageAuthorization{Identity: workspace.Identity, Page: page}, true, nil
}

// loadRowPageBlockOwner names the database page a page block belongs to.
func loadRowPageBlockOwner(app core.App, blockID string) (string, string, bool, error) {
	var rowID, workspaceID string
	err := app.DB().NewQuery(`
		SELECT [[block]].[[page_id]], [[block]].[[workspace_id]] FROM {{leafen_blocks}} AS [[block]]
		JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[id]]=[[block]].[[page_id]] AND [[page]].[[data_source_id]]!=''
		WHERE [[block]].[[id]]={:id}
	`).Bind(dbx.Params{"id": blockID}).Row(&rowID, &workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, nil
	}
	return rowID, workspaceID, err == nil, err
}

// serveNotionRowPageBlockRetrieve reads a database page as a block: a child
// page whose parent is its data source, as Notion's API reads it (seen
// 2026-11-11), in the trash with it, its database or a page above them.
func serveNotionRowPageBlockRetrieve(event *core.RequestEvent, publicRowID string) error {
	if handled, err := serveNotionDatabaseTemplateBlockRetrieve(event, publicRowID); handled {
		return err
	}
	admission, authorized, err := authorizeNotionDatabaseRowPageState(
		event, publicRowID, "pages:read", permissionWorkspaceRead, notionRowAnyTrash,
	)
	if err != nil || !authorized {
		return err
	}
	response, err := notionRowPageBlockObject(event.App, admission.Identity, admission.Authority.RowID)
	if err != nil {
		return notionDatabaseRowPageReadError(event, err)
	}
	recordAPIReads(event.App, admission.Identity, []contentRead{{Kind: "row", ID: admission.Authority.RowID, How: contentReadOpen}})
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

// notionRowPageBlockObject is a database page as a block, read in one
// transaction: a child page whose parent is its data source, in the trash
// with it, its database or a page above them.
func notionRowPageBlockObject(app core.App, identity notionOAuthBearerIdentity, rowID string) (map[string]any, error) {
	response := map[string]any{}
	err := app.RunInTransaction(func(txApp core.App) error {
		current, err := authorizeNotionDatabaseRowPageCurrentState(
			txApp, identity, rowID, permissionWorkspaceRead, notionRowAnyTrash,
		)
		if err != nil {
			return err
		}
		identities, err := ensureNotionDatabaseRowPagePublicIdentities(txApp, current)
		if err != nil {
			return err
		}
		row, err := txApp.FindRecordById(pagesCollection, current.Authority.RowID)
		if err != nil {
			return err
		}
		hasChildren, err := loadNotionPageHasChildren(txApp, []string{row.Id})
		if err != nil {
			return err
		}
		parent, err := notionRowPageParent(txApp, row.Id, identities.DataSourceID, identities.DatabaseID)
		if err != nil {
			return err
		}
		response, err = notionChildPageBlockObject(txApp, row, parent, hasChildren[row.Id])
		if err != nil {
			return err
		}
		place, err := notionPageTrashPlace(txApp, row.Id)
		response["in_trash"] = place.inTrash()
		return err
	})
	return response, err
}
