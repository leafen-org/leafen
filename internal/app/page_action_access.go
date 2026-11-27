package app

import (
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// A row's actions use the same read/body-write authorities as its open page.
// A read admission's role alone can be narrower than an additional edit share.
// No page body or share-principal list crosses this boundary.
func readPageActionAccess(event *core.RequestEvent, principalID, pageID string) error {
	response := pageActionAccessResponse{}
	err := runReadSnapshot(event.Request.Context(), event.App, func(txApp core.App) error {
		authorization, err := authorizePage(txApp, principalID, pageID, permissionWorkspaceRead)
		if err != nil {
			// Held by an operation under way, a member hears which.
			if busy := pageBusyForMember(txApp, principalID, pageID); isPermissionDenied(err) && busy != "" {
				response.Busy = busy
				return nil
			}
			return err
		}
		if response.CopyPercent, err = pageCopyPercent(txApp, authorization.Page); err != nil {
			return err
		}
		if response.CopyTopWritten, err = pageCopyTopWritten(txApp, authorization.Page); err != nil {
			return err
		}
		response.CanEdit, err = pageAcceptsContentEdits(txApp, principalID, pageID)
		if err != nil {
			return err
		}
		// Whether they may suggest, as the page's content read says.
		if response.CanSuggest, err = pageAcceptsSuggestions(txApp, principalID, pageID); err != nil {
			return err
		}
		if response.Busy = pageHoldForMember(txApp, principalID, pageID, response.CanEdit); !response.CanEdit {
			return nil
		}
		// Full access to the body, which Notion asks before a database on the
		// page is turned into a simple table.
		if _, err := authorizePageBody(txApp, principalID, pageID, permissionWorkspaceOwner); err == nil {
			response.CanManage = true
		} else if !isPermissionDenied(err) {
			return err
		}
		return nil
	})
	if isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodePageNotFound, "Page not found.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodePageCreateFailed, "Couldn't read the page access.", err)
	}
	return event.JSON(http.StatusOK, response)
}
