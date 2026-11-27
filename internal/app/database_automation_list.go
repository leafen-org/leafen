package app

import (
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func listDatabaseAutomationDefinitions(
	app core.App, actorID, pageID, databaseID, after string,
) (databaseAutomationListResponse, error) {
	authorization, err := authorizeDatabaseAutomationSource(app, actorID, pageID, permissionWorkspaceRead)
	if err != nil {
		return databaseAutomationListResponse{}, err
	}
	if err := validateDatabaseAutomationSource(
		app, authorization, databaseID, databaseAutomationScope{Type: databaseAutomationScopeDatabase},
	); err != nil {
		return databaseAutomationListResponse{}, err
	}
	after = strings.TrimSpace(after)
	rows := []storedDatabaseAutomationDefinition{}
	err = app.DB().NewQuery(`
		SELECT [[id]], [[owner]], [[workspace_id]], [[page_id]], [[database_id]], [[name]],
			[[scope_type]], [[view_id]], [[trigger_type]], [[trigger_json]], [[action_json]], [[status]], [[revision]],
			[[created_frontier]], [[updated_frontier]], [[next_run_at]], [[paused_reason]],
			[[created_by]], [[updated_by]], [[created]], [[updated]]
		FROM {{leafen_database_automation_definitions}}
			INDEXED BY [[idx_leafen_database_automation_definitions_live_id]]
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[database_id]] = {:databaseId}
			AND [[status]] <> 'deleted' AND ({:after} = '' OR [[id]] > {:after})
		ORDER BY [[id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"owner": authorization.Workspace.OwnerID, "pageId": pageID, "databaseId": databaseID,
		"after": after, "limit": databaseAutomationListPage + 1,
	}).All(&rows)
	if err != nil {
		return databaseAutomationListResponse{}, err
	}
	response := databaseAutomationListResponse{Items: []databaseAutomationDefinitionResponse{}}
	if len(rows) > databaseAutomationListPage {
		response.NextCursor = rows[databaseAutomationListPage-1].ID
		rows = rows[:databaseAutomationListPage]
	}
	for _, row := range rows {
		definition, err := renderDatabaseAutomationDefinition(row)
		if err != nil {
			return databaseAutomationListResponse{}, err
		}
		response.Items = append(response.Items, definition)
	}
	return response, nil
}

// markAutomatedDatabases tells a read how many automations each of its data
// sources has, paused and turned off ones too: the toolbar's ⚡ is blue while
// there is one, as Notion's (seen 2026-11-08).
func markAutomatedDatabases(app core.App, owner string, items []graphReadItem) error {
	for _, item := range items {
		if item.Kind != graphReadKindDataSource {
			continue
		}
		source, _ := item.Value["id"].(string)
		pageID, _ := item.Value["pageId"].(string)
		if source == "" || pageID == "" {
			continue
		}
		count := 0
		if err := app.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_database_automation_definitions}}
				INDEXED BY [[idx_leafen_database_automation_definitions_live_id]]
			WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[database_id]] = {:databaseId}
				AND [[status]] <> 'deleted'
		`).Bind(dbx.Params{"owner": owner, "pageId": pageID, "databaseId": source}).Row(&count); err != nil {
			return err
		}
		if count > 0 {
			item.Value["automationCount"] = count
		}
	}
	return nil
}
