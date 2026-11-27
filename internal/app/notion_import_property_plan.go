package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Notion import brings each database property over as the native property of
// the same kind, with the settings Leafen keeps for it: options and colors,
// status groups, the number format, a formula's expression, a unique ID's
// prefix. A relation or rollup names properties and databases the import may
// bring over later, so it keeps what it points at in Notion (its link) until
// every database has been applied. What a property loses on the way is
// reported against the property, in the database's place on its page.

// notionImportPropertyLink is what a relation or rollup points at in Notion.
type notionImportPropertyLink struct {
	// A relation's Notion database, the data source it relates to when Notion
	// names one, and, for a two-way relation, the Notion property on the
	// other side. A rollup keeps its relation's database and data source.
	RelationDatabaseID   string `json:"relationDatabaseId,omitempty"`
	RelationDataSourceID string `json:"relationDataSourceId,omitempty"`
	SyncedPropertyID     string `json:"syncedPropertyId,omitempty"`
	// A rollup's relation property and the property it reads in the related
	// database, as staged property sources.
	RelationSourceID string `json:"relationSourceId,omitempty"`
	TargetSourceID   string `json:"targetSourceId,omitempty"`
}

type notionDatabasePropertyPlan struct {
	Type   string                    `json:"type"`
	Config map[string]any            `json:"config"`
	Link   *notionImportPropertyLink `json:"link,omitempty"`
}

// linked reports whether the property is made once every database has been
// applied.
func (plan notionDatabasePropertyPlan) linked() bool {
	return plan.Type == graphValueTypeRelation || plan.Type == graphValueTypeRollup
}

// notionGraphValueType is the native property type a Notion property type
// becomes, or "" when Leafen has none.
func notionGraphValueType(providerType string) string {
	return notionDataSourceRequestTypes[providerType]
}

// notionImportNumberFormats are the Notion number formats Leafen shows the
// same way, all of Notion's menu (seen 2026-10-01): each keeps its Notion
// name, but "number_with_commas" is "comma".
var notionImportNumberFormats = func() map[string]string {
	formats := map[string]string{}
	for _, name := range notionNumberFormats {
		formats[name] = name
	}
	formats["number_with_commas"] = "comma"
	return formats
}()

// notionImportRollupFunctions are the Notion rollup functions Leafen
// calculates.
var notionImportRollupFunctions = map[string]struct{}{
	"count": {}, "count_values": {}, "empty": {}, "not_empty": {}, "unique": {}, "show_unique": {},
	"percent_empty": {}, "percent_not_empty": {}, "sum": {}, "average": {}, "median": {}, "min": {},
	"max": {}, "range": {}, "earliest_date": {}, "latest_date": {}, "date_range": {}, "checked": {},
	"unchecked": {}, "percent_checked": {}, "percent_unchecked": {}, "count_per_group": {},
	"percent_per_group": {}, "show_original": {},
}

