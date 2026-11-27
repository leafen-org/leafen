package organization

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/pocketbase/core"
)

const notionAdminManagedUserSessionMaxBodyBytes = 64 << 10

var (
	errNotionAdminManagedUserSessionInvalid   = errors.New("invalid Notion Admin managed-user session request")
	errNotionAdminManagedUserSessionForbidden = leafen.NotionAdminInvalid(errors.New("Notion Admin managed-user session authority is forbidden"), leafen.NotionAdminScopeChanged)
)

type notionAdminManagedUserSessionRequest struct {
	User json.RawMessage `json:"user"`
}

type notionAdminManagedUserSelector struct {
	Type  string  `json:"type"`
	Email *string `json:"email"`
	ID    *string `json:"id"`
}

func serveNotionAdminManagedUserSessionRevoke(event *core.RequestEvent) error {
	secret := leafen.BearerToken(event.Request)
	release := leafen.AcquireNotionAdminTokenLifecycleLock(leafen.NotionAdminTokenHash(secret))
	defer release()
	identity, authorized := leafen.AuthorizeNotionAdminAPI(
		event, secret, leafen.NotionAdminCapabilityManagedUserSessionWrite,
	)
	if !authorized {
		return nil
	}
	selector, err := parseNotionAdminManagedUserSessionRequest(event)
	if err != nil {
		return notionAdminManagedUserSessionError(event, err)
	}
	if err := revokeNotionAdminManagedUserSessions(
		event.Request.Context(), event.App, secret, identity, selector,
	); err != nil {
		return notionAdminManagedUserSessionError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, map[string]string{"status": "revoked"})
}

func parseNotionAdminManagedUserSessionRequest(
	event *core.RequestEvent,
) (leafen.NormalizedNotionAdminManagedUserSelector, error) {
	if len(event.Request.URL.Query()) != 0 || event.Request.ContentLength == 0 {
		return leafen.NormalizedNotionAdminManagedUserSelector{}, leafen.NotionAdminInvalid(errNotionAdminManagedUserSessionInvalid, "This request takes a JSON body and no query parameters.")
	}
	contentType, _, err := mime.ParseMediaType(event.Request.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		return leafen.NormalizedNotionAdminManagedUserSelector{}, leafen.NotionAdminInvalid(errNotionAdminManagedUserSessionInvalid, "The body must be application/json.")
	}
	event.Request.Body = http.MaxBytesReader(
		event.Response, event.Request.Body, notionAdminManagedUserSessionMaxBodyBytes,
	)
	decoder := json.NewDecoder(event.Request.Body)
	request := notionAdminManagedUserSessionRequest{}
	if err := decoder.Decode(&request); err != nil {
		return leafen.NormalizedNotionAdminManagedUserSelector{}, leafen.NotionAdminInvalid(errNotionAdminManagedUserSessionInvalid, leafen.NotionAdminValidationReason(err))
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return leafen.NormalizedNotionAdminManagedUserSelector{}, leafen.NotionAdminInvalid(errNotionAdminManagedUserSessionInvalid, "The body holds more than one JSON value.")
	}
	selectorDecoder := json.NewDecoder(strings.NewReader(string(request.User)))
	selectorDecoder.DisallowUnknownFields()
	selector := notionAdminManagedUserSelector{}
	if len(request.User) == 0 || selectorDecoder.Decode(&selector) != nil {
		return leafen.NormalizedNotionAdminManagedUserSelector{}, leafen.NotionAdminInvalid(errNotionAdminManagedUserSessionInvalid, "user must be {\"type\": \"email\", \"email\": …} or {\"type\": \"id\", \"id\": …}.")
	}
	if err := selectorDecoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return leafen.NormalizedNotionAdminManagedUserSelector{}, leafen.NotionAdminInvalid(errNotionAdminManagedUserSessionInvalid, "user holds more than one JSON value.")
	}
	return normalizeNotionAdminManagedUserSelector(selector)
}

