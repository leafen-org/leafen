package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

var errWorkspaceChangeBaselineAmbiguous = errors.New(workspaceChangeReasonRefreshBaselineAmbiguous)

// workspaceRoleGuest is the workspace role of a guest: an active member of
// the workspace's organization who is neither its owner nor its member and
// sees the pages shared with them.
const workspaceRoleGuest = "guest"

type workspaceChangeSnapshot struct {
	WorkspaceID               string `db:"workspace_id" json:"workspaceId"`
	PrincipalID               string `db:"-" json:"principalId"`
	OrganizationID            string `db:"organization_id" json:"organizationId"`
	OwnerID                   string `db:"owner_id" json:"ownerId"`
	WorkspaceRole             string `db:"workspace_role" json:"workspaceRole"`
	OrganizationRole          string `db:"organization_role" json:"organizationRole"`
	OrganizationStatus        string `db:"organization_status" json:"organizationStatus"`
	ContentRevision           int64  `db:"content_revision" json:"contentRevision"`
	PermissionCursor          int64  `db:"permission_cursor" json:"permissionCursor"`
	OrganizationPolicyVersion int64  `db:"organization_policy_version" json:"organizationPolicyVersion"`
	InstanceRevision          int64  `db:"instance_revision" json:"instanceRevision"`
	AccountEmailRevision      string `db:"-" json:"accountEmailRevision"`
	// WorkspaceSetRevision names the workspaces the person belongs to, so a
	// workspace they are added to reaches a screen already open, whose
	// workspace menu then lists it without a reload, as Notion's does.
	WorkspaceSetRevision string `db:"-" json:"workspaceSetRevision"`
	// AccessRevision is the server's access revision (ygo_access_revision_schema.go):
	// a page's sharing, a membership or a page's place changed somewhere. It
	// doesn't make the list stale by itself; it says whether a change the
	// check finds may have touched what the person may do.
	AccessRevision int64 `db:"access_revision" json:"accessRevision"`
}

func registerWorkspaceChangeCatchupRoutes(event *core.ServeEvent) {
	event.Router.Route(
		workspaceChangeProbeMethod,
		apiRoot+workspaceChangeProbePath,
		probeWorkspaceChange,
	).Bind(apis.BodyLimit(workspaceChangeProbeMaxBodyBytes))
	registerWorkspaceChangeSocketRoute(event)
}

func probeWorkspaceChange(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	if workspaceID == "" || len(workspaceID) > maxWorkspacePermissionIdentifierLength {
		return workspaceChangeProbeAPIError(
			event, http.StatusBadRequest, errorCodeWorkspaceChangeProbeInvalid,
			"Check the workspace change check request.", "input", errWorkspaceRefreshTokenInvalid,
		)
	}
	input := workspaceChangeProbeInput{}
	if err := event.BindBody(&input); err != nil {
		return workspaceChangeProbeAPIError(
			event, http.StatusBadRequest, errorCodeWorkspaceChangeProbeInvalid,
			"Check the workspace change check request.", "input", err,
		)
	}
	input.WorkspaceRefreshToken = strings.TrimSpace(input.WorkspaceRefreshToken)

	// A member's screen in view may wait for the page list or the sharing to
	// change from what they are now, or for a notice to come for them
	// (workspace_page_list_wait.go), and is then answered; a guest's waits for
	// its notices alone, as long as its ten-second check.
	// It holds none of the server's turns while it waits (whileWaiting).
	notified := false
	if input.Wait {
		ctx := event.Request.Context()
		answering := true
		if since, member := workspacePageListSince(event.App, user.Id, workspaceID); member {
			answering = whileWaiting(event.Request, func() {
				notified = workspacePageListWatchFor(event.App).wait(ctx, event.App, workspaceID, user.Id, since, true, workspacePageListWaitLimit).notified
			})
		} else if _, err := readGuestWorkspaceChangeSnapshot(event.App, user.Id, workspaceID, true); err == nil {
			answering = whileWaiting(event.Request, func() {
				notified = workspacePageListWatchFor(event.App).wait(ctx, event.App, workspaceID, user.Id, 0, false, workspaceGuestNoticeWait).notified
			})
		}
		// A wait its client left is answered with nothing; one a stopping
		// server ended is told busy, and the screen checks again once the
		// server is back (server_stopping.go).
		if ctx.Err() != nil {
			if requestClientLeft(event) {
				return nil
			}
			return serverBusy(event)
		}
		if !answering {
			return serverBusy(event)
		}
	}
	response, missing, err := answerWorkspaceChangeProbe(event.App, user, workspaceID, input.WorkspaceRefreshToken, input.PagesChangedSince)
	response.Notified = notified
	if missing {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	}
	if err != nil {
		return workspaceChangeProbeAPIError(
			event, http.StatusInternalServerError, errorCodeWorkspaceChangeProbeFailed,
			"Couldn't check the workspace change state.", "authority_snapshot", err,
		)
	}
	return event.JSON(http.StatusOK, response)
}

