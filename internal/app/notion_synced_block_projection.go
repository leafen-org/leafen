package app

import (
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

func projectNotionSyncedBlockContent(
	app core.App,
	authorization notionBlockAuthorization,
	content map[string]any,
) map[string]any {
	sourceID, _ := content["syncedBlockId"].(string)
	if strings.TrimSpace(sourceID) == "" {
		return content
	}
	// A copy syncing from no block names none, as Notion's reads
	// (synced_block_sourceless.go).
	if sourceID == syncedBlockSourceless {
		return map[string]any{"synced_from": map[string]any{"type": "block_id"}}
	}
	// A copy of a page's body names the page, as Notion's API reads one
	// (seen 2026-11-27), for a reader who may open it.
	if page := syncedCopyPage(content); page != "" {
		authorized, err := authorizePage(app, authorization.Identity.PrincipalID, page, permissionWorkspaceRead)
		if err != nil || authorized.Workspace.WorkspaceID != authorization.Page.Workspace.WorkspaceID ||
			!notionOAuthIdentityAllowsPage(app, authorization.Identity, authorized) {
			return content
		}
		publicID, err := notionPagePublicID(app, page)
		if err != nil {
			return content
		}
		return map[string]any{"synced_from": map[string]any{"type": "block_id", "block_id": publicID}}
	}
	source, found, err := loadStoredNotionBlock(app, sourceID)
	if err != nil {
		return content
	}
	// A copy whose original was deleted keeps naming it, as Notion's API
	// shows such a copy (seen 2026-10-14); its children are not found.
	if !found {
		return notionSyncedFromPayload(app, authorization.Page.Workspace.WorkspaceID, sourceID, content)
	}
	if source.Owner != authorization.Page.Workspace.OwnerID ||
		source.WorkspaceID != authorization.Page.Workspace.WorkspaceID {
		return content
	}
	if expectedPageID, _ := content["syncedPageId"].(string); expectedPageID != "" &&
		expectedPageID != source.PageID {
		return content
	}
	page, err := authorizePage(
		app, authorization.Identity.PrincipalID, source.PageID, permissionWorkspaceRead,
	)
	if err != nil || source.ID == authorization.Block.ID ||
		!notionOAuthIdentityAllowsPage(app, authorization.Identity, page) {
		return content
	}
	return notionSyncedFromPayload(app, source.WorkspaceID, source.ID, content)
}

func notionSyncedFromPayload(app core.App, workspaceID, sourceID string, content map[string]any) map[string]any {
	publicID, err := notionBlockPublicID(app, workspaceID, sourceID)
	if err != nil {
		return content
	}
	return map[string]any{
		"synced_from": map[string]any{"type": "block_id", "block_id": publicID},
	}
}
