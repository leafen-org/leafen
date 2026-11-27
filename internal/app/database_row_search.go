package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database item is found by search as a page is, as in Notion: its title,
// the text of its property values and its body are indexed under the row's
// ID beside pages, and the search reads it through its database's page. The
// index is one for everyone who may open the page, so the values of a
// property under an access rule stay out of it.

type databaseRowSearchSource struct {
	OwnerID         string `db:"owner"`
	WorkspaceID     string `db:"workspace_id"`
	ContainerPageID string `db:"container_page_id"`
	DataSourceID    string `db:"data_source_id"`
}

type databaseRowSearchValue struct {
	PropertyID string `db:"property_id"`
	Type       string `db:"type"`
	Config     string `db:"config"`
	Value      string `db:"value"`
	Spans      string `db:"spans"`
}

// markDatabaseRowsSearchDirty queues database items whose title, values or
// body changed for the search index.
func markDatabaseRowsSearchDirty(ctx context.Context, app core.App, workspaceID string, rowIDs []string) error {
	targets := make([]searchDirtyTarget, 0, len(rowIDs))
	seen := map[string]struct{}{}
	for _, rowID := range rowIDs {
		if _, duplicate := seen[rowID]; duplicate || rowID == "" {
			continue
		}
		seen[rowID] = struct{}{}
		targets = append(targets, searchDirtyTarget{PageID: rowID, WorkspaceID: workspaceID})
	}
	// Their bodies' reminders and mentions are read again with them, in the
	// page queue.
	return markSearchDirtyTargets(ctx, app, targets)
}

