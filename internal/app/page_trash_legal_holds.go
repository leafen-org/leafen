package app

import (
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The pages in a workspace's trash that an active legal hold keeps from
// permanent deletion, for the trashed page's band and the trash's lines to
// say so before the press instead of a refusal after it. Only the workspace's
// owners read it, the ones who may delete forever, as only they see
// 보안 › 데이터 보존's held pages, and of those only the pages they read
// (owner_listed_pages.go).
//
// A line is held when its page or a page under it is covered — the whole
// organization, the workspace, the page or its owner as custodian — as the
// permanent deletion asks of the page and then of each page it discovers
// below. Without the organization module an active hold keeps every page.

func registerPageTrashLegalHoldRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+pageTrashLegalHoldsPath, readPageTrashLegalHolds)
}

func readPageTrashLegalHolds(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	if _, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceOwner); err != nil {
		return pagePermanentDeleteError(event, err, "trash-legal-holds-authorize")
	}
	pageIDs, err := trashedPagesKeptByLegalHold(event.App, workspaceID)
	if err == nil {
		pageIDs, err = pagesPersonReads(event.App, user.Id, pageIDs)
	}
	if err != nil {
		return pagePermanentDeleteError(event, errors.Join(errPagePermanentDeleteUnavailable, err), "trash-legal-holds")
	}
	databaseIDs, err := trashedDatabasesKeptByLegalHold(event.App, workspaceID, user.Id)
	if err != nil {
		return pagePermanentDeleteError(event, errors.Join(errPagePermanentDeleteUnavailable, err), "trash-legal-hold-databases")
	}
	return event.JSON(http.StatusOK, pageTrashLegalHoldsResponse{PageIDs: pageIDs, DatabaseIDs: databaseIDs})
}

func trashedPagesKeptByLegalHold(app core.App, workspaceID string) ([]string, error) {
	pageIDs := []string{}
	// Nothing to walk while no hold is active: the usual answer, at once.
	if !tableKnown(app, organizationLegalHoldsTable) {
		return pageIDs, nil
	}
	var active int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_organization_legal_holds}} AS [[hold]]
		JOIN {{leafen_workspaces}} AS [[workspace]] ON [[workspace]].[[organization]] = [[hold]].[[organization_id]]
		WHERE [[workspace]].[[id]] = {:workspaceId} AND [[hold]].[[status]] = 'active'
	`).Bind(dbx.Params{"workspaceId": workspaceID}).Row(&active); err != nil {
		return nil, err
	}
	held := legalHoldRetainsPageSQL("[[trashed]].[[id]]", "{:workspaceId}")
	if organizationPolicy.PermanentDeleteHold == nil {
		kept, err := legalHoldsKeptWithoutModule(app)
		if err != nil || !kept {
			return pageIDs, err
		}
		held = "1"
	} else if active == 0 {
		return pageIDs, nil
	}
	// The pages in the trash with the pages under them, each with the one
	// above it there; then the held ones and every trashed page above them.
	err := app.DB().NewQuery(`
		WITH RECURSIVE [[trashed]] ([[id]], [[above]]) AS (
			SELECT [[page]].[[id]], '' FROM {{leafen_pages}} AS [[page]]
			WHERE [[page]].[[workspace]] = {:workspaceId} AND [[page]].[[data_source_id]] = '' AND [[page]].[[in_trash]] = 1
			UNION
			SELECT [[row_trash]].[[row_id]], '' FROM {{leafen_database_row_trash}} AS [[row_trash]]
			WHERE [[row_trash]].[[workspace_id]] = {:workspaceId} AND [[row_trash]].[[deleting]] = 0
			UNION
			SELECT [[child]].[[id]], [[trashed]].[[id]] FROM [[trashed]]
			JOIN {{leafen_pages}} AS [[child]] ON [[child]].[[parent_id]] = [[trashed]].[[id]]
		),
		[[kept]] ([[id]], [[above]]) AS (
			SELECT [[trashed]].[[id]], [[trashed]].[[above]] FROM [[trashed]] WHERE ` + held + `
			UNION
			SELECT [[trashed]].[[id]], [[trashed]].[[above]] FROM [[kept]]
			JOIN [[trashed]] ON [[trashed]].[[id]] = [[kept]].[[above]]
		)
		SELECT DISTINCT [[id]] FROM [[kept]]
	`).Bind(dbx.Params{"workspaceId": workspaceID}).Column(&pageIDs)
	if err != nil {
		return nil, err
	}
	return pageIDs, nil
}