// A member's check that says which page list change its list holds
// (pagesChangedSince) is answered with the pages changed since, when they
// can say what changed (bootstrap_page_changes.go): the screen takes them
// over its list rather than reading it whole again.
func answerWorkspaceChangeProbe(
	app core.App, user *core.Record, workspaceID, refreshToken string, pagesChangedSince int64,
) (workspaceChangeProbeResponse, bool, error) {
	response := workspaceChangeProbeResponse{}
	missing := false
	err := runReadSnapshot(context.Background(), app, func(txApp core.App) error {
		snapshots, err := readWorkspaceChangeSnapshots(txApp, user.Id, []string{workspaceID})
		member := err == nil
		if isPermissionDenied(err) {
			// A guest follows the workspace's changes too.
			guest, guestErr := readGuestWorkspaceChangeSnapshot(txApp, user.Id, workspaceID, true)
			if isPermissionDenied(guestErr) {
				missing = true
				return nil
			}
			if guestErr != nil {
				return guestErr
			}
			snapshots, err = map[string]workspaceChangeSnapshot{workspaceID: guest}, nil
		}
		if errors.Is(err, errWorkspaceChangeBaselineAmbiguous) {
			response.Decision = workspaceChangeDecisionBootstrapRequired
			response.Reason = workspaceChangeReasonRefreshBaselineAmbiguous
			return nil
		}
		if err != nil {
			return err
		}
		snapshot := snapshots[workspaceID]
		currentToken, err := sealWorkspaceRefreshToken(user, snapshot)
		if err != nil {
			return err
		}
		response.WorkspaceRefreshToken = currentToken
		payload, err := openWorkspaceRefreshToken(user, refreshToken)
		if err != nil || payload.Snapshot.WorkspaceID != workspaceID || payload.Snapshot.PrincipalID != user.Id {
			response.Decision = workspaceChangeDecisionBootstrapRequired
			response.Reason = workspaceChangeReasonRefreshTokenInvalid
			return nil
		}
		listed := payload.Snapshot
		listed.AccessRevision = snapshot.AccessRevision
		// Beside the content, nothing of the person's place changed: the
		// pages changed since are what changed.
		contentAside := listed
		contentAside.ContentRevision = snapshot.ContentRevision
		if member && pagesChangedSince > 0 && contentAside == snapshot {
			if err := attachPageChanges(txApp, user.Id, workspaceID, pagesChangedSince, listed != snapshot, &response); err != nil {
				return err
			}
		}
		if listed == snapshot {
			response.Decision = workspaceChangeDecisionUnchanged
			response.Reason = workspaceChangeReasonUnchanged
			return nil
		}
		response.Decision = workspaceChangeDecisionBootstrapRequired
		// Content alone, or what the person may do too — a page's sharing
		// moves the content revision as well, and the access revision says
		// so: a screen reads its open page again only for the second, as the
		// page's own changes reach it through its room (2026-10-16: a large
		// open page read its whole graph again for every page made elsewhere
		// in the workspace).
		if contentAside == snapshot && payload.Snapshot.AccessRevision == snapshot.AccessRevision {
			response.Reason = workspaceChangeReasonContentChanged
		} else {
			response.Reason = workspaceChangeReasonAuthorityChanged
		}
		return nil
	})
	return response, missing, err
}

// attachPageChanges puts on the answer the pages changed since a change
// number, read in the answer's own moment with what the person may see then,
// when they say what changed and some page did or the list's token is behind
// (a page's body changed alone: none of its pages did, and the list stays).
func attachPageChanges(app core.App, principalID, workspaceID string, since int64, behind bool, response *workspaceChangeProbeResponse) error {
	changes, ok, err := bootstrapPageChanges(app, principalID, workspaceID, since)
	if err != nil || !ok || (!behind && len(changes.Pages) == 0 && len(changes.RemovedPageIDs) == 0) {
		return err
	}
	if changes.SnapshotRevision, err = readBootstrapSnapshotRevision(app); err != nil {
		return err
	}
	if changes.AuthorityRevision, err = bootstrapReaderAuthority(app, principalID); err != nil {
		return err
	}
	changes.DataEpoch = currentDataEpoch(app)
	response.Changes = changes
	return nil
}