// markDatabaseRowSearchDirty queues a database page whose body changed: a
// row's, or a template's (workspace_search_databases.go).
func markDatabaseRowSearchDirty(app core.App, rowID string) error {
	var workspaceID string
	err := app.DB().NewQuery(`
		SELECT [[workspace_id]] FROM {{leafen_database_row_pages}} WHERE [[row_id]] = {:rowId}
		UNION ALL SELECT [[workspace_id]] FROM {{leafen_database_templates}} WHERE [[id]] = {:rowId}
		LIMIT 1
	`).Bind(dbx.Params{"rowId": rowID}).Row(&workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return markDatabaseRowsSearchDirty(context.Background(), app, workspaceID, []string{rowID})
}

// buildSearchRowProjection indexes a database item, or reads it for a reader
// some pages are closed to (closed, nil for the index); found is false when
// the ID names no database item. Its title is shown with its mentions' words
// (Title) and found without them (TitleFolded), as its other words are.
func buildSearchRowProjection(app core.App, dirty storedSearchDirty, closed searchMentionCloser) (searchPageProjection, bool, error) {
	source := databaseRowSearchSource{}
	err := app.DB().NewQuery(`
		SELECT [[owner]], [[workspace_id]], [[container_page_id]], [[data_source_id]]
		FROM {{leafen_database_row_pages}} WHERE [[row_id]] = {:rowId}
	`).Bind(dbx.Params{"rowId": dirty.PageID}).One(&source)
	if errors.Is(err, sql.ErrNoRows) {
		return searchPageProjection{}, false, nil
	}
	if err != nil {
		return searchPageProjection{}, false, err
	}
	if source.WorkspaceID != dirty.WorkspaceID {
		return searchPageProjection{}, false, errors.New(errorCodeWorkspaceSearchProjectionFailed)
	}
	values := []databaseRowSearchValue{}
	if err := app.DB().NewQuery(`
		SELECT [[property]].[[id]] AS [[property_id]], [[property]].[[type]], [[property]].[[config]], [[value]].[[value]], [[value]].[[spans]]
		FROM {{leafen_database_row_values}} [[value]]
		JOIN {{leafen_database_properties}} [[property]] ON [[property]].[[id]] = [[value]].[[property_id]]
		WHERE [[value]].[[row_id]] = {:rowId} AND ` + databasePropertyNotTrashedSQL("[[property]].[[id]]") + `
		ORDER BY [[property]].[[position]], [[property]].[[id]]
	`).Bind(dbx.Params{"rowId": dirty.PageID}).All(&values); err != nil {
		return searchPageProjection{}, false, err
	}
	rules, err := loadDatabasePropertyAccessRules(app, []string{source.DataSourceID})
	if err != nil {
		return searchPageProjection{}, false, err
	}
	title, titleWords, titled := "", "", false
	parts := []string{}
	// A person mention is kept with the person's name now (person_mention_names.go),
	// and whom the cells mention is kept beside the entry (search_cell_person_mentions.go).
	names := map[string]string{}
	mentioned := []string{}
	for _, value := range values {
		if rules[source.DataSourceID][value.PropertyID] != nil {
			continue
		}
		text := databaseRowSearchValueText(value)
		shown := text
		if spans := databaseStoredSpans(value.Spans); len(spans) > 0 && (value.Type == graphValueTypeTitle || value.Type == graphValueTypeRichText) {
			for _, span := range spans {
				if span.Mention == "person" && span.UserID != "" {
					mentioned = append(mentioned, span.UserID)
				}
			}
			text, shown = searchSpansText(app, dirty.WorkspaceID, spans, names, closed)
		}
		if value.Type == graphValueTypeTitle && !titled && shown != "" {
			title, titleWords, titled = shown, text, true
			continue
		}
		if text == "" {
			continue
		}
		// A value, as a block, is one line of the item's search text
		// (workspace_search_passage.go).
		parts = append(parts, searchBlockText(text))
	}
	partial := false
	if parts, err = appendSearchRowPageBody(app, dirty.WorkspaceID, dirty.PageID, names, closed, parts, &partial); err != nil {
		return searchPageProjection{}, false, err
	}
	// The documents its file properties hold follow, as their words reach
	// whoever may read the page.
	propertyFiles, err := searchPropertyFileTexts(app, dirty.PageID, func(propertyID string) bool {
		return rules[source.DataSourceID][propertyID] != nil
	})
	if err != nil {
		return searchPageProjection{}, false, err
	}
	for _, file := range propertyFiles {
		parts = appendSearchFileText(parts, file, &partial)
	}
	body := strings.Join(parts, "\n")
	if utf8.RuneCountInString(body) > maxWorkspaceSearchBodyRunes {
		return searchPageProjection{}, false, errors.New(errorCodeWorkspaceSearchDocumentTooLarge)
	}
	return searchPageProjection{
		PageID: dirty.PageID, WorkspaceID: dirty.WorkspaceID, OwnerID: source.OwnerID,
		Title: title, TitleFolded: foldSearchText(titleWords), Body: body, BodyFolded: foldSearchText(body),
		SourceVersion: dirty.Version, Partial: partial, Row: true, CellMentions: mentioned,
	}, true, nil
}

// appendSearchRowPageBody adds a database page's body — a row's or a
// template's — to its search text: each block's text, a sub-page's title and
// a held database's name (searchPageChildLines), and a document's words where
// its file block stands, in the line order a page's search text keeps
// (searchBlocksInLineOrder), by its blocks' places, as a page's body is
// (seen 2026-11-22 twice: Notion's MCP search finds a row by its sub-page
// "plinzokr 하위 둘" alone, and by a database "quordelv 행 안 디비" in its
// body, with those lines, and shows "**trembixq** 본문 앞 --- **trembixq** 하위
// 셋 --- **trembixq** 본문 뒤" for a sub-page between two paragraphs). Its
// text lives in the page's document. A reader some pages are closed to
// (closed) doesn't see a sub-page closed to them on the page, nor its line.
func appendSearchRowPageBody(
	app core.App, workspaceID, pageID string, names map[string]string, closed searchMentionCloser, parts []string, partial *bool,
) ([]string, error) {
	blocks, wordless, err := searchPlacedBlocksInLineOrder(app, pageID)
	if err != nil {
		return nil, err
	}
	_, lines, err := searchPageChildLines(app, pageID)
	if err != nil {
		return nil, err
	}
	textDocument, err := loadRowPageTextDocument(app, pageID)
	if err != nil {
		return nil, err
	}
	fileTexts, err := searchFileTexts(app, pageID)
	if err != nil {
		return nil, err
	}
	for _, blockID := range blocks {
		if line, named := lines[blockID]; named {
			if !searchMentionClosed(closed, "page", line.PageID, "") {
				parts = append(parts, line.Title)
			}
		} else if text, file := fileTexts[blockID]; file {
			parts = appendSearchFileText(parts, text, partial)
		} else if wordless[blockID] {
			// A divider's text is none of its words (graphBlockWordless).
			continue
		} else if spans, text := rowPageBlockTextAsItStands(textDocument, blockID, false); text != "" {
			if words, _ := searchSpansText(app, workspaceID, spans, names, nil); words != "" {
				parts = append(parts, searchBlockText(words))
			}
		}
	}
	return parts, nil
}

// requeueSearchRowChildLines is migration 846's content: the entry of a
// database page — a row's or a template's — whose body holds a sub-page or a
// database is made again whole, with their lines (appendSearchRowPageBody),
// which entries made before left out.
func requeueSearchRowChildLines(app core.App) error {
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_search_dirty}} ([[page_id]], [[workspace_id]], [[version]], [[enqueued_at]])
		SELECT [[entry]].[[page_id]], [[entry]].[[workspace_id]], 1, {:now} FROM {{leafen_search_pages}} [[entry]]
		WHERE ([[entry]].[[page_id]] IN (SELECT [[row_id]] FROM {{leafen_database_row_pages}})
				OR [[entry]].[[page_id]] IN (SELECT [[id]] FROM {{leafen_database_templates}}))
			AND EXISTS (SELECT 1 FROM {{leafen_blocks}} [[block]] INDEXED BY [[idx_leafen_blocks_search_projection]]
				WHERE [[block]].[[page_id]] = [[entry]].[[page_id]] AND [[block]].[[type]] IN ({:childPage}, {:database}))
		ON CONFLICT ([[page_id]]) DO UPDATE SET
			[[workspace_id]] = excluded.[[workspace_id]],
			[[version]] = [[leafen_search_dirty]].[[version]] + 1,
			[[enqueued_at]] = excluded.[[enqueued_at]]
	`).Bind(dbx.Params{
		"now": time.Now().UnixMilli(), "childPage": graphBlockTypeChildPage, "database": graphBlockTypeInlineDatabase,
	}).Execute()
	return err
}

func validateSearchRowChildLinesRequeued(app core.App) error {
	for _, table := range []string{"leafen_search_pages", "leafen_search_dirty", "leafen_database_row_pages", "leafen_database_templates", "leafen_blocks"} {
		if !app.HasTable(table) {
			return fmt.Errorf("search row child lines: the table %s is missing", table)
		}
	}
	return nil
}

// databaseRowSearchIndexesType reports whether search keeps a type's values.
func databaseRowSearchIndexesType(propertyType string) bool {
	switch propertyType {
	case graphValueTypeTitle, graphValueTypeRichText, graphValueTypeURL, graphValueTypeEmail, graphValueTypePhone,
		graphValueTypeNumber, graphValueTypeSelect, graphValueTypeStatus, graphValueTypeMultiSelect:
		return true
	}
	return false
}

// databaseRowSearchValueText is the searchable text of a property value:
// text as written, numbers as digits, and the names of chosen options.
func databaseRowSearchValueText(value databaseRowSearchValue) string {
	raw := json.RawMessage(value.Value)
	switch value.Type {
	case graphValueTypeTitle, graphValueTypeRichText, graphValueTypeURL, graphValueTypeEmail, graphValueTypePhone:
		var text string
		if json.Unmarshal(raw, &text) == nil {
			return text
		}
	case graphValueTypeNumber:
		var number float64
		if json.Unmarshal(raw, &number) == nil {
			return strconv.FormatFloat(number, 'f', -1, 64)
		}
	case graphValueTypeSelect, graphValueTypeStatus, graphValueTypeMultiSelect:
		ids := []string{}
		if value.Type == graphValueTypeMultiSelect {
			_ = json.Unmarshal(raw, &ids)
		} else {
			var id string
			if json.Unmarshal(raw, &id) == nil {
				ids = append(ids, id)
			}
		}
		config := map[string]any{}
		_ = json.Unmarshal([]byte(value.Config), &config)
		names := map[string]string{}
		for _, option := range databasePropertyConversionOptions(config) {
			id, _ := option["id"].(string)
			name, _ := option["name"].(string)
			names[id] = name
		}
		chosen := []string{}
		for _, id := range ids {
			if name := names[id]; name != "" {
				chosen = append(chosen, name)
			}
		}
		return strings.Join(chosen, " ")
	}
	return ""
}

// graphMutationRowIDs names the database items a graph change writes.
func graphMutationRowIDs(operations []graphOperation) []string {
	ids := []string{}
	for _, operation := range operations {
		switch {
		case operation.Kind == graphOperationKindUpsertRow && operation.Row != nil:
			ids = append(ids, operation.Row.ID)
		case operation.Kind == graphOperationKindDeleteRow && operation.RowDeletion != nil:
			ids = append(ids, operation.RowDeletion.ID)
		}
	}
	return ids
}
