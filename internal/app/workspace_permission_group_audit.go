package app

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// workspacePermissionGroupAuditLine is a workspace group's audit line, as the
// organization's own group lines are written (organizationSettingsAuditSubject):
// the group by the name it had then, which the log and its CSV show once it
// is renamed or gone (organizationAuditTargetNameSQL), the member it took or
// lost, and its name before and after a rename.
type workspacePermissionGroupAuditLine struct {
	organizationID, workspaceID, actorID, action string
	subjectType, subjectID, groupID, memberID    string
	from, to                                     string
	at                                           int64
}

func recordWorkspacePermissionGroupAudit(app core.App, line workspacePermissionGroupAuditLine) error {
	if line.organizationID == "" {
		return nil
	}
	name, err := workspacePermissionGroupAuditName(app, line.groupID)
	if err != nil {
		return err
	}
	metadata := map[string]any{}
	if line.to != "" {
		name = line.to
	}
	if name != "" {
		metadata["name"] = name
	}
	if line.memberID != "" {
		// By account too: the name goes from the line with the account
		// (organization_audit_gone_people.go).
		metadata["member"], metadata["memberId"] = userDisplayName(app, line.memberID), line.memberID
	}
	if line.from != "" && line.to != "" && line.from != line.to {
		metadata["from"], metadata["to"] = line.from, line.to
	}
	subjectType, subjectID := line.subjectType, line.subjectID
	if subjectType == "" {
		subjectType, subjectID = "permission_group", line.groupID
	}
	return insertOrganizationAuditEvent(app, organizationAuditEvent{
		OrganizationID: line.organizationID, WorkspaceID: line.workspaceID, ActorID: line.actorID,
		Action: line.action, TargetType: subjectType, TargetID: subjectID, Metadata: metadata, OccurredAt: line.at,
	})
}

// workspacePermissionGroupAuditName is a group's name, a deleted group's too.
func workspacePermissionGroupAuditName(app core.App, groupID string) (string, error) {
	name := ""
	err := app.DB().NewQuery(`SELECT [[name]] FROM {{leafen_workspace_permission_groups}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": strings.TrimSpace(groupID)}).Row(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return name, err
}

// recordWorkspacePermissionGroupChangeAudit writes a group change's lines: a
// rename or a new icon as the group's update, and each member it took or
// lost as a line of its own, as the organization's groups write theirs.
func recordWorkspacePermissionGroupChangeAudit(
	app core.App, access workspaceAuthorization, actorID, groupID string,
	before workspacePermissionGroupDetail, name string, iconChanged bool, add, remove []string, now int64,
) error {
	line := workspacePermissionGroupAuditLine{
		organizationID: access.Organization.OrganizationID, workspaceID: access.WorkspaceID,
		actorID: actorID, groupID: groupID, at: now,
	}
	had := map[string]bool{}
	for _, member := range before.Members {
		had[member.UserID] = true
	}
	renamed := name != "" && name != before.Name
	wrote := false
	if renamed || iconChanged {
		update := line
		update.action = "workspace.permission_group.update"
		if renamed {
			update.from, update.to = before.Name, name
		}
		if err := recordWorkspacePermissionGroupAudit(app, update); err != nil {
			return err
		}
		wrote = true
	}
	for _, change := range []struct {
		action string
		users  []string
		want   bool
	}{
		{"workspace.permission_group.member_add", add, false},
		{"workspace.permission_group.member_remove", remove, true},
	} {
		for _, userID := range change.users {
			if had[userID] != change.want {
				continue
			}
			member := line
			member.action, member.memberID = change.action, userID
			if err := recordWorkspacePermissionGroupAudit(app, member); err != nil {
				return err
			}
			wrote = true
		}
	}
	if wrote {
		return nil
	}
	line.action = "workspace.permission_group.update"
	return recordWorkspacePermissionGroupAudit(app, line)
}
