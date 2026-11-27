package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

type databaseSourceAuthority struct {
	Title    string `json:"title"`
	Icon     string `json:"icon"`
	IconType string `json:"iconType"`
}

type databaseViewAuthority struct {
	DataSourceID string         `json:"dataSourceId"`
	ContainerID  string         `json:"containerId"`
	Type         string         `json:"type"`
	Name         string         `json:"name"`
	Position     string         `json:"position"`
	Config       map[string]any `json:"config"`
}

type databasePropertyAuthority struct {
	DataSourceID string         `json:"dataSourceId"`
	Type         string         `json:"type"`
	Name         string         `json:"name"`
	Position     string         `json:"position"`
	Config       map[string]any `json:"config"`
}

type databaseRowAuthority struct {
	DataSourceID    string `json:"dataSourceId"`
	Position        string `json:"position"`
	SubitemParentID string `json:"subitemParentId"`
}

type databaseCellAuthority struct {
	Type  string `json:"type"`
	Value any    `json:"value"`
	Spans any    `json:"spans,omitempty"`
}

// applyDatabaseRecordMutation writes a mutation's database changes to the
// database records, which are their authority: each change lands in order,
// a row's values merge by property, and every order keeps the position its
// writer chose. The page document keeps only the body's blocks; doc gives
// their structure to the changes that ask which block owns a view.
func applyDatabaseRecordMutation(
	app core.App,
	doc *crdt.Doc,
	owner, workspaceID, pageID, actorID string,
	operations []graphOperation,
	rowReferences *graphReferenceCache,
	options databaseRecordMutationOptions,
) error {
	now := time.Now().UTC().UnixMilli()
	for _, operation := range operations {
		var err error
		switch operation.Kind {
		case graphOperationKindUpsertBlock, graphOperationKindMoveBlock, graphOperationKindDeleteBlock:
			continue
		case graphOperationKindUpsertDataSource:
			err = writeDatabaseSourceRecord(app, owner, workspaceID, pageID, *operation.DataSource, now)
		case graphOperationKindDeleteDataSource:
			err = deleteDatabaseSourceRecord(app, owner, workspaceID, pageID, operation.DataSourceDeletion.ID)
		case graphOperationKindUpsertView:
			err = writeDatabaseViewRecord(app, doc, owner, workspaceID, pageID, actorID, *operation.View, now)
		case graphOperationKindDeleteView:
			err = deleteDatabaseContainerViewProjection(
				app, owner, workspaceID, pageID, operation.ViewDeletion.ID,
			)
		case graphOperationKindUpsertProperty:
			err = writeDatabasePropertyRecordFound(app, owner, workspaceID, pageID, *operation.Property, now)
		case graphOperationKindDeleteProperty:
			err = deleteDatabasePropertyRecord(app, owner, workspaceID, pageID, operation.PropertyDeletion.ID)
		case graphOperationKindUpsertRow:
			_, mirrored := options.PreserveEditStampsOf[operation.Row.ID]
			err = writeDatabaseRowRecord(
				app, owner, workspaceID, pageID, actorID, operation.Row, rowReferences, now, options.RowVersionsBy,
				options.PreserveRowEditStamps || mirrored, options.CommentMarks,
			)
		case graphOperationKindDeleteRow:
			err = deleteDatabaseRowProjection(
				app, owner, workspaceID, pageID, actorID, operation.RowDeletion.ID,
			)
		case graphOperationKindConfigureDependency, graphOperationKindUpsertDependencyEdge,
			graphOperationKindDeleteDependencyEdge:
			err = applyDatabaseDependencyRecord(app, owner, workspaceID, pageID, operation, now)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// databaseRowCreatedClock is the last created time a row took.
var databaseRowCreatedClock struct {
	sync.Mutex
	last int64
}

// databaseRowCreatedAt is the created time a row made at now takes: now, or
// a millisecond after the row made before it when that one took now or later.
// Each row takes a time of its own in the order rows are made, so the rows
// of one change — a CSV's import or merge, an export's database, pasted or
// copied rows — show newest first in its reverse order, as Notion's do (seen
// 2026-11-12: rows merged from a CSV show in its reverse order where a view
// has not placed them, and a new view of a CSV's database shows its rows
// last first), not in the order of their IDs.
func databaseRowCreatedAt(now int64) int64 {
	databaseRowCreatedClock.Lock()
	defer databaseRowCreatedClock.Unlock()
	at := max(now, databaseRowCreatedClock.last+1)
	databaseRowCreatedClock.last = at
	return at
}

func writeDatabaseSourceRecord(
	app core.App,
	owner, workspaceID, pageID string,
	source graphDataSource,
	now int64,
) error {
	// A source written without an icon keeps the one it has.
	iconSet, icon, iconType := false, "", graphDataSourceIconNone
	if source.IconType != nil {
		iconSet, iconType = true, *source.IconType
		if source.Icon != nil {
			icon = *source.Icon
		}
		if iconType == "" {
			iconType = graphDataSourceIconNone
		}
	}
	// So does one written without a description or its showing.
	description, descriptionSet, descriptionSpans := "", source.Description != nil, ""
	if descriptionSet {
		description = *source.Description
		// Its formatting goes with it; text written alone keeps none.
		if len(source.DescriptionSpans) > 0 {
			encoded, err := json.Marshal(source.DescriptionSpans)
			if err != nil {
				return err
			}
			descriptionSpans = string(encoded)
		}
	}
	// Its name's formatting goes with it. A name written with no spans keeps
	// the formatting it has while it is the same name, as a server's own
	// writes do; a new one written alone has none. A source written without
	// its name keeps the one it has, and its formatting (TitleOmitted).
	titleSet := !source.TitleOmitted
	titleSpans, titleSpansSet := "", source.TitleSpans != nil
	if len(source.TitleSpans) > 0 {
		encoded, err := json.Marshal(source.TitleSpans)
		if err != nil {
			return err
		}
		titleSpans = string(encoded)
	}
	descriptionHidden, hiddenSet := false, source.DescriptionHidden != nil
	if hiddenSet {
		descriptionHidden = *source.DescriptionHidden
	}
	cover, coverSet := "", source.Cover != nil
	if coverSet {
		cover = *source.Cover
	}
	coverPosition, positionSet := 50, source.CoverPosition != nil
	if positionSet {
		coverPosition = *source.CoverPosition
	}
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_data_sources}}
			([[id]], [[owner]], [[workspace_id]], [[page_id]], [[title]], [[title_spans]], [[icon]], [[icon_type]],
			 [[description]], [[description_spans]], [[description_hidden]], [[cover]], [[cover_position]], [[created]], [[updated]])
		VALUES ({:id}, {:owner}, {:workspaceId}, {:pageId}, {:title}, {:titleSpans}, {:icon}, {:iconType},
			{:description}, {:descriptionSpans}, {:descriptionHidden}, {:cover}, {:coverPosition}, {:now}, {:now})
		ON CONFLICT ([[id]]) DO UPDATE SET [[title]] = CASE WHEN {:titleSet} THEN excluded.[[title]] ELSE [[title]] END,
			[[title_spans]] = CASE WHEN {:titleSpansSet} THEN excluded.[[title_spans]]
				WHEN NOT {:titleSet} OR [[title]] = excluded.[[title]] THEN [[title_spans]] ELSE '' END,
			[[icon]] = CASE WHEN {:iconSet} THEN excluded.[[icon]] ELSE [[icon]] END,
			[[icon_type]] = CASE WHEN {:iconSet} THEN excluded.[[icon_type]] ELSE [[icon_type]] END,
			[[description]] = CASE WHEN {:descriptionSet} THEN excluded.[[description]] ELSE [[description]] END,
			[[description_spans]] = CASE WHEN {:descriptionSet} THEN excluded.[[description_spans]] ELSE [[description_spans]] END,
			[[description_hidden]] = CASE WHEN {:hiddenSet} THEN excluded.[[description_hidden]] ELSE [[description_hidden]] END,
			[[cover]] = CASE WHEN {:coverSet} THEN excluded.[[cover]] ELSE [[cover]] END,
			[[cover_position]] = CASE WHEN {:positionSet} THEN excluded.[[cover_position]] ELSE [[cover_position]] END,
			[[updated]] = excluded.[[updated]]
	`).Bind(dbx.Params{
		"id": source.ID, "owner": owner, "workspaceId": workspaceID, "pageId": pageID,
		"title": source.Title, "titleSet": titleSet, "titleSpans": titleSpans, "titleSpansSet": titleSpansSet, "icon": icon, "iconType": iconType, "iconSet": iconSet,
		"description": description, "descriptionSet": descriptionSet, "descriptionSpans": descriptionSpans,
		"descriptionHidden": descriptionHidden, "hiddenSet": hiddenSet,
		"cover": cover, "coverSet": coverSet, "coverPosition": coverPosition, "positionSet": positionSet, "now": now,
	}).Execute(); err != nil {
		return err
	}
	if err := app.DB().NewQuery(`
		SELECT [[icon]], [[icon_type]], [[cover]] FROM {{leafen_data_sources}} WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"id": source.ID}).Row(&icon, &iconType, &cover); err != nil {
		return err
	}
	if err := replaceProductFileDataSourceCoverReference(app, workspaceID, pageID, source.ID, cover); err != nil {
		return err
	}
	return replaceProductFileDataSourceIconReference(app, workspaceID, pageID, source.ID, icon, iconType)
}

