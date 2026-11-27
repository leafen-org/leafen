package app

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The pages directly under a page, or at a workspace's top, that the person
// sees — as their sidebar lists them, shared ones too, and leaving out what
// it leaves out (sidebarTreeLeftOut): archived pages, a button's template
// pages, pages hanging on a table row's cells or a caption, and pages
// hanging where their mention is no longer — for an app or agent that walks
// a workspace as a tree, such as a file server showing pages as folders.
// Whether each has pages under it is read a window at a time
// (page_children_window.go). The list itself comes a window at a time too,
// with nextCursor while more follow (page_children_cursor.go): an answer
// that said only more left a Drive folder's pages past the first thousand
// out of Finder and an Obsidian vault's move (2026-11-24).

const (
	pageChildrenPath     = "/workspaces/{workspaceId}/page-children"
	pageChildrenMax      = 1000
	pageChildrenFallback = 200
)

type pageChildWire struct {
	ID          string `json:"id" db:"id"`
	Title       string `json:"title" db:"title"`
	Icon        string `json:"icon" db:"icon"`
	IconType    string `json:"iconType" db:"icon_type"`
	Kind        string `json:"kind" db:"kind"`
	Created     string `json:"created" db:"created"`
	Updated     string `json:"updated" db:"updated"`
	HasChildren bool   `json:"hasChildren" db:"-"`
	// Position is its place among its siblings, where the next window goes
	// on from (pageChildrenCursor).
	Position float64 `json:"-" db:"position"`
	// ChildIDs are the first pages right under it the query reads
	// (pageChildWindow), as a JSON list, before the sidebar's rule leaves
	// some out (keepListedPageChildren).
	ChildIDs string `json:"-" db:"child_ids"`
	// Shared says it came in with the pages the person opens only through
	// shares, whose pages under it are read among those (loadSharedPageChildren).
	Shared bool `json:"-" db:"-"`
}

type pageChildrenWire struct {
	Pages []pageChildWire `json:"pages"`
	// More says the list stopped at the limit, and NextCursor, then given,
	// is where ?cursor= goes on: a window may hold fewer pages than the
	// limit, or none, when the sidebar's rule leaves some out.
	More       bool   `json:"more"`
	NextCursor string `json:"nextCursor,omitempty"`
}

func registerPageChildrenRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+pageChildrenPath, readPageChildrenRoute)
}

func readPageChildrenRoute(event *core.RequestEvent) error {
	event.Response.Header().Set("Cache-Control", "no-store")
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	parentID := strings.TrimSpace(event.Request.URL.Query().Get("parent"))
	limit := pageChildrenFallback
	if raw := event.Request.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > pageChildrenMax {
			return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Ask for 1 to 1000 pages.")
		}
		limit = parsed
	}
	// ?cursor= goes on past where an answer's nextCursor says it stopped.
	var after pageChildrenCursor
	if raw := strings.TrimSpace(event.Request.URL.Query().Get("cursor")); raw != "" {
		var known bool
		if after, known = readPageChildrenCursor(raw); !known {
			return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Start the list again.")
		}
	}
	if parentID != "" {
		authorization, err := authorizePage(event.App, user.Id, parentID, permissionWorkspaceRead)
		if err != nil || authorization.Page.GetString("workspace") != workspaceID {
			return apiError(event, http.StatusNotFound, errorCodePageNotFound, "Page not found.")
		}
	}
	// The person's workspaces as they now stand first (bootstrap_page_access.go).
	if ready, err := ensureBootstrapVisibilityProjectionReadiness(event, user.Id, errorCodePageCreateFailed, "Couldn't list the pages.",
		"page-children"); !ready {
		return err
	}
	children, next, err := loadPageChildren(event.App, user.Id, workspaceID, parentID, after, limit)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodePageCreateFailed, "Couldn't list the pages.", err)
	}
	// Each titled as its reader reads it (page_name_mentions.go), for an
	// app: a mention closed to them "Untitled".
	titles := make(map[string]*string, len(children))
	for index := range children {
		titles[children[index].ID] = &children[index].Title
	}
	pageTitlesAsRead(event.App, user.Id, titles, false)
	return event.JSON(http.StatusOK, pageChildrenWire{Pages: children, More: next != "", NextCursor: next})
}

