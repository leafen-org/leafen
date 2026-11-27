package organization

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	notionAdminLegalHoldPagesCursorVersion   = 1
	notionAdminLegalHoldPagesCursorPrefix    = "hnalp1"
	notionAdminLegalHoldPagesCursorMaxLength = 1024
	notionAdminLegalHoldPagesCursorDomain    = "leafen:notion-admin-legal-hold-pages-cursor:v1"
)

var errInvalidNotionAdminLegalHoldPagesCursor = leafen.NotionAdminInvalid(errors.New("invalid Notion Admin legal-hold pages cursor"), "start_cursor isn't one this list gave.")

type notionAdminLegalHoldPagesCursor struct {
	Version             int    `json:"v"`
	OrganizationID      string `json:"o"`
	TokenID             string `json:"t"`
	LegalHoldID         string `json:"h"`
	WorkspaceID         string `json:"w"`
	HoldRevision        int64  `json:"r"`
	InteractionRevision int64  `json:"i"`
	AfterPageID         string `json:"a"`
}

type notionAdminLegalHoldPagesPage struct {
	PageIDs             []string
	HoldRevision        int64
	InteractionRevision int64
	HasMore             bool
}

type notionAdminLegalHoldPagesRow struct {
	HoldID               string          `db:"hold_id"`
	Status               string          `db:"status"`
	NotionAdminVersion   int             `db:"notion_admin_version"`
	NotionAdminStartDate float64         `db:"notion_admin_start_date"`
	NotionAdminEndDate   sql.NullFloat64 `db:"notion_admin_end_date"`
	InteractionMask      int             `db:"interaction_mask"`
	HoldRevision         int64           `db:"hold_revision"`
	InteractionRevision  int64           `db:"interaction_revision"`
	PageID               sql.NullString  `db:"page_id"`
}

