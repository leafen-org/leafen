package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

var (
	errPublicMutationInvalid          = errors.New(errorCodePublicMutationInvalid)
	errPublicMutationNotFound         = errors.New(errorCodePublicMutationNotFound)
	errPublicMutationRevisionConflict = errors.New(errorCodePublicMutationRevisionConflict)
	errPublicMutationRequestReused    = errors.New(errorCodePublicMutationRequestReused)
	// Another site already has the address being published to.
	errPublicMutationSiteAddressTaken = errors.New(errorCodePublicMutationSiteAddressTaken)
	errPublicMutationFailed           = errors.New(errorCodePublicMutationFailed)
	// The page is in the trash, or under a page in it (refusePublicMutationInTrash).
	errPublicMutationInTrash = errors.New(errorCodePageInTrash)
)

const publicMutationLockNamespace = "public-mutation"

type storedPublicMutationState struct {
	WorkspaceID     string `db:"workspace_id"`
	PageID          string `db:"page_id"`
	Revision        int    `db:"revision"`
	ShareLinkID     string `db:"share_link_id"`
	SiteID          string `db:"site_id"`
	UpdatedBy       string `db:"updated_by"`
	UpdatedAt       int64  `db:"updated_at"`
	AgentGrantScope string `db:"agent_grant_scope"`
}

type storedPublicMutationOperation struct {
	ID           string `db:"id"`
	WorkspaceID  string `db:"workspace_id"`
	PageID       string `db:"page_id"`
	ActorID      string `db:"actor_id"`
	RequestID    string `db:"request_id"`
	RequestHash  string `db:"request_hash"`
	Action       string `db:"action"`
	Revision     int    `db:"revision"`
	Status       string `db:"status"`
	ErrorCode    string `db:"error_code"`
	ResponseJSON string `db:"response_json"`
	CreatedAt    int64  `db:"created_at"`
	UpdatedAt    int64  `db:"updated_at"`
}

type storedPublicMutationShareLink struct {
	ID             string `db:"id"`
	WorkspaceID    string `db:"workspace_id"`
	RootPageID     string `db:"root_page_id"`
	TokenCipher    string `db:"token_cipher"`
	Enabled        bool   `db:"enabled"`
	WebEnabled     bool   `db:"web_enabled"`
	ExpiresAt      string `db:"expires_at"`
	SourceRevision int    `db:"source_revision"`
	Role           string `db:"role"`
}

type storedPublicMutationSite struct {
	ID                string `db:"id"`
	WorkspaceID       string `db:"workspace_id"`
	RootPageID        string `db:"root_page_id"`
	ShareLinkID       string `db:"share_link_id"`
	Slug              string `db:"slug"`
	Title             string `db:"title"`
	Description       string `db:"description"`
	Theme             string `db:"theme"`
	ShowBreadcrumbs   bool   `db:"show_breadcrumbs"`
	ShowSearch        bool   `db:"show_search"`
	ShowBranding      bool   `db:"show_branding"`
	NavigationPageIDs string `db:"navigation_page_ids"`
	Published         bool   `db:"published"`
}

type storedPublicMutationPermission struct {
	ID             string `db:"id"`
	PrincipalType  string `db:"principal_type"`
	PrincipalID    string `db:"principal_id"`
	PrincipalLabel string `db:"principal_label"`
	PrincipalEmail string `db:"principal_email"`
	PrincipalUser  string `db:"principal_user_id"`
	Guest          bool   `db:"principal_guest"`
	Restricted     bool   `db:"principal_restricted"`
	Role           string `db:"role"`
	CreatedAt      int64  `db:"created_at"`
	// The property of the page-level rule the share comes from, if any, or
	// the rule's built-in "생성자".
	Rule        string `db:"rule"`
	RuleCreator bool   `db:"rule_creator"`
	// An address no account has carries its sign-up token, for the page's
	// managers to copy where the server sends no mail (signup_invitations.go).
	SignupToken string `db:"signup_token"`
	// Its invitation email, when it didn't go: "limited", the limits held it
	// back, or "failed" (page_share_invite_emails.go).
	InviteMail string `db:"invite_mail"`
}

