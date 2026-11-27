package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

const (
	databasePropertyConversionRunnerStoreKey = "leafenDatabasePropertyConversionRunner"
	databasePropertyConversionCellsPerTurn   = 64
	databasePropertyConversionFailureDelay   = 200 * time.Millisecond
)

var errDatabasePropertyConversionSettled = errors.New("database property conversion already settled")

type storedDatabasePropertyConversion struct {
	ID           string         `db:"id"`
	Owner        string         `db:"owner"`
	WorkspaceID  string         `db:"workspace_id"`
	PageID       string         `db:"page_id"`
	DataSourceID string         `db:"data_source_id"`
	PropertyID   string         `db:"property_id"`
	ActorID      string         `db:"actor_id"`
	FromType     string         `db:"from_type"`
	FromConfig   string         `db:"from_config"`
	ToType       string         `db:"to_type"`
	Status       string         `db:"status"`
	Processed    int            `db:"processed"`
	RestoreOf    sql.NullString `db:"restore_of"`
	// ListedOptions marks a conversion to a select or multi-select whose
	// options are those its request listed (database_property_conversion_listed.go).
	ListedOptions bool `db:"listed_options"`
}

const databasePropertyConversionSelect = `
	SELECT [[id]],[[owner]],[[workspace_id]],[[page_id]],[[data_source_id]],[[property_id]],[[actor_id]],
		[[from_type]],[[from_config]],[[to_type]],[[status]],[[processed]],[[restore_of]],[[listed_options]]
	FROM {{leafen_database_property_conversions}}`

