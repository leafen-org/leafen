package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
)

// A run again writes a row's relations as Notion gives them now
// (notionImportLinker.row), and a two-way relation's other side changes with
// it, as any writer's change of one does (database_relation_pairs.go): the
// rows the row now names name it back, and those that named it and that it no
// longer names let it go. Each side is written as the relation pair runner
// writes the other side (databaseRelationPairLinkOperations), in the run's own
// turn. A run again keeps a row Notion no longer gives (decision "설계 우선
// Notion 이사" 3절 원칙 4, notion_migration_removed.go) and doesn't write it;
// without this, the row it kept went on naming a row whose relation Notion
// had taken it out of (2026-11-24: Notion's trashed 거래 3호 still named 고객
// 하나, whose 거래 no longer held it). The other side is put in step with
// what the run wrote, not with what the row held before, so a pair an earlier
// run left one-sided comes back in step when the run goes again. A first run
// reads both sides from Notion and writes each, and the other side follows
// the rows a column holds for another property (notion_migration_shared_pairs.go).

// A relation Notion emptied since the run that last took its row is emptied
// too, through the same write, so a pair cut on both sides in Notion is cut
// on both in Leafen (2026-11-24: a row and its customer, each left with none
// in Notion, went on naming each other): Notion gives no value of an empty
// relation (notionImportRowValue), so the run tells one by the row as the
// ledger keeps it — the ledger takes the row again only at the run's end —
// and a row it held one in is linked even when it holds none now
// (notionImportLinkSourceSQL). A relation that names only what the run
// doesn't bring is emptied as well, as the first run leaves it, and so is a
// column beside it that a split made and it names none of now
// (notionImportLinker.siblingValues). Relations take the source's value on
// every run, so this follows the rule of a changed cell
// (notion_migration_rerun_emptied.go).

// rowValues are a row's staged values and, on a run again, each relation the
// source emptied since — or a property the plan turns into objects — with no
// value.
func (linker *notionImportLinker) rowValues(source storedNotionSource) (map[string]notionDatabaseRowValuePlan, error) {
	values := map[string]notionDatabaseRowValuePlan{}
	if err := json.Unmarshal([]byte(source.Text), &values); err != nil || linker.payload.MigrationRerunSince <= 0 {
		return values, err
	}
	kept, err := notionMigrationLedgerState(linker.app, linker.payload.MigrationID, source.SourceID, "row")
	before := map[string]notionDatabaseRowValuePlan{}
	if err != nil || kept == "" || json.Unmarshal([]byte(kept), &before) != nil {
		return values, err
	}
	for propertySource, value := range before {
		if _, given := values[propertySource]; given || notionImportRowSpecialKey(propertySource) ||
			value.Type != graphValueTypeRelation && !linker.run.linkedLater(propertySource) {
			continue
		}
		values[propertySource] = notionDatabaseRowValuePlan{Type: value.Type}
	}
	return values, nil
}

// relationValue puts a relation's rows among a row's values, joined with
// those another property of the row put in the same column; on a run again,
// none empties the column, unless another put some there.
func (linker *notionImportLinker) relationValue(rowValues map[string]graphRowValue, propertyID string, targets []string) error {
	if len(targets) == 0 {
		if _, written := rowValues[propertyID]; !written && linker.payload.MigrationRerunSince > 0 {
			rowValues[propertyID] = graphRowValue{Type: graphValueTypeRelation, Value: graphRowValueCleared}
		}
		return nil
	}
	targets, _ = gatheredValue(rowValues, propertyID, targets)
	encoded, err := json.Marshal(targets)
	if err != nil {
		return err
	}
	rowValues[propertyID] = graphRowValue{Type: graphValueTypeRelation, Value: encoded}
	return nil
}

// writeRow writes a row's relation values; on a run again, the other side of
// its two-way relations follows, and on a first run that of the rows a
// column holds for another property (shared), each two-way column keeping
// the rows whose column the run puts the row in for another
// (notion_migration_shared_pairs.go).
func (linker *notionImportLinker) writeRow(
	batch *structuralMutationBatch, pageID, rowSource string, operations []graphOperation,
	externalRows map[string]graphEntityReference, shared map[string][]string,
) error {
	owner := linker.payload.AuthorityOwnerID
	if err := linker.keepSharedHolders(operations, rowSource, externalRows); err != nil {
		return err
	}
	if err := applyNotionGraphOperationsWith(
		linker.app, batch, owner, linker.job.WorkspaceID, pageID, linker.job.Owner,
		operations, notionGraphApplyReferences{externalRows: externalRows},
	); err != nil {
		return err
	}
	var links []databaseRelationPairPayload
	var err error
	if linker.payload.MigrationRerunSince > 0 {
		links, err = linker.relationPairLinks(operations)
	} else {
		links, err = linker.sharedPairLinks(operations, shared)
	}
	if err != nil {
		return err
	}
	for _, payload := range links {
		mirrored, err := databaseRelationPairLinkOperations(linker.app, owner, payload)
		if err != nil || len(mirrored) == 0 {
			if err != nil {
				return err
			}
			continue
		}
		targetPage, err := linker.dataSourcePage(payload.TargetDataSourceID)
		if err != nil {
			return err
		}
		named := map[string]struct{}{}
		for _, operation := range mirrored {
			for _, value := range operation.Row.Values {
				for _, id := range graphRelationRowIDs(value.Value) {
					named[id] = struct{}{}
				}
			}
		}
		rows, err := loadGraphReferences(linker.app, "leafen_database_rows", named)
		if err != nil {
			return err
		}
		targetBatch, err := loadStructuralMutationBatch(linker.app, targetPage, linker.documents)
		if err != nil {
			return err
		}
		if err := applyNotionGraphOperationsWith(
			linker.app, targetBatch, owner, linker.job.WorkspaceID, targetPage, linker.job.Owner,
			mirrored, notionGraphApplyReferences{externalRows: rows},
		); err != nil {
			return err
		}
	}
	return nil
}

