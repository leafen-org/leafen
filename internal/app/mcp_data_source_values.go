package app

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database page's values as Notion's MCP gives them (seen 2026-09-30):
// the values of the SQLite table fetch describes, which fetch shows for a
// page and SQL queries read from, one column per property or a date's and a
// place's parts (mcp_data_source_text.go describes the table).

// mcpSQLiteDateText writes a date part as Notion's values do: a day as
// written, a time on a clock of its own as that moment on the connection's
// person's clock (person_clock.go), and a time on no clock as written.
func mcpSQLiteDateText(part, zone string, clock *time.Location) string {
	part = strings.TrimSpace(part)
	if len(part) <= len("2006-01-02") {
		return part
	}
	if instant, err := time.Parse(time.RFC3339Nano, part); err == nil {
		return personClockMillis(instant, clock)
	}
	location, err := time.LoadLocation(zone)
	if zone == "" || err != nil {
		return part
	}
	for _, layout := range []string{"2006-01-02T15:04:05.999999999", "2006-01-02T15:04"} {
		if wall, err := time.ParseInLocation(layout, part, location); err == nil {
			return personClockMillis(wall, clock)
		}
	}
	return part
}

// mcpDatabaseRowFacts are what a database page's own properties read from
// the page: its ID, its database and who made and last edited it when.
type mcpDatabaseRowFacts struct {
	RowID        string
	DataSourceID string
	CreatedAt    int64
	UpdatedAt    int64
	CreatedBy    string
	LastEditedBy string
}

// mcpDatabasePageFetchProperties reads a database page's values as Notion's
// fetch gives them, by column, a formula's and a rollup's as rows mode gives
// them (mcp_page_computed_values.go); empty values are left out, and so are
// those the principal may not see under the properties' access rules.
func mcpDatabasePageFetchProperties(app core.App, authority mcpAuthority, row databaseRowPageAuthority) (map[string]any, error) {
	stored, err := loadStoredNotionDatabaseRowPageProperties(app, row)
	if err != nil {
		return nil, err
	}
	computed := mcpFetchedComputedValues(app, authority, row, stored)
	access, err := newNotionRowPropertyAccess(app, authority.PrincipalID, row, stored)
	if err != nil {
		return nil, err
	}
	gone, empty, err := access.redact(stored, computed)
	if err != nil {
		return nil, err
	}
	facts := mcpDatabaseRowFacts{
		RowID: row.RowID, DataSourceID: row.DataSourceID, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		CreatedBy: row.CreatedBy, LastEditedBy: row.LastEditedBy,
	}
	values := map[string]any{"url": mcpPageAddress(app, authority, row.RowID)}
	// A text's mention of a page closed to the agent reads "Untitled"
	// (mcp_reader_mentions.go).
	closed := mcpReaderCloses(app, authority)
	relations := mcpRelationsAsRead(app, authority, row.WorkspaceID)
	mentions := mcpPropertyMentions(app, authority, row.WorkspaceID)
	for _, property := range stored {
		// A property the principal may not see isn't named, as the schema
		// leaves it out.
		if gone[property.ID] {
			continue
		}
		switch normalizeNotionDatabaseRowPagePropertyType(property.Type) {
		case graphValueTypeFormula, graphValueTypeRollup:
			if value := mcpFetchedComputedValue(computed, property.ID); value != nil && !empty[property.ID] {
				values[mcpDatabaseColumnName(property, "")] = personClockDateValue(value, authority.clock())
			}
			continue
		}
		property = mcpCellAsRead(closed, property)
		if normalizeNotionDatabaseRowPagePropertyType(property.Type) == graphValueTypeRelation && property.Value.Valid {
			if read, changed := relations(json.RawMessage(property.Value.String)); changed {
				property.Value.String = string(read)
			}
		}
		for part, value := range mcpFetchedCellParts(property, mcpDatabasePropertyParts(authority, property, facts)) {
			values[mcpDatabaseColumnName(property, part)] = value
		}
		if markdown, written := mcpTextCellMarkdown(property, mentions); written {
			values[mcpDatabaseColumnName(property, "")] = markdown
		}
	}
	return values, nil
}

