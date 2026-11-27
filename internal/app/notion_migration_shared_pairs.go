package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
)

// A column the plan has hold another property's values — a relation
// gathered into it (notion_migration_roles.go), a merged database's its
// target's column stands for (notion_migration_merge.go), a split row's in
// its target's (notion_migration_split.go) — holds rows Notion's other side
// of the column doesn't name. When the column is two-way, its other side
// names them back from the first run, as a run again's writes leave it
// (notion_migration_relation_pairs.go): the first run makes the other side
// follow the rows the column holds for another property, making that side
// first when the run hasn't come to it yet, and any run writing a row's
// two-way column keeps the rows whose column the run puts the row in for
// another property, so neither side's write takes back what the other's
// gave, in whatever order the run comes to them (2026-11-24: a relation
// gathered into a two-way one left its other side one way on the first run,
// and a run again mirrored it, so the two runs gave different relations,
// and a run again coming to the other side after it would have taken the
// gathered row out of both).

// sharesColumns says whether the plan has any column hold another
// property's values.
func (run *notionMigrationRunPlan) sharesColumns() bool {
	if run == nil {
		return false
	}
	if len(run.merges) > 0 || len(run.splits) > 0 {
		return true
	}
	for _, columns := range run.columns {
		for _, entry := range columns {
			if strings.TrimSpace(entry.Into) != "" {
				return true
			}
		}
	}
	return false
}

// relationColumnFor is where a row's relation's values go in the data source
// the row is in: holder is the ledger's key of the column (the split's in a
// target), column the property whose column it is — its own, or the role it
// is gathered into — propertyID the column, and shared whether the column
// holds them for another property.
func (linker *notionImportLinker) relationColumnFor(propertySource, dataSourceID string, split bool) (string, string, string, bool, error) {
	column, shared := propertySource, false
	if role, gathered := linker.run.roleColumn(propertySource); gathered {
		column, shared = role, true
	}
	if split {
		propertyID, err := linker.splitRelationColumn(column, dataSourceID)
		return notionSplitColumnSource(column, dataSourceID), column, propertyID, true, err
	}
	propertyID, err := linker.propertyID(column)
	if err != nil || shared || propertyID == "" {
		return column, column, propertyID, shared, err
	}
	databaseID, _, _ := strings.Cut(column, ":property:")
	if _, merged := linker.run.mergeTarget(databaseID); !merged {
		return column, column, propertyID, false, nil
	}
	aliased, found := linker.shared[column]
	if !found {
		if aliased, err = linker.aliased(column); err != nil {
			return "", "", "", false, err
		}
		if linker.shared == nil {
			linker.shared = map[string]bool{}
		}
		linker.shared[column] = aliased
	}
	return column, column, propertyID, aliased, nil
}

// keepSharedHolders adds to each two-way column a row writes the rows of
// the other side whose column the run puts the row in for another property.
func (linker *notionImportLinker) keepSharedHolders(
	operations []graphOperation, rowSource string, externalRows map[string]graphEntityReference,
) error {
	if !linker.run.sharesColumns() {
		return nil
	}
	for _, operation := range operations {
		row := operation.Row
		if operation.Kind != graphOperationKindUpsertRow || row == nil {
			continue
		}
		ids := map[string]struct{}{}
		for propertyID, value := range row.Values {
			if value.Type == graphValueTypeRelation {
				ids[propertyID] = struct{}{}
			}
		}
		if len(ids) == 0 {
			continue
		}
		properties, err := loadDatabaseRelationPairProperties(linker.app, linker.payload.AuthorityOwnerID, workspaceExportSortedKeys(ids))
		if err != nil {
			return err
		}
		for _, propertyID := range workspaceExportSortedKeys(ids) {
			property, known := properties[propertyID]
			if !known || property.DataSourceID != row.DataSourceID {
				continue
			}
			pair := databaseRelationPairOf(propertyID, property.DataSourceID, property.Type, property.Config)
			if pair == nil || pair.selfPaired(propertyID) {
				continue
			}
			next := graphRelationRowIDs(row.Values[propertyID].Value)
			holders, err := linker.relationHolders(pair, row.ID)
			if err != nil {
				return err
			}
			kept := map[string]struct{}{}
			for _, holder := range holders {
				if slices.Contains(next, holder) {
					continue
				}
				writes, err := linker.writesShared(holder, pair.PairPropertyID, rowSource)
				if err != nil {
					return err
				}
				if writes {
					kept[holder] = struct{}{}
				}
			}
			if len(kept) == 0 {
				continue
			}
			references, err := loadGraphReferences(linker.app, "leafen_database_rows", kept)
			if err != nil {
				return err
			}
			for id, reference := range references {
				externalRows[id] = reference
			}
			encoded, err := json.Marshal(append(next, workspaceExportSortedKeys(kept)...))
			if err != nil {
				return err
			}
			row.Values[propertyID] = graphRowValue{Type: graphValueTypeRelation, Value: encoded}
		}
	}
	return nil
}

