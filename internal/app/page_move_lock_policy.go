package app

import "github.com/pocketbase/pocketbase/core"

// pageMoveLockConflict is the sole server-side policy owner for movement out
// of a locked parent or into a locked destination. The caller supplies the
// already-authorized destination so same-parent moves add no duplicate read.
func pageMoveLockConflict(app core.App, authorization pageAuthorization, destination *core.Record) (bool, error) {
	if destination != nil && destination.GetBool("is_locked") {
		return true, nil
	}
	currentParentID := authorization.Page.GetString("parent_id")
	if currentParentID == "" {
		return false, nil
	}
	if destination != nil && currentParentID == destination.Id {
		return destination.GetBool("is_locked"), nil
	}
	currentParent, err := app.FindRecordById(pagesCollection, currentParentID)
	if err != nil {
		return false, err
	}
	if currentParent.GetString("workspace") != authorization.Workspace.WorkspaceID ||
		currentParent.GetString("owner") != authorization.Workspace.OwnerID {
		return true, nil
	}
	return currentParent.GetBool("is_locked"), nil
}
