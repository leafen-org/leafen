package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A full-page database of one source of its own shows its source's name, its
// page titled so (full_page_database_name.go), but keeps a name of its own
// apart from it: Notion's keeps the name it was made with while its one
// source is renamed through the API or MCP, and its page takes that name
// again once a second source joins (seen 2026-11-12: "W673 A db" made, its
// source renamed "A one", then "A second" added — the page "W673 A db" over
// "A one", its rows' path "… / W673 A db / A second / 〈row〉"; its source
// renamed to no name, the same). A rename of the source alone keeps that
// name on the database's block (ownTitle, its formatting as ownTitleSpans),
// with the source's name it was kept beside (ownTitleFor). A rename of the
// database itself since — its title typed on the page, its page renamed,
// PATCH /v1/databases — leaves its page another name than that, and the kept
// name goes unused: the page keeps the name it shows.

const (
	databaseOwnTitleKey      = "ownTitle"
	databaseOwnTitleSpansKey = "ownTitleSpans"
	databaseOwnTitleForKey   = "ownTitleFor"
)

// keptDatabaseOwnName is the name a database's block keeps of its own while
// its page shows the source name it was kept beside.
func keptDatabaseOwnName(content map[string]any, shown string) (string, []graphTextSpan, bool) {
	title, kept := content[databaseOwnTitleKey].(string)
	if beside, _ := content[databaseOwnTitleForKey].(string); !kept || beside != shown {
		return "", nil, false
	}
	spans := []graphTextSpan{}
	if raw, formatted := content[databaseOwnTitleSpansKey]; formatted {
		encoded, _ := json.Marshal(raw)
		if json.Unmarshal(encoded, &spans) != nil || spelledName(spans) != title {
			spans = nil
		}
	}
	return title, spans, true
}

// keepFullPageDatabaseOwnName is the change that keeps a full-page database's
// own name on its block as a rename of its one source alone gives the source
// (and so its page) the name renamed; none where the source doesn't name a
// full-page database, or a change already writes its block.
func keepFullPageDatabaseOwnName(
	app core.App, ownerID, pageID, sourceID, renamed string, operations []graphOperation,
) ([]graphOperation, error) {
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil || !isFullPageDatabase(page) {
		return operations, nil
	}
	source, err := fullPageDatabaseTitleSource(app, ownerID, pageID)
	if err != nil || source == nil || source.ID != sourceID {
		return operations, err
	}
	stored, found, err := loadDatabasePageBlock(app, ownerID, pageID)
	if err != nil || !found {
		return operations, err
	}
	for _, operation := range operations {
		if operation.Kind == graphOperationKindUpsertBlock && operation.Block != nil && operation.Block.ID == stored.ID {
			return operations, nil
		}
	}
	content := map[string]any{}
	if json.Unmarshal([]byte(stored.Content), &content) != nil {
		return operations, nil
	}
	shown := page.GetString("title")
	own, spans := shown, source.TitleSpans
	if spelledName(spans) != own {
		spans = nil
	}
	if title, kept, found := keptDatabaseOwnName(content, shown); found {
		own, spans = title, kept
	}
	next := map[string]any{}
	for key, value := range content {
		next[key] = value
	}
	delete(next, databaseOwnTitleKey)
	delete(next, databaseOwnTitleSpansKey)
	delete(next, databaseOwnTitleForKey)
	if own != renamed {
		next[databaseOwnTitleKey], next[databaseOwnTitleForKey] = own, renamed
		if spansFormatted(spans) {
			next[databaseOwnTitleSpansKey] = spans
		}
	}
	if reflect.DeepEqual(next, content) {
		return operations, nil
	}
	encoded, err := json.Marshal(next)
	if err != nil {
		return nil, err
	}
	block := graphBlock{ID: stored.ID, Type: graphBlockTypeInlineDatabase, Position: stored.Position, Content: encoded,
		BaseContent: json.RawMessage(stored.Content)}
	return append(operations, graphOperation{Kind: graphOperationKindUpsertBlock, Block: &block}), nil
}

