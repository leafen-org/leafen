package organization

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	leafen "leafen.org/leafen/internal/app"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The Identity item's "사용자 관리", as Notion's managed users (its help, read
// 2026-10-02): the people whose email is on one of the organization's
// verified domains, each with the workspaces they belong to — marked when
// outside the organization — when they were last active and whether they
// use 2-step verification, to search by name, email or ID and filter by
// workspace and 2-step method, and to log out one by one or all at once.
// The people come by name a window at a time: the list's answer carries the
// first window and where the next starts, and the same route reads on from
// that cursor with the same search and filters, to the last person.
// One person's other actions are organization_managed_user_actions.go and
// organization_user_suspensions.go.

const (
	organizationManagedUsersPath       = "/organizations/{organizationId}/managed-users"
	organizationManagedUserPath        = "/organizations/{organizationId}/managed-users/{userId}"
	organizationManagedUserLogoutPath  = "/organizations/{organizationId}/managed-users/{userId}/logout"
	organizationManagedUsersLogoutPath = "/organizations/{organizationId}/managed-users/logout"
	organizationManagedUsersPerWindow  = 200
	organizationManagedUserTwoStepTOTP = "totp"
	organizationManagedUserTwoStepNone = "none"
)

type organizationManagedUserWorkspace struct {
	ID   string `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
	// The workspace is another organization's.
	External bool `json:"external" db:"external"`
	// The name as the filter's choices are ordered, for their cursor.
	SortName string `json:"-" db:"sort_name"`
}

type organizationManagedUser struct {
	ID         string                             `json:"id"`
	Name       string                             `json:"name"`
	Email      string                             `json:"email"`
	Workspaces []organizationManagedUserWorkspace `json:"workspaces"`
	// Milliseconds; 0 when the person has never signed in.
	LastActiveAt int64 `json:"lastActiveAt"`
	TwoStep      bool  `json:"twoStep"`
	// The organization suspended the person.
	Suspended bool `json:"suspended"`
	// Milliseconds; when their scheduled deletion happens, or 0.
	DeletionDueAt int64 `json:"deletionDueAt"`
}

// organizationManagedUsersWindow is a window of the people, by name.
type organizationManagedUsersWindow struct {
	Items   []organizationManagedUser `json:"items"`
	HasMore bool                      `json:"hasMore"`
	// Where the next window starts; "" once the last is read.
	NextCursor string `json:"nextCursor,omitempty"`
}

// organizationManagedUsersResponse is the first window, with the filter's
// choices; a window read on from a cursor is the people alone.
type organizationManagedUsersResponse struct {
	organizationManagedUsersWindow
	// The workspaces the people belong to, to filter by: the first window,
	// and where the next starts (organization_managed_user_workspaces.go).
	WorkspaceChoices           []organizationManagedUserWorkspace `json:"workspaceChoices"`
	WorkspaceChoicesNextCursor string                             `json:"workspaceChoicesNextCursor,omitempty"`
}

var errManagedUsersCursor = errors.New("the cursor isn't one this list gave")

// managedUsersCursor is where a window of the people or of their workspace
// filter ends: the last one's name as the list orders it (SQLite's lower())
// and its ID.
type managedUsersCursor struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

func encodeManagedUsersCursor(name, id string) string {
	encoded, _ := json.Marshal(managedUsersCursor{Name: name, ID: id})
	return base64.RawURLEncoding.EncodeToString(encoded)
}

// decodeManagedUsersCursor reads a cursor a window gave; none starts the list.
func decodeManagedUsersCursor(raw string) (managedUsersCursor, error) {
	after := managedUsersCursor{}
	if raw = strings.TrimSpace(raw); raw == "" {
		return after, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || json.Unmarshal(decoded, &after) != nil || after.ID == "" {
		return managedUsersCursor{}, errManagedUsersCursor
	}
	return after, nil
}

type storedOrganizationManagedUser struct {
	ID           string `db:"id"`
	Name         string `db:"name"`
	SortName     string `db:"sort_name"`
	Email        string `db:"email"`
	LastActiveAt int64  `db:"last_active_at"`
	TwoStep      int    `db:"two_step"`
	Suspended    int    `db:"suspended"`
	DeletionDue  int64  `db:"deletion_due_at"`
}

type organizationManagedUserFilter struct {
	Query       string
	WorkspaceID string
	TwoStep     string
	UserID      string
	// The window starts after this person, as the list orders them.
	After managedUsersCursor
}

// organizationManagedUserSQL is true of a person on one of the organization's
// verified domains.
var organizationManagedUserSQL = `EXISTS (SELECT 1 FROM {{leafen_organization_domains}} [[domain]]
		WHERE [[domain]].[[organization_id]] = {:organizationId} AND [[domain]].[[status]] = 'verified'
			AND [[domain]].[[domain]] IN (` + leafen.ManagedAccountDomainSQL("[[user]].[[id]]") + `))`

// organizationSuspendedUserSQL is true of a person the organization suspended,
// whom it may still see and unsuspend after their email leaves its domains.
const organizationSuspendedUserSQL = `EXISTS (SELECT 1 FROM {{leafen_organization_user_suspensions}} [[suspension]]
		WHERE [[suspension]].[[organization_id]] = {:organizationId} AND [[suspension]].[[user_id]] = [[user]].[[id]])`

const organizationTwoStepSQL = `EXISTS (SELECT 1 FROM {{leafen_auth_mfa_factors}} [[factor]]
		WHERE [[factor]].[[owner_id]] = [[user]].[[id]] AND [[factor]].[[verified_at]] > 0)`

const organizationManagedUserColumns = `[[user]].[[id]], [[user]].[[name]], [[user]].[[email]],
		COALESCE((SELECT MAX([[session]].[[updated_at]]) FROM {{leafen_auth_sessions}} [[session]]
			WHERE [[session]].[[owner_id]] = [[user]].[[id]]), 0) AS [[last_active_at]],
		` + organizationTwoStepSQL + ` AS [[two_step]],
		` + organizationSuspendedUserSQL + ` AS [[suspended]],
		COALESCE((SELECT [[scheduled]].[[deletion_due_at]] FROM {{leafen_organization_user_suspensions}} [[scheduled]]
			WHERE [[scheduled]].[[organization_id]] = {:organizationId} AND [[scheduled]].[[user_id]] = [[user]].[[id]]), 0)
			AS [[deletion_due_at]]`

// organizationManagedUsersSQL selects the people on the organization's
// verified domains whose name, email or ID holds the query.
var organizationManagedUsersSQL = `
	SELECT ` + organizationManagedUserColumns + `
	FROM {{users}} [[user]]
	WHERE ` + organizationManagedUserSQL + `
		AND ({:query} = '' OR instr(lower([[user]].[[name]]), {:query}) > 0
			OR EXISTS (SELECT 1 FROM {{leafen_account_emails}} [[address]] WHERE [[address]].[[user_id]] = [[user]].[[id]] AND instr([[address]].[[email]], {:query}) > 0) OR [[user]].[[id]] = {:id})`

func registerOrganizationManagedUserRoutes(event *core.ServeEvent, rooms leafen.WorkspaceRoomInvalidator) {
	registerManagedUserEmailRoutes(event)
	event.Router.GET(leafen.APIRoot+organizationManagedUsersPath, listOrganizationManagedUsers)
	event.Router.GET(leafen.APIRoot+organizationManagedUserWorkspacesPath, listOrganizationManagedUserWorkspaces)
	event.Router.PATCH(leafen.APIRoot+organizationManagedUserPath, changeOrganizationManagedUser)
	event.Router.PUT(leafen.APIRoot+organizationManagedUserPath+"/suspension", suspendOrganizationManagedUser)
	event.Router.DELETE(leafen.APIRoot+organizationManagedUserPath+"/suspension", unsuspendOrganizationManagedUser)
	event.Router.GET(leafen.APIRoot+organizationManagedUserPath+"/deletion", previewOrganizationManagedUserDeletion)
	event.Router.POST(leafen.APIRoot+organizationManagedUserPath+"/deletion", scheduleOrganizationManagedUserDeletion)
	event.Router.DELETE(leafen.APIRoot+organizationManagedUserPath+"/deletion", restoreOrganizationManagedUser)
	event.Router.DELETE(leafen.APIRoot+organizationManagedUserPath+"/external-workspaces", func(event *core.RequestEvent) error {
		return removeOrganizationManagedUserFromExternalWorkspaces(event, rooms)
	})
	event.Router.POST(leafen.APIRoot+organizationManagedUserLogoutPath, logOutOrganizationManagedUser)
	event.Router.POST(leafen.APIRoot+organizationManagedUsersLogoutPath, logOutOrganizationManagedUsers)
	event.Router.POST(leafen.APIRoot+organizationManagedUserPath+"/password-reset", resetOrganizationManagedUserPassword)
	event.Router.POST(leafen.APIRoot+organizationManagedUsersPath+"/password-reset", resetOrganizationManagedUserPasswords)
}

// authorizeManagedUsers holds the managed users to the organization's
// security admins.
func authorizeManagedUsers(event *core.RequestEvent) (leafen.OrganizationAuthorization, bool) {
	user, ok := leafen.AuthenticatedUser(event)
	if !ok {
		_ = leafen.APIError(event, http.StatusUnauthorized, leafen.ErrorCodeUnauthorized, "Login required.")
		return leafen.OrganizationAuthorization{}, false
	}
	organizationID := strings.TrimSpace(event.Request.PathValue("organizationId"))
	authorization, err := leafen.AuthorizeOrganization(event.App, user.Id, organizationID, leafen.PermissionOrganizationIdentityAdmin)
	if err != nil {
		_ = managedUsersError(event, err, "authorize")
		return leafen.OrganizationAuthorization{}, false
	}
	return authorization, true
}

func listOrganizationManagedUsers(event *core.RequestEvent) error {
	authorization, ok := authorizeManagedUsers(event)
	if !ok {
		return nil
	}
	values := event.Request.URL.Query()
	filter := organizationManagedUserFilter{
		Query:       strings.TrimSpace(values.Get("query")),
		WorkspaceID: strings.TrimSpace(values.Get("workspaceId")),
		TwoStep:     strings.TrimSpace(values.Get("twoStep")),
	}
	if filter.TwoStep != "" && filter.TwoStep != organizationManagedUserTwoStepTOTP &&
		filter.TwoStep != organizationManagedUserTwoStepNone {
		return leafen.APIError(event, http.StatusBadRequest, leafen.ErrorCodeInvalidInput, "Check the 2-step verification filter.")
	}
	after, err := decodeManagedUsersCursor(values.Get("cursor"))
	if err != nil {
		return leafen.APIError(event, http.StatusBadRequest, leafen.ErrorCodeInvalidInput, "Give the nextCursor of the last list, or none to start.")
	}
	filter.After = after
	users, next, err := queryOrganizationManagedUsers(event.App, authorization.OrganizationID, filter, organizationManagedUsersPerWindow)
	if err != nil {
		return managedUsersError(event, err, "list")
	}
	window := organizationManagedUsersWindow{Items: users, HasMore: next != "", NextCursor: next}
	if after.ID != "" {
		return event.JSON(http.StatusOK, window)
	}
	choices, err := loadOrganizationManagedUserWorkspaceChoices(event.App, authorization.OrganizationID, "", "")
	if err != nil {
		return managedUsersError(event, err, "list-workspace-choices")
	}
	return event.JSON(http.StatusOK, organizationManagedUsersResponse{
		organizationManagedUsersWindow: window, WorkspaceChoices: choices.Items, WorkspaceChoicesNextCursor: choices.NextCursor,
	})
}

// queryOrganizationManagedUsers is a window of the organization's managed
// users and those it suspended, by name from the filter's cursor on, with
// their workspaces, as far as the filter goes, and where the next window
// starts ("" at the last); a filter's user is the one an action was just
// authorized on, read as it is.
func queryOrganizationManagedUsers(
	app core.App, organizationID string, filter organizationManagedUserFilter, limit int,
) ([]organizationManagedUser, string, error) {
	query := strings.ToLower(filter.Query)
	rows := []storedOrganizationManagedUser{}
	if err := app.DB().NewQuery(`
		SELECT ` + organizationManagedUserColumns + `, lower([[user]].[[name]]) AS [[sort_name]]
		FROM {{users}} [[user]]
		WHERE (({:userId} = '' AND (` + organizationManagedUserSQL + ` OR ` + organizationSuspendedUserSQL + `))
				OR [[user]].[[id]] = {:userId})
			AND ({:query} = '' OR instr(lower([[user]].[[name]]), {:query}) > 0
				OR EXISTS (SELECT 1 FROM {{leafen_account_emails}} [[address]] WHERE [[address]].[[user_id]] = [[user]].[[id]] AND instr([[address]].[[email]], {:query}) > 0) OR [[user]].[[id]] = {:id})
			AND ({:workspaceId} = '' OR EXISTS (SELECT 1 FROM {{leafen_workspaces}} [[filtered]]
				WHERE [[filtered]].[[id]] = {:workspaceId} AND [[filtered]].[[deletion_job_id]] = ''
					AND ([[filtered]].[[workspace_owner]] = [[user]].[[id]]
						OR EXISTS (SELECT 1 FROM {{leafen_workspace_memberships}} [[member]]
							WHERE [[member]].[[workspace_id]] = [[filtered]].[[id]] AND [[member]].[[user_id]] = [[user]].[[id]]))))
			AND ({:twoStep} = '' OR ({:twoStep} = 'totp') = ` + organizationTwoStepSQL + `)
			AND ({:afterId} = '' OR lower([[user]].[[name]]) > {:afterName}
				OR (lower([[user]].[[name]]) = {:afterName} AND [[user]].[[id]] > {:afterId}))
		ORDER BY lower([[user]].[[name]]), [[user]].[[id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"organizationId": organizationID, "userId": filter.UserID, "query": query, "id": filter.Query,
		"workspaceId": filter.WorkspaceID, "twoStep": filter.TwoStep,
		"afterName": filter.After.Name, "afterId": filter.After.ID, "limit": limit + 1,
	}).All(&rows); err != nil {
		return nil, "", err
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = encodeManagedUsersCursor(rows[limit-1].SortName, rows[limit-1].ID)
	}
	users := make([]organizationManagedUser, 0, len(rows))
	for _, row := range rows {
		// Every workspace of theirs, not the first few: the profile's
		// removal from other organizations' workspaces counts on them.
		workspaces := []organizationManagedUserWorkspace{}
		if err := app.DB().NewQuery(`
			SELECT [[workspace]].[[id]], [[workspace]].[[name]],
				[[workspace]].[[organization]] <> {:organizationId} AS [[external]]
			FROM {{leafen_workspaces}} [[workspace]]
			WHERE [[workspace]].[[deletion_job_id]] = '' AND ([[workspace]].[[workspace_owner]] = {:userId}
				OR EXISTS (SELECT 1 FROM {{leafen_workspace_memberships}} [[membership]]
					WHERE [[membership]].[[workspace_id]] = [[workspace]].[[id]] AND [[membership]].[[user_id]] = {:userId}))
			ORDER BY lower([[workspace]].[[name]]), [[workspace]].[[id]]
		`).Bind(dbx.Params{"organizationId": organizationID, "userId": row.ID}).All(&workspaces); err != nil {
			return nil, "", err
		}
		users = append(users, organizationManagedUser{
			ID: row.ID, Name: row.Name, Email: row.Email, Workspaces: workspaces,
			LastActiveAt: row.LastActiveAt, TwoStep: row.TwoStep == 1, Suspended: row.Suspended == 1,
			DeletionDueAt: row.DeletionDue,
		})
	}
	return users, next, nil
}

func logOutOrganizationManagedUser(event *core.RequestEvent) error {
	authorization, ok := authorizeManagedUsers(event)
	if !ok {
		return nil
	}
	userID := strings.TrimSpace(event.Request.PathValue("userId"))
	var revoked int64
	change := func(txApp core.App) error {
		var managed int
		if err := txApp.DB().NewQuery(`SELECT COUNT(*) FROM (` + organizationManagedUsersSQL + `) WHERE [[id]] = {:userId}`).Bind(dbx.Params{
			"organizationId": authorization.OrganizationID, "query": "", "id": "", "userId": userID,
		}).Row(&managed); err != nil {
			return err
		}
		if managed != 1 {
			return errManagedUserNotFound
		}
		count, err := leafen.RevokeAllBrowserSessionsCount(txApp, userID)
		if err != nil {
			return err
		}
		revoked = count
		return recordManagedUsersLogout(txApp, authorization, userID, count)
	}
	// A key's call asks its person only for a sign-out that would be made
	// (internal/app/user_key_acting_ask.go).
	err := leafen.RehearseBeforeAsking(event, change)
	if err == nil {
		if asked, askErr := leafen.AskBeforeActing(event); asked {
			return askErr
		}
		err = event.App.RunInTransaction(change)
	}
	if err != nil {
		return managedUsersError(event, err, "logout")
	}
	return event.JSON(http.StatusOK, map[string]int64{"revoked": revoked})
}

// logOutOrganizationManagedUsers logs out every managed user but whoever
// asks, as Notion's "관리 대상 사용자 모두 로그아웃" leaves the admin signed in.
func logOutOrganizationManagedUsers(event *core.RequestEvent) error {
	authorization, ok := authorizeManagedUsers(event)
	if !ok {
		return nil
	}
	users, revoked := 0, int64(0)
	change := func(txApp core.App) error {
		ids := []string{}
		if err := txApp.DB().NewQuery(`SELECT [[id]] FROM (` + organizationManagedUsersSQL + `)`).Bind(dbx.Params{
			"organizationId": authorization.OrganizationID, "query": "", "id": "",
		}).Column(&ids); err != nil {
			return err
		}
		for _, id := range ids {
			if id == authorization.PrincipalID {
				continue
			}
			count, err := leafen.RevokeAllBrowserSessionsCount(txApp, id)
			if err != nil {
				return err
			}
			users++
			revoked += count
		}
		return recordManagedUsersLogout(txApp, authorization, authorization.OrganizationID, revoked)
	}
	// A key's call asks its person only for a sign-out that would be made
	// (internal/app/user_key_acting_ask.go).
	err := leafen.RehearseBeforeAsking(event, change)
	if err == nil {
		if asked, askErr := leafen.AskBeforeActing(event); asked {
			return askErr
		}
		users, revoked = 0, 0
		err = event.App.RunInTransaction(change)
	}
	if err != nil {
		return managedUsersError(event, err, "logout-all")
	}
	return event.JSON(http.StatusOK, map[string]any{"users": users, "revoked": revoked})
}

func recordManagedUsersLogout(app core.App, authorization leafen.OrganizationAuthorization, targetID string, revoked int64) error {
	now := time.Now().UTC().UnixMilli()
	targetType := "user"
	if targetID == authorization.OrganizationID {
		targetType = "organization"
	}
	return leafen.RecordOrganizationAuditEvent(app, leafen.OrganizationAuditEvent{
		ID: leafen.OrganizationAuditID(authorization.OrganizationID, "managed_user.logout",
			targetID+"@"+strconv.FormatInt(now, 10)),
		OrganizationID: authorization.OrganizationID, ActorID: authorization.PrincipalID,
		Action: "managed_user.logout", TargetType: targetType, TargetID: targetID,
		Metadata: map[string]any{"revokedSessions": revoked}, OccurredAt: now,
	})
}

func managedUsersError(event *core.RequestEvent, err error, stage string) error {
	status, code, message := http.StatusInternalServerError, leafen.ErrorCodeOrganizationAuthorityInvalid,
		"Couldn't complete the managed users request."
	switch {
	// Which isn't there is said apart: the person the path names, or the
	// organization (2026-11-26: "Organization or user not found." named the
	// organization as the target for a person no one managed, sending an
	// agent to look for the organization).
	case errors.Is(err, errManagedUserNotFound):
		return leafen.APIError(event, http.StatusNotFound, errorCodeManagedUserNotFound, "Check userId: no managed user of this "+
			"organization has that ID. GET /organizations/{organizationId}/managed-users lists them.")
	case leafen.IsPermissionDenied(err):
		status, code, message = http.StatusNotFound, leafen.ErrorCodeOrganizationNotFound, "Organization not found."
	case errors.Is(err, errManagedUserProtected):
		return leafen.APIError(event, http.StatusConflict, errorCodeManagedUserProtected,
			"The organization's owner, server administrators and you can't be changed here.")
	case errors.Is(err, errManagedUserEmailInvalid):
		return leafen.APIError(event, http.StatusBadRequest, errorCodeManagedUserEmailInvalid,
			"The new email must be on one of the organization's verified domains.")
	case errors.Is(err, errManagedUserEmailTaken):
		return leafen.APIError(event, http.StatusConflict, errorCodeManagedUserEmailTaken, "Another account uses this email.")
	case errors.Is(err, errManagedUserDeletionConfirm):
		return leafen.APIError(event, http.StatusBadRequest, errorCodeManagedUserDeletionConfirm, "Type the person's email to delete them.")
	case errors.Is(err, errManagedUserDeletionBlocked):
		return leafen.APIError(event, http.StatusConflict, errorCodeManagedUserDeletionBlocked,
			"This person owns an organization with other people's workspaces; a server administrator deletes the account.")
	case errors.Is(err, errManagedUserDeletionStarted):
		return leafen.APIError(event, http.StatusConflict, errorCodeManagedUserDeletionStarted, "The deletion has already started.")
	case errors.Is(err, errManagedUserDeletionScheduled):
		return leafen.APIError(event, http.StatusConflict, errorCodeManagedUserDeletionScheduled, "The person is to be deleted; restore them instead.")
	}
	return leafen.WriteAPIError(event, status, code, message, "organization_managed_users", stage, code, err, leafen.DiagnosticSourcePosition(1))
}
