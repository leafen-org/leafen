package app

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"unicode/utf8"
)

func decodeBoundedGraphJSON(raw json.RawMessage) (any, error) {
	switch {
	case len(raw) == 0:
		return nil, graphRefused(errorCodeGraphInvalid, "Check the content structure.", "the value is missing")
	case len(raw) > maxGraphChangeBytes:
		return nil, graphRefused(errorCodeGraphInvalid, "Check the content structure.",
			"the value is %d bytes, over %d", len(raw), maxGraphChangeBytes)
	case !json.Valid(raw):
		return nil, graphRefused(errorCodeGraphInvalid, "Check the content structure.", "the value isn't JSON")
	}
	var value any
	if err := json.Unmarshal(composedJSON(raw), &value); err != nil { // its words composed (composed_text.go)
		return nil, graphRefused(errorCodeGraphInvalid, "Check the content structure.", "the value isn't JSON")
	}
	return value, nil
}

func normalizeJSONObject(raw json.RawMessage) (json.RawMessage, error) {
	value, err := decodeBoundedGraphJSON(raw)
	if err != nil {
		return nil, err
	}
	object, ok := value.(map[string]any)
	if !ok || object == nil {
		return nil, graphRefused(errorCodeGraphInvalid, "Check the content structure.", "the value is a JSON object, {} for none")
	}
	canonical, err := json.Marshal(object)
	if err != nil {
		return nil, graphInputError(errorCodeGraphInvalid, "Check the content structure.")
	}
	return canonical, nil
}

// UnmarshalJSON reads a data source as written. One written without its name
// keeps the name it has (writeDatabaseSourceRecord), as one written without
// its icon, description or cover keeps those: a change to them carries no
// name that could put back one someone renamed it to meanwhile.
func (source *graphDataSource) UnmarshalJSON(data []byte) error {
	type written graphDataSource
	var named struct {
		Title *string `json:"title"`
	}
	if err := json.Unmarshal(data, (*written)(source)); err != nil {
		return err
	}
	if err := json.Unmarshal(data, &named); err != nil {
		return err
	}
	source.TitleOmitted = named.Title == nil
	return nil
}

func normalizeGraphDataSource(source *graphDataSource) error {
	// What makes it a Sprints database is read from the sprints, never written; so is People (people_database_rules.go).
	source.SprintStatusPropertyID, source.SprintFutureOptionID = "", ""
	source.ProjectDatabase, source.TaskDatabase, source.PeopleDatabase, source.FixedPropertyIDs = false, false, false, nil
	// How many automations it has is read from them, never written; so is
	// what its untitled rows are called, from its templates.
	source.AutomationCount, source.UntitledRowName = 0, ""
	var err error
	source.ID, err = graphIDField("id", source.ID)
	if err != nil {
		return err
	}
	// A name keeps its spaces, tabs and line breaks where they are, its ends
	// too, as Notion's does typed in its app or sent through its API (seen
	// 2026-11-13: "앱DB 끝 " and " DB 끝 " read back so); one of spaces alone
	// is none. Words and pieces written one trimmed and the other not are
	// both trimmed, as they read alike.
	if strings.TrimSpace(source.Title) == "" {
		source.Title = ""
	}
	spans := source.TitleSpans
	if len(spans) > 0 && spelledName(spans) != source.Title {
		spans = trimGraphTextSpans(spans)
		source.Title = strings.TrimSpace(source.Title)
	}
	if count := len([]rune(source.Title)); count > maxGraphTitleRunes {
		return graphRefused(errorCodeGraphInvalid, "Check the database title.", "title is %d characters, over %d", count, maxGraphTitleRunes)
	}
	// A name's formatting goes with its words; one left out keeps both.
	if source.TitleOmitted && source.TitleSpans != nil {
		return graphRefused(errorCodeGraphInvalid, "Check the database title's formatting.", "titleSpans goes with title: send both or neither")
	}
	// Its formatting spells the name, its pieces as written. One plain piece
	// stays an empty list: the name is written plain, where no spans at all
	// keep what the same name has (writeDatabaseSourceRecord).
	if source.TitleSpans != nil {
		encoded, _ := json.Marshal(source.Title)
		normalized, err := normalizeGraphNameSpans(encoded, spans)
		if err != nil {
			return graphRefused(errorCodeGraphInvalid, "Check the database title's formatting.", "%s", graphTitleSpansProblem("titleSpans", spans, false))
		}
		if normalized == nil {
			normalized = []graphTextSpan{}
		}
		source.TitleSpans = normalized
	}
	// A description keeps its text as written; one past the bound is refused.
	// Its formatting spells the same text (a text value's rule).
	if source.Description != nil {
		description := strings.ReplaceAll(*source.Description, "\r\n", "\n")
		if count := len([]rune(description)); count > maxGraphTextCharacters {
			return graphRefused(errorCodeGraphInvalid, "Check the database description.",
				"description is %d characters, over %d", count, maxGraphTextCharacters)
		}
		source.Description = &description
		if len(source.DescriptionSpans) > 0 {
			for index := range source.DescriptionSpans {
				source.DescriptionSpans[index].Text = strings.ReplaceAll(source.DescriptionSpans[index].Text, "\r\n", "\n")
			}
			encoded, _ := json.Marshal(description)
			spans, err := normalizeGraphRowValueSpans(graphValueTypeRichText, encoded, source.DescriptionSpans)
			if err != nil {
				return graphRefused(errorCodeGraphInvalid, "Check the database description's formatting.", "descriptionSpans' texts must spell description")
			}
			source.DescriptionSpans = spans
		}
	} else if len(source.DescriptionSpans) > 0 {
		return graphRefused(errorCodeGraphInvalid, "Check the database description's formatting.", "descriptionSpans goes with description: send both")
	}
	if source.Cover != nil && len([]rune(*source.Cover)) > maxPageCoverRunes ||
		source.CoverPosition != nil && (*source.CoverPosition < 0 || *source.CoverPosition > 100) {
		return graphRefused(errorCodeGraphInvalid, "Check the database cover.",
			"cover is up to %d characters and coverPosition 0–100", maxPageCoverRunes)
	}
	return normalizeGraphDataSourceIcon(source)
}

