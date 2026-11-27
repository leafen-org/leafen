package organization

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	notionAdminLegalHoldUsersCursorVersion   = 1
	notionAdminLegalHoldUsersCursorPrefix    = "hnalu1"
	notionAdminLegalHoldUsersCursorMaxLength = 1024
	notionAdminLegalHoldUsersCursorDomain    = "leafen:notion-admin-legal-hold-users-cursor:v1"
)

var errInvalidNotionAdminLegalHoldUsersCursor = leafen.NotionAdminInvalid(errors.New("invalid Notion Admin legal-hold users cursor"), "start_cursor isn't one this list gave.")

type notionAdminLegalHoldUsersCursor struct {
	Version        int    `json:"v"`
	OrganizationID string `json:"o"`
	TokenID        string `json:"t"`
	LegalHoldID    string `json:"h"`
	AfterUserID    string `json:"a"`
	ScopeCount     int    `json:"n"`
	FrontierUserID string `json:"f"`
}

type notionAdminLegalHoldUsersPage struct {
	InternalUserIDs []string
	ScopeCount      int
	FrontierUserID  string
	HasMore         bool
}

type notionAdminLegalHoldUsersRow struct {
	HoldID         string         `db:"hold_id"`
	TargetID       sql.NullString `db:"target_id"`
	ScopeCount     int            `db:"scope_count"`
	FrontierUserID string         `db:"frontier_user_id"`
}