// recordDatabasePropertyConversion starts a conversion when a property upsert
// changes the stored type to one that reads cells differently. It runs inside
// the graph mutation that changes the type. A change back to a type the
// property had returns that type's settings, which the property takes in
// place of those asked for, unless asAsked: then it keeps those asked for,
// as Notion's API does (seen 2026-11-12: a number made text and a number
// again with format percent, or none, is a percent, or a plain number; a
// formula takes its new expression, a status with none named Notion's three
// options), its cells still coming back. Made a select or multi-select
// asAsked, it keeps the options asked for and its cells read by them
// (database_property_conversion_listed.go).
func recordDatabasePropertyConversion(
	app core.App, owner, workspaceID, pageID, actorID string, property graphProperty, asAsked bool,
) (json.RawMessage, error) {
	stored, exists, err := loadDatabasePropertyRecord(app, pageID, property.ID)
	if err != nil || !exists {
		return nil, err
	}
	fromType, source := stored.Type, stored.DataSourceID
	// A sprint's own properties keep their type, and its relations stay two-way
	// to the same database, as Notion's (seen 2026-11-07).
	if source == property.DataSourceID {
		if kind, err := databaseSprintManagedProperty(app, source, property.ID); err != nil {
			return nil, err
		} else if kind != "" && fromType != "" && fromType != property.Type {
			return nil, graphInputError(errorCodeGraphInvalid, "This property belongs to the database's type, so its type can't change.")
		} else if (kind == databaseSprintManagedTaskSprint || kind == databaseSprintManagedSprintTasks) && len(property.Config) > 0 {
			next := map[string]any{}
			if json.Unmarshal(property.Config, &next) == nil &&
				(next["relatedPropertyId"] != stored.Config["relatedPropertyId"] || next["relationDatabaseId"] != stored.Config["relationDatabaseId"]) {
				return nil, errDatabaseSprintRelationPair
			}
		}
	}
	if fromType == "" || fromType == property.Type || source != property.DataSourceID {
		return nil, nil
	}
	if locked, err := databaseTaskRoleTypeLocked(app, source, property.ID, fromType, property.Type); err != nil {
		return nil, err
	} else if locked {
		return nil, graphInputError(errorCodeGraphInvalid, "This property belongs to the database's type, so its type can't change.")
	}
	if graphValueTypesShareRepresentation(fromType, property.Type) {
		if fromType == graphValueTypeStatus || property.Type == graphValueTypeStatus {
			return recordDatabaseOptionTypeChange(app, owner, workspaceID, pageID, actorID, stored, property, asAsked)
		}
		// A URL, email or phone made text shows its value linked where it leads.
		if property.Type == graphValueTypeRichText {
			return nil, linkDatabaseTextCells(app, owner, pageID, property.ID)
		}
		return nil, nil
	}
	if _, err := newDatabasePropertyConversion(fromType, property.Type, nil, nil, databasePropertyConversionNames{}); err != nil {
		// Title and place keep their own lifecycles.
		return nil, nil
	}
	var pending int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_database_property_conversions}}
		WHERE [[property_id]]={:propertyId} AND [[status]]='pending'`,
	).Bind(dbx.Params{"propertyId": property.ID}).Row(&pending); err != nil {
		return nil, err
	}
	if pending > 0 {
		return nil, graphConflictError(errorCodeGraphRevisionConflict, "The property type is being changed. Try again shortly.")
	}
	// A restore marks the conversion it undoes as restored first; link the
	// new conversion so its cells can take their original values back.
	restoreOf := sql.NullString{}
	if err := app.DB().NewQuery(`SELECT [[id]] FROM {{leafen_database_property_conversions}} AS [[restored]]
		WHERE [[property_id]]={:propertyId} AND [[status]]='restored' AND [[from_type]]={:toType} AND [[to_type]]={:fromType}
			AND NOT EXISTS (SELECT 1 FROM {{leafen_database_property_conversions}} AS [[restoring]]
				WHERE [[restoring]].[[restore_of]]=[[restored]].[[id]])
		ORDER BY [[created_at]] DESC LIMIT 1`,
	).Bind(dbx.Params{"propertyId": property.ID, "fromType": fromType, "toType": property.Type}).Row(&restoreOf.String); err == nil {
		restoreOf.Valid = true
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	// A relation changes to another type, as Notion's does (seen 2026-11-04),
	// and comes back with its cells when that is undone. Made a relation by
	// hand, it points where it was just chosen to point, its cells read from
	// what the old ones name (databasePropertyConversion.RelationFrom), never
	// taken back with settings it had.
	toRelation := property.Type == graphValueTypeRelation && !restoreOf.Valid
	listed := asAsked && databasePropertyConversionListsOptions(property.Type)
	// Changed back by hand to a type it had, the property takes back that
	// type's settings and the cells kept from then, as Notion's keeps a
	// value through changes of type (seen 2026-11-07: text to number to
	// select and back gave the text back, but where a cell was edited
	// meanwhile; select to text and back its options and their colors).
	var kept json.RawMessage
	if !restoreOf.Valid && !toRelation {
		returned, found, err := markDatabasePropertyConversionReturn(app, property.ID, fromType, property.Type)
		if err != nil {
			return nil, err
		}
		if found {
			restoreOf = sql.NullString{String: returned.ID, Valid: true}
			if !asAsked {
				kept = json.RawMessage(returned.FromConfig)
			}
		}
	}
	fromConfig, err := json.Marshal(stored.Config)
	if err != nil || string(fromConfig) == "null" {
		fromConfig = []byte("{}")
	}
	id := uuid.NewString()
	if _, err := app.DB().NewQuery(`INSERT INTO {{leafen_database_property_conversions}}
		([[id]],[[owner]],[[workspace_id]],[[page_id]],[[data_source_id]],[[property_id]],[[actor_id]],
		 [[from_type]],[[from_config]],[[to_type]],[[status]],[[created_at]],[[restore_of]])
		VALUES ({:id},{:owner},{:workspaceId},{:pageId},{:dataSourceId},{:propertyId},{:actorId},
		 {:fromType},{:fromConfig},{:toType},'pending',{:now},{:restoreOf})`,
	).Bind(dbx.Params{
		"id": id, "owner": owner, "workspaceId": workspaceID, "pageId": pageID,
		"dataSourceId": property.DataSourceID, "propertyId": property.ID, "actorId": actorID,
		"fromType": fromType, "fromConfig": string(fromConfig), "toType": property.Type,
		"now": time.Now().UTC().UnixMilli(), "restoreOf": restoreOf,
	}).Execute(); err != nil {
		return nil, err
	}
	if listed {
		if err := markDatabasePropertyConversionListed(app, id); err != nil {
			return nil, err
		}
	}
	notifyDatabasePropertyConversionAfterCommit(app)
	if kept == nil && property.Type == graphValueTypeStatus {
		// Made a status, it takes Notion's groups and options, which its
		// cells are then read by (databaseStatusConfigMadeFrom).
		return databaseStatusConfigMadeFrom(fromType, property.Config, newStatusWords(app, actorID))
	}
	return kept, nil
}

// pendingDatabasePropertyConversions lists the properties this mutation
// upserted that still convert cells, so the editor can wait and reread them.
func pendingDatabasePropertyConversions(app core.App, pageID string, operations []graphOperation) ([]string, error) {
	ids := []string{}
	for _, operation := range operations {
		if operation.Kind == graphOperationKindUpsertProperty && operation.Property != nil {
			ids = append(ids, operation.Property.ID)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	pending := []string{}
	err = app.DB().NewQuery(`SELECT DISTINCT [[property_id]] FROM {{leafen_database_property_conversions}}
		WHERE [[page_id]]={:pageId} AND [[status]]='pending'
			AND [[property_id]] IN (SELECT [[value]] FROM json_each({:ids}))
		ORDER BY [[property_id]]`).Bind(dbx.Params{"pageId": pageID, "ids": string(encoded)}).Column(&pending)
	if len(pending) == 0 {
		return nil, err
	}
	return pending, err
}

type databasePropertyConversionCell struct {
	RowID string `db:"row_id"`
	Type  string `db:"type"`
	Value string `db:"value"`
	// Spans are a text cell's marks and mentions, read for a relation it becomes.
	Spans string `db:"spans"`
}

type databasePropertyConversionTurn struct {
	conversion storedDatabasePropertyConversion
	originals  []databasePropertyConversionCell
	cleared    []databaseMutationCell
	complete   bool
	// refilled are rows with no value now that a conversion before kept a
	// value of; stored is the value each cell reads from and ownConfig the
	// settings of the cells' own values (database_property_conversion_stored.go).
	refilled  []databasePropertyConversionCell
	stored    map[string]databasePropertyConversionStored
	ownConfig map[string]any
	// produced is what this turn leaves in each cell, kept for the
	// conversions after it.
	produced map[string]json.RawMessage
}

// advanceDatabasePropertyConversion converts one bounded batch of cells in a
// graph mutation, clearing cells the target type cannot read and keeping each
// original cell for recovery.
func (p *pocketBaseYgoPersistence) advanceDatabasePropertyConversion(ctx context.Context, id string) error {
	conversion, err := loadDatabasePropertyConversion(p.app, id)
	if errors.Is(err, sql.ErrNoRows) || err == nil && conversion.Status != databasePropertyConversionPending {
		return nil
	}
	if err != nil {
		return err
	}
	turn := databasePropertyConversionTurn{}
	_, err = p.applyContentGraphMutationContext(ctx, conversion.ActorID, conversion.PageID, graphMutationInput{
		MutationID: fmt.Sprintf("property-conversion:%s:%d", conversion.ID, conversion.Processed),
	}, contentGraphMutationHooks{
		SuppressAutomationEvents: true, PreserveRowEditStamps: true, KeepTrashedRowsInStep: true,
		Authorize: func(txApp core.App) (pageAuthorization, error) {
			workspace, err := txApp.FindRecordById(workspacesCollection, conversion.WorkspaceID)
			if err != nil {
				return pageAuthorization{}, err
			}
			return systemJobSubjectFor(workspace).authorizePageBody(txApp, conversion.PageID, permissionWorkspaceWrite)
		},
		PrepareInput: func(txApp core.App, _ pageAuthorization, doc *crdt.Doc, input graphMutationInput) (graphMutationInput, error) {
			var err error
			turn, err = planDatabasePropertyConversionTurn(txApp, conversion.ID)
			if err != nil {
				return input, err
			}
			input.Operations, err = databasePropertyConversionOperations(txApp, &turn)
			return input, err
		},
		AfterApply: func(txApp core.App, _ pageAuthorization, doc *crdt.Doc) error {
			return settleDatabasePropertyConversionTurn(txApp, turn)
		},
	})
	if errors.Is(err, errDatabasePropertyConversionSettled) {
		// The property changed under the conversion; stop converting and keep
		// what was recorded for recovery.
		now := time.Now().UTC()
		_, err = p.app.DB().NewQuery(`UPDATE {{leafen_database_property_conversions}}
			SET [[status]]='completed',[[completed_at]]={:now},[[expires_at]]={:expires}
			WHERE [[id]]={:id} AND [[status]]='pending'`).Bind(dbx.Params{
			"id": conversion.ID, "now": now.UnixMilli(), "expires": now.Add(recoverableBlockTrashRetention).UnixMilli(),
		}).Execute()
		notifyDatabaseUniqueIDRunnerAfterCommit(p.app)
	}
	return err
}

func loadDatabasePropertyConversion(app core.App, id string) (storedDatabasePropertyConversion, error) {
	conversion := storedDatabasePropertyConversion{}
	err := app.DB().NewQuery(databasePropertyConversionSelect + ` WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": id}).One(&conversion)
	return conversion, err
}

