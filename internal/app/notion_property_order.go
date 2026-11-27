package app

import (
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"slices"
	"sort"

	"github.com/pocketbase/pocketbase/core"
)

// notionPropertyObjects is a data source's or a database page's properties
// by name, written in the order Notion's API writes them (seen 2026-11-10,
// in a source's schema, a page's values and a query's rows alike): by their
// raw IDs as Postgres orders a jsonb object's keys, the shorter first, then
// byte by byte — so the title's "title", of five letters, follows every
// four-letter ID. A map alone is written in no set order.
type notionPropertyObjects map[string]any

func (properties notionPropertyObjects) MarshalJSON() ([]byte, error) {
	type entry struct{ name, id string }
	entries := make([]entry, 0, len(properties))
	for name, value := range properties {
		entries = append(entries, entry{name: name, id: notionPropertyOrderID(value)})
	}
	sort.Slice(entries, func(left, right int) bool {
		a, b := entries[left], entries[right]
		if a.id != b.id {
			return notionPropertyOrderLess(a.id, b.id)
		}
		return a.name < b.name
	})
	written := []byte{'{'}
	for index, entry := range entries {
		if index > 0 {
			written = append(written, ',')
		}
		key, err := jsonv2.Marshal(entry.name, jsontext.AllowInvalidUTF8(true))
		if err != nil {
			return nil, err
		}
		value, err := jsonv2.Marshal(properties[entry.name], jsontext.AllowInvalidUTF8(true))
		if err != nil {
			return nil, err
		}
		written = append(append(append(written, key...), ':'), value...)
	}
	return append(written, '}'), nil
}

// notionPropertyOrderLess orders two raw property IDs as Notion writes its
// properties, its API and its MCP's SQLite table alike: as Postgres orders a
// jsonb object's keys, the shorter first, then byte by byte.
func notionPropertyOrderLess(left, right string) bool {
	if len(left) != len(right) {
		return len(left) < len(right)
	}
	return left < right
}

// publicOrdered is the view's source's properties in the order its API lists
// them (by public ID, notionPropertyOrderLess): the order Notion's view
// keeps the properties a view's list leaves out in (seen 2026-11-12: "고친
// 시간·다중·사람…", the title last), not their table's. One without a public
// ID keeps its place after them.
func (scope notionViewFilterScope) publicOrdered() []notionViewProperty {
	ordered := slices.Clone(scope.ordered)
	slices.SortStableFunc(ordered, func(a, b notionViewProperty) int {
		left, right := scope.ids.public[a.ID], scope.ids.public[b.ID]
		switch {
		case left == "" && right == "":
			return 0
		case left == "" || right == "":
			if left == "" {
				return 1
			}
			return -1
		case notionPropertyOrderLess(notionPropertyRawID(left), notionPropertyRawID(right)):
			return -1
		case notionPropertyOrderLess(notionPropertyRawID(right), notionPropertyRawID(left)):
			return 1
		}
		return 0
	})
	return ordered
}

// notionPublicOrderedProperties are a source's properties in the order its
// API lists them (by public ID, notionPropertyOrderLess): the order Notion
// picks a view's first property of a kind in, a new or API-made board's
// group, a calendar's or timeline's date, a chart's X axis and a new list's
// shown property, and the fallback of one whose property went (seen
// 2026-11-12: selects made 선택1, 선택2, 선택3 with IDs "qUdm", "@_mA",
// "Z_j|" grouped a board by 선택2 and, 선택2 deleted, by 선택3; the dates
// alike) — neither their names' nor their making's order.
func notionPublicOrderedProperties(
	app core.App, dataSourceID string, properties []notionMCPDataSourceProperty,
) ([]notionMCPDataSourceProperty, error) {
	ids, err := loadNotionPropertyIDs(app, dataSourceID)
	if err != nil {
		return nil, err
	}
	ordered := slices.Clone(properties)
	slices.SortStableFunc(ordered, func(a, b notionMCPDataSourceProperty) int {
		left, right := notionPropertyRawID(ids.publicID(a.ID)), notionPropertyRawID(ids.publicID(b.ID))
		switch {
		case notionPropertyOrderLess(left, right):
			return -1
		case notionPropertyOrderLess(right, left):
			return 1
		}
		return 0
	})
	return ordered, nil
}

// markNotionPropertyOrder tells a read where each of its properties stands
// in its data source's properties as the API lists them (by public ID,
// notionPropertyOrderLess), so the app lists them where Notion's does from
// the same IDs: a form's "질문 추가" menu, in the order of their IDs as its
// API's (seen 2026-11-12), and a new view's first property of a kind
// (notionPublicOrderedProperties).
func markNotionPropertyOrder(app core.App, items []graphReadItem) error {
	sources := map[string]bool{}
	for _, item := range items {
		if source, _ := item.Value["dataSourceId"].(string); item.Kind == graphReadKindProperty && source != "" {
			sources[source] = true
		}
	}
	places := map[string]int{}
	for source := range sources {
		ids, err := loadNotionPropertyIDs(app, source)
		if err != nil {
			return err
		}
		internal := make([]string, 0, len(ids.public))
		for id := range ids.public {
			internal = append(internal, id)
		}
		sort.Slice(internal, func(left, right int) bool {
			return notionPropertyOrderLess(notionPropertyRawID(ids.public[internal[left]]), notionPropertyRawID(ids.public[internal[right]]))
		})
		for place, id := range internal {
			places[id] = place
		}
	}
	for _, item := range items {
		if id, _ := item.Value["id"].(string); item.Kind == graphReadKindProperty {
			if place, found := places[id]; found {
				item.Value["publicOrder"] = place
			}
		}
	}
	return nil
}

// notionPropertyOrderID is the raw ID a property's object carries.
func notionPropertyOrderID(value any) string {
	object, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	id, _ := object["id"].(string)
	return notionPropertyRawID(id)
}

// notionDateObject is a date as Notion's API writes it: start, end and
// time_zone in that order, in a mention's date and a property's alike (seen
// 2026-11-10), anything else after them by name.
type notionDateObject map[string]any

func (date notionDateObject) MarshalJSON() ([]byte, error) {
	keys := make([]string, 0, len(date))
	for key := range date {
		switch key {
		case "start", "end", "time_zone":
		default:
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	written := []byte{'{'}
	for _, key := range append([]string{"start", "end", "time_zone"}, keys...) {
		value, present := date[key]
		if !present {
			continue
		}
		if len(written) > 1 {
			written = append(written, ',')
		}
		name, err := jsonv2.Marshal(key, jsontext.AllowInvalidUTF8(true))
		if err != nil {
			return nil, err
		}
		encoded, err := jsonv2.Marshal(value, jsontext.AllowInvalidUTF8(true))
		if err != nil {
			return nil, err
		}
		written = append(append(append(written, name...), ':'), encoded...)
	}
	return append(written, '}'), nil
}

// notionDateFields reads a date object a projection made, ordered or not.
func notionDateFields(value any) (map[string]any, bool) {
	switch date := value.(type) {
	case notionDateObject:
		return date, true
	case map[string]any:
		return date, true
	}
	return nil, false
}
