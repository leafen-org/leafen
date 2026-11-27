package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// A teamspace's invite link, as Notion's teamspace settings › 보안 "초대
// 링크", which only those who may invite members see (on for a new
// teamspace, seen 2026-10-12), and its help: "멤버 추가" › "초대 링크
// 복사". A member of the workspace who opens it joins the teamspace, even an
// invite-only one; anyone else asks an owner to let them into the workspace
// first. Turning it off ends the link and turning it on again makes a
// new one. The key rides after "#" with a prefix that tells it from a
// workspace link's (workspace_invite_links.go).

const teamspaceInviteTokenPrefix = "t."

func registerTeamspaceInviteLinkRoutes(event *core.ServeEvent) {
	event.Router.Route(teamspaceInviteLinkReadMethod, apiRoot+teamspaceInviteLinkReadPath, func(event *core.RequestEvent) error {
		return serveTeamspaceInviteLink(event, false, func(app core.App, teamspace storedTeamspace, actorID string) (workspaceInviteLink, error) {
			return readTeamspaceInviteLink(app, teamspace, actorID)
		})
	})
	event.Router.Route(teamspaceInviteLinkUpdateMethod, apiRoot+teamspaceInviteLinkUpdatePath, func(event *core.RequestEvent) error {
		input := workspaceInviteLinkInput{}
		if err := event.BindBody(&input); err != nil {
			return accessRequestError(event, bodyRefusal(errAccessRequestInvalid, "invite link change", err))
		}
		return serveTeamspaceInviteLink(event, true, func(app core.App, teamspace storedTeamspace, actorID string) (workspaceInviteLink, error) {
			return setTeamspaceInviteLink(app, teamspace, actorID, input.Enabled)
		})
	}).Bind(apis.BodyLimit(accessRequestMaxBodyBytes))
}

// serveTeamspaceInviteLink reads the link for whoever may invite members,
// or changes it for whoever may change the teamspace's settings.
func serveTeamspaceInviteLink(
	event *core.RequestEvent, change bool,
	run func(app core.App, teamspace storedTeamspace, actorID string) (workspaceInviteLink, error),
) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	teamspaceID := strings.TrimSpace(event.Request.PathValue("teamspaceId"))
	var link workspaceInviteLink
	// One the person can't reach is a teamspace not found; one they reach
	// but can't invite to says so.
	unreached, uninvited := false, false
	err := event.App.RunInTransaction(func(txApp core.App) error {
		authority, teamspace, decision, err := authorizeTeamspace(txApp, user.Id, workspaceID, teamspaceID, change)
		if err != nil {
			unreached = true
			return refusedUnlessStorage(errAccessRequestNotFound, err)
		}
		// Those who manage it and, where it lets them invite, its members — a
		// workspace owner as either, as Notion's security tab (seen 2026-11-18).
		owner, err := managesTeamspace(txApp, authority, teamspace.ID, decision)
		if err != nil {
			return err
		}
		if teamspace.ArchivedAt != 0 || !owner && (decision.MembershipRole == "" || !teamspaceMembersInvite(teamspace)) {
			uninvited = true
			return errAccessRequestNotFound
		}
		link, err = run(txApp, teamspace, user.Id)
		return err
	})
	switch {
	case unreached:
		return apiError(event, http.StatusNotFound, errorCodeTeamspaceNotFound, "Teamspace not found.")
	case uninvited:
		return apiError(event, http.StatusForbidden, errorCodePermissionDenied,
			"Only the teamspace's owners make its invite link, and its members when it lets members invite; an archived teamspace has none.")
	case err != nil:
		return accessRequestError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, link)
}

type storedTeamspaceInviteLink struct {
	TeamspaceID string `db:"teamspace_id"`
	WorkspaceID string `db:"workspace_id"`
	Token       string `db:"token"`
	Enabled     bool   `db:"enabled"`
}

