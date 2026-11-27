package organization

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedLegalHold struct {
	ID                           string          `db:"id"`
	OrganizationID               string          `db:"organization_id"`
	Name                         string          `db:"name"`
	Reason                       string          `db:"reason"`
	Status                       string          `db:"status"`
	CreatedBy                    string          `db:"created_by"`
	CreatedAt                    int64           `db:"created_at"`
	ReleasedBy                   string          `db:"released_by"`
	ReleasedAt                   int64           `db:"released_at"`
	NotionAdminVersion           int             `db:"notion_admin_version"`
	NotionAdminStartDate         float64         `db:"notion_admin_start_date"`
	NotionAdminEndDate           sql.NullFloat64 `db:"notion_admin_end_date"`
	NotionAdminInteractionMask   int             `db:"notion_admin_interaction_mask"`
	NotionAdminDescription       sql.NullString  `db:"notion_admin_description"`
	NotionAdminIcon              sql.NullString  `db:"notion_admin_icon"`
	NotionAdminTokenID           string          `db:"notion_admin_token_id"`
	NotionAdminNativeDescription sql.NullString  `db:"notion_admin_native_description"`
	NotionAdminNativeIcon        sql.NullString  `db:"notion_admin_native_icon"`
}

type storedLegalHoldScope struct {
	HoldID         string `db:"hold_id"`
	OrganizationID string `db:"organization_id"`
	Kind           string `db:"kind"`
	TargetID       string `db:"target_id"`
}

type storedLegalHoldMutation struct {
	RequestHash string `db:"request_hash"`
	HoldID      string `db:"hold_id"`
}

func loadLegalHoldMutation(
	app core.App,
	organizationID, actorID, operation, mutationID string,
) (storedLegalHoldMutation, bool, error) {
	row := storedLegalHoldMutation{}
	err := app.DB().NewQuery(`
		SELECT [[request_hash]], [[hold_id]]
		FROM {{leafen_organization_legal_hold_mutations}}
		WHERE [[organization_id]] = {:organizationId} AND [[actor_id]] = {:actorId}
			AND [[operation]] = {:operation} AND [[mutation_id]] = {:mutationId}
	`).Bind(dbx.Params{
		"organizationId": organizationID, "actorId": actorID,
		"operation": operation, "mutationId": mutationID,
	}).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return storedLegalHoldMutation{}, false, nil
	}
	if err != nil {
		return storedLegalHoldMutation{}, false, errors.Join(leafen.ErrLegalHoldUnavailable, err)
	}
	return row, true, nil
}

func insertLegalHoldMutation(
	app core.App,
	organizationID, actorID, operation, mutationID, requestHash, holdID string,
	now int64,
) error {
	_, err := app.DB().Insert(leafen.OrganizationLegalHoldMutationsTable, dbx.Params{
		"organization_id": organizationID, "actor_id": actorID, "operation": operation,
		"mutation_id": mutationID, "request_hash": requestHash, "hold_id": holdID,
		"created_at": now,
	}).Execute()
	return err
}