func readWorkspaceChangeSnapshots(
	app core.App,
	principalID string,
	workspaceIDs []string,
) (map[string]workspaceChangeSnapshot, error) {
	if principalID == "" || len(workspaceIDs) > maxBootstrapWorkspaces {
		return nil, permissionDenied()
	}
	if len(workspaceIDs) == 0 {
		return map[string]workspaceChangeSnapshot{}, nil
	}
	params := dbx.Params{"principalId": principalID}
	placeholders := make([]string, 0, len(workspaceIDs))
	seen := make(map[string]struct{}, len(workspaceIDs))
	for _, workspaceID := range workspaceIDs {
		if workspaceID == "" {
			return nil, permissionDenied()
		}
		if _, duplicate := seen[workspaceID]; duplicate {
			return nil, errWorkspaceChangeBaselineAmbiguous
		}
		seen[workspaceID] = struct{}{}
		key := fmt.Sprintf("workspace%d", len(placeholders))
		params[key] = workspaceID
		placeholders = append(placeholders, "{:"+key+"}")
	}
	if err := authorizeWorkspaceSet(app, principalID, workspaceIDs, permissionWorkspaceRead); err != nil {
		return nil, err
	}
	rows, err := loadWorkspaceChangeSnapshotRows(app, params, placeholders)
	if err != nil {
		return nil, err
	}
	if len(rows) != len(workspaceIDs) {
		return nil, errWorkspaceChangeBaselineAmbiguous
	}
	result := make(map[string]workspaceChangeSnapshot, len(rows))
	for index := range rows {
		rows[index].PrincipalID = principalID
		if !validWorkspaceChangeSnapshot(rows[index]) {
			return nil, errWorkspaceChangeBaselineAmbiguous
		}
		if _, duplicate := result[rows[index].WorkspaceID]; duplicate {
			return nil, errWorkspaceChangeBaselineAmbiguous
		}
		result[rows[index].WorkspaceID] = rows[index]
	}
	return result, nil
}

// readGuestWorkspaceChangeSnapshot reads the change state of a workspace a
// person is a guest in.
func readGuestWorkspaceChangeSnapshot(app core.App, principalID, workspaceID string, allowTrashed ...bool) (workspaceChangeSnapshot, error) {
	if principalID == "" || workspaceID == "" {
		return workspaceChangeSnapshot{}, permissionDenied()
	}
	rows, err := loadWorkspaceChangeSnapshotRows(
		app, dbx.Params{"principalId": principalID, "workspace0": workspaceID}, []string{"{:workspace0}"},
	)
	if err != nil {
		return workspaceChangeSnapshot{}, err
	}
	if len(rows) != 1 {
		return workspaceChangeSnapshot{}, permissionDenied()
	}
	snapshot := rows[0]
	snapshot.PrincipalID = principalID
	if snapshot.OwnerID == principalID || snapshot.WorkspaceRole != "" ||
		snapshot.OrganizationStatus != organizationMemberStatusActive {
		return workspaceChangeSnapshot{}, permissionDenied()
	}
	// Like the page list, only a page shared with them makes them a guest; a
	// member who was removed and has none no longer sees the workspace.
	if len(allowTrashed) > 0 && allowTrashed[0] {
		// The change probe also serves readers that can open recoverable
		// trash. Bootstrap keeps its live-page admission rule.
		if _, err := authorizeCommentNotificationScope(app, principalID, workspaceID, true); err != nil {
			return workspaceChangeSnapshot{}, err
		}
	} else if shared, err := hasBootstrapGuestShare(app, principalID, workspaceID); err != nil {
		return workspaceChangeSnapshot{}, err
	} else if !shared {
		return workspaceChangeSnapshot{}, permissionDenied()
	}
	snapshot.WorkspaceRole = workspaceRoleGuest
	if !validWorkspaceChangeSnapshot(snapshot) {
		return workspaceChangeSnapshot{}, errWorkspaceChangeBaselineAmbiguous
	}
	return snapshot, nil
}

