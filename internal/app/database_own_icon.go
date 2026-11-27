package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database of two or more data sources of its own has an icon of its own,
// apart from its sources', as Notion's does (seen 2026-11-22 through the
// public API and in Chrome at 1440: a database's icon set leaves its sources'
// as they are, which its views' source headings, the sources' list and their
// mentions show, the cylinder for one without; one of one source takes the
// icon with its source, and keeps it when a second source comes). A full-page
// one's is its page's. An inline one's is its block's (databaseOwnIconKey);
// until one is given there it goes by its first source's, as it did before
// it had its own.

const (
	databaseOwnIconKey     = "databaseIcon"
	databaseOwnIconTypeKey = "databaseIconType"
)

// inlineDatabaseOwnIcon is the icon an inline database's block keeps of its
// own, "none" for one taken away, and whether it keeps one.
func inlineDatabaseOwnIcon(content []byte) (string, string, bool) {
	raw := map[string]json.RawMessage{}
	iconType := ""
	if json.Unmarshal(content, &raw) != nil || json.Unmarshal(raw[databaseOwnIconTypeKey], &iconType) != nil || iconType == "" {
		return "", "", false
	}
	icon := ""
	_ = json.Unmarshal(raw[databaseOwnIconKey], &icon)
	if iconType == graphDataSourceIconNone {
		icon = ""
	}
	return icon, iconType, true
}

// databaseOwnSourceCount is how many data sources of its own a database has.
func databaseOwnSourceCount(app core.App, containerID string) (int, error) {
	own := 0
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_database_container_sources}}
		WHERE [[container_id]] = {:id} AND [[source_container_id]] = ''
	`).Bind(dbx.Params{"id": containerID}).Row(&own)
	return own, err
}

// databaseOfSeveralIcon is the icon a database of two or more sources of its
// own shows: a full-page one's page's (pageIcon), an inline one's its block
// keeps, else, with none given there, its first source's (firstIcon).
func databaseOfSeveralIcon(fullPage bool, pageIcon, pageIconType string, blockContent []byte, firstIcon, firstIconType string) (string, string) {
	if fullPage {
		if pageIcon == "" || pageIconType == "" {
			return "", graphDataSourceIconNone
		}
		return pageIcon, pageIconType
	}
	if icon, iconType, kept := inlineDatabaseOwnIcon(blockContent); kept {
		return icon, iconType
	}
	return firstIcon, firstIconType
}

// setInlineDatabaseOwnIcon writes an inline database's own icon on its
// block's content, "none" taking it away.
func setInlineDatabaseOwnIcon(content map[string]any, icon, iconType string) {
	if iconType == "" || iconType == graphDataSourceIconNone || icon == "" {
		icon, iconType = "", graphDataSourceIconNone
	}
	content[databaseOwnIconKey], content[databaseOwnIconTypeKey] = icon, iconType
}

// setNotionInlineDatabaseOwnIcon gives an inline database of several sources
// the icon a Public API connection's database update names, on its block, as
// the editor writes it (updateNotionDatabaseBlockContent).
func setNotionInlineDatabaseOwnIcon(
	event *core.RequestEvent, ygo *pocketBaseYgoPersistence, identity notionOAuthBearerIdentity,
	container notionDatabaseContainer, icon json.RawMessage,
) error {
	presentation, err := notionPagePresentation(icon, nil)
	if err != nil {
		return notionDatabaseRowPageRequestInvalid("%s", err.Error())
	}
	if err := validateNotionPagePresentationReferences(event.App, identity.PrincipalID, identity.WorkspaceID, &presentation); err != nil {
		return err
	}
	value, kind := "", graphDataSourceIconNone
	if presentation.Icon != nil && presentation.IconType != nil {
		value, kind = *presentation.Icon, *presentation.IconType
	}
	return updateNotionDatabaseBlockContent(event, ygo, identity, container, "notion-public-database-icon:", func(content map[string]any) {
		setInlineDatabaseOwnIcon(content, value, kind)
	})
}

// databaseOfSeveralIconOf is databaseOfSeveralIcon for a database the public
// API reads, of the page holding it, an inline one's block read here.
func databaseOfSeveralIconOf(app core.App, container notionDatabaseContainer, page *core.Record, firstIcon, firstIconType string) (string, string) {
	if container.fullPage() {
		return databaseOfSeveralIcon(true, page.GetString("icon"), page.GetString("icon_type"), nil, firstIcon, firstIconType)
	}
	return databaseOfSeveralIcon(false, "", "", databaseBlockContentOf(app, container), firstIcon, firstIconType)
}

// databaseBlockContentOf is the content of a database's block
// (notionDatabaseBlockContent), none where it can't be read.
func databaseBlockContentOf(app core.App, container notionDatabaseContainer) []byte {
	content, _ := notionDatabaseBlockContent(app, container)
	return []byte(content)
}

// turnDatabaseOwnIcon carries the icon of a database of several sources of
// its own as it turns between a page of its own and a block of a page, as a
// linked database's goes (database_turn_linked_name.go): inline it is its
// block's, on a page of its own its page's. The page it becomes takes the
// block's when it has none, the block keeping none; brought back inline the
// block takes the page's.
func turnDatabaseOwnIcon(app core.App, ownerID string, page *core.Record, input graphMutationInput) (graphMutationInput, error) {
	if input.PageKind == nil || page == nil || pageRecordKind(page) == *input.PageKind {
		return input, nil
	}
	stored, found, err := loadDatabasePageBlock(app, ownerID, page.Id)
	if err != nil || !found {
		return input, err
	}
	// A change that writes the block already (its name kept, keepTurnedDatabaseBlockTitle) carries the icon too.
	written := -1
	for index, operation := range input.Operations {
		if operation.Kind == graphOperationKindUpsertBlock && operation.Block != nil && operation.Block.ID == stored.ID {
			written = index
		}
	}
	base := []byte(stored.Content)
	if written >= 0 {
		base = input.Operations[written].Block.Content
	}
	content := map[string]any{}
	members, err := parseDatabaseContainerBlockContent(json.RawMessage(base))
	if err != nil || json.Unmarshal(base, &content) != nil || content["linkedDatabaseSource"] == true ||
		len(members.dataSourceIDs()) < 2 {
		return input, nil
	}
	if *input.PageKind == pageKindPage {
		icon, iconType, ok := linkedDatabaseIcon(page.GetString("icon"), page.GetString("icon_type"))
		if !ok {
			return input, nil
		}
		setInlineDatabaseOwnIcon(content, icon, iconType)
	} else {
		icon, iconType, kept := inlineDatabaseOwnIcon(base)
		if !kept {
			return input, nil
		}
		delete(content, databaseOwnIconKey)
		delete(content, databaseOwnIconTypeKey)
		if icon != "" && page.GetString("icon") == "" {
			page.Set("icon", icon)
			page.Set("icon_type", iconType)
		}
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return input, err
	}
	operations := append([]graphOperation(nil), input.Operations...)
	if written >= 0 {
		block := *operations[written].Block
		block.Content = encoded
		operations[written].Block = &block
	} else {
		block := graphBlock{ID: stored.ID, Type: graphBlockTypeInlineDatabase, Position: stored.Position, Content: encoded,
			BaseContent: json.RawMessage(stored.Content)}
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertBlock, Block: &block})
	}
	input.Operations = operations
	return input, nil
}