func serveNotionAdminLegalHoldUsersList(
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

	rawCursor, valid := leafen.NotionAdminLegalHoldScopeQuery(event)
	if !valid {
		return notionAdminLegalHoldUsersValidationError(event, nil)
	}
	legalHoldID, err := normalizeNotionAdminLegalHoldID(rawLegalHoldID)
	if err != nil {
		return notionAdminLegalHoldUsersValidationError(event, err)
	}
	if !identity.CanAccessLegalHold(legalHoldID) {
		return notionAdminLegalHoldUsersNotFoundError(event, nil)
	}
	cursor, err := decodeNotionAdminLegalHoldUsersCursor(
		rawCursor, secret, identity, legalHoldID,
	)
	if err != nil {
		return notionAdminLegalHoldUsersValidationError(event, err)
	}
	page, err := listNotionAdminLegalHoldUsers(
		event.Request.Context(), event.App, identity, legalHoldID, cursor,
	)
	if errors.Is(err, errInvalidNotionAdminLegalHoldUsersCursor) {
		return notionAdminLegalHoldUsersValidationError(event, err)
	}
	if errors.Is(err, errLegalHoldNotFound) {
		return notionAdminLegalHoldUsersNotFoundError(event, err)
	}
	if err != nil {
		return leafen.NotionAdminAPIError(
			event, http.StatusInternalServerError, leafen.NotionAdminErrorInternal,
			leafen.NotionAdminInternalErrorMessage, err,
		)
	}

	userIDs := make([]string, 0, len(page.InternalUserIDs))
	for _, internalID := range page.InternalUserIDs {
		publicID, err := leafen.ProjectNotionUserIdentity(internalID)
		if err != nil {
			return leafen.NotionAdminAPIError(
				event, http.StatusInternalServerError, leafen.NotionAdminErrorInternal,
				leafen.NotionAdminInternalErrorMessage, err,
			)
		}
		userIDs = append(userIDs, publicID)
	}
	response := map[string]any{"user_ids": userIDs}
	if page.HasMore {
		nextCursor, err := encodeNotionAdminLegalHoldUsersCursor(
			secret, identity, legalHoldID,
			page.InternalUserIDs[len(page.InternalUserIDs)-1],
			page.ScopeCount, page.FrontierUserID,
		)
		if err != nil {
			return leafen.NotionAdminAPIError(
				event, http.StatusInternalServerError, leafen.NotionAdminErrorInternal,
				leafen.NotionAdminInternalErrorMessage, err,
			)
		}
		response["next_cursor"] = nextCursor
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

func listNotionAdminLegalHoldUsers(
	ctx context.Context,
	app core.App,
	identity leafen.NotionAdminIdentity,
	legalHoldID string,
	cursor notionAdminLegalHoldUsersCursor,
) (notionAdminLegalHoldUsersPage, error) {
	page := notionAdminLegalHoldUsersPage{}
	err := app.RunInTransaction(func(txApp core.App) error {
		rows := []notionAdminLegalHoldUsersRow{}
		err := txApp.DB().NewQuery(`
			WITH [[targets]] AS MATERIALIZED (
				SELECT [[target_id]]
				FROM {{leafen_organization_legal_hold_scopes}}
					INDEXED BY [[sqlite_autoindex_leafen_organization_legal_hold_scopes_1]]
				WHERE [[hold_id]] = {:holdId}
					AND [[organization_id]] = {:organizationId}
					AND [[kind]] = {:custodianKind}
			), [[snapshot]] AS (
				SELECT COUNT(*) AS [[scope_count]],
					COALESCE(MAX([[target_id]]), '') AS [[frontier_user_id]]
				FROM [[targets]]
			), [[page]] AS (
				SELECT [[target_id]] FROM [[targets]]
				WHERE [[target_id]] > {:afterUserId}
				ORDER BY [[target_id]] LIMIT {:limit}
			)
			SELECT [[hold]].[[id]] AS [[hold_id]], [[page]].[[target_id]],
				[[snapshot]].[[scope_count]], [[snapshot]].[[frontier_user_id]]
			FROM {{leafen_organization_legal_holds}} AS [[hold]]
			CROSS JOIN [[snapshot]] LEFT JOIN [[page]] ON TRUE
			WHERE [[hold]].[[id]] = {:holdId}
				AND [[hold]].[[organization_id]] = {:organizationId}
			ORDER BY [[page]].[[target_id]]
		`).WithContext(ctx).Bind(dbx.Params{
			"holdId": legalHoldID, "organizationId": identity.OrganizationID,
			"custodianKind": leafen.LegalHoldScopeCustodian,
			"afterUserId":   cursor.AfterUserID,
			"limit":         leafen.LegalHoldMaxPageSize + 1,
		}).All(&rows)
		if err != nil {
			return errors.Join(leafen.ErrLegalHoldUnavailable, err)
		}
		if len(rows) == 0 {
			return errLegalHoldNotFound
		}
		page.ScopeCount = rows[0].ScopeCount
		page.FrontierUserID = rows[0].FrontierUserID
		if page.ScopeCount < 0 ||
			(page.ScopeCount == 0) != (page.FrontierUserID == "") {
			return leafen.ErrLegalHoldUnavailable
		}
		if cursor.Version != 0 &&
			(cursor.ScopeCount != page.ScopeCount ||
				cursor.FrontierUserID != page.FrontierUserID) {
			return errInvalidNotionAdminLegalHoldUsersCursor
		}
		for _, row := range rows {
			if row.HoldID != legalHoldID || row.ScopeCount != page.ScopeCount ||
				row.FrontierUserID != page.FrontierUserID {
				return leafen.ErrLegalHoldUnavailable
			}
			if row.TargetID.Valid {
				page.InternalUserIDs = append(page.InternalUserIDs, row.TargetID.String)
			}
		}
		page.HasMore = len(page.InternalUserIDs) > leafen.LegalHoldMaxPageSize
		if page.HasMore {
			page.InternalUserIDs = page.InternalUserIDs[:leafen.LegalHoldMaxPageSize]
		}
		return nil
	})
	return page, err
}

func encodeNotionAdminLegalHoldUsersCursor(
	secret string,
	identity leafen.NotionAdminIdentity,
	legalHoldID, afterUserID string,
	scopeCount int,
	frontierUserID string,
) (string, error) {
	cursor := notionAdminLegalHoldUsersCursor{
		Version:        notionAdminLegalHoldUsersCursorVersion,
		OrganizationID: identity.OrganizationID, TokenID: identity.TokenID,
		LegalHoldID: legalHoldID, AfterUserID: afterUserID,
		ScopeCount: scopeCount, FrontierUserID: frontierUserID,
	}
	result, err := leafen.EncodeNotionAdminLegalHoldScopeCursor(
		secret,
		notionAdminLegalHoldUsersCursorPrefix,
		notionAdminLegalHoldUsersCursorDomain,
		notionAdminLegalHoldUsersCursorMaxLength,
		cursor,
	)
	if err != nil {
		return "", errInvalidNotionAdminLegalHoldUsersCursor
	}
	return result, nil
}

func decodeNotionAdminLegalHoldUsersCursor(
	raw, secret string,
	identity leafen.NotionAdminIdentity,
	legalHoldID string,
) (notionAdminLegalHoldUsersCursor, error) {
	if raw == "" {
		return notionAdminLegalHoldUsersCursor{}, nil
	}
	cursor := notionAdminLegalHoldUsersCursor{}
	if leafen.DecodeNotionAdminLegalHoldScopeCursor(
		raw,
		secret,
		notionAdminLegalHoldUsersCursorPrefix,
		notionAdminLegalHoldUsersCursorDomain,
		notionAdminLegalHoldUsersCursorMaxLength,
		&cursor,
	) != nil ||
		cursor.Version != notionAdminLegalHoldUsersCursorVersion ||
		cursor.OrganizationID != identity.OrganizationID ||
		cursor.TokenID != identity.TokenID || cursor.LegalHoldID != legalHoldID ||
		cursor.ScopeCount <= leafen.LegalHoldMaxPageSize ||
		cursor.AfterUserID == "" ||
		cursor.FrontierUserID == "" || cursor.AfterUserID >= cursor.FrontierUserID {
		return notionAdminLegalHoldUsersCursor{}, errInvalidNotionAdminLegalHoldUsersCursor
	}
	if _, err := leafen.NotionCompatibleUserID(cursor.AfterUserID); err != nil {
		return notionAdminLegalHoldUsersCursor{}, errInvalidNotionAdminLegalHoldUsersCursor
	}
	if _, err := leafen.NotionCompatibleUserID(cursor.FrontierUserID); err != nil {
		return notionAdminLegalHoldUsersCursor{}, errInvalidNotionAdminLegalHoldUsersCursor
	}
	return cursor, nil
}

func notionAdminLegalHoldUsersValidationError(event *core.RequestEvent, err error) error {
	return leafen.NotionAdminAPIError(
		event, http.StatusBadRequest, leafen.NotionAdminErrorValidation,
		leafen.NotionAdminValidationReason(err), err,
	)
}

func notionAdminLegalHoldUsersNotFoundError(event *core.RequestEvent, err error) error {
	return leafen.NotionAdminAPIError(
		event, http.StatusNotFound, leafen.NotionAdminErrorNotFound,
		leafen.NotionAdminNotFoundMessage, err,
	)
}
