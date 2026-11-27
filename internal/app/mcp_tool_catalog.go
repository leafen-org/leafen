package app

import (
	"context"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// leafen_catalog gives a workspace's databases at a glance (2026-11-16: three
// agents analysing a moved workspace each first fetched every data source to
// learn its ID and columns, four calls more even after they were handed the
// IDs): each data source the connection may read, by name, with its
// database's address, its collection:// URL — the table name SQL takes —, a
// view whose query gives computed values, and each column with its type and
// the SQLite columns notion-query-data-sources names it by. Notion's MCP has
// no such tool; the names and columns are the ones notion-fetch gives. Each
// carries its path, as Notion's search gives every result one — the pages
// above it, so two of one name are told apart without a fetch (2026-11-22:
// four names held two databases each, with nothing to tell which).

const mcpCatalogPage = 50

var mcpCatalogLike = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

type leafenMCPCatalogInput struct {
	WorkspaceID string `json:"workspace_id,omitempty" jsonschema:"Workspace to list, when the connection reaches more than one"`
	Query       string `json:"query,omitempty" jsonschema:"Words in a database's name, to list only those; without them every database"`
	NamesOnly   bool   `json:"names_only,omitzero" jsonschema:"Leave out the columns: just each database's name and addresses"`
	Cursor      string `json:"start_cursor,omitempty" jsonschema:"next_cursor from the last call, for the next databases"`
}

type leafenMCPCatalogColumn struct {
	Name string `json:"name"`
	// The type as Notion's MCP names it (mcpNotionPropertyType).
	Type string `json:"type"`
	// The SQLite columns notion-query-data-sources reads it by; none for a
	// formula or a rollup.
	SQL []string `json:"sql,omitempty"`
	// Where its values come from when SQL has none, or what a relation
	// relates to.
	Note string `json:"note,omitempty"`
}

type leafenMCPCatalogDatabase struct {
	Name string `json:"name"`
	// The pages and databases above it from the top down, joined by " / ",
	// as notion-search's path; none at the workspace's top.
	Path string `json:"path,omitempty"`
	// The database's address, for notion-fetch.
	URL string `json:"url"`
	// The data source's collection:// URL: data_source_url, and the table
	// name SQL takes in quotes.
	DataSource string `json:"data_source"`
	// A view of it, whose query (notion-query-data-sources mode view) gives
	// formula and rollup values.
	View    string                   `json:"view,omitempty"`
	Columns []leafenMCPCatalogColumn `json:"columns,omitempty"`
}

type leafenMCPCatalogOutput struct {
	WorkspaceID string                     `json:"workspace_id"`
	Databases   []leafenMCPCatalogDatabase `json:"databases"`
	NextCursor  string                     `json:"next_cursor,omitempty"`
	// Note says the workspace listed is the default of several, not all
	// the connection reaches: one not listed here isn't missing (2026-11-23,
	// as leafen_search's, mcp_tool_search_scope.go).
	Note string `json:"note,omitempty"`
}

func registerMCPCatalogTool(server *mcp.Server, app core.App, readOnly *mcp.ToolAnnotations) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "leafen_catalog", Title: "List a workspace's databases and columns",
		Description: "Lists a workspace's databases in one short answer, " + strconv.Itoa(mcpCatalogPage) + " at a time: each data source's name, its database's url (for notion-fetch), " +
			"its collection:// URL (data_source_url, and the table name SQL takes in double quotes) and a view:// URL, and each column with its type and the SQLite columns " +
			"notion-query-data-sources names it by (a date has :start, :end and :is_datetime). A formula or rollup has no SQL column: its values come from that view's query (mode view). " +
			"Each has its path (the pages above it, as notion-search's path), which tells two of one name apart. " +
			"With query, only databases whose name holds those words; names_only leaves the columns out. " +
			"A database in the trash, or on a page in it, isn't listed, as notion-search leaves it out. " +
			"It lists databases, not tools: leafen_operations finds a tool your list leaves out.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input leafenMCPCatalogInput) (*mcp.CallToolResult, leafenMCPCatalogOutput, error) {
		authority, err := mcpAuthorityFromContext(ctx)
		if err != nil {
			return nil, leafenMCPCatalogOutput{}, err
		}
		workspace, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{
			WorkspaceID: input.WorkspaceID, AllowImplicit: true,
		}, permissionWorkspaceRead)
		if err != nil {
			return nil, leafenMCPCatalogOutput{}, err
		}
		offset := 0
		if raw := strings.TrimSpace(input.Cursor); raw != "" {
			if offset, err = strconv.Atoi(raw); err != nil || offset < 0 {
				return nil, leafenMCPCatalogOutput{}, mcpInvalid("start_cursor is the next_cursor of the last call, or none")
			}
		}
		output, err := readMCPCatalog(app, authority, workspace.WorkspaceID, strings.TrimSpace(input.Query), input.NamesOnly, offset)
		if err != nil {
			return nil, leafenMCPCatalogOutput{}, failedBecause(errMCPToolFailed, err)
		}
		if strings.TrimSpace(input.WorkspaceID) == "" && len(authority.WorkspaceIDs) > 1 {
			output.Note = "This is the default of the " + strconv.Itoa(len(authority.WorkspaceIDs)) +
				" workspaces this connection reaches (leafen_list_workspaces): name workspace_id to list another."
		}
		return nil, output, nil
	})
}