func insertLegalHold(
	app core.App,
	hold leafen.LegalHoldResponse,
	scope leafen.LegalHoldScopeResponse,
	metadata *notionAdminLegalHoldMetadata,
) error {
	params := dbx.Params{
		"id": hold.ID, "organization_id": hold.OrganizationID, "name": hold.Name,
		"reason": hold.Reason, "status": hold.Status, "created_by": hold.CreatedBy,
		"created_at": hold.CreatedAt, "released_by": "", "released_at": 0,
		"notion_admin_version": 0, "notion_admin_start_date": 0,
		"notion_admin_end_date": nil, "notion_admin_interaction_mask": 0,
		"notion_admin_description": nil, "notion_admin_icon": nil,
		"notion_admin_token_id":           "",
		"notion_admin_native_description": nil, "notion_admin_native_icon": nil,
	}
	if metadata != nil {
		params["notion_admin_version"] = metadata.Version
		params["notion_admin_start_date"] = metadata.StartDate
		params["notion_admin_end_date"] = metadata.EndDate
		params["notion_admin_interaction_mask"] = metadata.InteractionMask
		params["notion_admin_description"] = metadata.Description
		params["notion_admin_icon"] = metadata.Icon
		params["notion_admin_token_id"] = metadata.TokenID
	}
	if _, err := app.DB().Insert(leafen.OrganizationLegalHoldsTable, params).Execute(); err != nil {
		return err
	}
	rows := legalHoldScopeRows(hold.ID, hold.OrganizationID, scope)
	scopeParams := dbx.Params{}
	values := make([]string, 0, len(rows))
	for index, row := range rows {
		prefix := fmt.Sprintf("scope%d", index)
		scopeParams[prefix+"Hold"] = row.HoldID
		scopeParams[prefix+"Organization"] = row.OrganizationID
		scopeParams[prefix+"Kind"] = row.Kind
		scopeParams[prefix+"Target"] = row.TargetID
		values = append(values, fmt.Sprintf(
			"({:%sHold}, {:%sOrganization}, {:%sKind}, {:%sTarget})",
			prefix, prefix, prefix, prefix,
		))
	}
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_organization_legal_hold_scopes}}
			([[hold_id]], [[organization_id]], [[kind]], [[target_id]])
		VALUES ` + strings.Join(values, ", ")).Bind(scopeParams).Execute(); err != nil {
		return err
	}
	return nil
}

func validateLegalHoldScopeTargets(
	app core.App,
	organizationID string,
	scope leafen.LegalHoldScopeResponse,
) error {
	// Each names the field it reads, for a refusal to say which: a person by
	// the ID GET /organizations/{organizationId}/people lists (or any form
	// the Notion-compatible tools give, which reaches here as that ID).
	checks := []struct {
		ids     []string
		query   string
		refusal string
	}{
		{
			refusal: "scope.workspaceIds names a workspace that isn't in this organization.",
			ids:     scope.WorkspaceIDs,
			query: `SELECT COUNT(*) FROM {{leafen_workspaces}}
				WHERE [[organization]] = {:organizationId} AND [[id]] IN (%s)`,
		},
		{
			refusal: "scope.pageIds names a page that isn't in this organization's workspaces.",
			ids:     scope.PageIDs,
			query: `SELECT COUNT(*) FROM {{leafen_pages}} [[page]]
				JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = [[page]].[[workspace]]
				WHERE [[workspace]].[[organization]] = {:organizationId} AND [[page]].[[id]] IN (%s)`,
		},
		{
			refusal: "scope.userIds names someone who isn't in this organization (GET /organizations/{organizationId}/people lists its people).",
			ids:     scope.CustodianUserIDs,
			query: `SELECT COUNT(*) FROM {{leafen_organization_memberships}}
				WHERE [[organization]] = {:organizationId} AND [[user]] IN (%s)`,
		},
	}
	for index, check := range checks {
		if len(check.ids) == 0 {
			continue
		}
		params := dbx.Params{"organizationId": organizationID}
		placeholders := bindLegalHoldIDs(params, fmt.Sprintf("target%d", index), check.ids)
		var count int
		if err := app.DB().NewQuery(fmt.Sprintf(
			check.query, strings.Join(placeholders, ", "),
		)).Bind(params).Row(&count); err != nil {
			return errors.Join(leafen.ErrLegalHoldUnavailable, err)
		}
		if count != len(check.ids) {
			return errors.Join(leafen.NotionAdminInvalid(errLegalHoldInvalid, check.refusal), errLegalHoldTargetNotFound)
		}
	}
	return nil
}

// bindLegalHoldIDs binds the IDs as one JSON list, however many, and gives
// what an IN (...) reads them by.
func bindLegalHoldIDs(params dbx.Params, prefix string, ids []string) []string {
	encoded, _ := json.Marshal(ids)
	params[prefix] = string(encoded)
	return []string{"SELECT [[value]] FROM json_each({:" + prefix + "})"}
}

func markLegalHoldReleased(
	app core.App,
	organizationID, holdID, actorID string,
	now int64,
) error {
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_organization_legal_holds}}
		SET [[status]] = {:released}, [[released_by]] = {:actorId}, [[released_at]] = {:now}
		WHERE [[id]] = {:holdId} AND [[organization_id]] = {:organizationId}
			AND [[status]] = {:active}
	`).Bind(dbx.Params{
		"released": leafen.LegalHoldStatusReleased, "active": leafen.LegalHoldStatusActive,
		"actorId": actorID, "now": now, "holdId": holdID, "organizationId": organizationID,
	}).Execute()
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return errors.Join(leafen.ErrLegalHoldUnavailable, err)
	}
	if affected != 1 {
		return errLegalHoldInvalid
	}
	return nil
}

