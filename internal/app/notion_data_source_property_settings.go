package app

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/core"
)

// notionRenamedPropertyRefusal refuses a changed property that came in by
// its name (notionDataSourcePropertyChangeShape) with more beside it that
// doesn't fit a kind as a whole (notionDataSourcePropertyRenameShape), as
// Notion's API refuses it (seen 2026-11-12): "<its name as asked> is not a
// valid property schema", a new one's named by its key.
func notionRenamedPropertyRefusal(
	key string,
	raw json.RawMessage,
	object map[string]json.RawMessage,
	current storedNotionDataSourceProperty,
	exists bool,
) error {
	beyond := false
	for field := range object {
		beyond = beyond || field != "name"
	}
	if !beyond {
		return nil
	}
	if value, err := parseNotionValue(raw); err == nil && len(notionDataSourcePropertyRenameShape.check(value, nil)) == 0 {
		return nil
	}
	label := key
	if exists {
		label = current.Name
		_ = json.Unmarshal(object["name"], &label)
	}
	return notionDatabaseRowPageRequestInvalid("%s isn't a property schema this takes", label)
}

// notionRelationKept is a relation request that leaves a relation to the
// same data source, one way or two as it was, which keeps its name as
// Notion's API keeps it (seen 2026-11-12: a "name" beside it is left; one
// turned two-way, made one-way or pointed elsewhere is renamed).
func notionRelationKept(current storedNotionDataSourceProperty, requested map[string]any) bool {
	before := map[string]any{}
	if json.Unmarshal([]byte(current.Config), &before) != nil {
		return false
	}
	twoWay := func(config map[string]any) bool {
		pair, _ := config["relatedPropertyId"].(string)
		return pair != "" && pair != current.ID
	}
	return before["relationDatabaseId"] == requested["relationDatabaseId"] && twoWay(before) == twoWay(requested)
}

var notionUndashedUUID = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)

// notionRelationPairDefaultName is what Notion's API calls the other side of
// a two-way relation it makes without a synced_property_name (seen
// 2026-11-12): "Related to <the data source's title> (<the property>)", an
// untitled source "Untitled Database", a name taken taking a number. One
// made in the app is named as the app names it (database_relation_pairs.go).
func notionRelationPairDefaultName(sourceTitle, property string) string {
	title := strings.TrimSpace(sourceTitle)
	if title == "" {
		title = "Untitled Database"
	}
	name := "Related to " + title + " (" + property + ")"
	if runes := []rune(name); len(runes) > maxGraphIdentifierLength {
		name = strings.TrimSpace(string(runes[:maxGraphIdentifierLength]))
	}
	return name
}

