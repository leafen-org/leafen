package organization

import (
	"errors"
	"net/http"
	"strings"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// Notion's "제한된 멤버" (its help "Who's who in a workspace", read
// 2026-10-12): a workspace owner turns a member into one from the role menu
// of Settings → People, and back. The core keeps who they are and keeps them
// out of what the rest of the workspace sees; the change takes them out of
// the workspace's open pages at once.

const (
	workspaceRestrictionPath         = "/workspaces/{workspaceId}/members/{userId}/restriction"
	workspaceRestrictionMaxBodyBytes = 4 << 10
	maxWorkspaceRestrictionIDLength  = 64
)

type workspaceRestrictionInput struct {
	Restricted *bool `json:"restricted"`
}

type workspaceRestrictionResponse struct {
	UserID     string `json:"userId"`
	Restricted bool   `json:"restricted"`
	Changed    bool   `json:"changed"`
}

func registerWorkspaceRestrictionRoutes(event *core.ServeEvent, rooms leafen.WorkspaceRoomInvalidator) {
	event.Router.PUT(leafen.APIRoot+workspaceRestrictionPath, func(request *core.RequestEvent) error {
		return updateWorkspaceRestriction(request, rooms)
	}).Bind(apis.BodyLimit(workspaceRestrictionMaxBodyBytes))
}

func updateWorkspaceRestriction(event *core.RequestEvent, rooms leafen.WorkspaceRoomInvalidator) error {
	user, ok := leafen.AuthenticatedUser(event)
	if !ok {
		return leafen.APIError(event, http.StatusUnauthorized, leafen.ErrorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	memberID := strings.TrimSpace(event.Request.PathValue("userId"))
	input := workspaceRestrictionInput{}
	if err := event.BindBody(&input); err != nil || input.Restricted == nil || workspaceID == "" || memberID == "" ||
		len(workspaceID) > maxWorkspaceRestrictionIDLength || len(memberID) > maxWorkspaceRestrictionIDLength {
		return leafen.APIError(event, http.StatusBadRequest, leafen.ErrorCodeInvalidInput, "Check the member details.")
	}
	response := workspaceRestrictionResponse{UserID: memberID, Restricted: *input.Restricted}
	change := func(txApp core.App) error {
		authorization, err := leafen.AuthorizeWorkspace(txApp, user.Id, workspaceID, leafen.PermissionWorkspaceOwner)
		if err != nil {
			return err
		}
		now := time.Now().UTC().UnixMilli()
		response.Changed, err = leafen.SetWorkspaceMemberRestricted(txApp, workspaceID, memberID, user.Id, *input.Restricted, now)
		if err != nil || !response.Changed {
			return err
		}
		action := "workspace.member.restricted"
		if !*input.Restricted {
			action = "workspace.member.unrestricted"
		}
		return leafen.RecordOrganizationAuditEvent(txApp, leafen.OrganizationAuditEvent{
			ID: security.RandomString(15), OrganizationID: authorization.Organization.OrganizationID,
			WorkspaceID: workspaceID, ActorID: user.Id, Action: action,
			TargetType: "user", TargetID: memberID, OccurredAt: now,
		})
	}
	// A key's call asks its person only for a change that would be made
	// (internal/app/user_key_acting_ask.go).
	err := leafen.RehearseBeforeAsking(event, change)
	if err == nil {
		if asked, askErr := leafen.AskBeforeActing(event); asked {
			return askErr
		}
		err = event.App.RunInTransaction(change)
	}
	if leafen.IsPermissionDenied(err) {
		return leafen.APIError(event, http.StatusNotFound, leafen.ErrorCodeWorkspaceNotFound, "Workspace not found.")
	}
	if errors.Is(err, leafen.ErrWorkspaceRestrictedMemberTarget) {
		return leafen.APIError(event, http.StatusNotFound, leafen.ErrorCodeWorkspaceMemberNotFound, "Workspace member not found.")
	}
	if err != nil {
		return leafen.APIErrorWithCause(event, http.StatusInternalServerError, leafen.ErrorCodeWorkspaceMemberMutationFailed,
			"Couldn't change the member's role.", "workspace_restriction", "transaction",
			leafen.ErrorCodeWorkspaceMemberMutationFailed, err)
	}
	// Whoever had the workspace's pages open reconnects under the new role.
	if response.Changed && rooms != nil {
		_ = rooms.InvalidateWorkspace(workspaceID)
	}
	return event.JSON(http.StatusOK, response)
}
