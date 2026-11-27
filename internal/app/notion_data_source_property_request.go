package app

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/core"
)

// A table view nobody arranged shows its source's title first and then the
// other properties by name, in Notion's API-made databases even for
// properties added later (seen 2026-11-10: Name, Alpha, Extra, F1, F10, F2 …
// Zed, _x, beta, f5, 가나 — names compared character by character). The
// properties the public API makes take their places in that order:
// notionNamedPropertyKeys orders a new source's, notionNamedPropertyPosition
// places one added to a source.

// notionRequestPropertyName is the name a property added to a data source
// takes: its "name", or else its key — a rollup's always its key, as
// Notion's API leaves a rollup's "name" (seen 2026-11-12).
func notionRequestPropertyName(key string, raw json.RawMessage) string {
	object := map[string]json.RawMessage{}
	name := key
	if json.Unmarshal(raw, &object) == nil {
		_, rollup := object["rollup"]
		if encoded, present := object["name"]; present && !rollup {
			_ = json.Unmarshal(encoded, &name)
		}
	}
	return strings.TrimSpace(name)
}

func notionRequestPropertyIsTitle(raw json.RawMessage) bool {
	object := map[string]json.RawMessage{}
	if json.Unmarshal(raw, &object) != nil {
		return false
	}
	_, title := object["title"]
	kind := ""
	_ = json.Unmarshal(object["type"], &kind)
	return title || kind == "title"
}

// notionNamedPropertyKeys orders a new source's requested properties: the
// title first, the rest by name, which is their key (a new source's
// properties take no other, seen 2026-11-12).
func notionNamedPropertyKeys(properties map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b string) int {
		aTitle, bTitle := notionRequestPropertyIsTitle(properties[a]), notionRequestPropertyIsTitle(properties[b])
		switch {
		case aTitle != bTitle && aTitle:
			return -1
		case aTitle != bTitle:
			return 1
		}
		if order := strings.Compare(strings.TrimSpace(a), strings.TrimSpace(b)); order != 0 {
			return order
		}
		return strings.Compare(a, b)
	})
	return keys
}

// notionNamedPropertyPosition places a property added to a source named
// name: before the first property other than the title, in the source's
// order, whose name sorts after it, else after them all.
func notionNamedPropertyPosition(placed []databaseFormulaReferenceProperty, titleID, name string) (string, error) {
	ordered := slices.Clone(placed)
	slices.SortFunc(ordered, func(a, b databaseFormulaReferenceProperty) int {
		if order := strings.Compare(a.Position, b.Position); order != 0 {
			return order
		}
		return strings.Compare(a.ID, b.ID)
	})
	low := ""
	for _, property := range ordered {
		if property.ID != titleID && property.Name > name {
			return databasePositionBetween(low, property.Position)
		}
		low = property.Position
	}
	return databasePositionAfter(low)
}

// notionDataSourceRequestTypes are Notion's property kinds and the native
// types they become, read from its API and its exports alike.
var notionDataSourceRequestTypes = map[string]string{
	"title": graphValueTypeTitle, "rich_text": graphValueTypeRichText,
	"number": graphValueTypeNumber, "select": graphValueTypeSelect,
	"multi_select": graphValueTypeMultiSelect, "date": graphValueTypeDate,
	"people": graphValueTypePerson, "files": graphValueTypeFiles,
	"checkbox": graphValueTypeCheckbox, "url": graphValueTypeURL,
	"email": graphValueTypeEmail, "phone_number": graphValueTypePhone,
	"created_time": graphValueTypeCreatedTime, "last_edited_time": graphValueTypeLastEditedTime,
	"created_by": graphValueTypeCreatedBy, "last_edited_by": graphValueTypeLastEditedBy,
	"status":  graphValueTypeStatus,
	"formula": graphValueTypeFormula, "relation": graphValueTypeRelation,
	"rollup": graphValueTypeRollup, "unique_id": graphValueTypeUniqueID,
	"button": graphValueTypeButton, "place": graphValueTypePlace,
	"verification": graphValueTypeVerification,
	"location":     graphValueTypeLocation, "last_visited_time": graphValueTypeLastVisitedTime,
}