// normalizeGraphDataSourceIcon keeps an omitted icon unchanged and otherwise
// applies the same presentation shape as page icons.
func normalizeGraphDataSourceIcon(source *graphDataSource) error {
	if source.IconType == nil {
		if source.Icon != nil {
			return graphRefused(errorCodeGraphInvalid, "Check the database icon.", "icon goes with iconType: send both")
		}
		return nil
	}
	iconType := strings.TrimSpace(*source.IconType)
	icon := ""
	if source.Icon != nil {
		icon = strings.TrimSpace(*source.Icon)
	}
	if _, supported := graphDataSourceIconTypes[iconType]; !supported ||
		len([]rune(icon)) > maxPageIconRunes || (iconType == graphDataSourceIconNone) != (icon == "") ||
		iconType == graphDataSourceIconCustomEmoji && !validCustomEmojiID(icon) ||
		iconType == graphDataSourceIconBuiltIn && !validNativeIconValue(icon) {
		return graphRefused(errorCodeGraphInvalid, "Check the database icon.",
			"iconType is one of %s, with icon its value (empty only for %q)", graphNamesList(graphDataSourceIconTypes), graphDataSourceIconNone)
	}
	source.Icon, source.IconType = &icon, &iconType
	return nil
}

func normalizeGraphDatabaseDelete(deletion *graphDatabaseDelete) error {
	id, err := graphIDField("id", deletion.ID)
	if err != nil {
		return err
	}
	deletion.ID = id
	return nil
}