// readMCPCatalog reads a window of a workspace's data sources the
// connection may read, by name; a source it may not read is left out, and
// one in the trash (mcp_database_trash_rule.go).
func readMCPCatalog(
	app core.App, authority mcpAuthority, workspaceID, query string, namesOnly bool, offset int,
) (leafenMCPCatalogOutput, error) {
	output := leafenMCPCatalogOutput{WorkspaceID: workspaceID, Databases: []leafenMCPCatalogDatabase{}}
	locations := []mcpDataSourceLocation{}
	where, params := "", dbx.Params{"workspaceId": workspaceID, "limit": mcpCatalogPage + 1, "offset": offset}
	for index, word := range strings.Fields(query) {
		key := "word" + strconv.Itoa(index)
		where += ` AND [[source]].[[title]] LIKE {:` + key + `} ESCAPE '\'`
		params[key] = "%" + mcpCatalogLike.Replace(word) + "%"
	}
	err := app.DB().NewQuery(`
		SELECT [[source]].[[id]], [[source]].[[owner]], [[source]].[[workspace_id]],
			[[source]].[[page_id]], [[source]].[[title]]
		FROM {{leafen_data_sources}} AS [[source]]
		WHERE [[source]].[[workspace_id]] = {:workspaceId} AND ` + databaseSourceNotTrashedSQL("[[source]].[[id]]") + where + `
		ORDER BY [[source]].[[title]], [[source]].[[id]] LIMIT {:limit} OFFSET {:offset}
	`).Bind(params).All(&locations)
	if err != nil {
		return output, err
	}
	if len(locations) > mcpCatalogPage {
		locations, output.NextCursor = locations[:mcpCatalogPage], strconv.Itoa(offset+mcpCatalogPage)
	}
	for _, location := range locations {
		page, err := authorizePage(app, authority.PrincipalID, location.PageID, permissionWorkspaceRead)
		if err != nil || page.Workspace.WorkspaceID != location.WorkspaceID || !mcpAuthorityReachesPage(app, authority, page.Page) {
			continue
		}
		if trashed, err := mcpDataSourceInTrash(app, location); err != nil {
			return output, err
		} else if trashed {
			continue
		}
		properties, views, err := loadMCPDataSourceSchema(app, location, authority.PrincipalID)
		if err != nil {
			return output, err
		}
		// An untitled one is called what fetch heads it (mcp_untitled_names.go).
		database := leafenMCPCatalogDatabase{
			Name:       mcpNamedOr(mcpSourceNameAsRead(app, authority, location.ID, location.Title), mcpDataSourceShownTitle(app, authority.PrincipalID, location)),
			URL:        mcpDataSourceURL(app, location.ID),
			DataSource: mcpDataSourceURL(app, location.ID),
		}
		if containerID, err := loadDatabaseSQLSourceContainerID(app, location); err == nil {
			database.URL = mcpDatabaseAddress(authority, containerID)
			if ancestors, err := mcpDatabaseAncestors(app, authority, location.PageID, containerID); err == nil {
				if path := mcpAncestorPath(app, ancestors); path != nil {
					database.Path = *path
				}
			}
		}
		if len(views) > 0 {
			database.View = mcpViewURLPrefix + views[0].ID
		}
		if !namesOnly {
			database.Columns = mcpCatalogColumns(app, properties)
		}
		output.Databases = append(output.Databases, database)
	}
	return output, nil
}

func mcpCatalogColumns(app core.App, properties []notionMCPDataSourceProperty) []leafenMCPCatalogColumn {
	columns := make([]leafenMCPCatalogColumn, 0, len(properties))
	for _, property := range properties {
		column := leafenMCPCatalogColumn{Name: property.Name, Type: mcpNotionPropertyType(property.Type)}
		for _, sqlColumn := range mcpSQLiteColumns(property) {
			column.SQL = append(column.SQL, sqlColumn.Name)
		}
		switch property.Type {
		case graphValueTypeFormula, graphValueTypeRollup:
			column.Note = "computed: no SQL column; the view's query gives its values"
		case graphValueTypeRelation:
			if target, _ := property.Config["relationDatabaseId"].(string); target != "" {
				column.Note = "relates to " + mcpDataSourceURL(app, target)
			}
		}
		columns = append(columns, column)
	}
	return columns
}
