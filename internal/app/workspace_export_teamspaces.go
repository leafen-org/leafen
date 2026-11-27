package app

import (
	"github.com/pocketbase/pocketbase/core"
)

// loadWorkspaceExportTeamspaceWindow exports the workspace's live Teamspaces
// so an import can put pages back in them. They are no page's.
func loadWorkspaceExportTeamspaceWindow(
	app core.App, authorization workspaceAuthorization, filter workspaceExportPageFilter, cursor string, limit int,
) ([]storedWorkspaceExportInventoryItem, error) {
	after, err := workspaceExportStageCursor(cursor, "15", 1)
	if err != nil {
		return nil, err
	}
	params := exportStageParams(authorization, filter)
	params["afterId"] = after[0]
	return queryWorkspaceExportStage(app, `
		SELECT 'teamspace' AS [[kind]], '15:' || [[id]] AS [[sort_key]],
			[[id]] AS [[page_id]], json_object(
				'id', [[id]], 'name', [[name]], 'icon', [[icon]], 'description', [[description]],
				'access', [[access]], 'memberPageRole', [[member_page_role]], 'openPageRole', [[open_page_role]],
				'outsideAccess', json(CASE WHEN [[outside_access]] THEN 'true' ELSE 'false' END),
				'disablePublishing', json(CASE WHEN [[disable_publishing]] THEN 'true' ELSE 'false' END),
				'disableGuests', json(CASE WHEN [[disable_guests]] THEN 'true' ELSE 'false' END),
				'disableExport', json(CASE WHEN [[disable_export]] THEN 'true' ELSE 'false' END),
				'disablePublishing', json(CASE WHEN [[disable_publishing]] THEN 'true' ELSE 'false' END),
				'disableGuests', json(CASE WHEN [[disable_guests]] THEN 'true' ELSE 'false' END),
				'disableExport', json(CASE WHEN [[disable_export]] THEN 'true' ELSE 'false' END),
				'membersCanInvite', json(CASE WHEN [[members_can_invite]] THEN 'true' ELSE 'false' END),
				'membersCanEditSidebar', json(CASE WHEN [[members_can_edit_sidebar]] THEN 'true' ELSE 'false' END)
			) AS [[payload]]
		FROM {{leafen_teamspaces}}
		WHERE [[workspace_id]]={:workspaceId} AND [[deleted_at]]=0 AND [[id]]>{:afterId}
		ORDER BY [[id]] LIMIT {:limit}
	`, params, limit)
}
