package app

import (
	"github.com/pocketbase/pocketbase/core"
)

// A guest — someone who writes no page of the workspace but those shared
// with them — sends a page they edit to the trash only from under a page
// they also edit, and archives none, as Notion's menu offers them (seen
// 2026-11-16 by a guest editing a page: "휴지통으로 이동" dimmed where they
// only read the page above or the page tops what is shared with them, and
// no "보관"; Notion's menu has no "보관" for anyone since 2026-11-05, and
// Leafen keeps it for members). The page's menu reads the same rule
// (pageActionAccessContext's useGuestEditsPageAbove); a member's change is
// judged by the page alone, as before, and so is a database item's page,
// which its database holds.
func pageMetadataGuestRefusal(
	app core.App, actorID string, authorization pageAuthorization, input pageMetadataInput,
) (*pageParentRefusal, error) {
	page := authorization.Page
	if page.GetString("data_source_id") != "" {
		return nil, nil
	}
	trashing := input.InTrash && !page.GetBool("in_trash")
	archiving := input.IsArchived != nil && *input.IsArchived != pageArchived(page)
	if !trashing && !archiving {
		return nil, nil
	}
	if _, err := authorizeWorkspace(app, actorID, authorization.Workspace.WorkspaceID, permissionWorkspaceWrite); !isPermissionDenied(err) {
		return nil, err
	}
	if archiving {
		return refusedPlace(true, "a guest doesn't archive pages: ask a member of the workspace", false, true), nil
	}
	if parentID := page.GetString("parent_id"); parentID != "" {
		_, err := authorizePage(app, actorID, parentID, permissionWorkspaceWrite)
		if err == nil || !isPermissionDenied(err) {
			return nil, err
		}
	}
	return refusedPlace(true, "a guest moves a page to the trash only from under a page they may edit", false, true), nil
}