func serveNotionAdminLegalHoldPagesList(
	event *core.RequestEvent,
	rawLegalHoldID, rawWorkspaceID string,
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
		return notionAdminLegalHoldPagesValidationError(event, nil)
	}
	rawCursor, valid := leafen.NotionAdminLegalHoldScopeQuery(event)
	if !valid {
		return notionAdminLegalHoldPagesValidationError(event, nil)
	}
	legalHoldID, err := normalizeNotionAdminLegalHoldID(rawLegalHoldID)
	if err != nil {
		return notionAdminLegalHoldPagesValidationError(event, err)
	}
	workspaceID, err := leafen.ResolveNotionCompatibleWorkspaceID(rawWorkspaceID)
	if err != nil {
		return notionAdminLegalHoldPagesValidationError(event, err)
	}
	if !identity.CanAccessLegalHold(legalHoldID) || !identity.CanAccessWorkspace(workspaceID) {
		return notionAdminLegalHoldPagesNotFoundError(event, nil)
	}
	cursor, err := decodeNotionAdminLegalHoldPagesCursor(
		rawCursor, secret, identity, legalHoldID, workspaceID,
	)
	if err != nil {
		return notionAdminLegalHoldPagesValidationError(event, err)
	}
	page, err := listNotionAdminLegalHoldPages(
		event.Request.Context(), event.App, identity, legalHoldID, workspaceID, cursor,
	)
	if errors.Is(err, errInvalidNotionAdminLegalHoldPagesCursor) {
		return notionAdminLegalHoldPagesValidationError(event, err)
	}
	if errors.Is(err, errLegalHoldNotFound) {
		return notionAdminLegalHoldPagesNotFoundError(event, err)
	}
	if err != nil {
		return leafen.NotionAdminAPIError(
			event, http.StatusInternalServerError, leafen.NotionAdminErrorInternal,
			leafen.NotionAdminInternalErrorMessage, err,
		)
	}
	response := map[string]any{"page_ids": page.PageIDs}
	if page.HasMore {
		nextCursor, err := encodeNotionAdminLegalHoldPagesCursor(
			secret, identity, legalHoldID, workspaceID,
			page.PageIDs[len(page.PageIDs)-1], page.HoldRevision,
			page.InteractionRevision,
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

func listNotionAdminLegalHoldPages(
	ctx context.Context,
	app core.App,
	identity leafen.NotionAdminIdentity,
	legalHoldID, workspaceID string,
	cursor notionAdminLegalHoldPagesCursor,
) (notionAdminLegalHoldPagesPage, error) {
	params := dbx.Params{
		"organizationId": identity.OrganizationID,
		"holdId":         legalHoldID, "workspaceId": workspaceID,
		"adminVersion": leafen.NotionAdminLegalHoldMetadataVersion,
		"allKind":      leafen.LegalHoldScopeAll, "workspaceKind": leafen.LegalHoldScopeWorkspace,
		"pageKind": leafen.LegalHoldScopePage, "custodianKind": leafen.LegalHoldScopeCustodian,
		"createdType": leafen.PageInteractionCreated, "editedType": leafen.PageInteractionEdited,
		"viewedType":  leafen.PageInteractionViewed,
		"afterPageId": cursor.AfterPageID,
		"limit":       leafen.LegalHoldMaxPageSize + 1,
	}
	page := notionAdminLegalHoldPagesPage{}
	err := app.RunInTransaction(func(txApp core.App) error {
		rows := []notionAdminLegalHoldPagesRow{}
		err := txApp.DB().NewQuery(`
			WITH [[authority]] AS (
				SELECT [[hold]].[[id]] AS [[hold_id]], [[hold]].[[status]],
					[[hold]].[[notion_admin_version]], [[hold]].[[notion_admin_start_date]],
					[[hold]].[[notion_admin_end_date]],
					[[hold]].[[notion_admin_interaction_mask]] AS [[interaction_mask]],
					[[policy]].[[version]] AS [[hold_revision]],
					[[revision]].[[revision]] AS [[interaction_revision]]
				FROM {{leafen_organization_legal_holds}} AS [[hold]]
				JOIN {{leafen_organization_policy_state}} AS [[policy]]
					ON [[policy]].[[organization_id]] = [[hold]].[[organization_id]]
				JOIN {{leafen_workspaces}} AS [[workspace]]
					ON [[workspace]].[[id]] = {:workspaceId}
					AND [[workspace]].[[organization]] = [[hold]].[[organization_id]]
				JOIN {{leafen_page_interaction_revisions}} AS [[revision]]
					ON [[revision]].[[workspace_id]] = [[workspace]].[[id]]
				WHERE [[hold]].[[id]] = {:holdId}
					AND [[hold]].[[organization_id]] = {:organizationId}
			), [[page]] AS (
				SELECT [[candidate]].[[page_id]] FROM (
					SELECT [[interaction]].[[page_id]]
					FROM [[authority]]
					JOIN {{leafen_organization_legal_hold_scopes}} AS [[scope]]
						ON [[scope]].[[hold_id]] = [[authority]].[[hold_id]]
						AND [[scope]].[[organization_id]] = {:organizationId}
						AND [[scope]].[[kind]] = {:custodianKind}
					JOIN {{leafen_page_interactions}} AS [[interaction]]
						INDEXED BY [[idx_leafen_page_interactions_page_window]]
						ON [[interaction]].[[workspace_id]] = {:workspaceId}
						AND [[interaction]].[[user_id]] = [[scope]].[[target_id]]
					WHERE [[authority]].[[notion_admin_version]] = {:adminVersion}
						AND [[interaction]].[[page_id]] > {:afterPageId}
						AND [[interaction]].[[occurred_at]] >=
							CAST([[authority]].[[notion_admin_start_date]] * 1000 AS INTEGER)
						AND ([[authority]].[[notion_admin_end_date]] IS NULL OR
							[[interaction]].[[occurred_at]] <=
								CAST([[authority]].[[notion_admin_end_date]] * 1000 AS INTEGER))
						AND (([[interaction]].[[interaction_type]] = {:createdType}
								AND ([[authority]].[[interaction_mask]] & 1) != 0)
							OR ([[interaction]].[[interaction_type]] = {:editedType}
								AND ([[authority]].[[interaction_mask]] & 2) != 0)
							OR ([[interaction]].[[interaction_type]] = {:viewedType}
								AND ([[authority]].[[interaction_mask]] & 4) != 0))
					UNION
					SELECT [[native_page]].[[id]]
					FROM [[authority]]
					JOIN {{leafen_organization_legal_hold_scopes}} AS [[scope]]
						ON [[scope]].[[hold_id]] = [[authority]].[[hold_id]]
						AND [[scope]].[[organization_id]] = {:organizationId}
					JOIN {{leafen_pages}} AS [[native_page]]
						ON [[native_page]].[[workspace]] = {:workspaceId} AND [[native_page]].[[data_source_id]] = ''
					WHERE [[authority]].[[notion_admin_version]] = 0
						AND ([[scope]].[[kind]] = {:allKind} OR
							([[scope]].[[kind]] = {:workspaceKind}
								AND [[scope]].[[target_id]] = {:workspaceId}))
						AND [[native_page]].[[id]] > {:afterPageId}
					UNION
					SELECT [[native_page]].[[id]]
					FROM [[authority]]
					JOIN {{leafen_organization_legal_hold_scopes}} AS [[scope]]
						ON [[scope]].[[hold_id]] = [[authority]].[[hold_id]]
						AND [[scope]].[[organization_id]] = {:organizationId}
						AND [[scope]].[[kind]] = {:pageKind}
					JOIN {{leafen_pages}} AS [[native_page]]
						ON [[native_page]].[[id]] = [[scope]].[[target_id]]
						AND [[native_page]].[[workspace]] = {:workspaceId}
					WHERE [[authority]].[[notion_admin_version]] = 0
						AND [[native_page]].[[id]] > {:afterPageId}
					UNION
					SELECT [[native_page]].[[id]]
					FROM [[authority]]
					JOIN {{leafen_organization_legal_hold_scopes}} AS [[scope]]
						ON [[scope]].[[hold_id]] = [[authority]].[[hold_id]]
						AND [[scope]].[[organization_id]] = {:organizationId}
						AND [[scope]].[[kind]] = {:custodianKind}
					JOIN {{leafen_pages}} AS [[native_page]]
						ON [[native_page]].[[workspace]] = {:workspaceId} AND [[native_page]].[[data_source_id]] = ''
						AND ([[native_page]].[[created_by]] = [[scope]].[[target_id]] OR
							[[native_page]].[[last_edited_by]] = [[scope]].[[target_id]])
					WHERE [[authority]].[[notion_admin_version]] = 0
						AND [[native_page]].[[id]] > {:afterPageId}
					UNION
					SELECT [[block]].[[page_id]]
					FROM [[authority]]
					JOIN {{leafen_organization_legal_hold_scopes}} AS [[scope]]
						ON [[scope]].[[hold_id]] = [[authority]].[[hold_id]]
						AND [[scope]].[[organization_id]] = {:organizationId}
						AND [[scope]].[[kind]] = {:custodianKind}
					JOIN {{leafen_blocks}} AS [[block]]
						ON [[block]].[[workspace_id]] = {:workspaceId}
						AND ([[block]].[[created_by]] = [[scope]].[[target_id]] OR
							[[block]].[[last_edited_by]] = [[scope]].[[target_id]])
					WHERE [[authority]].[[notion_admin_version]] = 0
						AND [[block]].[[page_id]] > {:afterPageId}
				) AS [[candidate]]
				ORDER BY [[candidate]].[[page_id]] LIMIT {:limit}
			)
			SELECT [[authority]].[[hold_id]], [[authority]].[[status]],
				[[authority]].[[notion_admin_version]],
				[[authority]].[[notion_admin_start_date]],
				[[authority]].[[notion_admin_end_date]], [[authority]].[[interaction_mask]],
				[[authority]].[[hold_revision]], [[authority]].[[interaction_revision]],
				[[page]].[[page_id]]
			FROM [[authority]] LEFT JOIN [[page]] ON TRUE
			ORDER BY [[page]].[[page_id]]
		`).WithContext(ctx).Bind(params).All(&rows)
		if err != nil {
			return errors.Join(leafen.ErrLegalHoldUnavailable, err)
		}
		if len(rows) == 0 {
			return errLegalHoldNotFound
		}
		first := rows[0]
		if (first.Status != leafen.LegalHoldStatusActive && first.Status != leafen.LegalHoldStatusReleased) ||
			first.HoldRevision < 0 || first.InteractionRevision < 0 ||
			(first.NotionAdminVersion != 0 &&
				(first.NotionAdminVersion != leafen.NotionAdminLegalHoldMetadataVersion ||
					first.NotionAdminStartDate < 0 || first.InteractionMask < 1 ||
					first.InteractionMask > 7 || first.NotionAdminEndDate.Valid &&
					first.NotionAdminEndDate.Float64 < first.NotionAdminStartDate)) {
			return leafen.ErrLegalHoldUnavailable
		}
		page.HoldRevision = first.HoldRevision
		page.InteractionRevision = first.InteractionRevision
		if cursor.Version != 0 &&
			(cursor.HoldRevision != page.HoldRevision ||
				cursor.InteractionRevision != page.InteractionRevision) {
			return errInvalidNotionAdminLegalHoldPagesCursor
		}
		for _, row := range rows {
			if row.HoldID != legalHoldID || row.Status != first.Status ||
				row.NotionAdminVersion != first.NotionAdminVersion ||
				row.HoldRevision != page.HoldRevision ||
				row.InteractionRevision != page.InteractionRevision {
				return leafen.ErrLegalHoldUnavailable
			}
			if row.PageID.Valid {
				page.PageIDs = append(page.PageIDs, row.PageID.String)
			}
		}
		page.HasMore = len(page.PageIDs) > leafen.LegalHoldMaxPageSize
		if page.HasMore {
			page.PageIDs = page.PageIDs[:leafen.LegalHoldMaxPageSize]
		}
		return nil
	})
	return page, err
}

func encodeNotionAdminLegalHoldPagesCursor(
	secret string,
	identity leafen.NotionAdminIdentity,
	legalHoldID, workspaceID, afterPageID string,
	holdRevision, interactionRevision int64,
) (string, error) {
	cursor := notionAdminLegalHoldPagesCursor{
		Version:        notionAdminLegalHoldPagesCursorVersion,
		OrganizationID: identity.OrganizationID, TokenID: identity.TokenID,
		LegalHoldID: legalHoldID, WorkspaceID: workspaceID,
		HoldRevision: holdRevision, InteractionRevision: interactionRevision,
		AfterPageID: afterPageID,
	}
	result, err := leafen.EncodeNotionAdminLegalHoldScopeCursor(
		secret, notionAdminLegalHoldPagesCursorPrefix,
		notionAdminLegalHoldPagesCursorDomain,
		notionAdminLegalHoldPagesCursorMaxLength, cursor,
	)
	if err != nil {
		return "", errInvalidNotionAdminLegalHoldPagesCursor
	}
	return result, nil
}

func decodeNotionAdminLegalHoldPagesCursor(
	raw, secret string,
	identity leafen.NotionAdminIdentity,
	legalHoldID, workspaceID string,
) (notionAdminLegalHoldPagesCursor, error) {
	if raw == "" {
		return notionAdminLegalHoldPagesCursor{}, nil
	}
	cursor := notionAdminLegalHoldPagesCursor{}
	if leafen.DecodeNotionAdminLegalHoldScopeCursor(
		raw, secret, notionAdminLegalHoldPagesCursorPrefix,
		notionAdminLegalHoldPagesCursorDomain,
		notionAdminLegalHoldPagesCursorMaxLength, &cursor,
	) != nil || cursor.Version != notionAdminLegalHoldPagesCursorVersion ||
		cursor.OrganizationID != identity.OrganizationID ||
		cursor.TokenID != identity.TokenID || cursor.LegalHoldID != legalHoldID ||
		cursor.WorkspaceID != workspaceID || cursor.HoldRevision < 0 ||
		cursor.InteractionRevision < 0 || cursor.AfterPageID == "" ||
		len(cursor.AfterPageID) > leafen.MaxGraphIdentifierLength ||
		strings.TrimSpace(cursor.AfterPageID) != cursor.AfterPageID {
		return notionAdminLegalHoldPagesCursor{}, errInvalidNotionAdminLegalHoldPagesCursor
	}
	return cursor, nil
}

func notionAdminLegalHoldPagesValidationError(event *core.RequestEvent, err error) error {
	return leafen.NotionAdminAPIError(
		event, http.StatusBadRequest, leafen.NotionAdminErrorValidation,
		leafen.NotionAdminValidationReason(err), err,
	)
}

func notionAdminLegalHoldPagesNotFoundError(event *core.RequestEvent, err error) error {
	return leafen.NotionAdminAPIError(
		event, http.StatusNotFound, leafen.NotionAdminErrorNotFound,
		leafen.NotionAdminNotFoundMessage, err,
	)
}