func planDatabasePropertyConversionTurn(app core.App, id string) (databasePropertyConversionTurn, error) {
	conversion, err := loadDatabasePropertyConversion(app, id)
	if err != nil || conversion.Status != databasePropertyConversionPending {
		return databasePropertyConversionTurn{}, errors.Join(errDatabasePropertyConversionSettled, err)
	}
	current, exists, err := loadDatabasePropertyRecord(app, conversion.PageID, conversion.PropertyID)
	if err != nil {
		return databasePropertyConversionTurn{}, err
	}
	if !exists || current.Type != conversion.ToType {
		return databasePropertyConversionTurn{}, errDatabasePropertyConversionSettled
	}
	turn := databasePropertyConversionTurn{conversion: conversion}
	// A change in place left each cell the type it was written as: a text
	// cell under a URL converts as the URL's (databasePropertyTypesStoredAlike).
	// Cells are read by their rows' ids, as Notion's conversions read theirs
	// (databasePropertyConversionTurn.cells).
	if err := app.DB().NewQuery(`SELECT [[cell]].[[row_id]],[[cell]].[[type]],[[cell]].[[value]],[[cell]].[[spans]]
		FROM {{leafen_database_row_values}} AS [[cell]]
		WHERE [[cell]].[[owner]]={:owner} AND [[cell]].[[page_id]]={:pageId} AND [[cell]].[[property_id]]={:propertyId}
			AND [[cell]].[[type]] IN (SELECT [[value]] FROM json_each({:fromTypes}))
		ORDER BY [[cell]].[[row_id]] LIMIT {:limit}`,
	).Bind(dbx.Params{
		"owner": conversion.Owner, "pageId": conversion.PageID, "propertyId": conversion.PropertyID,
		"fromTypes": databasePropertyTypesStoredAlike(conversion.FromType), "limit": databasePropertyConversionCellsPerTurn,
	}).All(&turn.originals); err != nil {
		return turn, err
	}
	return planDatabasePropertyConversionStored(app, turn)
}

