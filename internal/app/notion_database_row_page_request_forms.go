package app

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A database page's properties in a Public API request are read as Notion's
// API reads them once its request check has passed (seen 2026-11-10), in the
// order sent, before any value is:
//
//   - When every value is an object, each is a kind's {"kind": value}: a key
//     no property has is said to be none of the database's, a value of
//     another kind than its property's "Num is expected to be number.", said
//     together; a property that can't be written (a formula, a created time)
//     is passed over, and an "id" or "type" beside the kind is not read.
//   - Otherwise each value is the bare value of its property's kind: a title
//     as its list, a number as itself, a select as its option. Keys no
//     property that can be written has are said first, joined by commas
//     ("Nope,Made"); then each value that isn't its kind's, under one head,
//     "Invalid property value for: "Num"." on a line each.
//   - A property named twice, by its name and its ID, is "Cannot set property
//     with ID: ;~Al", its raw ID.

// notionRowReadOnlyTypes are the kinds of property a page's values can't
// set: a value of any kind for a last visited time is passed over too
// (seen 2026-11-12).
var notionRowReadOnlyTypes = map[string]struct{}{
	graphValueTypeCreatedTime: {}, graphValueTypeLastEditedTime: {},
	graphValueTypeCreatedBy: {}, graphValueTypeLastEditedBy: {},
	graphValueTypeUniqueID: {}, graphValueTypeButton: {},
	graphValueTypeRollup: {}, graphValueTypeFormula: {},
	graphValueTypeLastVisitedTime: {},
}

// notionRowPropertyIndex finds the property a request names: by its name or
// Leafen's ID, then by its public ID, encoded or not (seen 2026-11-09: Notion
// takes "y%7C%3B%5C" and "y|;\"); the title's is "title", and a property
// named so keeps its name.
type notionRowPropertyIndex struct {
	byKey      map[string]storedNotionDatabaseRowPageProperty
	byPublicID map[string]storedNotionDatabaseRowPageProperty
	titleIDs   []string
}

func newNotionRowPropertyIndex(schema []storedNotionDatabaseRowPageProperty) (notionRowPropertyIndex, error) {
	index := notionRowPropertyIndex{
		byKey:      make(map[string]storedNotionDatabaseRowPageProperty, len(schema)*2),
		byPublicID: make(map[string]storedNotionDatabaseRowPageProperty, len(schema)),
	}
	for _, property := range schema {
		if !notionDatabaseRowPagePropertyVisible(property.Type) {
			continue
		}
		if _, duplicate := index.byKey[property.ID]; duplicate {
			return index, errNotionDatabaseRowPageProjectionInvalid
		}
		index.byKey[property.ID] = property
		if existing, duplicate := index.byKey[property.Name]; duplicate && existing.ID != property.ID {
			return index, errNotionDatabaseRowPageProjectionInvalid
		}
		index.byKey[property.Name] = property
		index.byPublicID[notionPropertyIDKey(property.publicID())] = property
		if normalizeNotionDatabaseRowPagePropertyType(property.Type) == graphValueTypeTitle {
			index.titleIDs = append(index.titleIDs, property.ID)
		}
	}
	if len(index.titleIDs) != 1 {
		return index, errNotionDatabaseRowPageProjectionInvalid
	}
	return index, nil
}

func (index notionRowPropertyIndex) find(key string) (storedNotionDatabaseRowPageProperty, bool) {
	if property, found := index.byKey[key]; found {
		return property, true
	}
	property, found := index.byPublicID[notionPropertyIDKey(key)]
	return property, found
}