// deleteDatabaseSourceRecord takes a source with its views, properties and
// rows; a relation another database on the page keeps to it must go first.
func deleteDatabaseSourceRecord(app core.App, owner, workspaceID, pageID, sourceID string) error {
	var incoming int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_database_properties}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[data_source_id]] != {:id}
			AND [[type]] = {:relationType} AND json_valid([[config]])
			AND json_extract([[config]], '$.relationDatabaseId') = {:id}
	`).Bind(dbx.Params{
		"owner": owner, "pageId": pageID, "id": sourceID, "relationType": graphValueTypeRelation,
	}).Row(&incoming); err != nil {
		return err
	}
	if incoming > 0 {
		return graphInputError(errorCodeGraphReferenceInvalid, "Delete the linked relation property first.")
	}
	return deleteDatabaseSourceProjection(app, owner, workspaceID, pageID, sourceID)
}

// writeDatabaseViewRecord writes a view as its change has it, made by the
// change's actor when new and last changed by them; a change no one makes
// leaves who last changed it as it was.
func writeDatabaseViewRecord(
	app core.App,
	doc *crdt.Doc,
	owner, workspaceID, pageID, actorID string,
	view graphDatabaseView,
	now int64,
) error {
	containerID := view.ContainerID
	if containerID == "" {
		var err error
		if containerID, err = retainedDatabaseViewOwner(app, doc, pageID, view.ID, view.DataSourceID); err != nil {
			return err
		}
	}
	config, err := databaseJSONObject(view.Config)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return err
	}
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_database_views}}
			([[id]], [[owner]], [[workspace_id]], [[page_id]], [[data_source_id]], [[container_id]], [[type]], [[name]], [[position]], [[config]], [[created]], [[updated]],
			[[created_by]], [[last_edited_by]])
		VALUES ({:id}, {:owner}, {:workspaceId}, {:pageId}, {:sourceId}, {:containerId}, {:type}, {:name}, {:position}, {:config}, {:now}, {:now},
			{:actorId}, {:actorId})
		ON CONFLICT ([[id]]) DO UPDATE SET
			[[data_source_id]] = excluded.[[data_source_id]], [[container_id]] = excluded.[[container_id]],
			[[type]] = excluded.[[type]], [[name]] = excluded.[[name]], [[position]] = excluded.[[position]],
			[[config]] = excluded.[[config]], [[updated]] = excluded.[[updated]],
			[[last_edited_by]] = CASE WHEN excluded.[[last_edited_by]] = '' THEN [[last_edited_by]] ELSE excluded.[[last_edited_by]] END
	`).Bind(dbx.Params{
		"id": view.ID, "owner": owner, "workspaceId": workspaceID, "pageId": pageID,
		"sourceId": view.DataSourceID, "containerId": containerID, "type": view.Type, "name": view.Name,
		"position": view.Position, "config": string(encoded), "now": now, "actorId": strings.TrimSpace(actorID),
	}).Execute(); err != nil {
		return err
	}
	if err := replaceDatabaseViewPropertyReferences(app, databaseMutationProjectionRow{
		ID: view.ID, Owner: owner, WorkspaceID: workspaceID, PageID: pageID, Type: view.Type,
		Config: string(encoded), SourceID: view.DataSourceID,
	}); err != nil {
		return err
	}
	return refreshDatabaseContainerViewsForDataSource(app, owner, workspaceID, view.DataSourceID)
}

