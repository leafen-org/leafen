package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Leaving a page shared with you, as Notion's "나가기" on the page a guest's
// "공유된 페이지" lists at its top (seen 2026-11-16 in Chrome, two tabs at
// different times, a guest with full access in a free workspace): it asks
// nothing, takes away the person's own share of the page, and the page is
// gone from their sidebar once the server has answered; its owner's share
// window no longer lists them. Only the person's own share, given to them by
// name or address and not by a page rule, goes: someone else's share is the
// page's managers' to take away, and what reaches them from the page above
// isn't the page's to drop.

const pageShareLeavePath = "/pages/{pageId}/leave"

// errPageShareLeaveNoOwnShare is a page the person opens through the page
// above it, a rule or their workspace, with no share of their own to leave.
var errPageShareLeaveNoOwnShare = publicMutationRefused(
	"you have no share of your own on this page to leave; you open it through the page above it, a page rule or your workspace")

// errPageShareLeaveRaises is a share of one's own that holds the person below
// what the page above gives them: leaving it would open the page wider to
// them, so it is the page's managers' to change.
var errPageShareLeaveRaises = publicMutationRefused(
	"leaving this share would give you more access than it does; the page's managers change it")

func registerPageShareLeaveRoute(event *core.ServeEvent) {
	event.Router.POST(apiRoot+pageShareLeavePath, leavePageShare)
}

func leavePageShare(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	var err error
	// A sharing change that lands between reading the page's revision and
	// taking its lock is read again once.
	for attempt := 0; attempt < 2; attempt++ {
		var input publicMutationInput
		if input, err = pageShareLeaveInput(event.App, user.Id, pageID); err != nil {
			break
		}
		if userKeyAsksWhereItActs(event) {
			release, lockErr := lockPublicMutation(event.App, user.Id, pageID, &input)
			if lockErr == nil {
				lockErr = rehearseBeforeAsking(event, func(txApp core.App) error {
					_, _, err := executePublicMutationTransaction(event.Request.Context(), txApp, user.Id, pageID, input)
					return err
				})
				release()
			}
			if lockErr != nil {
				return publicMutationRouteError(event, lockErr, "leave")
			}
			if asked, askErr := askBeforeActing(event); asked {
				return askErr
			}
		}
		_, _, err = executePublicMutation(event.Request.Context(), event.App, user.Id, pageID, input)
		if !errors.Is(err, errPublicMutationRevisionConflict) {
			break
		}
	}
	if err != nil {
		return publicMutationRouteError(event, err, "leave")
	}
	return event.NoContent(http.StatusNoContent)
}

// pageShareLeaveInput is the sharing change that takes the person's own
// share of the page away, made at the page's sharing as it is now.
func pageShareLeaveInput(app core.App, actorID, pageID string) (publicMutationInput, error) {
	if _, err := authorizePublicMutationReader(app, actorID, pageID); err != nil {
		return publicMutationInput{}, err
	}
	own, err := ownPageShareID(app, actorID, pageID)
	if err != nil {
		return publicMutationInput{}, err
	}
	if own == "" {
		return publicMutationInput{}, errPageShareLeaveNoOwnShare
	}
	revision := 0
	if stored, err := loadStoredPublicMutationState(app, pageID); err == nil {
		revision = stored.Revision
	} else if !errors.Is(err, sql.ErrNoRows) {
		return publicMutationInput{}, err
	}
	return publicMutationInput{
		RequestID: securityRandomID(), Action: publicMutationActionRemovePermission,
		PermissionID: own, ExpectedRevision: revision,
	}, nil
}

// ownPageShareID is the person's own share of the page: given to them by
// name, or to an address of theirs they proved, and not by a page rule; ""
// when they have none.
func ownPageShareID(app core.App, actorID, pageID string) (string, error) {
	ids := []string{}
	err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_page_permissions}}
		WHERE [[page_id]]={:pageId} AND [[rule_id]]='' AND (
			([[principal_type]]='user' AND [[principal_id]]={:actorId})
			OR ([[principal_type]]='email' AND lower(trim([[principal_id]])) IN (` + verifiedAccountEmailSQL("actorId") + `)))
		ORDER BY [[created_at]], [[id]] LIMIT 1
	`).Bind(dbx.Params{"pageId": pageID, "actorId": actorID}).Column(&ids)
	if err != nil || len(ids) == 0 {
		return "", err
	}
	return ids[0], nil
}

// authorizePublicMutationChange is who may change what a page shares: its
// full access; and anyone who opens the page may take their own share of it
// away (leavePageShare), as Notion's "나가기".
func authorizePublicMutationChange(app core.App, actorID, pageID string, input publicMutationInput) (pageAuthorization, error) {
	authorization, err := authorizePublicMutationManager(app, actorID, pageID)
	if !errors.Is(err, errPublicMutationNotFound) || input.Action != publicMutationActionRemovePermission {
		return authorization, err
	}
	reader, readErr := authorizePublicMutationReader(app, actorID, pageID)
	if readErr != nil {
		return pageAuthorization{}, err
	}
	own, ownErr := ownPageShareID(app, actorID, pageID)
	if ownErr != nil {
		return pageAuthorization{}, ownErr
	}
	if own == "" || own != input.PermissionID {
		return pageAuthorization{}, err
	}
	return reader, nil
}

// removePageSharePermission takes a share away; one's own share that held one
// below what the page above gives goes back, so leaving never opens a page
// wider to its leaver (errPageShareLeaveRaises).
func removePageSharePermission(
	app core.App, authorization pageAuthorization, state storedPublicMutationState, input publicMutationInput,
) error {
	actorID := authorization.Workspace.PrincipalID
	own, err := ownPageShareID(app, actorID, state.PageID)
	if err != nil {
		return err
	}
	if err := removePublicMutationPermission(app, state, input); err != nil || own == "" || own != input.PermissionID {
		return err
	}
	after, err := authorizePublicMutationReader(app, actorID, state.PageID)
	if errors.Is(err, errPublicMutationNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if after.AccessRole > authorization.AccessRole {
		return errPageShareLeaveRaises
	}
	return nil
}
