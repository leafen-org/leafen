package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// A person asks for their own membership. Until an owner admits them, the
// request grants no workspace access, private pages or roster visibility.
func validMembershipRequestPolicy(value string) bool {
	return value == "none" || value == "all" || value == "guests" || value == "non_guests"
}

func registerWorkspaceMembershipRequestRoutes(event *core.ServeEvent) {
	event.Router.Route(accessRequestMembershipReadMethod, apiRoot+accessRequestMembershipReadPath, func(event *core.RequestEvent) error {
		user, ok := authenticatedUser(event)
		if !ok {
			return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
		}
		status, err := readWorkspaceMembershipRequestStatus(event.App, user.Id, strings.TrimSpace(event.Request.PathValue("workspaceId")))
		if err != nil {
			return accessRequestError(event, err)
		}
		event.Response.Header().Set("Cache-Control", "no-store")
		return event.JSON(http.StatusOK, status)
	})
	event.Router.Route(accessRequestMembershipCreateMethod, apiRoot+accessRequestMembershipCreatePath, func(event *core.RequestEvent) error {
		user, ok := authenticatedUser(event)
		if !ok {
			return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
		}
		// The body may say why (accessRequestMembershipInput); an empty one asks without a message.
		input := accessRequestMembershipInput{}
		if event.Request.ContentLength != 0 {
			if err := event.BindBody(&input); err != nil {
				return accessRequestError(event, bodyRefusal(errAccessRequestInvalid, "membership request", err))
			}
		}
		message, ok := accessRequestMessage(input.Message)
		if !ok {
			return accessRequestError(event, refuse(errAccessRequestInvalid, "message is at most %d characters", accessRequestMessageRunes))
		}
		response, err := requestWorkspaceMembership(event.App, user.Id, strings.TrimSpace(event.Request.PathValue("workspaceId")), message)
		if err != nil {
			return accessRequestError(event, err)
		}
		return event.JSON(http.StatusCreated, response)
	}).Bind(apis.BodyLimit(accessRequestMaxBodyBytes))
	registerWorkspaceInviteLinkRoutes(event)
	registerPageGuestLinkRoutes(event)
	registerTeamspaceInviteLinkRoutes(event)
}

func canRequestWorkspaceMembership(app core.App, requesterID, workspaceID string) (bool, error) {
	if requesterID == "" || workspaceID == "" || len(workspaceID) > maxWorkspacePermissionIdentifierLength {
		return false, errAccessRequestNotFound
	}
	role, err := workspaceRoleOf(app, requesterID, workspaceID)
	if err != nil {
		return false, err
	}
	if role != "" {
		return false, nil
	}
	policy, err := workspaceSharingPolicy(app, workspaceID)
	if err != nil {
		return false, err
	}
	if policy.MembershipRequests == "none" {
		return false, nil
	}
	memberAdds, err := loadWorkspaceMemberAddPolicy(app)
	if err != nil {
		return false, err
	}
	if memberAdds != administratorMemberAddPolicyEnabled {
		return false, nil
	}
	guest, err := hasBootstrapGuestShare(app, requesterID, workspaceID)
	if err != nil {
		return false, err
	}
	return policy.MembershipRequests == "all" || policy.MembershipRequests == "guests" && guest || policy.MembershipRequests == "non_guests" && !guest, nil
}

