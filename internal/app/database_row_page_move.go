package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Moving a row to another database of its page keeps every stored value the
// target can hold. The title always moves; any other value moves to the
// target property with the same name and type. Select and multi-select values
// move by option name, adding an option the target lacks with its color;
// status values move by option name; relations move only between properties
// that relate to the same database. A value whose property the target lacks
// brings that property along, by its name, type and settings, as Notion's
// move adds it to the target database (seen 2026-10-31); a relation, or a
// name the target gives another type, can't come, and the mover sees those
// values before they are dropped. The row leaves its parent item, its
// sub-items stay behind as top-level rows, its dependency edges end, and rows
// that related to it through the old database let it go: one-way relations
// and two-way ones within the data source in the move itself, two-way ones
// to other data sources through the relation pair runner. A unique
// ID belongs to its database and is not carried.

var errDatabaseRowPageMoveDropsValues = errors.New(errorCodeDatabaseRowPageMoveDropsValues)

// Cells of these types are computed or numbered by their own database.
var databaseRowPageMoveUncarriedTypes = map[string]struct{}{
	graphValueTypeUniqueID: {}, graphValueTypeCreatedTime: {}, graphValueTypeLastEditedTime: {},
	graphValueTypeCreatedBy: {}, graphValueTypeLastEditedBy: {}, graphValueTypeRollup: {},
	graphValueTypeFormula: {}, graphValueTypeButton: {}, graphValueTypeLocation: {},
	graphValueTypeLastVisitedTime: {},
}

type databaseRowPageProperty struct {
	ID       string `db:"id"`
	Type     string `db:"type"`
	Name     string `db:"name"`
	Config   string `db:"config"`
	Position string `db:"position"`
}

// databaseRowPageMoveValues is what the moved rows' cells become in the
// target, row by row.
type databaseRowPageMoveValues struct {
	Values map[string]map[string]graphRowValue
	// Options holds the full option list of each target property the move
	// adds options to.
	Options map[string][]map[string]any
	// Dropped names each source property whose values some moved row loses.
	Dropped []databaseRowPageMoveDroppedProperty
	// Added are the source properties the target takes on for the moved
	// values, under IDs of their own.
	Added []graphProperty
	// Sources are each row's stored cells, which leave the source with it.
	Sources map[string][]string
	TitleID string
}

type databaseRowPageMoveOptions struct {
	options []map[string]any
	byName  map[string]string
	added   bool
}

func newDatabaseRowPageMoveOptions(configJSON string) *databaseRowPageMoveOptions {
	config := map[string]any{}
	_ = json.Unmarshal([]byte(configJSON), &config)
	result := &databaseRowPageMoveOptions{
		options: databasePropertyConversionOptions(config), byName: map[string]string{},
	}
	for _, option := range result.options {
		id, _ := option["id"].(string)
		name, _ := option["name"].(string)
		name = strings.TrimSpace(name)
		if _, exists := result.byName[name]; id != "" && name != "" && !exists {
			result.byName[name] = id
		}
	}
	return result
}

// option returns the ID of the option with this name, adding it when the
// property may, however many options it has; an empty ID means the target
// cannot hold the value.
func (options *databaseRowPageMoveOptions) option(name, color string, add bool) string {
	if id := options.byName[name]; id != "" || !add {
		return id
	}
	if name == "" {
		return ""
	}
	if _, valid := notionDatabaseRowPageOptionColors[color]; !valid {
		color = "default"
	}
	id := core.GenerateDefaultRandomId()
	options.options = append(options.options, map[string]any{"id": id, "name": name, "color": color})
	options.byName[name] = id
	options.added = true
	return id
}

