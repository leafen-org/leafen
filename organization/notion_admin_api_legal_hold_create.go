package organization

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/pocketbase/core"
)

const (
	// A request as large as Notion's (500 KB); past it, refused saying so.
	notionAdminLegalHoldMaxBodyBytes = 500 << 10
)

var errNotionAdminLegalHoldForbidden = leafen.NotionAdminInvalid(errors.New("Notion Admin legal hold authority is forbidden"), leafen.NotionAdminScopeChanged)

type notionAdminLegalHoldCreateRequest struct {
	Name                string    `json:"name"`
	StartDate           *float64  `json:"start_date"`
	UserIDs             *[]string `json:"user_ids"`
	UserInteractionType *[]string `json:"user_interaction_type"`
	Description         *string   `json:"description"`
	EndDate             *float64  `json:"end_date"`
	Icon                *string   `json:"icon"`
}

type normalizedNotionAdminLegalHoldCreate struct {
	Input       leafen.LegalHoldCreateInput
	Metadata    notionAdminLegalHoldMetadata
	RequestHash string
}

func normalizeNotionAdminLegalHoldCreate(
	request notionAdminLegalHoldCreateRequest,
	idempotencyHeaders []string,
	identity leafen.NotionAdminIdentity,
) (normalizedNotionAdminLegalHoldCreate, error) {
	name := strings.TrimSpace(request.Name)
	if name == "" || utf8.RuneCountInString(name) > leafen.LegalHoldMaxNameRunes ||
		request.StartDate == nil || !finiteNotionAdminNumber(*request.StartDate) ||
		request.UserIDs == nil || request.UserInteractionType == nil {
		return normalizedNotionAdminLegalHoldCreate{}, leafen.NotionAdminInvalid(errLegalHoldInvalid, fmt.Sprintf("name (up to %d characters), start_date, user_ids and user_interaction_type are required.", leafen.LegalHoldMaxNameRunes))
	}
	if request.EndDate != nil && (!finiteNotionAdminNumber(*request.EndDate) ||
		*request.EndDate < *request.StartDate) {
		return normalizedNotionAdminLegalHoldCreate{}, leafen.NotionAdminInvalid(errLegalHoldInvalid, "end_date must be a number not before start_date.")
	}
	if request.Description != nil &&
		utf8.RuneCountInString(*request.Description) > leafen.NotionAdminLegalHoldMaxDescriptionRunes {
		return normalizedNotionAdminLegalHoldCreate{}, leafen.NotionAdminInvalid(errLegalHoldInvalid, fmt.Sprintf("description is up to %d characters.", leafen.NotionAdminLegalHoldMaxDescriptionRunes))
	}
	if request.Icon != nil && utf8.RuneCountInString(*request.Icon) > leafen.NotionAdminLegalHoldMaxIconRunes {
		return normalizedNotionAdminLegalHoldCreate{}, leafen.NotionAdminInvalid(errLegalHoldInvalid, fmt.Sprintf("icon is up to %d characters.", leafen.NotionAdminLegalHoldMaxIconRunes))
	}

	interactions := append([]string(nil), (*request.UserInteractionType)...)
	mask, valid := notionAdminLegalHoldInteractionMask(interactions)
	if !valid {
		return normalizedNotionAdminLegalHoldCreate{}, leafen.NotionAdminInvalid(errLegalHoldInvalid, "user_interaction_type takes page.created, page.edited and page.viewed, at least one.")
	}
	interactions, err := notionAdminLegalHoldInteractions(mask)
	if err != nil {
		return normalizedNotionAdminLegalHoldCreate{}, err
	}
	publicUsers := append([]string(nil), (*request.UserIDs)...)
	internalUsers := make([]string, 0, len(publicUsers))
	seenUsers := make(map[string]struct{}, len(publicUsers))
	for _, publicID := range publicUsers {
		internalID, err := leafen.ResolveNotionUserIdentityFilter(publicID)
		if err != nil {
			return normalizedNotionAdminLegalHoldCreate{}, leafen.NotionAdminInvalid(errLegalHoldInvalid, "user_ids has an entry that isn't a user ID.")
		}
		if _, duplicate := seenUsers[internalID]; duplicate {
			continue
		}
		seenUsers[internalID] = struct{}{}
		internalUsers = append(internalUsers, internalID)
	}
	sort.Strings(internalUsers)

	mutationID, err := notionAdminLegalHoldMutationID(idempotencyHeaders)
	if err != nil {
		return normalizedNotionAdminLegalHoldCreate{}, err
	}
	reason := "Notion Admin API legal hold"
	if request.Description != nil && strings.TrimSpace(*request.Description) != "" {
		reason = strings.TrimSpace(*request.Description)
	}
	input := leafen.LegalHoldCreateInput{
		MutationID: mutationID, Name: name, Reason: reason,
		Scope: leafen.LegalHoldScopeInput{CustodianUserIDs: internalUsers},
	}
	metadata := notionAdminLegalHoldMetadata{
		Version:         leafen.NotionAdminLegalHoldMetadataVersion,
		StartDate:       *request.StartDate,
		EndDate:         request.EndDate,
		InteractionMask: mask,
		Description:     request.Description,
		Icon:            request.Icon,
		TokenID:         identity.TokenID,
	}
	requestHash, err := leafen.HashNotionAdminTokenRequest(struct {
		Name                string   `json:"name"`
		StartDate           float64  `json:"start_date"`
		UserIDs             []string `json:"user_ids"`
		UserInteractionType []string `json:"user_interaction_type"`
		Description         *string  `json:"description,omitempty"`
		EndDate             *float64 `json:"end_date,omitempty"`
		Icon                *string  `json:"icon,omitempty"`
	}{name, *request.StartDate, internalUsers, interactions,
		request.Description, request.EndDate, request.Icon})
	if err != nil {
		return normalizedNotionAdminLegalHoldCreate{}, errLegalHoldInvalid
	}
	return normalizedNotionAdminLegalHoldCreate{
		Input: input, Metadata: metadata, RequestHash: requestHash,
	}, nil
}

