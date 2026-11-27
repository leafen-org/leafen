package app

import (
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type administratorOrganizationOwnershipTransfer struct {
	ActorID         string
	PreviousOwnerID string
	OwnerID         string
	OrganizationID  string
}

func validateAdministratorAccountCleanupSuccessor(
	app core.App,
	targetUserID string,
	selection administratorAccountTransferSelection,
) error {
	if selection.SuccessorUserID == "" || selection.SuccessorUserID == targetUserID || selection.ResourceID == "" {
		return errAdministratorAccountTransferInvalid
	}
	allowed, err := administratorAccountCanAuthenticate(app, selection.SuccessorUserID)
	if err != nil || !allowed {
		return errors.Join(errAdministratorAccountTransferInvalid, err)
	}
	switch selection.ResourceType {
	case administratorAccountResourceWorkspace:
		var count int
		err = app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_workspaces}} [[workspace]]
			WHERE [[workspace]].[[id]]={:resourceId} AND [[workspace]].[[workspace_owner]]={:targetId}
				AND [[workspace]].[[deletion_job_id]]=''`).Bind(dbx.Params{
			"resourceId": selection.ResourceID, "targetId": targetUserID,
		}).Row(&count)
		if err != nil || count != 1 {
			return errors.Join(errAdministratorAccountTransferInvalid, err)
		}
	case administratorAccountResourceOrganization:
		var eligible int
		err = app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_organizations}} [[organization]]
			WHERE [[organization]].[[id]]={:resourceId} AND [[organization]].[[owner]]={:targetId}
		`).Bind(dbx.Params{
			"resourceId": selection.ResourceID, "targetId": targetUserID,
		}).Row(&eligible)
		if err != nil || eligible != 1 {
			return errors.Join(errAdministratorAccountTransferInvalid, err)
		}
		// One person owns at most one organization, so a successor who owns
		// one already is refused here, before anything is moved or signed out.
		var owned int
		if err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_organizations}} WHERE [[owner]]={:successorId}`).Bind(dbx.Params{
			"successorId": selection.SuccessorUserID,
		}).Row(&owned); err != nil {
			return err
		}
		if owned != 0 {
			return errAdministratorAccountTransferInvalid
		}
	default:
		return errAdministratorAccountTransferInvalid
	}
	return nil
}

func transferOrganizationOwnershipInTransaction(app core.App, input administratorOrganizationOwnershipTransfer) error {
	selection := administratorAccountTransferSelection{
		ResourceType: administratorAccountResourceOrganization,
		ResourceID:   input.OrganizationID, SuccessorUserID: input.OwnerID,
	}
	if err := validateAdministratorAccountCleanupSuccessor(app, input.PreviousOwnerID, selection); err != nil {
		return err
	}
	memberships, err := app.FindCollectionByNameOrId(organizationMembershipsCollection)
	if err != nil {
		return err
	}
	previous, err := app.FindFirstRecordByFilter(memberships,
		"organization = {:organization} && user = {:user}",
		dbx.Params{"organization": input.OrganizationID, "user": input.PreviousOwnerID})
	if err != nil {
		return err
	}
	successor, err := app.FindFirstRecordByFilter(memberships,
		"organization = {:organization} && user = {:user}",
		dbx.Params{"organization": input.OrganizationID, "user": input.OwnerID})
	if errors.Is(err, sql.ErrNoRows) {
		successor = core.NewRecord(memberships)
		successor.Set("organization", input.OrganizationID)
		successor.Set("user", input.OwnerID)
	} else if err != nil {
		return err
	}
	previous.Set("role", organizationRoleMember)
	previous.Set("status", organizationMemberStatusActive)
	successor.Set("role", organizationRoleOwner)
	successor.Set("status", organizationMemberStatusActive)
	if err := app.Save(previous); err != nil {
		return err
	}
	if err := app.Save(successor); err != nil {
		return err
	}
	organization, err := app.FindRecordById(organizationsCollection, input.OrganizationID)
	if err != nil {
		return err
	}
	organization.Set("owner", input.OwnerID)
	if err := app.Save(organization); err != nil {
		return err
	}
	return recordOrganizationAuditEvent(app, organizationAuditEvent{
		OrganizationID: input.OrganizationID, ActorID: input.ActorID,
		Action: "organization_owner.transfer", TargetType: "organization_owner", TargetID: input.OwnerID,
		Metadata: map[string]any{"previousOwnerId": input.PreviousOwnerID}, OccurredAt: time.Now().UTC().UnixMilli(),
	})
}

func administratorAccountOwnershipMatches(app core.App, jobID, targetUserID string) (bool, error) {
	counts := struct {
		Missing  int `db:"missing"`
		Obsolete int `db:"obsolete"`
	}{}
	err := app.DB().NewQuery(`
		SELECT
			(SELECT COUNT(*) FROM (
				SELECT 'workspace' AS [[resource_type]],[[id]] AS [[resource_id]] FROM {{leafen_workspaces}} WHERE [[workspace_owner]]={:targetId}
				UNION ALL SELECT 'organization',[[id]] FROM {{leafen_organizations}} WHERE [[owner]]={:targetId}
			) [[owned]] WHERE NOT EXISTS (
				SELECT 1 FROM {{leafen_administrator_account_cleanup_assignments}} [[assignment]]
				WHERE [[assignment]].[[job_id]]={:jobId} AND [[assignment]].[[resource_type]]=[[owned]].[[resource_type]]
					AND [[assignment]].[[resource_id]]=[[owned]].[[resource_id]]
			)) AS [[missing]],
			(SELECT COUNT(*) FROM {{leafen_administrator_account_cleanup_assignments}} [[assignment]]
			 WHERE [[assignment]].[[job_id]]={:jobId} AND [[assignment]].[[status]]<>'completed' AND NOT EXISTS (
				SELECT 1 FROM {{leafen_workspaces}} [[workspace]] WHERE [[assignment]].[[resource_type]]='workspace'
					AND [[workspace]].[[id]]=[[assignment]].[[resource_id]] AND [[workspace]].[[workspace_owner]]={:targetId}
				UNION ALL SELECT 1 FROM {{leafen_organizations}} [[organization]] WHERE [[assignment]].[[resource_type]]='organization'
					AND [[organization]].[[id]]=[[assignment]].[[resource_id]] AND [[organization]].[[owner]]={:targetId}
			 )) AS [[obsolete]]
	`).Bind(dbx.Params{"jobId": jobID, "targetId": targetUserID}).One(&counts)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil && counts.Missing == 0 && counts.Obsolete == 0, err
}