// writesShared says whether the run puts a row, by its staged source, in a
// holder's column for another property.
func (linker *notionImportLinker) writesShared(holderID, column, rowSource string) (bool, error) {
	source := storedNotionSource{}
	err := linker.app.DB().NewQuery(`
		SELECT [[source]].[[sequence]], [[source]].[[source_id]], [[source]].[[kind]], [[source]].[[parent_source_id]],
			[[source]].[[parent_kind]], [[source]].[[position]], [[source]].[[title]], [[source]].[[text]]
		FROM {{leafen_notion_import_mappings}} AS [[mapping]]
		JOIN {{leafen_notion_import_sources}} AS [[source]] ON [[source]].[[job_id]]=[[mapping]].[[job_id]]
			AND [[source]].[[source_id]]=[[mapping]].[[source_id]] AND [[source]].[[kind]]='row'
		WHERE [[mapping]].[[job_id]]={:jobId} AND [[mapping]].[[kind]]='row' AND [[mapping]].[[native_id]]={:row}
		LIMIT 1
	`).Bind(dbx.Params{"jobId": linker.job.ID, "row": holderID}).One(&source)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	values := map[string]notionDatabaseRowValuePlan{}
	if json.Unmarshal([]byte(source.Text), &values) != nil {
		return false, nil
	}
	dataSourceID, split, err := linker.rowPlace(source, holderID)
	if err != nil {
		return false, err
	}
	for _, propertySource := range workspaceExportSortedKeys(values) {
		value := values[propertySource]
		if value.Type != graphValueTypeRelation || notionImportRowSpecialKey(propertySource) {
			continue
		}
		related, _ := value.Value.([]any)
		if !slices.ContainsFunc(related, func(item any) bool {
			id, _ := item.(string)
			return notionImportSameID(id, rowSource)
		}) {
			continue
		}
		_, _, propertyID, shared, err := linker.relationColumnFor(propertySource, dataSourceID, split)
		if err != nil {
			return false, err
		}
		if shared && propertyID == column {
			return true, nil
		}
	}
	return false, nil
}

// sharedPairLinks is, on a first run, for each two-way column a row holds
// rows in for another property, the rows of its other side to add the row
// to.
func (linker *notionImportLinker) sharedPairLinks(operations []graphOperation, shared map[string][]string) ([]databaseRelationPairPayload, error) {
	if len(shared) == 0 {
		return nil, nil
	}
	properties, err := loadDatabaseRelationPairProperties(linker.app, linker.payload.AuthorityOwnerID, workspaceExportSortedKeys(shared))
	if err != nil {
		return nil, err
	}
	links := []databaseRelationPairPayload{}
	for _, operation := range operations {
		row := operation.Row
		if operation.Kind != graphOperationKindUpsertRow || row == nil {
			continue
		}
		for _, propertyID := range workspaceExportSortedKeys(shared) {
			property, known := properties[propertyID]
			if !known || property.DataSourceID != row.DataSourceID {
				continue
			}
			pair := databaseRelationPairOf(propertyID, property.DataSourceID, property.Type, property.Config)
			if pair == nil || pair.selfPaired(propertyID) {
				continue
			}
			if err := linker.madePair(pair); err != nil {
				return nil, err
			}
			holders, err := linker.relationHolders(pair, row.ID)
			if err != nil {
				return nil, err
			}
			add, _ := databaseRelationPairDiff(holders, shared[propertyID])
			if len(add) == 0 {
				continue
			}
			links = append(links, databaseRelationPairPayload{
				SourceDataSourceID: property.DataSourceID, PropertyID: propertyID,
				TargetDataSourceID: pair.TargetDataSourceID, PairPropertyID: pair.PairPropertyID,
				Links: []databaseRelationPairLink{{RowID: row.ID, Add: add}},
			})
		}
	}
	return links, nil
}

// madePair makes a two-way column's other side when the first run hasn't
// come to it yet, as a role column is made for the relations gathered into
// it: the property of the other side's data source the run makes it from.
func (linker *notionImportLinker) madePair(pair *databaseRelationPair) error {
	var made int
	if err := linker.app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_database_properties}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": pair.PairPropertyID}).Row(&made); err != nil || made != 0 {
		return err
	}
	sources := []storedNotionSource{}
	if err := linker.app.DB().NewQuery(`
		SELECT [[source]].[[sequence]], [[source]].[[source_id]], [[source]].[[kind]], [[source]].[[parent_source_id]],
			[[source]].[[parent_kind]], [[source]].[[position]], [[source]].[[title]], [[source]].[[text]]
		FROM {{leafen_notion_import_mappings}} AS [[mapping]]
		JOIN {{leafen_notion_import_sources}} AS [[source]] ON [[source]].[[job_id]]=[[mapping]].[[job_id]]
			AND [[source]].[[parent_source_id]]=[[mapping]].[[source_id]] AND [[source]].[[kind]]='property'
		WHERE [[mapping]].[[job_id]]={:jobId} AND [[mapping]].[[kind]]='data_source' AND [[mapping]].[[native_id]]={:dataSource}
		ORDER BY [[source]].[[sequence]]
	`).Bind(dbx.Params{"jobId": linker.job.ID, "dataSource": pair.TargetDataSourceID}).All(&sources); err != nil {
		return err
	}
	for _, source := range sources {
		if notionImportLinkedPropertyID(linker.job.ID, source.SourceID) != pair.PairPropertyID {
			continue
		}
		// What the run left out isn't made (notion_import_left_out.go).
		if slices.Contains(linker.payload.LeftOut, source.SourceID) || slices.Contains(linker.payload.LeftOut, source.ParentSourceID) {
			return nil
		}
		return linker.property(source)
	}
	return nil
}
