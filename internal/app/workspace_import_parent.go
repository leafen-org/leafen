package app

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// A new page from an import may be placed under a page, as page creation
// places a new page: under a page, a database page or a database template,
// whose body the importer must be able to edit, and which must not be
// locked for them (Notion's page menu has no 가져오기 on a locked page, seen
// 2026-11-19, though its sidebar still adds a sub-page there), still being
// imported, or a full-page database. A parent in the trash (itself or with a
// page above it) or deleted for good is refused as the page an import adds
// to is (workspaceImportTargetGone): told so, with what to do, to one who
// could add to it, and as a permission to anyone else (2026-11-20: a page
// under a trashed page answered 403 "You don't have permission to import.").
// A parent an operation holds for a while (pageHeldForMember: its blocks
// being moved, a wiki's change, its copy) takes it once that is done: it is
// refused as a conflict (workspaceImportParentHeld), not as a parent the
// importer may not add to.
// The parent is checked when the import is admitted, when its page is
// created — a held parent stays its parent — and when it is published; an
// import waits while it is held (workspace_import_held.go). The published
// page joins the end of the parent's body, or the top level if the parent
// can no longer hold it (workspaceImportParentLost): no longer the
// importer's to add to, gone to the trash or deleted for good on the way.

// workspaceImportParentHeldWords is the words of a held parent's refusal.
func workspaceImportParentHeldWords(err error) (string, bool) {
	var refusal *graphRequestError
	if errors.As(err, &refusal) && refusal.code == errorCodePageCreateParentConflict {
		return refusal.message, true
	}
	return "", false
}

func isWorkspaceImportParentHeld(err error) bool {
	_, held := workspaceImportParentHeldWords(err)
	return held
}

// workspaceImportParentLost reports whether a parent refused with err can
// no longer hold an import's page: not the importer's to add to, in the
// trash or deleted for good.
func workspaceImportParentLost(err error) bool {
	_, gone := workspaceImportTargetGoneRefusal(err)
	return gone || isPermissionDenied(err)
}

func authorizeWorkspaceImportParent(app core.App, ownerID, workspaceID, parentID string) (*core.Record, error) {
	parent, err := authorizePageBody(app, ownerID, parentID, permissionWorkspaceWrite)
	if isPermissionDenied(err) {
		if busy := pageHeldForMember(app, ownerID, parentID); busy != "" {
			return nil, workspaceImportParentHeld(busy)
		}
	}
	if err != nil {
		return nil, err
	}
	if parent.Workspace.WorkspaceID != workspaceID {
		return nil, permissionDenied()
	}
	// In the trash with a page above it too, as the editor reads it.
	if gone := workspaceImportTargetGone(app, parentID); gone != nil {
		return nil, gone
	}
	locked, err := pageLockedFor(app, parent.Page, ownerID)
	if err != nil {
		return nil, err
	}
	if locked || parent.Page.GetBool("notion_import_staging") || isFullPageDatabase(parent.Page) {
		return nil, permissionDenied()
	}
	fenced, err := wikiMutationFenceActive(app, parentID)
	if err != nil {
		return nil, err
	}
	if fenced {
		return nil, workspaceImportParentHeld(pageBusyWiki)
	}
	return parent.Page, nil
}

// authorizeWorkspaceImportPlace checks where an import asks to put its new
// page: under a page, at the top of a teamspace the importer can add pages
// to, as page creation checks one (Notion's CSV window lists its teamspaces
// under "팀스페이스", seen 2026-10-01), or into a database the importer can
// add rows to (workspace_import_database_place.go).
func authorizeWorkspaceImportPlace(app core.App, ownerID, workspaceID string, input workspaceImportInput) error {
	if input.ParentPageID != "" {
		_, err := authorizeWorkspaceImportParent(app, ownerID, workspaceID, input.ParentPageID)
		return err
	}
	if input.TeamspaceID != "" {
		return authorizeWorkspaceImportTeamspace(app, ownerID, workspaceID, input.TeamspaceID)
	}
	if input.DataSourceID != "" {
		_, err := authorizeWorkspaceImportDataSource(app, ownerID, workspaceID, input.DataSourceID)
		return err
	}
	return nil
}

func authorizeWorkspaceImportTeamspace(app core.App, ownerID, workspaceID, teamspaceID string) error {
	teamspaceWorkspace, role, err := effectiveTeamspacePageAccessRole(app, ownerID, workspaceID, teamspaceID)
	if err != nil {
		return err
	}
	if role < pageAccessRoleEdit || teamspaceWorkspace.WorkspaceID != workspaceID {
		return permissionDenied()
	}
	return nil
}

// placeWorkspaceImportRootUnder sets a new import page under its parent, or
// at the top level when the parent can no longer hold it: the top of the
// asked teamspace while the importer can add pages to it, else their own.
func placeWorkspaceImportRootUnder(app core.App, ownerID, workspaceID, parentID, teamspaceID string, page *core.Record) error {
	if parentID != "" {
		parent, err := authorizeWorkspaceImportParent(app, ownerID, workspaceID, parentID)
		if isWorkspaceImportParentHeld(err) {
			// Held for a while, it stays the parent; publishing checks it again.
			parent, err = app.FindRecordById(pagesCollection, parentID)
		}
		if err != nil && !workspaceImportParentLost(err) {
			return err
		}
		if err == nil {
			position, err := nextPageSiblingPosition(app, workspaceID, parentID)
			if err != nil {
				return err
			}
			page.Set("parent_id", parentID)
			page.Set("teamspace_id", parent.GetString("teamspace_id"))
			page.Set("position", position)
			return nil
		}
	}
	page.Set("parent_id", "")
	page.Set("position", 1)
	if teamspaceID != "" {
		err := authorizeWorkspaceImportTeamspace(app, ownerID, workspaceID, teamspaceID)
		if err != nil && !isPermissionDenied(err) {
			return err
		}
		if err == nil {
			page.Set("teamspace_id", teamspaceID)
		}
	}
	return nil
}

// publishWorkspaceImportRootPlacement adds a published import page to its
// parent's body.
func publishWorkspaceImportRootPlacement(app core.App, ownerID, workspaceOwnerID, workspaceID string, page *core.Record) error {
	parentID := page.GetString("parent_id")
	if parentID == "" {
		// A teamspace the importer can no longer add pages to gives it up.
		teamspaceID := page.GetString("teamspace_id")
		if teamspaceID == "" {
			return nil
		}
		err := authorizeWorkspaceImportTeamspace(app, ownerID, workspaceID, teamspaceID)
		if err == nil || !isPermissionDenied(err) {
			return err
		}
		page.Set("teamspace_id", "")
		return app.Save(page)
	}
	if _, err := authorizeWorkspaceImportParent(app, ownerID, workspaceID, parentID); err != nil {
		if !workspaceImportParentLost(err) {
			return err
		}
		position, err := nextPageSiblingPosition(app, workspaceID, "")
		if err != nil {
			return err
		}
		page.Set("parent_id", "")
		page.Set("teamspace_id", "")
		page.Set("position", position)
		return app.Save(page)
	}
	return placeChildPageBlock(app, workspaceOwnerID, workspaceID, parentID, page.Id, ownerID, pageBodyPlacement{Type: "end"})
}