// notionImportPropertyPlanFor converts one Notion database property into the
// native property it becomes, with the report reasons for what it loses. It
// reports false for a property type Leafen has no property for.
func notionImportPropertyPlanFor(
	database notionProviderObject, key string, property notionProviderProperty,
) (notionDatabasePropertyPlan, []string, bool) {
	kind := notionGraphValueType(property.Type)
	if kind == "" {
		return notionDatabasePropertyPlan{}, nil, false
	}
	plan := notionDatabasePropertyPlan{Type: kind, Config: map[string]any{
		"notionType": property.Type, "notionPropertyId": property.ID,
	}}
	if property.Description != nil && strings.TrimSpace(*property.Description) != "" {
		plan.Config["description"] = strings.TrimSpace(*property.Description)
	}
	settings := map[string]json.RawMessage{}
	_ = json.Unmarshal(notionImportPropertySettings(property), &settings)
	reasons := []string{}
	omitted := func() { reasons = append(reasons, notionImportIssuePropertySettingOmitted) }
	switch kind {
	case graphValueTypeNumber:
		format := "number"
		_ = json.Unmarshal(settings["format"], &format)
		nativeFormat, known := notionImportNumberFormats[format]
		if !known {
			nativeFormat = "number"
			omitted()
		}
		plan.Config["numberFormat"] = nativeFormat
	case graphValueTypeSelect, graphValueTypeMultiSelect:
		options, complete := notionImportPropertyOptions(settings["options"])
		plan.Config["options"] = options
		if !complete {
			omitted()
		}
	case graphValueTypeStatus:
		complete := notionImportStatusConfig(settings, plan.Config)
		if !complete {
			omitted()
		}
	case graphValueTypeFormula:
		expression := ""
		_ = json.Unmarshal(settings["expression"], &expression)
		rewritten, resolved := notionImportFormulaExpression(expression, database)
		if strings.TrimSpace(rewritten) == "" {
			// An empty formula is kept as one that shows nothing.
			rewritten = `""`
		}
		plan.Config["formula"] = rewritten
		if !resolved {
			reasons = append(reasons, notionImportIssueFormulaReferenceUnresolved)
		}
		if len(databaseFormulaUnknownFunctions(rewritten)) > 0 {
			reasons = append(reasons, notionImportIssueFormulaFunctionUnsupported)
		}
	case graphValueTypeRelation:
		link := &notionImportPropertyLink{
			RelationDatabaseID: notionImportRelationDatabaseID(settings), RelationDataSourceID: notionImportRelationDataSourceID(settings),
		}
		relationKind := ""
		_ = json.Unmarshal(settings["type"], &relationKind)
		if relationKind == "dual_property" {
			dual := struct {
				SyncedPropertyID string `json:"synced_property_id"`
			}{}
			_ = json.Unmarshal(settings["dual_property"], &dual)
			link.SyncedPropertyID = notionImportPropertyIDKey(dual.SyncedPropertyID)
			if link.SyncedPropertyID == "" {
				omitted()
			}
		}
		plan.Link = link
	case graphValueTypeRollup:
		link, function := notionImportRollupLink(database, notionImportPropertySettings(property))
		if function == "" {
			function = "show_original"
			omitted()
		}
		plan.Link, plan.Config["rollupFunction"] = link, function
	case graphValueTypeUniqueID:
		prefix := ""
		_ = json.Unmarshal(settings["prefix"], &prefix)
		if prefix = strings.TrimSpace(prefix); prefix != "" {
			plan.Config["idPrefix"] = prefix
		}
	case graphValueTypeButton:
		// Notion does not tell the API what a button does.
		omitted()
	case graphValueTypeCreatedBy, graphValueTypeLastEditedBy:
		// A row keeps Notion's edit time, and its people where the plan maps
		// them (notion_import_row_stamps.go); else the person importing.
		reasons = append(reasons, notionImportIssuePropertyValueRecomputed)
	}
	return plan, reasons, true
}

// notionImportPropertySettings is the object under a property's type. A
// property built in Go rather than read from Notion keeps it in its typed
// field.
func notionImportPropertySettings(property notionProviderProperty) json.RawMessage {
	if len(property.Settings) > 0 {
		return property.Settings
	}
	switch property.Type {
	case "title":
		return property.Title
	case "rich_text":
		return property.RichText
	case "number":
		return property.Number
	case "people":
		return property.People
	}
	return nil
}

// notionImportPropertyName is the name an imported property takes.
func notionImportPropertyName(key string, property notionProviderProperty) string {
	name := strings.TrimSpace(property.Name)
	if name == "" {
		name = strings.TrimSpace(key)
	}
	if utf8.RuneCountInString(name) > maxGraphIdentifierLength {
		name = string([]rune(name)[:maxGraphIdentifierLength])
	}
	return name
}

