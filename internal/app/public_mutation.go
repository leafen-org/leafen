package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

type publicMutationTurn struct {
	Response publicMutationResponse
	Fresh    bool
}

type publicMutationApplyResult struct {
	State storedPublicMutationState
	Token string
	// Changed is who a role change was for, read before the change, since a
	// change back to what the pages above give removes the page's own share.
	Changed *storedPublicMutationPermission
}

func registerPublicMutationRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+publicMutationStatePath, readPublicMutationManagementState)
	event.Router.GET(apiRoot+publicMutationInheritedPeoplePath, readInheritedSharePeople)
	event.Router.POST(apiRoot+publicMutationMutatePath, mutatePublicVisibility).
		Bind(apis.BodyLimit(publicMutationMaxBodyBytes))
	registerPublicMutationRunnerRoutes(event)
	registerPageShareInheritanceRoute(event)
	registerPageAppConnectionRoutes(event)
	registerDatabasePageRuleRoutes(event)
	registerDeadlineRoutes(event)
	registerReadLogRoutes(event)
	registerRowSendRoutes(event)
	registerPageLinkAccessRoutes(event)
}

func readPublicMutationManagementState(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	// A page's share button and copy feedback read its reach as a summary, without the directory to invite from; the button's
	// takes the people who have the page too (people=1), which its share window opens with before its own read answers.
	summary, people := event.Request.URL.Query().Get("summary") == "1", event.Request.URL.Query().Get("people") == "1"
	authorization, err := authorizePublicMutationManager(event.App, user.Id, pageID)
	// Someone below full access, or with it on a page that takes no changes
	// for now (a copy still being made into it), reads the page's reach as
	// their share reads it (redactPublicMutationStateForReader): how the page
	// is open, which its share button shows as Notion's does for every role
	// (2026-11-16: answered with no reach, a member's button said "shared
	// with people" on a page open to the whole workspace).
	if errors.Is(err, errPublicMutationNotFound) {
		authorization, err = authorizePublicMutationReader(event.App, user.Id, pageID)
	}
	if err != nil {
		return publicMutationRouteError(event, err, "state-authorization")
	}
	permissionCursor := strings.TrimSpace(event.Request.URL.Query().Get("permissionCursor"))
	principalCursor := strings.TrimSpace(event.Request.URL.Query().Get("principalCursor"))
	if permissionCursor != "" && principalCursor != "" {
		return publicMutationRouteError(event, publicMutationRefused("permissionCursor and principalCursor go one at a time"), "state-cursors")
	}
	manages := authorization.AccessRole >= pageAccessRoleFullAccess
	state, err := readPublicMutationState(
		event.Request.Context(), event.App, authorization, permissionCursor, principalCursor,
		(!summary || people) && principalCursor == "", manages && !summary && permissionCursor == "",
	)
	if err != nil {
		return publicMutationRouteError(event, err, "state-read")
	}
	return event.JSON(http.StatusOK, publicMutationStateResponse{State: state})
}

