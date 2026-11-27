package app

import (
	"encoding/json"
	"net/url"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Notion import reads a Notion view into a native view config through the
// normalization the view API uses for the same request (updatedViewConfig),
// so an imported view holds what creating it through the API would. First it
// points each property the view names at the imported property, by Notion's
// property ID or by name, and sets aside what that normalization cannot keep:
// conditions, sorts and settings on properties the import did not bring,
// timestamp conditions, and settings Leafen has no place for. Each setting is
// then applied on its own, so one it refuses leaves the others. What is set
// aside is reported against the view.

type notionImportViewConverter struct {
	scope notionViewFilterScope
	// notionIDs maps each imported property's Notion ID, unescaped, to it.
	notionIDs map[string]string
	// sourceProperties holds what the data source had when it was read: its
	// properties' Notion IDs, unescaped, and names.
	sourceProperties map[string]bool
}

// inSource reports whether the data source had the property a view names.
func (converter *notionImportViewConverter) inSource(reference any) bool {
	text, _ := reference.(string)
	return text != "" && converter.sourceProperties[notionImportPropertyKey(text)]
}

func newNotionImportViewConverter(
	app core.App, dataSourceID, pageID string, rowToNative func(string) (string, bool),
) (*notionImportViewConverter, error) {
	properties, err := notionViewScopeProperties(app, dataSourceID, pageID)
	if err != nil {
		return nil, err
	}
	stored := []struct {
		ID     string `db:"id"`
		Config string `db:"config"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[config]] FROM {{leafen_database_properties}}
		WHERE [[data_source_id]]={:dataSourceId} AND [[page_id]]={:pageId}
	`).Bind(dbx.Params{"dataSourceId": dataSourceID, "pageId": pageID}).All(&stored); err != nil {
		return nil, err
	}
	converter := &notionImportViewConverter{scope: newNotionViewFilterScope(properties), notionIDs: map[string]string{}}
	// A view lists every property it shows or hides, however many, and its
	// filter is read as Notion's views give it.
	converter.scope.wholePropertyLists = true
	converter.scope.readsNotionForm = true
	for _, property := range stored {
		config := struct {
			NotionPropertyID string `json:"notionPropertyId"`
		}{}
		if _, visible := converter.scope.byID[property.ID]; visible &&
			json.Unmarshal([]byte(property.Config), &config) == nil && config.NotionPropertyID != "" {
			converter.notionIDs[notionImportPropertyKey(config.NotionPropertyID)] = property.ID
		}
	}
	// A person is the imported person until someone maps it to a member, as
	// in the rows' values; "me" is whoever looks at the view.
	converter.scope.userToNative = func(id string) (string, bool) {
		id = strings.TrimSpace(id)
		return importedPeopleSourcePrefix + id, id != ""
	}
	converter.scope.rowToNative = rowToNative
	return converter, nil
}

// notionImportPropertyKey is a Notion property ID as Notion writes it
// unescaped; a view and its database may escape the same ID differently.
func notionImportPropertyKey(id string) string {
	if unescaped, err := url.PathUnescape(id); err == nil {
		return unescaped
	}
	return id
}

// property finds the imported property a Notion view names.
func (converter *notionImportViewConverter) property(reference any) (notionViewProperty, bool) {
	text, _ := reference.(string)
	if text == "" {
		return notionViewProperty{}, false
	}
	if id, found := converter.notionIDs[notionImportPropertyKey(text)]; found {
		return converter.scope.byID[id], true
	}
	property, found := converter.scope.byName[text]
	return property, found
}

type notionImportViewReasons map[string]struct{}

func (reasons notionImportViewReasons) add(reason string) { reasons[reason] = struct{}{} }

func (reasons notionImportViewReasons) sorted() []string {
	result := make([]string, 0, len(reasons))
	for reason := range reasons {
		result = append(result, reason)
	}
	sort.Strings(result)
	return result
}

// importViewPlacedRowsMax is how many rows a view an import makes places at
// most. A view's settings travel whole, and a file import stages a view in
// one chunk (workspaceImportStagedChunkBytes), so a file of more rows than
// this keeps the database's one order: its rows in the file's order, by their
// positions, and a row made later at the end.
const importViewPlacedRowsMax = 20000

// importViewBase is what a view an import makes starts from — every view of a
// Notion import, the tables a database is made with, a CSV's or an export
// ZIP's database: its own row order (the web's viewRowOrder.ts) with the rows
// placed whose order the import knows, in that order. A file gives its rows'
// order, and Notion's CSV import places them so (seen 2026-11-12): they show
// in the file's order, a row made later through the API follows them all,
// newest first, and New puts its row on top. Notion's API doesn't give the
// order people arranged a view's rows in
// (notionImportIssueViewRowOrderUnavailable), so a Notion import places
// none: the rows show newest first, as the API lists them, and a row made
// later — through the API, MCP, New or another view — takes the place it
// takes in a Notion view with none placed: the top (seen 2026-11-12).
func importViewBase(placed []string) map[string]any {
	if len(placed) > importViewPlacedRowsMax {
		return map[string]any{}
	}
	return map[string]any{"rowOrder": append([]string{}, placed...)}
}

// config reads a staged Notion view into a native view config over base,
// with the reasons for what it left out.
func (converter *notionImportViewConverter) config(
	plan notionImportViewPlan, base map[string]any,
) (json.RawMessage, notionImportViewReasons, error) {
	reasons := notionImportViewReasons{}
	current, err := json.Marshal(base)
	if err != nil {
		return nil, nil, err
	}
	apply := func(filter, sorts, quickFilters, configuration json.RawMessage) {
		next, err := converter.scope.updatedViewConfig(plan.Type, string(current), filter, sorts, quickFilters, configuration)
		if err != nil {
			reasons.add(notionImportIssueViewSettingOmitted)
			return
		}
		current = next
	}
	if plan.Truncated {
		reasons.add(notionImportIssueViewSettingOmitted)
	}
	// A filter the view's settings refuse whole keeps the conditions they
	// take one by one: one that can't stand doesn't take the others along.
	if filter := converter.filter(plan.Filter, reasons); filter != nil {
		if kept := converter.filterParts(plan.Type, string(current), filter, reasons); kept != nil {
			apply(kept, nil, nil, nil)
		}
	}
	if sorts := converter.sorts(plan.Sorts, reasons); sorts != nil {
		apply(nil, sorts, nil, nil)
	}
	for _, quickFilter := range converter.quickFilters(plan.QuickFilters, reasons) {
		apply(nil, nil, quickFilter, nil)
	}
	for _, field := range converter.configuration(plan.Type, plan.Configuration, reasons) {
		apply(nil, nil, nil, field)
	}
	// Notion doesn't say whether a view's filter bar is open. It stands closed,
	// with the filter and sort buttons blue, as a new view's does.
	config := map[string]any{}
	if err := json.Unmarshal(current, &config); err != nil {
		return nil, nil, err
	}
	config["filterBarHidden"] = true
	// Nor whether a board wraps its cards. It does until someone turns it off,
	// as a new board's (seen 2026-11-05: a board made through its API wraps).
	if plan.Type == graphViewTypeBoard {
		config["wrap"] = true
	}
	if current, err = json.Marshal(config); err != nil {
		return nil, nil, err
	}
	return current, reasons, nil
}

func notionImportJSON(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return encoded
}
