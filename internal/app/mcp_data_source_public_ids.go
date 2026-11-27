package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The MCP tools name a data source by the ID the public API gives it, as
// Notion's MCP names one by its API's ID, so its collection:// address and a
// link to it (/p/<ID>, notionMarkdownAppAddress) name it alike: fetch's
// <data-source> and a database's data-source-url, views', relations', the
// catalog's, the version tools' and a SQL query's data_source_ids. Before
// 2026-11-22 they wrote its own Leafen ID, which a link never named; that ID
// still names it wherever the tools read one (mcpLocalID, the SQL plan's
// sourceNames, matchNotionMarkdownReference).

// mcpDataSourceURL is a data source's collection:// address as the MCP
// tools write it.
func mcpDataSourceURL(app core.App, dataSourceID string) string {
	return mcpDataSourceURLPrefix + mcpDataSourcePublicID(app, dataSourceID)
}

// mcpDataSourcePublicID is a data source's public API ID, its own ID for one
// no longer there. Inside a read snapshot or a transaction, app is that
// one's, as ensureNotionObjectIdentity takes it.
func mcpDataSourcePublicID(app core.App, dataSourceID string) string {
	workspaceID := ""
	if err := app.DB().NewQuery(`SELECT [[workspace_id]] FROM {{leafen_data_sources}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": dataSourceID}).Row(&workspaceID); err != nil || workspaceID == "" {
		return dataSourceID
	}
	publicID, err := ensureNotionObjectIdentity(app, notionObjectIdentityDataSource, workspaceID, dataSourceID)
	if err != nil {
		return dataSourceID
	}
	return publicID
}

// mcpQueryPublicDataSourceIDs is a SQL query's answer naming the data
// sources it read by their public API IDs.
func mcpQueryPublicDataSourceIDs(app core.App, output notionMCPQueryDataSourcesOutput) notionMCPQueryDataSourcesOutput {
	ids := make([]string, 0, len(output.DataSourceIDs))
	for _, id := range output.DataSourceIDs {
		ids = append(ids, mcpDataSourcePublicID(app, id))
	}
	output.DataSourceIDs = ids
	return output
}

// mcpPropertyNamingPublicSource is a property as the MCP tools describe it,
// a relation's data source named by its public API ID
// (mcpSQLiteRelationNote).
func mcpPropertyNamingPublicSource(app core.App, property notionMCPDataSourceProperty) notionMCPDataSourceProperty {
	target, _ := property.Config["relationDatabaseId"].(string)
	if property.Type != graphValueTypeRelation || target == "" {
		return property
	}
	config := make(map[string]any, len(property.Config))
	for key, value := range property.Config {
		config[key] = value
	}
	config["relationDatabaseId"] = mcpDataSourcePublicID(app, target)
	property.Config = config
	return property
}
