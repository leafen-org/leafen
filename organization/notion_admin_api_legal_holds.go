package organization

import (
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/pocketbase/core"
)

var notionAdminLegalHoldInteractionTypes = []string{
	"page.created",
	"page.edited",
	"page.viewed",
}

type notionAdminLegalHoldIdentitySet struct {
	IDs   []string `json:"ids"`
	Total int      `json:"total"`
}

type notionAdminLegalHoldResponse struct {
	CreatedBy           string                          `json:"created_by"`
	CreatedTime         int64                           `json:"created_time"`
	ID                  string                          `json:"id"`
	StartDate           float64                         `json:"start_date"`
	Status              string                          `json:"status"`
	UserInteractionType []string                        `json:"user_interaction_type"`
	Users               notionAdminLegalHoldIdentitySet `json:"users"`
	Workspaces          notionAdminLegalHoldIdentitySet `json:"workspaces"`
	Description         *string                         `json:"description,omitzero"`
	EndDate             *float64                        `json:"end_date,omitzero"`
	Icon                *string                         `json:"icon,omitzero"`
	Name                string                          `json:"name,omitempty"`
}

func serveNotionAdminLegalHoldRetrieve(
	event *core.RequestEvent,
	rawLegalHoldID string,
) error {
	secret := leafen.BearerToken(event.Request)
	release := leafen.AcquireNotionAdminTokenLifecycleLock(leafen.NotionAdminTokenHash(secret))
	defer release()
	identity, authorized := leafen.AuthorizeNotionAdminAPI(
		event, secret, leafen.NotionAdminCapabilityLegalHoldRead,
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
	response, err := getNotionAdminLegalHold(
		event.Request.Context(), event.App, identity, legalHoldID,
	)
	if errors.Is(err, errLegalHoldNotFound) {
		return leafen.NotionAdminAPIError(
			event, http.StatusNotFound, leafen.NotionAdminErrorNotFound,
			leafen.NotionAdminNotFoundMessage, err,
		)
	}
	if err != nil {
		return leafen.NotionAdminAPIError(
			event, http.StatusInternalServerError, leafen.NotionAdminErrorInternal,
			leafen.NotionAdminInternalErrorMessage, err,
		)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

func normalizeNotionAdminLegalHoldID(value string) (string, error) {
	if value == "" || value != strings.TrimSpace(value) {
		return "", leafen.NotionAdminInvalid(errLegalHoldInvalid, "The legal hold ID should be a UUID.")
	}
	compact := value
	if len(value) == 36 {
		if value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
			return "", leafen.NotionAdminInvalid(errLegalHoldInvalid, "The legal hold ID should be a UUID.")
		}
		compact = strings.ReplaceAll(value, "-", "")
	} else if len(value) != 32 {
		return "", leafen.NotionAdminInvalid(errLegalHoldInvalid, "The legal hold ID should be a UUID.")
	}
	decoded, err := hex.DecodeString(compact)
	if err != nil || len(decoded) != 16 {
		return "", leafen.NotionAdminInvalid(errLegalHoldInvalid, "The legal hold ID should be a UUID.")
	}
	return strings.ToLower(compact), nil
}

func serveNotionAdminLegalHoldCreate(event *core.RequestEvent) error {
	secret := leafen.BearerToken(event.Request)
	release := leafen.AcquireNotionAdminTokenLifecycleLock(leafen.NotionAdminTokenHash(secret))
	defer release()
	identity, authorized := leafen.AuthorizeNotionAdminAPI(
		event, secret, leafen.NotionAdminCapabilityLegalHoldWrite,
	)
	if !authorized {
		return nil
	}
	if !notionAdminLegalHoldCreateScope(identity) {
		return leafen.NotionAdminAPIError(
			event, http.StatusForbidden, leafen.NotionAdminErrorForbidden,
			"Creating a legal hold needs a token for all workspaces and all legal holds.", nil,
		)
	}
	if len(event.Request.URL.Query()) != 0 {
		return leafen.NotionAdminAPIError(
			event, http.StatusBadRequest, leafen.NotionAdminErrorValidation,
			"This request takes no query parameters.", nil,
		)
	}
	event.Request.Body = http.MaxBytesReader(
		event.Response, event.Request.Body, notionAdminLegalHoldMaxBodyBytes,
	)
	request := notionAdminLegalHoldCreateRequest{}
	if err := event.BindBody(&request); err != nil {
		if tooLarge, answer := notionAdminLegalHoldBodyTooLarge(event, err); tooLarge {
			return answer
		}
		return leafen.NotionAdminAPIError(
			event, http.StatusBadRequest, leafen.NotionAdminErrorValidation,
			leafen.NotionAdminValidationReason(err), err,
		)
	}
	normalized, err := normalizeNotionAdminLegalHoldCreate(
		request, event.Request.Header.Values("Idempotency-Key"), identity,
	)
	if err != nil {
		return notionAdminLegalHoldCreateError(event, err)
	}
	response, err := createNotionAdminLegalHold(
		event.Request.Context(), event.App, secret, identity, normalized,
	)
	if err != nil {
		return notionAdminLegalHoldCreateError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

func serveNotionAdminLegalHoldList(event *core.RequestEvent) error {
	secret := leafen.BearerToken(event.Request)
	release := leafen.AcquireNotionAdminTokenLifecycleLock(leafen.NotionAdminTokenHash(secret))
	defer release()
	identity, authorized := leafen.AuthorizeNotionAdminAPI(
		event,
		secret,
		leafen.NotionAdminCapabilityLegalHoldRead,
	)
	if !authorized {
		return nil
	}

	rawCursor, publicUserID, valid := notionAdminLegalHoldQuery(event)
	if !valid {
		return leafen.NotionAdminAPIError(
			event, http.StatusBadRequest, leafen.NotionAdminErrorValidation,
			"The query takes start_cursor and user_id, each once and not empty.", nil,
		)
	}
	userID := ""
	if publicUserID != "" {
		resolved, err := leafen.ResolveNotionUserIdentityFilter(publicUserID)
		if err != nil {
			return leafen.NotionAdminAPIError(
				event, http.StatusBadRequest, leafen.NotionAdminErrorValidation,
				leafen.NotionAdminValidationReason(err), err,
			)
		}
		userID = resolved
	}
	cursor, err := decodeNotionAdminLegalHoldCursor(rawCursor, secret, identity, userID)
	if err != nil {
		return leafen.NotionAdminAPIError(
			event, http.StatusBadRequest, leafen.NotionAdminErrorValidation,
			leafen.NotionAdminValidationReason(err), err,
		)
	}
	page, err := listNotionAdminLegalHolds(
		event.Request.Context(), event.App, identity, userID, cursor,
	)
	if err != nil {
		return leafen.NotionAdminAPIError(
			event, http.StatusInternalServerError, leafen.NotionAdminErrorInternal,
			leafen.NotionAdminInternalErrorMessage, err,
		)
	}
	items := make([]notionAdminLegalHoldResponse, 0, len(page.Items))
	for index, hold := range page.Items {
		item, err := notionAdminLegalHoldProjection(
			hold, page.Rows[index], identity, page.WorkspaceIDs,
		)
		if err != nil {
			return leafen.NotionAdminAPIError(
				event, http.StatusInternalServerError, leafen.NotionAdminErrorInternal,
				leafen.NotionAdminInternalErrorMessage, err,
			)
		}
		items = append(items, item)
	}
	response := map[string]any{"legal_holds": items}
	if page.HasMore {
		cursor, err := encodeNotionAdminLegalHoldCursor(
			secret, identity, userID, page.Rows[len(page.Rows)-1],
		)
		if err != nil {
			return leafen.NotionAdminAPIError(
				event, http.StatusInternalServerError, leafen.NotionAdminErrorInternal,
				leafen.NotionAdminInternalErrorMessage, err,
			)
		}
		response["next_cursor"] = cursor
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

func notionAdminLegalHoldQuery(event *core.RequestEvent) (string, string, bool) {
	query := event.Request.URL.Query()
	for key, values := range query {
		if key != "start_cursor" && key != "user_id" || len(values) != 1 {
			return "", "", false
		}
	}
	rawCursor := query.Get("start_cursor")
	publicUserID := query.Get("user_id")
	if values, present := query["start_cursor"]; present && values[0] == "" {
		return "", "", false
	}
	if values, present := query["user_id"]; present && values[0] == "" {
		return "", "", false
	}
	return rawCursor, publicUserID, true
}

func notionAdminLegalHoldProjection(
	hold leafen.LegalHoldResponse,
	stored storedLegalHold,
	identity leafen.NotionAdminIdentity,
	adminWorkspaceIDs []string,
) (notionAdminLegalHoldResponse, error) {
	if stored.ID != hold.ID || stored.OrganizationID != hold.OrganizationID {
		return notionAdminLegalHoldResponse{}, leafen.ErrLegalHoldUnavailable
	}
	createdBy, err := leafen.ProjectNotionUserIdentity(hold.CreatedBy)
	if err != nil {
		return notionAdminLegalHoldResponse{}, err
	}
	users := make([]string, 0, len(hold.Scope.CustodianUserIDs))
	for _, userID := range hold.Scope.CustodianUserIDs {
		publicID, err := leafen.ProjectNotionUserIdentity(userID)
		if err != nil {
			return notionAdminLegalHoldResponse{}, err
		}
		users = append(users, publicID)
	}
	metadata, adminCreated, err := notionAdminLegalHoldMetadataFromStored(stored)
	if err != nil {
		return notionAdminLegalHoldResponse{}, err
	}
	nativeOverride, err := notionAdminLegalHoldNativeOverrideFromStored(stored)
	if err != nil {
		return notionAdminLegalHoldResponse{}, err
	}
	interactions := append([]string(nil), notionAdminLegalHoldInteractionTypes...)
	startDate := float64(hold.CreatedAt)
	description := hold.Reason
	responseDescription := &description
	var endDate *float64
	var icon *string
	workspaces := append([]string(nil), adminWorkspaceIDs...)
	if adminCreated {
		interactions, err = notionAdminLegalHoldInteractions(metadata.InteractionMask)
		if err != nil {
			return notionAdminLegalHoldResponse{}, err
		}
		startDate, responseDescription = metadata.StartDate, metadata.Description
		endDate, icon = metadata.EndDate, metadata.Icon
	} else {
		if nativeOverride.Description != nil {
			responseDescription = nativeOverride.Description
		}
		icon = nativeOverride.Icon
		workspaces = make([]string, 0, len(hold.Scope.WorkspaceIDs))
		for _, workspaceID := range hold.Scope.WorkspaceIDs {
			if !identity.CanAccessWorkspace(workspaceID) {
				continue
			}
			publicID, err := leafen.NotionCompatibleWorkspaceID(workspaceID)
			if err != nil {
				return notionAdminLegalHoldResponse{}, err
			}
			workspaces = append(workspaces, publicID)
		}
		if hold.Status == leafen.LegalHoldStatusReleased {
			value := float64(hold.ReleasedAt)
			endDate = &value
		}
	}
	response := notionAdminLegalHoldResponse{
		CreatedBy:           createdBy,
		CreatedTime:         hold.CreatedAt,
		ID:                  hold.ID,
		StartDate:           startDate,
		Status:              hold.Status,
		UserInteractionType: interactions,
		Users:               notionAdminLegalHoldIdentitySet{IDs: users, Total: len(users)},
		Workspaces:          notionAdminLegalHoldIdentitySet{IDs: workspaces, Total: len(workspaces)},
		Description:         responseDescription,
		EndDate:             endDate,
		Icon:                icon,
		Name:                strings.TrimSpace(hold.Name),
	}
	return response, nil
}