func notionAdminLegalHoldMutationID(headers []string) (string, error) {
	if len(headers) == 0 {
		return core.GenerateDefaultRandomId(), nil
	}
	if len(headers) != 1 {
		return "", leafen.NotionAdminInvalid(errLegalHoldInvalid, "Send one Idempotency-Key header at most.")
	}
	value := strings.TrimSpace(headers[0])
	if value == "" || utf8.RuneCountInString(value) > leafen.LegalHoldMaxMutationIDRunes {
		return "", leafen.NotionAdminInvalid(errLegalHoldInvalid, fmt.Sprintf("Idempotency-Key must be non-empty and at most %d characters.", leafen.LegalHoldMaxMutationIDRunes))
	}
	return value, nil
}

func createNotionAdminLegalHold(
	ctx context.Context,
	app core.App,
	secret string,
	expected leafen.NotionAdminIdentity,
	normalized normalizedNotionAdminLegalHoldCreate,
) (notionAdminLegalHoldResponse, error) {
	response := notionAdminLegalHoldResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		current, err := leafen.AuthenticateNotionAdminToken(ctx, txApp, secret, time.Now().UTC())
		if err != nil {
			return err
		}
		if current.OrganizationID != expected.OrganizationID ||
			current.TokenID != expected.TokenID || current.CreatedBy != expected.CreatedBy ||
			!current.HasCapability(leafen.NotionAdminCapabilityLegalHoldWrite) ||
			!notionAdminLegalHoldCreateScope(current) {
			return errNotionAdminLegalHoldForbidden
		}
		created, err := createLegalHoldInTransaction(
			txApp,
			legalHoldCreateAuthority{
				OrganizationID: current.OrganizationID,
				MutationActor:  current.TokenID,
				CreatedBy:      current.CreatedBy,
				AuditActor:     current.CreatedBy,
				AuditAction:    "notion_admin.legal_hold.create",
			},
			normalized.Input,
			normalized.RequestHash,
			&normalized.Metadata,
		)
		if err != nil {
			return err
		}
		rows, err := loadStoredLegalHolds(
			txApp, current.OrganizationID, "", 0, created.ID,
		)
		if err != nil || len(rows) != 1 {
			return leafen.ErrLegalHoldUnavailable
		}
		workspaceIDs, err := loadNotionAdminWorkspaceIDs(ctx, txApp, current)
		if err != nil {
			return err
		}
		response, err = notionAdminLegalHoldProjection(
			created, rows[0], current, workspaceIDs,
		)
		return err
	})
	return response, err
}

func notionAdminLegalHoldCreateScope(identity leafen.NotionAdminIdentity) bool {
	return notionAdminIdentityHasOnlyWildcard(identity.WorkspaceIDs()) &&
		notionAdminIdentityHasOnlyWildcard(identity.LegalHoldIDs())
}

func notionAdminLegalHoldCreateError(event *core.RequestEvent, err error) error {
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
	case errors.Is(err, errLegalHoldTargetNotFound):
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

func finiteNotionAdminNumber(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

// notionAdminLegalHoldBodyTooLarge answers a body past the request limit as
// Notion's validation error, saying why; however many people a hold names,
// only the size of one request is bounded.
func notionAdminLegalHoldBodyTooLarge(event *core.RequestEvent, err error) (bool, error) {
	var tooLarge *http.MaxBytesError
	if !errors.As(err, &tooLarge) {
		return false, nil
	}
	return true, leafen.NotionAdminAPIError(event, http.StatusBadRequest, leafen.NotionAdminErrorValidation,
		"The request body is larger than 500 KB; send the people in more than one request.", err)
}
