package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The @ menu offers each source of a database holding several as a row of
// its own, as Notion's does (seen 2026-11-24 in two tabs: an inline one's
// sources each under "Top / ... / Database" — its own name, or its sources'
// names "A 및 B" while it has none — a full-page one's under "Top / ... /
// Page", by a data source's icon), and a source chosen is mentioned as that
// source (data_source_mentions.go), which opens its database at that
// source's view. A database of one source is its source, offered as the
// database alone. The workspace's page list carries no sources, so the
// search the menu asks rows of finds them by their names, each in a
// database left out as the sidebar leaves one out (a view block, one in the
// trash) on a page the reader may read.

const databaseSourceMentionScanWindow = 100

type databaseSourceMentionHolder struct {
	DataSourceID string `db:"data_source_id"`
	ContainerID  string `db:"container_id"`
	PageID       string `db:"page_id"`
	Title        string `db:"title"`
	Spans        string `db:"title_spans"`
	Icon         string `db:"icon"`
	IconType     string `db:"icon_type"`
	Updated      int64  `db:"updated"`
}

// readDatabaseSourceMentionCandidates is the sources of databases holding
// several whose names answer a typed query, the better answer first and
// then the newest, as candidates
// naming each source (SourceID, Title) and its database's place
// (ContainerPage, InlineDatabase, DatabaseTitle and SourceTitles), up to
// limit; none for an empty query, which the menu answers with recent pages.
func readDatabaseSourceMentionCandidates(
	app core.App, readerID, workspaceID, ownerID, query string, limit int,
) ([]databaseRowMentionCandidate, error) {
	if query == "" || limit <= 0 {
		return nil, nil
	}
	judge := withAccessMemo(app)
	closer := readerCloser(app, readerID)
	containers := map[string]databaseRowMentionContainer{}
	// A tier of answers at a time, newest first in each (mentionSearchTiers).
	return fillMentionTiers(query, limit, func(tier mentionSearchTier, need int) ([]databaseRowMentionCandidate, error) {
		candidates := []databaseRowMentionCandidate{}
		err := eachWindow(databaseSourceMentionScanWindow,
			func(size int, last *databaseSourceMentionHolder) ([]databaseSourceMentionHolder, error) {
				return readDatabaseSourceMentionHolders(app, ownerID, workspaceID, query, tier, size,
					windowFillAfterNewest(last, func(holder *databaseSourceMentionHolder) (int64, string) {
						return holder.Updated, holder.DataSourceID
					}))
			},
			func(holders []databaseSourceMentionHolder) (bool, error) {
				for _, holder := range holders {
					if len(candidates) == need {
						break
					}
					title := databaseNameAsRead(closer, holder.Title, decodeStoredTextSpans(holder.Spans))
					if !mentionNameInTier(title, query, tier) {
						continue
					}
					container, checked := containers[holder.PageID]
					if !checked {
						var err error
						if container, err = loadDatabaseRowMentionContainer(judge, readerID, workspaceID, holder.PageID); err != nil {
							return false, err
						}
						if !container.trashed {
							if container.trashed, err = pageInTrashPath(judge, holder.PageID); err != nil {
								return false, err
							}
						}
						containers[holder.PageID] = container
					}
					if container.trashed || container.title == "" && !pageReadableIn(judge, readerID, workspaceID, holder.PageID) {
						continue
					}
					candidate, err := databaseSourceMentionCandidate(app, closer, holder, title, container)
					if err != nil {
						return false, err
					}
					candidates = append(candidates, candidate)
				}
				return len(candidates) == need, nil
			})
		return candidates, err
	})
}

// pageReadableIn is whether the reader may read the page in the workspace,
// for a page whose title is empty, which loadDatabaseRowMentionContainer
// leaves empty for one closed to them too.
func pageReadableIn(app core.App, readerID, workspaceID, pageID string) bool {
	readable, err := authorizePage(app, readerID, pageID, permissionWorkspaceRead)
	return err == nil && readable.Workspace.WorkspaceID == workspaceID
}

