package app

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

var errOrganizationSharingSecurityBlocked = errors.New(errorCodeOrganizationSharingSecurityBlocked)

// defaultSharingSecurityPolicy is an organization's sharing security before
// anyone changes it: everything allowed, with each request setting as
// Notion's paid plans start it — members may ask to add other members, and
// anyone with a teamspace invite link, guests included, may ask to become a
// member (a Business workspace's Settings › Security › Members and guests;
// a free one shows the latter locked at "모든 사용자"). Leafen has no plans,
// so a setting only paid plans change starts at the paid default.
func defaultSharingSecurityPolicy(organizationID string) storedOrganizationSharingSecurityPolicy {
	return storedOrganizationSharingSecurityPolicy{
		OrganizationID: organizationID, PublicWebSharing: 1, ExternalEmailSharing: 1,
		GuestAccess: 1, Exports: 1, CrossWorkspaceCopies: 1,
		GuestInvites: sharingSecurityGuestInvitesMembers, MemberAdds: sharingSecurityMemberAddsApproval,
		MembershipRequests: "all", PageAccessRequests: 1, ViewLinks: 1, ViewLinkMaxSeconds: viewLinkMaxSeconds, ViewLinkWithoutSecret: 1, UserHTML: 1,
		RestrictedTeamspaceAdds: restrictedAddsOwners, RestrictedPageAdds: restrictedAddsMembers, PendingGuestPrivatePages: 1,
		GuestInviteRequests: 1, RestrictedPrivatePages: 1,
	}
}

// loadEnforcedSharingSecurityPolicy is the organization's sharing security
// settings as they apply: the defaults for an organization that has none
// stored.
func loadEnforcedSharingSecurityPolicy(app core.App, organizationID string) (storedOrganizationSharingSecurityPolicy, error) {
	allowed := defaultSharingSecurityPolicy(organizationID)
	if organizationID == "" {
		return allowed, nil
	}
	policy, err := loadOrganizationSharingSecurityPolicy(app, organizationID)
	if errors.Is(err, sql.ErrNoRows) {
		return allowed, nil
	}
	return policy, err
}

// publicSharingTurnedOff reports whether the organization has publishing to
// the web turned off, so what it published is no longer served.
func publicSharingTurnedOff(app core.App, organizationID string) (bool, error) {
	err := enforceSharingSecurityPublicSharing(app, organizationID)
	if errors.Is(err, errOrganizationSharingSecurityBlocked) {
		return true, nil
	}
	return false, err
}

// enforceSharingSecurityWorkspaceExports refuses a member's export of a
// workspace's content while its organization has exports turned off.
func enforceSharingSecurityWorkspaceExports(app core.App, principalID, workspaceID string) error {
	authorization, err := authorizeWorkspace(app, principalID, workspaceID, permissionWorkspaceRead)
	if err != nil {
		return err
	}
	return enforceSharingSecurityExports(app, authorization.Organization.OrganizationID)
}

// sharingSecurityOptions are what the organization lets a page's share
// dialog offer: publishing to the web, which its DLP policy can also block,
// and sharing with guests and with emails outside it.
type sharingSecurityOptions struct {
	Publishing    bool
	Guests        bool
	ExternalEmail bool
}

// sharingSecurityEmailMember is the active organization member an email
// share opens to — the account that verified the address — or "" when the
// address belongs to no one in the organization.
func sharingSecurityEmailMember(app core.App, organizationID, email string) (string, error) {
	accounts := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[user_id]] FROM {{leafen_account_emails}}
		WHERE [[email]]={:email} AND ` + provenAccountEmailSQL("") + `
		LIMIT 2
	`).Bind(dbx.Params{"email": normalizePageShareEmail(email)}).Column(&accounts); err != nil {
		return "", err
	}
	if len(accounts) != 1 {
		return "", nil
	}
	_, err := authorizeOrganization(app, accounts[0], organizationID, permissionOrganizationActive)
	if isPermissionDenied(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return accounts[0], nil
}

func writeSharingSecurityBlockedError(event *core.RequestEvent) error {
	return apiError(event, http.StatusForbidden, errorCodeOrganizationSharingSecurityBlocked,
		"Your organization's sharing security settings don't allow this.")
}

// The organization module blocks sharing outside the organization
// (SharingBlocks); without it nothing is blocked.

// enforceSharingSecurityPublicSharing refuses publishing to the web — share
// links, sites and web forms — while the organization has it turned off.
func enforceSharingSecurityPublicSharing(app core.App, organizationID string) error {
	if blocks := organizationPolicy.SharingBlocks; blocks != nil {
		return blocks.PublicSharing(app, organizationID)
	}
	return nil
}

// enforceSharingSecurityExports refuses a member's export while the
// organization has exports turned off.
func enforceSharingSecurityExports(app core.App, organizationID string) error {
	if blocks := organizationPolicy.SharingBlocks; blocks != nil {
		return blocks.Exports(app, organizationID)
	}
	return nil
}

// enforceSharingSecurityCrossWorkspaceCopy refuses moving or copying a page
// into another workspace while the organization it leaves has that turned
// off.
func enforceSharingSecurityCrossWorkspaceCopy(app core.App, source, target workspaceAuthorization) error {
	if blocks := organizationPolicy.SharingBlocks; blocks != nil {
		return blocks.CrossWorkspaceCopy(app, source, target)
	}
	return nil
}

// enforceSharingSecurityShare refuses sharing a page with someone the
// organization keeps out: a guest while guests are turned off, an outside
// email while external email sharing is.
func enforceSharingSecurityShare(
	app core.App, organizationID, workspaceID, principalType, principalID string,
) error {
	if blocks := organizationPolicy.SharingBlocks; blocks != nil {
		return blocks.Share(app, organizationID, workspaceID, principalType, principalID)
	}
	return nil
}

func sharingSecurityShareOptions(app core.App, organizationID string) (sharingSecurityOptions, error) {
	options := sharingSecurityOptions{Publishing: true, Guests: true, ExternalEmail: true}
	if blocks := organizationPolicy.SharingBlocks; blocks != nil {
		var err error
		options.Publishing, options.Guests, options.ExternalEmail, err = blocks.Options(app, organizationID)
		if err != nil {
			return sharingSecurityOptions{}, err
		}
	}
	if options.Publishing && organizationID != "" {
		err := enforceOrganizationDLPOutbound(app, organizationID, dlpOutboundPublicSharing)
		if errors.Is(err, errDLPPolicyBlocked) {
			options.Publishing = false
		} else if err != nil {
			return sharingSecurityOptions{}, err
		}
	}
	return options, nil
}
