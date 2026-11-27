package app

import (
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// Two of Notion's request controls (Settings › Requests › Controls and
// Security › Members & guests, seen 2026-11-21 in the business trial's
// owner): "멤버가 게스트 초대를 요청할 수 있도록 허용" — while only owners
// invite guests, whether a member may ask an owner to — and "제한된 멤버가
// 개인 페이지를 만들 수 있도록 허용" (베타) — whether a restricted member makes
// private pages. Both start on. With the first off a member's guest invite is
// refused outright and no request is taken; with the second off a restricted
// member makes no private page and moves none to the top of their private
// pages, keeping those they have, as Notion's row says.

// guestInviteRefusal is how a share bringing a guest from a member is refused
// while only owners invite guests: an owner's approval to ask for, or, while
// the organization takes no such requests, a plain refusal.
func guestInviteRefusal(policy storedOrganizationSharingSecurityPolicy) error {
	if policy.GuestInviteRequests == 0 {
		return errOrganizationSharingSecurityBlocked
	}
	return errGuestInviteRequiresApproval
}

// guestInviteRequestAllowed refuses asking an owner to invite a guest the
// person may invite themselves — they are an owner, or members invite guests —
// or one the organization takes no requests for. A restricted member asked
// for under the approval policy (restrictedShareNeedsApproval) is asked for
// whatever these say.
func guestInviteRequestAllowed(authorization pageAuthorization, policy storedOrganizationSharingSecurityPolicy, restrictedApproval bool) error {
	if restrictedApproval {
		return nil
	}
	if authorization.Workspace.Role == workspaceRoleOwner || policy.GuestInvites != sharingSecurityGuestInvitesOwners {
		return refuse(errAccessRequestInvalid, "you may share the page with them yourself: share it instead of asking")
	}
	if policy.GuestInviteRequests == 0 {
		return errAccessRequestNotAllowed
	}
	return nil
}

// restrictedPrivatePagesRefused is true of a restricted member of a workspace
// whose organization keeps restricted members from making private pages.
func restrictedPrivatePagesRefused(app core.App, authorization workspaceAuthorization) (bool, error) {
	if !authorization.Restricted {
		return false, nil
	}
	policy, err := loadEnforcedSharingSecurityPolicy(app, authorization.Organization.OrganizationID)
	return err == nil && policy.RestrictedPrivatePages == 0, err
}

// restrictedPrivatePagesOffSQL is an SQL expression, true when the workspace
// the first expression names keeps the person the second names, a restricted
// member, from making private pages: the start-up read tells the sidebar so.
func restrictedPrivatePagesOffSQL(workspaceExpression, organizationExpression, userExpression string) string {
	return "(" + workspaceRestrictedMemberSQL(workspaceExpression, userExpression) +
		" AND EXISTS (SELECT 1 FROM {{leafen_organization_sharing_security_policies}} [[private_policy]]" +
		" WHERE [[private_policy]].[[organization_id]] = " + organizationExpression +
		" AND [[private_policy]].[[restricted_private_pages]] = 0))"
}

// Migration 836 adds both, on for every organization.

func ensureNativeRequestControlsSchema(app core.App) error {
	if _, err := app.DB().NewQuery(`
		ALTER TABLE {{leafen_organization_sharing_security_policies}} ADD COLUMN [[guest_invite_requests]] INTEGER NOT NULL DEFAULT 1
			CHECK ([[guest_invite_requests]] IN (0, 1));
	`).Execute(); err != nil {
		return err
	}
	if _, err := app.DB().NewQuery(`
		ALTER TABLE {{leafen_organization_sharing_security_policies}} ADD COLUMN [[restricted_private_pages]] INTEGER NOT NULL DEFAULT 1
			CHECK ([[restricted_private_pages]] IN (0, 1));
	`).Execute(); err != nil {
		return err
	}
	return validateNativeRequestControlsSchema(app)
}

func validateNativeRequestControlsSchema(app core.App) error {
	var columns int
	if err := app.DB().NewQuery(`SELECT COUNT(*) FROM pragma_table_info('leafen_organization_sharing_security_policies')
		WHERE [[name]] IN ('guest_invite_requests', 'restricted_private_pages') AND [[notnull]] = 1 AND [[dflt_value]] = '1'`).Row(&columns); err != nil {
		return err
	}
	if columns != 2 {
		return errors.New(errorCodeSchemaMigrationHistoryInvalid)
	}
	return nil
}