// workspaceInviteLinkTakesRequests says whether someone outside the
// workspace may be let in by an invite link's request: the server lets
// workspaces add members and the workspace's link, or one of its
// teamspaces', is on. The link is its owner's permission, whatever the
// workspace says of asking unprompted (2026-10-20: a link's request could
// never be approved while the workspace took no requests of its own).
func workspaceInviteLinkTakesRequests(app core.App, workspaceID string) (bool, error) {
	memberAdds, err := loadWorkspaceMemberAddPolicy(app)
	if err != nil || memberAdds != administratorMemberAddPolicyEnabled {
		return false, err
	}
	open := 0
	err = app.DB().NewQuery(`SELECT
		EXISTS (SELECT 1 FROM {{leafen_workspace_invite_links}} WHERE [[workspace_id]] = {:workspace} AND [[enabled]] = 1)
		OR EXISTS (SELECT 1 FROM {{leafen_teamspace_invite_links}} [[link]]
			JOIN {{leafen_teamspaces}} [[teamspace]] ON [[teamspace]].[[id]] = [[link]].[[teamspace_id]]
			WHERE [[link]].[[workspace_id]] = {:workspace} AND [[link]].[[enabled]] = 1
				AND [[teamspace]].[[deleted_at]] = 0 AND [[teamspace]].[[archived_at]] = 0)`).
		Bind(dbx.Params{"workspace": workspaceID}).Row(&open)
	return open == 1, err
}

