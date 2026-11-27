package app

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/pocketbase/pocketbase/core"
	sqlite3 "modernc.org/sqlite/lib"
)

func serveNotionDatabaseRowPageRetrieve(
	event *core.RequestEvent,
	publicPageID string,
) error {
	// A template's page reads as a page of its data source.
	if handled, err := serveNotionDatabaseTemplatePageRetrieve(event, publicPageID); handled {
		return err
	}
	admission, authorized, err := authorizeNotionDatabaseRowPageState(
		event, publicPageID, "pages:read", permissionWorkspaceRead, notionRowAnyTrash,
	)
	if err != nil || !authorized {
		return err
	}
	response, err := loadNotionDatabaseRowPageResponse(
		event, admission.Identity, admission.Authority.RowID, permissionWorkspaceRead,
	)
	if err != nil {
		return notionDatabaseRowPageReadError(event, err)
	}
	// filter_properties keeps the properties it names, as a data source
	// query's does (seen 2026-11-09).
	if wanted := notionDataSourceQueryProperties(event.Request.URL.Query()); wanted != nil {
		response["properties"] = notionDataSourceQuerySelectedProperties(response["properties"], wanted)
	}
	recordAPIReads(event.App, admission.Identity, []contentRead{{Kind: "row", ID: admission.Authority.RowID, How: contentReadOpen}})
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

// loadNotionDatabaseRowPageResponse is a row's page as the API answers it,
// read in a snapshot of its own (runNotionReadSnapshot). It keeps its access
// decisions as a read scope does (access_memo.go): what it writes is the
// row's public identities, which no decision reads, and a rollup's related
// rows are each judged once, not for every rollup showing them, under checks
// of their database judged once (2026-11-12: a query of 97 rows each showing
// two rollups of the same 100 related rows took 35 to 48 seconds, most of it
// judging those rows again).
func loadNotionDatabaseRowPageResponse(
	event *core.RequestEvent,
	identity notionOAuthBearerIdentity,
	rowID string,
	requirement permissionRequirement,
) (map[string]any, error) {
	var response map[string]any
	err := runNotionReadSnapshot(event.Request.Context(), event.App, func(transaction core.App) error {
		var err error
		response, err = buildNotionDatabaseRowPageResponse(event, withAccessMemo(transaction), identity, rowID, requirement)
		return err
	})
	return response, err
}

// loadNotionDatabaseRowPageResponses are the pages of rowIDs as a data
// source query answers them, read in one snapshot: each related row is
// judged once for the whole answer (access_memo.go), not once a row, and the
// public identities the answer names that weren't stored yet are stored
// before it goes out (runReadSnapshot). Each row in a write
// transaction of its own judged the same related rows again for every row
// and held the one writer for each (2026-11-12: 97 rows each showing a
// relation and two rollups of the same 100 rows took 15 seconds). A row
// whose reading writes something else (a server's first file secret, say)
// has the answer read row by row as before.
func loadNotionDatabaseRowPageResponses(
	event *core.RequestEvent,
	identity notionOAuthBearerIdentity,
	rowIDs []string,
	requirement permissionRequirement,
) ([]map[string]any, error) {
	responses := make([]map[string]any, 0, len(rowIDs))
	err := runReadSnapshot(event.Request.Context(), event.App, func(snapshot core.App) error {
		app := withAccessMemo(snapshot)
		for _, rowID := range rowIDs {
			response, err := buildNotionDatabaseRowPageResponse(event, app, identity, rowID, requirement)
			if err != nil {
				return err
			}
			responses = append(responses, response)
		}
		return nil
	})
	if isSQLiteBaseCode(err, sqlite3.SQLITE_READONLY) {
		responses = responses[:0]
		for _, rowID := range rowIDs {
			response, err := loadNotionDatabaseRowPageResponse(event, identity, rowID, requirement)
			if err != nil {
				return nil, err
			}
			responses = append(responses, response)
		}
		return responses, nil
	}
	if err != nil {
		return nil, err
	}
	return responses, nil
}

// buildNotionDatabaseRowPageResponse is a row's page as the API answers it,
// read in app.
func buildNotionDatabaseRowPageResponse(
	event *core.RequestEvent,
	app core.App,
	identity notionOAuthBearerIdentity,
	rowID string,
	requirement permissionRequirement,
) (map[string]any, error) {
	// A read reaches it in the trash or under it (notion_trash_reads.go).
	reach := notionRowOutOfTrash
	if requirement == permissionWorkspaceRead {
		reach = notionRowAnyTrash
	}
	current, err := authorizeNotionDatabaseRowPageCurrentState(app, identity, rowID, requirement, reach)
	if err != nil {
		return nil, err
	}
	identities, err := ensureNotionDatabaseRowPagePublicIdentities(app, current)
	if err != nil {
		return nil, err
	}
	fileIssuer := newNotionBlockFileCapabilityIssuer(event.Request, current.Identity)
	properties, err := loadNotionDatabaseRowPageProperties(app, current, fileIssuer)
	if err != nil {
		return nil, err
	}
	origin, err := outwardServerOrigin(event.Request)
	if err != nil {
		return nil, err
	}
	response, err := notionDatabaseRowPageObject(app, origin, current, identities, properties, fileIssuer)
	if err != nil {
		return nil, err
	}
	// In the trash with its database, or a page above it, too.
	place, err := notionPageTrashPlace(app, rowID)
	response["in_trash"] = place.inTrash()
	return response, err
}

// notionDatabaseRowPageObject is a database page as the Public API gives it,
// its links at origin; an automation's webhook sends the same object.
func notionDatabaseRowPageObject(
	app core.App,
	origin string,
	authorization notionDatabaseRowPageAuthorization,
	identities notionDatabaseRowPagePublicIdentities,
	projected []notionDatabaseRowPageProjectedProperty,
	fileIssuer *notionBlockFileCapabilityIssuer,
) (map[string]any, error) {
	authority := authorization.Authority
	createdBy, lastEditedBy, err := connectionAuthorIDs(
		app, connectionAuthoredRow, authority.RowID, notionBlockEditMark(authority.UpdatedAt), authority.CreatedBy, authority.LastEditedBy,
	)
	if err != nil {
		return nil, err
	}
	createdTime, err := notionDatabaseRowPageTime(authority.CreatedAt)
	if err != nil {
		return nil, err
	}
	lastEditedTime, err := notionDatabaseRowPageTime(authority.UpdatedAt)
	if err != nil {
		return nil, err
	}
	properties := make(map[string]any, len(projected))
	for _, property := range projected {
		if property.Name == "" || property.PageValue == nil {
			return nil, errNotionDatabaseRowPageProjectionInvalid
		}
		if _, duplicate := properties[property.Name]; duplicate {
			return nil, errNotionDatabaseRowPageProjectionInvalid
		}
		properties[property.Name] = property.PageValue
	}
	icon, err := notionPageIconWithManagedFile(
		app, fileIssuer, authority.WorkspaceID, authority.IconType, authority.Icon,
	)
	if err != nil {
		return nil, err
	}
	cover, err := notionPageCoverWithManagedFile(app, fileIssuer, authority.WorkspaceID, authority.Cover)
	if err != nil {
		return nil, err
	}
	archived, err := databaseRowPageArchived(app, authority)
	if err != nil {
		return nil, err
	}
	parent, err := notionRowPageParent(app, authority.RowID, identities.DataSourceID, identities.DatabaseID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"object":           "page",
		"id":               identities.PageID,
		"created_time":     createdTime,
		"last_edited_time": lastEditedTime,
		"created_by":       map[string]any{"object": "user", "id": createdBy},
		"last_edited_by":   map[string]any{"object": "user", "id": lastEditedBy},
		"cover":            cover,
		"icon":             icon,
		"parent":           parent,
		"in_trash":         authority.TrashedAt != 0,
		"is_archived":      archived,
		"is_locked":        authority.Locked,
		"properties":       notionPropertyObjects(properties),
		"url":              notionRowPageObjectURL(origin, identities.PageID, properties),
		"public_url":       nil,
	}, nil
}

func serveNotionDatabaseRowPagePropertyRetrieve(
	event *core.RequestEvent,
	publicPageID string,
	propertyID string,
) error {
	admission, authorized, err := authorizeNotionDatabaseRowPageState(
		event, publicPageID, "pages:read", permissionWorkspaceRead, notionRowAnyTrash,
	)
	if err != nil || !authorized {
		return err
	}
	// A database page in the trash itself gives no property item, one under
	// its database in the trash does (notion_trash_reads.go).
	if admission.Authority.TrashedAt != 0 {
		return notionPageNotFound(event)
	}
	pageSize, err := notionPagePropertyPageSize(event.Request)
	if err != nil {
		return notionPageValidationError(event, "page_size must be a number.")
	}
	// A rollup's cursor names a related page and an item in it
	// (notion_rollup_property_items.go).
	rawCursor := event.Request.URL.Query().Get("start_cursor")
	start, cursorPresent, startErr := notionDatabaseRowPagePropertyStart(event.Request)
	rollupCursor, rollupCursorPresent, rollupErr := parseNotionRollupCursor(rawCursor)
	if startErr != nil && rollupErr != nil {
		// A cursor of no form a property's list gives is let be, the list
		// answered from its start (notionDatabaseRowPagePropertyStart).
		start, cursorPresent, startErr = 0, false, nil
		rollupCursor, rollupCursorPresent, rollupErr = parseNotionRollupCursor("")
	}
	var property notionDatabaseRowPageProjectedProperty
	var response map[string]any
	found, inRange := false, true
	err = runNotionReadSnapshot(event.Request.Context(), event.App, func(txApp core.App) error {
		property, response, found, inRange = notionDatabaseRowPageProjectedProperty{}, nil, false, true
		current, err := authorizeNotionDatabaseRowPageCurrentState(
			txApp, admission.Identity, admission.Authority.RowID, permissionWorkspaceRead, notionRowAnyTrash,
		)
		if err != nil {
			return err
		}
		fileIssuer := newNotionBlockFileCapabilityIssuer(event.Request, current.Identity)
		property, found, err = loadNotionDatabaseRowPageProperty(txApp, current, propertyID, fileIssuer)
		if err != nil || !found {
			return err
		}
		if property.Type == graphValueTypeRollup {
			if rollupErr != nil {
				inRange = false
				return nil
			}
			response, inRange, err = notionRollupPropertyList(
				txApp, event.Request, current, property, fileIssuer, rollupCursor, rollupCursorPresent, pageSize,
			)
			return err
		}
		if startErr != nil || cursorPresent && (!property.Paginated || start >= len(property.Items)) {
			inRange = false
			return nil
		}
		response = notionDatabaseRowPagePropertyResponse(property, pageSize, start)
		return notionPropertyItemNamesNext(txApp, event.Request, current, response, pageSize)
	})
	if err != nil {
		return notionDatabaseRowPageReadError(event, err)
	}
	if !found {
		return notionCompatibilityError(
			event, http.StatusNotFound, notionErrorObjectNotFound,
			"The page has no such property.",
		)
	}
	if !inRange {
		return notionPageValidationError(event, "start_cursor is invalid for this property.")
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	recordAPIReads(event.App, admission.Identity, []contentRead{{Kind: "row", ID: admission.Authority.RowID, How: contentReadOpen}})
	return event.JSON(http.StatusOK, response)
}

func notionDatabaseRowPagePropertyResponse(
	property notionDatabaseRowPageProjectedProperty,
	pageSize int,
	start int,
) map[string]any {
	if !property.Paginated {
		return notionPropertyItemsOf(property)[0]
	}
	end := start + pageSize
	if end > len(property.Items) {
		end = len(property.Items)
	}
	items := property.Items[start:end]
	hasMore := end < len(property.Items)
	var nextCursor any
	if hasMore {
		nextCursor = fmt.Sprintf("hnrp1.%d", end)
	}
	return map[string]any{
		"object": "list", "results": items, "next_cursor": nextCursor, "has_more": hasMore,
		"type": "property_item",
		"property_item": map[string]any{
			"id": property.PublicID, "type": property.Type,
			property.Type: map[string]any{}, "next_url": nil,
		},
	}
}

// notionDatabaseRowPagePropertyStart is where a page property's items start
// from by start_cursor, and whether it gave one. A cursor of no form its
// lists give errs, and its callers let it be, answering the items from their
// start, as Notion does (seen 2026-11-12: "nope" and a UUID without dashes,
// on a title, a people and a select property; one with dashes it answered
// 500, which Leafen doesn't copy); one of the form that names no place in
// the list is refused.
func notionDatabaseRowPagePropertyStart(request *http.Request) (int, bool, error) {
	raw := strings.TrimSpace(request.URL.Query().Get("start_cursor"))
	if raw == "" {
		return 0, false, nil
	}
	offset, err := strconv.Atoi(strings.TrimPrefix(raw, "hnrp1."))
	if err != nil || !strings.HasPrefix(raw, "hnrp1.") || offset < 0 || offset > 100000 {
		return 0, false, errNotionDatabaseRowPageProjectionInvalid
	}
	return offset, true, nil
}

func notionDatabaseRowPageReadError(event *core.RequestEvent, err error) error {
	if isDLPPolicyDecisionError(err) {
		return notionDLPPolicyError(event, err)
	}
	if errors.Is(err, errDatabaseRowPageNotFound) {
		return notionPageNotFound(event)
	}
	propertyType := &notionDatabaseRowPagePropertyTypeError{}
	if errors.As(err, &propertyType) {
		return notionPageValidationError(
			event,
			"Database property type is not supported by this Notion-compatible endpoint: "+propertyType.Type+".",
		)
	}
	return notionRequestNotCompleted(event, err)
}