func writeDatabasePropertyRecord(
	app core.App,
	owner, workspaceID, pageID string,
	property graphProperty,
	now int64,
) error {
	config, err := databaseJSONObject(property.Config)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return err
	}
	if property.Position, err = databasePropertyOwnPlace(app, owner, property); err != nil {
		return err
	}
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_database_properties}}
			([[id]], [[owner]], [[workspace_id]], [[page_id]], [[data_source_id]], [[type]], [[name]], [[position]], [[config]], [[created]], [[updated]])
		VALUES ({:id}, {:owner}, {:workspaceId}, {:pageId}, {:sourceId}, {:type}, {:name}, {:position}, {:config}, {:now}, {:now})
		ON CONFLICT ([[id]]) DO UPDATE SET
			[[data_source_id]] = excluded.[[data_source_id]], [[type]] = excluded.[[type]],
			[[name]] = excluded.[[name]], [[position]] = excluded.[[position]],
			[[config]] = excluded.[[config]], [[updated]] = excluded.[[updated]]
	`).Bind(dbx.Params{
		"id": property.ID, "owner": owner, "workspaceId": workspaceID, "pageId": pageID,
		"sourceId": property.DataSourceID, "type": property.Type, "name": property.Name,
		"position": property.Position, "config": string(encoded), "now": now,
	}).Execute(); err != nil {
		return err
	}
	if err := retireDatabasePageRulesOfProperty(app, property.ID, property.Type); err != nil {
		return err
	}
	return replaceDatabaseRelationPropertyTarget(app, databaseMutationProjectionRow{
		ID: property.ID, Owner: owner, WorkspaceID: workspaceID, PageID: pageID, Type: property.Type,
		Config: string(encoded), SourceID: property.DataSourceID,
	})
}

// deleteDatabasePropertyRecord takes a property and its values; a view on
// the page that still uses it must change first.
func deleteDatabasePropertyRecord(app core.App, owner, workspaceID, pageID, propertyID string) error {
	var used int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_database_view_property_refs}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[property_id]] = {:id}
	`).Bind(dbx.Params{"owner": owner, "pageId": pageID, "id": propertyID}).Row(&used); err != nil {
		return err
	}
	if used > 0 {
		return graphInputError(errorCodeGraphReferenceInvalid, "Change the views that use this property first.")
	}
	if err := retireDatabasePageRulesOfProperty(app, propertyID, ""); err != nil {
		return err
	}
	return deleteDatabasePropertyProjection(app, owner, workspaceID, pageID, propertyID)
}