func readWorkspaceMembershipRequestStatus(app core.App, requesterID, workspaceID string) (workspaceMembershipRequestStatus, error) {
	status := workspaceMembershipRequestStatus{}
	err := runReadSnapshot(context.Background(), app, func(txApp core.App) error {
		var err error
		status.CanRequest, err = canRequestWorkspaceMembership(txApp, requesterID, workspaceID)
		if err != nil {
			return err
		}
		var count int
		err = txApp.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_access_requests}}
			WHERE [[workspace_id]]={:workspace} AND [[requester_id]]={:requester} AND [[kind]]='membership' AND [[status]]='pending'`).
			Bind(dbx.Params{"workspace": workspaceID, "requester": requesterID}).Row(&count)
		status.Pending = count > 0
		return err
	})
	return status, err
}

// accessRequestMessage is a request's message as kept: trimmed, at most
// accessRequestMessageRunes long, and valid text.
func accessRequestMessage(raw string) (string, bool) {
	message := strings.TrimSpace(raw)
	return message, utf8.ValidString(message) && utf8.RuneCountInString(message) <= accessRequestMessageRunes
}

func requestWorkspaceMembership(app core.App, requesterID, workspaceID, message string) (accessRequestResponse, error) {
	response := accessRequestResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		allowed, err := canRequestWorkspaceMembership(txApp, requesterID, workspaceID)
		if err != nil {
			return err
		}
		if !allowed {
			return errAccessRequestNotAllowed
		}
		stored, _, err := upsertAccessRequest(txApp, storedAccessRequest{
			WorkspaceID: workspaceID, Kind: accessRequestKindMembership, RequesterID: requesterID,
			PrincipalType: publicMutationPrincipalUser, PrincipalID: requesterID, Role: accessRequestKindMember,
			Message: message,
		})
		if err != nil {
			return err
		}
		response, err = accessRequestResponseFor(txApp, stored, requesterID)
		return err
	})
	return response, err
}

// A non-member reads only their own pending requests, never a workspace's
// list or its members. This also works after the last guest share is removed.
func ownPendingAccessRequests(app core.App, principalID, workspaceID string, after timeIDCursor) (accessRequestListResponse, error) {
	if _, err := app.FindRecordById(workspacesCollection, workspaceID); errors.Is(err, sql.ErrNoRows) {
		return accessRequestListResponse{}, errAccessRequestNotFound
	} else if err != nil {
		return accessRequestListResponse{}, err
	}
	rows := []storedAccessRequest{}
	err := app.DB().NewQuery(`SELECT ` + accessRequestColumns + ` FROM {{leafen_access_requests}} [[request]]
		WHERE [[workspace_id]]={:workspace} AND [[requester_id]]={:principal} AND [[status]]='pending' AND ` + accessRequestListedSQL + `
			AND ({:afterId}='' OR [[created_at]]<{:afterAt} OR ([[created_at]]={:afterAt} AND [[id]]<{:afterId}))
		ORDER BY [[created_at]] DESC, [[id]] DESC LIMIT {:limit}`).
		Bind(dbx.Params{"workspace": workspaceID, "principal": principalID, "limit": accessRequestListWindow + 1,
			"afterAt": after.At, "afterId": after.ID}).All(&rows)
	response := accessRequestListResponse{Requests: []accessRequestResponse{}}
	if err != nil {
		return response, err
	}
	if len(rows) > accessRequestListWindow {
		rows = rows[:accessRequestListWindow]
		response.NextCursor = encodeTimeIDCursor(rows[len(rows)-1].CreatedAt, rows[len(rows)-1].ID)
	}
	for _, row := range rows {
		item, err := accessRequestResponseFor(app, row, principalID)
		if err != nil {
			return response, err
		}
		// A revoked share must not reveal a later private page title.
		item.PageTitle = ""
		response.Requests = append(response.Requests, item)
	}
	return response, nil
}

// requestMemberAdd asks the owner to add someone, with why when the member
// says, as Notion's "멤버 초대 요청" asks it ("요청 이유", seen 2026-11-21 in
// two tabs of the business trial's member); the owner reads it with the
// request. The reason is optional.
func requestMemberAdd(app core.App, requesterID, workspaceID, rawEmail string, reason ...string) (accessRequestResponse, error) {
	rawMessage := ""
	if len(reason) > 0 {
		rawMessage = reason[0]
	}
	response := accessRequestResponse{}
	err := app.RunInTransaction(func(txApp core.App) error {
		var err error
		response, err = requestMemberAddInTransaction(txApp, requesterID, workspaceID, rawEmail, rawMessage)
		return err
	})
	return response, err
}
func requestMemberAddInTransaction(app core.App, requesterID, workspaceID, rawEmail, rawMessage string) (accessRequestResponse, error) {
	email, ok := normalizeWorkspaceMemberEmail(rawEmail)
	if requesterID == "" || workspaceID == "" {
		return accessRequestResponse{}, refuse(errAccessRequestInvalid, "the workspace ID in the path is empty")
	}
	if !ok || len(email) > accessRequestEmailRunes {
		return accessRequestResponse{}, refuse(errAccessRequestInvalid, "email is one email address, at most %d characters", accessRequestEmailRunes)
	}
	message, ok := accessRequestMessage(rawMessage)
	if !ok {
		return accessRequestResponse{}, refuse(errAccessRequestInvalid, "message is text of at most %d characters", accessRequestMessageRunes)
	}
	role, err := workspaceRoleOf(app, requesterID, workspaceID)
	if err != nil {
		return accessRequestResponse{}, err
	}
	switch role {
	case "":
		return accessRequestResponse{}, errAccessRequestNotFound
	case workspaceRoleOwner:
		// An owner adds members directly.
		return accessRequestResponse{}, refuse(errAccessRequestInvalid, "you own the workspace: add the member yourself instead of asking")
	}
	policy, err := workspaceSharingPolicy(app, workspaceID)
	if err != nil {
		return accessRequestResponse{}, err
	}
	if policy.MemberAdds != sharingSecurityMemberAddsApproval {
		return accessRequestResponse{}, errAccessRequestNotAllowed
	}
	if existing, err := findWorkspaceMemberByEmail(app, email); err == nil {
		if memberRole, err := workspaceRoleOf(app, existing.Id, workspaceID); err != nil {
			return accessRequestResponse{}, err
		} else if memberRole != "" {
			return accessRequestResponse{}, refuse(errAccessRequestInvalid, "email is a member of the workspace already")
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return accessRequestResponse{}, err
	}
	stored, _, err := upsertAccessRequest(app, storedAccessRequest{
		WorkspaceID: workspaceID, Kind: accessRequestKindMember, RequesterID: requesterID,
		PrincipalType: publicMutationPrincipalEmail, PrincipalID: email, Role: accessRequestKindMember, Message: message,
	})
	if err != nil {
		return accessRequestResponse{}, err
	}
	return accessRequestResponseFor(app, stored, requesterID)
}
