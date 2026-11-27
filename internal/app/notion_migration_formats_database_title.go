package app

import (
	"encoding/json"
	"reflect"
	"strings"
	"unicode/utf8"
)

// A database's block keeps a title of its own in Notion, apart from its
// sources' names, which Notion's MCP names it by in a page's ancestor path
// and its API doesn't give (database_block_title.go). The reading of the
// person's Notion brings it with the database's page layout, and the run
// gives it to the block the move made of the database's (blockTitle) where
// it differs from what the database's name otherwise says there: an inline
// one's with any, a full-page one's apart from its page's title, and a
// full-page one's none as none ("" — one made in Notion's screen without a
// name keeps none, its title typed naming its source alone; seen
// 2026-11-21), which its page's name doesn't fill. A reading that says none
// of an inline one takes away one an earlier reading gave. A database whose
// block the run can't write, or no reading read, keeps its layout's match
// unmatched, so the list of what is left names it
// (notion_migration_formats_left.go).

// databaseTitle gives the block the move made of a database its own title
// as the reading read it; true when it has it, or the reading said nothing
// of it (a reader before it).
func (matcher *notionMigrationFormatsMatcher) databaseTitle(notionID string, read *string) (bool, error) {
	if read == nil {
		return true, nil
	}
	blockID, err := matcher.mapped(notionID+":block", "block")
	if err != nil || blockID == "" {
		return false, err
	}
	stored, found, err := loadStoredNotionBlock(matcher.app, blockID)
	if err != nil || !found || stored.Type != graphBlockTypeInlineDatabase {
		return false, err
	}
	content, before := map[string]any{}, map[string]any{}
	if json.Unmarshal([]byte(stored.Content), &content) != nil || json.Unmarshal([]byte(stored.Content), &before) != nil ||
		content["linkedDatabaseSource"] == true {
		return false, nil
	}
	// One too long for a title here isn't kept.
	title, overlong := *read, utf8.RuneCountInString(*read) > maxGraphTitleRunes
	if strings.TrimSpace(title) == "" || overlong {
		title = ""
	}
	// A full-page database's page says its name there already, unless its
	// block keeps none.
	fullPage, pageTitle := false, ""
	if top, found, err := loadDatabasePageBlock(matcher.app, stored.Owner, stored.PageID); err != nil {
		return false, err
	} else if found && top.ID == stored.ID {
		if page, err := matcher.app.FindRecordById(pagesCollection, stored.PageID); err == nil && isFullPageDatabase(page) {
			fullPage, pageTitle = true, page.GetString("title")
		}
	}
	switch {
	case fullPage && title == "" && !overlong:
		content[databaseBlockTitleKey] = ""
	case title == "" || fullPage && title == pageTitle:
		delete(content, databaseBlockTitleKey)
	default:
		content[databaseBlockTitleKey] = title
	}
	if reflect.DeepEqual(content, before) {
		return true, nil
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return false, err
	}
	block := &graphBlock{
		ID: stored.ID, Type: graphBlockTypeInlineDatabase, Position: stored.Position, Content: encoded,
		BaseContent: json.RawMessage(stored.Content),
	}
	if stored.ParentID != "" {
		parentID := stored.ParentID
		block.ParentID = &parentID
	}
	if err := matcher.apply(stored.PageID, []graphOperation{{Kind: graphOperationKindUpsertBlock, Block: block}}); err != nil {
		return false, ignoreNotionMigrationFormatConflict(err)
	}
	return true, nil
}
