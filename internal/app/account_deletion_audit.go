package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// recordAccountDeletionAudit puts an account's deletion on the audit log of
// each organization it leaves, as Notion's "User deleted" (its help
// "Workspace audit log", read 2026-11-13), as its memberships go: done by
// whoever began the deletion, and naming the account by the email it had
// then, the one line that keeps it (user decision 2026-11-07). Its
// memberships of the organizations' workspaces go with it, on no line of
// their own.
func recordAccountDeletionAudit(app core.App, jobID, userID string) error {
	organizations := []string{}
	if err := app.DB().NewQuery(`SELECT DISTINCT [[organization]] FROM {{leafen_organization_memberships}}
		WHERE [[user]]={:userId} AND [[organization]]<>''`).Bind(dbx.Params{"userId": userID}).Column(&organizations); err != nil {
		return err
	}
	return recordAccountDeletionAuditIn(app, jobID, userID, organizations)
}

// recordManagedAccountDeletionAudit puts a managed user's deletion on the
// audit log of the organization that deleted them as it begins: the one
// "사용자 삭제됨" line, Notion's "User deleted" — its log has no line of its
// own for deleting a managed user (its help "Workspace audit log", read
// 2026-11-15; 2026-11-13: the organization got "관리 대상 사용자 삭제됨" and
// then "사용자 삭제됨" for the one deletion). An organization manages people
// by their email's domain, members of it or not, so it gets the line even
// when the account cleanup finds no membership there; where it does, the
// cleanup's line is this same one (one id for the account's cleanup job).
func recordManagedAccountDeletionAudit(app core.App, organizationID, userID string) error {
	jobID := ""
	if err := app.DB().NewQuery(`SELECT [[cleanup_job_id]] FROM {{leafen_administrator_account_states}}
		WHERE [[user_id]]={:userId} AND [[status]]='deleting'`).Bind(dbx.Params{"userId": userID}).Row(&jobID); err != nil {
		return err
	}
	if jobID == "" {
		return errAdministratorAccountRevision
	}
	return recordAccountDeletionAuditIn(app, jobID, userID, []string{organizationID})
}

// recordAccountDeletionAuditIn writes an account's deletion line in each of
// the organizations, once for its cleanup job however often it is asked.
func recordAccountDeletionAuditIn(app core.App, jobID, userID string, organizations []string) error {
	if len(organizations) == 0 {
		return nil
	}
	actorID := ""
	err := app.DB().NewQuery(`SELECT [[updated_by]] FROM {{leafen_administrator_account_states}}
		WHERE [[user_id]]={:userId} AND [[status]]='deleting'`).Bind(dbx.Params{"userId": userID}).Row(&actorID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if actorID == "" {
		actorID = userID
	}
	metadata := map[string]any{}
	if email := deletionAuditEmail(app, userID); email != "" {
		metadata["email"] = email
	}
	for _, organizationID := range organizations {
		if err := recordOrganizationAuditEvent(app, organizationAuditEvent{
			ID:             organizationAuditID(organizationID, "user.deleted", userID+"@"+jobID),
			OrganizationID: organizationID, ActorID: actorID, Action: "user.deleted",
			TargetType: "user", TargetID: userID, Metadata: metadata,
		}); err != nil {
			return err
		}
	}
	return nil
}