func mutatePublicVisibility(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := publicMutationInput{}
	if err := event.BindBody(&input); err != nil {
		return publicMutationRouteError(event, bodyRefusal(errPublicMutationInvalid, "sharing change", err), "mutation-input")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	// A key's call asks its person only for a change that would be made,
	// made once and taken back under the page's sharing lock
	// (user_key_acting_ask.go).
	if userKeyAsksWhereItActs(event) {
		release, err := lockPublicMutation(event.App, user.Id, pageID, &input)
		if err == nil {
			err = rehearseBeforeAsking(event, func(txApp core.App) error {
				_, _, err := executePublicMutationTransaction(event.Request.Context(), txApp, user.Id, pageID, input)
				return err
			})
			release()
		}
		if err != nil {
			return publicMutationRouteError(event, err, "mutation")
		}
		if asked, err := askBeforeActing(event); asked {
			return err
		}
	}
	response, fresh, err := executePublicMutation(event.Request.Context(), event.App, user.Id, pageID, input)
	if err != nil {
		return publicMutationRouteError(event, err, "mutation")
	}
	httpStatus := http.StatusOK
	if fresh {
		httpStatus = http.StatusAccepted
	}
	return event.JSON(httpStatus, response)
}

func executePublicMutation(
	ctx context.Context,
	app core.App, actorID, pageID string, input publicMutationInput,
) (publicMutationResponse, bool, error) {
	release, err := lockPublicMutation(app, actorID, pageID, &input)
	if err != nil {
		return publicMutationResponse{}, false, err
	}
	defer release()

	return executePublicMutationTransaction(ctx, app, actorID, pageID, input)
}

// lockPublicMutation checks a change of a page's sharing and holds the
// page's sharing lock for it, until release.
func lockPublicMutation(app core.App, actorID, pageID string, input *publicMutationInput) (func(), error) {
	normalizePublicMutationInput(input)
	if actorID == "" || pageID == "" {
		return nil, errPublicMutationInvalid
	}
	if err := validatePublicMutationInput(*input); err != nil {
		return nil, err
	}
	authorization, err := authorizePublicMutationFor(app, actorID, pageID, *input)
	if err != nil {
		return nil, err
	}
	return acquireProductOperationLock(
		publicMutationLockNamespace, authorization.Workspace.WorkspaceID+"\x00"+pageID,
	), nil
}

func applyPublicMutation(
	ctx context.Context, app core.App,
	authorization pageAuthorization,
	stored storedPublicMutationState,
	input publicMutationInput,
	revision int,
	now time.Time,
) (publicMutationApplyResult, error) {
	stored.Revision, stored.UpdatedBy, stored.UpdatedAt = revision, authorization.Workspace.PrincipalID, now.UnixMilli()
	result := publicMutationApplyResult{State: stored}
	err := refuseGeneralAccessChange(app, authorization, stored, input)
	if err != nil {
		return result, err
	}
	switch input.Action {
	case publicMutationActionSetWebSharing:
		result.State, result.Token, err = setPublicMutationWebSharing(app, result.State, input, revision, now)
	case publicMutationActionRotateShareToken:
		result.State, result.Token, err = rotatePublicMutationShareToken(app, result.State, revision, now)
	case publicMutationActionPublishSite:
		result.State, result.Token, err = publishPublicMutationSite(app, result.State, input, revision, now)
		if err == nil {
			err = applyPublicSitePreview(app, result.State, input)
		}
	case publicMutationActionUnpublishSite:
		err = unpublishPublicMutationSite(app, result.State, revision, now)
	case publicMutationActionGrantPermission:
		err = grantPublicMutationPermissionTelling(ctx, app, authorization, input, now)
	case publicMutationActionUpdatePermission:
		if changed, readErr := loadPublicMutationPermission(app, result.State, input.PermissionID); readErr == nil {
			result.Changed = &changed
		} else if !errors.Is(readErr, sql.ErrNoRows) {
			return result, readErr
		}
		err = updateSharedPermissionTelling(app, authorization, result.State, input, result.Changed, now)
		if err == nil && !isDatabaseRowPageRecord(authorization.Page) {
			err = dropShareOverrideMatchingAbove(app, result.State, input.PermissionID)
		}
	case publicMutationActionRemovePermission:
		err = removeSharedPermissionTelling(app, authorization, result.State, input, now)
	default:
		err = publicMutationRefused("action %q isn't a sharing change", input.Action)
	}
	// A share of an unlinked database item changes who its database's views
	// show it to.
	switch input.Action {
	case publicMutationActionGrantPermission, publicMutationActionUpdatePermission, publicMutationActionRemovePermission:
		if err == nil && isDatabaseRowPageRecord(authorization.Page) {
			var unlinked bool
			if unlinked, err = pageShareUnlinked(app, authorization.Page.Id); err == nil && unlinked {
				err = announceRowShareChange(app, authorization.Page)
			}
		}
	}
	return result, err
}

func grantPublicMutationPermission(
	ctx context.Context, app core.App, authorization pageAuthorization, input publicMutationInput, now time.Time,
) error {
	if input.PrincipalType == publicMutationPrincipalWorkspace {
		if input.PrincipalID != authorization.Workspace.WorkspaceID {
			return publicMutationRefused("principalId of a share to the workspace is the page's workspace's id")
		}
		if _, err := authorizeWorkspace(app, authorization.Workspace.PrincipalID, input.PrincipalID, permissionWorkspaceRead); err != nil {
			return err
		}
	}
	if input.PrincipalType == publicMutationPrincipalUser {
		if _, err := authorizeOrganization(
			app, input.PrincipalID, authorization.Workspace.Organization.OrganizationID,
			permissionOrganizationActive,
		); err != nil {
			return publicMutationRefused("principalId names no person of this workspace's organization; share by email to invite someone new")
		}
	}
	stored, err := pageShareStoredPrincipal(app, authorization.Page.Id, input.PrincipalType, input.PrincipalID)
	if err != nil {
		return err
	}
	existed := stored != ""
	if existed {
		// Sharing again changes the share the page already has.
		input.PrincipalID = stored
	}
	shareID := securityRandomID()
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_page_permissions}}
			([[id]],[[workspace_id]],[[page_id]],[[principal_type]],[[principal_id]],[[role]],
			 [[created_by]],[[created_at]],[[updated_at]],[[invite_message]],[[notify_on_access]])
		VALUES ({:id},{:workspaceId},{:pageId},{:principalType},{:principalId},{:role},{:actorId},{:now},{:now},{:message},{:notify})
		ON CONFLICT ([[page_id]],[[principal_type]],[[principal_id]]) DO UPDATE SET
			[[role]]=excluded.[[role]],[[updated_at]]=excluded.[[updated_at]],[[rule_id]]=''
	`).Bind(dbx.Params{
		"id": shareID, "workspaceId": authorization.Workspace.WorkspaceID,
		"pageId": authorization.Page.Id, "principalType": input.PrincipalType,
		"principalId": input.PrincipalID, "role": input.Role,
		"actorId": authorization.Workspace.PrincipalID, "now": now.UnixMilli(),
		"message": input.InviteMessage, "notify": input.NotifyOnAccess != nil && *input.NotifyOnAccess,
	}).Execute(); err != nil {
		return err
	}
	if input.PrincipalType == publicMutationPrincipalWorkspace && input.HideFromSearch != nil {
		if _, err := app.DB().NewQuery(`UPDATE {{leafen_page_permissions}} SET [[hide_from_search]]={:hide}
		 WHERE [[page_id]]={:pageId} AND [[principal_type]]='workspace' AND [[principal_id]]={:principalId}`).Bind(dbx.Params{
			"hide": *input.HideFromSearch, "pageId": authorization.Page.Id, "principalId": input.PrincipalID,
		}).Execute(); err != nil {
			return err
		}
	}
	invited := ""
	switch input.PrincipalType {
	case publicMutationPrincipalUser:
		invited = input.PrincipalID
	case publicMutationPrincipalEmail:
		// Someone outside the organization shared with by email comes in as a
		// guest.
		if err := admitEmailShareGuest(
			app, authorization.Workspace.Organization.OrganizationID, authorization.Workspace.PrincipalID, input.PrincipalID,
		); err != nil {
			return err
		}
		if invited, err = pageShareEmailAccount(app, input.PrincipalID); err != nil {
			return err
		}
	}
	// An address no account has gets a sign-up token with the share, sent in
	// its email or copied by the inviter (signup_invitations.go).
	if input.PrincipalType == publicMutationPrincipalEmail && invited == "" {
		if _, err := ensureSignupInvitation(app, signupInvitationPage, shareID, input.PrincipalID, now); err != nil {
			return err
		}
	}
	if existed {
		return nil
	}
	if input.PrincipalType == publicMutationPrincipalEmail && invited == "" {
		return queuePageInviteEmail(
			ctx, app, authorization.Workspace.WorkspaceID, authorization.Page.Id, authorization.Workspace.PrincipalID,
			input.PrincipalID, shareID, now,
		)
	}
	return notifyPageShareInvite(
		app, authorization.Workspace.WorkspaceID, authorization.Page.Id, authorization.Workspace.PrincipalID,
		invited, shareID, now, input.InviteMessage,
	)
}

// A guest with full access shares a page with people, not with the workspace
// or the web: its general access isn't theirs to change, as Notion's (seen
// 2026-11-05). Nor is People's database's anyone's (people_database_access.go).
func refuseGeneralAccessChange(
	app core.App, authorization pageAuthorization, state storedPublicMutationState, input publicMutationInput,
) error {
	general := input.Action == publicMutationActionSetWebSharing ||
		input.Action == publicMutationActionGrantPermission && input.PrincipalType == publicMutationPrincipalWorkspace
	if input.Action == publicMutationActionUpdatePermission || input.Action == publicMutationActionRemovePermission {
		var count int
		err := app.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_page_permissions}}
			WHERE [[id]]={:id} AND [[workspace_id]]={:workspaceId} AND [[page_id]]={:pageId} AND [[principal_type]]={:workspace}
		`).Bind(dbx.Params{
			"id": input.PermissionID, "workspaceId": state.WorkspaceID, "pageId": state.PageID,
			"workspace": publicMutationPrincipalWorkspace,
		}).Row(&count)
		if err != nil {
			return err
		}
		general = count > 0
	}
	if !general {
		return nil
	}
	if err := refusePeopleDatabaseGeneralAccessChange(app, authorization, input); err != nil {
		return err
	}
	_, guest, err := publicMutationReaderSeesPeople(app, authorization)
	if err != nil {
		return err
	}
	if guest {
		return publicMutationRefused("a guest shares the page with people, not with the workspace or the web")
	}
	return nil
}