func normalizePublicMutationInput(input *publicMutationInput) {
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.Action = strings.TrimSpace(input.Action)
	input.ExpiresAt = strings.TrimSpace(input.ExpiresAt)
	input.Slug = strings.ToLower(strings.TrimSpace(input.Slug))
	input.Title = strings.TrimSpace(input.Title)
	input.Description = strings.TrimSpace(input.Description)
	input.Theme = strings.TrimSpace(input.Theme)
	input.PrincipalType = strings.TrimSpace(input.PrincipalType)
	input.PrincipalID = strings.TrimSpace(input.PrincipalID)
	if input.PrincipalType == publicMutationPrincipalEmail {
		input.PrincipalID = normalizePageShareEmail(input.PrincipalID)
	}
	input.Role = strings.TrimSpace(input.Role)
	input.PermissionID = strings.TrimSpace(input.PermissionID)
	input.InviteMessage = strings.TrimSpace(input.InviteMessage)
	seen := map[string]struct{}{}
	navigation := make([]string, 0, len(input.NavigationPageIDs))
	for _, raw := range input.NavigationPageIDs {
		pageID := strings.TrimSpace(raw)
		if pageID == "" {
			continue
		}
		if _, exists := seen[pageID]; exists {
			continue
		}
		seen[pageID] = struct{}{}
		navigation = append(navigation, pageID)
	}
	input.NavigationPageIDs = navigation
	normalizePublicSitePreviewInput(input)
}

func validPublicMutationExpiry(value string) bool {
	if value == "" {
		return true
	}
	_, err := time.Parse(time.RFC3339Nano, value)
	return err == nil
}

func validPublicMutationPrincipal(value string) bool {
	return value == publicMutationPrincipalUser || value == publicMutationPrincipalEmail ||
		value == publicMutationPrincipalGroup || value == publicMutationPrincipalIntegration || value == publicMutationPrincipalWorkspace
}

func validPublicMutationRole(value string) bool {
	return value == publicMutationRoleView || value == publicMutationRoleComment ||
		value == publicMutationRoleEdit || value == publicMutationRoleFullAccess
}

