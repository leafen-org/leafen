package app

import "github.com/pocketbase/pocketbase/core"

// workspaceExportItemDatabaseAutomation is an archive's database automation.
const workspaceExportItemDatabaseAutomation = "databaseAutomation"

// A database's automations go with the database into an archive, as its
// templates do: the view they watch, their trigger and their steps. One
// that was deleted is gone and stays out. Its steps may act on a database,
// page or person outside the archive; those are carried as they are, as a
// button's are (workspace_import_archive_references.go).
const workspaceExportAutomationItemSQL = `json_object(
	'id', [[items]].[[id]], 'pageId', [[items]].[[page_id]], 'dataSourceId', [[items]].[[database_id]],
	'name', [[items]].[[name]], 'scopeType', [[items]].[[scope_type]], 'viewId', [[items]].[[view_id]],
	'triggerType', [[items]].[[trigger_type]], 'trigger', json([[items]].[[trigger_json]]),
	'actionDocument', json([[items]].[[action_json]]), 'status', [[items]].[[status]],
	'pausedReason', [[items]].[[paused_reason]],
	'createdAt', [[items]].[[created]], 'updatedAt', [[items]].[[updated]])`

// workspaceExportAutomationSourceSQL holds for an automation whose data
// source stands on its page, which the archive carries with it.
const workspaceExportAutomationSourceSQL = `EXISTS (SELECT 1 FROM {{leafen_data_sources}} AS [[source]]
	WHERE [[source]].[[id]] = [[items]].[[database_id]] AND [[source]].[[page_id]] = [[items]].[[page_id]])`

// loadWorkspaceExportAutomationWindow reads the automations of the
// databases on the pages a workspace's archive takes, and People's: its
// database's own, which the archive carries by People's names
// (peopleArchiveAutomation) and an archive restoring the workspace brings
// back on its People (workspace_import_archive_automations.go). A restricted
// member, whom People isn't open to, takes none of People's.
func loadWorkspaceExportAutomationWindow(
	app core.App, authorization workspaceAuthorization, filter workspaceExportPageFilter, cursor string, limit int,
) ([]storedWorkspaceExportInventoryItem, error) {
	// A readable document shows no automations.
	if filter.SharedRows {
		return nil, nil
	}
	after, err := workspaceExportStageCursor(cursor, "16", 2)
	if err != nil {
		return nil, err
	}
	params := exportStageParams(authorization, filter)
	params["afterPage"], params["afterId"] = after[0], after[1]
	people, hasPeople, err := loadPeopleDatabase(app, authorization.WorkspaceID)
	if err != nil {
		return nil, err
	}
	params["peoplePage"], params["peopleSource"] = "", ""
	if hasPeople && !authorization.Restricted {
		params["peoplePage"], params["peopleSource"] = people.PageID, people.DataSourceID
	}
	items, err := queryWorkspaceExportStage(app, `
		SELECT '`+workspaceExportItemDatabaseAutomation+`' AS [[kind]],
			'16:' || [[items]].[[page_id]] || ':' || [[items]].[[id]] AS [[sort_key]],
			[[items]].[[page_id]], `+workspaceExportAutomationItemSQL+` AS [[payload]]
		FROM {{leafen_database_automation_definitions}} AS [[items]]
		LEFT JOIN {{leafen_pages}} AS [[pages]] ON [[pages]].[[id]] = [[items]].[[page_id]]
		WHERE [[items]].[[workspace_id]] = {:workspaceId} AND [[items]].[[owner]] = {:ownerId}
			AND [[items]].[[status]] != '`+databaseAutomationStatusDeleted+`'
			AND (([[pages]].[[id]] IS NOT NULL AND [[pages]].[[notion_import_staging]] = FALSE AND `+filter.sql("[[pages]]")+`
					AND `+workspaceExportAutomationSourceSQL+`)
				OR ([[items]].[[page_id]] = {:peoplePage} AND [[items]].[[database_id]] = {:peopleSource}))
			AND ([[items]].[[page_id]], [[items]].[[id]]) > ({:afterPage}, {:afterId})
		ORDER BY [[items]].[[page_id]], [[items]].[[id]] LIMIT {:limit}
	`, params, limit)
	if err != nil || !hasPeople {
		return items, err
	}
	for index := range items {
		if items[index].PageID == people.PageID {
			if items[index].Payload, err = peopleArchiveAutomation(app, people, items[index].Payload); err != nil {
				return nil, err
			}
			items[index].PageID = peopleArchiveSourcePrefix + people.WorkspaceID
		}
	}
	return items, nil
}

// loadWorkspaceExportScopedAutomationWindow reads the automations of the
// data sources a page's or a database's archive takes.
func loadWorkspaceExportScopedAutomationWindow(app core.App, authorization workspaceAuthorization, job storedJob, payload workspaceExportJobPayload, cursor string, limit int) ([]storedWorkspaceExportInventoryItem, error) {
	if payload.Format != workspaceExportFormatPageNativeArchive {
		return nil, nil
	}
	after, err := workspaceExportStageCursor(cursor, "19", 2)
	if err != nil {
		return nil, err
	}
	params := scopedExportParams(authorization, job, payload)
	params["afterPage"], params["afterId"] = after[0], after[1]
	// The same data sources the archive's data source stage takes.
	filter := "EXISTS (SELECT 1 FROM {{leafen_workspace_export_scope_pages}} [[scope]] WHERE [[scope]].[[job_id]] = {:jobId} AND [[scope]].[[page_id]] = [[items]].[[page_id]])"
	if workspaceExportDatabaseArchive(payload) {
		filter = "(" + workspaceExportBeyondRootSQL("[[items]].[[page_id]]") + " OR " +
			workspaceExportDatabaseArchiveSourceSQL("[[items]].[[database_id]]") + ")"
	}
	return queryWorkspaceExportStage(app, `
		SELECT '`+workspaceExportItemDatabaseAutomation+`' AS [[kind]],
			'19:' || [[items]].[[page_id]] || ':' || [[items]].[[id]] AS [[sort_key]],
			[[items]].[[page_id]], `+workspaceExportAutomationItemSQL+` AS [[payload]]
		FROM {{leafen_database_automation_definitions}} AS [[items]]
		WHERE [[items]].[[workspace_id]] = {:workspaceId} AND [[items]].[[owner]] = {:ownerId}
			AND [[items]].[[status]] != '`+databaseAutomationStatusDeleted+`' AND `+filter+`
			AND `+workspaceExportAutomationSourceSQL+`
			AND ([[items]].[[page_id]], [[items]].[[id]]) > ({:afterPage}, {:afterId})
		ORDER BY [[items]].[[page_id]], [[items]].[[id]] LIMIT {:limit}
	`, params, limit)
}