func normalizeGraphView(view *graphDatabaseView) error {
	// Read-time dependency settings are derived from the source's authority,
	// never persisted or accepted from a client's copied view.
	view.TimelineDependency = nil
	var err error
	view.ID, err = graphIDField("id", view.ID)
	if err != nil {
		return err
	}
	view.DataSourceID, err = graphIDField("dataSourceId", view.DataSourceID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(view.ContainerID) != "" {
		if view.ContainerID, err = graphIDField("containerId", view.ContainerID); err != nil {
			return err
		}
	} else {
		view.ContainerID = ""
	}
	view.Position, err = graphPositionField("position", view.Position, false)
	if err != nil {
		return err
	}
	view.Type = strings.TrimSpace(view.Type)
	if _, ok := graphViewTypes[view.Type]; !ok {
		return graphRefused(errorCodeGraphInvalid, "Unsupported view type.", "type %q isn't one of %s", view.Type, graphNamesList(graphViewTypes))
	}
	// A view may be unnamed, as Notion leaves one made through Markdown or
	// cleared by its reader (seen 2026-10-10), and is shown by its kind; a
	// name is kept as given, spaces too, as Notion's (database_view_name.go).
	if !viewNameValid(view.Name) {
		return graphNameField("Check the view name.", "name", view.Name, false)
	}
	view.Config, err = normalizeJSONObject(view.Config)
	return graphValueAt(err, "config")
}

func normalizeGraphProperty(property *graphProperty) error {
	// What a property is to its database's sprints is read from them, never written.
	property.SprintManaged = ""
	var err error
	property.ID, err = graphIDField("id", property.ID)
	if err != nil {
		return err
	}
	property.DataSourceID, err = graphIDField("dataSourceId", property.DataSourceID)
	if err != nil {
		return err
	}
	property.Position, err = graphPositionField("position", property.Position, false)
	if err != nil {
		return err
	}
	property.Type = strings.TrimSpace(property.Type)
	if property.Type == "text" {
		property.Type = graphValueTypeRichText
	}
	if _, ok := graphValueTypes[property.Type]; !ok {
		return graphRefused(errorCodeGraphInvalid, "Unsupported property type.", "type %q isn't one of %s", property.Type, graphNamesList(graphValueTypes))
	}
	property.Name = strings.TrimSpace(property.Name)
	if err := graphNameField("Check the property name.", "name", property.Name, graphPropertyNamed(property.Type)); err != nil {
		return err
	}
	property.Config, err = normalizeJSONObject(property.Config)
	if err != nil {
		return graphValueAt(err, "config")
	}
	return normalizeGraphPropertyConfig(property)
}

func normalizeGraphRow(row *graphDatabaseRow, runtimeWritablePropertyIDs map[string]struct{}) error {
	var err error
	// Presentation fields are read-only projections from the row-page authority, round-tripped
	// as read, never a second authority; others are refused (content_graph_row_presentation.go).
	row.Icon = ""
	row.IconType = ""
	row.Cover, row.CoverPosition = "", 0
	row.CreatedAt, row.CreatedBy, row.UpdatedAt, row.LastEditedBy = "", "", "", ""
	row.HasContent, row.FullWidth, row.SmallText = false, false, false
	row.ID, err = graphIDField("id", row.ID)
	if err != nil {
		return err
	}
	row.DataSourceID, err = graphIDField("dataSourceId", row.DataSourceID)
	if err != nil {
		return err
	}
	if row.SubitemParentID != nil {
		parentID, err := graphIDField("subitemParentId", *row.SubitemParentID)
		if err != nil {
			return err
		}
		if parentID == row.ID {
			return graphInputError(errorCodeGraphReferenceInvalid, "A row can't be its own parent item.")
		}
		row.SubitemParentID = &parentID
	}
	row.Position, err = graphPositionField("position", row.Position, false)
	if err != nil {
		return err
	}
	// A row writes the values it carries over its others, so a row moved
	// without a value changing carries none.
	if len(row.Values) > maxGraphMutationOperations {
		return graphRefused(errorCodeGraphInvalid, "Check the row values.",
			"values holds %d properties, over %d", len(row.Values), maxGraphMutationOperations)
	}
	normalized := make(map[string]graphRowValue, len(row.Values))
	for rawPropertyID, value := range row.Values {
		propertyID, err := graphIDField("values' property id", rawPropertyID)
		if err != nil {
			return err
		}
		value.Type = strings.TrimSpace(value.Type)
		if value.Type == "text" {
			value.Type = graphValueTypeRichText
		}
		value.Value, err = normalizeGraphRowValue(propertyID, value.Type, value.Value, runtimeWritablePropertyIDs)
		if err != nil {
			return graphRefusalAt(err, "values."+propertyID+".")
		}
		value.Spans, err = normalizeGraphRowValueSpans(value.Type, value.Value, value.Spans)
		if err != nil {
			return err
		}
		normalized[propertyID] = value
	}
	row.Values = normalized
	return nil
}

func normalizeGraphDependencyBinding(binding *graphDependencyBinding) error {
	var err error
	binding.DataSourceID, err = graphIDField("dataSourceId", binding.DataSourceID)
	if err != nil {
		return err
	}
	binding.PredecessorPropertyID, err = graphIDField("predecessorPropertyId", binding.PredecessorPropertyID)
	if err != nil {
		return err
	}
	binding.SuccessorPropertyID, err = graphIDField("successorPropertyId", binding.SuccessorPropertyID)
	if err != nil {
		return err
	}
	if binding.PredecessorPropertyID == binding.SuccessorPropertyID {
		return graphInputError(errorCodeGraphReferenceInvalid, "The predecessor and successor properties must differ.")
	}
	binding.DataKey = strings.TrimSpace(binding.DataKey)
	if binding.DataKey == "" || len(binding.DataKey) > maxGraphIdentifierLength || binding.ExpectedRevision < 0 {
		return graphRefused(errorCodeGraphInvalid, "Check the dependency configuration.",
			"dataKey is 1–%d bytes and expectedRevision 0 or more", maxGraphIdentifierLength)
	}
	return nil
}

func normalizeGraphDependencyEdge(edge *graphDependencyEdge) error {
	var err error
	edge.ID, err = graphIDField("id", edge.ID)
	if err != nil {
		return err
	}
	edge.DataSourceID, err = graphIDField("dataSourceId", edge.DataSourceID)
	if err != nil {
		return err
	}
	edge.PredecessorRowID, err = graphIDField("predecessorRowId", edge.PredecessorRowID)
	if err != nil {
		return err
	}
	edge.SuccessorRowID, err = graphIDField("successorRowId", edge.SuccessorRowID)
	if err != nil {
		return err
	}
	if edge.PredecessorRowID == edge.SuccessorRowID || edge.ExpectedRevision < 1 {
		return graphInputError(errorCodeGraphReferenceInvalid, "Check the dependency link.")
	}
	return nil
}

func normalizeGraphDependencyEdgeDelete(deletion *graphDependencyEdgeDelete) error {
	var err error
	deletion.ID, err = graphIDField("id", deletion.ID)
	if err != nil {
		return err
	}
	deletion.DataSourceID, err = graphIDField("dataSourceId", deletion.DataSourceID)
	if err != nil {
		return err
	}
	if deletion.ExpectedRevision < 1 {
		return graphRefused(errorCodeGraphInvalid, "Check the dependency link deletion details.", "expectedRevision is the link's revision, 1 or more")
	}
	return nil
}

func normalizeGraphRowValue(
	propertyID, valueType string,
	raw json.RawMessage,
	runtimeWritablePropertyIDs map[string]struct{},
) (json.RawMessage, error) {
	value, err := decodeBoundedGraphJSON(raw)
	if err != nil {
		return nil, graphValueAt(err, "value")
	}
	if value == nil {
		if _, clearable := graphClearableValueTypes[valueType]; clearable {
			return graphRowValueCleared, nil
		}
	}
	valid := false
	switch valueType {
	case graphValueTypeTitle, graphValueTypeRichText:
		// An item's title is as long as a page's (maxGraphTitleRunes), a text
		// cell's words a rich text's (graphRichTextFits), formatted or not.
		bound := maxGraphTextCharacters
		if valueType == graphValueTypeTitle {
			bound = maxGraphTitleRunes
		}
		text, isText := value.(string)
		if count := utf8.RuneCountInString(text); isText && count > bound {
			return nil, graphRefused(errorCodeGraphInvalid, "The text is too long.", "value is %d characters, over %d", count, bound)
		}
		valid = isText
	case graphValueTypeSelect, graphValueTypeStatus, graphValueTypeURL, graphValueTypeEmail, graphValueTypePhone:
		_, valid = value.(string)
	case graphValueTypeNumber:
		_, valid = value.(float64)
	case graphValueTypeUniqueID:
		number, numeric := value.(float64)
		_, runtimeWritable := runtimeWritablePropertyIDs[propertyID]
		valid = runtimeWritable && numeric && number >= 1 && number <= 9007199254740991 && math.Trunc(number) == number
	case graphValueTypeCheckbox:
		_, valid = value.(bool)
	case graphValueTypeMultiSelect, graphValueTypePerson:
		_, valid = value.([]any)
	case graphValueTypeFiles:
		items, parseErr := parseGraphFileValueItemsOf(raw, true)
		if parseErr != nil {
			return nil, parseErr
		}
		canonical, marshalErr := json.Marshal(items)
		if marshalErr != nil {
			return nil, graphInputError(errorCodeGraphInvalid, "Check the file property value.")
		}
		return canonical, nil
	case graphValueTypeRelation:
		valid = graphStringArray(value)
	case graphValueTypeDate:
		value, valid = canonicalGraphDateValue(value)
	case graphValueTypeVerification, graphValueTypePlace:
		_, valid = value.(map[string]any)
	case graphValueTypeCreatedTime, graphValueTypeLastEditedTime,
		graphValueTypeCreatedBy, graphValueTypeLastEditedBy, graphValueTypeRollup,
		graphValueTypeFormula, graphValueTypeButton,
		graphValueTypeLocation, graphValueTypeLastVisitedTime:
		return nil, graphRefused(errorCodeGraphInvalid, "A read-only property value can't be saved directly.",
			"value of a %s property is worked out, never written", valueType)
	default:
		return nil, graphRefused(errorCodeGraphInvalid, "Unsupported row property type.", "type %q isn't one of %s", valueType, graphNamesList(graphValueTypes))
	}
	if !valid {
		return nil, graphRefused(errorCodeGraphInvalid, "Check the row property value's type.",
			"value of a %s property is %s", valueType, graphRowValueShape(valueType))
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return nil, graphInputError(errorCodeGraphInvalid, "Check the row property value.")
	}
	return canonical, nil
}

// A null row value clears the cell: an empty value is no cell, so the
// graph authority removes it and its projection instead of keeping a
// per-type empty value. Stored empty values ("" and []) still read as empty.
var graphRowValueCleared = json.RawMessage("null")

var graphClearableValueTypes = map[string]struct{}{
	graphValueTypeTitle: {}, graphValueTypeRichText: {}, graphValueTypeSelect: {}, graphValueTypeStatus: {},
	graphValueTypeURL: {}, graphValueTypeEmail: {}, graphValueTypePhone: {}, graphValueTypeNumber: {},
	graphValueTypeCheckbox: {}, graphValueTypeMultiSelect: {}, graphValueTypePerson: {}, graphValueTypeFiles: {},
	graphValueTypeRelation: {}, graphValueTypeDate: {}, graphValueTypeVerification: {}, graphValueTypePlace: {},
}

func graphRowValueIsCleared(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), graphRowValueCleared)
}

