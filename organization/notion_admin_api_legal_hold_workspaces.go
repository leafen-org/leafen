package organization

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sort"
	"strings"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	notionAdminLegalHoldWorkspacesCursorVersion        = 1
	notionAdminLegalHoldWorkspacesCursorPrefix         = "hnalw1"
	notionAdminLegalHoldWorkspacesCursorMaxLength      = 1024
	notionAdminLegalHoldWorkspacesCursorDomain         = "leafen:notion-admin-legal-hold-workspaces-cursor:v1"
	notionAdminLegalHoldWorkspaceAuthorityOrganization = "organization-workspaces"
	notionAdminLegalHoldWorkspaceAuthorityNative       = "native-workspace-scope"
)

var errInvalidNotionAdminLegalHoldWorkspacesCursor = leafen.NotionAdminInvalid(errors.New("invalid Notion Admin legal-hold workspaces cursor"), "start_cursor isn't one this list gave.")

type notionAdminLegalHoldWorkspacesCursor struct {
	Version           int    `json:"v"`
	OrganizationID    string `json:"o"`
	TokenID           string `json:"t"`
	LegalHoldID       string `json:"h"`
	AuthorityClass    string `json:"c"`
	AuthorityRevision int64  `json:"r"`
	AfterWorkspaceID  string `json:"a"`
}

type notionAdminLegalHoldWorkspacesPage struct {
	InternalWorkspaceIDs []string
	AuthorityClass       string
	AuthorityRevision    int64
	HasMore              bool
}

type notionAdminLegalHoldWorkspacesRow struct {
	HoldID            string         `db:"hold_id"`
	AuthorityClass    string         `db:"authority_class"`
	AuthorityRevision int64          `db:"authority_revision"`
	TargetID          sql.NullString `db:"target_id"`
}

