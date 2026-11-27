package organization

import (
	"context"
	"errors"
	"sort"
	"strings"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type notionAdminLegalHoldPage struct {
	Rows         []storedLegalHold
	Items        []leafen.LegalHoldResponse
	WorkspaceIDs []string
	HasMore      bool
}

func getNotionAdminLegalHold(
	ctx context.Context,
	app core.App,
	identity leafen.NotionAdminIdentity,
	legalHoldID string,
) (notionAdminLegalHoldResponse, error) {
	response := notionAdminLegalHoldResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		rows, err := loadStoredLegalHolds(
			txApp, identity.OrganizationID, "", 0, legalHoldID,
		)
		if err != nil {
			return err
		}
		if len(rows) != 1 {
			return errLegalHoldNotFound
		}
		items, err := hydrateLegalHolds(txApp, rows)
		if err != nil {
			return err
		}
		workspaceIDs := []string{}
		_, adminCreated, err := notionAdminLegalHoldMetadataFromStored(rows[0])
		if err != nil {
			return err
		}
		if adminCreated {
			workspaceIDs, err = loadNotionAdminWorkspaceIDs(ctx, txApp, identity)
			if err != nil {
				return err
			}
		}
		response, err = notionAdminLegalHoldProjection(
			items[0], rows[0], identity, workspaceIDs,
		)
		return err
	})
	return response, err
}

func listNotionAdminLegalHolds(
	ctx context.Context,
	app core.App,
	identity leafen.NotionAdminIdentity,
	userID string,
	cursor notionAdminLegalHoldCursor,
) (notionAdminLegalHoldPage, error) {
	page := notionAdminLegalHoldPage{}
	err := app.RunInTransaction(func(txApp core.App) error {
		params := dbx.Params{
			"organizationId": identity.OrganizationID,
			"limit":          leafen.LegalHoldMaxPageSize + 1,
		}
		query := `
			SELECT [[id]], [[organization_id]], [[name]], [[reason]], [[status]],
				[[created_by]], [[created_at]], [[released_by]], [[released_at]],
				[[notion_admin_version]], [[notion_admin_start_date]],
				[[notion_admin_end_date]], [[notion_admin_interaction_mask]],
				[[notion_admin_description]], [[notion_admin_icon]],
				[[notion_admin_token_id]], [[notion_admin_native_description]],
				[[notion_admin_native_icon]]
			FROM {{leafen_organization_legal_holds}} [[hold]]
			WHERE [[organization_id]] = {:organizationId}`

		allowedIDs, wildcard := notionAdminLegalHoldAccess(identity)
		if !wildcard {
			if len(allowedIDs) == 0 {
				query += ` AND 1 = 0`
			} else {
				placeholders := bindLegalHoldIDs(params, "allowedHold", allowedIDs)
				query += ` AND [[id]] IN (` + strings.Join(placeholders, ", ") + `)`
			}
		}
		if userID != "" {
			params["userId"], params["userKind"] = userID, leafen.LegalHoldScopeCustodian
			query += ` AND EXISTS (
				SELECT 1 FROM {{leafen_organization_legal_hold_scopes}} [[scope]]
				WHERE [[scope]].[[hold_id]] = [[hold]].[[id]]
					AND [[scope]].[[organization_id]] = [[hold]].[[organization_id]]
					AND [[scope]].[[kind]] = {:userKind}
					AND [[scope]].[[target_id]] = {:userId}
			)`
		}
		if cursor.AfterHoldID != "" {
			params["afterCreatedAt"] = cursor.AfterCreatedAt
			params["afterHoldId"] = cursor.AfterHoldID
			query += ` AND ([[created_at]] > {:afterCreatedAt}
				OR ([[created_at]] = {:afterCreatedAt} AND [[id]] > {:afterHoldId}))`
		}
		query += ` ORDER BY [[created_at]], [[id]] LIMIT {:limit}`

		rows := []storedLegalHold{}
		if err := txApp.DB().NewQuery(query).WithContext(ctx).Bind(params).All(&rows); err != nil {
			return errors.Join(leafen.ErrLegalHoldUnavailable, err)
		}
		page.HasMore = len(rows) > leafen.LegalHoldMaxPageSize
		if page.HasMore {
			rows = rows[:leafen.LegalHoldMaxPageSize]
		}
		items, err := hydrateLegalHolds(txApp, rows)
		if err != nil {
			return err
		}
		needsAdminWorkspaces := false
		for _, row := range rows {
			_, adminCreated, err := notionAdminLegalHoldMetadataFromStored(row)
			if err != nil {
				return err
			}
			needsAdminWorkspaces = needsAdminWorkspaces || adminCreated
		}
		workspaceIDs := []string{}
		if needsAdminWorkspaces {
			workspaceIDs, err = loadNotionAdminWorkspaceIDs(ctx, txApp, identity)
			if err != nil {
				return err
			}
		}
		page.Rows, page.Items, page.WorkspaceIDs = rows, items, workspaceIDs
		return nil
	})
	return page, err
}

func notionAdminLegalHoldAccess(identity leafen.NotionAdminIdentity) ([]string, bool) {
	if _, wildcard := identity.LegalHoldIDs()["*"]; wildcard {
		return nil, true
	}
	ids := make([]string, 0, len(identity.LegalHoldIDs()))
	for id := range identity.LegalHoldIDs() {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, false
}