// errNotionDataSourcePropertyLeft is a requested property Notion's API
// takes and leaves as it is: one of a kind it doesn't make.
var errNotionDataSourcePropertyLeft = errors.New("the property is left as it is")

// notionDataSourceKindLeft is a kind Notion's API names but doesn't make
// (seen 2026-11-12): a button or a location is taken and not made, when
// changing a source only beside a "name" (notionDataSourcePropertyChangeShape).
// A last visited time is made either way, changing a source beside a name,
// and a verification too, as any database may have one (user decision
// 2026-10-30; Notion's API, on a plan without it, doesn't make it).
func notionDataSourceKindLeft(kind string) bool {
	return kind == "button" || kind == "location"
}

// planNotionDataSourceProperty plans one requested property, making makes
// a new source's. Keys a property takes beside its kind are left, as
// Notion's API leaves them. A property's "name" renames it, but not a
// rollup's, a relation's kept as it is (notionRelationKept) nor a new
// source's, whose name is the key (seen 2026-11-12).
func planNotionDataSourceProperty(
	app core.App,
	authorization notionDatabaseRowPageAuthorization,
	key string,
	raw json.RawMessage,
	current storedNotionDataSourceProperty,
	exists bool,
	newPosition string,
	making bool,
) (storedNotionDataSourceProperty, error) {
	object := map[string]json.RawMessage{}
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return storedNotionDataSourceProperty{}, notionDatabaseRowPageRequestInvalid(
			"database property %q configuration is invalid", key,
		)
	}
	if !making {
		if err := notionRenamedPropertyRefusal(key, raw, object, current, exists); err != nil {
			return storedNotionDataSourceProperty{}, err
		}
	}
	// The kind is the first Notion names that the property has; its type,
	// checked with its shape, is left.
	typeKey, nativeType, left := "", "", false
	for _, candidate := range notionDataSourceKindOrder {
		if _, present := object[candidate]; !present {
			continue
		}
		if notionDataSourceKindLeft(candidate) {
			left = true
			continue
		}
		typeKey, nativeType = candidate, notionDataSourceRequestTypes[candidate]
		break
	}
	if typeKey == "" && left {
		return storedNotionDataSourceProperty{}, errNotionDataSourcePropertyLeft
	}
	// A new property by its name alone, or with a kind Notion doesn't have.
	if !exists && typeKey == "" {
		return storedNotionDataSourceProperty{}, notionDatabaseRowPageRequestInvalid("%s isn't a property schema this takes", key)
	}
	name := key
	if exists {
		name = current.Name
	}
	if encoded, present := object["name"]; present && !making && typeKey != "rollup" {
		if json.Unmarshal(encoded, &name) != nil {
			return storedNotionDataSourceProperty{}, notionDatabaseRowPageRequestInvalid(
				"database property %q name is invalid", key,
			)
		}
	}
	// A new source's title may be unnamed, as Notion's (graphPropertyNamed);
	// a rename to no name Notion refuses.
	name = strings.TrimSpace(name)
	if name == "" && !(making && nativeType == graphValueTypeTitle) || utf8.RuneCountInString(name) > maxGraphIdentifierLength {
		return storedNotionDataSourceProperty{}, notionDatabaseRowPageRequestInvalid(
			"database property %q name is invalid", key,
		)
	}
	currentType := ""
	if exists {
		currentType = normalizeNotionDatabaseRowPagePropertyType(current.Type)
		if typeKey == "" {
			nativeType = currentType
		} else if nativeType != currentType && (currentType == graphValueTypeTitle || nativeType == graphValueTypeTitle) {
			return storedNotionDataSourceProperty{}, notionDatabaseRowPageRequestInvalid(
				"database property %q cannot be converted to or from title", key,
			)
		} else if nativeType != currentType && (currentType == graphValueTypeUniqueID || nativeType == graphValueTypeUniqueID) {
			return storedNotionDataSourceProperty{}, notionUniqueIDConversionRefused(current, nativeType == graphValueTypeUniqueID)
		} else if !databasePropertyTypeChangeAllowed(currentType, nativeType) {
			return storedNotionDataSourceProperty{}, notionDatabaseRowPageRequestInvalid(
				"database property %q conversion from %s to %s is not available", key, currentType, nativeType,
			)
		}
	}
	config := map[string]any{}
	if exists && json.Unmarshal([]byte(current.Config), &config) != nil {
		return storedNotionDataSourceProperty{}, errNotionDatabaseRowPageProjectionInvalid
	}
	// Made a status from another type, a property is a new status, as
	// Notion's API makes it; the options it had only lend their IDs
	// (notionDataSourceStatusRequestConfig), and so do those it had when it
	// was last a status, so its rows get their status back
	// (databasePropertyFormerStatusOptions).
	var former []map[string]any
	if exists && nativeType == graphValueTypeStatus && currentType != nativeType {
		former = append(databasePropertyConversionOptions(config), databasePropertyFormerStatusOptions(app, current.ID)...)
	}
	// Made a select or multi-select from another type, it has just the options
	// asked for (database_property_conversion_listed.go); a status's cells,
	// which a select reads as they are, keep naming theirs.
	listed := exists && currentType != nativeType && databasePropertyConversionListsOptions(nativeType)
	var lending []map[string]any
	if listed && graphValueTypesShareRepresentation(currentType, nativeType) {
		lending = databasePropertyConversionOptions(config)
	}
	if exists {
		config = notionDataSourceConvertedConfig(currentType, nativeType, config)
	}
	if listed {
		delete(config, "options")
	}
	if typeKey != "" {
		var err error
		config, err = notionDataSourceRequestConfig(
			app, authorization, key, name, nativeType, object[typeKey], config, former, current.ID,
		)
		if err != nil {
			return storedNotionDataSourceProperty{}, err
		}
	}
	if lending != nil {
		notionDataSourceLendOptionIDs(config, lending)
	}
	if typeKey == "relation" && currentType == graphValueTypeRelation && notionRelationKept(current, config) {
		name = current.Name
	}
	if encoded, present := object["description"]; present {
		var description *string
		if json.Unmarshal(encoded, &description) != nil {
			return storedNotionDataSourceProperty{}, notionDatabaseRowPageRequestInvalid(
				"database property %q description is invalid", key,
			)
		}
		if description == nil || strings.TrimSpace(*description) == "" {
			delete(config, "description")
		} else {
			config["description"] = strings.TrimSpace(*description)
		}
	}
	canonical, err := json.Marshal(config)
	if err != nil {
		return storedNotionDataSourceProperty{}, err
	}
	if !exists {
		current = storedNotionDataSourceProperty{
			ID: core.GenerateDefaultRandomId(), Position: newPosition,
		}
	}
	current.Type, current.Name, current.Config = nativeType, name, string(canonical)
	return current, nil
}

