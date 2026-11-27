package app

import (
	"encoding/json"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/core"
)

// Notion's database block keeps a title of its own apart from its sources'
// names, which its MCP names it by in a page's ancestor path while its path,
// its fetch head, its screen and its API go by its source's (seen
// 2026-11-21): one made inline keeps none; one made as a full page keeps the
// name it was made with, and keeps it as it turns inline; a title typed on
// one of one source, or its source renamed through the API, renames only its
// source (seen 2026-11-21: "… F made" over "… F made typed", "… G made" over
// "… G made typed" inline, none over "… H made typed"); one made as a full
// page without a name keeps none, its title typed after naming its source
// alone (seen 2026-11-21: "" over "… Q DB" made in the screen with
// '/데이터베이스 - 전체 페이지'). Leafen keeps it on the database's block as
// blockTitle where it differs from what the database's name otherwise says
// there, "" for none where that is a full-page database's own name: a move
// brings it from the reading of the person's Notion
// (notion_migration_formats_database_title.go), as Notion's API doesn't give
// it; a full-page database turned inline keeps the name it went by as one,
// which a title typed on it inline leaves; and one of one source renamed
// keeps the name it went by until then, none for one made without a name.

const databaseBlockTitleKey = "blockTitle"

// databaseBlockTitle is the title a database's block keeps of its own, and
// whether it keeps one: kept as "", it keeps none, which names it by nothing
// over a full-page database's own name.
func databaseBlockTitle(content map[string]any) (string, bool) {
	title, kept := content[databaseBlockTitleKey].(string)
	if !kept {
		return "", false
	}
	if strings.TrimSpace(title) == "" {
		return "", true
	}
	return title, true
}

// loadDatabaseBlockContent reads a database block's content, in the trash
// alone too (storedDatabaseBlockContent).
func loadDatabaseBlockContent(app core.App, ownerID, pageID, blockID string) map[string]any {
	content := map[string]any{}
	stored, found, err := storedDatabaseBlockContent(app, ownerID, pageID, blockID)
	if err != nil || !found || json.Unmarshal([]byte(stored), &content) != nil {
		return map[string]any{}
	}
	return content
}

// keepTurnedDatabaseBlockTitle gives the database at the top of a full-page
// database's page, as a change turns that page back into a page and its
// database inline, the name it went by as its block's own title: the one
// it kept of its own (full_page_database_own_name.go) or its page's title.
// One whose block keeps a title already keeps that one, as Notion's does; a
// linked one goes by its own name (database_turn_linked_name.go).
func keepTurnedDatabaseBlockTitle(app core.App, ownerID string, page *core.Record, input graphMutationInput) (graphMutationInput, error) {
	if input.PageKind == nil || *input.PageKind != pageKindPage || page == nil || pageRecordKind(page) != pageKindDatabase {
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
		content["linkedDatabaseSource"] == true {
		return input, nil
	}
	if _, kept := databaseBlockTitle(content); kept {
		return input, nil
	}
	title := page.GetString("title")
	if own, _, kept := keptDatabaseOwnName(content, title); kept {
		title = own
	}
	title = strings.TrimSpace(withoutMentionMarks(title))
	if title == "" || utf8.RuneCountInString(title) > maxGraphTitleRunes {
		return input, nil
	}
	content[databaseBlockTitleKey] = title
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

// keepRenamedDatabaseBlockTitle keeps, as a change renames a full-page
// database of one source of its own — its title typed, its page renamed,
// its database or its source renamed through the API or MCP — the name it
// went by until then (its source's) as its block's own title, where its
// block keeps none yet: Notion's keeps the name it was made with there while
// each rename goes to its source (seen 2026-11-21: its rows' ancestor path
// "… F made" over its source "… F made typed"; "" over "… Q DB", made
// without a name and its title typed). One of several sources is renamed
// itself, as Notion's is, so its block keeps no title of its own apart from
// its page's.
func keepRenamedDatabaseBlockTitle(app core.App, ownerID string, page *core.Record, input graphMutationInput) (graphMutationInput, error) {
	if page == nil || input.PageKind != nil || !isFullPageDatabase(page) {
		return input, nil
	}
	stored, found, err := loadDatabasePageBlock(app, ownerID, page.Id)
	if err != nil || !found {
		return input, err
	}
	written, current := -1, stored.Content
	for index, operation := range input.Operations {
		if operation.Kind == graphOperationKindUpsertBlock && operation.Block != nil && operation.Block.ID == stored.ID {
			written, current = index, string(operation.Block.Content)
		}
	}
	content := map[string]any{}
	if json.Unmarshal([]byte(current), &content) != nil || content["linkedDatabaseSource"] == true {
		return input, nil
	}
	encodedBefore, _ := json.Marshal(content)
	if databaseNamingSource(encodedBefore) == "" {
		if input.PageTitle == nil || *input.PageTitle == page.GetString("title") {
			return input, nil
		}
		delete(content, databaseBlockTitleKey)
	} else {
		if _, kept := databaseBlockTitle(content); kept {
			return input, nil
		}
		source, err := fullPageDatabaseTitleSource(app, ownerID, page.Id)
		if err != nil || source == nil {
			return input, err
		}
		renamed := ""
		if input.PageTitle != nil {
			renamed = *input.PageTitle
		}
		for _, operation := range input.Operations {
			if operation.Kind == graphOperationKindUpsertDataSource && operation.DataSource != nil &&
				operation.DataSource.ID == source.ID && !operation.DataSource.TitleOmitted && input.PageTitle == nil {
				renamed = operation.DataSource.Title
			}
		}
		// Unnamed until then, it keeps none ("").
		title := strings.TrimSpace(withoutMentionMarks(source.Title))
		if renamed == "" || renamed == source.Title || utf8.RuneCountInString(title) > maxGraphTitleRunes {
			return input, nil
		}
		content[databaseBlockTitleKey] = title
	}
	encoded, err := json.Marshal(content)
	if err != nil || string(encoded) == string(encodedBefore) {
		return input, err
	}
	operations := append([]graphOperation(nil), input.Operations...)
	if written >= 0 {
		block := *operations[written].Block
		block.Content = encoded
		operations[written].Block = &block
	} else {
		if err := validateDatabaseContainerBlockShape(encoded); err != nil {
			return input, err
		}
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertBlock, Block: &graphBlock{
			ID: stored.ID, Type: graphBlockTypeInlineDatabase, Position: stored.Position, Content: encoded,
			BaseContent: json.RawMessage(stored.Content),
		}})
	}
	input.Operations = operations
	return input, nil
}