func findDirectActiveLegalHoldScope(
	app core.App,
	organizationID string,
	target leafen.LegalHoldDeletionTarget,
) (storedLegalHoldScope, bool, error) {
	params := dbx.Params{
		"organizationId": organizationID, "active": leafen.LegalHoldStatusActive,
		"allKind": leafen.LegalHoldScopeAll, "workspaceKind": leafen.LegalHoldScopeWorkspace,
		"workspaceId": target.WorkspaceID,
	}
	conditions := []string{
		`([[scope]].[[kind]] = {:allKind} AND [[scope]].[[target_id]] = '')`,
		`([[scope]].[[kind]] = {:workspaceKind} AND [[scope]].[[target_id]] = {:workspaceId})`,
	}
	if len(target.PageIDs) > 0 {
		params["pageKind"] = leafen.LegalHoldScopePage
		conditions = append(conditions,
			`([[scope]].[[kind]] = {:pageKind} AND [[scope]].[[target_id]] IN (`+
				strings.Join(bindLegalHoldIDs(params, "page", target.PageIDs), ", ")+`))`,
		)
	}
	if len(target.CustodianUserIDs) > 0 {
		params["custodianKind"] = leafen.LegalHoldScopeCustodian
		conditions = append(conditions,
			`([[scope]].[[kind]] = {:custodianKind} AND [[scope]].[[target_id]] IN (`+
				strings.Join(bindLegalHoldIDs(params, "custodian", target.CustodianUserIDs), ", ")+`))`,
		)
	}
	return loadActiveLegalHoldScopeMatch(app, `
		SELECT [[scope]].[[hold_id]], [[scope]].[[organization_id]],
			[[scope]].[[kind]], [[scope]].[[target_id]]
		FROM {{leafen_organization_legal_hold_scopes}} [[scope]]
		JOIN {{leafen_organization_legal_holds}} [[hold]]
			ON [[hold]].[[id]] = [[scope]].[[hold_id]]
			AND [[hold]].[[organization_id]] = [[scope]].[[organization_id]]
		WHERE [[hold]].[[organization_id]] = {:organizationId}
			AND [[hold]].[[status]] = {:active}
			AND (`+strings.Join(conditions, " OR ")+`)
		ORDER BY [[hold]].[[created_at]], [[hold]].[[id]], [[scope]].[[kind]], [[scope]].[[target_id]]
		LIMIT 1
	`, params)
}

func findContainedActiveLegalHoldScope(
	app core.App,
	organizationID, workspaceID string,
) (storedLegalHoldScope, bool, error) {
	return loadActiveLegalHoldScopeMatch(app, `
		SELECT [[scope]].[[hold_id]], [[scope]].[[organization_id]],
			[[scope]].[[kind]], [[scope]].[[target_id]]
		FROM {{leafen_organization_legal_hold_scopes}} [[scope]]
		JOIN {{leafen_organization_legal_holds}} [[hold]]
			ON [[hold]].[[id]] = [[scope]].[[hold_id]]
			AND [[hold]].[[organization_id]] = [[scope]].[[organization_id]]
		WHERE [[hold]].[[organization_id]] = {:organizationId}
			AND [[hold]].[[status]] = {:active}
			AND (
				([[scope]].[[kind]] = {:pageKind} AND EXISTS (
					SELECT 1 FROM {{leafen_pages}} [[page]]
					WHERE [[page]].[[id]] = [[scope]].[[target_id]]
						AND [[page]].[[workspace]] = {:workspaceId}
				))
				OR ([[scope]].[[kind]] = {:custodianKind} AND EXISTS (
					SELECT 1 FROM {{leafen_pages}} [[page]]
					WHERE [[page]].[[owner]] = [[scope]].[[target_id]]
						AND [[page]].[[workspace]] = {:workspaceId}
				))
			)
		ORDER BY [[hold]].[[created_at]], [[hold]].[[id]], [[scope]].[[kind]], [[scope]].[[target_id]]
		LIMIT 1
	`, dbx.Params{
		"organizationId": organizationID, "workspaceId": workspaceID,
		"active": leafen.LegalHoldStatusActive, "pageKind": leafen.LegalHoldScopePage,
		"custodianKind": leafen.LegalHoldScopeCustodian,
	})
}

func loadActiveLegalHoldScopeMatch(
	app core.App,
	query string,
	params dbx.Params,
) (storedLegalHoldScope, bool, error) {
	row := storedLegalHoldScope{}
	err := app.DB().NewQuery(query).Bind(params).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return storedLegalHoldScope{}, false, nil
	}
	if err != nil {
		return storedLegalHoldScope{}, false, errors.Join(leafen.ErrLegalHoldUnavailable, err)
	}
	if len(row.HoldID) != 32 || row.OrganizationID == "" ||
		(row.Kind == leafen.LegalHoldScopeAll && row.TargetID != "") ||
		(row.Kind != leafen.LegalHoldScopeAll && (row.TargetID == "" ||
			row.TargetID != strings.TrimSpace(row.TargetID) ||
			len([]rune(row.TargetID)) > leafen.LegalHoldMaxScopeIDRunes)) {
		return storedLegalHoldScope{}, false, leafen.ErrLegalHoldUnavailable
	}
	return row, true, nil
}