func updatePublicMutationPermission(
	app core.App, state storedPublicMutationState, input publicMutationInput, now time.Time,
) error {
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_page_permissions}} SET [[role]]={:role},[[updated_at]]={:now}
		WHERE [[id]]={:id} AND [[workspace_id]]={:workspaceId} AND [[page_id]]={:pageId} AND [[rule_id]]=''
	`).Bind(dbx.Params{
		"role": input.Role, "now": now.UnixMilli(), "id": input.PermissionID,
		"workspaceId": state.WorkspaceID, "pageId": state.PageID,
	}).Execute()
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errPublicMutationNoSuchShare
	}
	return nil
}

// dropShareOverrideMatchingAbove lets a linked page inherit someone again
// once the role of its own it gives them is what the pages above give, as
// Notion's share window does (seen 2026-11-06: an inherited person raised and
// set back reads as inherited, and the "differs" note goes).
func dropShareOverrideMatchingAbove(app core.App, state storedPublicMutationState, permissionID string) error {
	unlinked, err := pageShareUnlinked(app, state.PageID)
	if err != nil || unlinked {
		return err
	}
	ids, err := loadPageAccessAncestorIDs(app, state.WorkspaceID, state.PageID)
	if err != nil || len(ids) < 2 {
		return err
	}
	encoded, err := json.Marshal(ids[1:])
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`
		DELETE FROM {{leafen_page_permissions}}
		WHERE [[id]]={:id} AND [[workspace_id]]={:workspaceId} AND [[page_id]]={:pageId} AND [[rule_id]]=''
			AND [[principal_type]]!='workspace'
			AND [[role]]=(
				SELECT [[above]].[[role]] FROM {{leafen_page_permissions}} [[above]]
				JOIN {{leafen_page_permissions}} [[own]] ON [[own]].[[id]]={:id}
				WHERE [[above]].[[workspace_id]]={:workspaceId}
					AND [[above]].[[principal_type]]=[[own]].[[principal_type]]
					AND [[above]].[[principal_id]]=[[own]].[[principal_id]]
					AND [[above]].[[page_id]] IN (SELECT [[value]] FROM json_each({:pages}))
				ORDER BY CASE [[above]].[[role]] WHEN 'view' THEN 1 WHEN 'comment' THEN 2
					WHEN 'edit' THEN 3 WHEN 'full_access' THEN 4 ELSE 0 END DESC
				LIMIT 1)
	`).Bind(dbx.Params{
		"id": permissionID, "workspaceId": state.WorkspaceID, "pageId": state.PageID, "pages": string(encoded),
	}).Execute()
	return err
}

func removePublicMutationPermission(
	app core.App, state storedPublicMutationState, input publicMutationInput,
) error {
	result, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_page_permissions}}
		WHERE [[id]]={:id} AND [[workspace_id]]={:workspaceId} AND [[page_id]]={:pageId} AND [[rule_id]]=''
	`).Bind(dbx.Params{
		"id": input.PermissionID, "workspaceId": state.WorkspaceID, "pageId": state.PageID,
	}).Execute()
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errPublicMutationNoSuchShare
	}
	// A rule that names the person gives its share back.
	return reconcileDatabasePageRuleGrantsOfPage(app, state.PageID)
}