var notionImportPlainOptionID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// notionImportOptionID is the native ID of a Notion select, status or group
// option. Notion IDs may hold any character, so one that is not plain takes
// a stable digest of itself; a property and its values reach the same ID
// without looking each other up.
func notionImportOptionID(notionID, name string) string {
	id := strings.TrimSpace(notionID)
	if id == "" {
		id = "name:" + strings.TrimSpace(name)
	}
	if notionImportPlainOptionID.MatchString(id) {
		return id
	}
	digest := sha256.Sum256([]byte(id))
	return "notion-" + hex.EncodeToString(digest[:8])
}

type notionImportProviderOption struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Color       string  `json:"color"`
	Description *string `json:"description"`
}

// notionImportPropertyOptions converts a Notion option list, and reports
// whether every option came over with its color.
func notionImportPropertyOptions(raw json.RawMessage) ([]any, bool) {
	provided := []notionImportProviderOption{}
	if len(raw) > 0 && string(raw) != "null" && json.Unmarshal(raw, &provided) != nil {
		return []any{}, false
	}
	options := make([]any, 0, len(provided))
	complete := true
	ids, names := map[string]struct{}{}, map[string]struct{}{}
	for _, item := range provided {
		name := strings.TrimSpace(item.Name)
		id := notionImportOptionID(item.ID, name)
		if name == "" || utf8.RuneCountInString(name) > maxGraphIdentifierLength {
			complete = false
			continue
		}
		if _, duplicate := ids[id]; duplicate {
			complete = false
			continue
		}
		if _, duplicate := names[name]; duplicate {
			complete = false
			continue
		}
		ids[id], names[name] = struct{}{}, struct{}{}
		color := strings.TrimSpace(item.Color)
		if color == "" {
			color = "default"
		}
		if _, valid := notionDatabaseRowPageOptionColors[color]; !valid {
			color = "default"
			complete = false
		}
		option := map[string]any{"id": id, "name": name, "color": color}
		if item.Description != nil && strings.TrimSpace(*item.Description) != "" {
			option["description"] = strings.TrimSpace(*item.Description)
		}
		options = append(options, option)
	}
	return options, complete
}

// notionImportStatusConfig sets a status property's options and groups, as
// Notion groups them, and reports whether everything came over.
func notionImportStatusConfig(settings map[string]json.RawMessage, config map[string]any) bool {
	options, complete := notionImportPropertyOptions(settings["options"])
	config["options"] = options
	provided := []struct {
		ID        string   `json:"id"`
		Name      string   `json:"name"`
		Color     string   `json:"color"`
		OptionIDs []string `json:"option_ids"`
	}{}
	if raw := settings["groups"]; len(raw) > 0 && string(raw) != "null" && json.Unmarshal(raw, &provided) != nil {
		complete = false
	}
	known := map[string]map[string]any{}
	for _, value := range options {
		option := value.(map[string]any)
		known[option["id"].(string)] = option
	}
	// A Notion option ID is kept under its native ID; groups name options by
	// their Notion IDs.
	nativeByNotion := map[string]string{}
	rawOptions := []notionImportProviderOption{}
	_ = json.Unmarshal(settings["options"], &rawOptions)
	for _, option := range rawOptions {
		nativeByNotion[option.ID] = notionImportOptionID(option.ID, option.Name)
	}
	// Its default, named as Notion's API names it; null for none, as a
	// status made from a select has (seen 2026-11-08), whose rows that set
	// none read empty.
	if raw, given := settings["default_option"]; given {
		name := ""
		_ = json.Unmarshal(raw, &name)
		config["defaultOptionId"] = ""
		for _, option := range rawOptions {
			if name != "" && option.Name == name {
				config["defaultOptionId"] = nativeByNotion[option.ID]
				break
			}
		}
	}
	groups := []any{}
	grouped := map[string]struct{}{}
	for _, group := range provided {
		name := strings.TrimSpace(group.Name)
		if name == "" {
			complete = false
			continue
		}
		color := strings.TrimSpace(group.Color)
		if _, valid := notionDatabaseRowPageOptionColors[color]; !valid {
			color = "default"
		}
		optionIDs := []any{}
		for _, notionID := range group.OptionIDs {
			id := nativeByNotion[notionID]
			option, exists := known[id]
			if _, taken := grouped[id]; !exists || taken {
				continue
			}
			grouped[id] = struct{}{}
			option["group"] = name
			// "default" is what the API calls an option with no color of its
			// own, drawn in its group's — a new status's "Not started" (seen
			// 2026-11-14; an option given "기본" reads the same and is rarer):
			// it comes over so, as Leafen's API reads it back.
			if option["color"] == "default" {
				notionStatusOptionColorFromGroup(option)
				option["color"] = color
			}
			optionIDs = append(optionIDs, id)
		}
		groups = append(groups, map[string]any{
			"id": notionImportOptionID(group.ID, name), "name": name, "color": color, "optionIds": optionIDs,
		})
	}
	if len(groups) == 0 {
		return complete
	}
	// An option Notion left out of every group joins the first one.
	first := groups[0].(map[string]any)
	for _, value := range options {
		option := value.(map[string]any)
		id := option["id"].(string)
		if _, taken := grouped[id]; taken {
			continue
		}
		option["group"] = first["name"]
		first["optionIds"] = append(first["optionIds"].([]any), id)
		complete = false
	}
	config["statusGroups"] = groups
	return complete
}