// notionDataSourceRequestConfig reads a kind's settings, which its shape
// has checked (notionDataSourceKindSettingsShape): keys beside them are
// left, as Notion's API leaves them. label is the property as its messages
// name it: its name as asked, else its own, else its key.
func notionDataSourceRequestConfig(
	app core.App,
	authorization notionDatabaseRowPageAuthorization,
	name, label, nativeType string,
	raw json.RawMessage,
	current map[string]any,
	former []map[string]any,
	propertyID string,
) (map[string]any, error) {
	object := map[string]json.RawMessage{}
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return nil, notionDatabaseRowPageRequestInvalid("database property %q settings are invalid", name)
	}
	result := map[string]any{}
	keepDatabasePropertyOwnSettings(current, result)
	switch nativeType {
	case graphValueTypeTitle, graphValueTypeRichText, graphValueTypeCheckbox,
		graphValueTypeDate, graphValueTypePerson, graphValueTypeFiles,
		graphValueTypeURL, graphValueTypeEmail, graphValueTypePhone,
		graphValueTypeCreatedTime, graphValueTypeLastEditedTime,
		graphValueTypeCreatedBy, graphValueTypeLastEditedBy,
		graphValueTypeButton, graphValueTypePlace, graphValueTypeVerification,
		graphValueTypeLocation, graphValueTypeLastVisitedTime:
	case graphValueTypeNumber:
		// A format Notion doesn't name reaches here only beside another kind
		// that fits (notionDataSourceKindOrder); Notion's API keeps it, and
		// Leafen shows it as a plain number. Without one ({"number":{}}) the
		// property has none, and one it had goes, as Notion's (seen
		// 2026-11-14: its API still reads "number", its MCP names none).
		if raw, present := object["format"]; present {
			format := "number"
			_ = json.Unmarshal(raw, &format)
			nativeFormat, supported := notionImportNumberFormats[format]
			if !supported {
				nativeFormat = "number"
			}
			result["numberFormat"] = nativeFormat
		}
	case graphValueTypeSelect, graphValueTypeMultiSelect:
		kind := "select"
		if nativeType == graphValueTypeMultiSelect {
			kind = "multi_select"
		}
		deletes, err := takeNotionOptionDeletes(name, object, current)
		if err != nil {
			return nil, err
		}
		options, err := notionDataSourceRequestOptions(name, label, kind, object, current, propertyID != "")
		if err != nil {
			return nil, err
		}
		result["options"] = withNotionOptionDeletes(options, deletes)
	case graphValueTypeUniqueID:
		var prefix *string
		if encoded, present := object["prefix"]; present && json.Unmarshal(encoded, &prefix) != nil {
			return nil, notionDatabaseRowPageRequestInvalid("database property %q prefix is invalid", name)
		}
		// No prefix, or an empty one, leaves the one it has, as Notion's
		// API does (seen 2026-11-11).
		if prefix == nil || *prefix == "" {
			if kept, ok := current["idPrefix"].(string); ok && kept != "" {
				result["idPrefix"] = kept
			}
			break
		}
		kept, ok := uniqueIDPrefix(*prefix)
		if !ok {
			return nil, uniqueIDPrefixRefused(*prefix)
		}
		result["idPrefix"] = kept
	case graphValueTypeFormula:
		// No expression, or an empty one, is an empty formula; one that
		// doesn't read as one, blank included, is refused with what Notion's
		// parser says of it, as Notion refuses it (seen 2026-11-12).
		// One that reads is checked, and kept as Notion keeps it, once the
		// request's properties are known (checkNotionFormulaTypes).
		expression := ""
		_ = json.Unmarshal(object["expression"], &expression)
		if fault := notionFormulaParseFault(expression); fault != "" {
			return nil, notionDatabaseRowPageRequestInvalid("Parse error in formula \"%s\": %s", label, fault)
		}
		result["formula"] = expression
		if expression != "" {
			result[notionFormulaRequestField] = map[string]any{"expression": expression, "label": label}
		}
	case graphValueTypeRelation:
		relation, err := notionDataSourceRelationRequestConfig(app, authorization, name, label, object, current, propertyID)
		if err != nil {
			return nil, err
		}
		for field, value := range relation {
			result[field] = value
		}
	case graphValueTypeRollup:
		// The relation and property are found once the whole request is planned.
		result[notionRollupRequestField] = notionDataSourceRollupRequest(name, object)
	case graphValueTypeStatus:
		deletes, err := takeNotionOptionDeletes(name, object, current)
		if err != nil {
			return nil, err
		}
		status, err := notionDataSourceStatusRequestConfig(name, label, object, current, former)
		if err != nil {
			return nil, err
		}
		if options, listed := status["options"].([]map[string]any); listed {
			status["options"] = withNotionOptionDeletes(options, deletes)
		}
		for field, value := range status {
			result[field] = value
		}
	default:
		return nil, &notionDatabaseRowPagePropertyTypeError{Type: nativeType}
	}
	return result, nil
}
