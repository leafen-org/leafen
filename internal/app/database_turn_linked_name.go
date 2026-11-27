package app

import (
	"encoding/json"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/core"
)

// A linked database's own name, its formatting and its icon go with it as
// it turns between a page of its own and a block of a page, as Notion's
// (seen 2026-10-15). Inline they are its block's (linkedTitle,
// linkedTitleSpans, linkedIcon); on a page of its own they are that page's
// title, the name's formatting kept for it (pageTitleSpans,
// database_page_name_spans.go), and icon, its block keeping none. The page
// it becomes is named after it and takes its icon, while a page with an icon
// keeps its own; one without a name of its own gives its page the name the
// page was made with as a made one (generatedPageTitle), which leaves the
// large title empty. Brought back inline it is named after its page, with
// the page's formatting and icon, unless the page had only that made name.
// The page-kind change does this, which the editor's "페이지로 전환"·"인라인으로
// 전환" and the API's is_inline both make, so they name it alike.

// turnLinkedDatabaseName names the linked database at the top of a page, or
// the page, as a change turns that page into a full-page database or back.
// A change that writes the database itself names it as it says.
func turnLinkedDatabaseName(app core.App, ownerID string, page *core.Record, input graphMutationInput) (graphMutationInput, error) {
	if input.PageKind == nil || page == nil || pageRecordKind(page) == *input.PageKind {
		return input, nil
	}
	stored, found, err := loadDatabasePageBlock(app, ownerID, page.Id)
	if err != nil || !found {
		return input, err
	}
	for _, operation := range input.Operations {
		if operation.Kind == graphOperationKindUpsertBlock && operation.Block != nil && operation.Block.ID == stored.ID {
			return input, nil
		}
	}
	content, before := map[string]any{}, map[string]any{}
	if json.Unmarshal([]byte(stored.Content), &content) != nil || json.Unmarshal([]byte(stored.Content), &before) != nil ||
		content["linkedDatabaseSource"] != true {
		return input, nil
	}
	title := page.GetString("title")
	if input.PageTitle != nil {
		title = *input.PageTitle
	}
	if *input.PageKind == pageKindPage {
		nameLinkedDatabaseAfterPage(content, []byte(stored.Content), title, page)
	} else if named := namePageAfterLinkedDatabase(content, []byte(stored.Content), title, page); named != title {
		input.PageTitle = &named
	}
	if reflect.DeepEqual(content, before) {
		return input, nil
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return input, err
	}
	if err := validateDatabaseContainerBlockShape(encoded); err != nil {
		return input, err
	}
	block := graphBlock{ID: stored.ID, Type: graphBlockTypeInlineDatabase, Position: stored.Position, Content: encoded,
		BaseContent: json.RawMessage(stored.Content)}
	input.Operations = append(append([]graphOperation(nil), input.Operations...),
		graphOperation{Kind: graphOperationKindUpsertBlock, Block: &block})
	return input, nil
}

// nameLinkedDatabaseAfterPage gives a linked database its full page's
// title, that title's formatting while it spells it, and the page's icon as
// its own, the page's name for it going.
func nameLinkedDatabaseAfterPage(content map[string]any, stored []byte, title string, page *core.Record) {
	generated, _ := content["generatedPageTitle"].(string)
	spans := databasePageNameSpans(stored, title)
	for _, key := range []string{"generatedPageTitle", "pageTitleSpans", "linkedTitle", "linkedTitleSpans", "linkedIcon", "linkedIconType"} {
		delete(content, key)
	}
	if named := strings.TrimSpace(title); named != "" && named != generated && utf8.RuneCountInString(named) <= maxGraphTitleRunes {
		content["linkedTitle"] = named
		encoded, _ := json.Marshal(named)
		if formatted, err := normalizeGraphNameSpans(encoded, spans); err == nil && formatted != nil {
			content["linkedTitleSpans"] = formatted
		}
	}
	if icon, iconType, ok := linkedDatabaseIcon(page.GetString("icon"), page.GetString("icon_type")); ok {
		content["linkedIcon"], content["linkedIconType"] = icon, iconType
	}
}

// namePageAfterLinkedDatabase is the title a page takes as a linked database
// makes it a full-page database: the database's own name, with its
// formatting kept for the page, or the title the page has, kept as a made
// one. A page without an icon takes the database's, saved with its new kind.
// The block's own name and icon go, being the page's now.
func namePageAfterLinkedDatabase(content map[string]any, stored []byte, title string, page *core.Record) string {
	own, _ := content["linkedTitle"].(string)
	own = strings.TrimSpace(own)
	linkedIcon, _ := content["linkedIcon"].(string)
	linkedIconType, _ := content["linkedIconType"].(string)
	for _, key := range []string{"pageTitleSpans", "linkedTitle", "linkedTitleSpans", "linkedIcon", "linkedIconType"} {
		delete(content, key)
	}
	if own != "" {
		delete(content, "generatedPageTitle")
		if spans := blockPageNameSpans(stored, "linkedTitleSpans"); len(spans) > 0 && spelledName(spans) == own {
			content["pageTitleSpans"] = spans
		}
		title = own
	} else if generated, _ := content["generatedPageTitle"].(string); generated == "" && strings.TrimSpace(title) != "" {
		content["generatedPageTitle"] = title
	}
	if current := page.GetString("icon_type"); (current == "" || current == graphDataSourceIconNone) && page.GetString("icon") == "" {
		if icon, iconType, ok := linkedDatabaseIcon(linkedIcon, linkedIconType); ok {
			page.Set("icon", icon)
			page.Set("icon_type", iconType)
		}
	}
	return title
}

// linkedDatabaseIcon is an icon a linked database can have as its own
// (validateLinkedDatabaseName); none for no icon or one it can't have.
func linkedDatabaseIcon(icon, iconType string) (string, string, bool) {
	if icon == "" || iconType == "" || iconType == graphDataSourceIconNone {
		return "", "", false
	}
	shape := graphDataSource{Icon: &icon, IconType: &iconType}
	if normalizeGraphDataSourceIcon(&shape) != nil || *shape.Icon != icon || *shape.IconType != iconType {
		return "", "", false
	}
	return icon, iconType, true
}
