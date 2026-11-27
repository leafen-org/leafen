package organization

import (
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The organizations that verified a person's email domain manage the person's
// emails: they may keep the primary, allow secondaries only on their own
// domains and hear of every change.

func managedEmailChange(app core.App, user *core.Record, target string) error {
	var blocked int
	err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_organization_domains}} [[domain]]
		LEFT JOIN {{leafen_organization_managed_user_policies}} [[policy]] ON [[policy]].[[organization_id]] = [[domain]].[[organization_id]]
		WHERE [[domain]].[[status]] = 'verified' AND [[domain]].[[domain]] IN (` + leafen.ManagedAccountDomainSQL("{:userId}") + `)
		AND (COALESCE([[policy]].[[secondary_emails]], 'disabled') = 'disabled' OR
			(COALESCE([[policy]].[[secondary_emails]], 'disabled') = 'verified_domains' AND NOT EXISTS (
				SELECT 1 FROM {{leafen_organization_domains}} [[target]] WHERE [[target]].[[organization_id]] = [[domain]].[[organization_id]]
				AND [[target]].[[status]] = 'verified' AND [[target]].[[domain]] = {:targetDomain})))`).
		Bind(dbx.Params{"userId": user.Id, "targetDomain": leafen.EmailDomain(target)}).Row(&blocked)
	if err == nil && blocked > 0 {
		err = leafen.ErrAccountEmailManaged
	}
	return err
}

func managedEmailAddition(app core.App, user *core.Record, target string) error {
	if err := managedEmailChange(app, user, target); err != nil {
		return err
	}
	var blocked int
	err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_organization_domains}} [[domain]]
		LEFT JOIN {{leafen_organization_managed_user_policies}} [[policy]] ON [[policy]].[[organization_id]] = [[domain]].[[organization_id]]
		WHERE [[domain]].[[status]] = 'verified' AND [[domain]].[[domain]] = {:targetDomain}
		AND (COALESCE([[policy]].[[secondary_emails]], 'disabled') = 'disabled' OR NOT EXISTS (
			SELECT 1 FROM {{leafen_organization_memberships}} [[membership]] WHERE [[membership]].[[organization]] = [[domain]].[[organization_id]]
			AND [[membership]].[[user]] = {:userId} AND [[membership]].[[status]] = 'active' AND [[membership]].[[role]] <> 'guest'))`).
		Bind(dbx.Params{"targetDomain": leafen.EmailDomain(target), "userId": user.Id}).Row(&blocked)
	if err == nil && blocked > 0 {
		err = leafen.ErrAccountEmailManaged
	}
	return err
}

func auditManagedEmailChange(app core.App, user *core.Record, email, action string) error {
	cursor := ""
	for {
		organizations := []struct {
			ID         string `db:"organization_id"`
			OwnsTarget bool   `db:"owns_target"`
		}{}
		if err := app.DB().NewQuery(`SELECT [[organization_id]], MAX([[domain]] = {:targetDomain}) AS [[owns_target]] FROM {{leafen_organization_domains}}
			WHERE [[status]] = 'verified' AND [[domain]] IN (` + leafen.ManagedAccountDomainSQL("{:userId}") + ` UNION SELECT {:targetDomain})
			AND [[organization_id]] > {:cursor} GROUP BY [[organization_id]] ORDER BY [[organization_id]] LIMIT 100`).
			Bind(dbx.Params{"userId": user.Id, "targetDomain": leafen.EmailDomain(email), "cursor": cursor}).All(&organizations); err != nil {
			return err
		}
		for _, organization := range organizations {
			metadata := map[string]any{}
			if organization.OwnsTarget {
				metadata["email"] = email
			}
			if err := leafen.RecordOrganizationAuditEvent(app, leafen.OrganizationAuditEvent{OrganizationID: organization.ID, ActorID: user.Id,
				Action: "account_email." + action, TargetType: "user", TargetID: user.Id,
				Metadata: metadata, OccurredAt: time.Now().UTC().UnixMilli()}); err != nil {
				return err
			}
			if err := leafen.BumpOrganizationPolicy(app, organization.ID, time.Now().UTC().UnixMilli()); err != nil {
				return err
			}
		}
		if len(organizations) < 100 {
			break
		}
		cursor = organizations[len(organizations)-1].ID
	}
	return nil
}

// A managing organization cannot erase another domain's primary address.
// An unverified foreign primary cannot become a verified secondary as a
// side effect of an administrator proving only their own domain.
func retainedManagedPrimary(app core.App, user *core.Record, organizationID string, keep bool) (string, error) {
	email := leafen.NormalizePageShareEmail(user.Email())
	foreign := requireVerifiedOrganizationDomain(app, organizationID, email) != nil
	if foreign && !user.Verified() {
		return "", leafen.ErrAccountEmailManaged
	}
	if (keep || foreign) && user.Verified() {
		return email, nil
	}
	return "", nil
}

// managedAccounts answers the core's questions about managed accounts.
type managedAccounts struct{}

func (managedAccounts) Rules(app core.App, user *core.Record) (leafen.ManagedUserRules, error) {
	return managedAccountRules(app, user)
}

func (managedAccounts) ExternalWorkspaceJoin(app core.App, userID, workspaceOrganizationID string) error {
	return externalWorkspaceJoin(app, userID, workspaceOrganizationID)
}

func (managedAccounts) EmailChange(app core.App, user *core.Record, target string) error {
	return managedEmailChange(app, user, target)
}

func (managedAccounts) EmailAddition(app core.App, user *core.Record, target string) error {
	return managedEmailAddition(app, user, target)
}

func (managedAccounts) EmailChanged(app core.App, user *core.Record, email, action string) error {
	return auditManagedEmailChange(app, user, email, action)
}