func serveNotionAdminLegalHoldWorkspacesList(
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
	if event.Request.ContentLength > 0 {
		return notionAdminLegalHoldWorkspacesValidationError(event, nil)
	}
	rawCursor, valid := leafen.NotionAdminLegalHoldScopeQuery(event)
	if !valid {
		return notionAdminLegalHoldWorkspacesValidationError(event, nil)
	}
	legalHoldID, err := normalizeNotionAdminLegalHoldID(rawLegalHoldID)
	if err != nil {
		return notionAdminLegalHoldWorkspacesValidationError(event, err)
	}
	if !identity.CanAccessLegalHold(legalHoldID) {
		return notionAdminLegalHoldWorkspacesNotFoundError(event, nil)
	}
	cursor, err := decodeNotionAdminLegalHoldWorkspacesCursor(
		rawCursor, secret, identity, legalHoldID,
	)
	if err != nil {
		return notionAdminLegalHoldWorkspacesValidationError(event, err)
	}
	page, err := listNotionAdminLegalHoldWorkspaces(
		event.Request.Context(), event.App, identity, legalHoldID, cursor,
	)
	if errors.Is(err, errInvalidNotionAdminLegalHoldWorkspacesCursor) {
		return notionAdminLegalHoldWorkspacesValidationError(event, err)
	}
	if errors.Is(err, errLegalHoldNotFound) {
		return notionAdminLegalHoldWorkspacesNotFoundError(event, err)
	}
	if err != nil {
		return leafen.NotionAdminAPIError(
			event, http.StatusInternalServerError, leafen.NotionAdminErrorInternal,
			leafen.NotionAdminInternalErrorMessage, err,
		)
	}

	workspaceIDs := make([]string, 0, len(page.InternalWorkspaceIDs))
	for _, internalID := range page.InternalWorkspaceIDs {
		publicID, err := leafen.NotionCompatibleWorkspaceID(internalID)
		if err != nil {
			return leafen.NotionAdminAPIError(
				event, http.StatusInternalServerError, leafen.NotionAdminErrorInternal,
				leafen.NotionAdminInternalErrorMessage, err,
			)
		}
		workspaceIDs = append(workspaceIDs, publicID)
	}
	response := map[string]any{"workspace_ids": workspaceIDs}
	if page.HasMore {
		nextCursor, err := encodeNotionAdminLegalHoldWorkspacesCursor(
			secret, identity, legalHoldID,
			page.InternalWorkspaceIDs[len(page.InternalWorkspaceIDs)-1],
			page.AuthorityClass, page.AuthorityRevision,
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

func listNotionAdminLegalHoldWorkspaces(
	ctx context.Context,
	app core.App,
	identity leafen.NotionAdminIdentity,
	legalHoldID string,
	cursor notionAdminLegalHoldWorkspacesCursor,
) (notionAdminLegalHoldWorkspacesPage, error) {
	params := dbx.Params{
		"organizationId":        identity.OrganizationID,
		"holdId":                legalHoldID,
		"adminVersion":          leafen.NotionAdminLegalHoldMetadataVersion,
		"organizationAuthority": notionAdminLegalHoldWorkspaceAuthorityOrganization,
		"nativeAuthority":       notionAdminLegalHoldWorkspaceAuthorityNative,
		"workspaceKind":         leafen.LegalHoldScopeWorkspace,
		"afterWorkspaceId":      cursor.AfterWorkspaceID,
		"limit":                 leafen.LegalHoldMaxPageSize + 1,
	}
	workspaceVisibility, scopeVisibility := notionAdminLegalHoldWorkspaceVisibility(
		params, identity,
	)
	page := notionAdminLegalHoldWorkspacesPage{}
	err := app.RunInTransaction(func(txApp core.App) error {
		rows := []notionAdminLegalHoldWorkspacesRow{}
		err := txApp.DB().NewQuery(`
			WITH [[authority]] AS (
				SELECT [[hold]].[[id]] AS [[hold_id]],
					CASE WHEN [[hold]].[[notion_admin_version]] = {:adminVersion}
						THEN {:organizationAuthority} ELSE {:nativeAuthority} END AS [[authority_class]],
					CASE WHEN [[hold]].[[notion_admin_version]] = {:adminVersion}
						THEN [[policy]].[[workspace_revision]] ELSE [[hold]].[[created_at]] END AS [[authority_revision]]
				FROM {{leafen_organization_legal_holds}} AS [[hold]]
				JOIN {{leafen_organization_policy_state}} AS [[policy]]
					ON [[policy]].[[organization_id]] = [[hold]].[[organization_id]]
				WHERE [[hold]].[[id]] = {:holdId}
					AND [[hold]].[[organization_id]] = {:organizationId}
			), [[page]] AS (
				SELECT [[candidate]].[[target_id]] FROM (
					SELECT [[organization_page]].[[target_id]] FROM (
						SELECT [[workspace]].[[id]] AS [[target_id]]
						FROM [[authority]]
						JOIN {{leafen_workspaces}} AS [[workspace]]
							INDEXED BY [[idx_leafen_workspaces_organization_id]]
							ON [[workspace]].[[organization]] = {:organizationId}
						WHERE [[authority]].[[authority_class]] = {:organizationAuthority}
							AND [[workspace]].[[id]] > {:afterWorkspaceId}` + workspaceVisibility + `
						ORDER BY [[workspace]].[[id]] LIMIT {:limit}
					) AS [[organization_page]]
					UNION ALL
					SELECT [[native_page]].[[target_id]] FROM (
						SELECT [[scope]].[[target_id]]
						FROM [[authority]]
						JOIN {{leafen_organization_legal_hold_scopes}} AS [[scope]]
							INDEXED BY [[sqlite_autoindex_leafen_organization_legal_hold_scopes_1]]
							ON [[scope]].[[hold_id]] = {:holdId}
							AND [[scope]].[[organization_id]] = {:organizationId}
						WHERE [[authority]].[[authority_class]] = {:nativeAuthority}
							AND [[scope]].[[kind]] = {:workspaceKind}
							AND [[scope]].[[target_id]] > {:afterWorkspaceId}` + scopeVisibility + `
						ORDER BY [[scope]].[[target_id]] LIMIT {:limit}
					) AS [[native_page]]
				) AS [[candidate]]
				ORDER BY [[candidate]].[[target_id]] LIMIT {:limit}
			)
			SELECT [[authority]].[[hold_id]], [[authority]].[[authority_class]],
				[[authority]].[[authority_revision]], [[page]].[[target_id]]
			FROM [[authority]] LEFT JOIN [[page]] ON TRUE
			ORDER BY [[page]].[[target_id]]
		`).WithContext(ctx).Bind(params).All(&rows)
		if err != nil {
			return errors.Join(leafen.ErrLegalHoldUnavailable, err)
		}
		if len(rows) == 0 {
			return errLegalHoldNotFound
		}
		page.AuthorityClass = rows[0].AuthorityClass
		page.AuthorityRevision = rows[0].AuthorityRevision
		if !validNotionAdminLegalHoldWorkspaceAuthority(page.AuthorityClass) ||
			page.AuthorityRevision < 1 {
			return leafen.ErrLegalHoldUnavailable
		}
		if cursor.Version != 0 &&
			(cursor.AuthorityClass != page.AuthorityClass ||
				cursor.AuthorityRevision != page.AuthorityRevision) {
			return errInvalidNotionAdminLegalHoldWorkspacesCursor
		}
		for _, row := range rows {
			if row.HoldID != legalHoldID || row.AuthorityClass != page.AuthorityClass ||
				row.AuthorityRevision != page.AuthorityRevision {
				return leafen.ErrLegalHoldUnavailable
			}
			if row.TargetID.Valid {
				page.InternalWorkspaceIDs = append(page.InternalWorkspaceIDs, row.TargetID.String)
			}
		}
		page.HasMore = len(page.InternalWorkspaceIDs) > leafen.LegalHoldMaxPageSize
		if page.HasMore {
			page.InternalWorkspaceIDs = page.InternalWorkspaceIDs[:leafen.LegalHoldMaxPageSize]
		}
		return nil
	})
	return page, err
}

func notionAdminLegalHoldWorkspaceVisibility(
	params dbx.Params,
	identity leafen.NotionAdminIdentity,
) (string, string) {
	if _, wildcard := identity.WorkspaceIDs()["*"]; wildcard {
		return "", ""
	}
	allowed := make([]string, 0, len(identity.WorkspaceIDs()))
	for id := range identity.WorkspaceIDs() {
		allowed = append(allowed, id)
	}
	sort.Strings(allowed)
	if len(allowed) == 0 {
		return " AND 0 = 1", " AND 0 = 1"
	}
	bound := strings.Join(bindLegalHoldIDs(params, "visibleWorkspace", allowed), ", ")
	return " AND [[workspace]].[[id]] IN (" + bound + ")",
		" AND [[scope]].[[target_id]] IN (" + bound + ")"
}

func encodeNotionAdminLegalHoldWorkspacesCursor(
	secret string,
	identity leafen.NotionAdminIdentity,
	legalHoldID, afterWorkspaceID, authorityClass string,
	authorityRevision int64,
) (string, error) {
	cursor := notionAdminLegalHoldWorkspacesCursor{
		Version:           notionAdminLegalHoldWorkspacesCursorVersion,
		OrganizationID:    identity.OrganizationID,
		TokenID:           identity.TokenID,
		LegalHoldID:       legalHoldID,
		AuthorityClass:    authorityClass,
		AuthorityRevision: authorityRevision,
		AfterWorkspaceID:  afterWorkspaceID,
	}
	result, err := leafen.EncodeNotionAdminLegalHoldScopeCursor(
		secret,
		notionAdminLegalHoldWorkspacesCursorPrefix,
		notionAdminLegalHoldWorkspacesCursorDomain,
		notionAdminLegalHoldWorkspacesCursorMaxLength,
		cursor,
	)
	if err != nil {
		return "", errInvalidNotionAdminLegalHoldWorkspacesCursor
	}
	return result, nil
}

func decodeNotionAdminLegalHoldWorkspacesCursor(
	raw, secret string,
	identity leafen.NotionAdminIdentity,
	legalHoldID string,
) (notionAdminLegalHoldWorkspacesCursor, error) {
	if raw == "" {
		return notionAdminLegalHoldWorkspacesCursor{}, nil
	}
	cursor := notionAdminLegalHoldWorkspacesCursor{}
	if leafen.DecodeNotionAdminLegalHoldScopeCursor(
		raw,
		secret,
		notionAdminLegalHoldWorkspacesCursorPrefix,
		notionAdminLegalHoldWorkspacesCursorDomain,
		notionAdminLegalHoldWorkspacesCursorMaxLength,
		&cursor,
	) != nil || cursor.Version != notionAdminLegalHoldWorkspacesCursorVersion ||
		cursor.OrganizationID != identity.OrganizationID ||
		cursor.TokenID != identity.TokenID || cursor.LegalHoldID != legalHoldID ||
		!validNotionAdminLegalHoldWorkspaceAuthority(cursor.AuthorityClass) ||
		cursor.AuthorityRevision < 1 || cursor.AfterWorkspaceID == "" {
		return notionAdminLegalHoldWorkspacesCursor{}, errInvalidNotionAdminLegalHoldWorkspacesCursor
	}
	if _, err := leafen.NotionCompatibleWorkspaceID(cursor.AfterWorkspaceID); err != nil {
		return notionAdminLegalHoldWorkspacesCursor{}, errInvalidNotionAdminLegalHoldWorkspacesCursor
	}
	return cursor, nil
}

func validNotionAdminLegalHoldWorkspaceAuthority(value string) bool {
	return value == notionAdminLegalHoldWorkspaceAuthorityOrganization ||
		value == notionAdminLegalHoldWorkspaceAuthorityNative
}

func notionAdminLegalHoldWorkspacesValidationError(event *core.RequestEvent, err error) error {
	return leafen.NotionAdminAPIError(
		event, http.StatusBadRequest, leafen.NotionAdminErrorValidation,
		leafen.NotionAdminValidationReason(err), err,
	)
}

func notionAdminLegalHoldWorkspacesNotFoundError(event *core.RequestEvent, err error) error {
	return leafen.NotionAdminAPIError(
		event, http.StatusNotFound, leafen.NotionAdminErrorNotFound,
		leafen.NotionAdminNotFoundMessage, err,
	)
}