// mcpFetchedCellParts gives a cell's parts as fetch and a query's rows give
// them, as Notion's MCP does (seen 2026-11-11): an empty title, text, URL,
// email or phone number reads "", a date without one its is_datetime 0, and
// an ID its number as text; other empty values are left out. SQL reads NULL
// for the empty ones and an ID as its number.
func mcpFetchedCellParts(property storedNotionDatabaseRowPageProperty, parts map[string]any) map[string]any {
	switch normalizeNotionDatabaseRowPagePropertyType(property.Type) {
	case graphValueTypeUniqueID:
		if number, ok := parts[""].(float64); ok {
			parts[""] = strconv.FormatFloat(number, 'f', -1, 64)
		} else if number, isNumber := parts[""].(int64); isNumber {
			parts[""] = strconv.FormatInt(number, 10)
		}
	case graphValueTypeTitle, graphValueTypeRichText, graphValueTypeURL, graphValueTypeEmail, graphValueTypePhone:
		if _, present := parts[""]; !present {
			parts[""] = ""
		}
	case graphValueTypeDate:
		if _, present := parts["is_datetime"]; !present {
			parts["is_datetime"] = 0
		}
	}
	return parts
}

// mcpCellPartOrder is the order of a property's columns in fetch's table
// (mcpSQLiteColumns), a date's deadline, Leafen's own, after its parts.
var mcpCellPartOrder = []string{
	"", "start", "end", "is_datetime", "deadline", "name", "address", "latitude", "longitude", "google_place_id",
}

// mcpTextCellMarkdown is a title or text cell's value as fetch and a query's
// rows give it, as Notion's MCP does: a paragraph of "v2" page Markdown
// (mcpPropertyMarkdown); false for another property's or an empty one.
// SQL reads its words (mcpDatabasePropertyParts).
func mcpTextCellMarkdown(property storedNotionDatabaseRowPageProperty, mentions notionMarkdownMentions) (string, bool) {
	switch normalizeNotionDatabaseRowPagePropertyType(property.Type) {
	case graphValueTypeTitle, graphValueTypeRichText:
	default:
		return "", false
	}
	words, err := notionDatabaseRowPageStringValue(property)
	if err != nil || words == "" {
		return "", false
	}
	return mcpPropertyMarkdown(words, databaseStoredSpans(property.Spans.String), mentions), true
}

// mcpSQLTextWords is a title's or text's value as SQL reads it, as Notion's
// (seen 2026-11-11: "  Row @date @page tail  " reads "Row   tail"): its
// words without its mentions, edge spaces trimmed. Fetch and a query's rows
// give its Markdown instead (mcpTextCellMarkdown), and so does SQL for one
// holding a mention, a link or an equation (databaseSQLTextValue).
func mcpSQLTextWords(property storedNotionDatabaseRowPageProperty) string {
	text, err := notionDatabaseRowPageStringValue(property)
	if err != nil {
		return ""
	}
	if spans := databaseStoredSpans(property.Spans.String); len(spans) > 0 {
		var words strings.Builder
		for _, span := range spans {
			if span.Mention == "" {
				words.WriteString(span.Text)
			}
		}
		text = words.String()
	}
	return strings.TrimSpace(text)
}

// mcpDatabaseColumnName is the column of a property's part, as fetch's table
// names it.
func mcpDatabaseColumnName(property storedNotionDatabaseRowPageProperty, part string) string {
	switch {
	case part == "":
		return mcpSQLiteColumnName(property.Name)
	case normalizeNotionDatabaseRowPagePropertyType(property.Type) == graphValueTypePlace:
		return mcpDatabasePlacePrefix + property.Name + ":" + part
	}
	return mcpDatabaseDatePrefix + property.Name + ":" + part
}

