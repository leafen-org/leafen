package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A rollup's property item is a list, as Notion's (seen 2026-11-11): one
// that calculates (a sum, a count, a date) lists the related pages as its
// relation's property items and gives its value once the last of them is
// listed, "incomplete" before; one that shows the related values
// (show_original, show_unique) lists the related pages' values of the
// property it rolls up, each page's as that property's own property items
// (its text piece by piece; a page with none takes a turn and lists
// nothing), and gives an empty array, or "unsupported" for show_unique; a
// count of different values or per status group lists them too and gives
// "unsupported".

// notionRollupCursor is where a rollup's listing goes on: the related page
// and the item in it.
type notionRollupCursor struct{ page, item int }

func parseNotionRollupCursor(raw string) (notionRollupCursor, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return notionRollupCursor{}, false, nil
	}
	parts := strings.Split(strings.TrimPrefix(raw, "hnrp1."), ".")
	if !strings.HasPrefix(raw, "hnrp1.") || len(parts) > 2 {
		return notionRollupCursor{}, false, errNotionDatabaseRowPageProjectionInvalid
	}
	cursor := notionRollupCursor{}
	for index, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 || value > 100000 {
			return notionRollupCursor{}, false, errNotionDatabaseRowPageProjectionInvalid
		}
		if index == 0 {
			cursor.page = value
		} else {
			cursor.item = value
		}
	}
	return cursor, true, nil
}

func (cursor notionRollupCursor) String(shows bool) string {
	if !shows {
		return fmt.Sprintf("hnrp1.%d", cursor.page)
	}
	return fmt.Sprintf("hnrp1.%d.%d", cursor.page, cursor.item)
}

// notionRollupPropertyList is a row page's rollup property item from the
// cursor on: its list, or false for a cursor past its end.
func notionRollupPropertyList(
	app core.App, request *http.Request, authorization notionDatabaseRowPageAuthorization,
	rollup notionDatabaseRowPageProjectedProperty, fileIssuer *notionBlockFileCapabilityIssuer,
	cursor notionRollupCursor, cursorPresent bool, pageSize int,
) (map[string]any, bool, error) {
	stored, err := loadStoredNotionDatabaseRowPageProperties(app, authorization.Authority)
	if err != nil {
		return nil, false, err
	}
	relationID, targetID, function := "", "", ""
	var relation storedNotionDatabaseRowPageProperty
	for _, property := range stored {
		if property.ID == rollup.ID {
			config, err := decodeDatabaseComputedConfig(property.Config)
			if err != nil {
				return nil, false, err
			}
			relationID, targetID, _ = config.rollupFields()
			function = notionRollupReadFunction(config)
		}
	}
	for _, property := range stored {
		if relationID != "" && property.ID == relationID && property.Type == graphValueTypeRelation {
			relation = property
		}
	}
	if function == "" {
		function = "show_original"
	}
	value, _ := rollup.PageValue["rollup"].(map[string]any)
	// A share or count of a status group, and a count of different values,
	// list the values they read, as one showing them does, and give none
	// (seen 2026-11-12: "unsupported", over every kind of value, a
	// relation's pages one by one).
	inGroup := function == "percent_per_group" || function == "count_per_group" || function == "unique"
	shows := function == "show_original" || function == "show_unique" || inGroup
	// One a reader sees as empty, or not set up, lists nothing.
	pages := [][]map[string]any{}
	if value != nil && relation.ID != "" {
		if pages, err = notionRollupRelatedItems(app, authorization, relation, targetID, shows, fileIssuer); err != nil {
			return nil, false, err
		}
	}
	if cursorPresent && (cursor.page >= len(pages) || cursor.item > 0 && cursor.item >= len(pages[cursor.page])) {
		return nil, false, nil
	}
	results := []map[string]any{}
	taken, next := 0, cursor
	for next.page < len(pages) && taken < pageSize {
		items := pages[next.page]
		if len(items) == 0 {
			next.page, taken = next.page+1, taken+1
			continue
		}
		results = append(results, items[next.item])
		taken++
		if next.item++; next.item >= len(items) {
			next.page, next.item = next.page+1, 0
		}
	}
	hasMore := next.page < len(pages)
	var nextCursor, nextURL any
	if hasMore {
		encoded := next.String(shows)
		nextCursor = encoded
		if nextURL, err = notionPropertyItemNextURL(app, request, authorization, rollup.PublicID, encoded, pageSize); err != nil {
			return nil, false, err
		}
	}
	switch {
	case function == "show_unique" || inGroup:
		value = map[string]any{"type": "unsupported", "unsupported": map[string]any{}, "function": function}
	case shows:
		value = map[string]any{"type": "array", "array": []any{}, "function": function}
	case hasMore:
		value = map[string]any{"function": function, "type": "incomplete", "incomplete": map[string]any{}}
	}
	return map[string]any{
		"object": "list", "results": results, "next_cursor": nextCursor, "has_more": hasMore,
		"type": "property_item",
		"property_item": map[string]any{
			"id": rollup.PublicID, "next_url": nextURL, "type": "rollup", "rollup": value,
		},
	}, true, nil
}

