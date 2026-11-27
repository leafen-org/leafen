package app

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// A workspace's invite link, as Notion's 설정 → 사람 "링크로 멤버 추가":
// off until an owner turns it on, and anyone signed in who opens it joins as
// a member at once, as Notion's does (2026-11-03 audit; it was a request an
// owner admitted), admitted by the owner who turned it on. Turning it off,
// or making a new link, ends the old one, and turning it on again makes a new
// one. The link's key rides after "#", so no server or proxy log keeps it.
// Adding members turned off for the server closes the link too.

var errWorkspaceInviteLinkInvalid = errors.New(errorCodeWorkspaceInviteLinkInvalid)

func registerWorkspaceInviteLinkRoutes(event *core.ServeEvent) {
	event.Router.Route(workspaceInviteLinkReadMethod, apiRoot+workspaceInviteLinkReadPath, func(event *core.RequestEvent) error {
		return serveWorkspaceInviteLink(event, func(app core.App, workspaceID, ownerID string) (workspaceInviteLink, error) {
			return readWorkspaceInviteLink(app, workspaceID)
		})
	})
	event.Router.Route(workspaceInviteLinkUpdateMethod, apiRoot+workspaceInviteLinkUpdatePath, func(event *core.RequestEvent) error {
		input := workspaceInviteLinkInput{}
		if err := event.BindBody(&input); err != nil {
			return accessRequestError(event, bodyRefusal(errAccessRequestInvalid, "invite link change", err))
		}
		return serveWorkspaceInviteLink(event, func(app core.App, workspaceID, ownerID string) (workspaceInviteLink, error) {
			return setWorkspaceInviteLink(app, workspaceID, ownerID, input.Enabled, false)
		})
	}).Bind(apis.BodyLimit(accessRequestMaxBodyBytes))
	event.Router.Route(workspaceInviteLinkRotateMethod, apiRoot+workspaceInviteLinkRotatePath, func(event *core.RequestEvent) error {
		return serveWorkspaceInviteLink(event, func(app core.App, workspaceID, ownerID string) (workspaceInviteLink, error) {
			return setWorkspaceInviteLink(app, workspaceID, ownerID, true, true)
		})
	}).Bind(apis.BodyLimit(accessRequestMaxBodyBytes))
	event.Router.Route(workspaceInviteReadMethod, apiRoot+workspaceInviteReadPath, func(event *core.RequestEvent) error {
		return serveWorkspaceInvite(event, false)
	}).Bind(apis.BodyLimit(accessRequestMaxBodyBytes))
	event.Router.Route(workspaceInviteJoinMethod, apiRoot+workspaceInviteJoinPath, func(event *core.RequestEvent) error {
		return serveWorkspaceInvite(event, true)
	}).Bind(apis.BodyLimit(accessRequestMaxBodyBytes))
}

// serveWorkspaceInviteLink runs an owner's read or change of the link.
func serveWorkspaceInviteLink(
	event *core.RequestEvent,
	run func(app core.App, workspaceID, ownerID string) (workspaceInviteLink, error),
) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	role, err := workspaceRoleOf(event.App, user.Id, workspaceID)
	if err != nil {
		return accessRequestError(event, err)
	}
	if role != "owner" {
		return accessRequestError(event, errAccessRequestNotFound)
	}
	var link workspaceInviteLink
	err = event.App.RunInTransaction(func(txApp core.App) error {
		var err error
		link, err = run(txApp, workspaceID, user.Id)
		return err
	})
	if err != nil {
		return accessRequestError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, link)
}

type storedWorkspaceInviteLink struct {
	WorkspaceID string `db:"workspace_id"`
	Token       string `db:"token"`
	Enabled     bool   `db:"enabled"`
}