func notionImportRelationDatabaseID(settings map[string]json.RawMessage) string {
	databaseID := ""
	_ = json.Unmarshal(settings["database_id"], &databaseID)
	return strings.TrimSpace(databaseID)
}

// notionImportRelationDataSourceID is the data source a relation read in
// the data source version relates to.
func notionImportRelationDataSourceID(settings map[string]json.RawMessage) string {
	dataSourceID := ""
	_ = json.Unmarshal(settings["data_source_id"], &dataSourceID)
	return strings.TrimSpace(dataSourceID)
}

// notionImportPropertyIDKey is a Notion property ID as its database's schema
// gives it, which is how the import keys each property. Notion encodes the
// IDs in a schema (encodeURIComponent) but not the ones a rollup or a
// two-way relation names in its settings (seen 2026-10-04: a schema's
// "RuX%5E" is a rollup's "RuX^"), so an ID is decoded and encoded again.
func notionImportPropertyIDKey(id string) string {
	id = strings.TrimSpace(id)
	if decoded, err := url.PathUnescape(id); err == nil {
		id = decoded
	}
	const hex = "0123456789ABCDEF"
	key := make([]byte, 0, len(id))
	for index := 0; index < len(id); index++ {
		character := id[index]
		if 'a' <= character && character <= 'z' || 'A' <= character && character <= 'Z' ||
			'0' <= character && character <= '9' || strings.IndexByte("-_.!~*'()", character) >= 0 {
			key = append(key, character)
			continue
		}
		key = append(key, '%', hex[character>>4], hex[character&15])
	}
	return string(key)
}

// notionImportRollupLink finds the relation a rollup reads through and the
// property it reads in the related database, and returns the rollup's
// function, or "" for one Leafen does not calculate.
func notionImportRollupLink(database notionProviderObject, settings json.RawMessage) (*notionImportPropertyLink, string) {
	rollup := struct {
		RelationPropertyID   string `json:"relation_property_id"`
		RelationPropertyName string `json:"relation_property_name"`
		RollupPropertyID     string `json:"rollup_property_id"`
		Function             string `json:"function"`
	}{}
	_ = json.Unmarshal(settings, &rollup)
	link := &notionImportPropertyLink{}
	relationKey := notionImportPropertyIDKey(rollup.RelationPropertyID)
	for name, property := range database.Properties {
		if property.Type != "relation" || (notionImportPropertyIDKey(property.ID) != relationKey &&
			(relationKey != "" || name != rollup.RelationPropertyName)) {
			continue
		}
		relation := map[string]json.RawMessage{}
		_ = json.Unmarshal(notionImportPropertySettings(property), &relation)
		link.RelationSourceID = notionPropertySourceID(database.ID, property.ID)
		if related := notionImportRelationDatabaseID(relation); related != "" && rollup.RollupPropertyID != "" {
			link.TargetSourceID = notionPropertySourceID(related, notionImportPropertyIDKey(rollup.RollupPropertyID))
			link.RelationDatabaseID = related
			link.RelationDataSourceID = notionImportRelationDataSourceID(relation)
		}
	}
	function := strings.TrimSpace(rollup.Function)
	if _, supported := notionImportRollupFunctions[function]; !supported {
		function = ""
	}
	return link, function
}