// notionDataSourceRelationRequestConfig reads a relation's settings: one way,
// or two when it names dual_property and not single_property, or its type
// asks for two (seen 2026-11-12: both named make one way). A new pair is
// named by synced_property_name, else as Notion's API names it
// (notionRelationPairDefaultName); one it has keeps its name. A data source
// that isn't found, or can't be read, is refused as Notion refuses it,
// naming the ID dashed and the property's label.
func notionDataSourceRelationRequestConfig(
	app core.App,
	authorization notionDatabaseRowPageAuthorization,
	name, label string,
	object map[string]json.RawMessage,
	current map[string]any,
	propertyID string,
) (map[string]any, error) {
	var publicID string
	if json.Unmarshal(object["data_source_id"], &publicID) != nil {
		return nil, notionDatabaseRowPageRequestInvalid("database property %q relation is invalid", name)
	}
	result := map[string]any{}
	relationKind := ""
	_ = json.Unmarshal(object["type"], &relationKind)
	_, single := object["single_property"]
	dualRaw, dual := object["dual_property"]
	pairName, newPair := "", false
	// dual_property asks for a two-way relation: the server creates the
	// matching property on the target, optionally named — on its own data
	// source too, a second property kept in step with this one, as Notion
	// makes it (2026-03-11 API, seen 2026-10-11).
	if relationKind == "dual_property" || dual && !single {
		dualSettings := struct {
			SyncedPropertyName *string `json:"synced_property_name"`
		}{}
		_ = json.Unmarshal(dualRaw, &dualSettings)
		relationTwoWayPairID := core.GenerateDefaultRandomId()
		newPair = true
		// A relation whose config names itself as its pair has no pair yet.
		if existing, _ := current["relatedPropertyId"].(string); existing != "" && existing != propertyID {
			relationTwoWayPairID, newPair = existing, false
		}
		result["relatedPropertyId"] = relationTwoWayPairID
		if dualSettings.SyncedPropertyName != nil {
			pairName = strings.TrimSpace(*dualSettings.SyncedPropertyName)
		}
	}
	missing := func() error {
		shown := publicID
		if notionUndashedUUID.MatchString(shown) {
			shown = shown[:8] + "-" + shown[8:12] + "-" + shown[12:16] + "-" + shown[16:20] + "-" + shown[20:]
		}
		return notionDatabaseRowPageRequestInvalid("A relation to %s can't be made for property %s", shown, label)
	}
	internalID, err := resolveNotionDataSourceInternalID(app, authorization.Container.WorkspaceID, publicID)
	if err != nil {
		return nil, missing()
	}
	if _, err := authorizeNotionDataSourceParentCurrent(
		app, authorization.Identity, internalID, permissionWorkspaceRead,
	); err != nil {
		return nil, missing()
	}
	result["relationDatabaseId"] = internalID
	// A pair only follows the relation to the database it was made for.
	if current["relationDatabaseId"] != internalID {
		if _, paired := result["relatedPropertyId"]; paired {
			result["relatedPropertyId"], newPair = core.GenerateDefaultRandomId(), true
		}
	} else if _, paired := result["relatedPropertyId"]; !paired {
		// A symmetric relation asked for one way to the same database again
		// stays symmetric, as Notion's API leaves one made on its screen (seen
		// 2026-11-12); one the API makes has no mark (databaseRelationSymmetricKey).
		if symmetric, _ := current[databaseRelationSymmetricKey].(bool); symmetric {
			result[databaseRelationSymmetricKey] = true
		}
	}
	if pairName == "" && newPair {
		pairName = notionRelationPairDefaultName(authorization.Container.Title, label)
	}
	if pairName != "" {
		result["reciprocalName"] = pairName
	}
	return result, nil
}

// notionSchemaOptionNamesRefusal refuses a source's options whose names hold
// a comma, all of them in one message, as Notion's API does (seen
// 2026-11-12), before it looks at any other fault of theirs. A name the
// property's options (current) already have is its own and kept
// (database_option_names.go).
func notionSchemaOptionNamesRefusal(kind, label string, names []string, current map[string]any) error {
	held := map[string]bool{}
	existing, _ := current["options"].([]any)
	for _, raw := range existing {
		if option, ok := raw.(map[string]any); ok {
			name, _ := option["name"].(string)
			held[name] = true
		}
	}
	quoted := []string{}
	for _, name := range names {
		if strings.Contains(name, ",") && !held[name] {
			quoted = append(quoted, strconvQuoteJS(name))
		}
	}
	if len(quoted) == 0 {
		return nil
	}
	return notionDatabaseRowPageRequestInvalid(
		"Option names can't have commas: take the comma out or split the name into options, for all of these: %s property %s: %s.",
		kind, strconvQuoteJS(label), strings.Join(quoted, ", "),
	)
}

// notionSchemaOptionDuplicate is an option named twice, in any case, as
// Notion's API refuses it, naming the later (seen 2026-11-12).
func notionSchemaOptionDuplicate(kind, name string) error {
	return notionDatabaseRowPageRequestInvalid("A %s option can't be there twice: %s", kind, name)
}

// notionSchemaOptionMissing is an option named by an ID its property doesn't
// have, as Notion's API refuses it for a select, a multi-select and a status
// alike (seen 2026-11-12).
func notionSchemaOptionMissing(id string) error {
	return notionDatabaseRowPageRequestInvalid("No select option has the ID: %s", id)
}

