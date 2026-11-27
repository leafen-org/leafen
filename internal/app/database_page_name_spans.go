package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A full-page database whose page's title is its name — one of two or more
// sources of its own (full_page_database_name.go), or a linked one — keeps
// that name's formatting on its block beside the title, as pageTitleSpans,
// as Notion's database name keeps it. The editor writes the name there as
// it is typed, its formatting or none, and the page takes its words in the
// same change, as a one-source database's page takes its source's name. The
// page renamed to other words, from its rename box, the sidebar or the API,
// keeps the formatting and mentions of the words it keeps, the new words
// plain (clearGeneratedDatabaseTitle). Lists, the sidebar, links and search
// show the title's text, its mentions said as a plain title says them.

type storedDatabasePageBlock struct {
	ID       string `db:"id"`
	Position string `db:"position"`
	Content  string `db:"content"`
}

// loadDatabasePageBlock reads the first database at the top of a page's body.
func loadDatabasePageBlock(app core.App, ownerID, pageID string) (storedDatabasePageBlock, bool, error) {
	stored := storedDatabasePageBlock{}
	err := app.DB().NewQuery(`SELECT [[id]],[[position]],[[content]] FROM {{leafen_blocks}} INDEXED BY [[idx_leafen_blocks_page_order]]
	 WHERE [[owner]]={:owner} AND [[page_id]]={:page} AND [[parent_id]]='' AND [[type]]='inline_database'
	 ORDER BY [[position]],[[id]] LIMIT 1`).Bind(dbx.Params{"owner": ownerID, "page": pageID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return stored, false, nil
	}
	return stored, err == nil, err
}

// blockPageNameSpans reads the name formatting a database block keeps; none
// where it keeps none.
func blockPageNameSpans(content []byte, key string) []graphTextSpan {
	fields := map[string]json.RawMessage{}
	spans := []graphTextSpan{}
	if json.Unmarshal(content, &fields) != nil || json.Unmarshal(fields[key], &spans) != nil {
		return nil
	}
	return spans
}

// spelledName is the text a name's formatting spells.
func spelledName(spans []graphTextSpan) string {
	var spelled strings.Builder
	for _, span := range spans {
		spelled.WriteString(span.Text)
	}
	return spelled.String()
}

// databasePageNameSpans is the formatting a database page's name keeps,
// while it spells the page's title.
func databasePageNameSpans(content []byte, title string) []graphTextSpan {
	spans := blockPageNameSpans(content, "pageTitleSpans")
	if len(spans) == 0 || spelledName(spans) != title {
		return nil
	}
	return spans
}

// pageNamedDatabase is whether a database block's page names it: a linked
// one, or one of several sources of its own.
func pageNamedDatabase(content []byte) bool {
	shown := struct {
		DataSourceID string `json:"dataSourceId"`
	}{}
	return json.Unmarshal(content, &shown) == nil && shown.DataSourceID != "" && databaseNamingSource(content) == ""
}

// alignDatabasePageName titles a database's page after the name a change
// writes to its block, as the editor writes one, the generated title of a
// linked starter going with it. A change that leaves the name as it was
// renames nothing.
func alignDatabasePageName(app core.App, ownerID string, page *core.Record, input graphMutationInput) (graphMutationInput, error) {
	if !slices.ContainsFunc(input.Operations, func(operation graphOperation) bool {
		return operation.Kind == graphOperationKindUpsertBlock && operation.Block != nil && operation.Block.Type == graphBlockTypeInlineDatabase
	}) {
		return input, nil
	}
	stored, found, err := loadDatabasePageBlock(app, ownerID, page.Id)
	if err != nil || !found {
		return input, err
	}
	for index, operation := range input.Operations {
		block := operation.Block
		if operation.Kind != graphOperationKindUpsertBlock || block == nil || block.ID != stored.ID ||
			block.Type != graphBlockTypeInlineDatabase || !pageNamedDatabase(block.Content) {
			continue
		}
		spans := blockPageNameSpans(block.Content, "pageTitleSpans")
		if spans == nil || reflect.DeepEqual(spans, blockPageNameSpans([]byte(stored.Content), "pageTitleSpans")) {
			return input, nil
		}
		title := spelledName(spans)
		current := page.GetString("title")
		if input.PageTitle != nil {
			current = *input.PageTitle
		}
		if title == current {
			return input, nil
		}
		content := map[string]any{}
		if err := json.Unmarshal(block.Content, &content); err != nil {
			return input, err
		}
		delete(content, "generatedPageTitle")
		named := *block
		if named.Content, err = json.Marshal(content); err != nil {
			return input, err
		}
		operations := append([]graphOperation(nil), input.Operations...)
		operations[index].Block = &named
		input.Operations, input.PageTitle = operations, &title
		return input, nil
	}
	return input, nil
}

// carrySourceNameToPage gives a full-page database's page its name's
// formatting as a change makes the database one of several sources of its
// own: its one source's until then, while that spells the page's title, as
// Notion's database keeps its name when a second source is added.
func carrySourceNameToPage(app core.App, ownerID string, page *core.Record, sourceID string, operations []graphOperation) ([]graphOperation, error) {
	stored, found := storedDatabasePageBlock{}, false
	for index, operation := range operations {
		block := operation.Block
		if operation.Kind != graphOperationKindUpsertBlock || block == nil || block.Type != graphBlockTypeInlineDatabase ||
			!pageNamedDatabase(block.Content) || blockPageNameSpans(block.Content, "pageTitleSpans") != nil {
			continue
		}
		if !found {
			var err error
			if stored, found, err = loadDatabasePageBlock(app, ownerID, page.Id); err != nil || !found {
				return operations, err
			}
		}
		if block.ID != stored.ID {
			continue
		}
		spans := sourceNameSpansForPage(app, sourceID, page.GetString("title"))
		content := map[string]any{}
		if len(spans) == 0 || json.Unmarshal(block.Content, &content) != nil {
			return operations, nil
		}
		content["pageTitleSpans"] = spans
		named := *block
		encoded, err := json.Marshal(content)
		if err != nil {
			return nil, err
		}
		named.Content = encoded
		next := append([]graphOperation(nil), operations...)
		next[index].Block = &named
		return next, nil
	}
	return operations, nil
}

// sourceNameSpansForPage is a source's name's formatting, while it spells a
// page's title.
func sourceNameSpansForPage(app core.App, sourceID, title string) []graphTextSpan {
	var encoded string
	if app.DB().NewQuery(`SELECT [[title_spans]] FROM {{leafen_data_sources}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": sourceID}).Row(&encoded) != nil {
		return nil
	}
	spans := decodeStoredTextSpans(encoded)
	if len(spans) == 0 || spelledName(spans) != title {
		return nil
	}
	return spans
}

// databaseOwnName is the name a database goes by apart from its first
// source's, as Notion's MCP fetch names a database (seen 2026-11-09: a
// database of two sources by its own title, its formatting left out): a
// full-page one's that its page's title is, a linked one's own, or the one
// an inline one of two or more sources keeps, as its name is decided
// everywhere (databaseContainerNames). None otherwise, or for one not named
// yet. A linked or inline one's comes with the spans its block keeps for it
// (none for plain words), which its reader reads it by; a full-page one's
// with nil, its page's title read as its page's (page_name_mentions.go). Its
// block in the trash alone names it still (storedDatabaseBlockContent). An
// inline one of one source of its own and one it links is one of two, named
// so (databaseNamingSources).
func databaseOwnName(app core.App, ownerID, pageID, blockID string, holder *core.Record) (string, []graphTextSpan, bool) {
	content, found, err := storedDatabaseBlockContent(app, ownerID, pageID, blockID)
	if err != nil || !found {
		return "", nil, false
	}
	fields := struct {
		Linked    bool   `json:"linkedDatabaseSource"`
		Title     string `json:"linkedTitle"`
		Generated string `json:"generatedPageTitle"`
	}{}
	_ = json.Unmarshal([]byte(content), &fields)
	if top, found, err := loadDatabasePageBlock(app, ownerID, pageID); err == nil && found && top.ID == blockID &&
		holder != nil && isFullPageDatabase(holder) {
		// Untitled, it has none: one of several goes by its sources' names
		// (databaseNameFrom).
		title := strings.TrimSpace(holder.GetString("title"))
		if !pageNamedDatabase([]byte(content)) || title == "" || fields.Linked && holder.GetString("title") == fields.Generated {
			return "", nil, false
		}
		return title, nil, true
	}
	if fields.Linked && !pageNamedDatabase([]byte(content)) {
		return "", nil, false
	}
	if title := strings.TrimSpace(fields.Title); fields.Linked && title != "" {
		spans := blockPageNameSpans([]byte(content), "linkedTitleSpans")
		if spelledName(spans) != title {
			spans = []graphTextSpan{}
		}
		return title, spans, true
	}
	if !fields.Linked {
		names, err := databaseContainerNames(app, []string{blockID})
		if name := names[blockID]; err == nil && name.Several && strings.TrimSpace(name.Title) != "" {
			return name.Title, name.Spans, true
		}
	}
	return "", nil, false
}
