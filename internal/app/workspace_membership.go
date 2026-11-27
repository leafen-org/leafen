package app

import (
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

type storedWorkspaceMembership struct {
	Role string `db:"role"`
}

func registerWorkspaceMembershipRoutes(event *core.ServeEvent, roomInvalidator workspaceRoomInvalidator) {
	event.Router.Route(workspaceMemberListMethod, apiRoot+workspaceMemberListPath, readWorkspaceMemberDirectory)
	event.Router.Route(workspaceGuestListMethod, apiRoot+workspaceGuestListPath, readWorkspaceGuestDirectory)
	event.Router.Route(workspaceMemberCandidateListMethod, apiRoot+workspaceMemberCandidateListPath, readWorkspaceMemberCandidates)
	event.Router.Route(workspaceGuestPagesListMethod, apiRoot+workspaceGuestPagesListPath, readWorkspaceGuestPages)
	event.Router.Route(workspaceGuestRemoveMethod, apiRoot+workspaceGuestRemovePath, removeWorkspaceGuests).
		Bind(apis.BodyLimit(workspaceGuestRemoveMaxBodyBytes))
	event.Router.GET(apiRoot+"/workspaces/{workspaceId}/people.csv", exportWorkspacePeople)
	event.Router.PUT(apiRoot+"/workspaces/{workspaceId}/members/{userId}", grantWorkspaceMembership)
	event.Router.POST(apiRoot+"/workspaces/{workspaceId}/members", addWorkspaceMemberByEmail)
	registerWorkspaceNameInvitationRoutes(event)
	event.Router.Route(workspaceMemberInvitationCancelMethod, apiRoot+workspaceMemberInvitationCancelPath,
		cancelWorkspaceMemberInvitation)
	event.Router.DELETE(apiRoot+"/workspaces/{workspaceId}/members/{userId}", func(event *core.RequestEvent) error {
		return revokeWorkspaceMembership(event, roomInvalidator)
	})
	registerWorkspaceOwnerTransferRoutes(event, roomInvalidator)
}

func grantWorkspaceMembership(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	memberID := strings.TrimSpace(event.Request.PathValue("userId"))
	var input workspaceMembershipInput
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the member details.")
	}
	input.Role = strings.TrimSpace(input.Role)
	if workspaceID == "" || memberID == "" || len(workspaceID) > maxWorkspacePermissionIdentifierLength || len(memberID) > maxWorkspacePermissionIdentifierLength || input.Role != workspaceRoleMember {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the member details.")
	}

	// Adding an account already on the server by its id is what the server's
	// "워크스페이스 멤버 추가" policy governs, as adding it by its address is.
	if policy, err := loadWorkspaceMemberAddPolicy(event.App); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeWorkspaceMemberMutationFailed, "Couldn't add the member.", err)
	} else if policy != administratorMemberAddPolicyEnabled {
		return apiError(event, http.StatusForbidden, errorCodeWorkspaceMemberAddDisabled, "Server policy blocked adding workspace members.")
	}
	result, err := admitWorkspaceMemberAsked(event, workspaceMemberAdmissionRequest{
		ActorID: user.Id, WorkspaceID: workspaceID, UserID: memberID, Restricted: input.Restricted,
	})
	if errors.Is(err, errWorkspaceRestrictedInvitationUnavailable) {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the member details.")
	}
	if errors.Is(err, errUserKeyAsked) {
		return nil
	}
	if isPermissionDenied(err) {
		return workspaceOwnersOnly(event, user.Id, workspaceID, "Only the workspace's owners add its members.", workspaceNotFound(event))
	}
	if errors.Is(err, errWorkspaceMemberTargetMissing) {
		return apiError(event, http.StatusNotFound, errorCodeUserNotFound, "No account has this user ID.")
	}
	if errors.Is(err, errWorkspaceMemberConflict) {
		return apiError(event, http.StatusConflict, errorCodeWorkspaceMemberConflict, "The workspace owner can't be changed to the member role.")
	}
	if errors.Is(err, errWorkspaceMemberAddDisabled) {
		return apiError(event, http.StatusForbidden, errorCodeWorkspaceMemberAddDisabled, "Server policy blocked adding workspace members.")
	}
	if errors.Is(err, errExternalWorkspacesDisabled) {
		return apiError(event, http.StatusForbidden, errorCodeExternalWorkspacesDisabled,
			"This person's organization doesn't let them join workspaces outside it.")
	}
	if err != nil {
		if storageError, ok := persistentWriteStorageAPIError(
			event, err, persistentWriteWorkspaceMemberAdd, "transaction",
		); ok {
			return storageError
		}
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeWorkspaceMemberMutationFailed, "Couldn't add the member.", err)
	}
	status := http.StatusCreated
	if result.Membership.Duplicate {
		status = http.StatusOK
	}
	return event.JSON(status, result.Membership)
}

