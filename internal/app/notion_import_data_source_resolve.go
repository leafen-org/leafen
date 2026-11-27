package app

import (
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// dataSource is what a data source's pending mention names once the move is
// published (notion_import_data_source_mentions.go): the data source a Notion
// move into the workspace made of the Notion data source of that ID, this
// one's or an earlier one's (notionMovedDataSource), by its address, as
// Leafen keeps a data source's mention (data_source_mentions.go); or, for one
// no move brought, the mention of a data source not there by the Notion ID,
// as page Markdown keeps one (keepUnresolvedDataSourceMention).
func (resolver *notionImportReferenceResolver) dataSource(id string) (string, error) {
	workspaceID := ""
	if err := resolver.app.DB().NewQuery(`SELECT [[workspace_id]] FROM {{leafen_jobs}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": resolver.jobID}).Row(&workspaceID); err != nil {
		return "", err
	}
	moved, err := notionMovedDataSource(resolver.app, workspaceID, id)
	if err != nil || moved != "" {
		return notionMarkdownDataSourceAddress + moved, err
	}
	kept, _ := notionKeptUUID(id)
	return notionMarkdownDataSourceAddress + kept, nil
}

// notionImportSourceMentionWords are the words of a data source's mention
// named by its address: the source's name, "Untitled" for one not there;
// false for any other mention.
func notionImportSourceMentionWords(app core.App, attribute, nativeID string) (string, bool) {
	if attribute != "databaseId" || !strings.HasPrefix(nativeID, notionMarkdownDataSourceAddress) {
		return "", false
	}
	dataSourceID, _, _, found, err := mentionedDataSource(app, nativeID)
	if err != nil || !found {
		return notionUnresolvedMentionTitle, true
	}
	name, err := mentionedDataSourceName(app, dataSourceID)
	if err != nil {
		return notionUnresolvedMentionTitle, true
	}
	return name, true
}