func loadWorkspaceInviteLink(app core.App, workspaceID string) (storedWorkspaceInviteLink, bool, error) {
	stored := storedWorkspaceInviteLink{}
	err := app.DB().NewQuery(`SELECT [[workspace_id]],[[token]],[[enabled]] FROM {{leafen_workspace_invite_links}}
		WHERE [[workspace_id]]={:workspace}`).Bind(dbx.Params{"workspace": workspaceID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return stored, false, nil
	}
	return stored, err == nil, err
}

// readWorkspaceInviteLink is the link as its owners see it: its key only
// while it is on.
func readWorkspaceInviteLink(app core.App, workspaceID string) (workspaceInviteLink, error) {
	stored, found, err := loadWorkspaceInviteLink(app, workspaceID)
	if err != nil || !found || !stored.Enabled {
		return workspaceInviteLink{}, err
	}
	return workspaceInviteLink{Enabled: true, Token: stored.Token}, nil
}

func newWorkspaceInviteToken() (string, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(random), nil
}

// setWorkspaceInviteLink turns the link on or off; turning it on from off,
// or rotate, makes a new key, which ends the old one.
func setWorkspaceInviteLink(app core.App, workspaceID, ownerID string, enabled, rotate bool) (workspaceInviteLink, error) {
	stored, found, err := loadWorkspaceInviteLink(app, workspaceID)
	if err != nil {
		return workspaceInviteLink{}, err
	}
	token := stored.Token
	if !found || rotate || (enabled && !stored.Enabled) {
		if token, err = newWorkspaceInviteToken(); err != nil {
			return workspaceInviteLink{}, err
		}
	}
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_workspace_invite_links}} ([[workspace_id]],[[token]],[[enabled]],[[updated_by]],[[updated_at]])
		VALUES ({:workspace},{:token},{:enabled},{:owner},{:now})
		ON CONFLICT ([[workspace_id]]) DO UPDATE SET
			[[token]]=excluded.[[token]], [[enabled]]=excluded.[[enabled]],
			[[updated_by]]=excluded.[[updated_by]], [[updated_at]]=excluded.[[updated_at]]
	`).Bind(dbx.Params{
		"workspace": workspaceID, "token": token, "enabled": enabled, "owner": ownerID, "now": time.Now().UTC().UnixMilli(),
	}).Execute()
	if err != nil {
		return workspaceInviteLink{}, err
	}
	return readWorkspaceInviteLink(app, workspaceID)
}

// serveWorkspaceInvite reads the workspace a link invites to, or asks to
// join it. A key no open link has reads as not found.
func serveWorkspaceInvite(event *core.RequestEvent, join bool) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := workspaceInviteTokenInput{}
	if err := event.BindBody(&input); err != nil {
		return accessRequestError(event, bodyRefusal(errAccessRequestInvalid, "invite link's key", err))
	}
	token := strings.TrimSpace(input.Token)
	if token == "" || utf8.RuneCountInString(token) > workspaceInviteTokenRunes {
		return accessRequestError(event, refuse(errAccessRequestInvalid, "token is the invite link's key, 1–%d characters", workspaceInviteTokenRunes))
	}
	var response workspaceInviteResponse
	var err error
	if strings.HasPrefix(token, teamspaceInviteTokenPrefix) {
		// A teamspace's link joins through the teamspace's own change, which
		// keeps its own transaction (teamspace_invite_links.go).
		response, err = teamspaceInvite(event.Request.Context(), event.App, user.Id, token, join)
	} else {
		err = event.App.RunInTransaction(func(txApp core.App) error {
			var err error
			response, err = workspaceInvite(txApp, user.Id, token, join)
			return err
		})
	}
	if errors.Is(err, errWorkspaceInviteLinkInvalid) {
		// Why, for whoever holds its key (invitation_ended.go).
		apiErrorBecause(event, "", invitationEndedCause(inviteLinkEndedFor(event.App, token)))
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceInviteLinkInvalid, "This invite link is off or has been replaced.")
	}
	if err != nil {
		return accessRequestError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

func workspaceInvite(app core.App, requesterID, token string, join bool) (workspaceInviteResponse, error) {
	stored := storedWorkspaceInviteLink{}
	err := app.DB().NewQuery(`SELECT [[workspace_id]],[[token]],[[enabled]] FROM {{leafen_workspace_invite_links}}
		WHERE [[token]]={:token} AND [[enabled]]=1`).Bind(dbx.Params{"token": token}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return workspaceInviteResponse{}, errWorkspaceInviteLinkInvalid
	}
	if err != nil {
		return workspaceInviteResponse{}, err
	}
	workspace, err := app.FindRecordById(workspacesCollection, stored.WorkspaceID)
	if errors.Is(err, sql.ErrNoRows) || err == nil && workspace.GetString("deletion_job_id") != "" {
		return workspaceInviteResponse{}, errWorkspaceInviteLinkInvalid
	}
	if err != nil {
		return workspaceInviteResponse{}, err
	}
	response := workspaceInviteResponse{
		WorkspaceID: stored.WorkspaceID, Name: workspace.GetString("name"), Icon: workspace.GetString("icon"),
	}
	role, err := workspaceRoleOf(app, requesterID, stored.WorkspaceID)
	if err != nil {
		return response, err
	}
	if role != "" {
		response.Member = true
		return response, nil
	}
	return workspaceInviteMembership(app, requesterID, stored.WorkspaceID, join, response)
}

// workspaceInviteMembership joins someone outside the workspace to it,
// while the server lets workspaces add members; a request someone made
// through the link before still shows as waiting.
func workspaceInviteMembership(app core.App, requesterID, workspaceID string, join bool, response workspaceInviteResponse) (workspaceInviteResponse, error) {
	memberAdds, err := loadWorkspaceMemberAddPolicy(app)
	if err != nil {
		return workspaceInviteResponse{}, err
	}
	if memberAdds != administratorMemberAddPolicyEnabled {
		return workspaceInviteResponse{}, errWorkspaceInviteLinkInvalid
	}
	stored := storedWorkspaceInviteLink{WorkspaceID: workspaceID}
	if join {
		var turnedOnBy string
		if err := app.DB().NewQuery(`SELECT [[updated_by]] FROM {{leafen_workspace_invite_links}} WHERE [[workspace_id]]={:workspace}`).
			Bind(dbx.Params{"workspace": workspaceID}).Row(&turnedOnBy); err != nil {
			return response, err
		}
		_, err := admitWorkspaceMember(app, workspaceMemberAdmissionRequest{ActorID: turnedOnBy, WorkspaceID: workspaceID, UserID: requesterID})
		if isPermissionDenied(err) {
			// An owner no longer: the link lets no one in.
			return workspaceInviteResponse{}, errWorkspaceInviteLinkInvalid
		}
		if err != nil {
			return response, err
		}
		response.Member = true
		return response, nil
	}
	var pending int
	err = app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_access_requests}}
		WHERE [[workspace_id]]={:workspace} AND [[requester_id]]={:requester} AND [[kind]]='membership' AND [[status]]='pending'`).
		Bind(dbx.Params{"workspace": stored.WorkspaceID, "requester": requesterID}).Row(&pending)
	response.Pending = pending > 0
	return response, err
}