func normalizedDatabaseRowPagePropertyName(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func loadDatabaseRowPageProperties(
	app core.App,
	authority databaseRowPageAuthority,
	pageID, dataSourceID string,
) ([]databaseRowPageProperty, error) {
	properties := []databaseRowPageProperty{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[type]], [[name]], [[config]], [[position]]
		FROM {{leafen_database_properties}}
		WHERE [[owner]]={:ownerId} AND [[workspace_id]]={:workspaceId}
			AND [[page_id]]={:pageId} AND [[data_source_id]]={:dataSourceId}
			AND ` + databasePropertyNotTrashedSQL("[[id]]") + `
		ORDER BY [[position]], [[id]]
	`).Bind(dbx.Params{
		"ownerId": authority.OwnerID, "workspaceId": authority.WorkspaceID,
		"pageId": pageID, "dataSourceId": dataSourceID,
	}).All(&properties)
	return properties, err
}

func databaseRowPageMoveTitle(properties []databaseRowPageProperty) (databaseRowPageProperty, error) {
	var title *databaseRowPageProperty
	for index := range properties {
		if properties[index].Type != graphValueTypeTitle {
			continue
		}
		if title != nil {
			return databaseRowPageProperty{}, errDatabaseRowPageConflict
		}
		title = &properties[index]
	}
	if title == nil {
		return databaseRowPageProperty{}, errDatabaseRowPageConflict
	}
	return *title, nil
}

// databaseRowPageMoveValueEmpty reports a cell that holds nothing to lose.
func databaseRowPageMoveValueEmpty(raw json.RawMessage) bool {
	switch strings.TrimSpace(string(raw)) {
	case "", "null", `""`, "[]", "{}", "false":
		return true
	}
	return false
}

// planDatabaseRowPageMoveValues maps the cells of the moved rows onto the
// target data source, on the rows' container page or another page of their
// workspace. Options added for one row serve the rows after it.
func planDatabaseRowPageMoveValues(
	app core.App,
	authority databaseRowPageAuthority,
	targetPageID, targetDataSourceID string,
	rowIDs []string,
) (databaseRowPageMoveValues, error) {
	result := databaseRowPageMoveValues{
		Values: map[string]map[string]graphRowValue{}, Options: map[string][]map[string]any{},
		Sources: map[string][]string{},
	}
	// The target takes added rows (database_added_rows.go): People's are its
	// people. One it is in, or one that takes none, is refused as such
	// (rowIntoDatabaseRefused); one moved meanwhile is the conflict.
	if targetDataSourceID == authority.DataSourceID {
		return result, rowIntoDatabaseRefused(true)
	}
	var targetCount, takes int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*), COALESCE(MAX(`+databaseSourceTakesAddedRowsSQL("{:targetId}")+`), 0) FROM {{leafen_data_sources}}
		WHERE [[id]]={:targetId} AND [[owner]]={:ownerId}
			AND [[workspace_id]]={:workspaceId} AND [[page_id]]={:pageId}
	`).Bind(dbx.Params{
		"targetId": targetDataSourceID, "ownerId": authority.OwnerID,
		"workspaceId": authority.WorkspaceID, "pageId": targetPageID,
	}).Row(&targetCount, &takes); err != nil {
		return result, err
	}
	if targetCount != 1 {
		return result, errDatabaseRowPageConflict
	} else if takes == 0 {
		return result, rowIntoDatabaseRefused(false)
	}
	sourceProperties, err := loadDatabaseRowPageProperties(app, authority, authority.ContainerPageID, authority.DataSourceID)
	if err != nil {
		return result, err
	}
	targetProperties, err := loadDatabaseRowPageProperties(app, authority, targetPageID, targetDataSourceID)
	if err != nil {
		return result, err
	}
	sourceTitle, err := databaseRowPageMoveTitle(sourceProperties)
	if err != nil {
		return result, err
	}
	targetTitle, err := databaseRowPageMoveTitle(targetProperties)
	if err != nil {
		return result, err
	}
	result.TitleID = targetTitle.ID
	stored, err := loadGraphRowValues(app, authority.OwnerID, authority.ContainerPageID, rowIDs)
	if err != nil {
		return result, err
	}
	cellsByRow := map[string]map[string]storedGraphRowValue{}
	for _, cell := range stored {
		if cellsByRow[cell.RowID] == nil {
			cellsByRow[cell.RowID] = map[string]storedGraphRowValue{}
		}
		cellsByRow[cell.RowID][cell.PropertyID] = cell
		result.Sources[cell.RowID] = append(result.Sources[cell.RowID], cell.PropertyID)
	}
	targetOptions := map[string]*databaseRowPageMoveOptions{}
	dropped := map[string]struct{}{}
	for _, rowID := range rowIDs {
		values := map[string]graphRowValue{}
		cells := cellsByRow[rowID]
		for _, source := range sourceProperties {
			cell, exists := cells[source.ID]
			if !exists {
				continue
			}
			raw := json.RawMessage(cell.Value)
			if !json.Valid(raw) || !graphValueTypesShareRepresentation(cell.Type, source.Type) {
				return result, errDatabaseRowPageConflict
			}
			if _, uncarried := databaseRowPageMoveUncarriedTypes[source.Type]; uncarried ||
				databaseRowPageMoveValueEmpty(raw) {
				continue
			}
			target, found := targetTitle, source.ID == sourceTitle.ID
			if !found {
				target, found = databaseRowPageMoveTarget(source, targetProperties, targetTitle.ID)
			}
			if !found {
				var added *graphProperty
				if added, err = databaseRowPageMoveAddedProperty(source, targetProperties, targetDataSourceID); err != nil {
					return result, err
				}
				if added != nil {
					result.Added = append(result.Added, *added)
					target = databaseRowPageProperty{
						ID: added.ID, Type: added.Type, Name: added.Name, Config: string(added.Config), Position: added.Position,
					}
					targetProperties, found = append(targetProperties, target), true
				}
			}
			complete := false
			if found {
				options := targetOptions[target.ID]
				if options == nil {
					options = newDatabaseRowPageMoveOptions(target.Config)
					targetOptions[target.ID] = options
				}
				var carried json.RawMessage
				carried, complete, err = databaseRowPageMoveValue(
					source, target, raw, options, rowID, targetDataSourceID,
				)
				if err != nil {
					return result, err
				}
				if carried != nil {
					value := graphRowValue{Type: target.Type, Value: carried}
					// Text that arrives unchanged keeps its formatting.
					if spans := databaseStoredSpans(cell.Spans); len(spans) > 0 && string(carried) == string(raw) &&
						(target.Type == graphValueTypeTitle || target.Type == graphValueTypeRichText) {
						value.Spans = spans
					}
					values[target.ID] = value
				}
			}
			if _, named := dropped[source.ID]; !complete && !named {
				dropped[source.ID] = struct{}{}
				result.Dropped = append(result.Dropped, databaseRowPageMoveDroppedProperty{
					PropertyID: source.ID, Name: source.Name, Type: source.Type,
				})
			}
		}
		if len(values) == 0 {
			// A row write names at least one cell; an untitled row keeps no title.
			values[targetTitle.ID] = graphRowValue{Type: graphValueTypeTitle, Value: graphRowValueCleared}
		}
		result.Values[rowID] = values
	}
	for id, options := range targetOptions {
		if options.added {
			result.Options[id] = options.options
		}
	}
	return result, nil
}

