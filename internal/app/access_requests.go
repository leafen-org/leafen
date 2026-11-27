package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Requests, as Notion's: someone asks for access to a page they can't open or
// edit, for a guest added to a page or for a member added to the workspace,
// and someone who may grant it approves or declines. A page's request goes
// to the people with full access to it, in their inbox; a guest's or a
// member's to the workspace's owners, in Settings → Requests. Approving
// grants through the owners that grant the same thing directly: a page's
// share and the workspace's membership.

var (
	errAccessRequestInvalid        = errors.New(errorCodeAccessRequestInvalid)
	errAccessRequestNotFound       = errors.New(errorCodeAccessRequestNotFound)
	errAccessRequestNotAllowed     = errors.New(errorCodeAccessRequestNotAllowed)
	errAccessRequestDecided        = errors.New(errorCodeAccessRequestDecided)
	errGuestInviteRequiresApproval = errors.New(errorCodeGuestInviteRequiresApproval)
)

const (
	accessRequestKindPage       = "page"
	accessRequestKindGuest      = "guest"
	accessRequestKindMember     = "member"
	accessRequestKindMembership = "membership"

	accessRequestPending  = "pending"
	accessRequestApproved = "approved"
	accessRequestDeclined = "declined"
)

type storedAccessRequest struct {
	ID            string `db:"id"`
	WorkspaceID   string `db:"workspace_id"`
	Kind          string `db:"kind"`
	RequesterID   string `db:"requester_id"`
	PageID        string `db:"page_id"`
	PrincipalType string `db:"principal_type"`
	PrincipalID   string `db:"principal_id"`
	Role          string `db:"role"`
	Status        string `db:"status"`
	DecidedBy     string `db:"decided_by"`
	DecidedAt     int64  `db:"decided_at"`
	CreatedAt     int64  `db:"created_at"`
	UpdatedAt     int64  `db:"updated_at"`
	// Why the person asks, when they say (a membership request's "메시지 추가").
	Message string `db:"message"`
}

const accessRequestColumns = `[[id]],[[workspace_id]],[[kind]],[[requester_id]],[[page_id]],[[principal_type]],
	[[principal_id]],[[role]],[[status]],[[decided_by]],[[decided_at]],[[created_at]],[[updated_at]],[[message]]`

// accessRequestRoleVerb is what a role lets one do to a page, in a refusal's
// words.
func accessRequestRoleVerb(role pageAccessRole) string {
	switch {
	case role >= pageAccessRoleFullAccess:
		return "fully manage"
	case role >= pageAccessRoleEdit:
		return "edit"
	case role >= pageAccessRoleComment:
		return "comment on"
	}
	return "view"
}

func accessRequestRoleFromName(name string) pageAccessRole {
	switch name {
	case publicMutationRoleView:
		return pageAccessRoleView
	case publicMutationRoleComment:
		return pageAccessRoleComment
	case publicMutationRoleEdit:
		return pageAccessRoleEdit
	case publicMutationRoleFullAccess:
		return pageAccessRoleFullAccess
	}
	return pageAccessRoleNone
}

// pageAccessRoleOf is the widest role the person has on the page now, an operation holding it aside (page_access_hold.go).
func pageAccessRoleOf(app core.App, principalID, pageID string) (pageAccessRole, error) {
	for _, role := range []pageAccessRole{pageAccessRoleFullAccess, pageAccessRoleEdit, pageAccessRoleComment, pageAccessRoleView} {
		_, err := pageAccessDespiteHold(authorizePageAccessRole(app, principalID, pageID, role))
		if err == nil {
			return role, nil
		}
		if !isPermissionDenied(err) {
			return pageAccessRoleNone, err
		}
	}
	return pageAccessRoleNone, nil
}