// mcpDatabasePropertyParts reads one property's value as fetch's table has
// it, by part: "" for a property's one column, a date's "start", "end" and
// "is_datetime", a place's "name", "address"…; an empty value gives none.
// fetch and SQL queries read values here, so both see the same table; pages
// are named by their addresses on the server the connection reached, people
// by the IDs notion-get-users gives (notionPersonAddress). A
// formula and a rollup have no parts here: a page read works their values
// out (mcpFetchedComputedValues), rows mode reads them from its query, SQL
// has no column for them and a version's rows leave them out.
func mcpDatabasePropertyParts(authority mcpAuthority, property storedNotionDatabaseRowPageProperty, row mcpDatabaseRowFacts) map[string]any {
	parts := map[string]any{}
	decode := func(target any) bool {
		return property.Value.Valid && json.Unmarshal([]byte(property.Value.String), target) == nil
	}
	user := func(id string) any {
		if id == "" {
			return nil
		}
		return notionPersonAddress(id)
	}
	nativeType := normalizeNotionDatabaseRowPagePropertyType(property.Type)
	var value any
	switch nativeType {
	case graphValueTypeTitle, graphValueTypeRichText:
		if text := mcpSQLTextWords(property); text != "" {
			value = text
		}
	case graphValueTypeURL, graphValueTypeEmail, graphValueTypePhone:
		if text, err := notionDatabaseRowPageStringValue(property); err == nil && text != "" {
			value = text
		}
	case graphValueTypeNumber:
		var number float64
		if decode(&number) {
			value = number
		}
	case graphValueTypeCheckbox:
		checked := false
		decode(&checked)
		value = map[bool]string{true: "__YES__", false: "__NO__"}[checked]
	case graphValueTypeSelect, graphValueTypeStatus:
		optionID, err := notionDatabaseRowPageOptionValue(property)
		options, optionsErr := notionDatabaseRowPageOptions(property)
		if option, found := notionDatabaseRowPageOptionByID(options, optionID); err == nil && optionsErr == nil && found {
			value = option.Name
		}
	case graphValueTypeMultiSelect:
		optionIDs := []string{}
		options, err := notionDatabaseRowPageOptions(property)
		if decode(&optionIDs) && err == nil && len(optionIDs) > 0 {
			names := []string{}
			for _, optionID := range optionIDs {
				if option, found := notionDatabaseRowPageOptionByID(options, optionID); found {
					names = append(names, option.Name)
				}
			}
			value = names
		}
	case graphValueTypePerson, graphValueTypeRelation:
		ids := []string{}
		decode(&ids)
		// A person that defaults to the creator reads as the row's creator
		// while it names no one.
		if len(ids) == 0 && row.CreatedBy != "" && databasePersonDefaultsToCreator(nativeType, property.Config) {
			ids = []string{row.CreatedBy}
		}
		if len(ids) > 0 {
			listed := make([]string, 0, len(ids))
			for _, id := range ids {
				if nativeType == graphValueTypePerson {
					listed = append(listed, notionPersonAddress(id))
				} else {
					listed = append(listed, mcpPageURL(authority, id))
				}
			}
			value = listed
		}
	case graphValueTypeFiles:
		// Uploads by ID, linked files as the API gives them (mcp_files_value.go).
		value = mcpFilesValue(property)
	case graphValueTypeDate:
		date := map[string]any{}
		if decode(&date) {
			start, _ := date["start"].(string)
			end, _ := date["end"].(string)
			zone, _ := date["timeZone"].(string)
			parts["start"] = mcpSQLiteDateText(start, zone, authority.clock())
			if end != "" {
				parts["end"] = mcpSQLiteDateText(end, zone, authority.clock())
			}
			parts["is_datetime"] = map[bool]int{true: 1, false: 0}[strings.Contains(start, "T")]
			// Leafen's own, only on an item that has one (deadline_values.go).
			if deadline, _ := date["deadline"].(string); deadline != "" {
				parts["deadline"] = deadline
			}
		}
	case graphValueTypePlace:
		if property.Value.Valid {
			if place, err := notionDatabaseRowPagePlace(json.RawMessage(property.Value.String)); err == nil {
				for part, key := range map[string]string{
					"name": "name", "address": "address", "latitude": "lat", "longitude": "lon", "google_place_id": "google_place_id",
				} {
					if place[key] != nil {
						parts[part] = place[key]
					}
				}
			}
		}
	case graphValueTypeVerification:
		// Its state, as Notion's filters and fetch name it.
		verification := map[string]any{}
		if decode(&verification) {
			value = mcpVerificationState(verification)
		}
	case graphValueTypeCreatedTime:
		value = personClockMillis(time.UnixMilli(row.CreatedAt), authority.clock())
	case graphValueTypeLastEditedTime:
		value = personClockMillis(time.UnixMilli(row.UpdatedAt), authority.clock())
	case graphValueTypeCreatedBy:
		value = user(row.CreatedBy)
	case graphValueTypeLastEditedBy:
		value = user(row.LastEditedBy)
	case graphValueTypeUniqueID:
		if projected, err := notionDatabaseRowPageUniqueID(property); err == nil && projected["number"] != nil {
			value = projected["number"]
			if prefix, ok := projected["prefix"].(string); ok {
				value = fmt.Sprintf("%s-%d", prefix, projected["number"])
			}
		}
	}
	if value != nil {
		parts[""] = value
	}
	return parts
}

