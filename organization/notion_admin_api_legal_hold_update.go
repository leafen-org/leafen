package organization

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type notionAdminOptionalString struct {
	Present bool
	Value   string
}

func (field *notionAdminOptionalString) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return leafen.NotionAdminInvalid(errLegalHoldInvalid, "A field can't be null; leave it out to keep it.")
	}
	value := ""
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	field.Present, field.Value = true, value
	return nil
}

type notionAdminLegalHoldUpdateRequest struct {
	Name        notionAdminOptionalString `json:"name"`
	Description notionAdminOptionalString `json:"description"`
	Icon        notionAdminOptionalString `json:"icon"`
}

type normalizedNotionAdminLegalHoldUpdate struct {
	Name        notionAdminOptionalString
	Description notionAdminOptionalString
	Icon        notionAdminOptionalString
}

func serveNotionAdminLegalHoldUpdate(
	event *core.RequestEvent,
	rawLegalHoldID string,
) error {
	secret := leafen.BearerToken(event.Request)
	release := leafen.AcquireNotionAdminTokenLifecycleLock(leafen.NotionAdminTokenHash(secret))
	defer release()
	identity, authorized := leafen.AuthorizeNotionAdminAPI(
		event, secret, leafen.NotionAdminCapabilityLegalHoldWrite,
	)
	if !authorized {
		return nil
	}
	if len(event.Request.URL.Query()) != 0 {
		return leafen.NotionAdminAPIError(
			event, http.StatusBadRequest, leafen.NotionAdminErrorValidation,
			"This request takes no query parameters.", nil,
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
	event.Request.Body = http.MaxBytesReader(
		event.Response, event.Request.Body, notionAdminLegalHoldMaxBodyBytes,
	)
	if event.Request.ContentLength == 0 {
		return leafen.NotionAdminAPIError(
			event, http.StatusBadRequest, leafen.NotionAdminErrorValidation,
			"The body is empty: send the fields to change.", nil,
		)
	}
	request := notionAdminLegalHoldUpdateRequest{}
	if err := event.BindBody(&request); err != nil {
		return leafen.NotionAdminAPIError(
			event, http.StatusBadRequest, leafen.NotionAdminErrorValidation,
			leafen.NotionAdminValidationReason(err), err,
		)
	}
	normalized, err := normalizeNotionAdminLegalHoldUpdate(request)
	if err != nil {
		return notionAdminLegalHoldUpdateError(event, err)
	}
	response, err := updateNotionAdminLegalHold(
		event.Request.Context(), event.App, secret, identity, legalHoldID, normalized,
	)
	if err != nil {
		return notionAdminLegalHoldUpdateError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

func normalizeNotionAdminLegalHoldUpdate(
	request notionAdminLegalHoldUpdateRequest,
) (normalizedNotionAdminLegalHoldUpdate, error) {
	if request.Name.Present {
		request.Name.Value = strings.TrimSpace(request.Name.Value)
		if request.Name.Value == "" ||
			utf8.RuneCountInString(request.Name.Value) > leafen.LegalHoldMaxNameRunes {
			return normalizedNotionAdminLegalHoldUpdate{}, leafen.NotionAdminInvalid(errLegalHoldInvalid, fmt.Sprintf("name must be non-empty and at most %d characters.", leafen.LegalHoldMaxNameRunes))
		}
	}
	if request.Description.Present &&
		utf8.RuneCountInString(request.Description.Value) >
			leafen.NotionAdminLegalHoldMaxDescriptionRunes {
		return normalizedNotionAdminLegalHoldUpdate{}, leafen.NotionAdminInvalid(errLegalHoldInvalid, fmt.Sprintf("description is up to %d characters.", leafen.NotionAdminLegalHoldMaxDescriptionRunes))
	}
	if request.Icon.Present &&
		utf8.RuneCountInString(request.Icon.Value) > leafen.NotionAdminLegalHoldMaxIconRunes {
		return normalizedNotionAdminLegalHoldUpdate{}, leafen.NotionAdminInvalid(errLegalHoldInvalid, fmt.Sprintf("icon is up to %d characters.", leafen.NotionAdminLegalHoldMaxIconRunes))
	}
	return normalizedNotionAdminLegalHoldUpdate{
		Name: request.Name, Description: request.Description, Icon: request.Icon,
	}, nil
}

func updateNotionAdminLegalHold(
	ctx context.Context,
	app core.App,
	secret string,
	expected leafen.NotionAdminIdentity,
	legalHoldID string,
	update normalizedNotionAdminLegalHoldUpdate,
) (notionAdminLegalHoldResponse, error) {
	response := notionAdminLegalHoldResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		current, err := leafen.AuthenticateNotionAdminToken(ctx, txApp, secret, time.Now().UTC())
		if err != nil {
			return err
		}
		if current.OrganizationID != expected.OrganizationID ||
			current.TokenID != expected.TokenID || current.CreatedBy != expected.CreatedBy ||
			!current.HasCapability(leafen.NotionAdminCapabilityLegalHoldWrite) {
			return errNotionAdminLegalHoldForbidden
		}
		if !current.CanAccessLegalHold(legalHoldID) {
			return errLegalHoldNotFound
		}
		changed, err := applyNotionAdminLegalHoldUpdate(
			txApp, current.OrganizationID, legalHoldID, update,
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
		items, err := hydrateLegalHolds(txApp, rows)
		if err != nil {
			return err
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
		if changed {
			now := time.Now().UTC().UnixMilli()
			if err := leafen.BumpOrganizationPolicy(txApp, current.OrganizationID, now); err != nil {
				return errors.Join(leafen.ErrLegalHoldUnavailable, err)
			}
			if err := recordLegalHoldAudit(
				txApp, current.OrganizationID, current.CreatedBy, "notion_admin.legal_hold.update", legalHoldID, now,
			); err != nil {
				return errors.Join(leafen.ErrLegalHoldUnavailable, err)
			}
		}
		response, err = notionAdminLegalHoldProjection(
			items[0], rows[0], current, workspaceIDs,
		)
		return err
	})
	return response, err
}

func applyNotionAdminLegalHoldUpdate(
	app core.App,
	organizationID, legalHoldID string,
	update normalizedNotionAdminLegalHoldUpdate,
) (bool, error) {
	if !update.Name.Present && !update.Description.Present && !update.Icon.Present {
		return false, nil
	}
	params := dbx.Params{
		"organizationId": organizationID,
		"legalHoldId":    legalHoldID,
		"hasName":        booleanInt(update.Name.Present),
		"name":           update.Name.Value,
		"hasDescription": booleanInt(update.Description.Present),
		"description":    update.Description.Value,
		"hasIcon":        booleanInt(update.Icon.Present),
		"icon":           update.Icon.Value,
	}
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_organization_legal_holds}}
		SET [[name]] = CASE WHEN {:hasName} = 1 THEN {:name} ELSE [[name]] END,
			[[notion_admin_description]] = CASE
				WHEN {:hasDescription} = 1 AND [[notion_admin_version]] = 1
				THEN {:description} ELSE [[notion_admin_description]] END,
			[[notion_admin_native_description]] = CASE
				WHEN {:hasDescription} = 1 AND [[notion_admin_version]] = 0
				THEN {:description} ELSE [[notion_admin_native_description]] END,
			[[notion_admin_icon]] = CASE
				WHEN {:hasIcon} = 1 AND [[notion_admin_version]] = 1
				THEN {:icon} ELSE [[notion_admin_icon]] END,
			[[notion_admin_native_icon]] = CASE
				WHEN {:hasIcon} = 1 AND [[notion_admin_version]] = 0
				THEN {:icon} ELSE [[notion_admin_native_icon]] END
		WHERE [[organization_id]] = {:organizationId} AND [[id]] = {:legalHoldId}
			AND (
				({:hasName} = 1 AND [[name]] IS NOT {:name})
				OR ({:hasDescription} = 1 AND (
					([[notion_admin_version]] = 1
						AND [[notion_admin_description]] IS NOT {:description})
					OR ([[notion_admin_version]] = 0
						AND [[notion_admin_native_description]] IS NOT {:description})))
				OR ({:hasIcon} = 1 AND (
					([[notion_admin_version]] = 1
						AND [[notion_admin_icon]] IS NOT {:icon})
					OR ([[notion_admin_version]] = 0
						AND [[notion_admin_native_icon]] IS NOT {:icon})))
			)
	`).Bind(params).Execute()
	if err != nil {
		return false, errors.Join(leafen.ErrLegalHoldUnavailable, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, errors.Join(leafen.ErrLegalHoldUnavailable, err)
	}
	if affected > 1 {
		return false, leafen.ErrLegalHoldUnavailable
	}
	return affected == 1, nil
}

func booleanInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func notionAdminLegalHoldUpdateError(event *core.RequestEvent, err error) error {
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
