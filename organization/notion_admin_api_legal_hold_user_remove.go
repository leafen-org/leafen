package organization

import (
	"net/http"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/pocketbase/core"
)

func serveNotionAdminLegalHoldUserRemove(
	event *core.RequestEvent,
	rawLegalHoldID, rawUserID string,
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
	if len(event.Request.URL.Query()) != 0 || event.Request.ContentLength != 0 ||
		len(event.Request.TransferEncoding) != 0 {
		return notionAdminLegalHoldUsersMutationError(event, leafen.NotionAdminInvalid(errLegalHoldInvalid, "Removing a user takes no query parameters and no body."))
	}
	legalHoldID, err := normalizeNotionAdminLegalHoldID(rawLegalHoldID)
	if err != nil {
		return notionAdminLegalHoldUsersMutationError(event, err)
	}
	if !identity.CanAccessLegalHold(legalHoldID) {
		return notionAdminLegalHoldUsersMutationError(event, errLegalHoldNotFound)
	}
	userID, err := leafen.ResolveNotionUserIdentityFilter(rawUserID)
	if err != nil {
		return notionAdminLegalHoldUsersMutationError(event, leafen.NotionAdminInvalid(errLegalHoldInvalid, "The user ID isn't a user ID."))
	}
	response, err := mutateNotionAdminLegalHoldUsers(
		event.Request.Context(), event.App, secret, identity, legalHoldID,
		[]string{userID}, notionAdminLegalHoldUsersMutationRemove,
	)
	if err != nil {
		return notionAdminLegalHoldUsersMutationError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}
