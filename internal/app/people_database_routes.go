package app

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// People as its list reads it (PeopleView.tsx): the workspace's People
// database (people_database.go) brought up to its people first, its page and
// its own five properties named, whether the reader — its owner — changes it,
// and its graph: its views, properties and rows. A reader gets the rows of
// the people whose pages they open (people_page_reach.go) — a restricted
// member their own alone — and their values; its owner writes it as any
// database, by its page.

const (
	peopleDatabasePath = "/workspaces/{workspaceId}/people/database"
	// One answer reads at most this many graph windows; a People database
	// past them says so (hasMore, nextCursor) and People reads on from there
	// (lib/peopleDatabase.ts) — never a part answered as the whole.
	peopleDatabaseReadPages = 64
)

// registerPeopleDatabaseRoutes starts, with the server, the making of the
// People database of each workspace whose People kept something before it
// was one (people_database_moves.go) and of each that has none yet
// (people_database_every_workspace.go), and answers People's read.
func registerPeopleDatabaseRoutes(event *core.ServeEvent) {
	movePeopleDatabasesSoon(event.App)
	makePeopleDatabasesSoon(event.App)
	event.Router.GET(apiRoot+peopleDatabasePath, readPeopleDatabase)
}

func readPeopleDatabase(event *core.RequestEvent) error {
	authorization, _, ok, err := personProfileReader(event)
	if !ok {
		return err
	}
	cursor, err := decodeGraphCursor(event.Request.URL.Query().Get("cursor"))
	if err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeGraphCursorInvalid, "Check the content query position.")
	}
	// People shows as it is kept when bringing it up to its people fails; the
	// next change tries again. A read going on from a cursor reads it as
	// kept: its first answer brought it up.
	var database peopleDatabase
	if cursor == "" {
		if database, err = syncPeopleDatabase(event.App, authorization.WorkspaceID); err != nil {
			event.App.Logger().Warn("leafen people database waits", "workspace", authorization.WorkspaceID, "error", err)
		}
	}
	if cursor != "" || err != nil {
		found, has, loadErr := loadPeopleDatabase(event.App, authorization.WorkspaceID)
		if loadErr != nil || !has {
			if err = errors.Join(err, loadErr); err == nil {
				err = errPersonProfileNotFound
			}
			return personProfileError(event, err, "database")
		}
		database = found
	}
	// A restricted member sees their own row alone, and a member not the people whose page was
	// lowered past them (people_page_reach.go).
	reach, err := loadPeoplePageReach(event.App, authorization)
	if err != nil {
		return personProfileError(event, err, "database-reach")
	}
	// The other databases its relations reach, those the reader reads, come with it, as a page's read gives them.
	place := pageAuthorization{Workspace: authorization, AccessRole: pageAccessRoleView}
	linked, err := authorizeLinkedDatabaseReadScope(event.App, authorization.PrincipalID, place, database.PageID)
	if err != nil {
		return personProfileError(event, err, "database-linked")
	}
	items, nextCursor := []graphReadItem{}, ""
	for page := 0; page < peopleDatabaseReadPages; page++ {
		window, err := loadContentGraphWindowParts(event.App, authorization.OwnerID, database.PageID, linked, cursor, maxGraphReadItems, true)
		if err != nil {
			return personProfileError(event, err, "database-graph")
		}
		for _, item := range window.Items {
			source, _ := item.Value["dataSourceId"].(string)
			if id, _ := item.Value["id"].(string); item.Kind == "row" && source == database.DataSourceID && !reach.opensRow(id) {
				continue
			}
			items = append(items, item)
		}
		nextCursor = ""
		if !window.HasMore || window.NextCursor == nil {
			break
		}
		// A window's cursor is as the route gives it; the next window reads it decoded.
		nextCursor = *window.NextCursor
		if cursor, err = decodeGraphCursor(nextCursor); err != nil {
			return personProfileError(event, err, "database-graph")
		}
	}
	// What the databases' property access rules keep from this reader, and
	// what names a page closed to them, as a page's read leaves out.
	if items, err = redactGraphItemsForReader(event.App, authorization.PrincipalID, items); err != nil {
		return personProfileError(event, err, "database-redact")
	}
	items = graphItemsAsRead(event.App, authorization.PrincipalID, items)
	if err := markUntitledRowNames(event.App, items); err != nil {
		return personProfileError(event, err, "database-names")
	}
	if err := markPeopleDatabases(event.App, authorization, items); err != nil {
		return personProfileError(event, err, "database-rules")
	}
	// A restricted member doesn't open People's page itself, as Notion's
	// restricted member gets no access to People (seen 2026-11-16), so its
	// bar shows them none of the page's actions (no updatedAt).
	bar := peopleDatabaseBar{}
	if !authorization.Restricted {
		if bar, err = readPeopleDatabaseBar(event.App, authorization.PrincipalID, database.PageID); err != nil {
			return personProfileError(event, err, "database-bar")
		}
	}
	return event.JSON(http.StatusOK, map[string]any{
		"updatedAt": bar.UpdatedAt, "lastEditedBy": bar.LastEditedBy, "favorite": bar.Favorite, "favoriteRevision": bar.FavoriteRevision,
		"pageId": database.PageID, "dataSourceId": database.DataSourceID,
		"propertyIds": map[string]string{
			"name": database.NamePropertyID, "email": database.EmailPropertyID, "about": database.AboutPropertyID,
			"membership": database.MembershipPropertyID, "person": database.PersonPropertyID,
		},
		"editable": authorization.Role == workspaceRoleOwner, "items": items,
		"hasMore": nextCursor != "", "nextCursor": nextCursor,
	})
}

// peopleDatabaseBar is what People's bar shows of its page, as Notion's
// People page bar does (seen 2026-11-16 in Chrome at 1440, in two tabs at
// two times: "N시간 전 편집 · 공유 · 링크 · ☆ · ⋯"): when the page was
// last edited and by whom — its "⋯" ends with them, as Notion's People
// menu ends "최종 편집 일시 · 〈사람〉 · 〈시각〉" (seen 2026-11-16 in two
// tabs: the owner who last changed it) — and whether the reader made it a
// favorite, with the favorite's revision a change of it names
// (page_favorite.go).
type peopleDatabaseBar struct {
	UpdatedAt        string `db:"updated"`
	LastEditedBy     string `db:"last_edited_by"`
	Favorite         bool   `db:"favorite"`
	FavoriteRevision int    `db:"revision"`
}

func readPeopleDatabaseBar(app core.App, principalID, pageID string) (peopleDatabaseBar, error) {
	bar := peopleDatabaseBar{}
	err := app.DB().NewQuery(`SELECT [[page]].[[updated]], [[page]].[[last_edited_by]],
			COALESCE([[favorite_state]].[[favorite]], 0) AS [[favorite]], COALESCE([[favorite_state]].[[revision]], 0) AS [[revision]]
		FROM {{leafen_pages}} [[page]]
		LEFT JOIN {{leafen_page_favorites}} [[favorite_state]]
			ON [[favorite_state]].[[page_id]] = [[page]].[[id]] AND [[favorite_state]].[[principal_id]] = {:principalId}
		WHERE [[page]].[[id]] = {:pageId}`).
		Bind(dbx.Params{"principalId": principalID, "pageId": pageID}).One(&bar)
	if errors.Is(err, sql.ErrNoRows) {
		return bar, nil
	}
	return bar, err
}