// databaseSourceMentionCandidate is a found source as the menu offers it:
// its name and icon, and its database's place — a full-page database's page,
// or the page holding an inline one with the database's own name, none while
// it has none, its sources' names in their order naming it then.
func databaseSourceMentionCandidate(
	app core.App, closer func() searchMentionCloser, holder databaseSourceMentionHolder, title string, container databaseRowMentionContainer,
) (databaseRowMentionCandidate, error) {
	sources := []struct {
		Title  string `db:"title"`
		Spans  string `db:"title_spans"`
		Linked bool   `db:"linked"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[source]].[[title]], [[source]].[[title_spans]], [[place]].[[source_container_id]] != '' AS [[linked]]
		FROM {{leafen_database_container_sources}} [[place]]
		JOIN {{leafen_data_sources}} [[source]] ON [[source]].[[id]] = [[place]].[[data_source_id]]
		WHERE [[place]].[[container_id]] = {:container} AND ` + databaseSourceNotTrashedSQL("[[place]].[[data_source_id]]") + `
		ORDER BY [[place]].[[position]], [[place]].[[data_source_id]]
	`).Bind(dbx.Params{"container": holder.ContainerID}).All(&sources); err != nil {
		return databaseRowMentionCandidate{}, err
	}
	candidate := databaseRowMentionCandidate{
		SourceID: holder.DataSourceID, Title: title, Icon: holder.Icon, IconType: holder.IconType,
		ContainerPage: holder.PageID, ContainerTitle: container.title, Updated: holder.Updated,
	}
	for _, source := range sources {
		candidate.SourceTitles = append(candidate.SourceTitles, databaseSourceTitle{
			Title: databaseNameAsRead(closer, source.Title, decodeStoredTextSpans(source.Spans)), Linked: source.Linked,
		})
	}
	// A full-page database's page is the database: its place ends at that page.
	names, err := inlineDatabaseNamesOfSeveral(app, closer, []string{holder.ContainerID})
	if err != nil {
		return databaseRowMentionCandidate{}, err
	}
	if name, inline := names[holder.ContainerID]; inline {
		candidate.InlineDatabase, candidate.DatabaseTitle = true, name.Title
	}
	return candidate, nil
}

// readDatabaseSourceMentionHolders reads a window of the workspace's data
// sources whose names may answer the query within a tier (mentionTierSQL),
// each in a database of its own (no view block's) out of the trash holding
// another source as well, newest first, after the one ending where after
// says (windowFillAfterNewestSQL).
func readDatabaseSourceMentionHolders(
	app core.App, ownerID, workspaceID, query string, tier mentionSearchTier, size int, after dbx.Params,
) ([]databaseSourceMentionHolder, error) {
	params := dbx.Params{"owner": ownerID, "workspaceId": workspaceID, "limit": size}
	for key, value := range after {
		params[key] = value
	}
	holders := []databaseSourceMentionHolder{}
	err := app.DB().NewQuery(`
		SELECT [[source]].[[id]] AS [[data_source_id]], [[place]].[[container_id]], [[container]].[[page_id]],
			[[source]].[[title]], [[source]].[[title_spans]], [[source]].[[updated]],
			` + dataSourceIconSQL("[[source]]") + ` AS [[icon]], ` + dataSourceIconTypeSQL("[[source]]") + ` AS [[icon_type]]
		FROM {{leafen_data_sources}} [[source]]
		JOIN {{leafen_database_container_sources}} [[place]] ON [[place]].[[data_source_id]] = [[source]].[[id]]
		JOIN {{leafen_database_containers}} [[container]] ON [[container]].[[id]] = [[place]].[[container_id]]
		WHERE [[source]].[[owner]] = {:owner} AND [[source]].[[workspace_id]] = {:workspaceId}
			AND [[container]].[[kind]] = 'source' AND NOT (` + databaseViewBlockSQL("[[container]]") + `)
			AND ` + mentionTierSQL("[[source]].[[title]]", query, tier, params) + `
			AND ` + databaseSourceNotTrashedSQL("[[source]].[[id]]") + `
			AND ` + searchSourceNotDeletedSQL("[[source]].[[workspace_id]]", "[[source]].[[id]]") + `
			AND NOT EXISTS (SELECT 1 FROM {{leafen_database_container_trash}} [[trash]] WHERE [[trash]].[[container_id]] = [[container]].[[id]])
			AND EXISTS (SELECT 1 FROM {{leafen_database_container_sources}} [[other]]
				WHERE [[other]].[[container_id]] = [[container]].[[id]] AND [[other]].[[data_source_id]] != [[source]].[[id]]
					AND ` + databaseSourceNotTrashedSQL("[[other]].[[data_source_id]]") + `)
			AND ` + windowFillAfterNewestSQL("[[source]].[[updated]]", "[[source]].[[id]]") + `
		ORDER BY [[source]].[[updated]] DESC, [[source]].[[id]] LIMIT {:limit}
	`).Bind(params).All(&holders)
	return holders, err
}