// loadPageChildren reads the person's visible pages under a parent, as the
// sidebar lists them (bootstrap_page_access.go, keepListedPageChildren),
// with the bootstrap's own membership rules: up to limit read past after,
// and where the next window starts when more follow ("" when none do).
func loadPageChildren(app core.App, principalID, workspaceID, parentID string, after pageChildrenCursor, limit int) ([]pageChildWire, string, error) {
	rows := []pageChildWire{}
	if !after.Shared {
		own, err := loadOwnPageChildren(app, principalID, workspaceID, parentID, after, limit+1)
		if err != nil {
			return nil, "", err
		}
		rows = own
	}
	// Pages opened only through shares come after the person's own, as in
	// their sidebar: from their first when the window started among those.
	sharedAfter := pageChildrenCursor{}
	if after.Shared {
		sharedAfter = after
	}
	shared, err := loadSharedPageChildren(app, principalID, workspaceID, parentID, sharedAfter, limit+1-len(rows))
	if err != nil {
		return nil, "", err
	}
	rows = append(rows, shared...)
	// The next window starts past the last page read, kept or not, so
	// those the sidebar's rule leaves out aren't read again.
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = pageChildrenCursorAfter(rows[limit-1])
	}
	listed, err := keepListedPageChildren(app, principalID, workspaceID, rows)
	return listed, next, err
}

// loadOwnPageChildren reads up to limit of the pages under a parent in the
// person's own workspaces, in the sidebar's order, past after when it
// names one of them.
func loadOwnPageChildren(app core.App, principalID, workspaceID, parentID string, after pageChildrenCursor, limit int) ([]pageChildWire, error) {
	rows := []pageChildWire{}
	kept, err := pageChildKeptSQL(app, ownPageChildKeptSQL)
	if err != nil {
		return nil, err
	}
	params := dbx.Params{
		"principalId": principalID, "workspaceId": workspaceID, "parentId": parentID,
		"activeStatus": organizationMemberStatusActive, "limit": limit, "childWindow": pageChildWindow,
	}
	err = app.DB().NewQuery(`
		SELECT [[page]].[[id]], [[page]].[[title]], [[page]].[[icon]], [[page]].[[icon_type]], [[page]].[[kind]],
			[[page]].[[created]], [[page]].[[updated]], [[page]].[[position]],
			` + pageChildWindowListSQL("[[page]].[[id]]", kept) + ` AS [[child_ids]]
		FROM {{leafen_bootstrap_workspace_visibility}} [[visibility]]
		JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = [[visibility]].[[workspace_id]]
		JOIN {{leafen_organization_memberships}} [[organization_membership]]
			ON [[organization_membership]].[[id]] = [[visibility]].[[organization_membership_id]]
		JOIN {{leafen_pages}} [[page]] ON [[page]].[[workspace]] = [[visibility]].[[workspace_id]] AND [[page]].[[parent_id]] = {:parentId}
		WHERE [[visibility]].[[principal_id]] = {:principalId}
			AND [[visibility]].[[workspace_id]] = {:workspaceId}
			AND [[page]].[[in_trash]] = FALSE
			AND [[page]].[[archived_at]] = ''
			AND [[page]].[[notion_import_staging]] = FALSE
			AND [[page]].[[owner]] = [[workspace]].[[owner]]` + pageChildrenAfterSQL(after, params) + `
			AND ` + bootstrapPageListedSQL("[[page]]", "{:principalId}") + `
			AND ` + bootstrapPageCompleteSQL + `
			AND [[workspace]].[[deletion_job_id]] = ''
			AND [[organization_membership]].[[user]] = {:principalId}
			AND [[organization_membership]].[[organization]] = [[workspace]].[[organization]]
			AND [[organization_membership]].[[status]] = {:activeStatus}
			AND (([[workspace]].[[workspace_owner]] = {:principalId} AND [[visibility]].[[workspace_membership_id]] = '')
				OR ([[workspace]].[[workspace_owner]] != {:principalId}
					AND [[visibility]].[[workspace_membership_id]] = {:principalId}
					AND EXISTS (
						SELECT 1 FROM {{leafen_workspace_memberships}} [[workspace_membership]]
						WHERE [[workspace_membership]].[[workspace_id]] = [[workspace]].[[id]]
							AND [[workspace_membership]].[[user_id]] = {:principalId}
							AND [[workspace_membership]].[[role]] = 'member'
					)))
		ORDER BY [[page]].[[position]], [[page]].[[id]]
		LIMIT {:limit}
	`).Bind(params).All(&rows)
	return rows, err
}
