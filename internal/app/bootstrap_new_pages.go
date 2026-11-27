package app

import (
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A screen follows a page made elsewhere by reading its whole page list
// again: in a workspace of 47,000 pages that is 48 windows, 17–25 s on a NAS
// even compressed (2026-11-14), while Notion's sidebar shows another
// screen's new page within a second. So the screen first asks for the pages
// of the workspace made since the newest one its list holds —
// GET /bootstrap?workspaceId=…&createdSince=… — one window by the
// (workspace, created) index, in the list's own shape and under the same
// rules of who sees what, and shows them at once; the whole read follows
// behind it as before, for every other change (web/src/components/product/
// useAuthenticatedBootstrap.ts). More than a window's worth says hasMore,
// and the screen waits for the whole read.

const maxBootstrapNewSinceLength = 40

// bootstrapNewSince is a moment as pages' created are written
// ("2026-11-14 13:47:18.579Z"): digits, '-', ':', '.', ' ', 'T' and 'Z'.
func bootstrapNewSince(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxBootstrapNewSinceLength {
		return "", false
	}
	for _, char := range raw {
		if !(char >= '0' && char <= '9') && !strings.ContainsRune("-:. TZ", char) {
			return "", false
		}
	}
	return raw, true
}

func bootstrapNewPages(event *core.RequestEvent, user *core.Record, workspaceID, rawSince string, pageLimit int) error {
	since, ok := bootstrapNewSince(rawSince)
	workspaceID = strings.TrimSpace(workspaceID)
	if !ok || workspaceID == "" || len(workspaceID) > maxWorkspacePermissionIdentifierLength {
		return apiError(event, http.StatusBadRequest, errorCodeBootstrapCursorInvalid, "Check the initial sync range.")
	}
	ready, err := ensureBootstrapVisibilityProjectionReadiness(
		event, user.Id, errorCodeBootstrapFailed, "Couldn't load the workspace.", "bootstrap",
	)
	if !ready {
		return err
	}
	response := bootstrapResponse{}
	err = runReadSnapshot(event.Request.Context(), event.App, func(txApp core.App) error {
		snapshotRevision, err := readBootstrapSnapshotRevision(txApp)
		if err != nil {
			return err
		}
		authority, err := bootstrapReaderAuthority(txApp, user.Id)
		if err != nil {
			return err
		}
		response = bootstrapResponse{
			SnapshotRevision: snapshotRevision, AuthorityRevision: authority,
			Workspaces: []workspaceResponse{}, Pages: []pageResponse{},
			PageFavorites: []pageFavoriteProjection{}, WorkspaceRefreshTokens: []workspaceRefreshTokenResponse{},
			DataEpoch: currentDataEpoch(event.App),
		}
		rows, err := loadBootstrapNewPages(txApp, user.Id, workspaceID, since, pageLimit+1)
		if err != nil {
			return err
		}
		if len(rows) > pageLimit {
			response.HasMore = true
			return nil
		}
		visible, err := withoutHiddenBootstrapPages(txApp, user.Id, rows)
		if err != nil {
			return err
		}
		if err := appendBootstrapPages(txApp, user.Id, &response, visible); err != nil {
			return err
		}
		if response.SharedPageIDs, err = sharedPrivatePageIDs(txApp, visible); err != nil {
			return err
		}
		pageIDs := make(map[string]bool, len(response.Pages))
		for _, page := range response.Pages {
			pageIDs[page.ID] = true
		}
		if response.DatabaseFavorites, err = bootstrapDatabaseFavorites(txApp, user.Id, pageIDs); err != nil {
			return err
		}
		response.InlineDatabases, err = bootstrapInlineDatabases(txApp, user.Id, pageIDs)
		return err
	})
	if err != nil {
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeBootstrapFailed,
			"Couldn't load the workspace.", "bootstrap", "new_pages", errorCodeBootstrapFailed, err,
		)
	}
	return event.JSON(http.StatusOK, response)
}

// loadBootstrapNewPages reads the pages of a workspace made at or after a
// moment that the person's sidebar lists (bootstrapPageSeenSQL), as the
// list's windows do (loadBootstrapPartPages).
func loadBootstrapNewPages(app core.App, principalID, workspaceID, since string, limit int) ([]storedBootstrapPage, error) {
	rows := []storedBootstrapPage{}
	err := app.DB().NewQuery(`
		SELECT ` + bootstrapPageColumnsSQL + `
		FROM {{leafen_pages}} [[page]] INDEXED BY [[idx_leafen_pages_workspace_created]]
		CROSS JOIN {{leafen_workspaces}} [[workspace]]
		LEFT JOIN {{leafen_page_favorites}} [[favorite_state]]
			ON [[favorite_state]].[[page_id]]=[[page]].[[id]]
			AND [[favorite_state]].[[principal_id]] = {:principalId}
		WHERE [[page]].[[workspace]] = {:workspaceId} AND [[page]].[[created]] >= {:since}
			AND [[workspace]].[[id]] = [[page]].[[workspace]]
			AND [[workspace]].[[deletion_job_id]] = ''
			AND [[page]].[[owner]] = [[workspace]].[[owner]]
			AND [[page]].[[notion_import_staging]] = FALSE
			AND ` + bootstrapPageSeenSQL("[[page]]", "{:principalId}") + `
			AND ` + pageNotHeldForDeletionSQL("[[page]]", "{:workspaceId}") + `
			AND ` + bootstrapPageCompleteSQL + `
		ORDER BY [[page]].[[created]], [[page]].[[id]]
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"principalId": principalID, "workspaceId": workspaceID, "since": since, "limit": limit,
	}).All(&rows)
	return rows, err
}