// workspaceRoleOf is the person's role in the workspace, or "" when they are
// not one of its members.
func workspaceRoleOf(app core.App, principalID, workspaceID string) (string, error) {
	authorization, err := authorizeWorkspace(app, principalID, workspaceID, permissionWorkspaceRead)
	if isPermissionDenied(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return authorization.Role, nil
}

func workspaceSharingPolicy(app core.App, workspaceID string) (storedOrganizationSharingSecurityPolicy, error) {
	workspace, err := app.FindRecordById(workspacesCollection, workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return storedOrganizationSharingSecurityPolicy{}, errAccessRequestNotFound
	}
	if err != nil {
		return storedOrganizationSharingSecurityPolicy{}, err
	}
	if workspace.GetString("deletion_job_id") != "" {
		return storedOrganizationSharingSecurityPolicy{}, errAccessRequestNotFound
	}
	return loadEnforcedSharingSecurityPolicy(app, workspace.GetString("organization"))
}

// sharePrincipalIsWorkspaceMember reports whether a share's principal is
// already one of the workspace's members, so the share brings no guest.
func sharePrincipalIsWorkspaceMember(app core.App, organizationID, workspaceID, principalType, principalID string) (bool, error) {
	userID := ""
	switch principalType {
	case publicMutationPrincipalUser:
		userID = principalID
	case publicMutationPrincipalEmail:
		var err error
		if organizationID != "" {
			userID, err = sharingSecurityEmailMember(app, organizationID, principalID)
		} else {
			userID, err = pageShareEmailAccount(app, principalID)
		}
		if err != nil {
			return false, err
		}
	default:
		return true, nil
	}
	if userID == "" {
		return false, nil
	}
	role, err := workspaceRoleOf(app, userID, workspaceID)
	return role != "", err
}

// enforceGuestInvitePolicy refuses a share that brings a guest from someone
// who isn't the workspace's owner while the organization lets only owners
// invite guests; they ask an owner instead where they may (guestInviteRefusal).
func enforceGuestInvitePolicy(app core.App, authorization pageAuthorization, input publicMutationInput) error {
	if input.Action != publicMutationActionGrantPermission || authorization.Workspace.Role == workspaceRoleOwner {
		return nil
	}
	organizationID := authorization.Workspace.Organization.OrganizationID
	policy, err := loadEnforcedSharingSecurityPolicy(app, organizationID)
	if err != nil || policy.GuestInvites != sharingSecurityGuestInvitesOwners {
		return err
	}
	member, err := sharePrincipalIsWorkspaceMember(
		app, organizationID, authorization.Workspace.WorkspaceID, input.PrincipalType, input.PrincipalID,
	)
	if err != nil || member {
		return err
	}
	return guestInviteRefusal(policy)
}

type accessRequestApprovalKey struct{}

// withApprovedGuestInvite marks a share an owner approved, which the guest
// invite policy then lets through.
func withApprovedGuestInvite(ctx context.Context) context.Context {
	return context.WithValue(ctx, accessRequestApprovalKey{}, true)
}

func approvedGuestInvite(ctx context.Context) bool {
	approved, _ := ctx.Value(accessRequestApprovalKey{}).(bool)
	return approved
}

func requestPageAccess(app core.App, requesterID, pageID, roleName string) (accessRequestResponse, error) {
	wanted := accessRequestRoleFromName(roleName)
	if requesterID == "" || pageID == "" {
		return accessRequestResponse{}, refuse(errAccessRequestInvalid, "the page ID in the path is empty")
	}
	if wanted == pageAccessRoleNone || wanted == pageAccessRoleFullAccess {
		return accessRequestResponse{}, refuse(errAccessRequestInvalid, "role is view, comment or edit, not %q", roleName)
	}
	page, err := app.FindRecordById(pagesCollection, pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return accessRequestResponse{}, errAccessRequestNotFound
	}
	if err != nil {
		return accessRequestResponse{}, err
	}
	if page.GetBool("in_trash") || page.GetBool("notion_import_staging") {
		return accessRequestResponse{}, errAccessRequestNotFound
	}
	workspaceID := page.GetString("workspace")
	current, err := pageAccessRoleOf(app, requesterID, pageID)
	if err != nil {
		return accessRequestResponse{}, err
	}
	if current >= wanted {
		return accessRequestResponse{}, refuse(errAccessRequestInvalid, "you can %s this page already", accessRequestRoleVerb(current))
	}
	role, err := workspaceRoleOf(app, requesterID, workspaceID)
	if err != nil {
		return accessRequestResponse{}, err
	}
	if role == "" {
		// Someone outside the workspace asks only while it takes such requests.
		policy, err := workspaceSharingPolicy(app, workspaceID)
		if err != nil {
			return accessRequestResponse{}, err
		}
		if policy.PageAccessRequests == 0 {
			return accessRequestResponse{}, errAccessRequestNotAllowed
		}
	}
	stored, fresh, err := upsertAccessRequest(app, storedAccessRequest{
		WorkspaceID: workspaceID, Kind: accessRequestKindPage, RequesterID: requesterID, PageID: pageID, Role: roleName,
	})
	if err != nil {
		return accessRequestResponse{}, err
	}
	if fresh {
		if err := notifyPageAccessRequest(app, page, stored); err != nil {
			return accessRequestResponse{}, err
		}
	}
	return accessRequestResponseFor(app, stored, requesterID)
}

func requestGuestInvite(app core.App, requesterID, pageID string, input accessRequestGuestInput) (accessRequestResponse, error) {
	input.PrincipalType = strings.TrimSpace(input.PrincipalType)
	input.PrincipalID = strings.TrimSpace(input.PrincipalID)
	if input.PrincipalType == publicMutationPrincipalEmail {
		input.PrincipalID = normalizePageShareEmail(input.PrincipalID)
	}
	switch {
	case requesterID == "" || pageID == "":
		return accessRequestResponse{}, refuse(errAccessRequestInvalid, "the page ID in the path is empty")
	case input.PrincipalType != publicMutationPrincipalUser && input.PrincipalType != publicMutationPrincipalEmail:
		return accessRequestResponse{}, refuse(errAccessRequestInvalid, "principalType is user or email, not %q", input.PrincipalType)
	case input.PrincipalID == "" || len(input.PrincipalID) > accessRequestEmailRunes:
		return accessRequestResponse{}, refuse(errAccessRequestInvalid, "principalId names the person or address, 1–%d characters", accessRequestEmailRunes)
	case accessRequestRoleFromName(input.Role) == pageAccessRoleNone:
		return accessRequestResponse{}, refuse(errAccessRequestInvalid, "role is view, comment, edit or full_access, not %q", input.Role)
	}
	if input.PrincipalType == publicMutationPrincipalEmail {
		if _, ok := normalizeWorkspaceMemberEmail(input.PrincipalID); !ok {
			return accessRequestResponse{}, refuse(errAccessRequestInvalid, "principalId isn't an email address")
		}
	}
	authorization, err := authorizePageAccessRole(app, requesterID, pageID, pageAccessRoleFullAccess)
	if err != nil {
		return accessRequestResponse{}, err
	}
	organizationID := authorization.Workspace.Organization.OrganizationID
	policy, err := loadEnforcedSharingSecurityPolicy(app, organizationID)
	if err != nil {
		return accessRequestResponse{}, err
	}
	// A restricted member the policy wants an owner's approval for is asked
	// for as a guest is (restricted_member_policy.go).
	restrictedApproval, err := restrictedShareNeedsApproval(app, authorization, input.PrincipalType, input.PrincipalID)
	if err != nil {
		return accessRequestResponse{}, err
	}
	// An owner, or anyone while members may invite guests, shares directly.
	if err := guestInviteRequestAllowed(authorization, policy, restrictedApproval); err != nil {
		return accessRequestResponse{}, err
	}
	member, err := sharePrincipalIsWorkspaceMember(
		app, organizationID, authorization.Workspace.WorkspaceID, input.PrincipalType, input.PrincipalID,
	)
	if err != nil {
		return accessRequestResponse{}, err
	}
	if member && !restrictedApproval {
		return accessRequestResponse{}, refuse(errAccessRequestInvalid, "principalId is a member of the workspace: share the page with them instead of asking")
	}
	stored, _, err := upsertAccessRequest(app, storedAccessRequest{
		WorkspaceID: authorization.Workspace.WorkspaceID, Kind: accessRequestKindGuest, RequesterID: requesterID,
		PageID: pageID, PrincipalType: input.PrincipalType, PrincipalID: input.PrincipalID, Role: input.Role,
	})
	if err != nil {
		return accessRequestResponse{}, err
	}
	return accessRequestResponseFor(app, stored, requesterID)
}

// pageAccessRequestApprovers are the people with full access to the page — its private owner, people
// its shares give full access, the workspace's and teamspaces' owners who reach it — while an operation
// holds it too (page_access_hold.go), who decide once it is let go.
func pageAccessRequestApprovers(app core.App, page *core.Record, requesterID string) ([]string, error) {
	workspaceID := page.GetString("workspace")
	candidates := map[string]struct{}{}
	add := func(id string) {
		if id != "" && id != requesterID {
			candidates[id] = struct{}{}
		}
	}
	add(page.GetString(privatePageOwnerField))
	if workspace, err := app.FindRecordById(workspacesCollection, workspaceID); err == nil {
		add(workspacePermissionOwnerID(workspace))
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	ids := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[user_id]] FROM {{leafen_workspace_memberships}} WHERE [[workspace_id]]={:workspaceId} AND [[role]]='owner'
		UNION SELECT [[principal_id]] FROM {{leafen_teamspace_members}}
			WHERE [[workspace_id]]={:workspaceId} AND [[principal_type]]='user' AND [[role]]='owner'
		UNION SELECT [[principal_id]] FROM {{leafen_page_permissions}}
			WHERE [[page_id]]={:pageId} AND [[principal_type]]='user' AND [[role]]='full_access'
	`).Bind(dbx.Params{"workspaceId": workspaceID, "pageId": page.Id}).Column(&ids); err != nil {
		return nil, err
	}
	for _, id := range ids {
		add(id)
	}
	approvers := []string{}
	for id := range candidates {
		if _, err := pageAccessDespiteHold(authorizePageAccessRole(app, id, page.Id, pageAccessRoleFullAccess)); err == nil {
			approvers = append(approvers, id)
		} else if !isPermissionDenied(err) {
			return nil, err
		}
	}
	sort.Strings(approvers)
	return approvers, nil
}

// notifyPageAccessRequest tells the page's approvers in their inbox, as
// Notion's "requested access" does.
func notifyPageAccessRequest(app core.App, page *core.Record, request storedAccessRequest) error {
	approvers, err := pageAccessRequestApprovers(app, page, request.RequesterID)
	if err != nil || len(approvers) == 0 {
		return err
	}
	target, err := pageNotificationTarget(app, page.Id)
	if err != nil {
		return err
	}
	stableID := func(parts ...string) string {
		digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
		return hex.EncodeToString(digest[:16])
	}
	now := time.Now().UTC().UnixMilli()
	projections := make([]productNotificationProjection, 0, len(approvers))
	for _, userID := range approvers {
		projections = append(projections, productNotificationProjection{
			ID: stableID("access-request", request.ID, userID), EventID: stableID("access-request-event", request.ID),
			WorkspaceID: request.WorkspaceID, UserID: userID, ActivityKey: "access-request:" + request.ID,
			Kind: "access_request", PageID: page.Id, ActorID: request.RequesterID, Title: page.GetString("title"),
			Preview: request.Role, Target: target, OccurredAt: now, Personal: true,
		})
	}
	// Every approver is told, a hundred at a time.
	for start := 0; start < len(projections); start += databaseRowNotificationBatch {
		if _, err := upsertProductNotifications(context.Background(), app, projections[start:min(start+databaseRowNotificationBatch, len(projections))], now); err != nil {
			return err
		}
	}
	return nil
}

func grantAccessRequest(ctx context.Context, app core.App, deciderID string, request storedAccessRequest) error {
	switch request.Kind {
	case accessRequestKindMembership:
		_, err := admitWorkspaceMember(app, workspaceMemberAdmissionRequest{
			ActorID: deciderID, WorkspaceID: request.WorkspaceID, UserID: request.RequesterID,
		})
		return err
	case accessRequestKindMember:
		_, err := admitWorkspaceMember(app, workspaceMemberAdmissionRequest{
			ActorID: deciderID, WorkspaceID: request.WorkspaceID, Email: request.PrincipalID, ResolveByEmail: true,
		})
		return err
	case accessRequestKindGuest:
		// The person who asked shares, as they would have themselves.
		return grantAccessRequestShare(withApprovedGuestInvite(ctx), app, request.RequesterID, request,
			request.PrincipalType, request.PrincipalID)
	}
	// The page is shared with the person who asked: by their account when
	// they belong to the organization, else by their address, which brings
	// them in as a guest; an account with no address (one made from a link
	// with a name only) comes in as a guest by itself, where guests may.
	principalType, principalID := publicMutationPrincipalUser, request.RequesterID
	workspace, err := app.FindRecordById(workspacesCollection, request.WorkspaceID)
	if err != nil {
		return err
	}
	organizationID := workspace.GetString("organization")
	inOrganization := false
	if organizationID != "" {
		_, err := authorizeOrganization(app, request.RequesterID, organizationID, permissionOrganizationActive)
		if err != nil && !isPermissionDenied(err) {
			return err
		}
		inOrganization = err == nil
	}
	if !inOrganization {
		requester, err := app.FindRecordById("users", request.RequesterID)
		if err != nil {
			return err
		}
		if email := normalizePageShareEmail(requester.GetString("email")); email != "" {
			principalType, principalID = publicMutationPrincipalEmail, email
		} else if err := admitAccessRequestGuest(app, organizationID, deciderID, request.RequesterID); err != nil {
			return err
		}
	}
	// One already shared with the person, by account or by one of their
	// addresses, is raised to what they asked for rather than joined by a
	// second share; one already at least that stays.
	existing, found, err := existingAccessRequestShare(app, request)
	if err != nil {
		return err
	}
	if found {
		if publicMutationRoleRank(existing.Role) >= publicMutationRoleRank(request.Role) {
			return nil
		}
		return raiseAccessRequestShare(ctx, app, deciderID, request, existing.ID)
	}
	return grantAccessRequestShare(ctx, app, deciderID, request, principalType, principalID)
}

// admitAccessRequestGuest brings an account with no address into the
// organization as a guest, as sharing with an address does, unless the
// organization keeps guests out.
func admitAccessRequestGuest(app core.App, organizationID, deciderID, userID string) error {
	if organizationID == "" {
		return refuse(errAccessRequestInvalid, "the workspace belongs to no organization that takes guests")
	}
	options, err := sharingSecurityShareOptions(app, organizationID)
	if err != nil {
		return err
	}
	if !options.Guests {
		return refuse(errAccessRequestInvalid, "the organization keeps guests out")
	}
	return admitOrganizationGuest(app, organizationID, deciderID, userID)
}

func publicMutationRoleRank(role string) int {
	switch role {
	case publicMutationRoleView:
		return 1
	case publicMutationRoleComment:
		return 2
	case publicMutationRoleEdit:
		return 3
	case publicMutationRoleFullAccess:
		return 4
	}
	return 0
}

type accessRequestShare struct {
	ID   string `db:"id"`
	Role string `db:"role"`
}

// existingAccessRequestShare is the page's direct share with the person who
// asked — by account, or by one of their confirmed addresses — at its
// highest role.
func existingAccessRequestShare(app core.App, request storedAccessRequest) (accessRequestShare, bool, error) {
	addresses, err := verifiedAccountEmails(app, request.RequesterID)
	if err != nil {
		return accessRequestShare{}, false, err
	}
	if requester, err := app.FindRecordById("users", request.RequesterID); err == nil {
		addresses = append(addresses, requester.GetString("email"))
	} else if !errors.Is(err, sql.ErrNoRows) {
		return accessRequestShare{}, false, err
	}
	normalized := make([]string, 0, len(addresses))
	for _, address := range addresses {
		if value := normalizePageShareEmail(address); value != "" {
			normalized = append(normalized, value)
		}
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return accessRequestShare{}, false, err
	}
	shares := []accessRequestShare{}
	if err := app.DB().NewQuery(`SELECT [[id]],[[role]] FROM {{leafen_page_permissions}}
		WHERE [[page_id]]={:pageId} AND (
			([[principal_type]]='user' AND [[principal_id]]={:userId})
			OR ([[principal_type]]='email' AND lower(trim([[principal_id]])) IN (SELECT [[value]] FROM json_each({:emails}))))
		ORDER BY [[id]]`).Bind(dbx.Params{
		"pageId": request.PageID, "userId": request.RequesterID, "emails": string(encoded),
	}).All(&shares); err != nil {
		return accessRequestShare{}, false, err
	}
	if len(shares) == 0 {
		return accessRequestShare{}, false, nil
	}
	best := shares[0]
	for _, share := range shares[1:] {
		if publicMutationRoleRank(share.Role) > publicMutationRoleRank(best.Role) {
			best = share
		}
	}
	return best, true, nil
}

func raiseAccessRequestShare(
	ctx context.Context, app core.App, actorID string, request storedAccessRequest, permissionID string,
) error {
	revision := 0
	stored, err := loadStoredPublicMutationState(app, request.PageID)
	if err == nil {
		revision = stored.Revision
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, _, err = executePublicMutationTransaction(ctx, app, actorID, request.PageID, publicMutationInput{
		RequestID: "access-request-" + request.ID, ExpectedRevision: revision,
		Action: publicMutationActionUpdatePermission, PermissionID: permissionID, Role: request.Role,
	})
	return err
}

func grantAccessRequestShare(
	ctx context.Context, app core.App, actorID string, request storedAccessRequest, principalType, principalID string,
) error {
	revision := 0
	stored, err := loadStoredPublicMutationState(app, request.PageID)
	if err == nil {
		revision = stored.Revision
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, _, err = executePublicMutationTransaction(ctx, app, actorID, request.PageID, publicMutationInput{
		RequestID: "access-request-" + request.ID, ExpectedRevision: revision,
		Action: publicMutationActionGrantPermission, PrincipalType: principalType, PrincipalID: principalID,
		Role: request.Role,
	})
	return err
}
