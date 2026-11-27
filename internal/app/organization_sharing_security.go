package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

var (
	errOrganizationSharingSecurityInvalid       = errors.New(errorCodeOrganizationSharingSecurityInvalid)
	errOrganizationSharingSecurityIdentityReuse = errors.New(errorCodeOrganizationSharingSecurityRequestIDReused)
	errOrganizationSharingSecurityConflict      = errors.New(errorCodeOrganizationSharingSecurityConflict)
)

type storedOrganizationSharingSecurityPolicy struct {
	OrganizationID       string `db:"organization_id"`
	PublicWebSharing     int    `db:"public_web_sharing"`
	ExternalEmailSharing int    `db:"external_email_sharing"`
	GuestAccess          int    `db:"guest_access"`
	Exports              int    `db:"exports"`
	CrossWorkspaceCopies int    `db:"cross_workspace_copies"`
	GuestInvites         string `db:"guest_invites"`
	// Whether a member asks an owner to invite a guest when only owners invite
	// them (organization_request_controls.go).
	GuestInviteRequests int    `db:"guest_invite_requests"`
	MemberAdds          string `db:"member_adds"`
	PageAccessRequests  int    `db:"page_access_requests"`
	MembershipRequests  string `db:"membership_requests"`
	// Who may add restricted members to teamspaces and to pages
	// (restricted_member_policy.go).
	RestrictedTeamspaceAdds string `db:"restricted_teamspace_adds"`
	RestrictedPageAdds      string `db:"restricted_page_adds"`
	// Whether a guest whose membership is pending makes private pages
	// meanwhile (pending_guest_private_pages.go).
	PendingGuestPrivatePages int `db:"pending_guest_private_pages"`
	// Whether a restricted member makes private pages (organization_request_controls.go).
	RestrictedPrivatePages int `db:"restricted_private_pages"`
	// The organization's view link rules: whether its people make them, how
	// long one lasts at most, whether each opens once, and whether one may be
	// made without the secret it otherwise opens with.
	ViewLinks             int `db:"view_links"`
	ViewLinkMaxSeconds    int `db:"view_link_max_seconds"`
	ViewLinkSingleUseOnly int `db:"view_link_single_use_only"`
	ViewLinkWithoutSecret int `db:"view_link_without_secret"`
	// Whether uploaded HTML files and "HTML" blocks are drawn.
	UserHTML int `db:"user_html"`
}

// Who may invite guests to pages, who may add members, and whether people
// outside a workspace may ask for its pages: the organization's choices on
// Notion's "invite guests and members" settings.
const (
	sharingSecurityGuestInvitesMembers = "members"
	sharingSecurityGuestInvitesOwners  = "owners"
	sharingSecurityMemberAddsOwners    = "owners"
	sharingSecurityMemberAddsApproval  = "approval"
)

func registerOrganizationSharingSecurityRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+organizationSharingSecurityPolicyReadPath, readOrganizationSharingSecurityPolicyRoute)
	event.Router.Route(
		organizationSharingSecurityPolicyUpdateMethod,
		apiRoot+organizationSharingSecurityPolicyUpdatePath,
		updateOrganizationSharingSecurityPolicyRoute,
	).Bind(apis.BodyLimit(organizationSharingSecurityMutationMaxBodyBytes))
}

func readOrganizationSharingSecurityPolicyRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	policy, err := readOrganizationSharingSecurityPolicy(
		event.App, user.Id, strings.TrimSpace(event.Request.PathValue("organizationId")),
	)
	if err != nil {
		return organizationSharingSecurityError(event, err, "read")
	}
	return event.JSON(http.StatusOK, policy)
}

func updateOrganizationSharingSecurityPolicyRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := organizationSharingSecurityPolicyUpdateInput{}
	if err := event.BindBody(&input); err != nil {
		return organizationSharingSecurityError(event, errOrganizationSharingSecurityInvalid, "bind")
	}
	// A key's call asks its person only for a change that would be made
	// (user_key_acting_ask.go).
	if err := rehearseBeforeAsking(event, func(txApp core.App) error {
		_, err := updateOrganizationSharingSecurityPolicy(
			txApp, user.Id, strings.TrimSpace(event.Request.PathValue("organizationId")), input,
		)
		return err
	}); err != nil {
		return organizationSharingSecurityError(event, err, "update")
	}
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	response, err := updateOrganizationSharingSecurityPolicy(
		event.App, user.Id, strings.TrimSpace(event.Request.PathValue("organizationId")), input,
	)
	if err != nil {
		return organizationSharingSecurityError(event, err, "update")
	}
	return event.JSON(http.StatusOK, response)
}

func readOrganizationSharingSecurityPolicy(
	app core.App, principalID, organizationID string,
) (organizationSharingSecurityPolicy, error) {
	if principalID == "" || organizationID == "" ||
		utf8.RuneCountInString(organizationID) > organizationIdentifierMaxRunes {
		return organizationSharingSecurityPolicy{}, errOrganizationSharingSecurityInvalid
	}
	response := organizationSharingSecurityPolicy{}
	err := runReadSnapshot(context.Background(), app, func(txApp core.App) error {
		if _, err := authorizeOrganization(
			txApp, principalID, organizationID, permissionOrganizationSecurityAdmin,
		); err != nil {
			return err
		}
		policy, err := loadOrganizationSharingSecurityPolicy(txApp, organizationID)
		if err != nil {
			return err
		}
		version, err := readOrganizationStoragePolicyVersion(txApp, organizationID)
		if err != nil {
			return err
		}
		response = sharingSecurityPolicyResponse(policy, version)
		return nil
	})
	return response, err
}