// nameDatabaseByLeftSource titles a full-page database's page after the
// source a change leaves naming it (databaseNamingSource) where it held two
// or more of its own, or its only one was in the trash with its views, or it
// showed one from another database beside its own: one of several left with
// one by the trash goes by that one's name, as one of one source does (seen
// 2026-11-19, Notion's own name unseen meanwhile), and so does one whose
// linked source is taken away (seen 2026-11-25: a page "F3" over its source
// "F4" titled "F4" once 데이터 소스 관리 took its linked source off). The name
// it went by its block keeps beside that one's (ownTitle, ownTitleFor), for
// its page to take again once another source comes back
// (restoreFullPageDatabaseOwnName). It reports whether it named the page.
func nameDatabaseByLeftSource(app core.App, ownerID string, page *core.Record, input graphMutationInput) (graphMutationInput, bool, error) {
	shown := page.GetString("title")
	if input.PageTitle != nil && *input.PageTitle != shown {
		return input, false, nil
	}
	stored, found, err := loadDatabasePageBlock(app, ownerID, page.Id)
	if err != nil || !found {
		return input, false, err
	}
	if own, live := databaseOwnSources([]byte(stored.Content)); len(own) < 2 && (len(own) == 0 || len(live) > 0) &&
		!databaseShowsLinkedSources([]byte(stored.Content)) {
		return input, false, nil
	}
	was := databaseNamingSource([]byte(stored.Content))
	for index, operation := range input.Operations {
		block := operation.Block
		if operation.Kind != graphOperationKindUpsertBlock || block == nil || block.ID != stored.ID ||
			block.Type != graphBlockTypeInlineDatabase {
			continue
		}
		sourceID := databaseNamingSource(block.Content)
		content, before := map[string]any{}, map[string]any{}
		if sourceID == "" || sourceID == was || json.Unmarshal(block.Content, &content) != nil ||
			json.Unmarshal([]byte(stored.Content), &before) != nil {
			return input, false, nil
		}
		left, err := leftSourceName(app, sourceID, input.Operations)
		if err != nil {
			return input, false, err
		}
		// The name it went by: its page's, as its block or its source spelled it.
		own, spans := shown, databasePageNameSpans([]byte(stored.Content), shown)
		if title, kept, found := keptDatabaseOwnName(before, shown); found {
			own, spans = title, kept
		} else if was != "" {
			spans = sourceNameSpansForPage(app, was, shown)
		}
		for _, key := range []string{databaseOwnTitleKey, databaseOwnTitleSpansKey, databaseOwnTitleForKey, "pageTitleSpans"} {
			delete(content, key)
		}
		if own != left.Title {
			content[databaseOwnTitleKey], content[databaseOwnTitleForKey] = own, left.Title
			if spansFormatted(spans) {
				content[databaseOwnTitleSpansKey] = spans
			}
		}
		named := *block
		if named.Content, err = json.Marshal(content); err != nil {
			return input, false, err
		}
		operations := append([]graphOperation(nil), input.Operations...)
		operations[index].Block = &named
		input.Operations, input.PageTitle = operations, &left.Title
		return input, true, nil
	}
	return input, false, nil
}

// leftSourceName is a source's name as a change leaves it: as the change
// writes it, else as stored.
func leftSourceName(app core.App, sourceID string, operations []graphOperation) (databaseNameRead, error) {
	for _, operation := range operations {
		if operation.Kind == graphOperationKindUpsertDataSource && operation.DataSource != nil &&
			operation.DataSource.ID == sourceID && !operation.DataSource.TitleOmitted {
			return databaseNameRead{Title: operation.DataSource.Title}, nil
		}
	}
	name := databaseNameRead{}
	err := app.DB().NewQuery(`SELECT [[title]] FROM {{leafen_data_sources}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": sourceID}).Row(&name.Title)
	if errors.Is(err, sql.ErrNoRows) {
		return name, nil
	}
	return name, err
}

// restoreFullPageDatabaseOwnName titles a full-page database's page with the
// name its block kept of its own as a change makes it one of several
// sources, that name's formatting kept for its page (pageTitleSpans); a
// kept name gone unused is dropped. It reports whether it named the page.
func restoreFullPageDatabaseOwnName(app core.App, ownerID string, page *core.Record, input graphMutationInput) (graphMutationInput, bool, error) {
	shown := page.GetString("title")
	if input.PageTitle != nil && *input.PageTitle != shown {
		return input, false, nil
	}
	stored, found, err := loadDatabasePageBlock(app, ownerID, page.Id)
	if err != nil || !found {
		return input, false, err
	}
	for index, operation := range input.Operations {
		block := operation.Block
		if operation.Kind != graphOperationKindUpsertBlock || block == nil || block.ID != stored.ID ||
			block.Type != graphBlockTypeInlineDatabase || !pageNamedDatabase(block.Content) {
			continue
		}
		content, before := map[string]any{}, map[string]any{}
		if json.Unmarshal(block.Content, &content) != nil || json.Unmarshal([]byte(stored.Content), &before) != nil ||
			content["linkedDatabaseSource"] == true {
			return input, false, nil
		}
		_, keyed := content[databaseOwnTitleKey]
		title, spans, kept := keptDatabaseOwnName(before, shown)
		if !kept && !keyed {
			return input, false, nil
		}
		delete(content, databaseOwnTitleKey)
		delete(content, databaseOwnTitleSpansKey)
		delete(content, databaseOwnTitleForKey)
		if kept {
			delete(content, "pageTitleSpans")
			if spansFormatted(spans) {
				content["pageTitleSpans"] = spans
			}
		}
		named := *block
		if named.Content, err = json.Marshal(content); err != nil {
			return input, false, err
		}
		operations := append([]graphOperation(nil), input.Operations...)
		operations[index].Block = &named
		input.Operations = operations
		if kept {
			input.PageTitle = &title
		}
		return input, kept, nil
	}
	return input, false, nil
}