// notionRowRequestValues reads a request's properties for a database page as
// Notion's API does, giving each property it sets, by Leafen's ID, its
// {"kind": value}, in the order they were sent: their values are read in
// that order too (planNotionDatabaseRowPageRequestValues), so the first
// refused is the first sent, as in Notion (seen 2026-11-10).
func notionRowRequestValues(
	app core.App,
	authorization notionDatabaseRowPageAuthorization,
	properties notionRequestProperties,
) (notionRequestProperties, error) {
	values := make(map[string]json.RawMessage, properties.count())
	if properties.count() == 0 {
		return notionRequestProperties{values: values, isObject: true}, nil
	}
	container := authorization.Container
	schema, err := loadStoredNotionDatabaseRowPageProperties(app, databaseRowPageAuthority{
		OwnerID: container.OwnerID, WorkspaceID: container.WorkspaceID,
		ContainerPageID: container.PageID, DataSourceID: container.DataSource,
	})
	if err != nil {
		return notionRequestProperties{}, err
	}
	index, err := newNotionRowPropertyIndex(schema)
	if err != nil {
		return notionRequestProperties{}, err
	}
	wrapped := true
	for _, key := range properties.keys {
		if raw := bytes.TrimSpace(properties.values[key]); len(raw) == 0 || raw[0] != '{' {
			wrapped = false
		}
	}
	named := make([]storedNotionDatabaseRowPageProperty, len(properties.keys))
	kept := make([]bool, len(properties.keys))
	if wrapped {
		faults := []string{}
		for at, key := range properties.keys {
			property, found := index.find(key)
			if !found {
				faults = append(faults, key+" isn't a property of this database.")
				continue
			}
			if _, readOnly := notionRowReadOnlyTypes[normalizeNotionDatabaseRowPagePropertyType(property.Type)]; readOnly {
				continue
			}
			kind := notionRowValueKind(property)
			object := map[string]json.RawMessage{}
			_ = json.Unmarshal(properties.values[key], &object)
			value, present := object[kind]
			if !present {
				faults = append(faults, key+" is expected to be "+kind+".")
				continue
			}
			named[at], kept[at] = property, true
			values[key] = notionRowWrappedValue(kind, value)
		}
		if len(faults) > 0 {
			return notionRequestProperties{}, notionDatabaseRowPageRequestInvalid("%s", strings.Join(faults, " "))
		}
	} else {
		missing := []string{}
		for at, key := range properties.keys {
			property, found := index.find(key)
			_, readOnly := notionRowReadOnlyTypes[normalizeNotionDatabaseRowPagePropertyType(property.Type)]
			if !found || readOnly {
				missing = append(missing, key)
				continue
			}
			named[at], kept[at] = property, true
		}
		if len(missing) > 0 {
			return notionRequestProperties{}, notionDatabaseRowPageRequestInvalid(
				"No property that can be changed has these IDs or names: %s", strings.Join(missing, ","))
		}
		faults := []string{}
		for at, key := range properties.keys {
			kind := notionRowValueKind(named[at])
			if !notionRowBareValueFits(kind, properties.values[key]) {
				faults = append(faults, "Invalid property value for: "+strconvQuoteJS(key)+".")
				continue
			}
			values[key] = notionRowWrappedValue(kind, properties.values[key])
		}
		if len(faults) > 0 {
			return notionRequestProperties{}, notionDatabaseRowPageRequestInvalid(
				"The request body isn't valid; fix these:\n%s", strings.Join(faults, "\n"))
		}
	}
	byID := notionRequestProperties{values: make(map[string]json.RawMessage, len(values)), isObject: true}
	for at, key := range properties.keys {
		if !kept[at] {
			continue
		}
		if _, duplicate := byID.values[named[at].ID]; duplicate {
			return notionRequestProperties{}, notionDatabaseRowPageRequestInvalid(
				"Cannot set property with ID: %s", notionPropertyRawID(named[at].publicID()))
		}
		byID.keys = append(byID.keys, named[at].ID)
		byID.values[named[at].ID] = values[key]
	}
	return byID, nil
}

// notionRowTitleLineValues are the values of a database page its Markdown's
// title line titles (notionMarkdownAPITitleLine): its title property's, the
// line's words as plain text.
func notionRowTitleLineValues(
	app core.App,
	authorization notionDatabaseRowPageAuthorization,
	words string,
) (notionRequestProperties, error) {
	container := authorization.Container
	schema, err := loadStoredNotionDatabaseRowPageProperties(app, databaseRowPageAuthority{
		OwnerID: container.OwnerID, WorkspaceID: container.WorkspaceID,
		ContainerPageID: container.PageID, DataSourceID: container.DataSource,
	})
	if err != nil {
		return notionRequestProperties{}, err
	}
	index, err := newNotionRowPropertyIndex(schema)
	if err != nil {
		return notionRequestProperties{}, err
	}
	text, _ := json.Marshal([]map[string]any{{"type": "text", "text": map[string]string{"content": words}}})
	titleID := index.titleIDs[0]
	return notionRequestProperties{
		keys: []string{titleID}, values: map[string]json.RawMessage{titleID: notionRowWrappedValue(graphValueTypeTitle, text)},
		isObject: true,
	}, nil
}

// notionRowValueKind is the kind a property's value is written as, as
// Notion's own clients read it.
func notionRowValueKind(property storedNotionDatabaseRowPageProperty) string {
	return notionDatabaseRowPageOfficialPropertyType(normalizeNotionDatabaseRowPagePropertyType(property.Type))
}

func notionRowWrappedValue(kind string, value json.RawMessage) json.RawMessage {
	encoded, _ := json.Marshal(map[string]json.RawMessage{kind: value})
	return encoded
}

// notionRowBareValueFits reports whether a bare value is one its kind holds,
// as that kind's value fits the request check (notionPropertyValueKinds).
func notionRowBareValueFits(kind string, raw json.RawMessage) bool {
	value, err := parseNotionValue(raw)
	if err != nil {
		return false
	}
	for _, field := range notionPropertyValueKinds {
		if field.name == kind {
			return len(field.shape.check(value, []string{"body"})) == 0
		}
	}
	return false
}