func loadTeamspaceInviteLink(app core.App, teamspaceID string) (storedTeamspaceInviteLink, bool, error) {
	stored := storedTeamspaceInviteLink{}
	err := app.DB().NewQuery(`SELECT [[teamspace_id]],[[workspace_id]],[[token]],[[enabled]] FROM {{leafen_teamspace_invite_links}}
		WHERE [[teamspace_id]]={:teamspace}`).Bind(dbx.Params{"teamspace": teamspaceID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return stored, false, nil
	}
	return stored, err == nil, err
}

// readTeamspaceInviteLink is the link as those who may invite see it. A
// teamspace starts with its link on, as Notion's, so the first read makes it.
func readTeamspaceInviteLink(app core.App, teamspace storedTeamspace, actorID string) (workspaceInviteLink, error) {
	stored, found, err := loadTeamspaceInviteLink(app, teamspace.ID)
	if err != nil {
		return workspaceInviteLink{}, err
	}
	if !found {
		return setTeamspaceInviteLink(app, teamspace, actorID, true)
	}
	if !stored.Enabled {
		return workspaceInviteLink{}, nil
	}
	return workspaceInviteLink{Enabled: true, Token: teamspaceInviteTokenPrefix + stored.Token}, nil
}

// setTeamspaceInviteLink turns the link on or off; turning it on from off
// makes a new key, which ends the old one.
func setTeamspaceInviteLink(app core.App, teamspace storedTeamspace, actorID string, enabled bool) (workspaceInviteLink, error) {
	stored, found, err := loadTeamspaceInviteLink(app, teamspace.ID)
	if err != nil {
		return workspaceInviteLink{}, err
	}
	token := stored.Token
	if !found || enabled && !stored.Enabled {
		if token, err = newWorkspaceInviteToken(); err != nil {
			return workspaceInviteLink{}, err
		}
	}
	if _, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_teamspace_invite_links}} ([[teamspace_id]],[[workspace_id]],[[token]],[[enabled]],[[updated_by]],[[updated_at]])
		VALUES ({:teamspace},{:workspace},{:token},{:enabled},{:actor},{:now})
		ON CONFLICT ([[teamspace_id]]) DO UPDATE SET
			[[token]]=excluded.[[token]], [[enabled]]=excluded.[[enabled]],
			[[updated_by]]=excluded.[[updated_by]], [[updated_at]]=excluded.[[updated_at]]
	`).Bind(dbx.Params{
		"teamspace": teamspace.ID, "workspace": teamspace.WorkspaceID, "token": token, "enabled": enabled,
		"actor": actorID, "now": time.Now().UTC().UnixMilli(),
	}).Execute(); err != nil {
		return workspaceInviteLink{}, err
	}
	if !enabled {
		return workspaceInviteLink{}, nil
	}
	return workspaceInviteLink{Enabled: true, Token: teamspaceInviteTokenPrefix + token}, nil
}

// teamspaceInviteAdmits is whether the key is the teamspace's open link.
func teamspaceInviteAdmits(app core.App, teamspaceID, token string) (bool, error) {
	key, found := strings.CutPrefix(token, teamspaceInviteTokenPrefix)
	if !found || key == "" {
		return false, nil
	}
	stored, found, err := loadTeamspaceInviteLink(app, teamspaceID)
	return err == nil && found && stored.Enabled && stored.Token == key, err
}

// teamspaceInvite reads the teamspace a link invites to, or joins it. A
// workspace member joins it through the teamspace's own join, which the key
// opens even where the teamspace is invite-only; anyone else asks to join
// the workspace as its link has them.
func teamspaceInvite(ctx context.Context, app core.App, requesterID, token string, join bool) (workspaceInviteResponse, error) {
	key := strings.TrimPrefix(token, teamspaceInviteTokenPrefix)
	link := storedTeamspaceInviteLink{}
	err := app.DB().NewQuery(`SELECT [[teamspace_id]],[[workspace_id]],[[token]],[[enabled]] FROM {{leafen_teamspace_invite_links}}
		WHERE [[token]]={:token} AND [[enabled]]=1`).Bind(dbx.Params{"token": key}).One(&link)
	if errors.Is(err, sql.ErrNoRows) {
		return workspaceInviteResponse{}, errWorkspaceInviteLinkInvalid
	}
	if err != nil {
		return workspaceInviteResponse{}, err
	}
	teamspace := struct {
		Name       string `db:"name"`
		Icon       string `db:"icon"`
		Revision   int    `db:"revision"`
		ArchivedAt int64  `db:"archived_at"`
	}{}
	err = app.DB().NewQuery(`SELECT [[name]],[[icon]],[[revision]],[[archived_at]] FROM {{leafen_teamspaces}}
		WHERE [[id]]={:id} AND [[workspace_id]]={:workspace} AND [[deleted_at]]=0`).
		Bind(dbx.Params{"id": link.TeamspaceID, "workspace": link.WorkspaceID}).One(&teamspace)
	if errors.Is(err, sql.ErrNoRows) || err == nil && teamspace.ArchivedAt != 0 {
		return workspaceInviteResponse{}, errWorkspaceInviteLinkInvalid
	}
	if err != nil {
		return workspaceInviteResponse{}, err
	}
	workspace, err := app.FindRecordById(workspacesCollection, link.WorkspaceID)
	if errors.Is(err, sql.ErrNoRows) || err == nil && workspace.GetString("deletion_job_id") != "" {
		return workspaceInviteResponse{}, errWorkspaceInviteLinkInvalid
	}
	if err != nil {
		return workspaceInviteResponse{}, err
	}
	response := workspaceInviteResponse{
		WorkspaceID: link.WorkspaceID, Name: workspace.GetString("name"), Icon: workspace.GetString("icon"),
		TeamspaceID: link.TeamspaceID, TeamspaceName: teamspace.Name, TeamspaceIcon: teamspace.Icon,
	}
	role, err := workspaceRoleOf(app, requesterID, link.WorkspaceID)
	if err != nil {
		return response, err
	}
	if role == workspaceRoleOwner || role == workspaceRoleMember {
		response.Member = true
		_, _, decision, err := authorizeTeamspace(app, requesterID, link.WorkspaceID, link.TeamspaceID, false)
		if err != nil && !errors.Is(err, errTeamspaceNotFound) {
			return response, err
		}
		response.TeamspaceMember = decision.Joined
		if !join || response.TeamspaceMember {
			return response, nil
		}
		// One join per teamspace revision: a retry repeats it, a later visit
		// after leaving joins again.
		_, _, err = executeTeamspaceMutation(ctx, app, nil, requesterID, link.WorkspaceID, teamspaceMutationInput{
			RequestID: "invite-link:" + link.TeamspaceID + ":" + strconv.Itoa(teamspace.Revision), Action: teamspaceActionJoin,
			TeamspaceID: link.TeamspaceID, ExpectedRevision: teamspace.Revision, InviteToken: token,
		})
		if errors.Is(err, errTeamspaceNotFound) || errors.Is(err, errTeamspaceInvalid) {
			return response, errWorkspaceInviteLinkInvalid
		}
		response.TeamspaceMember = err == nil
		return response, err
	}
	// A guest or someone outside asks to join the workspace first.
	err = app.RunInTransaction(func(txApp core.App) error {
		var err error
		response, err = teamspaceInviteMembershipRequest(txApp, requesterID, link.WorkspaceID, join, response)
		return err
	})
	return response, err
}

// teamspaceInviteMembershipRequest has someone outside the workspace ask an
// owner to let them in, as Notion's teamspace link shows them "이 워크스페이스의
// 멤버가 아닙니다 · 사용 권한 요청" (seen 2026-11-09). Unlike the workspace's own
// link, a teamspace's never makes them a member by itself, whether or not
// the workspace's link is on.
func teamspaceInviteMembershipRequest(app core.App, requesterID, workspaceID string, join bool, response workspaceInviteResponse) (workspaceInviteResponse, error) {
	memberAdds, err := loadWorkspaceMemberAddPolicy(app)
	if err != nil {
		return workspaceInviteResponse{}, err
	}
	if memberAdds != administratorMemberAddPolicyEnabled {
		return workspaceInviteResponse{}, errWorkspaceInviteLinkInvalid
	}
	if join {
		if _, _, err := upsertAccessRequest(app, storedAccessRequest{
			WorkspaceID: workspaceID, Kind: accessRequestKindMembership, RequesterID: requesterID,
			PrincipalType: publicMutationPrincipalUser, PrincipalID: requesterID, Role: accessRequestKindMember,
		}); err != nil {
			return response, err
		}
		response.Pending = true
		return response, nil
	}
	var pending int
	err = app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_access_requests}}
		WHERE [[workspace_id]]={:workspace} AND [[requester_id]]={:requester} AND [[kind]]='membership' AND [[status]]='pending'`).
		Bind(dbx.Params{"workspace": workspaceID, "requester": requesterID}).Row(&pending)
	response.Pending = pending > 0
	return response, err
}
