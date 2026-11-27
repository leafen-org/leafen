package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// A published site's "복제", as Notion's (its help "Duplicate public
// pages", read 2026-10-12): a visitor signed in to this server copies the
// page they are on, with the pages under it that the site shows, into the
// private pages of a workspace of theirs, where the site lets visitors
// duplicate it ("템플릿으로 복제", on by default). The copy reads its pages
// through the site's link, not the workspace the visitor is outside of: the
// site must stay published and letting them duplicate while it runs, and a
// page under it the site doesn't show stays behind.

const publicPageDuplicatePath = "/public/sites/{slug}/duplicate"

type publicPageDuplicateInput struct {
	RequestID   string `json:"requestId"`
	PageID      string `json:"pageId"`
	WorkspaceID string `json:"workspaceId"`
}

func registerPublicPageDuplicateRoutes(event *core.ServeEvent) {
	event.Router.POST(apiRoot+publicPageDuplicatePath, startPublicPageDuplicate).Bind(apis.BodyLimit(pageDuplicationMaxBodyBytes))
}

func startPublicPageDuplicate(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	slug := strings.TrimSpace(event.Request.PathValue("slug"))
	input := publicPageDuplicateInput{}
	if err := event.BindBody(&input); err != nil {
		return pageDuplicationError(event, errPageDuplicationInvalid, "bind")
	}
	input.RequestID, input.PageID, input.WorkspaceID = strings.TrimSpace(input.RequestID), strings.TrimSpace(input.PageID), strings.TrimSpace(input.WorkspaceID)
	if slug == "" || input.RequestID == "" || input.PageID == "" || input.WorkspaceID == "" ||
		len([]rune(input.RequestID)) > maxPageDuplicationRequestIDRunes ||
		len(input.PageID) > maxWorkspacePermissionIdentifierLength || len(input.WorkspaceID) > maxWorkspacePermissionIdentifierLength {
		return pageDuplicationError(event, errPageDuplicationInvalid, "validate")
	}
	authority, err := authorizePublicSitePages(event.App, slug, []string{input.PageID}, time.Now().UTC())
	if errors.Is(err, errPublicVisibilityNotFound) {
		return pageDuplicationError(event, errPageDuplicationNotFound, "site")
	}
	if err != nil {
		return pageDuplicationError(event, err, "site")
	}
	admission, err := admitPageDuplicationWith(event.App, pageDuplicationFromApp, user.Id, input.PageID, pageDuplicationInput{
		RequestID: input.RequestID, DestinationWorkspaceID: input.WorkspaceID,
	}, "", authority.AuthorityID, "")
	if err != nil {
		return pageDuplicationError(event, err, "admit")
	}
	response, err := pageDuplicationResponseForJob(admission.Job, admission.Duplicate)
	if err != nil {
		return pageDuplicationError(event, err, "response")
	}
	if runner, ok := event.App.Store().Get(durableJobRunnerStoreKey).(*durableJobRunner); ok {
		runner.notify()
	}
	status := http.StatusAccepted
	if response.Status == pageDuplicationStatusCompleted || response.Status == pageDuplicationStatusFailed {
		status = http.StatusOK
	}
	return event.JSON(status, response)
}

// publicPageDuplicationSource is the page a visitor copies and its workspace,
// read through the site's link: published, letting visitors duplicate it,
// and showing the page.
func publicPageDuplicationSource(app core.App, linkID, pageID string) (pageAuthorization, workspaceAuthorization, error) {
	if err := publicPageDuplicationOpen(app, linkID, pageID); err != nil {
		return pageAuthorization{}, workspaceAuthorization{}, err
	}
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return pageAuthorization{}, workspaceAuthorization{}, errors.Join(errPageDuplicationNotFound, err)
	}
	workspace, err := app.FindRecordById(workspacesCollection, page.GetString("workspace"))
	if err != nil || workspace.GetString("deletion_job_id") != "" || page.GetString("owner") != workspace.GetString("owner") {
		return pageAuthorization{}, workspaceAuthorization{}, errors.Join(errPageDuplicationNotFound, err)
	}
	authority := workspaceAuthorization{
		Workspace: workspace, WorkspaceID: workspace.Id, OwnerID: workspace.GetString("owner"),
		WorkspaceOwnerID: workspacePermissionOwnerID(workspace),
		Organization:     organizationAuthorization{OrganizationID: workspace.GetString("organization"), OwnerID: workspace.GetString("owner")},
	}
	return pageAuthorization{Workspace: authority, Page: page, AccessRole: pageAccessRoleView}, authority, nil
}

// publicPageDuplicationOpen fails unless the link's site is published, lets
// its visitors duplicate it and shows the page.
func publicPageDuplicationOpen(app core.App, linkID, pageID string) error {
	var open int
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_public_sites}} [[site]]
		JOIN {{leafen_public_share_links}} [[link]] ON [[link]].[[id]] = [[site]].[[share_link_id]]
		WHERE [[link]].[[id]] = {:link} AND [[link]].[[enabled]] = 1 AND [[site]].[[published]] = 1
			AND [[site]].[[allow_duplicate]] = 1
			AND ([[link]].[[root_page_id]] = {:page} OR EXISTS (
				SELECT 1 FROM {{leafen_public_share_graph_memberships}} [[member]]
				WHERE [[member]].[[share_link_id]] = [[link]].[[id]] AND [[member]].[[page_id]] = {:page}
					AND [[member]].[[state]] = 'complete'))
	`).Bind(dbx.Params{"link": linkID, "page": pageID}).Row(&open)
	if errors.Is(err, sql.ErrNoRows) || err == nil && open == 0 {
		return errPageDuplicationNotFound
	}
	return err
}

// publicPageDuplicationChildSQL keeps a visitor's copy to the pages the site
// shows; empty for a copy within the visitor's own workspaces.
func publicPageDuplicationChildSQL(job storedPageDuplicationJob) (string, dbx.Params) {
	if job.SourcePublicLinkID == "" {
		return "", dbx.Params{}
	}
	return ` AND EXISTS (SELECT 1 FROM {{leafen_public_share_graph_memberships}} [[public_member]]
			WHERE [[public_member]].[[share_link_id]] = {:publicLinkId} AND [[public_member]].[[page_id]] = [[id]]
				AND [[public_member]].[[state]] = 'complete')`, dbx.Params{"publicLinkId": job.SourcePublicLinkID}
}

// sourceReadable checks the person may still read the pages the copy reads:
// for a visitor's copy, the site still lets them; for a copy into another
// workspace, the page it copies.
func (job storedPageDuplicationJob) sourceReadable(app core.App) error {
	if job.SourcePublicLinkID != "" {
		if err := publicPageDuplicationOpen(app, job.SourcePublicLinkID, job.SourceRootPageID); err != nil {
			return refusedUnlessStorage(errPageDuplicationConflict, err)
		}
		return nil
	}
	// A copy into another workspace still reads its page, a guest by its shares (page_duplication_guest.go).
	if job.crossWorkspace() {
		if _, err := authorizePage(app, job.ActorID, job.SourceRootPageID, permissionWorkspaceRead); err != nil {
			return refusedUnlessStorage(errPageDuplicationConflict, err)
		}
	}
	return nil
}