// relationPairLinks is, for each two-way relation the operations write, the
// rows of its other side to add the written row to and to take it from.
func (linker *notionImportLinker) relationPairLinks(operations []graphOperation) ([]databaseRelationPairPayload, error) {
	owner := linker.payload.AuthorityOwnerID
	ids := map[string]struct{}{}
	for _, operation := range operations {
		if operation.Kind == graphOperationKindUpsertRow && operation.Row != nil {
			for propertyID, value := range operation.Row.Values {
				if value.Type == graphValueTypeRelation {
					ids[propertyID] = struct{}{}
				}
			}
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	properties, err := loadDatabaseRelationPairProperties(linker.app, owner, workspaceExportSortedKeys(ids))
	if err != nil {
		return nil, err
	}
	links := []databaseRelationPairPayload{}
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertRow || operation.Row == nil {
			continue
		}
		row := operation.Row
		for _, propertyID := range workspaceExportSortedKeys(row.Values) {
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
				return nil, err
			}
			add, remove := databaseRelationPairDiff(holders, next)
			// A value Notion's list was cut short of lets nothing go: the rows
			// past the cut still name it.
			if len(remove) > 0 {
				if cut, err := linker.relationCutShort(propertyID); err != nil || cut {
					remove = nil
					if err != nil {
						return nil, err
					}
				}
			}
			if len(add) == 0 && len(remove) == 0 {
				continue
			}
			links = append(links, databaseRelationPairPayload{
				SourceDataSourceID: property.DataSourceID, PropertyID: propertyID,
				TargetDataSourceID: pair.TargetDataSourceID, PairPropertyID: pair.PairPropertyID,
				Links: []databaseRelationPairLink{{RowID: row.ID, Add: add, Remove: remove}},
			})
		}
	}
	return links, nil
}

// relationHolders are the rows of a pair's other side whose relation names
// the row.
func (linker *notionImportLinker) relationHolders(pair *databaseRelationPair, rowID string) ([]string, error) {
	// A pair whose other data source is gone has no other side to read.
	pages := []string{}
	if err := linker.app.DB().NewQuery(`SELECT [[page_id]] FROM {{leafen_data_sources}} WHERE [[id]]={:id} AND [[owner]]={:owner}`).
		Bind(dbx.Params{"id": pair.TargetDataSourceID, "owner": linker.payload.AuthorityOwnerID}).Column(&pages); err != nil || len(pages) != 1 {
		return nil, err
	}
	holders := []string{}
	err := linker.app.DB().NewQuery(`
		SELECT DISTINCT [[row_id]] FROM {{leafen_database_relation_items}}
		INDEXED BY [[idx_leafen_database_relation_items_target]]
		WHERE [[owner]]={:owner} AND [[page_id]]={:page} AND [[target_row_id]]={:row} AND [[property_id]]={:pair}
		ORDER BY [[row_id]]
	`).Bind(dbx.Params{
		"owner": linker.payload.AuthorityOwnerID, "page": pages[0], "row": rowID, "pair": pair.PairPropertyID,
	}).Column(&holders)
	return holders, err
}

// relationCutShort says whether the run found a value of the relation that
// Notion's list was cut short of (notionImportRowValue).
func (linker *notionImportLinker) relationCutShort(propertyID string) (bool, error) {
	var cut int
	err := linker.app.DB().NewQuery(`
		SELECT EXISTS (
			SELECT 1 FROM {{leafen_notion_import_mappings}} AS [[mapping]]
			JOIN {{leafen_notion_import_sources}} AS [[issue]] ON [[issue]].[[job_id]]=[[mapping]].[[job_id]]
				AND [[issue]].[[kind]]='issue' AND [[issue]].[[source_id]]=[[mapping]].[[source_id]] || '#' || {:reason}
			WHERE [[mapping]].[[job_id]]={:job} AND [[mapping]].[[kind]]='property' AND [[mapping]].[[native_id]]={:property}
		)
	`).Bind(dbx.Params{
		"job": linker.job.ID, "property": propertyID, "reason": notionImportIssuePropertyValueTruncated,
	}).Row(&cut)
	return cut == 1, err
}
