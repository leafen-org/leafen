package app

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// notionPagePropertyQueryShape is a page property's query, which Notion
// checks before it looks for the page (seen 2026-11-21: ?zz=1 and
// page_size=abc refused 400 for a page it can't find) and Leafen with the
// route's (notionRouteShapes); page_size's range only once it found it.
var notionPagePropertyQueryShape = notionObjectOf(true,
	notionField("start_cursor", notionMaybeString),
	notionField("page_size", shapeOf(notionNumber).opt()))

func serveNotionPagePropertyRetrieve(
	event *core.RequestEvent,
	publicPageID string,
	propertyID string,
) error {
	if !notionPublicIDNamesPage(event.App, strings.TrimSpace(publicPageID)) {
		return serveNotionDatabaseRowPagePropertyRetrieve(event, publicPageID, propertyID)
	}
	authorization, authorized, err := authorizeNotionPage(
		event, publicPageID, "pages:read", permissionWorkspaceRead,
	)
	if err != nil || !authorized {
		return err
	}
	// A page in the trash itself gives no property item, one under a page
	// in it does (notion_trash_reads.go).
	if place, err := notionPageTrashPlace(event.App, authorization.Page.Page.Id); err != nil {
		return notionRequestNotCompleted(event, err)
	} else if place == notionInTrashItself {
		return notionPageNotFound(event)
	}
	if !strings.EqualFold(strings.TrimSpace(propertyID), "title") {
		return notionCompatibilityError(
			event, http.StatusNotFound, notionErrorObjectNotFound,
			"The page has no such property.",
		)
	}
	pageSize, err := notionPagePropertyPageSize(event.Request)
	if err != nil {
		return notionPageValidationError(event, "page_size must be a number.")
	}
	items, err := notionPageTitleAsRead(event.App, newNotionBlockFileCapabilityIssuer(event.Request, authorization.Identity),
		authorization.Identity, authorization.Page.Page)
	if err != nil {
		return notionRequestNotCompleted(event, err)
	}
	// A cursor of no form the list gives is let be (notionDatabaseRowPagePropertyStart).
	start, _, err := notionDatabaseRowPagePropertyStart(event.Request)
	if err != nil {
		start = 0
	}
	if start > len(items) {
		return notionPageValidationError(event, "start_cursor is invalid for this property.")
	}
	end := min(start+pageSize, len(items))
	hasMore := end < len(items)
	var nextCursor, nextURL any
	if hasMore {
		cursor := fmt.Sprintf("hnrp1.%d", end)
		nextCursor = cursor
		publicID, err := notionPagePublicID(event.App, authorization.Page.Page.Id)
		if err == nil {
			nextURL, err = notionPagePropertyNextURL(event.Request, publicID, "title", cursor, pageSize)
		}
		if err != nil {
			return notionRequestNotCompleted(event, err)
		}
	}
	items = items[start:end]
	results := make([]map[string]any, len(items))
	for index, item := range items {
		results[index] = map[string]any{
			"object": "property_item", "id": "title", "type": "title", "title": item,
		}
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, map[string]any{
		"object": "list", "results": results, "next_cursor": nextCursor, "has_more": hasMore,
		"type": "property_item",
		"property_item": map[string]any{
			"id": "title", "type": "title", "title": map[string]any{}, "next_url": nextURL,
		},
	})
}

func notionPagePropertyPageSize(request *http.Request) (int, error) {
	pageSize := notionBlockDefaultPageSize
	if raw := notionQueryDigits(request.URL.Query().Get("page_size")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			return 0, errInvalidNotionPageIdentity
		}
		pageSize = notionListPageSize(parsed, pageSize)
	}
	return pageSize, nil
}
