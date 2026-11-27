package app

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's MCP describes a data source by its configurable state and a
// SQLite table, and gives a database page's values as that table's values
// (seen 2026-09-30). A property's column holds text, a number, or a list as
// JSON; a date spreads over date:<name>:start, :end and :is_datetime and a
// place over place:<name>:…, as notion-update-page takes them; formulas and
// rollups have no column, and a page's fetch gives their values
// (mcp_page_computed_values.go) where Notion's names them by
// formulaResult:// and rollupResult:// URLs that its fetch doesn't open.

// The notes its columns carry are in mcp_notion_tool_words.go, and the
// clock its times are on in person_clock.go.

type mcpSQLiteColumn struct {
	Name    string
	SQLType string
	NotNull bool
	Comment string
	// Part names which of a property's columns this is: a date's "start",
	// "end" or "is_datetime", a place's "name"…; empty for a property's one
	// column.
	Part string
}

// mcpNotionPropertyType is the name Notion's MCP gives a property type.
func mcpNotionPropertyType(nativeType string) string {
	switch nativeType {
	case graphValueTypeRichText:
		return "text"
	case graphValueTypeFiles:
		return "file"
	case graphValueTypePhone:
		return "phone_number"
	case graphValueTypeUniqueID:
		return "auto_increment_id"
	}
	return nativeType
}

// mcpSQLiteColumnName is a property's column: a property named id or url
// takes the userDefined: prefix notion-update-page takes it by.
func mcpSQLiteColumnName(name string) string {
	if lower := strings.ToLower(name); lower == "id" || lower == "url" {
		return mcpDatabaseUserDefined + name
	}
	return name
}