// notionRollupRelatedItems are the related pages the reader can open, in
// their relation's order, each as the items it lists: its relation's
// property item, or the rolled-up property's items when the rollup shows
// the values.
func notionRollupRelatedItems(
	app core.App, authorization notionDatabaseRowPageAuthorization, relation storedNotionDatabaseRowPageProperty,
	targetID string, shows bool, fileIssuer *notionBlockFileCapabilityIssuer,
) ([][]map[string]any, error) {
	if !shows {
		projected, found, err := loadNotionDatabaseRowPageProperty(app, authorization, relation.ID, fileIssuer)
		if err != nil || !found {
			return nil, err
		}
		pages := make([][]map[string]any, len(projected.Items))
		for index, item := range projected.Items {
			pages[index] = []map[string]any{item}
		}
		return pages, nil
	}
	targets, err := notionRollupRelatedTargets(app, authorization, relation, targetID, fileIssuer)
	if err != nil {
		return nil, err
	}
	pages := make([][]map[string]any, len(targets))
	for index, target := range targets {
		pages[index] = []map[string]any{}
		if target != nil {
			pages[index] = notionPropertyItemsOf(*target)
		}
	}
	return pages, nil
}

// notionRollupShownPageValue is a rollup's page value with the related
// values of one that shows them in place of the "unsupported" its
// calculation leaves (notionDatabaseRowPageRollupValue).
func notionRollupShownPageValue(
	app core.App, authorization notionDatabaseRowPageAuthorization, rollupID string, value map[string]any,
	fileIssuer *notionBlockFileCapabilityIssuer,
) (map[string]any, error) {
	function, _ := value["function"].(string)
	if value["type"] != "unsupported" || function != "show_original" && function != "show_unique" {
		return value, nil
	}
	shown, err := notionRollupShownValues(app, authorization, rollupID, fileIssuer)
	if err != nil {
		return nil, err
	}
	return map[string]any{"type": "array", "array": shown, "function": function}, nil
}

// notionRollupShownValues is the page value of a rollup that shows the
// related values (show_original, show_unique): each related page's value of
// the property it rolls up, as that property reads on the page, by its type
// — every related page, an empty one as its type's empty value, show_unique
// too (seen 2026-11-11: 30 of 30, "number": null for an empty number).
func notionRollupShownValues(
	app core.App, authorization notionDatabaseRowPageAuthorization, rollupID string,
	fileIssuer *notionBlockFileCapabilityIssuer,
) ([]any, error) {
	stored, err := loadStoredNotionDatabaseRowPageProperties(app, authorization.Authority)
	if err != nil {
		return nil, err
	}
	relationID, targetID := "", ""
	for _, property := range stored {
		if property.ID == rollupID {
			config, err := decodeDatabaseComputedConfig(property.Config)
			if err != nil {
				return nil, err
			}
			relationID, targetID, _ = config.rollupFields()
		}
	}
	values := []any{}
	for _, relation := range stored {
		if relationID == "" || targetID == "" || relation.ID != relationID || relation.Type != graphValueTypeRelation {
			continue
		}
		targets, err := notionRollupRelatedTargets(app, authorization, relation, targetID, fileIssuer)
		if err != nil {
			return nil, err
		}
		for _, target := range targets {
			if target == nil {
				continue
			}
			kind, _ := target.PageValue["type"].(string)
			values = append(values, map[string]any{"type": kind, kind: target.PageValue[kind]})
		}
	}
	return values, nil
}