func storePublicMutationState(app core.App, previousRevision int, state storedPublicMutationState) error {
	if previousRevision == 0 {
		_, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_public_mutation_states}}
				([[workspace_id]],[[page_id]],[[revision]],[[share_link_id]],[[site_id]],[[updated_by]],[[updated_at]])
			VALUES ({:workspaceId},{:pageId},{:revision},{:shareLinkId},{:siteId},{:updatedBy},{:updatedAt})
		`).Bind(dbx.Params{
			"workspaceId": state.WorkspaceID, "pageId": state.PageID, "revision": state.Revision,
			"shareLinkId": state.ShareLinkID, "siteId": state.SiteID,
			"updatedBy": state.UpdatedBy, "updatedAt": state.UpdatedAt,
		}).Execute()
		return err
	}
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_public_mutation_states}}
		SET [[revision]]={:revision},[[share_link_id]]={:shareLinkId},[[site_id]]={:siteId},
			[[updated_by]]={:updatedBy},[[updated_at]]={:updatedAt}
		WHERE [[workspace_id]]={:workspaceId} AND [[page_id]]={:pageId} AND [[revision]]={:previousRevision}
	`).Bind(dbx.Params{
		"workspaceId": state.WorkspaceID, "pageId": state.PageID, "revision": state.Revision,
		"previousRevision": previousRevision, "shareLinkId": state.ShareLinkID,
		"siteId": state.SiteID, "updatedBy": state.UpdatedBy, "updatedAt": state.UpdatedAt,
	}).Execute()
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return errPublicMutationRevisionConflict
	}
	return nil
}