// loadWorkspaceChangeSnapshotRows reads the change state of the workspaces
// the placeholders name, for the person the params name.
func loadWorkspaceChangeSnapshotRows(
	app core.App, params dbx.Params, placeholders []string,
) ([]workspaceChangeSnapshot, error) {
	rows := []workspaceChangeSnapshot{}
	user, err := app.FindRecordById("users", params["principalId"].(string))
	if err != nil {
		return nil, err
	}
	addresses, err := verifiedAccountEmails(app, user.Id)
	if err != nil {
		return nil, err
	}
	emailRevision := hashAuthMFAValue(user.Email() + "\x00" + strings.Join(addresses, "\x00"))
	setRevision, err := workspaceSetRevision(app, user.Id)
	if err != nil {
		return nil, err
	}
	err = app.DB().NewQuery(`
		SELECT [[workspace]].[[id]] AS [[workspace_id]],
			[[workspace]].[[organization]] AS [[organization_id]],
			[[workspace]].[[workspace_owner]] AS [[owner_id]],
			CASE WHEN [[workspace]].[[workspace_owner]] = {:principalId} THEN 'owner'
				ELSE COALESCE([[workspace_membership]].[[role]], '') END AS [[workspace_role]],
			[[organization_membership]].[[role]] AS [[organization_role]],
			[[organization_membership]].[[status]] AS [[organization_status]],
			[[content]].[[revision]] AS [[content_revision]],
			COALESCE([[permission]].[[cursor]], 0) AS [[permission_cursor]],
			[[policy]].[[version]] AS [[organization_policy_version]],
			[[instance]].[[revision]] AS [[instance_revision]],
			COALESCE((SELECT [[revision]] FROM {{` + accessRevisionTable + `}} WHERE [[singleton]] = 'access'), 0) AS [[access_revision]]
		FROM {{leafen_workspaces}} [[workspace]]
		JOIN {{leafen_organization_memberships}} [[organization_membership]]
			ON [[organization_membership]].[[organization]] = [[workspace]].[[organization]]
			AND [[organization_membership]].[[user]] = {:principalId}
		LEFT JOIN {{leafen_workspace_memberships}} [[workspace_membership]]
			ON [[workspace_membership]].[[workspace_id]] = [[workspace]].[[id]]
			AND [[workspace_membership]].[[user_id]] = {:principalId}
		JOIN {{leafen_workspace_content_revisions}} [[content]]
			ON [[content]].[[workspace_id]] = [[workspace]].[[id]]
		LEFT JOIN {{leafen_workspace_permission_state}} [[permission]]
			ON [[permission]].[[workspace_id]] = [[workspace]].[[id]]
		JOIN {{leafen_organization_policy_state}} [[policy]]
			ON [[policy]].[[organization_id]] = [[workspace]].[[organization]]
		JOIN {{leafen_server_settings}} [[instance]] ON [[instance]].[[id]] = 'default'
		WHERE [[workspace]].[[id]] IN (` + joinSQL(placeholders) + `)
		ORDER BY [[workspace]].[[id]]
	`).Bind(params).All(&rows)
	for index := range rows {
		rows[index].AccountEmailRevision = emailRevision
		rows[index].WorkspaceSetRevision = setRevision
	}
	return rows, err
}

// workspaceSetRevision is a digest of the workspaces a person owns or is a
// member of and the organizations they are active in (a guest's workspaces).
func workspaceSetRevision(app core.App, principalID string) (string, error) {
	rows := []struct {
		Kind string `db:"kind"`
		ID   string `db:"id"`
	}{}
	err := app.DB().NewQuery(`
		SELECT 'owner' AS [[kind]],[[id]] FROM {{leafen_workspaces}}
			WHERE [[workspace_owner]]={:principalId} AND COALESCE([[deletion_job_id]],'')=''
		UNION ALL
		SELECT 'member' AS [[kind]],[[workspace_id]] AS [[id]] FROM {{leafen_workspace_memberships}}
			WHERE [[user_id]]={:principalId}
		UNION ALL
		SELECT 'organization' AS [[kind]],[[organization]] AS [[id]] FROM {{leafen_organization_memberships}}
			WHERE [[user]]={:principalId} AND [[status]]='active'
		ORDER BY 1,2
	`).Bind(dbx.Params{"principalId": principalID}).All(&rows)
	if err != nil {
		return "", err
	}
	parts := make([]string, 0, len(rows))
	for _, row := range rows {
		parts = append(parts, row.Kind+":"+row.ID)
	}
	return hashAuthMFAValue(strings.Join(parts, "\x00")), nil
}

func validWorkspaceChangeSnapshot(snapshot workspaceChangeSnapshot) bool {
	if snapshot.WorkspaceID == "" || snapshot.PrincipalID == "" || snapshot.OrganizationID == "" ||
		snapshot.OwnerID == "" || snapshot.OrganizationStatus != organizationMemberStatusActive ||
		!validOrganizationRole(snapshot.OrganizationRole) || snapshot.ContentRevision < 0 ||
		snapshot.PermissionCursor < 0 || snapshot.OrganizationPolicyVersion < 0 || snapshot.InstanceRevision < 1 {
		return false
	}
	if snapshot.OwnerID == snapshot.PrincipalID {
		return snapshot.WorkspaceRole == workspaceRoleOwner
	}
	return snapshot.WorkspaceRole == workspaceRoleMember || snapshot.WorkspaceRole == workspaceRoleGuest
}

func workspaceChangeProbeAPIError(
	event *core.RequestEvent,
	status int,
	code string,
	message string,
	stage string,
	cause error,
) error {
	reference := newDiagnosticReference()
	return writeAPIErrorWithReference(
		event, status, code, message, reference, "workspace_change.probe", stage,
		code, cause, diagnosticSourcePosition(1),
	)
}