// notionRollupRelatedTargets are the related pages the reader can open, in
// their relation's order, each with the property the rollup shows as it
// reads there, or nil where it has none.
func notionRollupRelatedTargets(
	app core.App, authorization notionDatabaseRowPageAuthorization, relation storedNotionDatabaseRowPageProperty,
	targetID string, fileIssuer *notionBlockFileCapabilityIssuer,
) ([]*notionDatabaseRowPageProjectedProperty, error) {
	relatedIDs := []string{}
	if relation.Value.Valid && json.Unmarshal([]byte(relation.Value.String), &relatedIDs) != nil {
		return nil, errNotionDatabaseRowPageProjectionInvalid
	}
	targetSource, err := notionDatabaseRowPageRelationTarget(relation)
	if err != nil {
		return nil, err
	}
	pages := []*notionDatabaseRowPageProjectedProperty{}
	seen := map[string]struct{}{}
	// The related rows are of one data source, read by one caller: a
	// computed value's query context is loaded once for them all.
	contexts := &databaseQueryContextMemo{}
	for _, relatedID := range relatedIDs {
		if _, duplicate := seen[relatedID]; duplicate || len(pages) >= maxNotionDatabaseRowPageRelations {
			continue
		}
		seen[relatedID] = struct{}{}
		// A row the reader can't open, or in the trash, is left out as its
		// relation leaves it out (projectNotionDatabaseRowPageRelations).
		target, open, err := notionRelationRowOpen(app, authorization.Identity, nil, relatedID)
		if err != nil {
			return nil, err
		}
		if !open || target.Authority.TrashedAt != 0 || target.Authority.DataSourceID != targetSource {
			continue
		}
		var shown *notionDatabaseRowPageProjectedProperty
		if targetID != "" {
			projected, found, err := loadNotionDatabaseRowPagePropertyWith(app, target, targetID, fileIssuer, contexts)
			if err != nil {
				return nil, err
			}
			if found {
				shown = &projected
			}
		}
		pages = append(pages, shown)
	}
	return pages, nil
}

// notionPropertyItemsOf is a property's value as its property items: a
// paginated one's items, any other's one.
func notionPropertyItemsOf(property notionDatabaseRowPageProjectedProperty) []map[string]any {
	if property.Paginated {
		return property.Items
	}
	item := make(map[string]any, len(property.PageValue)+1)
	for key, value := range property.PageValue {
		item[key] = value
	}
	item["object"] = "property_item"
	return []map[string]any{item}
}

// notionPropertyItemNamesNext names, in a paged property item that has
// more, where its next page is read (next_url).
func notionPropertyItemNamesNext(
	app core.App, request *http.Request, authorization notionDatabaseRowPageAuthorization,
	response map[string]any, pageSize int,
) error {
	cursor, more := response["next_cursor"].(string)
	item, paged := response["property_item"].(map[string]any)
	if !more || !paged {
		return nil
	}
	id, _ := item["id"].(string)
	next, err := notionPropertyItemNextURL(app, request, authorization, id, cursor, pageSize)
	item["next_url"] = next
	return err
}

// notionPropertyItemNextURL is where a property item's next page is read,
// as Notion's next_url names it: the page's ID without dashes, the
// property's ID, the cursor and the page size.
func notionPropertyItemNextURL(
	app core.App, request *http.Request, authorization notionDatabaseRowPageAuthorization,
	propertyID, cursor string, pageSize int,
) (string, error) {
	identities, err := ensureNotionDatabaseRowPagePublicIdentities(app, authorization)
	if err != nil {
		return "", err
	}
	return notionPagePropertyNextURL(request, identities.PageID, propertyID, cursor, pageSize)
}

func notionPagePropertyNextURL(request *http.Request, pageID, propertyID, cursor string, pageSize int) (string, error) {
	origin, err := publicServerOrigin(request)
	if err != nil {
		return "", err
	}
	// A property ID Notion gives already escaped is named as it is.
	escaped := propertyID
	if !strings.Contains(propertyID, "%") {
		escaped = url.PathEscape(propertyID)
	}
	return origin + "/v1/pages/" + strings.ReplaceAll(pageID, "-", "") + "/properties/" + escaped +
		"?start_cursor=" + url.QueryEscape(cursor) + "&page_size=" + strconv.Itoa(pageSize), nil
}
