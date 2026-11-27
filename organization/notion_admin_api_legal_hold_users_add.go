package organization

import (
	"net/http"
	"sort"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/pocketbase/core"
)

type notionAdminLegalHoldUsersAddRequest struct {
	UserIDs *[]string `json:"user_ids"`
}

func serveNotionAdminLegalHoldUsersAdd(
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
		return notionAdminLegalHoldUsersMutationError(event, leafen.NotionAdminInvalid(errLegalHoldInvalid, "This request takes no query parameters."))
	}
	legalHoldID, err := normalizeNotionAdminLegalHoldID(rawLegalHoldID)
	if err != nil {
		return notionAdminLegalHoldUsersMutationError(event, err)
	}
	if !identity.CanAccessLegalHold(legalHoldID) {
		return notionAdminLegalHoldUsersMutationError(event, errLegalHoldNotFound)
	}
	event.Request.Body = http.MaxBytesReader(
		event.Response, event.Request.Body, notionAdminLegalHoldMaxBodyBytes,
	)
	if event.Request.ContentLength == 0 {
		return notionAdminLegalHoldUsersMutationError(event, leafen.NotionAdminInvalid(errLegalHoldInvalid, "The body is empty: send user_ids."))
	}
	request := notionAdminLegalHoldUsersAddRequest{}
	if err := event.BindBody(&request); err != nil {
		if tooLarge, answer := notionAdminLegalHoldBodyTooLarge(event, err); tooLarge {
			return answer
		}
		return notionAdminLegalHoldUsersMutationError(event, leafen.NotionAdminInvalid(errLegalHoldInvalid, leafen.NotionAdminValidationReason(err)))
	}
	userIDs, err := normalizeNotionAdminLegalHoldUsersAdd(request)
	if err != nil {
		return notionAdminLegalHoldUsersMutationError(event, err)
	}
	response, err := mutateNotionAdminLegalHoldUsers(
		event.Request.Context(), event.App, secret, identity, legalHoldID,
		userIDs, notionAdminLegalHoldUsersMutationAdd,
	)
	if err != nil {
		return notionAdminLegalHoldUsersMutationError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

func normalizeNotionAdminLegalHoldUsersAdd(
	request notionAdminLegalHoldUsersAddRequest,
) ([]string, error) {
	if request.UserIDs == nil {
		return nil, leafen.NotionAdminInvalid(errLegalHoldInvalid, "user_ids is required.")
	}
	internalIDs := make([]string, 0, len(*request.UserIDs))
	seen := make(map[string]struct{}, len(*request.UserIDs))
	for _, publicID := range *request.UserIDs {
		internalID, err := leafen.ResolveNotionUserIdentityFilter(publicID)
		if err != nil {
			return nil, leafen.NotionAdminInvalid(errLegalHoldInvalid, "user_ids has an entry that isn't a user ID.")
		}
		if _, duplicate := seen[internalID]; duplicate {
			continue
		}
		seen[internalID] = struct{}{}
		internalIDs = append(internalIDs, internalID)
	}
	sort.Strings(internalIDs)
	return internalIDs, nil
}
