package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What a server administrator changes in a workspace (administrator_
// workspace_content.go): a page put in the trash or permanently deleted,
// and what the workspace made public turned off — each as the workspace's
// owner would, and kept.

const administratorContentDeleteAdvances = 32

type administratorContentActionInput struct {
	RequestID string `json:"requestId"`
}

// administratorContentPage is a workspace's page an administrator acts on:
// one of the workspace's own pages, not a database item, which goes with its
// database's page.
func administratorContentPage(event *core.RequestEvent, workspace administratorContentWorkspace) (*core.Record, bool) {
	page, err := event.App.FindRecordById(pagesCollection, strings.TrimSpace(event.Request.PathValue("pageId")))
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (page.GetString("workspace") != workspace.ID ||
		page.GetString("owner") != workspace.OwnerID || page.GetBool("notion_import_staging"))) {
		_ = apiError(event, http.StatusNotFound, errorCodePageNotFound, "Page not found.")
		return nil, false
	}
	if err != nil {
		_ = administratorContentError(event, err, "page")
		return nil, false
	}
	if isDatabaseRowPageRecord(page) {
		_ = apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "A database item goes with its database's page.")
		return nil, false
	}
	return page, true
}

// trashAdministratorWorkspacePage puts a page, and the pages under it, in
// the workspace's trash, where its owner can restore it.
func trashAdministratorWorkspacePage(event *core.RequestEvent) error {
	actorID, workspace, ok := administratorContentRequest(event)
	if !ok {
		return nil
	}
	page, ok := administratorContentPage(event, workspace)
	if !ok {
		return nil
	}
	if !page.GetBool("in_trash") {
		input := normalizePageMetadataInput(pageMetadataInput{
			Title: page.GetString("title"), ParentID: page.GetString("parent_id"), InTrash: true,
			MutationID: "server-admin-trash:" + newDiagnosticReference(), BaseCursor: page.GetInt("cursor"),
		})
		_, status, err := applyPageMetadata(event.App, workspace.PersonID, page.Id, input, pageMetadataRequestHash(page.Id, input))
		if err != nil {
			return administratorContentError(event, err, "trash")
		}
		if status == pageMetadataCursorConflict {
			return apiError(event, http.StatusConflict, errorCodeGraphPageCursorConflict, "The page changed. Try again.")
		}
		if status != pageMetadataApplied {
			// As the page's own route says it to its workspace's owner: a page an
			// operation holds a while is busy (page_metadata_held.go).
			return answerPageMetadataRefused(event, workspace.PersonID, page.Id, status)
		}
	}
	if err := recordAdministratorContentEvent(event.App, workspace.ID, actorID, administratorContentTrashPage,
		"page", page.Id, workspace.titleAsRead(event.App, page.Id, page.GetString("title"))); err != nil {
		return administratorContentError(event, err, "record")
	}
	return event.JSON(http.StatusOK, administratorWorkspaceActionResult{Status: "trashed"})
}