func normalizeNotionAdminManagedUserSelector(
	selector notionAdminManagedUserSelector,
) (leafen.NormalizedNotionAdminManagedUserSelector, error) {
	switch selector.Type {
	case "email":
		if selector.Email == nil || selector.ID != nil {
			break
		}
		email, valid := leafen.NormalizeEmailIdentity(*selector.Email)
		if valid {
			return leafen.NormalizedNotionAdminManagedUserSelector{Email: email}, nil
		}
	case "id":
		if selector.ID == nil || selector.Email != nil {
			break
		}
		internalID, err := leafen.ResolveNotionCompatibleUserID(*selector.ID)
		if err == nil {
			return leafen.NormalizedNotionAdminManagedUserSelector{InternalUserID: internalID}, nil
		}
	}
	return leafen.NormalizedNotionAdminManagedUserSelector{}, leafen.NotionAdminInvalid(errNotionAdminManagedUserSessionInvalid, "user must be {\"type\": \"email\"} with a valid email, or {\"type\": \"id\"} with a user ID, not both.")
}

func revokeNotionAdminManagedUserSessions(
	ctx context.Context,
	app core.App,
	secret string,
	expected leafen.NotionAdminIdentity,
	selector leafen.NormalizedNotionAdminManagedUserSelector,
) error {
	return app.RunInTransaction(func(txApp core.App) error {
		current, err := leafen.AuthenticateNotionAdminToken(ctx, txApp, secret, time.Now().UTC())
		if err != nil {
			return err
		}
		if current.OrganizationID != expected.OrganizationID ||
			current.TokenID != expected.TokenID || current.CreatedBy != expected.CreatedBy ||
			!current.HasCapability(leafen.NotionAdminCapabilityManagedUserSessionWrite) {
			return errNotionAdminManagedUserSessionForbidden
		}
		userID, err := leafen.ResolveNotionAdminManagedUser(
			ctx, txApp, current.OrganizationID, selector,
		)
		if err != nil {
			return err
		}
		changed, err := leafen.RevokeAllBrowserSessionsCount(txApp, userID)
		if err != nil {
			return errors.Join(leafen.ErrNotionAdminManagedUserSessionUnavailable, err)
		}
		if changed > 0 {
			if err := leafen.RecordOrganizationAuditEvent(txApp, leafen.OrganizationAuditEvent{
				ID:             core.GenerateDefaultRandomId(),
				OrganizationID: current.OrganizationID,
				ActorID:        current.CreatedBy,
				Action:         "notion_admin.managed_user.session.revoke",
				TargetType:     "user",
				TargetID:       userID,
				Metadata: map[string]any{
					"admin_token_id": current.TokenID,
					"revoked_count":  changed,
				},
			}); err != nil {
				return errors.Join(leafen.ErrNotionAdminManagedUserSessionUnavailable, err)
			}
		}
		return nil
	})
}

func notionAdminManagedUserSessionError(event *core.RequestEvent, err error) error {
	switch {
	case errors.Is(err, errNotionAdminManagedUserSessionInvalid):
		return leafen.NotionAdminAPIError(
			event, http.StatusBadRequest, leafen.NotionAdminErrorValidation,
			leafen.NotionAdminValidationReason(err), err,
		)
	case errors.Is(err, leafen.ErrNotionAdminTokenUnauthorized):
		return leafen.NotionAdminAPIError(
			event, http.StatusUnauthorized, leafen.NotionAdminErrorUnauthorized,
			leafen.NotionAdminUnauthorizedMessage, err,
		)
	case errors.Is(err, errNotionAdminManagedUserSessionForbidden):
		return leafen.NotionAdminAPIError(
			event, http.StatusForbidden, leafen.NotionAdminErrorForbidden,
			leafen.NotionAdminRefusalReason(err, leafen.NotionAdminForbiddenMessage), err,
		)
	case errors.Is(err, leafen.ErrNotionAdminManagedUserSessionNotFound):
		return leafen.NotionAdminAPIError(
			event, http.StatusNotFound, leafen.NotionAdminErrorNotFound,
			leafen.NotionAdminNotFoundMessage, err,
		)
	default:
		return leafen.NotionAdminAPIError(
			event, http.StatusInternalServerError, leafen.NotionAdminErrorInternal,
			leafen.NotionAdminInternalErrorMessage, err,
		)
	}
}
