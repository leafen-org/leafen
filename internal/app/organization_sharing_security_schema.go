package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	organizationSharingSecurityReceiptsTable = "leafen_organization_sharing_security_receipts"
)

func ensureOrganizationSharingSecurityPolicy(app core.App, organizationID string, now int64) error {
	var available int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM [[sqlite_master]] WHERE [[type]]='table'
		AND [[name]]='leafen_organization_sharing_security_policies'`).Row(&available); err != nil || available == 0 {
		return err
	}
	// Every setting starts as defaultSharingSecurityPolicy says, which is its
	// column's default but for the two request settings: their columns keep
	// the narrower value ('owners', 'none') the organizations made before
	// Leafen took Notion's paid defaults still hold, so a new organization
	// names its own here.
	defaults := defaultSharingSecurityPolicy(organizationID)
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_organization_sharing_security_policies}}
			([[organization_id]],[[member_adds]],[[membership_requests]],[[updated]])
		VALUES ({:organizationId},{:memberAdds},{:membershipRequests},{:now})
		ON CONFLICT ([[organization_id]]) DO NOTHING
	`).Bind(dbx.Params{
		"organizationId": organizationID, "memberAdds": defaults.MemberAdds,
		"membershipRequests": defaults.MembershipRequests, "now": now,
	}).Execute(); err != nil {
		return err
	}
	return nil
}