// databasePropertyConversionTurnCell is a cell a turn reads; original when
// it holds a value now, not when a conversion before kept the value it
// reads from.
type databasePropertyConversionTurnCell struct {
	databasePropertyConversionCell
	original bool
}

// cells are the turn's cells in the order it reads them: by their rows' ids,
// whether a cell holds a value now or only one a conversion before kept, as
// Notion's conversions read theirs (seen 2026-11-11: rows made in one order
// made their options in their ids' order, after one change of type or two).
func (turn databasePropertyConversionTurn) cells() []databasePropertyConversionTurnCell {
	cells := make([]databasePropertyConversionTurnCell, 0, len(turn.originals)+len(turn.refilled))
	originals, refilled := turn.originals, turn.refilled
	for len(originals) > 0 || len(refilled) > 0 {
		if len(refilled) == 0 || (len(originals) > 0 && originals[0].RowID <= refilled[0].RowID) {
			cells = append(cells, databasePropertyConversionTurnCell{originals[0], true})
			originals = originals[1:]
			continue
		}
		cells = append(cells, databasePropertyConversionTurnCell{refilled[0], false})
		refilled = refilled[1:]
	}
	return cells
}

func databasePropertyConversionOperations(
	app core.App, turn *databasePropertyConversionTurn,
) ([]graphOperation, error) {
	conversion := turn.conversion
	stored, exists, err := loadDatabasePropertyRecord(app, conversion.PageID, conversion.PropertyID)
	if err != nil || !exists {
		return nil, errors.Join(errDatabasePropertyConversionSettled, err)
	}
	fromConfig := turn.ownConfig
	if fromConfig == nil {
		fromConfig = map[string]any{}
		if json.Unmarshal([]byte(conversion.FromConfig), &fromConfig) != nil {
			return nil, errNotionDatabaseRowPageProjectionInvalid
		}
	}
	targetConfig := stored.Config
	if targetConfig == nil {
		targetConfig = map[string]any{}
	}
	names := databasePropertyConversionNamesIn(app)
	rules, err := databasePropertyConversionRulesFor(conversion.ListedOptions, conversion.FromType, conversion.ToType,
		fromConfig, targetConfig, names)
	if err != nil {
		return nil, err
	}
	// Each cell converts from the value it keeps through changes of type, by
	// the rules from the type that value was written as; options any of them
	// make are the target's for the next.
	options := rules.Options()
	read := func(value databasePropertyConversionStored) (json.RawMessage, []graphTextSpan, error) {
		if len(value.value) == 0 {
			return nil, nil, nil
		}
		if value.valueType == conversion.ToType && !conversion.ListedOptions {
			// Back in the type it was written as, a value is itself.
			return value.value, nil, nil
		}
		target := make(map[string]any, len(targetConfig)+1)
		for key, setting := range targetConfig {
			target[key] = setting
		}
		items := make([]any, 0, len(options))
		for _, option := range options {
			items = append(items, option)
		}
		target["options"] = items
		cellRules, err := databasePropertyConversionRulesFor(conversion.ListedOptions, value.valueType, conversion.ToType,
			value.config, target, names)
		if err != nil {
			return nil, nil, err
		}
		converted, err := cellRules.Convert(value.value)
		if err != nil {
			return nil, nil, err
		}
		options = cellRules.Options()
		return converted, cellRules.Spans(value.value, converted), nil
	}
	operations := []graphOperation{}
	turn.produced = map[string]json.RawMessage{}
	for _, cell := range turn.cells() {
		from, found := turn.stored[cell.RowID]
		if !found {
			from = databasePropertyConversionStored{valueType: conversion.FromType, value: json.RawMessage(cell.Value), config: fromConfig}
		}
		var value json.RawMessage
		var spans []graphTextSpan
		if conversion.ToType == graphValueTypeRelation && from.valueType != graphValueTypeRelation {
			// A relation reads the pages a cell names now (RelationFrom).
			if cell.original {
				value, err = rules.RelationFrom(json.RawMessage(cell.Value), json.RawMessage(cell.Spans))
			}
		} else {
			value, spans, err = read(from)
		}
		if err != nil {
			return nil, err
		}
		turn.produced[cell.RowID] = value
		if value == nil {
			// A cell refilled as nothing has none to clear.
			if cell.original {
				turn.cleared = append(turn.cleared, databaseMutationCell{
					RowID: cell.RowID, PropertyID: conversion.PropertyID, DataSourceID: conversion.DataSourceID,
				})
			}
			continue
		}
		row, err := databaseRowRecordOperation(app, conversion.PageID, cell.RowID)
		if err != nil {
			return nil, err
		}
		row.Values = map[string]graphRowValue{conversion.PropertyID: {Type: conversion.ToType, Value: value, Spans: spans}}
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertRow, Row: &row})
	}
	if _, hasOptions := databasePropertyConversionOptionTypes[conversion.ToType]; hasOptions {
		items := make([]any, 0, len(options))
		for _, option := range options {
			items = append(items, option)
		}
		targetConfig["options"] = items
	}
	config, err := json.Marshal(targetConfig)
	if err != nil {
		return nil, err
	}
	property := graphOperation{Kind: graphOperationKindUpsertProperty, Property: &graphProperty{
		ID: conversion.PropertyID, DataSourceID: conversion.DataSourceID, Type: conversion.ToType,
		Name: stored.Name, Position: stored.Position, Config: config,
	}}
	return append([]graphOperation{property}, operations...), nil
}