// mcpDatabaseQueryProperty is a queried row's value of a property as a page
// stores it, for reading as fetch does; ids are its data source's.
func mcpDatabaseQueryProperty(property storedDatabaseQueryProperty, row databaseQueryRow, ids notionPropertyIDs) storedNotionDatabaseRowPageProperty {
	stored := storedNotionDatabaseRowPageProperty{
		ID: property.ID, Type: property.Type, Name: property.Name, Config: property.Config,
		PublicID: ids.publicID(property.ID),
	}
	if value, found := row.Values[property.ID]; found && len(value.Value) > 0 && string(value.Value) != "null" {
		stored.ValueType = sql.NullString{String: value.Type, Valid: true}
		stored.Value = sql.NullString{String: string(value.Value), Valid: true}
		if encoded, err := json.Marshal(value.Spans); err == nil && len(value.Spans) > 0 {
			stored.Spans = sql.NullString{String: string(encoded), Valid: true}
		}
	}
	return stored
}

// loadMCPDatabaseRowFacts reads the facts of queried database pages at once.
func loadMCPDatabaseRowFacts(app core.App, rows []databaseQueryRow) (map[string]mcpDatabaseRowFacts, error) {
	facts := make(map[string]mcpDatabaseRowFacts, len(rows))
	const window = 200
	for start := 0; start < len(rows); start += window {
		ids := rows[start:min(start+window, len(rows))]
		params := dbx.Params{}
		placeholders := make([]string, 0, len(ids))
		for index, row := range ids {
			key := fmt.Sprintf("rowFact%d", index)
			params[key] = row.ID
			placeholders = append(placeholders, "{:"+key+"}")
		}
		stored := []struct {
			RowID        string `db:"row_id"`
			DataSourceID string `db:"data_source_id"`
			CreatedAt    int64  `db:"created"`
			UpdatedAt    int64  `db:"updated"`
			CreatedBy    string `db:"created_by"`
			LastEditedBy string `db:"last_edited_by"`
		}{}
		if err := app.DB().NewQuery(`
			SELECT [[page]].[[row_id]], [[page]].[[data_source_id]], [[page]].[[created]], [[page]].[[updated]],
				[[record]].[[created_by]], [[record]].[[last_edited_by]]
			FROM {{leafen_database_row_pages}} [[page]]
			JOIN {{leafen_pages}} [[record]] ON [[record]].[[id]] = [[page]].[[row_id]]
			WHERE [[page]].[[row_id]] IN (` + strings.Join(placeholders, ", ") + `)
		`).Bind(params).All(&stored); err != nil {
			return nil, err
		}
		for _, row := range stored {
			facts[row.RowID] = mcpDatabaseRowFacts{
				RowID: row.RowID, DataSourceID: row.DataSourceID, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
				CreatedBy: row.CreatedBy, LastEditedBy: row.LastEditedBy,
			}
		}
	}
	return facts, nil
}

