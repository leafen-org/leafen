package app

import (
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

func eraseNotionPageContent(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
	authorization notionPageAuthorization,
	publicPageID string,
) error {
	return eraseNotionPageContentThen(event, ygo, authorization, publicPageID, nil)
}

// eraseNotionPageContentThen erases a page's body and answers with then, when
// given: the rest of its update — a template, its title, icon, cover, lock
// or trash — written once the body is gone (serveNotionPageUpdate). Its
// sub-pages and databases go to the trash with their blocks, from where
// each comes back at the body's end, as Notion's erase sends them (seen
// 2026-11-14: sub-pages, full-page and inline databases in the trash, the
// grandchildren and rows with them).
func eraseNotionPageContentThen(
	event *core.RequestEvent,
	ygo *pocketBaseYgoPersistence,
	authorization notionPageAuthorization,
	publicPageID string,
	then func() error,
) error {
	answer := func() error {
		if then != nil {
			return then()
		}
		updated, err := event.App.FindRecordById(pagesCollection, authorization.Page.Page.Id)
		if err != nil {
			return notionRequestNotCompleted(event, err)
		}
		return writeCurrentNotionPage(event, authorization.Identity, updated)
	}
	projection := notionPageMarkdownProjection{}
	pageCursor := 0
	err := event.App.RunInTransaction(func(txApp core.App) error {
		current, err := authorizePage(
			txApp, authorization.Identity.PrincipalID,
			authorization.Page.Page.Id, permissionWorkspaceWrite,
		)
		if err != nil || current.Workspace.WorkspaceID != authorization.Identity.WorkspaceID ||
			!notionOAuthIdentityAllowsPage(txApp, authorization.Identity, current) {
			if err == nil || isPermissionDenied(err) {
				return permissionDenied()
			}
			return err
		}
		projection, err = projectNotionPageMarkdown(
			txApp, current.Workspace.OwnerID, current.Page.Id, publicPageID, notionMarkdownReader{},
		)
		if err == nil && projection.Truncated {
			// A page longer than one read erases by all its top blocks, not
			// those the read reached.
			projection.RootBlockIDs, projection.SuggestedRootBlockIDs = nil, nil
			err = txApp.DB().NewQuery(`
				SELECT [[id]] FROM {{leafen_blocks}}
				WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND COALESCE([[parent_id]], '')=''
				ORDER BY [[position]], [[id]]
			`).Bind(map[string]any{
				"owner": current.Workspace.OwnerID, "pageId": current.Page.Id,
			}).Column(&projection.RootBlockIDs)
		}
		pageCursor = current.Page.GetInt("cursor")
		return err
	})
	if isPermissionDenied(err) {
		return notionPageNotFound(event)
	}
	if err != nil {
		return notionRequestNotCompleted(event, err)
	}
	rootBlockIDs := append(projection.RootBlockIDs, projection.SuggestedRootBlockIDs...)
	if len(rootBlockIDs) == 0 {
		return answer()
	}
	operations := make([]graphOperation, 0, len(rootBlockIDs))
	for _, blockID := range rootBlockIDs {
		operations = append(operations, graphOperation{
			Kind: graphOperationKindDeleteBlock,
			Deletion: &graphBlockDelete{
				BlockID: blockID,
			},
		})
	}
	_, err = ygo.applyContentGraphMutationContext(
		event.Request.Context(), authorization.Identity.PrincipalID,
		authorization.Page.Page.Id,
		graphMutationInput{
			MutationID: "notion-public-page-erase:" + newDiagnosticReference(),
			Operations: operations, BasePageCursor: &pageCursor,
			BaseGraphRevision: &projection.GraphRevision,
		},
		contentGraphMutationHooks{
			Authorize: authorizeNotionPageMutation(
				authorization.Identity, authorization.Page.Page.Id,
			),
			// Page blocks follow their pages: a sub-page's goes to the
			// trash with it, as Markdown's replace sends it.
			PersonChange: true,
		},
	)
	if err != nil {
		return notionPageMarkdownMutationError(event, err)
	}
	return answer()
}

func writeCurrentNotionPage(
	event *core.RequestEvent,
	identity notionOAuthBearerIdentity,
	page *core.Record,
) error {
	response, err := notionWrittenPageObject(event, identity, page)
	if err != nil {
		return notionDatabaseRowPageReadError(event, err)
	}
	return event.JSON(http.StatusOK, response)
}