// deleteAdministratorWorkspacePage permanently deletes a page in the trash
// and the pages under it, unless a legal hold keeps them. A deletion that
// takes longer goes on with the same request ID.
func deleteAdministratorWorkspacePage(roomInvalidator workspaceRoomInvalidator) func(*core.RequestEvent) error {
	return func(event *core.RequestEvent) error {
		actorID, workspace, ok := administratorContentRequest(event)
		if !ok {
			return nil
		}
		input := administratorContentActionInput{}
		if err := event.BindBody(&input); err != nil || strings.TrimSpace(input.RequestID) == "" ||
			len([]rune(input.RequestID)) > maxPagePermanentDeleteRequestIDRunes {
			return pagePermanentDeleteError(event, errPagePermanentDeleteInvalid, "administrator-validate")
		}
		page, ok := administratorContentPage(event, workspace)
		if !ok {
			return nil
		}
		requestID := "server-admin:" + strings.TrimSpace(input.RequestID)
		// A key's call asks its person only for a deletion that would start
		// (user_key_acting_ask.go).
		if err := rehearseBeforeAsking(event, func(txApp core.App) error {
			_, err := admitPagePermanentDelete(txApp, personAccessSubject(workspace.PersonID), page.Id, requestID)
			return err
		}); err != nil {
			return pagePermanentDeleteError(event, err, "administrator-admit")
		}
		if asked, err := askBeforeActing(event); asked {
			return err
		}
		admission, err := admitPagePermanentDelete(event.App, personAccessSubject(workspace.PersonID), page.Id, requestID)
		if err != nil {
			return pagePermanentDeleteError(event, err, "administrator-admit")
		}
		if admission.Admitted {
			if err := recordAdministratorContentEvent(event.App, workspace.ID, actorID, administratorContentDeletePage,
				"page", page.Id, workspace.titleAsRead(event.App, page.Id, page.GetString("title"))); err != nil {
				return administratorContentError(event, err, "record")
			}
		}
		job := admission.Job
		if job.Status != pagePermanentDeleteStatusCompleted {
			if err := invalidatePermanentPageRooms(roomInvalidator, job.WorkspaceID); err != nil {
				return pagePermanentDeleteError(event, errors.Join(errPagePermanentDeleteUnavailable, err), "administrator-invalidate")
			}
			for advance := 0; advance < administratorContentDeleteAdvances && job.Status != pagePermanentDeleteStatusCompleted; advance++ {
				if job, err = advancePagePermanentDelete(event.App, job.ID); err != nil {
					return pagePermanentDeleteError(event, err, "administrator-advance")
				}
			}
		}
		status := "deleting"
		if job.Status == pagePermanentDeleteStatusCompleted {
			status = "deleted"
		}
		return event.JSON(http.StatusOK, administratorWorkspaceActionResult{Status: status})
	}
}

// listAdministratorWorkspacePublic lists all a workspace has made public —
// sites, forms and pages anyone with the link opens — as its settings'
// "공개 페이지" does, whoever can open them.
func listAdministratorWorkspacePublic(event *core.RequestEvent) error {
	_, workspace, ok := administratorContentRequest(event)
	if !ok {
		return nil
	}
	list := administratorWorkspacePublicList{Items: []administratorWorkspacePublicItem{}}
	for _, kind := range []string{"sites", "forms", "links"} {
		items, err := administratorPublicItems(event.App, workspace, kind)
		if err != nil {
			return administratorContentError(event, err, "public")
		}
		list.Items = append(list.Items, items...)
	}
	return event.JSON(http.StatusOK, list)
}

// administratorPublicItems reads every public item of a kind in a
// workspace, newest first, each titled as the administrator reads it.
func administratorPublicItems(app core.App, workspace administratorContentWorkspace, kind string) ([]administratorWorkspacePublicItem, error) {
	items := []administratorWorkspacePublicItem{}
	after := textIDCursor{}
	for {
		window, ids, next, err := workspacePublicContentWindowOf(app, "", workspace.ID, kind, "", after)
		if err != nil {
			return nil, err
		}
		publicContentItemsAsRead(app, workspace.OwnerID, window)
		pageIDs := make([]string, 0, len(window))
		for _, item := range window {
			pageIDs = append(pageIDs, item.PageID)
		}
		kinds, err := loadPageNameKinds(app, pageIDs)
		if err != nil {
			return nil, err
		}
		for index, item := range window {
			public := administratorWorkspacePublicItem{
				Kind: map[string]string{"sites": administratorPublicSite, "forms": administratorPublicForm, "links": administratorPublicLink}[kind],
				ID:   ids[index], PageID: item.PageID, Title: item.Title, TitleSpans: item.TitleSpans, Path: item.Path,
				PublishedByName: item.PublishedBy.Name, PublishedAt: item.PublishedAt,
				PageKind: kinds[item.PageID].Kind, ViewBlock: kinds[item.PageID].ViewBlock, SourceTitles: kinds[item.PageID].SourceTitles,
			}
			items = append(items, public)
		}
		if next == "" {
			return items, nil
		}
		if after, err = decodeTextIDCursor(next); err != nil {
			return nil, err
		}
	}
}