func updateOrganizationSharingSecurityPolicy(
	app core.App, principalID, organizationID string, input organizationSharingSecurityPolicyUpdateInput,
) (organizationSharingSecurityPolicyUpdateResponse, error) {
	input.RequestID = strings.TrimSpace(input.RequestID)
	if principalID == "" || organizationID == "" || input.RequestID == "" ||
		utf8.RuneCountInString(organizationID) > organizationIdentifierMaxRunes ||
		utf8.RuneCountInString(input.RequestID) > organizationSharingSecurityRequestIDRunes ||
		input.ExpectedPolicyVersion < 0 || input.PublicWebSharing == nil ||
		input.ExternalEmailSharing == nil || input.GuestAccess == nil || input.Exports == nil ||
		(input.GuestInvites != nil && *input.GuestInvites != sharingSecurityGuestInvitesMembers &&
			*input.GuestInvites != sharingSecurityGuestInvitesOwners) ||
		(input.MembershipRequests != nil && !validMembershipRequestPolicy(*input.MembershipRequests)) ||
		(input.MemberAdds != nil && *input.MemberAdds != sharingSecurityMemberAddsOwners &&
			*input.MemberAdds != sharingSecurityMemberAddsApproval) ||
		(input.ViewLinkMaxSeconds != nil && (*input.ViewLinkMaxSeconds < 60 || *input.ViewLinkMaxSeconds > viewLinkMaxSeconds)) ||
		(input.RestrictedTeamspaceAdds != nil && !oneOf(*input.RestrictedTeamspaceAdds, restrictedAddsOwners, restrictedAddsTeamspaceOwners, restrictedAddsMembers)) ||
		(input.RestrictedPageAdds != nil && !oneOf(*input.RestrictedPageAdds, restrictedAddsOwners, restrictedAddsApproval, restrictedAddsMembers)) {
		return organizationSharingSecurityPolicyUpdateResponse{}, errOrganizationSharingSecurityInvalid
	}
	requestHash, err := sharingSecurityRequestHash(input)
	if err != nil {
		return organizationSharingSecurityPolicyUpdateResponse{}, err
	}
	release := acquireProductFileOperationLock(productOrganizationStorageLockNamespace, organizationID)
	defer release()
	response := organizationSharingSecurityPolicyUpdateResponse{}
	err = app.RunInTransaction(func(txApp core.App) error {
		authorization, err := authorizeOrganization(
			txApp, principalID, organizationID, permissionOrganizationSecurityAdmin,
		)
		if err != nil {
			return err
		}
		receipt, err := readOrganizationSharingSecurityReceipt(txApp, organizationID, principalID, input.RequestID)
		if err == nil {
			if receipt.RequestHash != requestHash {
				return errOrganizationSharingSecurityIdentityReuse
			}
			if err := json.Unmarshal([]byte(receipt.ResponseJSON), &response); err != nil {
				return err
			}
			// A receipt from before membership requests replays the 'none'
			// every organization then had, not today's default.
			if response.Policy.MembershipRequests == "" {
				response.Policy.MembershipRequests = "none"
			}
			response.Receipt.Duplicate = true
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		policy, err := loadOrganizationSharingSecurityPolicy(txApp, organizationID)
		if err != nil {
			return err
		}
		version, err := readOrganizationStoragePolicyVersion(txApp, organizationID)
		if err != nil {
			return err
		}
		if version != input.ExpectedPolicyVersion {
			return errOrganizationSharingSecurityConflict
		}
		next := sharingSecurityPolicyFromInput(organizationID, input)
		// The blocks on sharing outside the organization are the organization
		// module's to change.
		if organizationPolicy.SharingBlocks == nil && (next.PublicWebSharing != policy.PublicWebSharing ||
			next.ExternalEmailSharing != policy.ExternalEmailSharing || next.GuestAccess != policy.GuestAccess ||
			next.Exports != policy.Exports ||
			(input.CrossWorkspaceCopies != nil && next.CrossWorkspaceCopies != policy.CrossWorkspaceCopies)) {
			return errOrganizationSharingSecurityInvalid
		}
		// A client that doesn't name a newer setting keeps it as set.
		if input.CrossWorkspaceCopies == nil {
			next.CrossWorkspaceCopies = policy.CrossWorkspaceCopies
		}
		if input.GuestInvites == nil {
			next.GuestInvites = policy.GuestInvites
		}
		if input.MemberAdds == nil {
			next.MemberAdds = policy.MemberAdds
		}
		if input.MembershipRequests == nil {
			next.MembershipRequests = policy.MembershipRequests
		}
		if input.PageAccessRequests == nil {
			next.PageAccessRequests = policy.PageAccessRequests
		}
		if input.ViewLinks == nil {
			next.ViewLinks = policy.ViewLinks
		}
		if input.ViewLinkMaxSeconds == nil {
			next.ViewLinkMaxSeconds = policy.ViewLinkMaxSeconds
		}
		if input.ViewLinkSingleUseOnly == nil {
			next.ViewLinkSingleUseOnly = policy.ViewLinkSingleUseOnly
		}
		if input.ViewLinkWithoutSecret == nil {
			next.ViewLinkWithoutSecret = policy.ViewLinkWithoutSecret
		}
		if input.UserHTML == nil {
			next.UserHTML = policy.UserHTML
		}
		if input.RestrictedTeamspaceAdds == nil {
			next.RestrictedTeamspaceAdds = policy.RestrictedTeamspaceAdds
		}
		if input.RestrictedPageAdds == nil {
			next.RestrictedPageAdds = policy.RestrictedPageAdds
		}
		if input.PendingGuestPrivatePages == nil {
			next.PendingGuestPrivatePages = policy.PendingGuestPrivatePages
		}
		if input.GuestInviteRequests == nil {
			next.GuestInviteRequests = policy.GuestInviteRequests
		}
		if input.RestrictedPrivatePages == nil {
			next.RestrictedPrivatePages = policy.RestrictedPrivatePages
		}
		nextVersion := version
		now := time.Now().UTC().UnixMilli()
		if !sameSharingSecurityPolicy(policy, next) {
			if err := persistOrganizationSharingSecurityPolicy(txApp, next, now); err != nil {
				return err
			}
			nextVersion, err = bumpOrganizationStoragePolicyVersion(txApp, organizationID, version, now)
			if err != nil {
				return err
			}
			if err := recordOrganizationAuditEvent(txApp, organizationAuditEvent{
				ID:             organizationAuditID(organizationID, "organization_settings.update", organizationID+"\x00"+principalID+"\x00"+input.RequestID+"\x00"+strconv.Itoa(nextVersion)),
				OrganizationID: organizationID, ActorID: authorization.PrincipalID,
				Action: "organization_settings.update", TargetType: "organization", TargetID: organizationID,
				Metadata: map[string]any{"requestId": input.RequestID, "policyVersion": nextVersion, "sharingSecurity": next,
					"changes": organizationSettingsChanges(policy, next, "OrganizationID", "UpdatedAt", "CreatedAt")},
				OccurredAt: now,
			}); err != nil {
				return err
			}
			policy = next
		}
		response.Policy = sharingSecurityPolicyResponse(policy, nextVersion)
		response.Receipt = organizationSharingSecurityPolicyReceipt{RequestID: input.RequestID, PolicyVersion: nextVersion}
		encoded, err := json.Marshal(response)
		if err != nil || len(encoded) > organizationSharingSecurityResponseBytes {
			return errors.New(errorCodeOrganizationSharingSecurityStateInvalid)
		}
		created, err := nextOrganizationSharingSecurityReceiptCreated(txApp, organizationID, now)
		if err != nil {
			return err
		}
		if _, err := txApp.DB().Insert(organizationSharingSecurityReceiptsTable, dbx.Params{
			"organization_id": organizationID, "actor_id": principalID, "request_id": input.RequestID,
			"request_hash": requestHash, "policy_version": nextVersion,
			"response_json": string(encoded), "created": created,
		}).Execute(); err != nil {
			return err
		}
		return pruneOrganizationSharingSecurityReceipts(txApp, organizationID)
	})
	return response, err
}

func loadOrganizationSharingSecurityPolicy(app core.App, organizationID string) (storedOrganizationSharingSecurityPolicy, error) {
	policy := storedOrganizationSharingSecurityPolicy{}
	err := app.DB().NewQuery(`SELECT [[organization_id]],[[public_web_sharing]],[[external_email_sharing]],
		[[guest_access]],[[exports]],[[cross_workspace_copies]],[[guest_invites]],[[member_adds]],[[page_access_requests]],
		[[membership_requests]],[[view_links]],[[view_link_max_seconds]],[[view_link_single_use_only]],[[view_link_without_secret]],[[user_html]],
		[[restricted_teamspace_adds]],[[restricted_page_adds]],[[pending_guest_private_pages]],[[guest_invite_requests]],[[restricted_private_pages]]
		FROM {{leafen_organization_sharing_security_policies}}
		WHERE [[organization_id]]={:organizationId}`).Bind(dbx.Params{"organizationId": organizationID}).One(&policy)
	return policy, err
}

func persistOrganizationSharingSecurityPolicy(app core.App, policy storedOrganizationSharingSecurityPolicy, now int64) error {
	_, err := app.DB().NewQuery(`UPDATE {{leafen_organization_sharing_security_policies}} SET
		[[public_web_sharing]]={:publicWeb},[[external_email_sharing]]={:externalEmail},[[guest_access]]={:guest},
		[[exports]]={:exports},[[cross_workspace_copies]]={:copies},[[guest_invites]]={:guestInvites},
		[[membership_requests]]={:membershipRequests},[[member_adds]]={:memberAdds},[[page_access_requests]]={:pageRequests},[[view_links]]={:viewLinks},
		[[view_link_max_seconds]]={:viewLinkMaxSeconds},[[view_link_single_use_only]]={:viewLinkSingleUse},
		[[view_link_without_secret]]={:viewLinkWithoutSecret},[[user_html]]={:userHTML},
		[[restricted_teamspace_adds]]={:restrictedTeamspaceAdds},[[restricted_page_adds]]={:restrictedPageAdds},
		[[pending_guest_private_pages]]={:pendingGuestPrivatePages},[[guest_invite_requests]]={:guestInviteRequests},
		[[restricted_private_pages]]={:restrictedPrivatePages},[[updated]]={:now}
		WHERE [[organization_id]]={:organizationId}`).Bind(dbx.Params{
		"organizationId": policy.OrganizationID, "publicWeb": policy.PublicWebSharing,
		"externalEmail": policy.ExternalEmailSharing, "guest": policy.GuestAccess,
		"exports": policy.Exports, "copies": policy.CrossWorkspaceCopies, "guestInvites": policy.GuestInvites,
		"membershipRequests": policy.MembershipRequests, "memberAdds": policy.MemberAdds, "pageRequests": policy.PageAccessRequests, "viewLinks": policy.ViewLinks,
		"viewLinkMaxSeconds": policy.ViewLinkMaxSeconds, "viewLinkSingleUse": policy.ViewLinkSingleUseOnly,
		"viewLinkWithoutSecret": policy.ViewLinkWithoutSecret, "userHTML": policy.UserHTML,
		"restrictedTeamspaceAdds": policy.RestrictedTeamspaceAdds, "restrictedPageAdds": policy.RestrictedPageAdds,
		"pendingGuestPrivatePages": policy.PendingGuestPrivatePages, "guestInviteRequests": policy.GuestInviteRequests,
		"restrictedPrivatePages": policy.RestrictedPrivatePages, "now": now,
	}).Execute()
	return err
}

func sharingSecurityPolicyFromInput(organizationID string, input organizationSharingSecurityPolicyUpdateInput) storedOrganizationSharingSecurityPolicy {
	boolean := func(value bool) int {
		if value {
			return 1
		}
		return 0
	}
	policy := defaultSharingSecurityPolicy(organizationID)
	policy.PublicWebSharing, policy.ExternalEmailSharing = boolean(*input.PublicWebSharing), boolean(*input.ExternalEmailSharing)
	policy.GuestAccess, policy.Exports = boolean(*input.GuestAccess), boolean(*input.Exports)
	if input.PendingGuestPrivatePages != nil {
		policy.PendingGuestPrivatePages = boolean(*input.PendingGuestPrivatePages)
	}
	if input.GuestInviteRequests != nil {
		policy.GuestInviteRequests = boolean(*input.GuestInviteRequests)
	}
	if input.RestrictedPrivatePages != nil {
		policy.RestrictedPrivatePages = boolean(*input.RestrictedPrivatePages)
	}
	if input.RestrictedTeamspaceAdds != nil {
		policy.RestrictedTeamspaceAdds = *input.RestrictedTeamspaceAdds
	}
	if input.RestrictedPageAdds != nil {
		policy.RestrictedPageAdds = *input.RestrictedPageAdds
	}
	if input.UserHTML != nil {
		policy.UserHTML = boolean(*input.UserHTML)
	}
	if input.CrossWorkspaceCopies != nil {
		policy.CrossWorkspaceCopies = boolean(*input.CrossWorkspaceCopies)
	}
	if input.GuestInvites != nil {
		policy.GuestInvites = *input.GuestInvites
	}
	if input.MemberAdds != nil {
		policy.MemberAdds = *input.MemberAdds
	}
	if input.MembershipRequests != nil {
		policy.MembershipRequests = *input.MembershipRequests
	}
	if input.PageAccessRequests != nil {
		policy.PageAccessRequests = boolean(*input.PageAccessRequests)
	}
	if input.ViewLinks != nil {
		policy.ViewLinks = boolean(*input.ViewLinks)
	}
	if input.ViewLinkMaxSeconds != nil {
		policy.ViewLinkMaxSeconds = *input.ViewLinkMaxSeconds
	}
	if input.ViewLinkSingleUseOnly != nil {
		policy.ViewLinkSingleUseOnly = boolean(*input.ViewLinkSingleUseOnly)
	}
	if input.ViewLinkWithoutSecret != nil {
		policy.ViewLinkWithoutSecret = boolean(*input.ViewLinkWithoutSecret)
	}
	return policy
}

func sharingSecurityPolicyResponse(policy storedOrganizationSharingSecurityPolicy, version int) organizationSharingSecurityPolicy {
	return organizationSharingSecurityPolicy{
		OrganizationID: policy.OrganizationID, PolicyVersion: version,
		PublicWebSharing: policy.PublicWebSharing == 1, ExternalEmailSharing: policy.ExternalEmailSharing == 1,
		GuestAccess: policy.GuestAccess == 1, Exports: policy.Exports == 1,
		CrossWorkspaceCopies: policy.CrossWorkspaceCopies == 1, GuestInvites: policy.GuestInvites,
		MembershipRequests: policy.MembershipRequests, MemberAdds: policy.MemberAdds, PageAccessRequests: policy.PageAccessRequests == 1,
		ViewLinks: policy.ViewLinks == 1, ViewLinkMaxSeconds: policy.ViewLinkMaxSeconds,
		ViewLinkSingleUseOnly: policy.ViewLinkSingleUseOnly == 1, ViewLinkWithoutSecret: policy.ViewLinkWithoutSecret == 1,
		UserHTML: policy.UserHTML == 1, RestrictedTeamspaceAdds: policy.RestrictedTeamspaceAdds, RestrictedPageAdds: policy.RestrictedPageAdds,
		PendingGuestPrivatePages: policy.PendingGuestPrivatePages == 1,
		GuestInviteRequests:      policy.GuestInviteRequests == 1, RestrictedPrivatePages: policy.RestrictedPrivatePages == 1,
	}
}

func sameSharingSecurityPolicy(left, right storedOrganizationSharingSecurityPolicy) bool {
	return left.OrganizationID == right.OrganizationID && left.PublicWebSharing == right.PublicWebSharing &&
		left.ExternalEmailSharing == right.ExternalEmailSharing && left.GuestAccess == right.GuestAccess &&
		left.Exports == right.Exports && left.CrossWorkspaceCopies == right.CrossWorkspaceCopies &&
		left.GuestInvites == right.GuestInvites && left.MemberAdds == right.MemberAdds &&
		left.MembershipRequests == right.MembershipRequests && left.PageAccessRequests == right.PageAccessRequests && left.ViewLinks == right.ViewLinks &&
		left.ViewLinkMaxSeconds == right.ViewLinkMaxSeconds && left.ViewLinkSingleUseOnly == right.ViewLinkSingleUseOnly &&
		left.ViewLinkWithoutSecret == right.ViewLinkWithoutSecret && left.UserHTML == right.UserHTML &&
		left.RestrictedTeamspaceAdds == right.RestrictedTeamspaceAdds && left.RestrictedPageAdds == right.RestrictedPageAdds &&
		left.PendingGuestPrivatePages == right.PendingGuestPrivatePages &&
		left.GuestInviteRequests == right.GuestInviteRequests && left.RestrictedPrivatePages == right.RestrictedPrivatePages
}

func sharingSecurityRequestHash(input organizationSharingSecurityPolicyUpdateInput) (string, error) {
	payload, err := json.Marshal(struct {
		ExpectedPolicyVersion    int     `json:"expectedPolicyVersion"`
		PublicWebSharing         bool    `json:"publicWebSharing"`
		ExternalEmailSharing     bool    `json:"externalEmailSharing"`
		GuestAccess              bool    `json:"guestAccess"`
		Exports                  bool    `json:"exports"`
		CrossWorkspaceCopies     *bool   `json:"crossWorkspaceCopies,omitempty"`
		UserHTML                 *bool   `json:"userHtml,omitempty"`
		GuestInvites             *string `json:"guestInvites,omitempty"`
		MemberAdds               *string `json:"memberAdds,omitempty"`
		MembershipRequests       *string `json:"membershipRequests,omitempty"`
		PageAccessRequests       *bool   `json:"pageAccessRequests,omitempty"`
		ViewLinks                *bool   `json:"viewLinks,omitempty"`
		ViewLinkMaxSeconds       *int    `json:"viewLinkMaxSeconds,omitempty"`
		ViewLinkSingleUseOnly    *bool   `json:"viewLinkSingleUseOnly,omitempty"`
		ViewLinkWithoutSecret    *bool   `json:"viewLinkWithoutSecret,omitempty"`
		RestrictedTeamspaceAdds  *string `json:"restrictedTeamspaceAdds,omitempty"`
		RestrictedPageAdds       *string `json:"restrictedPageAdds,omitempty"`
		PendingGuestPrivatePages *bool   `json:"pendingGuestPrivatePages,omitempty"`
		GuestInviteRequests      *bool   `json:"guestInviteRequests,omitempty"`
		RestrictedPrivatePages   *bool   `json:"restrictedPrivatePages,omitempty"`
	}{ExpectedPolicyVersion: input.ExpectedPolicyVersion, PublicWebSharing: *input.PublicWebSharing,
		ExternalEmailSharing: *input.ExternalEmailSharing, GuestAccess: *input.GuestAccess, Exports: *input.Exports,
		CrossWorkspaceCopies: input.CrossWorkspaceCopies, UserHTML: input.UserHTML, GuestInvites: input.GuestInvites,
		MemberAdds: input.MemberAdds, MembershipRequests: input.MembershipRequests, PageAccessRequests: input.PageAccessRequests,
		ViewLinks: input.ViewLinks, ViewLinkMaxSeconds: input.ViewLinkMaxSeconds, ViewLinkSingleUseOnly: input.ViewLinkSingleUseOnly,
		ViewLinkWithoutSecret: input.ViewLinkWithoutSecret, RestrictedTeamspaceAdds: input.RestrictedTeamspaceAdds,
		RestrictedPageAdds: input.RestrictedPageAdds, PendingGuestPrivatePages: input.PendingGuestPrivatePages,
		GuestInviteRequests: input.GuestInviteRequests, RestrictedPrivatePages: input.RestrictedPrivatePages})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func organizationSharingSecurityError(event *core.RequestEvent, err error, stage string) error {
	switch {
	case errors.Is(err, errOrganizationSharingSecurityInvalid):
		return apiErrorWithCause(event, http.StatusBadRequest, errorCodeOrganizationSharingSecurityInvalid,
			"Check the organization sharing security policy.", "organization_sharing_security", stage,
			errorCodeOrganizationSharingSecurityInvalid, err)
	case errors.Is(err, errOrganizationSharingSecurityIdentityReuse):
		return apiErrorWithCause(event, http.StatusConflict, errorCodeOrganizationSharingSecurityRequestIDReused,
			"The same policy request can't be reused with different content.", "organization_sharing_security", stage,
			errorCodeOrganizationSharingSecurityRequestIDReused, err)
	case errors.Is(err, errOrganizationSharingSecurityConflict):
		return apiErrorWithCause(event, http.StatusConflict, errorCodeOrganizationSharingSecurityConflict,
			"The organization policy already changed.", "organization_sharing_security", stage,
			errorCodeOrganizationSharingSecurityConflict, err)
	case isPermissionDenied(err):
		return apiErrorWithCause(event, http.StatusNotFound, errorCodeOrganizationNotFound,
			"Organization not found.", "organization_sharing_security", stage,
			errorCodeOrganizationNotFound, err)
	default:
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeOrganizationSharingSecurityStateInvalid,
			"Couldn't process the organization sharing security policy.", "organization_sharing_security", stage,
			errorCodeOrganizationSharingSecurityStateInvalid, err)
	}
}
