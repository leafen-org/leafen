package app

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Formulas, rollups and a row's created and edited times and people are
// computed on read, not stored with the row. Exports compute formulas and
// rollups with the database's own evaluator over the rows the export holds,
// once per database; a rollup reads its targets in whichever exported
// database they live, after that database's own values are computed. Times
// and people come from the row pages. A value the evaluator cannot compute
// stays empty.

var workspaceExportComputedEmpty = json.RawMessage(`""`)

// queryContext is a database's properties as the query rules read them.
func (document *workspaceExportDocument) queryContext(dataSourceID string) (databaseQueryContext, bool) {
	context := databaseQueryContext{
		PropertiesByID:   map[string]storedDatabaseQueryProperty{},
		PropertiesByName: map[string]storedDatabaseQueryProperty{},
		People:           newDatabaseFormulaPeople(nil, document.People),
		// Formulas work on the exporter's clock and write dates in the
		// language the export writes its dates in, as Notion's export does
		// ("2026년 1월 2일 오전 10:30", seen 2026-11-12), and a view's filter
		// matches those words.
		FormulaTime: databaseFormulaTime{Location: document.exportLocation(), Language: document.Language},
	}
	for _, property := range document.Properties[dataSourceID] {
		stored, ok := workspaceExportQueryProperty(property)
		if !ok {
			return context, false
		}
		context.Properties = append(context.Properties, stored)
		context.PropertiesByID[stored.ID] = stored
		if _, exists := context.PropertiesByName[stored.Name]; !exists {
			context.PropertiesByName[stored.Name] = stored
		}
	}
	addDatabaseQueryRowTimestampProperties(&context, dataSourceID)
	// A view filtering or sorting by a value the principal may not see
	// doesn't compile, and its search skips such values.
	context.Access, context.DataSourceID = document.Access, dataSourceID
	return context, true
}

func workspaceExportQueryProperty(property *workspaceExportDocProperty) (storedDatabaseQueryProperty, bool) {
	config, err := json.Marshal(property.Config)
	if err != nil {
		return storedDatabaseQueryProperty{}, false
	}
	return storedDatabaseQueryProperty{
		ID: property.ID, DataSourceID: property.DataSourceID, Type: property.Type, Name: property.Name,
		Position: property.Position, Config: string(config),
	}, true
}

func workspaceExportComputedType(valueType string) bool {
	return valueType == graphValueTypeFormula || valueType == graphValueTypeRollup
}

// computedValues returns a row's formula and rollup values.
func (document *workspaceExportDocument) computedValues(row *workspaceExportDocRow) map[string]databaseComputedValue {
	document.computeSource(row.DataSourceID)
	return document.Computed[row.ID]
}

func (document *workspaceExportDocument) computeSource(dataSourceID string) {
	if document.ComputedSources == nil {
		document.ComputedSources = map[string]bool{}
		document.Computed = map[string]map[string]databaseComputedValue{}
	}
	if document.ComputedSources[dataSourceID] {
		return
	}
	// Marked first, so rollups that target each other's databases stop.
	document.ComputedSources[dataSourceID] = true
	computedProperties := []storedDatabaseQueryProperty{}
	for _, property := range document.Properties[dataSourceID] {
		if workspaceExportComputedType(property.Type) {
			if stored, ok := workspaceExportQueryProperty(property); ok {
				computedProperties = append(computedProperties, stored)
			}
		}
	}
	if len(computedProperties) == 0 {
		return
	}
	context, ok := document.queryContext(dataSourceID)
	if !ok {
		return
	}
	seeds := map[string]map[string]databaseComputedValue{}
	for _, property := range computedProperties {
		if property.Type != graphValueTypeRollup {
			continue
		}
		config, err := decodeDatabaseComputedConfig(property.Config)
		if err != nil {
			continue
		}
		_, targetID, _ := config.rollupFields()
		target := document.property(targetID)
		if target == nil || target.DataSourceID == dataSourceID {
			continue
		}
		if stored, ok := workspaceExportQueryProperty(target); ok {
			context.PropertiesByID[target.ID] = stored
		}
		if !workspaceExportComputedType(target.Type) {
			continue
		}
		// Another database's formula reads that database's properties, so
		// its values come from that database, or stay empty in a cycle.
		document.computeSource(target.DataSourceID)
		for _, targetRow := range document.Rows[target.DataSourceID] {
			value, ok := document.Computed[targetRow.ID][target.ID]
			if !ok {
				value = databaseComputedValue{Type: target.Type, Value: workspaceExportComputedEmpty}
			}
			if seeds[targetRow.ID] == nil {
				seeds[targetRow.ID] = map[string]databaseComputedValue{}
			}
			seeds[targetRow.ID][target.ID] = value
		}
	}
	rows := document.queryRows()
	resolver := &databaseComputedResolver{
		context: context, rows: rows, computed: seeds, visiting: map[string]bool{}, cyclic: map[string]bool{},
	}
	for _, row := range document.Rows[dataSourceID] {
		values := map[string]databaseComputedValue{}
		for _, property := range computedProperties {
			if _, err := resolver.evaluate(rows[row.ID], property); err != nil {
				values[property.ID] = databaseComputedValue{Type: property.Type, Value: workspaceExportComputedEmpty}
				continue
			}
			values[property.ID] = resolver.computed[row.ID][property.ID]
		}
		document.Computed[row.ID] = values
	}
}

func (document *workspaceExportDocument) property(id string) *workspaceExportDocProperty {
	for _, properties := range document.Properties {
		for _, property := range properties {
			if property.ID == id {
				return property
			}
		}
	}
	return nil
}

