package app

import (
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

func deleteNotionChildPageBlock(event *core.RequestEvent, publicPageID string) error {
	authorization, authorized, err := authorizeNotionPage(
		event, publicPageID, "pages:write", permissionWorkspaceWrite,
	)
	if err != nil || !authorized {
		return err
	}
	if refused, err := refuseNotionTrashedEdit(event, authorization.Page.Page.Id, true, false, notionTrashedEdit); refused {
		return err
	}
	response := map[string]any{}
	err = event.App.RunInTransaction(func(txApp core.App) error {
		current, err := authorizePage(
			txApp, authorization.Identity.PrincipalID,
			authorization.Page.Page.Id, permissionWorkspaceWrite,
		)
		if err != nil || current.Workspace.WorkspaceID != authorization.Identity.WorkspaceID ||
			!notionOAuthIdentityAllowsPage(txApp, authorization.Identity, current) ||
			strings.TrimSpace(current.Page.GetString("parent_id")) == "" ||
			current.Page.GetBool("in_trash") {
			if err == nil || isPermissionDenied(err) {
				return permissionDenied()
			}
			return err
		}
		parent, err := notionChildPageParent(txApp, current.Page)
		if err != nil {
			return err
		}
		hasChildren, err := loadNotionPageHasChildren(txApp, []string{current.Page.Id})
		if err != nil {
			return err
		}
		response, err = notionChildPageBlockObject(
			txApp, current.Page, parent, hasChildren[current.Page.Id],
		)
		if err != nil {
			return err
		}
		metadata := pageMetadataInput{
			Title: current.Page.GetString("title"), ParentID: current.Page.GetString("parent_id"),
			InTrash: true, MutationID: "notion-public-child-page-delete:" + newDiagnosticReference(),
			BaseCursor: current.Page.GetInt("cursor"),
		}
		_, status, err := applyPageMetadataTransaction(
			txApp, authorization.Identity.PrincipalID, current.Page.Id, metadata,
			pageMetadataRequestHash(current.Page.Id, metadata),
		)
		if err != nil {
			return err
		}
		if status != pageMetadataApplied {
			return graphConflictError(
				errorCodeGraphPageCursorConflict, "The child page could not be deleted in its current state.",
			)
		}
		return nil
	})
	if isPermissionDenied(err) {
		return notionBlockNotFound(event)
	}
	if err != nil {
		return notionBlockMutationError(event, err)
	}
	response["in_trash"] = true
	return event.JSON(http.StatusOK, response)
}
