package app

import (
	"context"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The @ menu offers the databases database pages hold in their bodies, as
// Notion's does (seen 2026-11-15: a row's inline database among its "페이지
// 링크", by a table's icon, under "Top / ... / Row"). The workspace's page
// list carries the databases of the pages it lists (bootstrapInlineDatabases),
// not those of rows, so the search the menu asks rows of finds them too: each
// named, drawn and left out as the sidebar names, draws and leaves out a
// page's (a view block, one in the trash, a wiki's own), in a row the reader
// may open, found by its name as a row is by its title.

const databaseRowBodyMentionScanWindow = 100

// readDatabaseRowBodyMentionCandidates is the databases rows hold in their
// bodies that answer a typed query, as candidates naming the row holding
// each (RowTitle) and that row's place (ContainerPage, InlineDatabase), up to
// limit; none for an empty query, which the menu answers with recent pages.
func readDatabaseRowBodyMentionCandidates(
	ctx context.Context, app core.App, readerID, workspaceID, ownerID, query string, limit int,
) ([]databaseRowMentionCandidate, error) {
	if query == "" || limit <= 0 {
		return nil, nil
	}
	// Every row holding a database by the name is looked at, a tier of
	// answers at a time and newest first in each (mentionSearchTiers), until
	// the menu's are found (fillWindows): those of the hundred latest the
	// reader could open were all it offered.
	judge := withAccessMemo(app)
	locations := newInlineDatabaseLocations(app, readerID, workspaceID).reading(ctx)
	return fillMentionTiers(query, limit, func(tier mentionSearchTier, need int) ([]databaseRowMentionCandidate, error) {
		candidates := []databaseRowMentionCandidate{}
		edited := map[string]int64{}
		err := eachWindow(databaseRowBodyMentionScanWindow,
			func(size int, last *databaseRowBodyMentionHolder) ([]databaseRowBodyMentionHolder, error) {
				return readDatabaseRowBodyMentionHolders(app, ownerID, workspaceID, query, tier, size,
					windowFillAfterNewest(last, func(holder *databaseRowBodyMentionHolder) (int64, string) {
						return holder.Updated, holder.RowID
					}))
			},
			func(holders []databaseRowBodyMentionHolder) (bool, error) {
				rows := map[string]bool{}
				for _, holder := range holders {
					rows[holder.RowID] = true
					edited[holder.RowID] = holder.Updated
				}
				held, err := bootstrapInlineDatabases(app, readerID, rows)
				if err != nil {
					return false, err
				}
				for _, database := range held {
					if len(candidates) == need {
						break
					}
					if database.ViewBlock || !mentionNameInTier(database.Title, query, tier) {
						continue
					}
					if _, err := authorizePage(judge, readerID, database.PageID, permissionWorkspaceRead); err != nil {
						if isPermissionDenied(err) {
							continue
						}
						return false, err
					}
					row := struct {
						ContainerPage string `db:"container_page_id"`
						DataSourceID  string `db:"data_source_id"`
					}{}
					if err := app.DB().NewQuery(`SELECT [[container_page_id]], [[data_source_id]] FROM {{leafen_database_row_pages}} WHERE [[row_id]] = {:rowId}`).
						Bind(dbx.Params{"rowId": database.PageID}).One(&row); err != nil {
						return false, err
					}
					container, err := loadDatabaseRowMentionContainer(judge, readerID, workspaceID, row.ContainerPage)
					if err != nil {
						return false, err
					}
					if container.trashed {
						continue
					}
					location, err := locations.of(row.ContainerPage, row.DataSourceID)
					if err != nil {
						return false, err
					}
					candidates = append(candidates, databaseRowMentionCandidate{
						RowID: database.PageID, Title: database.Title, Icon: database.Icon, IconType: database.IconType,
						ContainerPage: row.ContainerPage, ContainerTitle: container.title,
						InlineDatabase: location.Inline, DatabaseTitle: location.Title, UntitledName: location.UntitledRow,
						DatabaseID: database.ContainerID,
						RowTitle:   rowTitleAsRead(app, readerID, database.PageID, databaseRowPageTitle(app, database.PageID)),
						// Edited as its row was.
						Updated: edited[database.PageID],
					})
				}
				return len(candidates) == need, nil
			})
		return candidates, err
	})
}

type databaseRowBodyMentionHolder struct {
	RowID   string `db:"row_id"`
	Updated int64  `db:"updated"`
}

// readDatabaseRowBodyMentionHolders reads a window of the workspace's row
// pages holding a database whose name may answer the query within a tier
// (mentionTierSQL), newest first, after the one ending where after says
// (windowFillAfterNewestSQL).
func readDatabaseRowBodyMentionHolders(
	app core.App, ownerID, workspaceID, query string, tier mentionSearchTier, size int, after dbx.Params,
) ([]databaseRowBodyMentionHolder, error) {
	params := dbx.Params{"owner": ownerID, "workspaceId": workspaceID, "limit": size}
	for key, value := range after {
		params[key] = value
	}
	holders := []databaseRowBodyMentionHolder{}
	// The read starts from the databases and finds each one's row page by
	// id; the page's owner and workspace are read after (+), or SQLite,
	// which keeps no statistics, reads the workspace's every row page.
	err := app.DB().NewQuery(`
		SELECT DISTINCT [[row_page]].[[row_id]], [[row_page]].[[updated]]
		FROM {{leafen_database_containers}} [[container]]
		JOIN {{leafen_database_row_pages}} [[row_page]] ON [[row_page]].[[row_id]] = [[container]].[[page_id]]
		JOIN {{leafen_database_container_sources}} [[place]] ON [[place]].[[container_id]] = [[container]].[[id]]
		JOIN {{leafen_data_sources}} [[source]] ON [[source]].[[id]] = [[place]].[[data_source_id]]
		WHERE [[container]].[[kind]] = 'source' AND +[[row_page]].[[owner]] = {:owner} AND +[[row_page]].[[workspace_id]] = {:workspaceId}
			AND ` + databaseSourceNotTrashedSQL("[[row_page]].[[data_source_id]]") + `
			AND ` + searchSourceNotDeletedSQL("[[row_page]].[[workspace_id]]", "[[row_page]].[[data_source_id]]") + `
			AND ` + databaseRowNotTrashedSQL("[[row_page]].[[row_id]]") + `
			AND ` + mentionTierSQL("[[source]].[[title]]", query, tier, params) + `
			AND ` + windowFillAfterNewestSQL("+[[row_page]].[[updated]]", "[[row_page]].[[row_id]]") + `
		ORDER BY [[row_page]].[[updated]] DESC, [[row_page]].[[row_id]] LIMIT {:limit}
	`).Bind(params).All(&holders)
	return holders, err
}