// workspaceExportComputedValueText writes a computed value as the database
// shows it.
func workspaceExportComputedValueText(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case bool:
		return strconv.FormatBool(typed)
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return ""
		}
		return strconv.FormatFloat(math.Round(typed*1e6)/1e6, 'f', -1, 64)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, workspaceExportComputedValueText(item))
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		if text, rich := databaseFormulaStoredRichText(typed); rich {
			return text.shown()
		}
		return workspaceExportDateText(typed)
	}
	return databaseQueryValueText(value)
}

// loadRowPages reads each exported row page's times and people, which
// created and edited properties show and view filters and sorts compare.
func (document *workspaceExportDocument) loadRowPages(app core.App) error {
	ids := workspaceExportSortedKeys(document.RowByID)
	for start := 0; start < len(ids); start += 200 {
		end := min(start+200, len(ids))
		encoded, err := json.Marshal(ids[start:end])
		if err != nil {
			return err
		}
		pages := []struct {
			ID           string `db:"id"`
			Created      int64  `db:"created"`
			Updated      int64  `db:"updated"`
			CreatedBy    string `db:"created_by"`
			LastEditedBy string `db:"last_edited_by"`
		}{}
		// The rows are found by id; the owner is read after (+), or SQLite,
		// which keeps no statistics, reads the owner's every row.
		err = app.DB().NewQuery(`
			SELECT [[row]].[[id]], COALESCE([[page]].[[created]], [[row]].[[created]]) AS [[created]],
				COALESCE([[page]].[[updated]], [[row]].[[updated]]) AS [[updated]],
				COALESCE([[row_record]].[[created_by]], '') AS [[created_by]],
				COALESCE([[row_record]].[[last_edited_by]], '') AS [[last_edited_by]]
			FROM {{leafen_database_rows}} AS [[row]]
			LEFT JOIN {{leafen_database_row_pages}} AS [[page]] ON [[page]].[[row_id]] = [[row]].[[id]]
			LEFT JOIN {{leafen_pages}} AS [[row_record]] ON [[row_record]].[[id]] = [[row]].[[id]]
			WHERE +[[row]].[[owner]] = {:ownerId} AND [[row]].[[id]] IN (SELECT [[value]] FROM json_each({:ids}))
		`).Bind(dbx.Params{"ownerId": document.Payload.AuthorityOwnerID, "ids": string(encoded)}).All(&pages)
		if err != nil {
			return err
		}
		for _, page := range pages {
			if row := document.RowByID[page.ID]; row != nil {
				row.Created, row.Updated = page.Created, page.Updated
				row.CreatedBy, row.LastEditedBy = page.CreatedBy, page.LastEditedBy
			}
		}
	}
	return nil
}

// loadPeople names the people that comments, person cells and created and
// edited people show: the workspace's people, its connections' bots and
// those it invited.
func (document *workspaceExportDocument) loadPeople(app core.App) error {
	ids := map[string]bool{}
	for _, comments := range document.Comments {
		for _, comment := range comments {
			ids[comment.AuthorID] = true
		}
	}
	for dataSourceID, rows := range document.Rows {
		for _, property := range document.Properties[dataSourceID] {
			if property.Type == graphValueTypeCreatedBy || property.Type == graphValueTypeLastEditedBy {
				for _, row := range rows {
					ids[row.CreatedBy], ids[row.LastEditedBy] = true, true
				}
			}
			if property.Type != graphValueTypePerson {
				continue
			}
			for _, row := range rows {
				if property.Config["defaultCreator"] == true {
					ids[row.CreatedBy] = true
				}
				if wrapper, ok := row.Values[property.ID].(map[string]any); ok {
					for _, id := range mapStrings(wrapper, "value") {
						ids[id] = true
					}
				}
			}
		}
	}
	delete(ids, "")
	ordered := make([]string, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	for start := 0; start < len(ordered); start += 200 {
		end := min(start+200, len(ordered))
		records, err := app.FindRecordsByIds("users", ordered[start:end])
		if err != nil {
			return err
		}
		for _, record := range records {
			document.People[record.Id] = record.GetString("name")
		}
	}
	// One a copy brought from another workspace reads by the name it kept,
	// as the app shows them (workspace_carried_people.go).
	if err := document.loadCarriedPeople(app, ordered); err != nil {
		return err
	}
	// A connection's bot a person value names reads by its connection's
	// name, as Notion's export names an integration's bot (seen 2026-11-12:
	// its name in the CSV and the Markdown, after its first letter in the
	// HTML).
	bots, err := loadPeopleValueBots(app, document.WorkspaceID, ordered)
	if err != nil {
		return err
	}
	for id, bot := range bots {
		document.People[id] = bot.Name
	}
	// Someone invited into a person value reads by the address
	// (people_value_invitees.go).
	return document.loadInviteePeople(app, ordered)
}

func (document *workspaceExportDocument) loadInviteePeople(app core.App, ids []string) error {
	wanted := []string{}
	for _, id := range ids {
		if peopleValueInviteeIDShape(id) {
			wanted = append(wanted, id)
		}
	}
	if len(wanted) == 0 {
		return nil
	}
	encoded, err := json.Marshal(wanted)
	if err != nil {
		return err
	}
	invitees := []struct {
		ID    string `db:"id"`
		Email string `db:"email"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[id]], [[email]] FROM {{leafen_people_value_invitees}}
		WHERE [[id]] IN (SELECT [[value]] FROM json_each({:ids}))`).Bind(dbx.Params{"ids": string(encoded)}).All(&invitees); err != nil {
		return err
	}
	for _, invitee := range invitees {
		document.People[invitee.ID] = invitee.Email
	}
	return nil
}
