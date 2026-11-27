package organization

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func loadLegalHold(
	app core.App,
	organizationID, holdID string,
) (leafen.LegalHoldResponse, error) {
	rows, err := loadStoredLegalHolds(app, organizationID, "", 0, holdID)
	if err != nil {
		return leafen.LegalHoldResponse{}, err
	}
	if len(rows) != 1 {
		return leafen.LegalHoldResponse{}, errLegalHoldNotFound
	}
	responses, err := hydrateLegalHolds(app, rows)
	if err != nil {
		return leafen.LegalHoldResponse{}, err
	}
	return responses[0], nil
}

func listLegalHolds(
	app core.App,
	organizationID, cursor string,
) (leafen.LegalHoldListResponse, error) {
	createdAfter, idAfter, err := parseLegalHoldCursor(cursor)
	if err != nil {
		return leafen.LegalHoldListResponse{}, err
	}
	rows, err := loadStoredLegalHolds(
		app, organizationID, idAfter, createdAfter, "",
	)
	if err != nil {
		return leafen.LegalHoldListResponse{}, err
	}
	hasMore := len(rows) > leafen.LegalHoldMaxPageSize
	if hasMore {
		rows = rows[:leafen.LegalHoldMaxPageSize]
	}
	items, err := hydrateLegalHolds(app, rows)
	if err != nil {
		return leafen.LegalHoldListResponse{}, err
	}
	response := leafen.LegalHoldListResponse{Items: items}
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		response.NextCursor = fmt.Sprintf("%d:%s", last.CreatedAt, last.ID)
	}
	return response, nil
}

func loadStoredLegalHolds(
	app core.App,
	organizationID, idAfter string,
	createdAfter int64,
	exactID string,
) ([]storedLegalHold, error) {
	rows := []storedLegalHold{}
	query := `
		SELECT [[id]], [[organization_id]], [[name]], [[reason]], [[status]],
			[[created_by]], [[created_at]], [[released_by]], [[released_at]],
			[[notion_admin_version]], [[notion_admin_start_date]],
			[[notion_admin_end_date]], [[notion_admin_interaction_mask]],
			[[notion_admin_description]], [[notion_admin_icon]],
			[[notion_admin_token_id]], [[notion_admin_native_description]],
			[[notion_admin_native_icon]]
		FROM {{leafen_organization_legal_holds}}
		WHERE [[organization_id]] = {:organizationId}`
	params := dbx.Params{"organizationId": organizationID, "limit": leafen.LegalHoldMaxPageSize + 1}
	if exactID != "" {
		query += ` AND [[id]] = {:exactId}`
		params["exactId"] = exactID
	} else if idAfter != "" {
		query += ` AND ([[created_at]] > {:createdAfter}
			OR ([[created_at]] = {:createdAfter} AND [[id]] > {:idAfter}))`
		params["createdAfter"], params["idAfter"] = createdAfter, idAfter
	}
	query += ` ORDER BY [[created_at]], [[id]] LIMIT {:limit}`
	if err := app.DB().NewQuery(query).Bind(params).All(&rows); err != nil {
		return nil, errors.Join(leafen.ErrLegalHoldUnavailable, err)
	}
	return rows, nil
}

func hydrateLegalHolds(app core.App, holds []storedLegalHold) ([]leafen.LegalHoldResponse, error) {
	if len(holds) == 0 {
		return []leafen.LegalHoldResponse{}, nil
	}
	params := dbx.Params{}
	placeholders := make([]string, 0, len(holds))
	for index, hold := range holds {
		key := fmt.Sprintf("hold%d", index)
		params[key] = hold.ID
		placeholders = append(placeholders, "{:"+key+"}")
	}
	scopes := []storedLegalHoldScope{}
	if err := app.DB().NewQuery(`
		SELECT [[hold_id]], [[organization_id]], [[kind]], [[target_id]]
		FROM {{leafen_organization_legal_hold_scopes}}
		WHERE [[hold_id]] IN (` + leafen.JoinSQL(placeholders) + `)
		ORDER BY [[hold_id]], [[kind]], [[target_id]]
	`).Bind(params).All(&scopes); err != nil {
		return nil, errors.Join(leafen.ErrLegalHoldUnavailable, err)
	}
	byHold := make(map[string][]storedLegalHoldScope, len(holds))
	for _, scope := range scopes {
		byHold[scope.HoldID] = append(byHold[scope.HoldID], scope)
	}
	responses := make([]leafen.LegalHoldResponse, 0, len(holds))
	for _, hold := range holds {
		response, err := legalHoldResponseFromStored(hold, byHold[hold.ID])
		if err != nil {
			return nil, err
		}
		responses = append(responses, response)
	}
	return responses, nil
}

