package app

import (
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

type notionPageAuthorization struct {
	Identity notionOAuthBearerIdentity
	Page     pageAuthorization
}

type notionWorkspaceOperationAuthorization struct {
	Identity  notionOAuthBearerIdentity
	Workspace workspaceAuthorization
}

func authorizeNotionPage(
	event *core.RequestEvent,
	publicPageID string,
	requiredScope string,
	requirement permissionRequirement,
) (notionPageAuthorization, bool, error) {
	noteNotionAskedObject(event, "page", publicPageID)
	workspace, ok, err := authorizeNotionWorkspaceOperation(event, requiredScope, requirement)
	if err != nil || !ok {
		return notionPageAuthorization{}, false, err
	}
	identity := workspace.Identity
	internalPageID, err := resolveNotionPageRecordID(event.App, publicPageID)
	if err != nil {
		return notionPageAuthorization{}, false, notionPageNotFound(event)
	}
	page, err := authorizePage(event.App, identity.PrincipalID, internalPageID, requirement)
	if isPermissionDenied(err) || err == nil && page.Workspace.WorkspaceID != identity.WorkspaceID {
		return notionPageAuthorization{}, false, notionPageNotFound(event)
	}
	if err != nil {
		return notionPageAuthorization{}, false, notionRequestNotCompleted(event, err)
	}
	if !notionOAuthIdentityAllowsPage(event.App, identity, page) {
		return notionPageAuthorization{}, false, notionPageNotFound(event)
	}
	return notionPageAuthorization{Identity: identity, Page: page}, true, nil
}

func authorizeNotionWorkspaceOperation(
	event *core.RequestEvent,
	requiredScope string,
	requirement permissionRequirement,
) (notionWorkspaceOperationAuthorization, bool, error) {
	identity, ok, err := notionPublicAPIBearerFromRequest(event)
	if err != nil {
		return notionWorkspaceOperationAuthorization{}, false, notionRequestNotCompleted(event, err)
	}
	if !ok {
		return notionWorkspaceOperationAuthorization{}, false, notionUnauthorized(event)
	}
	if valid, err := validateNotionUserDirectoryVersion(event); !valid {
		return notionWorkspaceOperationAuthorization{}, false, err
	}
	if !notionOAuthHasScope(identity, requiredScope) {
		return notionWorkspaceOperationAuthorization{}, false, notionCompatibilityError(
			event, http.StatusForbidden, notionErrorRestrictedResource,
			notionInsufficientPermissions,
		)
	}
	workspace, err := authorizeWorkspace(event.App, identity.PrincipalID, identity.WorkspaceID, requirement)
	if isPermissionDenied(err) {
		return notionWorkspaceOperationAuthorization{}, false, notionUnauthorized(event)
	}
	if err != nil {
		return notionWorkspaceOperationAuthorization{}, false, notionRequestNotCompleted(event, err)
	}
	return notionWorkspaceOperationAuthorization{Identity: identity, Workspace: workspace}, true, nil
}

// notionPublicBearerRequestKey keeps a request's resolved connection, so the
// connection's window and its handler read the token once.
const notionPublicBearerRequestKey = "leafenNotionPublicBearer"

type notionPublicBearer struct {
	identity notionOAuthBearerIdentity
	ok       bool
	err      error
}

func notionPublicAPIBearerFromRequest(
	event *core.RequestEvent,
) (notionOAuthBearerIdentity, bool, error) {
	if resolved, found := event.Get(notionPublicBearerRequestKey).(notionPublicBearer); found {
		return resolved.identity, resolved.ok, resolved.err
	}
	rawToken := bearerToken(event.Request)
	identity, ok, err := resolveNotionStaticTokenBearer(event, rawToken)
	if err == nil && !ok {
		identity, ok, err = notionOAuthBearerFromRequest(event)
	}
	if err == nil && ok {
		identity.Origin = requestOrigin(event.Request)
		noteNotionCredentialReach(event.App, event.Request, identity)
		// What a connection writes in this request is its bot's.
		event.Request = event.Request.WithContext(withConnectionAuthor(event.Request.Context(), identity))
	}
	event.Set(notionPublicBearerRequestKey, notionPublicBearer{identity: identity, ok: ok, err: err})
	return identity, ok, err
}

func notionOAuthIdentityAllowsPage(
	app core.App,
	identity notionOAuthBearerIdentity,
	page pageAuthorization,
) bool {
	// A connection whose person chose no pages reaches none, as in Notion
	// (2026-10-13: it reached every page its person could).
	if !identity.PageScoped {
		return true
	}
	// Up to the top however deep, each page once, or to a page whose sharing
	// is its own, which holds its own copy of the connections that reached it,
	// as Notion's bot permissions (page_notion_connections_unlink.go).
	current := page.Page
	for seen := map[string]bool{}; !seen[current.Id]; {
		seen[current.Id] = true
		if kind, selected := identity.ResourceIDs[current.Id]; selected && (kind == "page" || kind == "database") {
			return true
		}
		if unlinked, err := pageShareUnlinked(app, current.Id); err != nil || unlinked {
			return false
		}
		// A database page is reached through its data source, then the page
		// holding its database.
		if isDatabaseRowPageRecord(current) {
			if kind, selected := identity.ResourceIDs[current.GetString("data_source_id")]; selected && kind == "database" {
				return true
			}
		}
		parentID, err := pageAncestryParentID(app, current)
		if err != nil || parentID == "" {
			return false
		}
		parent, err := app.FindRecordById(pagesCollection, parentID)
		if err != nil || parent.GetString("workspace") != identity.WorkspaceID {
			return false
		}
		current = parent
	}
	return false
}

func authorizeNotionPageMutation(
	identity notionOAuthBearerIdentity,
	pageID string,
) func(core.App) (pageAuthorization, error) {
	return func(app core.App) (pageAuthorization, error) {
		current, err := authorizePageBody(app, identity.PrincipalID, pageID, permissionWorkspaceWrite)
		if err != nil {
			if isPermissionDenied(err) {
				return pageAuthorization{}, graphNotFoundError()
			}
			return pageAuthorization{}, err
		}
		if current.Workspace.WorkspaceID != identity.WorkspaceID ||
			!notionOAuthIdentityAllowsPage(app, identity, current) {
			return pageAuthorization{}, graphNotFoundError()
		}
		if trashed, err := pageInTrashPath(app, pageID); err != nil {
			return pageAuthorization{}, err
		} else if trashed {
			return pageAuthorization{}, graphNotFoundError()
		}
		return current, nil
	}
}

func notionPageNotFound(event *core.RequestEvent) error {
	return notionCompatibilityError(
		event, http.StatusNotFound, notionErrorObjectNotFound,
		"There's no page with the requested ID.",
	)
}
