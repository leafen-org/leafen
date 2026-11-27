package organization

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/pocketbase/core"
)

type notionAdminLegalHoldExportRequest struct {
	LegalHoldExportID *string `json:"legal_hold_export_id"`
	RequestingUserID  *string `json:"requesting_user_id"`
	SpaceID           *string `json:"space_id"`
}

type notionAdminLegalHoldExportResponse struct {
	LegalHoldExportID string `json:"legal_hold_export_id"`
}

func serveNotionAdminLegalHoldExport(
	event *core.RequestEvent,
	jobs *leafen.DurableJobRunner,
	rawLegalHoldID string,
) error {
	secret := leafen.BearerToken(event.Request)
	release := leafen.AcquireNotionAdminTokenLifecycleLock(leafen.NotionAdminTokenHash(secret))
	defer release()
	identity, authorized := leafen.AuthorizeNotionAdminAPI(
		event, secret, leafen.NotionAdminCapabilityLegalHoldExport,
	)
	if !authorized {
		return nil
	}
	legalHoldID, request, err := parseNotionAdminLegalHoldExportRequest(
		event, rawLegalHoldID,
	)
	if err != nil {
		return notionAdminLegalHoldExportError(event, err)
	}
	if !identity.CanAccessLegalHold(legalHoldID) {
		return notionAdminLegalHoldExportError(event, errLegalHoldNotFound)
	}
	requestingUserID, err := leafen.ResolveNotionUserIdentityFilter(*request.RequestingUserID)
	if err != nil {
		return notionAdminLegalHoldExportError(event, leafen.NotionAdminInvalid(errLegalHoldInvalid, "requesting_user_id isn't a user ID."))
	}

	exportID := ""
	if request.LegalHoldExportID != nil {
		exportID, err = normalizeNotionAdminLegalHoldID(*request.LegalHoldExportID)
		if err == nil {
			err = readNotionAdminLegalHoldExport(
				event.Request.Context(), event.App, secret, identity,
				legalHoldID, exportID, requestingUserID,
			)
		}
	} else {
		workspaceID, resolveErr := leafen.ResolveNotionCompatibleWorkspaceID(*request.SpaceID)
		if resolveErr != nil {
			err = errLegalHoldInvalid
		} else if !identity.CanAccessWorkspace(workspaceID) {
			err = errLegalHoldNotFound
		} else {
			exportID, err = admitNotionAdminLegalHoldExport(jobs,
				event.Request.Context(), secret, identity,
				legalHoldID, workspaceID, requestingUserID,
			)
		}
	}
	if err != nil {
		return notionAdminLegalHoldExportError(event, err)
	}
	publicExportID, err := formatNotionAdminLegalHoldExportID(exportID)
	if err != nil {
		return notionAdminLegalHoldExportError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, notionAdminLegalHoldExportResponse{
		LegalHoldExportID: publicExportID,
	})
}

func parseNotionAdminLegalHoldExportRequest(
	event *core.RequestEvent,
	rawLegalHoldID string,
) (string, notionAdminLegalHoldExportRequest, error) {
	if len(event.Request.URL.Query()) != 0 || event.Request.ContentLength == 0 {
		return "", notionAdminLegalHoldExportRequest{}, leafen.NotionAdminInvalid(errLegalHoldInvalid, "Exporting takes a JSON body and no query parameters.")
	}
	contentType, _, err := mime.ParseMediaType(event.Request.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		return "", notionAdminLegalHoldExportRequest{}, leafen.NotionAdminInvalid(errLegalHoldInvalid, "The body must be application/json.")
	}
	legalHoldID, err := normalizeNotionAdminLegalHoldID(rawLegalHoldID)
	if err != nil {
		return "", notionAdminLegalHoldExportRequest{}, leafen.NotionAdminInvalid(errLegalHoldInvalid, "The legal hold ID should be a UUID.")
	}
	event.Request.Body = http.MaxBytesReader(
		event.Response, event.Request.Body, notionAdminLegalHoldMaxBodyBytes,
	)
	decoder := json.NewDecoder(event.Request.Body)
	decoder.DisallowUnknownFields()
	request := notionAdminLegalHoldExportRequest{}
	if err := decoder.Decode(&request); err != nil {
		return "", notionAdminLegalHoldExportRequest{}, leafen.NotionAdminInvalid(errLegalHoldInvalid, leafen.NotionAdminValidationReason(err))
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return "", notionAdminLegalHoldExportRequest{}, leafen.NotionAdminInvalid(errLegalHoldInvalid, "The body holds more than one JSON value.")
	}
	hasExport := request.LegalHoldExportID != nil
	hasSpace := request.SpaceID != nil
	if request.RequestingUserID == nil || hasExport == hasSpace ||
		strings.TrimSpace(*request.RequestingUserID) != *request.RequestingUserID ||
		(hasExport && strings.TrimSpace(*request.LegalHoldExportID) != *request.LegalHoldExportID) ||
		(hasSpace && strings.TrimSpace(*request.SpaceID) != *request.SpaceID) {
		return "", notionAdminLegalHoldExportRequest{}, leafen.NotionAdminInvalid(errLegalHoldInvalid, "IDs in the body can't have spaces around them.")
	}
	return legalHoldID, request, nil
}

func formatNotionAdminLegalHoldExportID(value string) (string, error) {
	normalized, err := normalizeNotionAdminLegalHoldID(value)
	if err != nil || len(normalized) != 32 {
		return "", leafen.ErrLegalHoldUnavailable
	}
	return normalized[:8] + "-" + normalized[8:12] + "-" + normalized[12:16] + "-" +
		normalized[16:20] + "-" + normalized[20:], nil
}

func notionAdminLegalHoldExportError(event *core.RequestEvent, err error) error {
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
