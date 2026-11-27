package organization

import (
	"context"
	"errors"
	"net/http"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/pocketbase/core"
)

func serveNotionAdminLegalHoldRelease(
	event *core.RequestEvent,
	rawLegalHoldID string,
) error {
	secret := leafen.BearerToken(event.Request)
	release := leafen.AcquireNotionAdminTokenLifecycleLock(leafen.NotionAdminTokenHash(secret))
	defer release()
	identity, authorized := leafen.AuthorizeNotionAdminAPI(
		event, secret, leafen.NotionAdminCapabilityLegalHoldWriteHighImpact,
	)
	if !authorized {
		return nil
	}
	if len(event.Request.URL.Query()) != 0 || event.Request.ContentLength != 0 ||
		len(event.Request.TransferEncoding) != 0 {
		return leafen.NotionAdminAPIError(
			event, http.StatusBadRequest, leafen.NotionAdminErrorValidation,
			"Releasing a legal hold takes no query parameters and no body.", nil,
		)
	}
	legalHoldID, err := normalizeNotionAdminLegalHoldID(rawLegalHoldID)
	if err != nil {
		return leafen.NotionAdminAPIError(
			event, http.StatusBadRequest, leafen.NotionAdminErrorValidation,
			leafen.NotionAdminValidationReason(err), err,
		)
	}
	if !identity.CanAccessLegalHold(legalHoldID) {
		return leafen.NotionAdminAPIError(
			event, http.StatusNotFound, leafen.NotionAdminErrorNotFound,
			leafen.NotionAdminNotFoundMessage, nil,
		)
	}
	response, err := releaseNotionAdminLegalHold(
		event.Request.Context(), event.App, secret, identity, legalHoldID,
	)
	if err != nil {
		return notionAdminLegalHoldReleaseError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

func releaseNotionAdminLegalHold(
	ctx context.Context,
	app core.App,
	secret string,
	expected leafen.NotionAdminIdentity,
	legalHoldID string,
) (notionAdminLegalHoldResponse, error) {
	response := notionAdminLegalHoldResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		current, err := leafen.AuthenticateNotionAdminToken(ctx, txApp, secret, time.Now().UTC())
		if err != nil {
			return err
		}
		if current.OrganizationID != expected.OrganizationID ||
			current.TokenID != expected.TokenID || current.CreatedBy != expected.CreatedBy ||
			!current.HasCapability(leafen.NotionAdminCapabilityLegalHoldWriteHighImpact) {
			return errNotionAdminLegalHoldForbidden
		}
		if !current.CanAccessLegalHold(legalHoldID) {
			return errLegalHoldNotFound
		}
		released, _, err := releaseLegalHoldInTransaction(
			txApp,
			legalHoldReleaseAuthority{
				OrganizationID: current.OrganizationID,
				ReleaseActor:   current.CreatedBy,
				AuditActor:     current.CreatedBy,
				AuditAction:    "notion_admin.legal_hold.release",
				AllowReleased:  true,
			},
			legalHoldID,
			time.Now().UTC().UnixMilli(),
		)
		if err != nil {
			return err
		}
		rows, err := loadStoredLegalHolds(
			txApp, current.OrganizationID, "", 0, legalHoldID,
		)
		if err != nil {
			return err
		}
		if len(rows) != 1 {
			return errLegalHoldNotFound
		}
		workspaceIDs := []string{}
		_, adminCreated, err := notionAdminLegalHoldMetadataFromStored(rows[0])
		if err != nil {
			return err
		}
		if adminCreated {
			workspaceIDs, err = loadNotionAdminWorkspaceIDs(ctx, txApp, current)
			if err != nil {
				return err
			}
		}
		response, err = notionAdminLegalHoldProjection(
			released, rows[0], current, workspaceIDs,
		)
		return err
	})
	return response, err
}

func notionAdminLegalHoldReleaseError(event *core.RequestEvent, err error) error {
	switch {
	case errors.Is(err, leafen.ErrNotionAdminTokenUnauthorized):
		return leafen.NotionAdminAPIError(
			event, http.StatusUnauthorized, leafen.NotionAdminErrorUnauthorized,
			leafen.NotionAdminUnauthorizedMessage, err,
		)
	case errors.Is(err, errNotionAdminLegalHoldForbidden):
		return leafen.NotionAdminAPIError(
			event, http.StatusForbidden, leafen.NotionAdminErrorForbidden,
			leafen.NotionAdminRefusalReason(err, leafen.NotionAdminForbiddenMessage), err,
		)
	case errors.Is(err, errLegalHoldNotFound):
		return leafen.NotionAdminAPIError(
			event, http.StatusNotFound, leafen.NotionAdminErrorNotFound,
			leafen.NotionAdminNotFoundMessage, err,
		)
	case errors.Is(err, errLegalHoldInvalid):
		return leafen.NotionAdminAPIError(
			event, http.StatusBadRequest, leafen.NotionAdminErrorValidation,
			leafen.NotionAdminValidationReason(err), err,
		)
	default:
		return leafen.NotionAdminAPIError(
			event, http.StatusInternalServerError, leafen.NotionAdminErrorInternal,
			leafen.NotionAdminInternalErrorMessage, err,
		)
	}
}
