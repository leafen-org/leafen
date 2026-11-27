package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database page's body — a row's, a template's — may hold a database of
// its own. A copy of the body takes a copy of that database with its views
// and rows, as Notion's does (seen 2026-11-14: a page made from a template
// got its title, values and icon at once, then "페이지 복제 중" and a few
// seconds later the body with a new database and copies of its rows; a row's
// duplicate got a new database with copies of its rows too): the body's
// other blocks are written at once, and each database is copied after them
// into its place, one after another, by a job (database_row_page_database_
// copies.go) that takes each through the copy a database's block takes
// (admitDatabaseTemplateCopy), the source page only read. Copied as a block
// before, the copy's database named the source's own data source: a page
// made by the server from a template showed "데이터베이스를 사용할 수
// 없습니다", applying the template to a page was refused, its values and
// icon too, and a row's duplicate or a database's copy showed and changed
// the source row's database.

// blockCopyPlaceFirst places a copy first under its parent (pastedBlockCopySpot).
const blockCopyPlaceFirst = "\x00first"

// rowPageOwnDatabase is a database of a body's own, which the body's copy
// takes as a copy of it, at its place among the copy's blocks.
type rowPageOwnDatabase struct {
	SourceBlockID string `json:"sourceBlockId"`
	// ParentID is the copy's block it goes under, "" for the page's top.
	ParentID string `json:"parentId,omitempty"`
	// AfterID is the copy's block it follows, "" for first under its parent;
	// AfterDatabase, when set, is the database right before it, whose copy
	// it follows instead.
	AfterID       string `json:"afterId,omitempty"`
	AfterDatabase string `json:"afterDatabase,omitempty"`
	// DataSourceID is the data source the block names. FromPageID and
	// FromBlockID, when set, are the database the copy takes in its place:
	// the one holding that data source on another page
	// (placeRowPageOwnDatabaseHolders).
	DataSourceID string `json:"dataSourceId,omitempty"`
	FromPageID   string `json:"fromPageId,omitempty"`
	FromBlockID  string `json:"fromBlockId,omitempty"`
	// Replaces says the block is on the page already, naming another page's
	// database: its copy takes its place (database_row_page_foreign_databases.go).
	Replaces bool `json:"replaces,omitzero"`
}

// takeRowPageOwnDatabases takes a body copy's own databases, with what is
// under them, out of it, each with its place; sourceOf names a copied
// block's source. A database first on the page's top goes after topAfter
// when it is set: the body goes after a page's own blocks.
func takeRowPageOwnDatabases(
	body []newRowPageBlock, sourceOf func(string) string, topAfter string,
) ([]newRowPageBlock, []rowPageOwnDatabase) {
	own := map[string]bool{}
	for _, block := range body {
		if linked, _ := block.Content["linkedDatabaseSource"].(bool); block.Type == graphBlockTypeInlineDatabase && !linked {
			own[block.ID] = true
		}
	}
	if len(own) == 0 {
		return body, nil
	}
	parents := make(map[string]string, len(body))
	for _, block := range body {
		parents[block.ID] = block.ParentID
	}
	// Up through every ancestor, however deep; each one once.
	underDatabase := func(id string) bool {
		seen := map[string]bool{}
		for parent := parents[id]; parent != "" && !seen[parent]; parent = parents[parent] {
			seen[parent] = true
			if own[parent] {
				return true
			}
		}
		return false
	}
	ordered := append([]newRowPageBlock(nil), body...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].ParentID != ordered[j].ParentID {
			return ordered[i].ParentID < ordered[j].ParentID
		}
		if ordered[i].Position != ordered[j].Position {
			return ordered[i].Position < ordered[j].Position
		}
		return sourceOf(ordered[i].ID) < sourceOf(ordered[j].ID)
	})
	kept := make([]newRowPageBlock, 0, len(body))
	databases := []rowPageOwnDatabase{}
	previous := map[string]rowPageOwnDatabase{}
	for _, block := range ordered {
		if underDatabase(block.ID) {
			continue
		}
		before, placed := previous[block.ParentID]
		if !own[block.ID] {
			kept = append(kept, block)
			previous[block.ParentID] = rowPageOwnDatabase{AfterID: block.ID}
			continue
		}
		dataSourceID, _ := block.Content["dataSourceId"].(string)
		place := rowPageOwnDatabase{
			SourceBlockID: sourceOf(block.ID), ParentID: block.ParentID, AfterID: before.AfterID, DataSourceID: dataSourceID,
		}
		if placed && before.SourceBlockID != "" {
			place.AfterID, place.AfterDatabase = "", before.SourceBlockID
		} else if !placed && block.ParentID == "" {
			place.AfterID = topAfter
		}
		databases = append(databases, place)
		previous[block.ParentID] = place
	}
	// The body's blocks in their order as read: parents first.
	order := make(map[string]int, len(body))
	for index, block := range body {
		order[block.ID] = index
	}
	sort.SliceStable(kept, func(i, j int) bool { return order[kept[i].ID] < order[kept[j].ID] })
	return kept, databases
}

