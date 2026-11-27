package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A wiki's pages sit where they are in the wiki — under another of its
// pages, or at its top — by their rows' sub-item parent alone, as a Notion
// wiki's pages sit in its tree with no relation among its properties (seen
// 2026-11-11: "하위 페이지" off in its settings). Its Sub-pages, turned on,
// are a relation pair of its own, Parent page and Sub-pages, which its views
// nest by and which leave the pages' places in the wiki as they are. Wikis
// made before kept their places in a Parent item and Sub-items pair as well;
// the job runner takes that pair away, the places staying in the rows.

// wikiSourcePairs reads, of the data sources given, the wikis', each with
// the Parent page and Sub-pages it turned on, or none.
func wikiSourcePairs(app core.App, sourceIDs []string) (map[string]*databaseSubitemPair, error) {
	wikis := map[string]*databaseSubitemPair{}
	if len(sourceIDs) == 0 {
		return wikis, nil
	}
	encoded, err := json.Marshal(sourceIDs)
	if err != nil {
		return nil, err
	}
	rows := []struct {
		Source   string `db:"id"`
		Parent   string `db:"parent"`
		Children string `db:"children"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[source]].[[id]],
			COALESCE(json_extract([[features]].[[subitems_json]], '$.parentPropertyId'), '') AS [[parent]],
			COALESCE(json_extract([[features]].[[subitems_json]], '$.subitemsPropertyId'), '') AS [[children]]
		FROM {{leafen_data_sources}} AS [[source]]
		LEFT JOIN {{leafen_database_task_features}} AS [[features]] ON [[features]].[[data_source_id]] = [[source]].[[id]]
			AND json_valid([[features]].[[subitems_json]]) AND json_extract([[features]].[[subitems_json]], '$.enabled') = 1
			AND json_type([[features]].[[subitems_json]], '$.parentPropertyId') = 'text'
			AND json_type([[features]].[[subitems_json]], '$.subitemsPropertyId') = 'text'
		WHERE [[source]].[[id]] IN (SELECT [[value]] FROM json_each({:sources})) AND [[source]].[[wiki]] = 1
	`).Bind(dbx.Params{"sources": string(encoded)}).All(&rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		wikis[row.Source] = nil
		if row.Parent != "" && row.Children != "" && row.Parent != row.Children {
			wikis[row.Source] = &databaseSubitemPair{Parent: row.Parent, Children: row.Children}
		}
	}
	return wikis, nil
}

// keepWikiPlacesOfPersonWrites keeps a wiki's pages where they are in the
// wiki through a person's change to its rows, which its views make with the
// rows as they show them: a wiki's page moves in the wiki from its page menu
// and the sidebar (wiki_page_change.go), and a row the person adds goes to
// the wiki's top. With its Sub-pages on, a row nested under another in a
// view, or taken out from under it — a new sub-page, a row dropped onto
// another — names that row as its Parent page instead.
func keepWikiPlacesOfPersonWrites(app core.App, ownerID string, operations []graphOperation) ([]graphOperation, error) {
	sources := map[string]struct{}{}
	for _, operation := range operations {
		if operation.Kind == graphOperationKindUpsertRow && operation.Row != nil {
			sources[operation.Row.DataSourceID] = struct{}{}
		}
	}
	wikis, err := wikiSourcePairs(app, workspaceExportSortedKeys(sources))
	if err != nil || len(wikis) == 0 {
		return operations, err
	}
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertRow || operation.Row == nil {
			continue
		}
		row := operation.Row
		pair, wiki := wikis[row.DataSourceID]
		if !wiki {
			continue
		}
		stored := ""
		err := app.DB().NewQuery(`SELECT COALESCE([[subitem_parent_id]], '') FROM {{leafen_database_rows}}
			WHERE [[owner]] = {:owner} AND [[id]] = {:id}`).Bind(dbx.Params{"owner": ownerID, "id": row.ID}).Row(&stored)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		asked := ""
		if row.SubitemParentID != nil {
			asked = *row.SubitemParentID
		}
		if asked == stored {
			continue
		}
		if pair != nil && !rowWritesValue(row, pair.Parent) {
			value := json.RawMessage(`[]`)
			if asked != "" {
				if value, err = json.Marshal([]string{asked}); err != nil {
					return nil, err
				}
			}
			if row.Values == nil {
				row.Values = map[string]graphRowValue{}
			}
			row.Values[pair.Parent] = graphRowValue{Type: graphValueTypeRelation, Value: value}
		}
		row.SubitemParentID = nil
		if stored != "" {
			place := stored
			row.SubitemParentID = &place
		}
	}
	return operations, nil
}

// dropWikiNestingPairs takes away the Parent item and Sub-items a wiki made
// before kept its pages' places in, through the deletion lifecycle as a pair
// turned off with its properties removed goes, and turns its Sub-pages off.
// The places stay in the rows' sub-item parents, which the pair only
// repeated and its deletion leaves as they are. It reports whether a wiki on
// a page in the trash waits.
func (p *pocketBaseYgoPersistence) dropWikiNestingPairs(ctx context.Context) (bool, error) {
	sources := []databaseSubitemMigrationSource{}
	if err := p.app.DB().NewQuery(`
		SELECT [[features]].[[data_source_id]], [[features]].[[page_id]], [[features]].[[workspace_id]], [[features]].[[owner]],
			json_extract([[features]].[[subitems_json]], '$.parentPropertyId') AS [[parent]]
		FROM {{leafen_database_task_features}} AS [[features]]
		JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[id]] = [[features]].[[data_source_id]] AND [[source]].[[wiki]] = 1
		WHERE json_type([[features]].[[subitems_json]], '$.parentPropertyId') = 'text'
		ORDER BY [[features]].[[data_source_id]]
	`).WithContext(ctx).All(&sources); err != nil {
		return false, err
	}
	waiting := false
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return true, err
		}
		pair := databaseSubitemPairIDs(source.Source)
		if source.Parent != pair.Parent {
			continue
		}
		if trashed, err := pageInTrashPath(p.app, source.Page); err != nil {
			return true, err
		} else if trashed {
			waiting = true
			continue
		}
		actorID, _, err := databaseSubitemMigrationActor(p.app, source)
		if err != nil {
			return true, err
		}
		if err := p.app.RunInTransaction(func(txApp core.App) error {
			var turn int64
			if err := txApp.DB().NewQuery(`
				UPDATE {{leafen_database_task_features}}
				SET [[subitems_json]] = 'null', [[authority_revision]] = [[authority_revision]] + 1
				WHERE [[data_source_id]] = {:source}
				RETURNING [[authority_revision]]
			`).WithContext(ctx).Bind(dbx.Params{"source": source.Source}).Row(&turn); err != nil {
				return err
			}
			return removeDatabasePropertyPair(txApp, actorID, source.Page, "subitem-pair-remove", turn, pair.Parent, pair.Children)
		}); err != nil {
			return true, err
		}
	}
	return waiting, nil
}

// rowWritesValue reports whether a row's write names a property's value.
func rowWritesValue(row *graphDatabaseRow, propertyID string) bool {
	_, written := row.Values[propertyID]
	return written
}
