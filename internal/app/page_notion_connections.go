package app

import (
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The workspace's Notion-compatible connections in a page's "연결" menu, as
// Notion lists the connections a page has and adds one with "연결 추가"
// (2026-10-29: a connection is its own bot user with a page list again).
// A connection reaches the pages its person chose when connecting and the
// ones added here, and the pages under them down to one whose sharing is
// unlinked from its parent's, which keeps its own copy of the connections
// it reached, as Notion's bot permissions (notion_page_authorization.go,
// page_notion_connections_unlink.go); adding a page puts it in the list,
// taking it off takes it out. One a page reaches through a page above is
// taken off it by unlinking the page's sharing, as Notion's (seen
// 2026-11-21).

// pageNotionReachChain is the page and the pages above it, each once, with
// a database page's data source, as a connection's list may name any of
// them, up to the page whose sharing is its own.
func pageNotionReachChain(app core.App, page *core.Record) ([]string, error) {
	return pageNotionClimb(app, page, false)
}

// pageNotionClimb is pageNotionReachChain, or with throughUnlinked the
// whole way up as connections reached pages before an unlinked page kept
// its own (migration 837).
func pageNotionClimb(app core.App, page *core.Record, throughUnlinked bool) ([]string, error) {
	chain := []string{}
	current := page
	for seen := map[string]bool{}; !seen[current.Id]; {
		seen[current.Id] = true
		chain = append(chain, current.Id)
		if !throughUnlinked {
			if unlinked, err := pageShareUnlinked(app, current.Id); err != nil || unlinked {
				return chain, err
			}
		}
		if isDatabaseRowPageRecord(current) {
			if source := current.GetString("data_source_id"); source != "" {
				chain = append(chain, source)
			}
		}
		parentID, err := pageAncestryParentID(app, current)
		if err != nil || parentID == "" {
			return chain, err
		}
		parent, err := app.FindRecordById(pagesCollection, parentID)
		if err != nil || parent.GetString("workspace") != page.GetString("workspace") {
			return chain, nil
		}
		current = parent
	}
	return chain, nil
}

// pageNotionCapabilities are what a connection may do with a page's content
// and comments, as Notion's "연결" lists them, by the scopes it was given: an
// internal connection's inserting and updating apart, a public one's both
// with its write scope (notion_content_capabilities.go).
var pageNotionCapabilities = []struct{ name, scope, internalScope string }{
	{"read_content", "pages:read", "pages:read"},
	{"insert_content", "pages:write", notionContentInsertScope},
	{"update_content", "pages:write", notionContentUpdateScope},
	{"insert_comments", "comments:write", "comments:write"},
	{"read_comments", "comments:read", "comments:read"},
}

// withPageNotionConnectionSource gives a connection the page, or the
// database of a data source, it reaches the page through: its name and, for
// the app to call one without a name as it calls that page anywhere else
// ("새 데이터베이스" for a database, "제목 없음" for a view block's page,
// "A 및 B" for a database of several sources by their titles, pageTitle.ts)
// and to draw its icon, what it is, its icon and whether its body holds
// anything, as Notion's "연결 출처" shows the page's own icon (seen
// 2026-11-13: a lined document beside a page holding text).
func withPageNotionConnectionSource(app core.App, connection *publicMutationNotionConnection, id string) {
	connection.SourceID = id
	page := struct {
		Title      string `db:"title"`
		Kind       string `db:"kind"`
		ViewBlock  bool   `db:"view_block"`
		Icon       string `db:"icon"`
		IconType   string `db:"icon_type"`
		HasContent bool   `db:"has_content"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[page]].[[title]], [[page]].[[kind]], ` + viewBlockDatabasePageSQL + ` AS [[view_block]],
			[[page]].[[icon]], [[page]].[[icon_type]], ` + pageHasContentSQL + ` AS [[has_content]]
		FROM {{leafen_pages}} [[page]] WHERE [[page]].[[id]]={:id}
	`).Bind(dbx.Params{"id": id}).One(&page); err == nil {
		connection.SourceTitle, connection.SourceViewBlock = page.Title, page.ViewBlock
		if page.Kind == pageKindDatabase {
			connection.SourceKind = pageKindDatabase
		}
		if page.Kind == pageKindDatabase && page.Title == "" && !page.ViewBlock {
			titles, err := untitledFullPageDatabaseSourceTitles(app, []string{id})
			if err == nil {
				connection.SourceTitles = titles[id]
			}
		}
		if page.IconType != "" && page.IconType != "none" && page.Icon != "" {
			connection.SourceIcon, connection.SourceIconType = page.Icon, page.IconType
		}
		connection.SourceHasContent = page.HasContent
		return
	}
	// A data source names its database, the page its name opens.
	dataSource := struct {
		Title  string `db:"title"`
		PageID string `db:"page_id"`
	}{}
	if app.DB().NewQuery(`SELECT [[title]], [[page_id]] FROM {{leafen_data_sources}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": id}).One(&dataSource) == nil {
		connection.SourceTitle, connection.SourceKind = dataSource.Title, pageKindDatabase
		if dataSource.PageID != "" {
			connection.SourceID = dataSource.PageID
		}
	}
}

// loadPageNotionConnections lists the workspace's active connections and how
// each reaches the page: the page itself in its list, one above it, or none;
// with who made it, what it may do, and the page it reaches this one through,
// as Notion's "활성화된 연결" shows each (seen 2026-11-13).
func loadPageNotionConnections(app core.App, workspaceID string, page *core.Record) ([]publicMutationNotionConnection, error) {
	grants := []struct {
		ID         string `db:"id"`
		ClientName string `db:"client_name"`
		OwnerID    string `db:"owner_id"`
		ClientID   string `db:"client_id"`
		Scope      string `db:"scope"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[client_name]], [[owner_id]], [[client_id]], [[scope]] FROM {{leafen_notion_oauth_grants}}
		WHERE [[workspace_id]]={:workspaceId} AND [[status]]='active'
		ORDER BY [[created_at]], [[id]]
	`).Bind(dbx.Params{"workspaceId": workspaceID}).All(&grants); err != nil {
		return nil, err
	}
	connections := make([]publicMutationNotionConnection, 0, len(grants))
	if len(grants) == 0 {
		return connections, nil
	}
	chain, err := pageNotionReachChain(app, page)
	if err != nil {
		return nil, err
	}
	ids := make([]any, 0, len(chain))
	for _, id := range chain {
		ids = append(ids, id)
	}
	listed := []struct {
		GrantID    string `db:"grant_id"`
		ResourceID string `db:"resource_id"`
	}{}
	if err := app.DB().Select("grant_id", "resource_id").From(notionOAuthResourcesTable).
		Where(dbx.In("resource_id", ids...)).All(&listed); err != nil {
		return nil, err
	}
	reach := map[string]string{}
	// The nearest listed page up the chain is where a connection comes from.
	place := map[string]int{}
	for index, id := range chain {
		place[id] = index
	}
	source := map[string]string{}
	for _, row := range listed {
		if row.ResourceID == page.Id {
			reach[row.GrantID] = "page"
		} else if reach[row.GrantID] == "" {
			reach[row.GrantID] = "parent"
		}
		if current, found := source[row.GrantID]; !found || place[row.ResourceID] < place[current] {
			source[row.GrantID] = row.ResourceID
		}
	}
	for _, grant := range grants {
		connection := publicMutationNotionConnection{GrantID: grant.ID, Name: grant.ClientName, Reach: reach[grant.ID],
			CreatorName: userDisplayName(app, grant.OwnerID)}
		if connection.Reach == "" {
			connection.Reach = "none"
		}
		if id := source[grant.ID]; id != "" {
			withPageNotionConnectionSource(app, &connection, id)
		}
		scopes := map[string]bool{}
		for _, scope := range strings.Fields(grant.Scope) {
			scopes[scope] = true
		}
		internal := strings.HasPrefix(grant.ClientID, internalConnectionClientPrefix)
		for _, capability := range pageNotionCapabilities {
			if internal && scopes[capability.internalScope] || !internal && scopes[capability.scope] {
				connection.Capabilities = append(connection.Capabilities, capability.name)
			}
		}
		connections = append(connections, connection)
	}
	return connections, nil
}

// changePageNotionConnection puts the page in an active connection of its
// workspace, or takes it out of one that lists it or reaches it from above
// (disconnectInheritedPageNotionConnection).
func changePageNotionConnection(app core.App, authorization pageAuthorization, grantID string, connect bool) error {
	var active bool
	if err := app.DB().NewQuery(`
		SELECT EXISTS (SELECT 1 FROM {{leafen_notion_oauth_grants}}
			WHERE [[id]]={:grantId} AND [[workspace_id]]={:workspaceId} AND [[status]]='active')
	`).Bind(dbx.Params{"grantId": grantID, "workspaceId": authorization.Workspace.WorkspaceID}).Row(&active); err != nil {
		return err
	}
	if !active {
		return publicMutationRefused("grantId names no active Notion-compatible connection of this workspace")
	}
	params := dbx.Params{"grantId": grantID, "pageId": authorization.Page.Id}
	if connect {
		_, err := app.DB().NewQuery(`
			INSERT OR IGNORE INTO {{leafen_notion_oauth_grant_resources}} ([[grant_id]], [[resource_id]], [[resource_kind]])
			VALUES ({:grantId}, {:pageId}, 'page')
		`).Bind(params).Execute()
		return err
	}
	result, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_notion_oauth_grant_resources}} WHERE [[grant_id]]={:grantId} AND [[resource_id]]={:pageId}
	`).Bind(params).Execute()
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return disconnectInheritedPageNotionConnection(app, authorization, grantID)
	}
	return nil
}