func publicMutationRequestHash(pageID string, input publicMutationInput) (string, error) {
	encoded, err := json.Marshal(struct {
		PageID string              `json:"pageId"`
		Input  publicMutationInput `json:"input"`
	}{PageID: pageID, Input: input})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func authorizePublicMutationManager(app core.App, actorID, pageID string) (pageAuthorization, error) {
	return authorizePublicMutationAccess(app, actorID, pageID, pageAccessRoleFullAccess)
}

// authorizePublicMutationFor is who may make a change to what a page shares.
// A workspace moved to another server takes no changes (workspace_move_
// fence.go), but what it made public may still be turned off — a site taken
// down, a link turned off — by its owner with full access to the page, so a
// page public here and on the new server can be closed here
// (workspace_move_follow.go, user decision 2026-11-07).
func authorizePublicMutationFor(app core.App, actorID, pageID string, input publicMutationInput) (pageAuthorization, error) {
	turnsOff := input.Action == publicMutationActionUnpublishSite ||
		input.Action == publicMutationActionSetWebSharing && input.Enabled != nil && !*input.Enabled
	if !turnsOff {
		authorization, err := authorizePublicMutationChange(app, actorID, pageID, input)
		if err != nil {
			return pageAuthorization{}, err
		}
		return authorization, refusePublicMutationInTrashFor(app, pageID, input)
	}
	page, err := app.FindRecordById(pagesCollection, pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return pageAuthorization{}, errPublicMutationNotFound
	}
	if err != nil {
		return pageAuthorization{}, err
	}
	workspaceID := page.GetString("workspace")
	if _, moved := workspaceMovedTo(app, workspaceID); !moved {
		return authorizePublicMutationManager(app, actorID, pageID)
	}
	authorization, err := authorizePublicMutationAccess(app, actorID, pageID, pageAccessRoleView)
	if err != nil {
		return pageAuthorization{}, err
	}
	if authorization.AccessRole < pageAccessRoleFullAccess {
		return pageAuthorization{}, errPublicMutationNotFound
	}
	if _, err := authorizeWorkspace(app, actorID, workspaceID, permissionWorkspaceOwner); err != nil {
		return pageAuthorization{}, refusedUnlessStorage(errPublicMutationNotFound, err)
	}
	return authorization, nil
}

// Anyone who opens a page reads who has it, as Notion's share window (seen
// 2026-11-04); only full access changes it.
func authorizePublicMutationReader(app core.App, actorID, pageID string) (pageAuthorization, error) {
	return authorizePublicMutationAccess(app, actorID, pageID, pageAccessRoleView)
}

func authorizePublicMutationAccess(app core.App, actorID, pageID string, minimum pageAccessRole) (pageAuthorization, error) {
	page, err := app.FindRecordById(pagesCollection, pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return pageAuthorization{}, errPublicMutationNotFound
	}
	if err != nil {
		return pageAuthorization{}, err
	}
	var authorization pageAuthorization
	// Sharing uses the row's own full-access authority, without enabling generic page-content writes
	// on database row records; its reader reads as a page (a person's page's own shares, people_database_access.go).
	if isDatabaseRowPageRecord(page) && minimum > pageAccessRoleView {
		authorization, err = authorizeRowPageRecord(app, actorID, page, minimum)
	} else {
		authorization, err = authorizePageAccessRole(app, actorID, pageID, minimum)
	}
	if isPermissionDenied(err) {
		return pageAuthorization{}, refusalKeepingHold(err, errPublicMutationNotFound)
	}
	return authorization, err
}

func enforcePublicMutationPolicies(app core.App, authorization pageAuthorization, input publicMutationInput) error {
	if input.Action == publicMutationActionUpdatePermission {
		permission, err := loadPublicMutationPermission(app, storedPublicMutationState{WorkspaceID: authorization.Workspace.WorkspaceID, PageID: authorization.Page.Id}, input.PermissionID)
		if errors.Is(err, sql.ErrNoRows) {
			return errPublicMutationNoSuchShare
		}
		if err != nil {
			return err
		}
		if permission.PrincipalType == publicMutationPrincipalWorkspace {
			if _, err := authorizeWorkspace(app, authorization.Workspace.PrincipalID, authorization.Workspace.WorkspaceID, permissionWorkspaceRead); err != nil {
				return err
			}
		}
	}
	action := ""
	if input.Action == publicMutationActionRotateShareToken || input.Action == publicMutationActionPublishSite ||
		(input.Action == publicMutationActionSetWebSharing && input.Enabled != nil && *input.Enabled) {
		action = dlpOutboundPublicSharing
	} else if input.Action == publicMutationActionGrantPermission || input.Action == publicMutationActionUpdatePermission {
		action = dlpOutboundExternalSharing
	}
	if action == "" {
		return nil
	}
	organizationID := authorization.Workspace.Organization.OrganizationID
	if err := enforceOrganizationDLPOutbound(app, organizationID, action); err != nil {
		return err
	}
	// The organization's sharing security settings apply as well, then the
	// page's teamspace's own.
	switch {
	case action == dlpOutboundPublicSharing:
		if err := enforceSharingSecurityPublicSharing(app, organizationID); err != nil {
			return err
		}
		return enforceTeamspacePublishing(app, authorization.Page.Id)
	case input.Action == publicMutationActionGrantPermission:
		if err := enforceSharingSecurityShare(
			app, organizationID, authorization.Workspace.WorkspaceID, input.PrincipalType, input.PrincipalID,
		); err != nil {
			return err
		}
		return enforceTeamspaceShare(
			app, authorization.Page.Id, organizationID, authorization.Workspace.WorkspaceID, input.PrincipalType, input.PrincipalID,
		)
	}
	return nil
}

func loadStoredPublicMutationState(app core.App, pageID string) (storedPublicMutationState, error) {
	row := storedPublicMutationState{}
	err := app.DB().NewQuery(`
		SELECT [[workspace_id]],[[page_id]],[[revision]],[[share_link_id]],[[site_id]],[[updated_by]],[[updated_at]],
			[[agent_grant_scope]]
		FROM {{leafen_public_mutation_states}} WHERE [[page_id]]={:pageId}
	`).Bind(dbx.Params{"pageId": pageID}).One(&row)
	return row, err
}

func loadPublicMutationOperationByRequest(
	app core.App, actorID, requestID string,
) (storedPublicMutationOperation, error) {
	row := storedPublicMutationOperation{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[workspace_id]],[[page_id]],[[actor_id]],[[request_id]],[[request_hash]],[[action]],
			[[revision]],[[status]],[[error_code]],[[response_json]],[[created_at]],[[updated_at]]
		FROM {{leafen_public_mutation_operations}}
		WHERE [[actor_id]]={:actorId} AND [[request_id]]={:requestId}
	`).Bind(dbx.Params{"actorId": actorID, "requestId": requestID}).One(&row)
	return row, err
}

func readPublicMutationState(
	ctx context.Context,
	app core.App,
	authorization pageAuthorization,
	permissionCursor, principalCursor string,
	includePermissions, includePrincipals bool,
) (publicMutationState, error) {
	stored, err := loadStoredPublicMutationState(app, authorization.Page.Id)
	if errors.Is(err, sql.ErrNoRows) {
		stored = storedPublicMutationState{
			WorkspaceID: authorization.Workspace.WorkspaceID, PageID: authorization.Page.Id,
		}
	} else if err != nil {
		return publicMutationState{}, err
	}
	state := publicMutationState{
		PageID: stored.PageID, WorkspaceID: stored.WorkspaceID, Revision: stored.Revision,
		Permissions: []publicMutationPermission{}, PrincipalOptions: []publicMutationPrincipalOption{},
		AgentGrantScope: agentGrantScopeFullAccess,
	}
	if stored.AgentGrantScope == agentGrantScopeAccess {
		state.AgentGrantScope = agentGrantScopeAccess
	}
	// "고급" counts its page-level rules and its properties' access rules.
	if err := app.DB().NewQuery(`
		SELECT (SELECT COUNT(*) FROM {{leafen_database_page_rules}} WHERE [[workspace_id]]={:workspaceId} AND [[page_id]]={:pageId})
			+ (SELECT COUNT(*) FROM {{leafen_database_property_access}} [[access]]
				JOIN {{leafen_data_sources}} [[source]] ON [[source]].[[id]]=[[access]].[[data_source_id]]
					AND [[source]].[[page_id]]={:pageId} AND [[source]].[[wiki]]=0
				WHERE ` + databasePropertyNotTrashedSQL("[[access]].[[property_id]]") + `)
	`).Bind(dbx.Params{"workspaceId": authorization.Workspace.WorkspaceID, "pageId": authorization.Page.Id}).Row(&state.PageRuleCount); err != nil {
		return publicMutationState{}, err
	}
	if state.PageID == "" {
		state.PageID, state.WorkspaceID = authorization.Page.Id, authorization.Workspace.WorkspaceID
	}
	if err := populatePublicMutationPrincipalDirectory(
		ctx, app, authorization, principalCursor, includePrincipals, &state,
	); err != nil {
		return publicMutationState{}, err
	}
	options, err := sharingSecurityShareOptions(app, authorization.Workspace.Organization.OrganizationID)
	if err != nil {
		return publicMutationState{}, err
	}
	state.PublishingAllowed, state.GuestsAllowed, state.ExternalEmailAllowed =
		options.Publishing, options.Guests, options.ExternalEmail
	teamspaceSecurity, err := teamspacePageSecurity(app, authorization.Page.Id)
	if err != nil {
		return publicMutationState{}, err
	}
	state.PublishingAllowed = state.PublishingAllowed && !teamspaceSecurity.DisablePublishing
	state.GuestsAllowed = state.GuestsAllowed && !teamspaceSecurity.DisableGuests
	state.ExternalEmailAllowed = state.ExternalEmailAllowed && !teamspaceSecurity.DisableGuests
	if stored.ShareLinkID != "" {
		share, err := loadPublicMutationShareLink(app, stored.ShareLinkID, stored.WorkspaceID, stored.PageID)
		if err != nil {
			return publicMutationState{}, err
		}
		token, err := decryptAdministratorSetting(app, share.TokenCipher)
		if err != nil {
			return publicMutationState{}, err
		}
		state.WebSharingEnabled = share.WebEnabled
		state.ShareLink = &publicMutationShareLink{
			ID: share.ID, Token: token, Enabled: share.WebEnabled, ExpiresAt: share.ExpiresAt, Role: share.Role,
		}
	}
	if stored.SiteID != "" {
		site, err := loadPublicMutationSite(app, stored.SiteID, stored.WorkspaceID, stored.PageID)
		if err != nil {
			return publicMutationState{}, err
		}
		navigation := []string{}
		if err := json.Unmarshal([]byte(site.NavigationPageIDs), &navigation); err != nil {
			return publicMutationState{}, err
		}
		preview, err := loadPublicSitePreview(app, site.ID)
		if err != nil {
			return publicMutationState{}, err
		}
		state.Site = &publicMutationSite{
			ID: site.ID, Slug: site.Slug, Title: site.Title, Description: site.Description,
			Theme: site.Theme, ShowBreadcrumbs: site.ShowBreadcrumbs, ShowSearch: site.ShowSearch,
			ShowBranding: site.ShowBranding, NavigationPageIDs: navigation, Published: site.Published,
			PreviewImageFileID: preview.PreviewImageFileID, FaviconFileID: preview.FaviconFileID,
			GoogleAnalyticsID: preview.GoogleAnalyticsID, AllowIndexing: preview.AllowIndexing, AllowDuplicate: preview.AllowDuplicate,
			ShowShare: preview.ShowShare,
		}
		// "검색 엔진 인덱싱" shows the words the site's head gives its first
		// page while the site keeps no description of its own
		// (publicSiteDocumentHTML), as Notion's preview and field do.
		if site.Published {
			state.Site.DefaultDescription = publicPageDescription(app, stored.WorkspaceID, stored.PageID)
		}
	}
	if includePermissions {
		cursor, err := decodePublicMutationPermissionCursor(
			permissionCursor, authorization.Workspace.PrincipalID, state.WorkspaceID, state.PageID, state.Revision,
		)
		if err != nil {
			return publicMutationState{}, err
		}
		permissions := []storedPublicMutationPermission{}
		err = app.DB().NewQuery(`
			SELECT [[permission]].[[id]],[[permission]].[[principal_type]],[[permission]].[[principal_id]],
				` + publicMutationPrincipalColumnsSQL + `,
				[[permission]].[[role]],[[permission]].[[created_at]],
				COALESCE((SELECT [[ruleProperty]].[[name]] FROM {{leafen_database_page_rules}} [[pageRule]]
					JOIN {{leafen_database_properties}} [[ruleProperty]] ON [[ruleProperty]].[[id]]=[[pageRule]].[[property_id]]
					WHERE [[pageRule]].[[id]]=[[permission]].[[rule_id]]), '') AS [[rule]],
				EXISTS (SELECT 1 FROM {{leafen_database_page_rules}} [[creatorRule]]
					WHERE [[creatorRule]].[[id]]=[[permission]].[[rule_id]] AND [[creatorRule]].[[property_id]]='created_by') AS [[rule_creator]],
				CASE WHEN [[permission]].[[principal_type]]='email' AND [[invited]].[[id]] IS NULL THEN COALESCE((SELECT [[signup]].[[token]]
					FROM {{leafen_signup_invitations}} [[signup]]
					WHERE [[signup]].[[kind]]='page' AND [[signup]].[[source_id]]=[[permission]].[[id]]), '') ELSE '' END AS [[signup_token]],
				CASE WHEN [[permission]].[[principal_type]]='email' AND [[invited]].[[id]] IS NULL THEN COALESCE((SELECT CASE
					WHEN [[mail]].[[error_code]]='rate_limited' THEN 'limited'
					WHEN [[mail]].[[status]]='failed' OR ([[mail]].[[status]]='pending' AND [[mail]].[[error_code]]!='')
						OR ([[mail]].[[status]]='skipped' AND [[mail]].[[error_code]]='email_unavailable') THEN 'failed' ELSE '' END
					FROM {{leafen_page_invite_emails}} [[mail]] WHERE [[mail]].[[share_id]]=[[permission]].[[id]]), '') ELSE '' END AS [[invite_mail]]
			FROM {{leafen_page_permissions}} [[permission]] INDEXED BY [[idx_leafen_page_permissions_roster]]
			` + publicMutationPrincipalJoinsSQL + `
			WHERE [[permission]].[[workspace_id]]={:workspaceId} AND [[permission]].[[page_id]]={:pageId}
				AND [[permission]].[[principal_type]]!='workspace'
				AND ({:afterId}='' OR [[permission]].[[created_at]]>{:afterCreated}
					OR ([[permission]].[[created_at]]={:afterCreated} AND [[permission]].[[id]]>{:afterId}))
			ORDER BY [[permission]].[[created_at]],[[permission]].[[id]] LIMIT {:limit}
		`).WithContext(ctx).Bind(dbx.Params{
			"workspaceId": state.WorkspaceID, "pageId": state.PageID,
			"afterCreated": cursor.CreatedAt, "afterId": cursor.ID,
			"limit": publicMutationPermissionWindow + 1,
		}).All(&permissions)
		if err != nil {
			return publicMutationState{}, err
		}
		hasMore := len(permissions) > publicMutationPermissionWindow
		if hasMore {
			permissions = permissions[:publicMutationPermissionWindow]
		}
		for _, permission := range permissions {
			state.Permissions = append(state.Permissions, publicMutationPermission{
				ID: permission.ID, PrincipalType: permission.PrincipalType,
				PrincipalID: permission.PrincipalID, PrincipalLabel: permission.PrincipalLabel,
				PrincipalEmail: permission.PrincipalEmail, PrincipalUserID: permission.PrincipalUser, Role: permission.Role,
				Guest: permission.Guest, Restricted: permission.Restricted,
				Rule: permission.Rule, RuleCreator: permission.RuleCreator, SignupToken: permission.SignupToken,
				InviteMail: permission.InviteMail,
			})
		}
		if hasMore {
			last := permissions[len(permissions)-1]
			next, err := encodePublicMutationPermissionCursor(publicMutationPermissionCursor{
				Version:     publicMutationPermissionCursorVersion,
				PrincipalID: authorization.Workspace.PrincipalID,
				WorkspaceID: state.WorkspaceID, PageID: state.PageID, Revision: state.Revision,
				CreatedAt: last.CreatedAt, ID: last.ID,
			})
			if err != nil {
				return publicMutationState{}, err
			}
			state.PermissionsNextCursor = &next
		}
	}
	if err := populatePublicMutationAccessSources(ctx, app, authorization, &state); err != nil {
		return publicMutationState{}, err
	}
	if includePermissions {
		if err := populatePublicMutationInheritedAccess(ctx, app, authorization, &state); err != nil {
			return publicMutationState{}, err
		}
	}
	if err := populatePublicMutationInheritedWebLink(ctx, app, &state); err != nil {
		return publicMutationState{}, err
	}
	state.ViewerRole = publicMutationRoleFor(authorization.AccessRole)
	// Read as it is, changed once restored (refusePublicMutationInTrash).
	if state.InTrash, err = pageInTrashPath(app, state.PageID); err != nil {
		return publicMutationState{}, err
	}
	if _, state.ViewerGuest, err = publicMutationReaderSeesPeople(app, authorization); err != nil {
		return publicMutationState{}, err
	}
	state.CanManage = authorization.AccessRole >= pageAccessRoleFullAccess
	if !state.CanManage {
		if err := redactPublicMutationStateForReader(app, authorization, &state); err != nil {
			return publicMutationState{}, err
		}
		return state, nil
	}
	// Who shares by address hears whether a mail goes, or that the link is theirs to hand on.
	settings, err := loadAdministratorSettings(app)
	if err != nil {
		return publicMutationState{}, err
	}
	state.EmailSends = notificationEmailConfigured(settings)
	return state, nil
}

// Someone without full access reads who has the page and how it is open. The
// web link's and the site's addresses, invite links, what would stay after a
// share is taken away and where the sharing was unlinked are for those who
// change them. Who reads only their own line still reads how the page is open
// — its general access and who may give agents access — as words, as Notion's
// restricted member does (seen 2026-11-07).
func redactPublicMutationStateForReader(app core.App, authorization pageAuthorization, state *publicMutationState) error {
	state.ShareLink, state.Site, state.ShareUnlink = nil, nil, nil
	if state.InheritedWebLink != nil {
		state.InheritedWebLink.PageID, state.InheritedWebLink.ExpiresAt = "", ""
	}
	state.PrincipalOptions, state.PrincipalOptionsNextCursor = []publicMutationPrincipalOption{}, nil
	for index := range state.Permissions {
		state.Permissions[index].SignupToken, state.Permissions[index].InviteMail, state.Permissions[index].OtherAccess = "", "", nil
	}
	seesPeople, guest, err := publicMutationReaderSeesPeople(app, authorization)
	if err != nil || seesPeople {
		return err
	}
	// Who may not read the people reads only their own line: who else a page
	// is shared with, and at what addresses, is not theirs to read.
	self, err := app.FindRecordById("users", authorization.Workspace.PrincipalID)
	if err != nil {
		return err
	}
	state.OwnLineOnly = true
	state.Permissions = []publicMutationPermission{{
		PrincipalType: publicMutationPrincipalUser, PrincipalID: self.Id,
		PrincipalLabel: firstNonEmpty(strings.TrimSpace(self.GetString("name")), self.Email()),
		PrincipalEmail: self.Email(), Role: state.ViewerRole, Guest: guest, Restricted: !guest,
	}}
	state.PermissionsNextCursor, state.InheritedGroups = nil, nil
	// The page above a web link comes from may not be theirs to read.
	if state.InheritedWebLink != nil {
		state.InheritedWebLink.PageTitle = ""
	}
	// A teamspace page's openness to the workspace, without its teamspace's people.
	if state.Teamspace != nil {
		state.Teamspace = &publicMutationTeamspace{OpenRole: state.Teamspace.OpenRole}
	}
	state.AccessOwner = publicMutationAccessOwner{}
	state.OnlyReaderOpens, state.WorkspaceSharingAllowed = false, false
	state.PageRuleCount = 0
	return nil
}

// publicMutationReaderSeesPeople says whether someone without full access may
// read who else has a page, and their addresses: the workspace's own members
// may, as they read its members, at every role below full access. A guest or
// a restricted member may not, whether they read, comment or edit: who else a
// page is shared with, and at what addresses, is not theirs to read — kept
// narrower than Notion's, whose editing guest and reading restricted member
// see the other people's rows (seen 2026-11-05 and 2026-11-12). With full
// access they manage the page's sharing and read its people as its other
// managers do. guest says which of the two they are.
func publicMutationReaderSeesPeople(app core.App, authorization pageAuthorization) (seesPeople, guest bool, err error) {
	workspace, err := authorizeWorkspace(app, authorization.Workspace.PrincipalID, authorization.Workspace.WorkspaceID, permissionWorkspaceRead)
	if isPermissionDenied(err) {
		return false, true, nil
	}
	if err != nil {
		return false, false, err
	}
	return !workspace.Restricted, false, nil
}

func loadPublicMutationShareLink(
	app core.App, id, workspaceID, pageID string,
) (storedPublicMutationShareLink, error) {
	row := storedPublicMutationShareLink{}
	err := app.DB().NewQuery(`
		SELECT [[id]],[[workspace_id]],[[root_page_id]],[[token_cipher]],[[enabled]],[[web_enabled]],[[expires_at]],
			[[source_revision]],[[role]]
		FROM {{leafen_public_share_links}}
		WHERE [[id]]={:id} AND [[workspace_id]]={:workspaceId} AND [[root_page_id]]={:pageId}
	`).Bind(dbx.Params{"id": id, "workspaceId": workspaceID, "pageId": pageID}).One(&row)
	return row, err
}

func securityRandomID() string {
	return security.RandomString(15)
}
