package app

import (
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// pageParentRefusal is why the place a page change takes a page to refuses
// it: what to fix, and whether it is a state that passes (busy — a Trash
// being emptied, a wiki being made) or a move the mover may not make
// (denied). Leafen's route answers each 409 with its reason; Notion's API
// answers a busy place as a conflict, a denied one as a restricted
// resource and the rest as a validation error.
type pageParentRefusal struct {
	reason string
	busy   bool
	denied bool
	// crossing is the refusal of a new parent in another workspace, which a
	// route answers as itself (other_workspace_refusal.go).
	crossing error
}

// pageMetadataPlaceRefusal is where a page change puts the page, or why its
// new place refuses it: the change and its explanation read the same checks.
func pageMetadataPlaceRefusal(
	app core.App, subject pageAccessSubject, authorization pageAuthorization, input pageMetadataInput, parentRequested bool, lock pageLockMode,
) (pagePlace, *pageParentRefusal, error) {
	page, actorID := authorization.Page, subject.actorID()
	if input.InTrash != page.GetBool("in_trash") || input.ParentID != page.GetString("parent_id") {
		fenced, err := pageTrashEmptySnapshotFenceActive(app, authorization.Workspace.WorkspaceID)
		if err != nil || fenced {
			return pagePlace{}, refusedPlace(fenced, "Trash is being emptied: try again once it's done", true, false), err
		}
	}
	if refusal, err := pageMetadataGuestRefusal(app, actorID, authorization, input); err != nil || refusal != nil {
		return pagePlace{}, refusal, err
	}
	var destinationPage *core.Record
	// A page goes to its new parent's place, or at the top to the teamspace
	// or the private pages named; a page moved to the top of private pages is
	// the mover's.
	destination := pagePlaceOf(page)
	if input.ParentID == "" && input.TeamspaceID != nil {
		destination = pagePlace{TeamspaceID: *input.TeamspaceID}
		if destination.TeamspaceID == "" {
			destination.PrivateOwnerID = actorID
		}
	}
	if input.ParentID != "" {
		parent, err := subject.authorizePage(app, input.ParentID, permissionWorkspaceRead)
		if err == nil && parent.Workspace.WorkspaceID != authorization.Workspace.WorkspaceID {
			return pagePlace{}, pageParentCrossing(app, page.Id, authorization.Workspace.WorkspaceID, input.ParentID, parent.Workspace.WorkspaceID), nil
		}
		if isPermissionDenied(err) {
			return pagePlace{}, refusedPlace(true, "the new parent isn't a page you can read in this workspace", false, false), nil
		}
		if err != nil {
			return pagePlace{}, nil, err
		}
		destinationPage = parent.Page
		destination = pagePlaceOf(destinationPage)
		switch {
		case input.TeamspaceID != nil && *input.TeamspaceID != destination.TeamspaceID:
			return pagePlace{}, refusedPlace(true, "teamspaceId isn't the new parent's teamspace", false, false), nil
		// A full-page database holds only its database.
		case input.ParentID != page.GetString("parent_id") && isFullPageDatabase(destinationPage):
			return pagePlace{}, refusedPlace(true,
				"the new parent is a full-page database, which holds only its database: add the page to it as a row", false, false), nil
		}
		fenced, err := wikiMutationFenceActive(app, input.ParentID)
		if err != nil || fenced {
			return pagePlace{}, refusedPlace(fenced, "the wiki the new parent is in is being made or undone: try again once it's done", true, false), err
		}
		cycle, err := pageParentCreatesCycle(app, page.Id, input.ParentID)
		if err != nil || cycle {
			return pagePlace{}, refusedPlace(cycle, "the new parent is the page itself or a page inside it", false, false), err
		}
	}
	placeChanges := destination != pagePlaceOf(page)
	// A page given another parent takes on that parent's sharing, so moving it
	// asks its full access and leave to add pages to the new parent, as
	// Notion's move does; only its order among
	// the same parent's pages is an edit.
	if placeChanges || parentRequested && input.ParentID != page.GetString("parent_id") {
		allowed, err := authorizePagePlaceMove(app, actorID, authorization, input.ParentID, destination)
		if err != nil || !allowed {
			return pagePlace{}, refusedPlace(err == nil,
				"moving a page there takes full access to it and leave to add pages to its new place", false, true), err
		}
	}
	movementRequested := input.Position != nil || input.ParentID != page.GetString("parent_id") || placeChanges
	if movementRequested && lock == pageLockEnforced {
		locked, err := pageMoveLockConflict(app, authorization, destinationPage)
		if err != nil || locked {
			return pagePlace{}, refusedPlace(locked, "the page it leaves or the page it goes into is locked: unlock it first", false, false), err
		}
	}
	return destination, nil, nil
}

// refusedPlace is a place's refusal when it holds.
func refusedPlace(holds bool, reason string, busy, denied bool) *pageParentRefusal {
	if !holds {
		return nil
	}
	return &pageParentRefusal{reason: reason, busy: busy, denied: denied}
}

// pageParentConflictReason says why a page change was refused its place,
// read again as the change read it; nil once the place no longer refuses
// it, as a Trash emptied since.
func pageParentConflictReason(app core.App, actorID, pageID string, input pageMetadataInput, lock pageLockMode) *pageParentRefusal {
	authorization, err := authorizePage(app, actorID, pageID, permissionWorkspaceWrite)
	if err != nil {
		return nil
	}
	parentRequested := input.ParentID != authorization.Page.GetString("parent_id")
	if input, err = placePageMetadataInputAgainstTrash(app, authorization.Page, input); err != nil {
		return nil
	}
	_, refusal, err := pageMetadataPlaceRefusal(app, personAccessSubject(actorID), authorization, input, parentRequested, lock)
	if err != nil {
		return nil
	}
	return refusal
}

// pageParentCrossing is the refusal of a new parent in another workspace.
func pageParentCrossing(app core.App, pageID, from, parentID, to string) *pageParentRefusal {
	crossing := workspaceCrossing{subject: "this page", from: from, place: "its new parent " + parentID, to: to, whole: pageID}
	return &pageParentRefusal{reason: crossing.words(app), crossing: otherWorkspaceRefused(app, "parentId", crossing)}
}

// answerPageParentRefused answers a page change its place refused: 409 with
// the place's reason, a new parent in another workspace as such.
func answerPageParentRefused(event *core.RequestEvent, actorID, pageID string, input pageMetadataInput) error {
	refusal := pageParentConflictReason(event.App, actorID, pageID, input, pageLockEnforced)
	if refusal != nil && refusal.crossing != nil {
		return apiErrorFrom(event, http.StatusConflict, errorCodePageMetadataConflict, "The page location can't be changed.", refusal.crossing)
	}
	return apiError(event, http.StatusConflict, errorCodePageMetadataConflict,
		pageParentRefusalMessage("The page location can't be changed.", refusal))
}

// pageParentRefusalMessage is a route's words with the place's reason.
func pageParentRefusalMessage(message string, refusal *pageParentRefusal) string {
	if refusal == nil {
		return message
	}
	return refusalMessage(message, refusal.reason)
}

// notionPageParentRefused answers a page change its place refused as
// Notion's API answers such a change: a place that is busy as a conflict
// to retry, a move the connection may not make as a restricted resource,
// and the rest as a validation error, each with the place's reason.
func notionPageParentRefused(event *core.RequestEvent, message string, refusal *pageParentRefusal) error {
	switch {
	case refusal == nil || refusal.busy:
		return notionCompatibilityError(event, http.StatusConflict, "conflict_error", pageParentRefusalMessage(message, refusal))
	case refusal.denied:
		return notionCompatibilityError(event, http.StatusForbidden, notionErrorRestrictedResource, pageParentRefusalMessage(message, refusal))
	}
	return notionPageValidationError(event, pageParentRefusalMessage(message, refusal))
}
