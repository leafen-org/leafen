package app

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// organizationSettingsAuditSubject is what an organization settings change's
// audit line names, read before the change (2026-11-12: a group's update,
// deletion and member lines named only who did them, and a deleted group's
// line had nothing left to name it by): the group or member it touched, the
// member a group took or lost, and the member's role before — a member by
// account too, so their name goes from the line with their account
// (organization_audit_gone_people.go).
type organizationSettingsAuditSubject struct {
	name, member, role string
	userID, memberID   string
}

func readOrganizationSettingsAuditSubject(
	app core.App, organizationID string, input organizationSettingsMutationInput,
) (organizationSettingsAuditSubject, error) {
	subject := organizationSettingsAuditSubject{}
	var err error
	switch input.Action {
	case organizationSettingsActionGroupUpdate, organizationSettingsActionGroupDelete:
		subject.name, err = organizationSettingsAuditGroupName(app, organizationID, input.TargetID)
	case organizationSettingsActionGroupMemberAdd, organizationSettingsActionGroupMemberRemove:
		if subject.name, err = organizationSettingsAuditGroupName(app, organizationID, input.TargetID); err == nil {
			subject.member, _, subject.memberID, err = organizationSettingsAuditMember(app, organizationID, input.SecondaryTargetID)
		}
	case organizationSettingsActionMemberDeactivate, organizationSettingsActionMemberReactivate,
		organizationSettingsActionMemberRemove, organizationSettingsActionMemberRole,
		organizationSettingsActionOwnerTransfer, organizationSettingsActionAdminRoleAdd,
		organizationSettingsActionAdminRoleRemove:
		subject.name, subject.role, subject.userID, err = organizationSettingsAuditMember(app, organizationID, input.TargetID)
	}
	return subject, err
}

func organizationSettingsAuditGroupName(app core.App, organizationID, groupID string) (string, error) {
	name := ""
	err := app.DB().NewQuery(`SELECT [[name]] FROM {{leafen_organization_groups}}
		WHERE [[id]]={:id} AND [[organization_id]]={:organizationId}`).Bind(dbx.Params{
		"id": strings.TrimSpace(groupID), "organizationId": organizationID,
	}).Row(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return name, err
}

// organizationSettingsAuditMember is a membership's person by name, as the
// organization's members list names them, their role and their account.
func organizationSettingsAuditMember(app core.App, organizationID, membershipID string) (string, string, string, error) {
	row := struct {
		Name string `db:"name"`
		Role string `db:"role"`
		User string `db:"user"`
	}{}
	err := app.DB().NewQuery(`SELECT ` + accountNameSQL("account") + ` AS [[name]], [[membership]].[[role]] AS [[role]],
		[[account]].[[id]] AS [[user]]
		FROM {{leafen_organization_memberships}} [[membership]]
		JOIN {{users}} [[account]] ON [[account]].[[id]] = [[membership]].[[user]]
		WHERE [[membership]].[[id]]={:id} AND [[membership]].[[organization]]={:organizationId}`).Bind(dbx.Params{
		"id": strings.TrimSpace(membershipID), "organizationId": organizationID,
	}).One(&row)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", "", nil
	}
	return row.Name, row.Role, row.User, err
}

// line is the target the change's audit line names and what it keeps of
// it: the name it had then, which the log and its CSV show once it is gone
// (organizationAuditTargetNameSQL), and what changed — a group's old and new
// name, the member a group took or lost, a role before and after; a person
// with their account (userId, a group's memberId).
func (subject organizationSettingsAuditSubject) line(
	input organizationSettingsMutationInput, response organizationSettingsMutationResponse,
) (string, map[string]any) {
	target := organizationSettingsMutationTarget(input)
	metadata := map[string]any{}
	switch input.Action {
	case organizationSettingsActionGroupCreate:
		if response.Group != nil {
			target, metadata["name"] = response.Group.ID, response.Group.Name
		}
	case organizationSettingsActionGroupUpdate:
		metadata["name"] = input.Value
		if subject.name != input.Value {
			metadata["from"], metadata["to"] = subject.name, input.Value
		}
	case organizationSettingsActionGroupMemberAdd, organizationSettingsActionGroupMemberRemove:
		metadata["name"], metadata["member"] = subject.name, subject.member
	case organizationSettingsActionMemberRole:
		metadata["name"], metadata["from"], metadata["to"] = subject.name, subject.role, strings.ToLower(input.Value)
	case organizationSettingsActionAdminRoleAdd:
		metadata["name"], metadata["to"] = subject.name, input.Value
	case organizationSettingsActionAdminRoleRemove:
		metadata["name"], metadata["from"] = subject.name, input.Value
	case organizationSettingsActionDomainAdd, organizationSettingsActionDomainVerify, organizationSettingsActionDomainRemove:
		metadata["name"] = target
		if response.Domain != nil && response.Domain.Domain != "" {
			metadata["name"] = response.Domain.Domain
		}
	case organizationSettingsActionWorkspaceCreationPolicy, organizationSettingsActionFeedRetention,
		organizationSettingsActionAnalyticsRetention:
		metadata["name"] = input.Value
	default:
		metadata["name"] = subject.name
	}
	if name, _ := metadata["name"].(string); name == "" {
		delete(metadata, "name")
	}
	if subject.userID != "" {
		metadata["userId"] = subject.userID
	}
	if subject.memberID != "" {
		metadata["memberId"] = subject.memberID
	}
	return target, metadata
}
