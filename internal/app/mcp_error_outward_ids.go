package app

import (
	"regexp"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A refusal speaks in the addresses and IDs the agent has from Leafen's
// tools, never in inner ones it hasn't seen (2026-11-24, agent audit: a SQL
// query's unknown column listed the columns under "collection://57eac315-…",
// the data source's own Leafen ID, where the agent had queried it, as fetch
// names it, by "collection://214a9169-…", its public API ID — in a join it
// couldn't tell whose columns were whose; and a file attached to a row of
// another workspace was refused under the Notion-compatible ID the write had
// made for it). Every tool refusal passes here once, at its end
// (mcpToolReasons): a data source's collection:// address its own Leafen ID
// makes is said as the tools write it (mcpDataSourceURL), and a file's
// Notion-compatible ID as the file_upload_id the file tools gave.

var (
	mcpOutwardDataSourcePattern = regexp.MustCompile(`collection://([0-9A-Za-z-]{15,36})`)
	mcpOutwardUUIDPattern       = regexp.MustCompile(`\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
)

// mcpOutwardIDs is a refusal's words with the inner IDs said as the agent's.
func mcpOutwardIDs(app core.App, text string) string {
	text = mcpOutwardDataSourcePattern.ReplaceAllStringFunc(text, func(address string) string {
		id := address[len(mcpDataSourceURLPrefix):]
		publicID := ""
		err := app.DB().NewQuery(`
			SELECT [[identity]].[[public_id]] FROM {{leafen_data_sources}} [[source]]
			JOIN {{leafen_notion_object_identities}} [[identity]] ON [[identity]].[[object_kind]] = {:kind}
				AND [[identity]].[[workspace_id]] = [[source]].[[workspace_id]] AND [[identity]].[[internal_id]] = [[source]].[[id]]
			WHERE [[source]].[[id]] = {:id} LIMIT 1
		`).Bind(dbx.Params{"id": id, "kind": notionObjectIdentityDataSource}).Row(&publicID)
		if err != nil || publicID == "" {
			return address
		}
		return mcpDataSourceURLPrefix + publicID
	})
	return mcpOutwardUUIDPattern.ReplaceAllStringFunc(text, func(publicID string) string {
		fileID := ""
		if app.DB().NewQuery(`
			SELECT [[internal_id]] FROM {{leafen_notion_object_identities}}
			WHERE [[public_id]] = {:id} AND [[object_kind]] = {:kind}
		`).Bind(dbx.Params{"id": publicID, "kind": notionObjectIdentityFileUpload}).Row(&fileID) != nil || fileID == "" {
			return publicID
		}
		return fileID
	})
}