// databaseRowPageMoveTreeRow is a moved row: the row itself and every
// sub-item under it, which move with it as in Notion.
type databaseRowPageMoveTreeRow struct {
	ID       string `db:"id"`
	ParentID string `db:"subitem_parent_id"`
	Depth    int    `db:"depth"`
}

// databaseRowPageMoveTree lists the row and its sub-items, parents first.
// A move is still one change, so a tree past half a change's operations is
// refused as a conflict.
func databaseRowPageMoveTree(app core.App, authority databaseRowPageAuthority) ([]databaseRowPageMoveTreeRow, error) {
	tree, err := databaseRowTreeRead(app, authority, false)
	if err != nil {
		return nil, err
	}
	rows := make([]databaseRowPageMoveTreeRow, len(tree))
	for index, row := range tree {
		rows[index] = databaseRowPageMoveTreeRow{ID: row.ID, ParentID: row.ParentID, Depth: row.Depth}
	}
	return rows, nil
}

func databaseRowPageMoveTreeIDs(tree []databaseRowPageMoveTreeRow) []string {
	ids := make([]string, len(tree))
	for index, row := range tree {
		ids[index] = row.ID
	}
	return ids
}

// databaseRowPageMoveTreeOperations writes the moved rows in the target,
// after its last row (tail), as Notion puts a page moved into a database:
// the row leaves its parent item and each sub-item keeps its own.
func databaseRowPageMoveTreeOperations(
	tree []databaseRowPageMoveTreeRow, values databaseRowPageMoveValues, targetDataSourceID, tail string,
) ([]graphOperation, error) {
	operations := make([]graphOperation, 0, len(tree))
	position := tail
	for index, row := range tree {
		var parentID *string
		if index > 0 {
			parentID = optionalDatabaseRowParent(row.ParentID)
		}
		var err error
		if position, err = databasePositionAfter(position); err != nil {
			return nil, err
		}
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertRow, Row: &graphDatabaseRow{
			ID: row.ID, DataSourceID: targetDataSourceID, SubitemParentID: parentID,
			Position: position, Values: values.Values[row.ID],
		}})
	}
	return operations, nil
}