func setPublicMutationWebSharing(
	app core.App, state storedPublicMutationState, input publicMutationInput, revision int, now time.Time,
) (storedPublicMutationState, string, error) {
	if input.Enabled == nil {
		return state, "", publicMutationRefused("enabled is true or false: whether anyone with the link opens the page")
	}
	if !*input.Enabled && state.ShareLinkID == "" {
		return state, "", nil
	}
	opening := *input.Enabled && state.ShareLinkID == ""
	share, token, err := ensurePublicMutationShareLink(app, state, revision, now, false)
	if err != nil {
		return state, "", err
	}
	// A link turned on again starts at reading, as a new one does; the role it
	// had before stays behind unless one is chosen.
	role := input.Role
	if *input.Enabled && role == "" && (opening || !share.WebEnabled) {
		role = publicMutationRoleView
	}
	// The web link is the link's own choice; its page graph stays live while
	// the page's site is published (schema_baseline.json).
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_public_share_links}} SET [[web_enabled]]={:enabled},
			[[enabled]]={:enabled} OR EXISTS (SELECT 1 FROM {{leafen_public_sites}} [[site]]
				WHERE [[site]].[[share_link_id]]={{leafen_public_share_links}}.[[id]] AND [[site]].[[published]]=1),
			[[expires_at]]={:expiresAt},[[role]]=CASE WHEN {:role}='' THEN [[role]] ELSE {:role} END,
			[[source_revision]]={:revision},[[updated_at]]={:updatedAt} WHERE [[id]]={:id}
	`).Bind(dbx.Params{
		"enabled": *input.Enabled, "expiresAt": input.ExpiresAt, "role": role, "revision": revision,
		"updatedAt": now.Format(time.RFC3339Nano), "id": share.ID,
	}).Execute()
	// A link turned off forgets who came in through it.
	if err == nil && !*input.Enabled {
		err = forgetPageLinkVisitors(app, share.ID)
	}
	// The web link sits beside the page's grant to its whole workspace, each
	// on a row of its own, as Notion's (seen 2026-11-08): turning one on or
	// off leaves the other as set.
	if err == nil && *input.Enabled {
		err = markPublicFirstPublish(app, "leafen_public_share_links", share.ID, state.UpdatedBy, now)
	}
	state.ShareLinkID = share.ID
	return state, token, err
}

func rotatePublicMutationShareToken(
	app core.App, state storedPublicMutationState, revision int, now time.Time,
) (storedPublicMutationState, string, error) {
	share, token, err := ensurePublicMutationShareLink(app, state, revision, now, true)
	if err != nil {
		return state, "", err
	}
	state.ShareLinkID = share.ID
	// The old link no longer opens anything, nor lets anyone work in it.
	return state, token, forgetPageLinkVisitors(app, share.ID)
}

func ensurePublicMutationShareLink(
	app core.App, state storedPublicMutationState, revision int, now time.Time, rotate bool,
) (storedPublicMutationShareLink, string, error) {
	share := storedPublicMutationShareLink{}
	var err error
	if state.ShareLinkID != "" {
		share, err = loadPublicMutationShareLink(app, state.ShareLinkID, state.WorkspaceID, state.PageID)
		if err != nil {
			return share, "", err
		}
	}
	token := ""
	if share.ID == "" || rotate {
		token = security.RandomString(32)
		cipher, err := encryptAdministratorSetting(app, cipherPublicShare, token)
		if err != nil {
			return share, "", err
		}
		if share.ID == "" {
			share = storedPublicMutationShareLink{
				ID: security.RandomString(15), WorkspaceID: state.WorkspaceID, RootPageID: state.PageID,
				TokenCipher: cipher, Enabled: true, SourceRevision: revision,
			}
			_, err = app.DB().NewQuery(`
				INSERT INTO {{leafen_public_share_links}}
					([[id]],[[workspace_id]],[[root_page_id]],[[token_digest]],[[token_cipher]],[[enabled]],
					 [[expires_at]],[[source_revision]],[[updated_at]])
				VALUES ({:id},{:workspaceId},{:pageId},{:digest},{:cipher},1,'',{:revision},{:updatedAt})
			`).Bind(dbx.Params{
				"id": share.ID, "workspaceId": share.WorkspaceID, "pageId": share.RootPageID,
				"digest": publicVisibilityTokenDigest(token), "cipher": cipher,
				"revision": revision, "updatedAt": now.Format(time.RFC3339Nano),
			}).Execute()
			return share, token, err
		}
		_, err = app.DB().NewQuery(`
			UPDATE {{leafen_public_share_links}} SET [[token_digest]]={:digest},[[token_cipher]]={:cipher},
				[[source_revision]]={:revision},[[updated_at]]={:updatedAt} WHERE [[id]]={:id}
		`).Bind(dbx.Params{
			"digest": publicVisibilityTokenDigest(token), "cipher": cipher, "revision": revision,
			"updatedAt": now.Format(time.RFC3339Nano), "id": share.ID,
		}).Execute()
		return share, token, err
	}
	token, err = decryptAdministratorSetting(app, share.TokenCipher)
	return share, token, err
}

func insertPublicMutationOperation(app core.App, operation storedPublicMutationOperation) error {
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_public_mutation_operations}}
			([[id]],[[workspace_id]],[[page_id]],[[actor_id]],[[request_id]],[[request_hash]],[[action]],
			 [[revision]],[[status]],[[error_code]],[[response_json]],[[created_at]],[[updated_at]])
		VALUES ({:id},{:workspaceId},{:pageId},{:actorId},{:requestId},{:requestHash},{:action},
			{:revision},{:status},'',{:response},{:createdAt},{:updatedAt})
	`).Bind(dbx.Params{
		"id": operation.ID, "workspaceId": operation.WorkspaceID, "pageId": operation.PageID,
		"actorId": operation.ActorID, "requestId": operation.RequestID,
		"requestHash": operation.RequestHash, "action": operation.Action,
		"revision": operation.Revision, "status": operation.Status,
		"response": operation.ResponseJSON, "createdAt": operation.CreatedAt, "updatedAt": operation.UpdatedAt,
	}).Execute()
	return err
}