func writeDatabaseRowRecord(
	app core.App,
	owner, workspaceID, pageID, actorID string,
	row *graphDatabaseRow,
	rowReferences *graphReferenceCache,
	now int64, versionsBy string,
	preserveEditStamps, commentMarks bool,
) error {
	subitemParentID := ""
	if row.SubitemParentID != nil {
		subitemParentID = *row.SubitemParentID
	}
	knownNew := rowReferences.completeMissing(row.ID)
	// A new row takes its page's ID; a page already using it stays a page
	// unless it was turned into this source's database page first.
	var claimed int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_pages}}
		WHERE [[id]] = {:id} AND [[data_source_id]] != {:sourceId}
			AND NOT EXISTS (SELECT 1 FROM {{leafen_database_rows}} WHERE [[id]] = {:id})
	`).Bind(dbx.Params{"id": row.ID, "sourceId": row.DataSourceID}).Row(&claimed); err != nil {
		return err
	}
	if claimed > 0 {
		return graphInputError(errorCodeGraphReferenceInvalid, "This ID is already used by a page.")
	}
	created := knownNew
	if !created {
		var existing int
		if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_database_rows}} WHERE [[id]] = {:id}`).
			Bind(dbx.Params{"id": row.ID}).Row(&existing); err != nil {
			return err
		}
		created = existing == 0
	}
	// A row made here takes a created time of its own (databaseRowCreatedAt):
	// the rows one change makes keep its order in the views that show the
	// newest first.
	rowNow := now
	if created {
		rowNow = databaseRowCreatedAt(now)
	}
	historyChanged, err := beginDatabaseRowValueHistory(app, row, knownNew)
	if err != nil {
		return err
	}
	oldParentID := ""
	if !knownNew {
		if err := app.DB().NewQuery(`
			SELECT [[subitem_parent_id]] FROM {{leafen_database_rows}}
			WHERE [[id]] = {:id} AND [[owner]] = {:owner} AND [[page_id]] = {:pageId}
		`).Bind(dbx.Params{
			"id": row.ID, "owner": owner, "pageId": pageID,
		}).Row(&oldParentID); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	childCountExpression := `(SELECT COUNT(*) FROM {{leafen_database_rows}} WHERE [[owner]] = {:owner}
		AND [[page_id]] = {:pageId} AND [[data_source_id]] = {:sourceId}
		AND [[subitem_parent_id]] = {:id})`
	// The system's own rewrite of an existing row leaves when and by whom it
	// was last edited, as Notion's ID numbering, type conversion and relation
	// mirroring do (seen 2026-11-03).
	keepEditStamps := preserveEditStamps && !created
	updatedExpression := "excluded.[[updated]]"
	if keepEditStamps {
		// The time its page shows stays, a later body edit's or Notion's an import gave it (notion_import_row_stamps.go), as the rows'
		// trigger copies the row's onto its page (database_row_page_schema.go); the row's own, later, moved a run again's rows on (2026-11-24).
		updatedExpression = `COALESCE((SELECT [[kept]].[[updated]] FROM {{leafen_database_row_pages}} [[kept]]
			WHERE [[kept]].[[row_id]] = {{leafen_database_rows}}.[[id]]), {{leafen_database_rows}}.[[updated]])`
	}
	if _, err := preparedQuery(app, fmt.Sprintf(`
		INSERT INTO {{leafen_database_rows}}
			([[id]], [[owner]], [[workspace_id]], [[page_id]], [[data_source_id]], [[subitem_parent_id]], [[subitem_child_count]], [[position]], [[created]], [[updated]])
		VALUES ({:id}, {:owner}, {:workspaceId}, {:pageId}, {:sourceId}, {:subitemParentId},
			%s, {:position}, {:now}, {:now})
		ON CONFLICT ([[id]]) DO UPDATE SET [[data_source_id]] = excluded.[[data_source_id]],
			[[subitem_parent_id]] = excluded.[[subitem_parent_id]],
			[[position]] = excluded.[[position]], [[updated]] = %s
	`, childCountExpression, updatedExpression)).Bind(dbx.Params{
		"id": row.ID, "owner": owner, "workspaceId": workspaceID, "pageId": pageID,
		"sourceId": row.DataSourceID, "subitemParentId": subitemParentID,
		"position": row.Position, "now": rowNow,
	}).Execute(); err != nil {
		return err
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		actorID = owner
	}
	if !keepEditStamps {
		if err := setRowPageRecordAuthors(app, row.ID, workspaceID, actorID); err != nil {
			return err
		}
	}
	if oldParentID != subitemParentID {
		if oldParentID != "" {
			if _, err := app.DB().NewQuery(`
				UPDATE {{leafen_database_rows}}
				SET [[subitem_child_count]] = MAX(0, [[subitem_child_count]] - 1), [[updated]] = {:now}
				WHERE [[id]] = {:parentId} AND [[owner]] = {:owner} AND [[page_id]] = {:pageId}
			`).Bind(dbx.Params{
				"parentId": oldParentID, "owner": owner, "pageId": pageID, "now": now,
			}).Execute(); err != nil {
				return err
			}
		}
		if subitemParentID != "" {
			if _, err := app.DB().NewQuery(`
				UPDATE {{leafen_database_rows}}
				SET [[subitem_child_count]] = [[subitem_child_count]] + 1, [[updated]] = {:now}
				WHERE [[id]] = {:parentId} AND [[owner]] = {:owner} AND [[page_id]] = {:pageId}
			`).Bind(dbx.Params{
				"parentId": subitemParentID, "owner": owner, "pageId": pageID, "now": now,
			}).Execute(); err != nil {
				return err
			}
		}
	}
	propertyIDs := make([]string, 0, len(row.Values))
	for propertyID := range row.Values {
		propertyIDs = append(propertyIDs, propertyID)
	}
	sort.Strings(propertyIDs)
	for _, propertyID := range propertyIDs {
		rowValue := row.Values[propertyID]
		if graphRowValueIsCleared(rowValue.Value) {
			if err := deleteDatabaseRowValueProjection(app, workspaceID, pageID, row.ID, propertyID, knownNew); err != nil {
				return err
			}
			continue
		}
		decoded, err := decodeBoundedGraphJSON(rowValue.Value)
		if err != nil {
			return err
		}
		cell := databaseCellAuthority{Type: rowValue.Type, Value: decoded}
		value, err := json.Marshal(decoded)
		if err != nil {
			return err
		}
		spans := ""
		if len(rowValue.Spans) > 0 {
			encoded, err := json.Marshal(rowValue.Spans)
			if err != nil {
				return err
			}
			spans, cell.Spans = string(encoded), rowValue.Spans
		}
		if _, err := preparedQuery(app, `
			INSERT INTO {{leafen_database_row_values}}
				([[owner]], [[workspace_id]], [[page_id]], [[row_id]], [[property_id]], [[type]], [[value]], [[spans]], [[created]], [[updated]])
			VALUES ({:owner}, {:workspaceId}, {:pageId}, {:rowId}, {:propertyId}, {:type}, {:value}, {:spans}, {:now}, {:now})
			ON CONFLICT ([[row_id]], [[property_id]]) DO UPDATE SET
				[[type]] = excluded.[[type]], [[value]] = excluded.[[value]], [[spans]] = excluded.[[spans]],
				[[updated]] = excluded.[[updated]]
		`).Bind(dbx.Params{
			"owner": owner, "workspaceId": workspaceID, "pageId": pageID,
			"rowId": row.ID, "propertyId": propertyID,
			"type": rowValue.Type, "value": string(value), "spans": spans, "now": now,
		}).Execute(); err != nil {
			return err
		}
		if rowValue.Type == graphValueTypeRelation {
			if err := replaceDatabaseRelationProjection(
				app, owner, workspaceID, pageID, row.DataSourceID,
				row.ID, propertyID, string(value), now,
			); err != nil {
				return err
			}
		} else if !knownNew {
			if err := deleteDatabaseRelationProjection(app, row.ID, propertyID); err != nil {
				return err
			}
		}
		if rowValue.Type == graphValueTypeFiles || !knownNew {
			if err := replaceProductFileRowReferences(app, workspaceID, pageID, row.ID, propertyID, cell); err != nil {
				return err
			}
		}
	}
	rowReferences.store(graphEntityReference{
		ID: row.ID, Owner: owner, PageID: pageID, DataSourceID: row.DataSourceID,
	})
	// The database's page-level rules follow the people its values name.
	if err := reconcileDatabasePageRuleGrants(app, row.DataSourceID, row.ID); err != nil {
		return err
	}
	return finishDatabaseRowValueHistory(app, row.ID, actorID, versionsBy, historyChanged, commentMarks, created)
}

// deleteDatabaseRowValueProjection removes a cleared cell's projection with
// its relation links and file references.
func deleteDatabaseRowValueProjection(app core.App, workspaceID, pageID, rowID, propertyID string, knownNew bool) error {
	if knownNew {
		return nil
	}
	if _, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_database_row_values}} WHERE [[row_id]] = {:rowId} AND [[property_id]] = {:propertyId}
	`).Bind(dbx.Params{"rowId": rowID, "propertyId": propertyID}).Execute(); err != nil {
		return err
	}
	if err := deleteDatabaseRelationProjection(app, rowID, propertyID); err != nil {
		return err
	}
	return deleteProductFileRowReferences(app, workspaceID, pageID, rowID, propertyID)
}
