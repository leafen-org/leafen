package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// readablePageIDs keeps, of the pages, those the person may read now: a
// database item is read through the page that holds its database, or by
// its own sharing where that is unlinked from the database's
// (database_row_unlinked_access.go), a database held in a page's body and a
// database template through the page holding the database, and a page in
// the trash, or under a trashed page, is left out — a database item sent
// there itself or with its database too, as a link to it reads it
// (loadPageTrashEffects).
func readablePageIDs(app core.App, principalID string, pageIDs []string) (map[string]bool, error) {
	return pagesReadableBy(app, principalID, pageIDs, true)
}

// openablePageIDs keeps, of the pages, those the person may open now, as a
// link opens them: as readablePageIDs, but a page in the trash opens under
// the trash's banner, and a share search keeps out of its results still
// opens its page.
func openablePageIDs(app core.App, principalID string, pageIDs []string) (map[string]bool, error) {
	return pagesReadableBy(app, principalID, pageIDs, false)
}

func pagesReadableBy(app core.App, principalID string, pageIDs []string, forSearch bool) (map[string]bool, error) {
	readable := make(map[string]bool, len(pageIDs))
	// A database item is read through the page that holds its database.
	rows, err := loadWorkspaceSearchRowSources(app, pageIDs)
	if err != nil {
		return nil, err
	}
	unlinked, err := unlinkedPageIDs(app, pageIDs)
	if err != nil {
		return nil, err
	}
	// A database held in a page's body, and a template, is read through the
	// page holding the database (workspace_search_databases.go).
	entries, err := loadSearchDatabaseEntries(app, pageIDs)
	if err != nil {
		return nil, err
	}
	// Whether each is in the trash is asked of it by the one rule of what
	// the trash holds (pageTrashWalkSQL): a database item and a template by
	// their own page — sent there itself, with its database (its block alone
	// included) or under a page there — and a database held in a page's body,
	// which has no page, by its own trash and the page holding it's.
	trashThrough := make([]string, 0, len(pageIDs))
	authorizeThrough := make([]string, 0, len(pageIDs))
	databases := []string{}
	for _, pageID := range pageIDs {
		if entry, isEntry := entries[pageID]; isEntry {
			authorizeThrough = append(authorizeThrough, entry.PageID)
			if entry.Template {
				trashThrough = append(trashThrough, pageID)
			} else {
				trashThrough = append(trashThrough, entry.PageID)
				databases = append(databases, pageID)
			}
		} else if row, isRow := rows[pageID]; isRow {
			trashThrough = append(trashThrough, pageID)
			if unlinked[pageID] {
				authorizeThrough = append(authorizeThrough, pageID)
			} else {
				authorizeThrough = append(authorizeThrough, row.ContainerPageID)
			}
		} else {
			trashThrough = append(trashThrough, pageID)
			authorizeThrough = append(authorizeThrough, pageID)
		}
	}
	trashed, err := loadPageTrashEffects(app, trashThrough)
	if err != nil {
		return nil, err
	}
	trashedDatabases, err := trashedDatabaseContainerIDs(app, databases)
	if err != nil {
		return nil, err
	}
	// The records of the pages judged, read at once.
	if err := primeRecordsRemembered(app, pagesCollection, authorizeThrough); err != nil {
		return nil, err
	}
	if err := primePageShareRoles(app, principalID, authorizeThrough); err != nil {
		return nil, err
	}
	hiddenGrants, err := loadWorkspaceSearchHiddenGrants(app, authorizeThrough)
	if err != nil {
		return nil, err
	}
	// Each page an item is read through is judged once: a database's items
	// share it.
	judged := map[string]bool{}
	for index, pageID := range pageIDs {
		if _, inTrash := trashed[trashThrough[index]]; (inTrash || trashedDatabases[pageID]) && forSearch {
			continue
		}
		target := authorizeThrough[index]
		allowed, known := judged[target]
		if !known {
			// A page an operation holds a while is still found (page_access_hold.go).
			_, err := pageAccessDespiteHold(authorizePageAccessRole(app, principalID, target, pageAccessRoleView))
			if err != nil && !isPermissionDenied(err) {
				return nil, err
			}
			allowed = err == nil
			if allowed && forSearch {
				kept, err := hiddenGrants.keepsFromSearch(app, principalID, target)
				if err != nil {
					return nil, err
				}
				allowed = !kept
			}
			judged[target] = allowed
		}
		if allowed {
			readable[pageID] = true
		}
	}
	return readable, nil
}

// trashedDatabaseContainerIDs keeps, of databases held in pages' bodies,
// those in the trash themselves, as a mention of one says
// (readDatabaseMentionPreview): its block sent there from a page out of it
// (database_container_trash.go), or deleted with it (deleted_databases.go).
func trashedDatabaseContainerIDs(app core.App, containerIDs []string) (map[string]bool, error) {
	trashed := map[string]bool{}
	if len(containerIDs) == 0 {
		return trashed, nil
	}
	encoded, err := json.Marshal(containerIDs)
	if err != nil {
		return nil, err
	}
	found := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[container_id]] FROM {{leafen_database_container_trash}}
		WHERE [[container_id]] IN (SELECT [[value]] FROM json_each({:ids}))
		UNION
		SELECT [[block_id]] FROM {{leafen_deleted_databases}}
		WHERE [[block_id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"ids": string(encoded)}).Column(&found); err != nil {
		return nil, err
	}
	for _, id := range found {
		trashed[id] = true
	}
	return trashed, nil
}

func loadWorkspaceSearchRowSources(app core.App, ids []string) (map[string]databaseRowSearchSource, error) {
	result := map[string]databaseRowSearchSource{}
	if len(ids) == 0 {
		return result, nil
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows := []struct {
		RowID string `db:"row_id"`
		databaseRowSearchSource
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[row_id]], [[owner]], [[workspace_id]], [[container_page_id]], [[data_source_id]]
		FROM {{leafen_database_row_pages}} WHERE [[row_id]] IN (SELECT [[value]] FROM json_each({:ids}))
	`).Bind(dbx.Params{"ids": string(encoded)}).All(&rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.RowID] = row.databaseRowSearchSource
	}
	return result, nil
}
