package app

import (
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A person's favorite database pages — a database's rows and templates,
// which the sidebar's page list doesn't carry — come with the first batch's
// favorites, as Notion lists them among the sidebar's and the library's
// favorites (seen 2026-10-30); what each shows is read as the library reads
// rows it doesn't hold. So does People's own database page, which is in no
// one's page list (people_database.go) and which Notion's library lists
// among the favorites of whoever stars it (seen 2026-11-16). Only pages the
// person can still read are listed.

const maxBootstrapDatabasePageFavorites = 200

type storedDatabasePageFavorite struct {
	PageID   string `db:"page_id"`
	Revision int    `db:"revision"`
}

func bootstrapDatabasePageFavorites(app core.App, principalID string) ([]pageFavoriteProjection, error) {
	return databasePageFavoritesWhere(app, principalID, "1", dbx.Params{})
}

// changedDatabasePageFavorites are those of pages changed since a list
// (bootstrap_page_changes.go): a row's page the person made a favorite, or
// one renamed elsewhere, comes with its favorite as the first batch has it,
// not among the pages the person may no longer see, which would take it out
// of the favorites until the list was read whole again (seen 2026-11-16 in
// a workspace whose pages kept changing).
func changedDatabasePageFavorites(app core.App, principalID string, ids []string) ([]pageFavoriteProjection, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	return databasePageFavoritesWhere(app, principalID, `[[page]].[[id]] IN (SELECT [[value]] FROM json_each({:ids}))`,
		dbx.Params{"ids": string(encoded)})
}

// databasePageFavoritesWhere are the person's favorite database pages where
// an SQL condition on [[page]] holds.
func databasePageFavoritesWhere(app core.App, principalID, where string, params dbx.Params) ([]pageFavoriteProjection, error) {
	params["principalId"], params["limit"] = principalID, maxBootstrapDatabasePageFavorites
	rows := []storedDatabasePageFavorite{}
	if err := app.DB().NewQuery(`
		SELECT [[favorite_state]].[[page_id]], [[favorite_state]].[[revision]]
		FROM {{leafen_page_favorites}} [[favorite_state]]
		JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[favorite_state]].[[page_id]]
		WHERE [[favorite_state]].[[principal_id]] = {:principalId} AND [[favorite_state]].[[favorite]] = 1
			AND [[page]].[[in_trash]] = FALSE AND (NOT ` + bootstrapSidebarKindSQL("[[page]]") + `
				OR [[page]].[[id]] IN (SELECT [[page_id]] FROM {{leafen_workspace_people_databases}})) AND (` + where + `)
		ORDER BY [[favorite_state]].[[updated]], [[favorite_state]].[[page_id]]
		LIMIT {:limit}
	`).Bind(params).All(&rows); err != nil {
		return nil, err
	}
	favorites := []pageFavoriteProjection{}
	for _, row := range rows {
		reads, err := databasePageFavoriteReads(app, principalID, row.PageID)
		if err != nil {
			return nil, err
		}
		if reads {
			favorites = append(favorites, pageFavoriteProjection{PageID: row.PageID, Favorite: true, Revision: row.Revision})
		}
	}
	return favorites, nil
}

// databasePageFavoriteReads is whether the person still reads a favorite
// database page: People's own by People's rule (a restricted member reads
// none of it), a row or template by its database's.
func databasePageFavoriteReads(app core.App, principalID, pageID string) (bool, error) {
	if page, err := app.FindRecordById(pagesCollection, pageID); err == nil && !isDatabaseRowPageRecord(page) {
		if _, _, people, err := peopleDatabasePageOf(app, page); err != nil || people {
			if err != nil {
				return false, err
			}
			_, err := authorizePage(app, principalID, pageID, permissionWorkspaceRead)
			if isPermissionDenied(err) {
				return false, nil
			}
			return err == nil, err
		}
	}
	// One in the trash, a template's included, is out of favorites, as the library reads it.
	authority, err := authorizeDatabaseRowOrTemplatePage(app, principalID, pageID, permissionWorkspaceRead)
	if isPermissionDenied(err) || errors.Is(err, errDatabaseRowPageNotFound) {
		return false, nil
	}
	return err == nil && authority.TrashedAt == 0, err
}