// rowPageOwnDatabaseSources is the data sources of a body copy's own
// databases, which the copy takes as copies of them.
func rowPageOwnDatabaseSources(body []newRowPageBlock) map[string]bool {
	sources := map[string]bool{}
	for _, block := range body {
		if linked, _ := block.Content["linkedDatabaseSource"].(bool); block.Type != graphBlockTypeInlineDatabase || linked {
			continue
		}
		encoded, err := json.Marshal(block.Content)
		if err != nil {
			continue
		}
		content, err := parseDatabaseContainerBlockContent(encoded)
		if err != nil {
			continue
		}
		for _, id := range content.dataSourceIDs() {
			sources[id] = true
		}
	}
	return sources
}

// duplicateDatabaseTemplateBodyText is duplicateDatabaseRowPageBodyText for
// a template's body: its own databases come as copies, after the rest.
func duplicateDatabaseTemplateBodyText(
	app core.App,
	templateID, targetRowID, workspaceID, actorID string,
	text func([]graphTextSpan) []graphTextSpan,
	synced rowPageBodySynced,
) error {
	return duplicateDatabaseRowPageBodyText(app, templateID, targetRowID, workspaceID, actorID, text, synced)
}

// rowPageOwnDatabasesCopied says whether a body takes its own databases as
// copies into target: one copied into another workspace takes them too, as
// a page's copy there does (block_copy_workspaces.go); an import's pages,
// which are not yet shown, take none (their bodies keep the blocks as read).
func rowPageOwnDatabasesCopied(target *core.Record) bool {
	return !target.GetBool("notion_import_staging")
}

// placeRowPageOwnDatabaseHolders names, for each database a body's copy
// takes whose data source the body's page doesn't hold, the database that
// holds it. A body copied before its databases came with it kept the
// source's block, naming the source's database on another page: the copy
// showed "데이터베이스를 사용할 수 없습니다", its block refused every change,
// and a copy of it in turn got no database, its block not being on the page
// it was copied from. Its copy now takes a copy of the database the block
// names, as Notion's copy of a body takes what the body shows, and a block
// no database holds any more is left out, as one the copy can't read.
func placeRowPageOwnDatabaseHolders(app core.App, workspaceID, sourcePageID string, databases []rowPageOwnDatabase) error {
	return placeRowPageDatabaseHolders(app, "[[source]].[[workspace_id]] = {:workspaceId}", workspaceID, sourcePageID, databases)
}

// placeRowPageDatabaseHolders is placeRowPageOwnDatabaseHolders, looking for
// each database's holder in the workspaces where names: the one given, or
// any other (database_row_page_foreign_databases.go).
func placeRowPageDatabaseHolders(app core.App, where, workspaceID, sourcePageID string, databases []rowPageOwnDatabase) error {
	for index := range databases {
		database := &databases[index]
		if database.DataSourceID == "" {
			continue
		}
		holder := struct {
			PageID  string `db:"page_id"`
			BlockID string `db:"block_id"`
		}{}
		err := app.DB().NewQuery(`
			SELECT [[container]].[[page_id]], [[container]].[[block_id]]
			FROM {{leafen_data_sources}} AS [[source]]
			JOIN {{leafen_database_containers}} AS [[container]]
				ON [[container]].[[page_id]] = [[source]].[[page_id]] AND [[container]].[[kind]] = 'source'
			JOIN {{leafen_database_container_sources}} AS [[member]]
				ON [[member]].[[container_id]] = [[container]].[[id]] AND [[member]].[[data_source_id]] = [[source]].[[id]]
					AND [[member]].[[source_container_id]] = ''
			WHERE [[source]].[[id]] = {:dataSourceId} AND ` + where + `
				AND NOT EXISTS (SELECT 1 FROM {{leafen_database_container_trash}} AS [[trash]]
					WHERE [[trash]].[[container_id]] = [[container]].[[id]])
			ORDER BY [[container]].[[id]] LIMIT 1
		`).Bind(dbx.Params{"dataSourceId": database.DataSourceID, "workspaceId": workspaceID}).One(&holder)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if holder.PageID != sourcePageID || holder.BlockID != database.SourceBlockID {
			database.FromPageID, database.FromBlockID = holder.PageID, holder.BlockID
		}
	}
	return nil
}