func databasePropertyConversionNamesIn(app core.App) databasePropertyConversionNames {
	return databasePropertyConversionNames{
		Person: databasePropertyConversionPersonName(app),
		Page:   func(id string) string { return databaseRowPageTitle(app, id) },
		Rows: func(dataSourceID string) []databasePropertyConversionRow {
			rows := []databasePropertyConversionRow{}
			if err := app.DB().NewQuery(`
				SELECT [[row]].[[id]] AS [[id]], COALESCE([[title]].[[value]], '') AS [[title]]
				FROM {{leafen_database_rows}} AS [[row]]
				LEFT JOIN {{leafen_database_row_values}} AS [[title]]
					ON [[title]].[[row_id]] = [[row]].[[id]] AND [[title]].[[type]] = 'title'
				WHERE ` + databaseSourceRowsSQL("[[row]]", "{:source}") + `
					AND NOT EXISTS (SELECT 1 FROM {{leafen_database_row_trash}} AS [[trash]] WHERE [[trash]].[[row_id]] = [[row]].[[id]])
				ORDER BY [[row]].[[position]], [[row]].[[id]]`,
			).Bind(dbx.Params{"source": dataSourceID}).All(&rows); err != nil {
				return nil
			}
			for index := range rows {
				rows[index].Title = databaseRowTitleText(rows[index].Title)
			}
			return rows
		},
	}
}

