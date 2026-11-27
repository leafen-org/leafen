package app

import (
	"errors"
	"net/http"

	"github.com/pocketbase/pocketbase/core"
)

// updateNotionPageMetadata writes a page's title, icon, cover, trash,
// archive and lock from the public API, and says how to refuse them when it
// can't.
func updateNotionPageMetadata(
	event *core.RequestEvent,
	authorization notionPageAuthorization,
	page *core.Record,
	metadata pageMetadataInput,
	presentation notionPagePresentationRequest,
) (*core.Record, func() error) {
	result, status := pageMetadataResponse{}, pageMetadataApplied
	err := event.App.RunInTransaction(func(txApp core.App) error {
		// The connection's change — a lock too — names it in the page's history and updates.
		defer namePageHistoryConnection(txApp, event.Request.Context())()
		if err := validateNotionPagePresentationReferences(
			txApp, authorization.Identity.PrincipalID,
			authorization.Page.Workspace.WorkspaceID, &presentation,
		); err != nil {
			return err
		}
		var err error
		result, status, err = applyPageMetadataTransaction(
			txApp, authorization.Identity.PrincipalID, page.Id, metadata,
			pageMetadataRequestHash(page.Id, metadata),
		)
		return err
	})
	requestError := &graphRequestError{}
	switch {
	case errors.As(err, &requestError) && (requestError.status == http.StatusBadRequest || requestError.precondition):
		return nil, func() error { return notionPageValidationError(event, requestError.message) }
	case status == pageMetadataPageMissing:
		return nil, func() error { return notionPageNotFound(event) }
	case isDLPPolicyDecisionError(err):
		return nil, func() error { return notionDLPPolicyError(event, err) }
	case status == pageMetadataParentConflict:
		return nil, func() error {
			return notionPageParentRefused(event, "The page could not be updated in its current state.",
				pageParentConflictReason(event.App, authorization.Identity.PrincipalID, page.Id, metadata, pageLockIgnored))
		}
	case status != pageMetadataApplied || err != nil:
		return nil, func() error {
			return notionCompatibilityErrorWithData(event, http.StatusConflict, "conflict_error", "The page could not be updated in its current state.", nil, err)
		}
	}
	updated, err := event.App.FindRecordById(pagesCollection, result.Page.ID)
	if err != nil {
		return nil, func() error {
			return notionRequestNotCompleted(event, err)
		}
	}
	return updated, nil
}