// databaseRowPageMoveAddedProperty is the property a target takes on for a
// value it has nowhere to hold: the source's own, under an ID made from the
// target and the source property, after the target's last. A relation, or a
// name the target already gives another property, adds none.
func databaseRowPageMoveAddedProperty(
	source databaseRowPageProperty,
	targets []databaseRowPageProperty,
	targetDataSourceID string,
) (*graphProperty, error) {
	if source.Type == graphValueTypeRelation || source.Type == graphValueTypeTitle {
		return nil, nil
	}
	name, tail := normalizedDatabaseRowPagePropertyName(source.Name), ""
	for _, target := range targets {
		if normalizedDatabaseRowPagePropertyName(target.Name) == name {
			return nil, nil
		}
		if target.Position > tail {
			tail = target.Position
		}
	}
	position, err := databasePositionAfter(tail)
	if err != nil {
		return nil, err
	}
	config := json.RawMessage(source.Config)
	if !json.Valid(config) {
		config = json.RawMessage(`{}`)
	}
	digest := sha256.Sum256([]byte("leafen:moved-property\x00" + targetDataSourceID + "\x00" + source.ID))
	return &graphProperty{
		ID: "m" + hex.EncodeToString(digest[:])[:14], DataSourceID: targetDataSourceID, Type: source.Type,
		Name: source.Name, Position: position, Config: config,
	}, nil
}

func databaseRowPageMoveTarget(
	source databaseRowPageProperty,
	targets []databaseRowPageProperty,
	targetTitleID string,
) (databaseRowPageProperty, bool) {
	name := normalizedDatabaseRowPagePropertyName(source.Name)
	for _, target := range targets {
		if target.ID != targetTitleID && target.Type == source.Type &&
			normalizedDatabaseRowPagePropertyName(target.Name) == name {
			return target, true
		}
	}
	return databaseRowPageProperty{}, false
}

// databaseRowPageMoveValue returns the target cell for one source cell and
// whether all of the value survives.
func databaseRowPageMoveValue(
	source, target databaseRowPageProperty,
	raw json.RawMessage,
	options *databaseRowPageMoveOptions,
	rowID, targetDataSourceID string,
) (json.RawMessage, bool, error) {
	switch source.Type {
	case graphValueTypeSelect, graphValueTypeStatus, graphValueTypeMultiSelect:
		ids := []string{}
		if source.Type == graphValueTypeMultiSelect {
			if json.Unmarshal(raw, &ids) != nil {
				return nil, false, errDatabaseRowPageConflict
			}
		} else {
			var id string
			if json.Unmarshal(raw, &id) != nil {
				return nil, false, errDatabaseRowPageConflict
			}
			ids = append(ids, id)
		}
		sourceOptions := map[string]map[string]any{}
		sourceConfig := map[string]any{}
		_ = json.Unmarshal([]byte(source.Config), &sourceConfig)
		for _, option := range databasePropertyConversionOptions(sourceConfig) {
			if id, _ := option["id"].(string); id != "" {
				sourceOptions[id] = option
			}
		}
		moved := []string{}
		for _, id := range ids {
			option := sourceOptions[id]
			name, _ := option["name"].(string)
			color, _ := option["color"].(string)
			if targetID := options.option(
				strings.TrimSpace(name), color, source.Type != graphValueTypeStatus,
			); targetID != "" {
				moved = append(moved, targetID)
			}
		}
		complete := len(moved) == len(ids)
		if len(moved) == 0 {
			return nil, false, nil
		}
		if source.Type != graphValueTypeMultiSelect {
			encoded, err := json.Marshal(moved[0])
			return encoded, complete, err
		}
		encoded, err := json.Marshal(moved)
		return encoded, complete, err
	case graphValueTypeRelation:
		related := graphRelationTarget(source.Type, json.RawMessage(source.Config))
		if related == "" || related != graphRelationTarget(target.Type, json.RawMessage(target.Config)) {
			return nil, false, nil
		}
		ids := []string{}
		if json.Unmarshal(raw, &ids) != nil {
			return nil, false, errDatabaseRowPageConflict
		}
		kept := make([]string, 0, len(ids))
		for _, id := range ids {
			// The row no longer belongs to the database it named itself in.
			if id != rowID || related == targetDataSourceID {
				kept = append(kept, id)
			}
		}
		if len(kept) == 0 {
			return nil, false, nil
		}
		encoded, err := json.Marshal(kept)
		return encoded, len(kept) == len(ids), err
	default:
		return raw, true, nil
	}
}