func addWorkspaceMemberByEmail(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	input := workspaceMemberAddInput{}
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceMemberAddInvalid, "Check the member email.")
	}
	email, valid := normalizeWorkspaceMemberEmail(input.Email)
	input.Role = strings.TrimSpace(input.Role)
	input.Message = strings.TrimSpace(input.Message)
	if workspaceID == "" || len(workspaceID) > maxWorkspacePermissionIdentifierLength ||
		!valid || input.Role != workspaceRoleMember || utf8.RuneCountInString(input.Message) > maxWorkspaceInvitationMessageRunes {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceMemberAddInvalid, "Check the member email.")
	}
	result, err := admitWorkspaceMemberAsked(event, workspaceMemberAdmissionRequest{
		ActorID: user.Id, WorkspaceID: workspaceID, Email: email, ResolveByEmail: true,
		Message: input.Message, Restricted: input.Restricted,
	})
	if errors.Is(err, errWorkspaceRestrictedInvitationUnavailable) {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceMemberAddInvalid, "Check the member email.")
	}
	if errors.Is(err, errUserKeyAsked) {
		return nil
	}
	if isPermissionDenied(err) {
		return workspaceOwnersOnly(event, user.Id, workspaceID, "Only the workspace's owners add its members.", workspaceNotFound(event))
	}
	if errors.Is(err, errWorkspaceMemberAddDisabled) {
		return apiError(event, http.StatusForbidden, errorCodeWorkspaceMemberAddDisabled, "Server policy blocked adding workspace members.")
	}
	if errors.Is(err, errExternalWorkspacesDisabled) {
		return apiError(event, http.StatusForbidden, errorCodeExternalWorkspacesDisabled,
			"This person's organization doesn't let them join workspaces outside it.")
	}
	// Someone the organization deactivated (its identity provider took them
	// out) isn't added back here; the administrator reactivates them first.
	if errors.Is(err, errOrganizationMemberInactive) {
		return apiError(event, http.StatusConflict, errorCodeWorkspaceMemberDeactivated,
			"This person is deactivated in the organization. Reactivate them first.")
	}
	if errors.Is(err, errWorkspaceMemberInvitationsFull) {
		return apiError(event, http.StatusConflict, errorCodeWorkspaceMemberInvitationsFull,
			"This workspace has too many invitations waiting. Cancel some first.")
	}
	if err != nil {
		if storageError, ok := persistentWriteStorageAPIError(
			event, err, persistentWriteWorkspaceMemberAdd, "transaction",
		); ok {
			return storageError
		}
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeWorkspaceMemberAddFailed, "Couldn't process the member addition request.", err)
	}
	return event.JSON(http.StatusOK, result.Accepted)
}

// admitWorkspaceMemberAsked admits a member; a key's call asks its person
// only for an addition or invitation that would be made
// (user_key_acting_ask.go), and one asked comes back errUserKeyAsked, its
// answer given.
func admitWorkspaceMemberAsked(
	event *core.RequestEvent, request workspaceMemberAdmissionRequest,
) (workspaceMemberAdmissionResult, error) {
	request.Request = event.Request.Context()
	if err := rehearseBeforeAsking(event, func(txApp core.App) error {
		_, err := admitWorkspaceMember(txApp, request)
		return err
	}); err != nil {
		return workspaceMemberAdmissionResult{}, err
	}
	if asked, err := askBeforeActing(event); asked {
		return workspaceMemberAdmissionResult{}, errors.Join(errUserKeyAsked, err)
	}
	return admitWorkspaceMember(event.App, request)
}

func normalizeWorkspaceMemberEmail(raw string) (string, bool) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" || len(email) > maxWorkspaceMemberEmailLength {
		return "", false
	}
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email {
		return "", false
	}
	return email, true
}

