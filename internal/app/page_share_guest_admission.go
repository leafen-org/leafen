package app

import (
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Someone outside an organization a page of which is shared with them by
// email comes into it as a guest, as in Notion: at once when the address
// belongs to an account that verified it, or when an account verifies it
// later. A guest opens only the pages shared with them. An organization
// whose sharing security settings keep out guests or emails outside it
// admits no one, and someone already in it — deactivated or not — stays as
// they are.

// emailAdmissionWindow is how many organizations, or invitations, one read
// looks at when a verified address is let in; the rest are read after it, to
// the last (2026-11-23: the first hundred read and then those whose settings
// keep the address out left out, the hundred-and-first was never let in).
const emailAdmissionWindow = 100

// admitEmailShareGuest lets the account that verified the address into the
// organization as a guest when it isn't in it yet. The share was admitted
// under the organization's settings as it was made.
func admitEmailShareGuest(app core.App, organizationID, actorID, email string) error {
	account, err := pageShareEmailAccount(app, email)
	if err != nil || account == "" {
		// No one has verified the address yet.
		return err
	}
	return admitOrganizationGuest(app, organizationID, actorID, account)
}

// admitPendingEmailShareGuests lets an account that has verified its email
// into each organization that shares a page with that email and still
// allows guests and emails outside it.
func admitPendingEmailShareGuests(app core.App, user *core.Record) error {
	if user == nil {
		return nil
	}
	after := ""
	for {
		organizations := []string{}
		if err := app.DB().NewQuery(`
			SELECT DISTINCT [[workspace]].[[organization]]
			FROM {{leafen_page_permissions}} [[permission]]
			JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = [[permission]].[[workspace_id]]
			WHERE [[permission]].[[principal_type]] = 'email'
				AND lower(trim([[permission]].[[principal_id]])) IN (` + verifiedAccountEmailSQL("userId") + `)
				AND [[workspace]].[[organization]] != '' AND [[workspace]].[[deletion_job_id]] = ''
				AND [[workspace]].[[organization]] > {:after}
			ORDER BY [[workspace]].[[organization]] LIMIT {:limit}
		`).Bind(dbx.Params{"userId": user.Id, "after": after, "limit": emailAdmissionWindow}).Column(&organizations); err != nil {
			return err
		}
		for _, organizationID := range organizations {
			options, err := sharingSecurityShareOptions(app, organizationID)
			if err != nil {
				return err
			}
			if !options.Guests || !options.ExternalEmail {
				continue
			}
			if err := admitOrganizationGuest(app, organizationID, user.Id, user.Id); err != nil {
				return err
			}
		}
		if len(organizations) < emailAdmissionWindow {
			return nil
		}
		after = organizations[len(organizations)-1]
	}
}

func admitOrganizationGuest(app core.App, organizationID, actorID, userID string) error {
	return admitOrganizationGuestVia(app, organizationID, actorID, userID, "page_share_email")
}

// admitOrganizationGuestVia is admitOrganizationGuest saying in the audit log
// what let them in: a page shared by email, or a guest invite link.
func admitOrganizationGuestVia(app core.App, organizationID, actorID, userID, via string) error {
	var memberships int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_organization_memberships}}
		WHERE [[organization]]={:organizationId} AND [[user]]={:userId}
	`).Bind(dbx.Params{"organizationId": organizationID, "userId": userID}).Row(&memberships); err != nil {
		return err
	}
	if memberships > 0 {
		return nil
	}
	if _, err := app.FindRecordById(organizationsCollection, organizationID); errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	if err := ensureOrganizationMembership(
		app, organizationID, userID, organizationRoleGuest, organizationMemberStatusActive,
	); err != nil {
		return err
	}
	return recordOrganizationAuditEvent(app, organizationAuditEvent{
		OrganizationID: organizationID, ActorID: actorID,
		Action: "organization_member.guest_admit", TargetType: "user", TargetID: userID,
		Metadata: map[string]any{"via": via}, OccurredAt: time.Now().UTC().UnixMilli(),
	})
}
