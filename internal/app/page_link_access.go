package app

import (
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// The role a web link gives the organization's people who open it signed in
// (schema_baseline.json). Opening the link with comment or edit
// access records them with it; while the link is on, unexpired and still
// publishes a page, its role is theirs on that page, read with the page's
// shares. Turning the link off or making a new one forgets them, and a role
// changed on the link changes theirs. Anyone else reads the page through
// the link as before.

var pageLinkRoles = map[string]bool{"view": true, "comment": true, "edit": true}

func registerPageLinkAccessRoutes(event *core.ServeEvent) {
	event.Router.POST(apiRoot+publicMutationShareLinkJoinPath, joinPageShareLink).
		Bind(apis.BodyLimit(publicMutationMaxBodyBytes))
}

// joinPageShareLink records a signed-in person of the organization as come
// in through a web link, and names the page to open in the app with the
// link's role, as Notion opens a public page in the app for someone signed
// in (seen 2026-11-06: a guest of the workspace without access reads it
// there).
func joinPageShareLink(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := publicMutationShareLinkJoinInput{}
	if err := event.BindBody(&input); err != nil {
		return publicMutationRouteError(event, bodyRefusal(errPublicMutationInvalid, "link join", err), "share-link-join-input")
	}
	if strings.TrimSpace(input.Token) == "" || len(input.Token) > 512 {
		return publicMutationRouteError(event, publicMutationRefused("token is the shared link's token, up to 512 bytes"), "share-link-join-input")
	}
	if len(input.PageID) > 255 {
		return publicMutationRouteError(event, publicMutationRefused("pageId is %d bytes, over 255", len(input.PageID)), "share-link-join-input")
	}
	now := time.Now().UTC()
	pageIDs := []string{}
	if input.PageID != "" {
		pageIDs = append(pageIDs, input.PageID)
	}
	authorization, err := authorizePublicSharePages(event.App, strings.TrimSpace(input.Token), pageIDs, now)
	if err != nil {
		return publicMutationRouteError(event, errPublicMutationNotFound, "share-link-join-link")
	}
	var role string
	if err := event.App.DB().NewQuery(`SELECT [[role]] FROM {{leafen_public_share_links}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": authorization.AuthorityID}).Row(&role); err != nil {
		return publicMutationRouteError(event, err, "share-link-join-role")
	}
	if !pageLinkRoles[role] {
		return publicMutationRouteError(event, publicMutationRefused("the link gives no role to join with"), "share-link-join-role")
	}
	// Only the organization's people, members or guests, work in it.
	if _, err := authorizePageGrantWorkspace(event.App, user.Id, authorization.Workspace); err != nil {
		return apiError(event, http.StatusForbidden, errorCodePublicMutationDenied,
			"Only people of the page's organization work in it through its link.")
	}
	if _, err := event.App.DB().NewQuery(`
		INSERT INTO {{leafen_page_link_visitors}} ([[link_id]],[[workspace_id]],[[user_id]],[[visited_at]])
		VALUES ({:linkId},{:workspaceId},{:userId},{:now})
		ON CONFLICT ([[link_id]],[[user_id]]) DO UPDATE SET [[visited_at]]=excluded.[[visited_at]]
	`).Bind(dbx.Params{
		"linkId": authorization.AuthorityID, "workspaceId": authorization.WorkspaceID, "userId": user.Id, "now": now.UnixMilli(),
	}).Execute(); err != nil {
		return publicMutationRouteError(event, err, "share-link-join")
	}
	pageID := input.PageID
	if pageID == "" {
		pageID = authorization.RootPageID
	}
	return event.JSON(http.StatusOK, publicMutationShareLinkJoin{PageID: pageID, Role: role})
}

// pageLinkAccessRole is the widest role the links a person came in through
// give them on a page: every such link is looked at, those giving edit
// first, a window at a time, until one reaches the page with edit.
func pageLinkAccessRole(app core.App, workspaceID, pageID, principalID string) (pageAccessRole, error) {
	now := time.Now().UTC()
	role := pageAccessRoleNone
	afterRole, afterID := "", ""
	for {
		// A person's links are the same for every page a read judges: each
		// window is read once a read scope.
		links, err := remembered(app, pageLinkWindowMemoKey{workspaceID, principalID, afterRole, afterID}, func() ([]pageLinkAccessLink, error) {
			links := []pageLinkAccessLink{}
			err := app.DB().NewQuery(`
			SELECT [[link]].[[id]] AS [[link_id]], '' AS [[site_id]], [[link]].[[workspace_id]], [[link]].[[root_page_id]],
				[[link]].[[source_revision]], [[link]].[[expires_at]], [[link]].[[role]]
			FROM {{leafen_page_link_visitors}} [[visitor]]
			JOIN {{leafen_public_share_links}} [[link]] ON [[link]].[[id]]=[[visitor]].[[link_id]]
			WHERE [[visitor]].[[workspace_id]]={:workspaceId} AND [[visitor]].[[user_id]]={:userId}
				AND [[link]].[[web_enabled]]=1 AND [[link]].[[role]] IN ('view','comment','edit')
				AND ({:afterId} = '' OR ([[link]].[[role]] = {:afterRole} AND [[link]].[[id]] > {:afterId})
					OR ([[link]].[[role]] < {:afterRole}))
			ORDER BY [[link]].[[role]] DESC, [[link]].[[id]]
			LIMIT {:limit}
		`).Bind(dbx.Params{"workspaceId": workspaceID, "userId": principalID, "afterRole": afterRole, "afterId": afterID,
				"limit": pageLinkAccessWindow}).All(&links)
			return links, err
		})
		if err != nil {
			return pageAccessRoleNone, err
		}
		for _, link := range links {
			afterRole, afterID = link.Role, link.LinkID
			linkRole := pageAccessRoleView
			switch link.Role {
			case "edit":
				linkRole = pageAccessRoleEdit
			case "comment":
				linkRole = pageAccessRoleComment
			}
			if linkRole <= role || publicVisibilityExpired(link.ExpiresAt, now) {
				continue
			}
			if _, err := authorizePublicVisibilityPages(app, link.storedPublicShareAuthority, []string{pageID}); err == nil {
				role = linkRole
			} else if err != errPublicVisibilityNotFound {
				return pageAccessRoleNone, err
			}
			if role == pageAccessRoleEdit {
				return role, nil
			}
		}
		if len(links) < pageLinkAccessWindow {
			return role, nil
		}
	}
}

type pageLinkWindowMemoKey struct{ workspaceID, userID, afterRole, afterID string }

type pageLinkAccessLink struct {
	storedPublicShareAuthority
	Role string `db:"role"`
}

// pageLinkAccessWindow is how many of a person's links one read looks at.
const pageLinkAccessWindow = 32

// forgetPageLinkVisitors forgets who came in through a link, as it goes off
// or is made anew.
func forgetPageLinkVisitors(app core.App, linkID string) error {
	_, err := app.DB().NewQuery(`DELETE FROM {{leafen_page_link_visitors}} WHERE [[link_id]]={:id}`).
		Bind(dbx.Params{"id": linkID}).Execute()
	return err
}