// notionDataSourceRequestOptions reads a select's or a multi-select's
// options (seen 2026-11-12): named none, it keeps what it has; else it has
// the ones named, in their order. One with an empty name and no ID is left
// out. One named by its ID or by its name is that one, its name kept, and
// takes the color and description named; any other is new. A new
// property's options are named by name, their IDs left, as a new source's
// are, and one with only an ID makes it "not a valid property schema".
func notionDataSourceRequestOptions(
	propertyName, label, kind string,
	object map[string]json.RawMessage,
	current map[string]any,
	exists bool,
) ([]map[string]any, error) {
	if _, present := object["options"]; !present {
		existing, _ := current["options"].([]any)
		if existing == nil {
			return []map[string]any{}, nil
		}
		encoded, err := json.Marshal(existing)
		if err != nil {
			return nil, err
		}
		preserved := []map[string]any{}
		if json.Unmarshal(encoded, &preserved) != nil {
			return nil, errNotionDatabaseRowPageProjectionInvalid
		}
		return preserved, nil
	}
	type requestOption struct {
		ID          string  `json:"id,omitempty"`
		Name        string  `json:"name,omitempty"`
		Color       string  `json:"color,omitempty"`
		Description *string `json:"description,omitempty"`
	}
	requested := []requestOption{}
	existing := []requestOption{}
	if encoded, err := json.Marshal(current["options"]); err != nil ||
		(encoded != nil && string(encoded) != "null" && json.Unmarshal(encoded, &existing) != nil) {
		return nil, errNotionDatabaseRowPageProjectionInvalid
	}
	if json.Unmarshal(object["options"], &requested) != nil {
		return nil, notionDatabaseRowPageRequestInvalid("database property %q options are invalid", propertyName)
	}
	items := make([]requestOption, 0, len(requested))
	names := make([]string, 0, len(requested))
	for _, item := range requested {
		if item.Name == "" && strings.TrimSpace(item.ID) == "" {
			continue
		}
		items = append(items, item)
		names = append(names, item.Name)
	}
	if err := notionSchemaOptionNamesRefusal(kind, label, names, current); err != nil {
		return nil, err
	}
	folded := map[string]bool{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		key := strings.ToLower(name)
		if name != "" && folded[key] {
			return nil, notionSchemaOptionDuplicate(kind, name)
		}
		folded[key] = true
	}
	if len(items) > maxNotionDatabaseRowPageOptions {
		return nil, notionDatabaseRowPageRequestInvalid("database property %q has too many options", propertyName)
	}
	result := make([]map[string]any, 0, len(items))
	seen := map[string]struct{}{}
	seenNames := map[string]struct{}{}
	byID := make(map[string]requestOption, len(existing))
	byName := make(map[string]requestOption, len(existing))
	for _, option := range existing {
		byID[option.ID] = option
		byName[option.Name] = option
	}
	for _, item := range items {
		if !exists {
			if item.Name == "" {
				return nil, notionDatabaseRowPageRequestInvalid("%s isn't a property schema this takes", propertyName)
			}
			item.ID = ""
		}
		item.ID = strings.TrimSpace(item.ID)
		item.Name = strings.TrimSpace(item.Name)
		item.Color = strings.TrimSpace(item.Color)
		matched := requestOption{}
		if item.ID != "" {
			matched = byID[item.ID]
			if matched.ID == "" {
				return nil, notionSchemaOptionMissing(item.ID)
			}
		} else if item.Name != "" {
			matched = byName[item.Name]
		}
		if matched.ID != "" {
			item.ID, item.Name = matched.ID, matched.Name
			if item.Color == "" {
				item.Color = matched.Color
			}
			if item.Description == nil {
				item.Description = matched.Description
			}
		} else if item.ID == "" {
			item.ID = core.GenerateDefaultRandomId()
		}
		if item.Name == "" || utf8.RuneCountInString(item.Name) > maxGraphIdentifierLength {
			return nil, notionDatabaseRowPageRequestInvalid("database property %q option is invalid", propertyName)
		}
		if item.Color == "" {
			item.Color = "default"
		}
		if _, valid := notionDatabaseRowPageOptionColors[item.Color]; !valid {
			return nil, notionDatabaseRowPageRequestInvalid("database property %q option color is invalid", propertyName)
		}
		_, duplicateID := seen[item.ID]
		_, duplicateName := seenNames[item.Name]
		if duplicateID || duplicateName {
			return nil, notionSchemaOptionDuplicate(kind, item.Name)
		}
		seen[item.ID] = struct{}{}
		seenNames[item.Name] = struct{}{}
		projected := map[string]any{"id": item.ID, "name": item.Name, "color": item.Color}
		if item.Description != nil {
			projected["description"] = *item.Description
		}
		result = append(result, projected)
	}
	return result, nil
}