var notionImportFormulaPropertyToken = regexp.MustCompile(`\{\{notion:block_property:([^:}]+)(?::([^:}]*))?(?::[^}]*)?\}\}`)

// notionImportFormulaExpression rewrites a Notion formula's references to
// this database's properties as prop("name") of the imported properties.
// Notion writes a reference it cannot name as a property token; one that
// names a property of this database becomes its prop(). It reports false
// when the formula names a property the import does not bring over.
func notionImportFormulaExpression(expression string, database notionProviderObject) (string, bool) {
	namesByID := map[string]string{}
	namesByNotionName := map[string]string{}
	for key, property := range database.Properties {
		if notionGraphValueType(property.Type) == "" {
			continue
		}
		name := notionImportPropertyName(key, property)
		namesByID[property.ID] = name
		if decoded, err := url.PathUnescape(property.ID); err == nil {
			namesByID[decoded] = name
		}
		namesByNotionName[property.Name] = name
		namesByNotionName[key] = name
	}
	resolved := true
	// A data source after a database's first is staged by its key; its
	// formulas name it by the database or the data source.
	databaseID, dataSourceID := notionImportKeyedIDs(database.ID)
	current := strings.ReplaceAll(databaseID, "-", "")
	currentSource := strings.ReplaceAll(dataSourceID, "-", "")
	expression = notionImportFormulaPropertyToken.ReplaceAllStringFunc(expression, func(token string) string {
		match := notionImportFormulaPropertyToken.FindStringSubmatch(token)
		source := strings.ReplaceAll(match[2], "-", "")
		if source != "" && strings.Trim(source, "0") != "" && source != current && (currentSource == "" || source != currentSource) {
			resolved = false
			return token
		}
		name, found := namesByID[match[1]]
		if !found {
			if decoded, err := url.PathUnescape(match[1]); err == nil {
				name, found = namesByID[decoded]
			}
		}
		if !found {
			resolved = false
			return token
		}
		return `prop("` + strings.ReplaceAll(strings.ReplaceAll(name, `\`, `\\`), `"`, `\"`) + `")`
	})
	rewritten, _ := rewriteDatabaseFormulaReferences(expression, func(reference string) (string, bool) {
		name, found := namesByNotionName[reference]
		if !found {
			resolved = false
			return "", false
		}
		return name, name != reference
	})
	return rewritten, resolved
}

// notionImportPropertyIssueSource stages one issue about a Notion property
// for the import report, in its database's place.
func notionImportPropertyIssueSource(databaseID, propertySourceID, name, reason string, position int) stagedNotionSource {
	return stagedNotionSource{
		SourceID: propertySourceID + "#" + reason, Kind: "issue", ParentSourceID: databaseID,
		ParentKind: notionImportPropertyIssueParentKind, Position: position, Title: name, Text: reason,
	}
}

// Property issues sit under the database source they belong to; block
// issues under the page or block that held the block.
const notionImportPropertyIssueParentKind = "data_source"

// notionImportLinkedPropertyID is the ID a relation or rollup property takes.
// It follows from the import and the Notion property, so a rollup or the
// other side of a two-way relation can name the property before the import
// has made it.
func notionImportLinkedPropertyID(jobID, sourceID string) string {
	digest := sha256.Sum256([]byte(jobID + "\x00" + sourceID))
	return hex.EncodeToString(digest[:])[:15]
}