func legalHoldResponseFromStored(
	hold storedLegalHold,
	rows []storedLegalHoldScope,
) (leafen.LegalHoldResponse, error) {
	if len(hold.ID) != 32 || hold.OrganizationID == "" || hold.Name == "" ||
		hold.Name != strings.TrimSpace(hold.Name) || hold.Reason == "" ||
		hold.Reason != strings.TrimSpace(hold.Reason) || hold.CreatedBy == "" || hold.CreatedAt < 0 {
		return leafen.LegalHoldResponse{}, leafen.ErrLegalHoldUnavailable
	}
	if hold.Status == leafen.LegalHoldStatusActive {
		if hold.ReleasedBy != "" || hold.ReleasedAt != 0 {
			return leafen.LegalHoldResponse{}, leafen.ErrLegalHoldUnavailable
		}
	} else if hold.Status == leafen.LegalHoldStatusReleased {
		if hold.ReleasedBy == "" || hold.ReleasedAt <= 0 {
			return leafen.LegalHoldResponse{}, leafen.ErrLegalHoldUnavailable
		}
	} else {
		return leafen.LegalHoldResponse{}, leafen.ErrLegalHoldUnavailable
	}
	scope, err := legalHoldScopeFromRows(hold, rows)
	if err != nil {
		return leafen.LegalHoldResponse{}, err
	}
	return leafen.LegalHoldResponse{
		ID: hold.ID, OrganizationID: hold.OrganizationID, Name: hold.Name,
		Status: hold.Status, Reason: hold.Reason, Scope: scope,
		CreatedBy: hold.CreatedBy, CreatedAt: hold.CreatedAt,
		ReleasedBy: hold.ReleasedBy, ReleasedAt: hold.ReleasedAt,
	}, nil
}

func legalHoldScopeFromRows(
	hold storedLegalHold,
	rows []storedLegalHoldScope,
) (leafen.LegalHoldScopeResponse, error) {
	response := leafen.LegalHoldScopeResponse{
		WorkspaceIDs: []string{}, PageIDs: []string{}, CustodianUserIDs: []string{},
	}
	if len(rows) == 0 {
		return leafen.LegalHoldScopeResponse{}, leafen.ErrLegalHoldUnavailable
	}
	for _, row := range rows {
		if row.HoldID != hold.ID || row.OrganizationID != hold.OrganizationID {
			return leafen.LegalHoldScopeResponse{}, leafen.ErrLegalHoldUnavailable
		}
		switch row.Kind {
		case leafen.LegalHoldScopeAll:
			if row.TargetID != "" || len(rows) != 1 {
				return leafen.LegalHoldScopeResponse{}, leafen.ErrLegalHoldUnavailable
			}
			response.All = true
		case leafen.LegalHoldScopeWorkspace:
			response.WorkspaceIDs = append(response.WorkspaceIDs, row.TargetID)
		case leafen.LegalHoldScopePage:
			response.PageIDs = append(response.PageIDs, row.TargetID)
		case leafen.LegalHoldScopeCustodian:
			response.CustodianUserIDs = append(response.CustodianUserIDs, row.TargetID)
		default:
			return leafen.LegalHoldScopeResponse{}, leafen.ErrLegalHoldUnavailable
		}
		if row.Kind != leafen.LegalHoldScopeAll &&
			(row.TargetID == "" || row.TargetID != strings.TrimSpace(row.TargetID) || len([]rune(row.TargetID)) > leafen.LegalHoldMaxScopeIDRunes) {
			return leafen.LegalHoldScopeResponse{}, leafen.ErrLegalHoldUnavailable
		}
	}
	return response, nil
}

func parseLegalHoldCursor(cursor string) (int64, string, error) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return 0, "", nil
	}
	created, id, ok := strings.Cut(cursor, ":")
	if !ok || len(id) != 32 {
		return 0, "", leafen.NotionAdminInvalid(errLegalHoldInvalid, "start_cursor isn't one this list gave.")
	}
	createdAt, err := strconv.ParseInt(created, 10, 64)
	if err != nil || createdAt < 0 {
		return 0, "", leafen.NotionAdminInvalid(errLegalHoldInvalid, "start_cursor isn't one this list gave.")
	}
	return createdAt, id, nil
}