func graphStringArray(value any) bool {
	values, ok := value.([]any)
	if !ok {
		return false
	}
	for _, entry := range values {
		if _, ok := entry.(string); !ok {
			return false
		}
	}
	return true
}

func graphDateValue(value any) bool {
	object, ok := value.(map[string]any)
	if !ok {
		return false
	}
	start, ok := object["start"].(string)
	if !ok || strings.TrimSpace(start) == "" {
		return false
	}
	if end, exists := object["end"]; exists && end != nil {
		if _, ok := end.(string); !ok {
			return false
		}
	}
	return true
}

// graphViewPropertySettingKeys are the settings a view names one property by
// for what it shows: its group, its calendar's date, a list's status shown as
// a box and its dependency dates. A property in Deleted properties stays named
// there; one deleted for good leaves them (withDatabaseViewsOffProperties).
var graphViewPropertySettingKeys = []string{"groupBy", "calendarBy", "statusCheckboxPropertyId", "startDatePropertyId", "endDatePropertyId"}

func graphViewPropertyReferences(raw json.RawMessage) []string {
	var config map[string]any
	if json.Unmarshal(raw, &config) != nil {
		return nil
	}
	keys := graphViewPropertySettingKeys
	result := make([]string, 0, len(keys))
	seen := map[string]struct{}{}
	for _, key := range keys {
		value, _ := config[key].(string)
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func graphRelationTarget(valueType string, raw json.RawMessage) string {
	if valueType != graphValueTypeRelation {
		return ""
	}
	var config struct {
		RelationDatabaseID string `json:"relationDatabaseId"`
	}
	if json.Unmarshal(raw, &config) != nil {
		return ""
	}
	return strings.TrimSpace(config.RelationDatabaseID)
}

func graphRelationRowIDs(raw json.RawMessage) []string {
	var values []string
	if json.Unmarshal(raw, &values) != nil {
		return nil
	}
	return values
}

var graphTextValueTypes = map[string]struct{}{
	graphValueTypeRichText: {}, graphValueTypeURL: {}, graphValueTypeEmail: {}, graphValueTypePhone: {},
}

// graphValueTypesShareRepresentation reports whether cells stored for one
// property type read unchanged as the other. It bounds the type changes the
// native product performs without rewriting existing cells.
func graphValueTypesShareRepresentation(left, right string) bool {
	if left == right {
		return true
	}
	_, leftText := graphTextValueTypes[left]
	_, rightText := graphTextValueTypes[right]
	if leftText && rightText {
		return true
	}
	options := func(value string) bool { return value == graphValueTypeSelect || value == graphValueTypeStatus }
	return options(left) && options(right)
}
