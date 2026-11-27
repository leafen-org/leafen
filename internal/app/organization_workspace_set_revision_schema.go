package app

import (
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func bumpOrganizationWorkspaceSetRevision(app core.App, organizationID string) error {
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_organization_policy_state}}
		SET [[workspace_revision]] = [[workspace_revision]] + 1
		WHERE [[organization_id]] = {:organizationId}
	`).Bind(dbx.Params{"organizationId": organizationID}).Execute()
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return errors.New(errorCodeOrganizationAuthorityInvalid)
	}
	return nil
}