// projectMCPDataSourceRows gives each row's values as fetch gives a page's,
// by column (mcpDatabasePropertyParts), so rows read as the table fetch
// describes, in its order and then the row's url, as Notion's rows mode
// (seen 2026-11-11); a formula's or a rollup's result is given as it stands.
func projectMCPDataSourceRows(
	authority mcpAuthority,
	rows []databaseQueryRow,
	queryContext databaseQueryContext,
	facts map[string]mcpDatabaseRowFacts,
	shown map[string]bool,
	ids notionPropertyIDs,
	mentions notionMarkdownMentions,
) []any {
	names := make(map[string]int, len(queryContext.Properties))
	for _, property := range queryContext.Properties {
		names[property.Name]++
	}
	properties := slices.Clone(queryContext.Properties)
	sort.SliceStable(properties, func(left, right int) bool {
		return notionPropertyOrderLess(
			notionPropertyRawID(ids.publicID(properties[left].ID)), notionPropertyRawID(ids.publicID(properties[right].ID)),
		)
	})
	results := make([]any, 0, len(rows))
	for _, row := range rows {
		result := mcpResultRow{}
		for _, property := range properties {
			if !shown[property.ID] {
				continue
			}
			suffix := ""
			if names[property.Name] > 1 || strings.HasPrefix(property.Name, "_") {
				suffix = " [" + ids.publicID(property.ID) + "]"
			}
			stored := mcpDatabaseQueryProperty(property, row, ids)
			switch normalizeNotionDatabaseRowPagePropertyType(property.Type) {
			case graphValueTypeFormula, graphValueTypeRollup:
				result.set(mcpDatabaseColumnName(stored, "")+suffix, personClockDateValue(databaseQueryRowValue(row, property.ID), authority.clock()))
				continue
			}
			// A list reads as its JSON text, as a column holds it (Notion's
			// rows mode, seen 2026-11-04).
			parts := mcpFetchedCellParts(stored, mcpDatabasePropertyParts(authority, stored, facts[row.ID]))
			for _, part := range mcpCellPartOrder {
				value, present := parts[part]
				if !present {
					continue
				}
				switch value.(type) {
				case []string, []any:
					if encoded, err := json.Marshal(value); err == nil {
						value = string(encoded)
					}
				}
				result.set(mcpDatabaseColumnName(stored, part)+suffix, value)
			}
			// A title's or text's value is its Markdown, as fetch gives it.
			if markdown, written := mcpTextCellMarkdown(stored, mentions); written {
				result.set(mcpDatabaseColumnName(stored, "")+suffix, markdown)
			}
		}
		result.set("url", mcpPageURL(authority, row.ID))
		if row.SubitemParentID != nil {
			result.set("_subitem_parent_id", *row.SubitemParentID)
		}
		results = append(results, result)
	}
	return results
}

// mcpVerificationState reads a verification property's value: verified, or
// expired when its days have passed; nothing without one.
func mcpVerificationState(verification map[string]any) any {
	state, _ := verification["state"].(string)
	if state == "" || state == "none" || state == "unverified" {
		return nil
	}
	if end, _ := notionDateFields(verification["date"]); end != nil {
		if until, _ := end["end"].(string); until != "" {
			if at, err := time.Parse(time.RFC3339, until); err == nil && at.Before(time.Now()) {
				return "expired"
			}
		}
	}
	return state
}