func mcpSQLiteList(names []string) string {
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		encoded, _ := mcpFetchJSON(name)
		quoted = append(quoted, encoded)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func mcpPropertyOptions(config map[string]any) []notionDatabaseRowPageOption {
	options := []notionDatabaseRowPageOption{}
	encoded, err := json.Marshal(config["options"])
	if err != nil || json.Unmarshal(encoded, &options) != nil {
		return nil
	}
	return options
}

func mcpOptionNames(options []notionDatabaseRowPageOption) []string {
	names := make([]string, 0, len(options))
	for _, option := range options {
		names = append(names, option.Name)
	}
	return names
}

// mcpSQLiteColumns are a property's columns; none for a formula, a rollup or
// a type Notion's table leaves out.
func mcpSQLiteColumns(property notionMCPDataSourceProperty) []mcpSQLiteColumn {
	name := mcpSQLiteColumnName(property.Name)
	text := func(comment string) []mcpSQLiteColumn {
		return []mcpSQLiteColumn{{Name: name, SQLType: "TEXT", Comment: comment}}
	}
	switch property.Type {
	case graphValueTypeTitle, graphValueTypeRichText, graphValueTypeURL, graphValueTypeEmail, graphValueTypePhone,
		graphValueTypeCreatedBy, graphValueTypeLastEditedBy:
		return text("")
	case graphValueTypeNumber:
		return []mcpSQLiteColumn{{Name: name, SQLType: "FLOAT"}}
	case graphValueTypeUniqueID:
		return []mcpSQLiteColumn{{Name: name, SQLType: "INTEGER"}}
	case graphValueTypeSelect:
		return text("one of " + mcpSQLiteList(mcpOptionNames(mcpPropertyOptions(property.Config))))
	case graphValueTypeStatus:
		return text(mcpSQLiteStatusNote(property, mcpSQLiteList(mcpOptionNames(mcpPropertyOptions(property.Config)))))
	case graphValueTypeMultiSelect:
		return text(mcpSQLiteNamesNote(mcpSQLiteList(mcpOptionNames(mcpPropertyOptions(property.Config)))))
	case graphValueTypeCheckbox:
		return text(mcpSQLiteCheckboxNote)
	case graphValueTypePerson:
		return text(mcpSQLitePeopleNote)
	case graphValueTypeFiles:
		// Notion's also takes folders there (seen 2026-11-14); Leafen has
		// none and refuses them as values (mcpDatabasePageFiles), so its
		// note names file IDs alone.
		return text(mcpSQLiteFilesNote)
	case graphValueTypeRelation:
		target, _ := property.Config["relationDatabaseId"].(string)
		source := "{{" + mcpDataSourceURLPrefix + target + "}}"
		return text(mcpSQLiteRelationNote(source))
	case graphValueTypeCreatedTime:
		return []mcpSQLiteColumn{{Name: name, SQLType: "TEXT", NotNull: true, Comment: mcpSQLiteCreatedComment}}
	case graphValueTypeLastEditedTime:
		return []mcpSQLiteColumn{{Name: name, SQLType: "TEXT", NotNull: true, Comment: mcpSQLiteEditedComment}}
	case graphValueTypeDate:
		prefix := mcpDatabaseDatePrefix + property.Name + ":"
		return []mcpSQLiteColumn{
			{Name: prefix + "start", Part: "start", SQLType: "TEXT", Comment: mcpSQLiteDateStartNote},
			{Name: prefix + "end", Part: "end", SQLType: "TEXT", Comment: mcpSQLiteDateEndNote},
			{Name: prefix + "is_datetime", Part: "is_datetime", SQLType: "INTEGER", Comment: mcpSQLiteDateTimeNote},
		}
	case graphValueTypePlace:
		prefix := mcpDatabasePlacePrefix + property.Name + ":"
		return []mcpSQLiteColumn{
			{Name: prefix + "name", Part: "name", SQLType: "TEXT"}, {Name: prefix + "address", Part: "address", SQLType: "TEXT"},
			{Name: prefix + "latitude", Part: "latitude", SQLType: "FLOAT"}, {Name: prefix + "longitude", Part: "longitude", SQLType: "FLOAT"},
			{Name: prefix + "google_place_id", Part: "google_place_id", SQLType: "TEXT"},
		}
	}
	return nil
}

func mcpSQLiteTableText(app core.App, dataSourceID string, properties []notionMCPDataSourceProperty) string {
	return mcpSQLiteTableTextOf(app, dataSourceID, properties, mcpSQLiteColumns, mcpSQLiteCreatedComment)
}

// mcpSQLiteTableTextOf is a data source's SQLite table with each property's
// columns as columnsOf gives them and the page's own creation time noted so,
// as a database page's fetch gives it short (mcp_parent_schema_text.go).
func mcpSQLiteTableTextOf(
	app core.App, dataSourceID string, properties []notionMCPDataSourceProperty,
	columnsOf func(notionMCPDataSourceProperty) []mcpSQLiteColumn, createdNote string,
) string {
	columns := []mcpSQLiteColumn{
		{Name: "url", SQLType: "TEXT UNIQUE"},
		{Name: "createdTime", SQLType: "TEXT", Comment: createdNote},
	}
	for _, property := range properties {
		columns = append(columns, columnsOf(mcpPropertyNamingPublicSource(app, property))...)
	}
	lines := make([]string, 0, len(columns))
	for index, column := range columns {
		name := column.Name
		if index > 1 {
			// Shown as Notion shows it.
			name = `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
		}
		line := "\t" + name + " " + column.SQLType
		if column.NotNull {
			line += " NOT NULL"
		}
		if index < len(columns)-1 {
			line += ","
		}
		if column.Comment != "" {
			line += " -- " + column.Comment
		}
		lines = append(lines, line)
	}
	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS \"%s\" (\n%s\n)", mcpDataSourceURL(app, dataSourceID), strings.Join(lines, "\n"))
}

// mcpOptionState is an option as the configurable state lists it; a color
// other than the default is named. Its property is named by its MCP ID
// (notionMCPPropertyID).
func mcpOptionState(dataSourceID, propertyID string, option notionDatabaseRowPageOption) map[string]any {
	description := ""
	if option.Description != nil {
		description = *option.Description
	}
	state := map[string]any{
		"description": description, "name": option.Name,
		// The option's ID goes in base64, as Notion's MCP writes it (seen
		// 2026-11-09: "…/b3FgeA/MDY5NWNj…" for option "0695cc…").
		"url": fmt.Sprintf("collectionPropertyOption://%s/%s/%s", dataSourceID, propertyID,
			base64.RawURLEncoding.EncodeToString([]byte(option.ID))),
	}
	// A status option drawn in its group's color has no color of its own.
	if option.Color != "" && option.Color != "default" && !option.ColorFromGroup {
		state["color"] = option.Color
	}
	return state
}

// mcpStatusGroupsState sorts a status's options into Notion's groups: to do,
// in progress and complete, as the property's groups hold them in order.
func mcpStatusGroupsState(dataSourceID, propertyID string, property notionMCPDataSourceProperty) map[string]any {
	groups := map[string]any{"complete": []any{}, "current": []any{}, "future": []any{}, "in_progress": []any{}, "to_do": []any{}}
	keys := []string{"to_do", "in_progress", "complete"}
	options := mcpPropertyOptions(property.Config)
	placed := map[string]bool{}
	stored := []struct {
		ID        string   `json:"id"`
		OptionIDs []string `json:"optionIds"`
	}{}
	if encoded, err := json.Marshal(property.Config["statusGroups"]); err == nil {
		_ = json.Unmarshal(encoded, &stored)
	}
	for index, group := range stored {
		key := strings.ReplaceAll(strings.ToLower(group.ID), "-", "_")
		if _, known := groups[key]; !known {
			if index >= len(keys) {
				continue
			}
			key = keys[index]
		}
		for _, optionID := range group.OptionIDs {
			option, found := notionDatabaseRowPageOptionByID(options, optionID)
			if !found || placed[optionID] {
				continue
			}
			placed[optionID] = true
			groups[key] = append(groups[key].([]any), mcpOptionState(dataSourceID, propertyID, option))
		}
	}
	for _, option := range options {
		if !placed[option.ID] {
			groups["to_do"] = append(groups["to_do"].([]any), mcpOptionState(dataSourceID, propertyID, option))
		}
	}
	return groups
}

// mcpStatusDefaultName is the option a status's rows that set none read as
// (databaseStatusDefaultOptionID), by its name; none for a status with no
// default, as one made from a select.
func mcpStatusDefaultName(property notionMCPDataSourceProperty) (string, bool) {
	config, err := json.Marshal(property.Config)
	if err != nil {
		return "", false
	}
	fallback := databaseStatusDefaultOptionID(string(config))
	if fallback == "" {
		return "", false
	}
	option, found := notionDatabaseRowPageOptionByID(mcpPropertyOptions(property.Config), fallback)
	return option.Name, found
}

// mcpSQLiteStatusNote is a status's column note: its options (names, a JSON
// list) and the one its rows that set none read as, as Notion's names it
// (seen 2026-11-14: `one of ["대기", "진행", "끝"] Default: "대기".`, and
// `Default: none.` for one made from a select).
func mcpSQLiteStatusNote(property notionMCPDataSourceProperty, names string) string {
	shown := "none"
	if name, found := mcpStatusDefaultName(property); found {
		shown, _ = mcpFetchJSON(name)
	}
	return "one of " + names + " Default: " + shown + "."
}

// mcpPropertyTypeOf reads the type of one property of a data source, for a
// rollup's target; "" when it is not there.
func mcpPropertyTypeOf(app core.App, dataSourceID, propertyID string) string {
	types := []string{}
	_ = app.DB().NewQuery(`
		SELECT [[type]] FROM {{leafen_database_properties}}
		WHERE [[id]] = {:id} AND [[data_source_id]] = {:dataSourceId} LIMIT 1
	`).Bind(dbx.Params{"id": propertyID, "dataSourceId": dataSourceID}).Column(&types)
	if len(types) == 0 {
		return ""
	}
	return mcpNotionPropertyType(normalizeNotionDatabaseRowPagePropertyType(types[0]))
}

// mcpDataSourceElement is a data source's <data-source> text, as a data
// source's fetch and its database's give it.
func mcpDataSourceElement(
	app core.App,
	authority mcpAuthority,
	location mcpDataSourceLocation,
	properties []notionMCPDataSourceProperty,
	templates []notionMCPDataSourceTemplate,
) (string, error) {
	schema := map[string]any{}
	unqueryable := []string{}
	byID := map[string]notionMCPDataSourceProperty{}
	for _, property := range properties {
		byID[property.ID] = property
	}
	// A property is named by its public ID as Notion's MCP writes it
	// (seen 2026-11-09: "collectionProperty://…/eXw7XA" for "y|;\").
	ids, err := loadNotionPropertyIDs(app, location.ID)
	if err != nil {
		return "", err
	}
	mcpID, publicSource := func(id string) string { return notionMCPPropertyID(ids.publicID(id)) }, mcpDataSourcePublicID(app, location.ID)
	visible := mcpSQLiteTableOrder(mcpSQLiteVisibleProperties(properties), ids)
	for _, property := range visible {
		entry, queryable, err := mcpPropertyState(app, publicSource, property, byID, mcpID)
		if err != nil {
			return "", err
		}
		// Named in the order its schema has them, its table's, as
		// Notion's names them beside its schema (seen 2026-11-14: fetch,
		// view-version and get-version-changes alike); Notion's lists a
		// change it has just made in the order it made them until the
		// source is read again.
		if !queryable {
			unqueryable = append(unqueryable, property.Name)
		}
		schema[property.Name] = entry
	}
	state := map[string]any{"name": mcpDatabaseTitleAsIndexed(app, location.ID, location.Title), "schema": schema, "url": mcpDataSourceURL(app, location.ID)}
	if len(unqueryable) > 0 {
		state["notAvailableInQuerySql"] = unqueryable
	}
	if len(templates) > 0 {
		listed := make([]any, 0, len(templates))
		for _, template := range templates {
			name := template.Name
			if strings.TrimSpace(name) == "" {
				name = mcpUntitledTemplateName(app, authority.PrincipalID, template.ID)
			}
			// A template's own address, as its fetch names it (mcpPageAddress).
			listed = append(listed, map[string]any{"name": name, "url": mcpPageURL(authority, template.ID)})
		}
		state["page_templates"] = listed
	}
	encoded, err := mcpFetchJSON(state)
	if err != nil {
		return "", err
	}
	// Its page layout follows, as Notion's fetch gives it (mcp_data_source_page_layout_text.go), then whether an import is still making it.
	tail := append(notionImportFillingNotices(app, location.ID), "</data-source>")
	if layout, err := mcpPageLayoutText(app, location, properties); err != nil {
		return "", err
	} else if layout != "" {
		tail = append([]string{"", layout}, tail...)
	}
	return strings.Join(append([]string{
		fmt.Sprintf(`<data-source url="{{%s}}">`, mcpDataSourceURL(app, location.ID)),
		mcpFetchTitleLine("Data source", mcpDataSourceShownTitle(app, authority.PrincipalID, mcpDataSourceAsShown(app, authority, location))),
		"",
		mcpFetchDataSourceStateHead,
		mcpFetchReadOnlyNote,
		"<data-source-state>", encoded, "</data-source-state>",
		"",
		mcpFetchDataSourceTableHead + mcpTimesNoteWhereHeld(visible, authority.clock()),
		"<sqlite-table>", mcpSQLiteTableText(app, location.ID, visible), "</sqlite-table>",
	}, tail...), "\n"), nil
}

// mcpPropertyState is a property as a data source's configurable state
// gives it, as fetch and the version tools read it, and whether SQL can
// query it.
func mcpPropertyState(
	app core.App, sourceID string, property notionMCPDataSourceProperty,
	byID map[string]notionMCPDataSourceProperty, mcpID func(string) string,
) (map[string]any, bool, error) {
	queryable := true
	description, _ := property.Config["description"].(string)
	entry := map[string]any{
		"description": description, "name": property.Name, "type": mcpNotionPropertyType(property.Type),
	}
	switch property.Type {
	case graphValueTypeNumber:
		// Its format, as Notion's MCP names it — only one chosen
		// ("number_format":"dollar", "number" when picked; none for a bare
		// number, seen 2026-11-14) — so an agent copying a database keeps
		// it (2026-11-13: it couldn't read it). A unique ID's prefix stays
		// unsaid there, as in Notion's ("type":"auto_increment_id" alone).
		if format, chosen := notionChosenNumberFormat(property.Config); chosen {
			entry["number_format"] = format
		}
	case graphValueTypeSelect, graphValueTypeMultiSelect:
		options := []any{}
		for _, option := range mcpPropertyOptions(property.Config) {
			options = append(options, mcpOptionState(sourceID, mcpID(property.ID), option))
		}
		entry["options"] = options
	case graphValueTypeStatus:
		// Its default by name, as Notion's (seen 2026-11-14:
		// "defaultOption":"대기"); no key for a status with no default.
		if name, found := mcpStatusDefaultName(property); found {
			entry["defaultOption"] = name
		}
		entry["groups"] = mcpStatusGroupsState(sourceID, mcpID(property.ID), property)
	case graphValueTypeFormula:
		entry["codeUrl"] = fmt.Sprintf("formulaCode://%s/%s", sourceID, mcpID(property.ID))
		queryable = false
	case graphValueTypeRelation:
		target, _ := property.Config["relationDatabaseId"].(string)
		entry["dataSourceUrl"] = mcpDataSourceURL(app, target)
		// A two-way relation names its pair on the target, as Notion's
		// MCP does (seen 2026-11-09: "propertyUrl":
		// "collectionProperty://<target>/R1NBfg"); one that pairs with
		// itself is a one-way relation.
		if pairID, _ := property.Config["relatedPropertyId"].(string); pairID != "" && pairID != property.ID && target != "" {
			targetIDs, err := loadNotionPropertyIDs(app, target)
			if err != nil {
				return nil, false, err
			}
			if public := targetIDs.publicID(pairID); public != "" {
				entry["propertyUrl"] = fmt.Sprintf("collectionProperty://%s/%s", mcpDataSourcePublicID(app, target), notionMCPPropertyID(public))
			}
		}
	case graphValueTypeRollup:
		relationID, _ := property.Config["rollupRelationPropertyId"].(string)
		targetID, _ := property.Config["rollupTargetPropertyId"].(string)
		function, _ := property.Config["rollupFunction"].(string)
		target, _ := byID[relationID].Config["relationDatabaseId"].(string)
		entry["aggregation"] = function
		entry["relationPropertyUrl"] = fmt.Sprintf("collectionProperty://%s/%s", sourceID, mcpID(relationID))
		entry["targetPropertyType"] = mcpPropertyTypeOf(app, target, targetID)
		targetIDs, err := loadNotionPropertyIDs(app, target)
		if err != nil {
			return nil, false, err
		}
		entry["targetPropertyUrl"] = fmt.Sprintf("collectionProperty://%s/%s", mcpDataSourcePublicID(app, target), notionMCPPropertyID(targetIDs.publicID(targetID)))
		queryable = false
	case graphValueTypeDate, graphValueTypePlace:
		columns := []any{}
		for _, column := range mcpSQLiteColumns(property) {
			columns = append(columns, map[string]any{"name": column.Name, "sqlType": column.SQLType})
		}
		entry["querySqlColumns"] = map[string]any{
			"columns": columns, "usage": mcpFetchQueryColumnsUsage,
		}
	}
	return entry, queryable, nil
}

// mcpSQLiteVisibleProperties are a data source's properties its SQLite
// table names, their types as it names them.
func mcpSQLiteVisibleProperties(properties []notionMCPDataSourceProperty) []notionMCPDataSourceProperty {
	visible := make([]notionMCPDataSourceProperty, 0, len(properties))
	for _, property := range properties {
		property.Type = normalizeNotionDatabaseRowPagePropertyType(property.Type)
		if mcpDatabasePropertyShown(property.Type) {
			visible = append(visible, property)
		}
	}
	return visible
}

// mcpSQLiteTableOrder puts a data source's properties in the order Notion's
// MCP gives its SQLite table's columns, the order its API writes them
// (seen 2026-11-11: "Link", "Score", "Due"… by their raw IDs, the title's
// "title" last; notionPropertyOrderLess), not the order a view shows.
func mcpSQLiteTableOrder(properties []notionMCPDataSourceProperty, ids notionPropertyIDs) []notionMCPDataSourceProperty {
	ordered := slices.Clone(properties)
	sort.SliceStable(ordered, func(left, right int) bool {
		return notionPropertyOrderLess(
			notionPropertyRawID(ids.publicID(ordered[left].ID)), notionPropertyRawID(ids.publicID(ordered[right].ID)),
		)
	})
	return ordered
}

// mcpSQLiteReadOnlyColumns are the columns of a table's properties that
// Leafen sets itself, in the table's order, as a page's schema names them.
func mcpSQLiteReadOnlyColumns(properties []notionMCPDataSourceProperty) []string {
	names := []string{}
	for _, property := range properties {
		switch property.Type {
		case graphValueTypeUniqueID, graphValueTypeCreatedBy, graphValueTypeCreatedTime,
			graphValueTypeLastEditedBy, graphValueTypeLastEditedTime:
			for _, column := range mcpSQLiteColumns(property) {
				encoded, _ := mcpFetchJSON(column.Name)
				names = append(names, encoded)
			}
		}
	}
	return names
}
