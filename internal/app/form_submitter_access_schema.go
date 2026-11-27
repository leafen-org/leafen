package app

import (
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A form answered only by people in the workspace can give each respondent
// access to the page their response creates, as Notion's form setting does:
// none, view, comment, edit or full access, named as the official view API
// names them. Like Notion's, the setting does nothing while the form is open
// to the web.

const (
	formSubmitterAccessNone         = "none"
	formSubmitterAccessReader       = "reader"
	formSubmitterAccessCommentOnly  = "comment_only"
	formSubmitterAccessReadAndWrite = "read_and_write"
	formSubmitterAccessEditor       = "editor"
)

// formSubmitterAccessRole is the page share a form's setting gives its
// respondent, or "" for none.
func formSubmitterAccessRole(access string) (string, bool) {
	switch access {
	case formSubmitterAccessNone:
		return "", true
	case formSubmitterAccessReader:
		return "view", true
	case formSubmitterAccessCommentOnly:
		return "comment", true
	case formSubmitterAccessReadAndWrite:
		return "edit", true
	case formSubmitterAccessEditor:
		return "full_access", true
	default:
		return "", false
	}
}

// grantFormSubmitterAccess shares the page a response created with its
// respondent, as the form's setting asks. A response to a form open to the
// web, or from a respondent who could not be shared a page of this workspace
// by hand, gets no access.
func grantFormSubmitterAccess(app core.App, link storedFormLink, principalID, pageID string) error {
	role, _ := formSubmitterAccessRole(formSubmitterAccessOrNone(link.SubmitterAccess))
	if role == "" || principalID == "" || link.Audience != formAudienceWorkspace {
		return nil
	}
	workspace, err := app.FindRecordById(workspacesCollection, link.WorkspaceID)
	if err != nil {
		return err
	}
	if _, err := authorizeOrganization(
		app, principalID, workspace.GetString("organization"), permissionOrganizationActive,
	); err != nil {
		if isPermissionDenied(err) {
			return nil
		}
		return err
	}
	// The share is the form creator's, or the respondent's own once the
	// creator's account is gone.
	now := time.Now().UTC().UnixMilli()
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_page_permissions}}
			([[id]],[[workspace_id]],[[page_id]],[[principal_type]],[[principal_id]],[[role]],
			 [[created_by]],[[created_at]],[[updated_at]])
		VALUES ({:id},{:workspaceId},{:pageId},'user',{:principalId},{:role},
			COALESCE((SELECT [[id]] FROM {{users}} WHERE [[id]] = {:createdBy}), {:principalId}),{:now},{:now})
		ON CONFLICT ([[page_id]],[[principal_type]],[[principal_id]]) DO NOTHING
	`).Bind(dbx.Params{
		"id": securityRandomID(), "workspaceId": link.WorkspaceID, "pageId": pageID,
		"principalId": principalID, "role": role, "createdBy": link.CreatedBy, "now": now,
	}).Execute()
	return err
}
