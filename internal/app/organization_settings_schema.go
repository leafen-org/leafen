package app

import (
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	organizationGroupsTable           = "leafen_organization_groups"
	organizationGroupMembersTable     = "leafen_organization_group_members"
	organizationSettingsReceiptsTable = "leafen_organization_settings_receipts"
)

func readOrganizationSettingsRevision(app core.App, organizationID string) (int, error) {
	var revision int
	err := app.DB().NewQuery(`SELECT [[settings_revision]] FROM {{leafen_organization_policy_state}}
		WHERE [[organization_id]]={:organizationId}`).Bind(
		dbx.Params{"organizationId": organizationID},
	).Row(&revision)
	return revision, err
}

func bumpOrganizationSettingsRevision(
	app core.App, organizationID string, expectedRevision int,
) (int, error) {
	result, err := app.DB().NewQuery(`UPDATE {{leafen_organization_policy_state}}
		SET [[settings_revision]]=[[settings_revision]]+1
		WHERE [[organization_id]]={:organizationId} AND [[settings_revision]]={:expectedRevision}`).Bind(dbx.Params{
		"organizationId": organizationID, "expectedRevision": expectedRevision,
	}).Execute()
	if err != nil {
		return 0, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if changed != 1 {
		return 0, errors.New(errorCodeOrganizationSettingsConflict)
	}
	return expectedRevision + 1, nil
}