func revokeWorkspaceMembership(event *core.RequestEvent, roomInvalidator workspaceRoomInvalidator) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	memberID := strings.TrimSpace(event.Request.PathValue("userId"))
	if workspaceID == "" || memberID == "" || len(workspaceID) > maxWorkspacePermissionIdentifierLength || len(memberID) > maxWorkspacePermissionIdentifierLength {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the member details.")
	}

	// A key's call asks its person only for a removal that would be made
	// (user_key_acting_ask.go).
	err := rehearseBeforeAsking(event, func(txApp core.App) error {
		admission, err := admitWorkspaceMembershipRevocation(txApp, user.Id, workspaceID, memberID)
		if err == nil && admission.Missing {
			err = errWorkspaceMemberTargetMissing
		}
		return err
	})
	admission := workspaceMembershipRevocationAdmission{}
	if err == nil {
		if asked, err := askBeforeActing(event); asked {
			return err
		}
		admission, err = admitWorkspaceMembershipRevocation(event.App, user.Id, workspaceID, memberID)
	}
	if isPermissionDenied(err) {
		return workspaceOwnersOnly(event, user.Id, workspaceID,
			"Only the workspace's owners remove its members; anyone may remove themselves to leave.", workspaceNotFound(event))
	}
	if admission.Missing || errors.Is(err, errWorkspaceMemberTargetMissing) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceMemberNotFound, "This person isn't a member of the workspace.")
	}
	if errors.Is(err, errWorkspaceMemberLeaveManaged) {
		return apiError(event, http.StatusConflict, errorCodeWorkspaceMemberLeaveManaged,
			"Your organization manages who is in this workspace. Ask an administrator to remove you.")
	}
	if err != nil {
		if storageError, ok := persistentWriteStorageAPIError(
			event, err, persistentWriteWorkspaceMemberRevoke, "admission",
		); ok {
			return storageError
		}
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeWorkspaceMemberMutationFailed, "Couldn't remove the member.", err)
	}
	runner := workspaceMembershipRevocationRunner(event.App, roomInvalidator)
	completed, err := runner.advanceWorkspaceMembershipRevocation(event.Request.Context(), admission.Job.ID)
	if err != nil {
		if storageError, ok := persistentWriteStorageAPIError(
			event, err, persistentWriteWorkspaceMemberRevoke, "room-invalidation-settlement",
		); ok {
			return storageError
		}
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeWorkspaceMemberMutationFailed, "Couldn't apply the member permission change.", err)
	}
	status := http.StatusAccepted
	if completed {
		status = http.StatusOK
	} else {
		runner.notify()
	}
	return event.JSON(status, admission.Response)
}

func invalidateWorkspaceMembershipRooms(
	roomInvalidator workspaceRoomInvalidator,
	workspaceID string,
) error {
	return roomInvalidator.InvalidateWorkspace(workspaceID)
}

func currentWorkspacePermissionCursor(app core.App, workspaceID string) (int, error) {
	var cursor int
	err := app.DB().NewQuery(`
		SELECT [[cursor]] FROM {{leafen_workspace_permission_state}}
		WHERE [[workspace_id]] = {:workspaceId}
	`).Bind(dbx.Params{"workspaceId": workspaceID}).Row(&cursor)
	return cursor, err
}

func initializeWorkspacePermissionCursor(app core.App, workspaceID string, now int64) error {
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_workspace_permission_state}} ([[workspace_id]], [[cursor]], [[updated]])
		VALUES ({:workspaceId}, 0, {:updated})
		ON CONFLICT ([[workspace_id]]) DO NOTHING
	`).Bind(dbx.Params{"workspaceId": workspaceID, "updated": now}).Execute()
	return err
}

// advanceWorkspacePermissionCursor moves the workspace's sharing cursor for
// a change to who is in it or what they may reach — a member joining,
// leaving or restricted, its ownership moved, its teamspaces' members —
// which People shows too, and its databases' views showing items by their
// people read again (people_change_graphs.go).
func advanceWorkspacePermissionCursor(app core.App, workspaceID string, now int64) (int, error) {
	notePeopleChangedAfterCommit(app, workspaceID)
	if err := announcePeopleChange(app, workspaceID); err != nil {
		return 0, err
	}
	var cursor int
	err := app.DB().NewQuery(`
		INSERT INTO {{leafen_workspace_permission_state}} ([[workspace_id]], [[cursor]], [[updated]])
		VALUES ({:workspaceId}, 1, {:updated})
		ON CONFLICT ([[workspace_id]]) DO UPDATE
		SET [[cursor]] = [[cursor]] + 1, [[updated]] = excluded.[[updated]]
		RETURNING [[cursor]]
	`).Bind(dbx.Params{"workspaceId": workspaceID, "updated": now}).Row(&cursor)
	return cursor, err
}