func databasePropertyConversionPersonName(app core.App) func(string) string {
	return func(id string) string {
		record, err := app.FindRecordById("users", id)
		if err != nil {
			return ""
		}
		if name := strings.TrimSpace(record.GetString("name")); name != "" {
			return name
		}
		return record.GetString("email")
	}
}

func settleDatabasePropertyConversionTurn(app core.App, turn databasePropertyConversionTurn) error {
	conversion := turn.conversion
	if len(turn.cleared) > 0 {
		if err := deleteDatabaseMutationCellProjections(app, storedDatabaseMutationDeletion{
			OwnerID: conversion.Owner, PageID: conversion.PageID,
		}, turn.cleared); err != nil {
			return err
		}
	}
	// Each cell read keeps its value before (a row with none as type "") and
	// what this conversion left there, for the conversions after it.
	for _, cell := range turn.cells() {
		valueType, value := cell.Type, cell.Value
		if !cell.original {
			valueType, value = "", "null"
		}
		produced := "null"
		if left := turn.produced[cell.RowID]; len(left) > 0 {
			produced = string(left)
		}
		if _, err := app.DB().NewQuery(`INSERT OR IGNORE INTO {{leafen_database_property_conversion_cells}}
			([[conversion_id]],[[row_id]],[[value_type]],[[value]],[[produced]]) VALUES ({:id},{:rowId},{:type},{:value},{:produced})`,
		).Bind(dbx.Params{
			"id": conversion.ID, "rowId": cell.RowID, "type": valueType, "value": value, "produced": produced,
		}).Execute(); err != nil {
			return err
		}
	}
	now := time.Now().UTC()
	query := `UPDATE {{leafen_database_property_conversions}} SET [[processed]]=[[processed]]+{:count}
		WHERE [[id]]={:id} AND [[status]]='pending'`
	if turn.complete {
		query = `UPDATE {{leafen_database_property_conversions}} SET [[processed]]=[[processed]]+{:count},
			[[status]]='completed',[[completed_at]]={:now},[[expires_at]]={:expires}
			WHERE [[id]]={:id} AND [[status]]='pending'`
	}
	result, err := app.DB().NewQuery(query).Bind(dbx.Params{
		"id": conversion.ID, "count": len(turn.originals) + len(turn.refilled), "now": now.UnixMilli(),
		"expires": now.Add(recoverableBlockTrashRetention).UnixMilli(),
	}).Execute()
	if turn.complete {
		// An ID property waits for its cells to convert before numbering.
		notifyDatabaseUniqueIDRunnerAfterCommit(app)
	}
	return requireDatabaseMutationJobUpdate(result, err)
}