// disableAdministratorWorkspacePublic turns off what a workspace made
// public: a site taken down, a link turned off, a form closed to the web.
// Its public address closes at once.
func disableAdministratorWorkspacePublic(event *core.RequestEvent) error {
	actorID, workspace, ok := administratorContentRequest(event)
	if !ok {
		return nil
	}
	kind, itemID := strings.TrimSpace(event.Request.PathValue("kind")), strings.TrimSpace(event.Request.PathValue("itemId"))
	var pageID, title string
	err := event.App.DB().NewQuery(`
		SELECT [[item]].[[page_id]], COALESCE([[page]].[[title]], '') AS [[title]] FROM (
			SELECT [[root_page_id]] AS [[page_id]] FROM {{leafen_public_sites}}
				WHERE {:kind} = 'site' AND [[id]] = {:id} AND [[workspace_id]] = {:workspaceId} AND [[published]] = 1
			UNION ALL SELECT [[root_page_id]] FROM {{leafen_public_share_links}}
				WHERE {:kind} = 'link' AND [[id]] = {:id} AND [[workspace_id]] = {:workspaceId} AND [[web_enabled]] = 1
			UNION ALL SELECT [[page_id]] FROM {{leafen_form_links}}
				WHERE {:kind} = 'form' AND [[id]] = {:id} AND [[workspace_id]] = {:workspaceId} AND [[enabled]] = 1
		) [[item]] LEFT JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[item]].[[page_id]] LIMIT 1
	`).Bind(dbx.Params{"kind": kind, "id": itemID, "workspaceId": workspace.ID}).Row(&pageID, &title)
	if errors.Is(err, sql.ErrNoRows) {
		return apiError(event, http.StatusNotFound, errorCodePublicContentNotFound, "Public page not found.")
	}
	if err != nil {
		return administratorContentError(event, err, "public-item")
	}
	if kind == administratorPublicForm {
		_, err = event.App.DB().NewQuery(`
			UPDATE {{leafen_form_links}} SET [[audience]] = {:none}, [[enabled]] = 0, [[updated_at]] = {:now}
			WHERE [[id]] = {:id} AND [[workspace_id]] = {:workspaceId}
		`).Bind(dbx.Params{"none": formAudienceNone, "now": time.Now().UTC().Format(time.RFC3339Nano),
			"id": itemID, "workspaceId": workspace.ID}).Execute()
	} else {
		err = turnOffPublicAsOwner(event, workspace, pageID, kind)
	}
	if err != nil {
		// Its page, held a while by an operation, says so (page_held_refusal.go).
		return publicMutationRouteError(event, refusalOnPageAs(err, pageID, workspace.PersonID), "administrator-disable")
	}
	if err := recordAdministratorContentEvent(event.App, workspace.ID, actorID, administratorContentDisablePublic,
		kind, itemID, workspace.titleAsRead(event.App, pageID, title)); err != nil {
		return administratorContentError(event, err, "record")
	}
	return event.JSON(http.StatusOK, administratorWorkspaceActionResult{Status: "disabled"})
}

// turnOffPublicAsOwner takes a page's site down or turns its link off as the
// workspace's owner would in the page's share menu.
func turnOffPublicAsOwner(event *core.RequestEvent, workspace administratorContentWorkspace, pageID, kind string) error {
	stored, err := loadStoredPublicMutationState(event.App, pageID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	off := false
	input := publicMutationInput{
		RequestID: "server-admin-public:" + newDiagnosticReference(), ExpectedRevision: stored.Revision,
		Action: publicMutationActionUnpublishSite,
	}
	if kind == administratorPublicLink {
		input.Action, input.Enabled = publicMutationActionSetWebSharing, &off
	}
	_, _, err = executePublicMutation(event.Request.Context(), event.App, workspace.PersonID, pageID, input)
	return err
}
