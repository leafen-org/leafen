package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// How each account came in by an invitation — which kind, to which
// workspace, sent by whom, and when — kept for the server's administrator,
// who sees it in Settings › Server › Users beside the account's devices and
// ends any of them at once (the multi-server decision's 2026-10-20 change:
// "관리자 화면에 누가 어느 초대로 언제 들어왔는지와 기기 목록을 보이고 바로
// 끊게 한다"). An account made without one has none.

// accountJoin is the invitation a sign-up was admitted by.
type accountJoin struct {
	Kind        string
	WorkspaceID string
	InviterID   string
}

// recordAccountJoin keeps how a new account came in, when an invitation
// admitted it.
func recordAccountJoin(txApp core.App, userID string, join accountJoin, now time.Time) error {
	if join.Kind == "" || join.WorkspaceID == "" {
		return nil
	}
	_, err := txApp.DB().NewQuery(`INSERT INTO {{leafen_account_joins}}
		([[user_id]], [[kind]], [[workspace_id]], [[inviter_id]], [[created_at]])
		VALUES ({:user}, {:kind}, {:workspace}, {:inviter}, {:now})
		ON CONFLICT ([[user_id]]) DO NOTHING`).Bind(dbx.Params{
		"user": userID, "kind": join.Kind, "workspace": join.WorkspaceID,
		"inviter": join.InviterID, "now": now.UnixMilli(),
	}).Execute()
	return err
}

// addressAccountJoin is the invitation that admits an address signing up
// without a token: a workspace's waiting invitation first, then a page
// shared with it, then a workspace allowing its domain.
func addressAccountJoin(app core.App, email string) (accountJoin, error) {
	join := accountJoin{}
	err := app.DB().NewQuery(`SELECT [[kind]], [[workspace_id]], [[inviter_id]] FROM (
			SELECT 1 AS [[rank]], 'workspace_email' AS [[kind]], [[workspace_id]], [[inviter_id]], [[created_at]] AS [[at]]
				FROM {{leafen_workspace_invitations}} WHERE [[email]] = {:email} AND [[status]] = 'pending'
			UNION ALL
			SELECT 2, 'page_email', [[workspace_id]], [[created_by]], [[created_at]]
				FROM {{leafen_page_permissions}} WHERE [[principal_type]] = 'email' AND [[principal_id]] = {:email}
			UNION ALL
			SELECT 3, 'allowed_domain', [[allowed]].[[workspace_id]], '', 0
				FROM {{leafen_workspace_allowed_email_domains}} [[allowed]]
				JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = [[allowed]].[[workspace_id]]
				WHERE {:domain} <> '' AND [[allowed]].[[domain]] = {:domain} AND [[workspace]].[[deletion_job_id]] = ''
		) ORDER BY [[rank]], [[at]] DESC LIMIT 1`).
		Bind(dbx.Params{"email": email, "domain": emailDomain(email)}).One(&join)
	if errors.Is(err, sql.ErrNoRows) {
		return accountJoin{}, nil
	}
	return join, err
}

func registerAdministratorAccountDeviceRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+"/admin/accounts/{accountId}/devices", readAdministratorAccountDevices)
	event.Router.POST(apiRoot+"/admin/accounts/{accountId}/sessions/{sessionId}/revoke", revokeAdministratorAccountSession)
}

// readAdministratorAccountDevices is how an account came in and the devices
// signed in to it.
func readAdministratorAccountDevices(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	accountID := strings.TrimSpace(event.Request.PathValue("accountId"))
	if _, err := event.App.FindRecordById("users", accountID); err != nil {
		return apiError(event, http.StatusNotFound, errorCodeAdministratorAccountNotFound, "Account not found.")
	}
	response := administratorAccountDevices{Devices: []administratorAccountDevice{}}
	joined := struct {
		Kind          string `db:"kind"`
		WorkspaceName string `db:"workspace_name"`
		InviterName   string `db:"inviter_name"`
		CreatedAt     int64  `db:"created_at"`
	}{}
	err := event.App.DB().NewQuery(`SELECT [[entry]].[[kind]], COALESCE([[workspace]].[[name]], '') AS [[workspace_name]],
			` + accountNameSQL("inviter") + ` AS [[inviter_name]], [[entry]].[[created_at]]
		FROM {{leafen_account_joins}} [[entry]]
		LEFT JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = [[entry]].[[workspace_id]]
		LEFT JOIN {{users}} [[inviter]] ON [[inviter]].[[id]] = [[entry]].[[inviter_id]]
		WHERE [[entry]].[[user_id]] = {:user}`).Bind(dbx.Params{"user": accountID}).One(&joined)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return administratorAccountDevicesError(event, err)
	}
	if err == nil {
		response.Join = &administratorAccountJoin{
			Kind: joined.Kind, WorkspaceName: joined.WorkspaceName, InviterName: joined.InviterName,
			JoinedAt: time.UnixMilli(joined.CreatedAt).UTC().Format(time.RFC3339Nano),
		}
	}
	rows := []storedAuthSession{}
	if err := event.App.DB().NewQuery(`
		SELECT [[id]], [[owner_id]], [[user_agent]], [[ip]], [[expires_at]],
			[[revoked_at]], [[created_at]], ` + authSessionLastUsedSQL + ` AS [[last_used_at]], ` + authSessionSecondFactorSQL + ` AS [[second_factor]],
			[[sign_in_method]]
		FROM {{leafen_auth_sessions}}
		WHERE [[owner_id]] = {:ownerId} AND [[revoked_at]] = 0 AND [[expires_at]] > {:now}
		ORDER BY ` + authSessionLastUsedSQL + ` DESC, [[id]] DESC
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"ownerId": accountID, "now": time.Now().UTC().UnixMilli(), "limit": authSessionLimit,
	}).All(&rows); err != nil {
		return administratorAccountDevicesError(event, err)
	}
	sortAuthSessionsByUse(rows)
	for _, row := range rows {
		response.Devices = append(response.Devices, administratorAccountDevice{
			ID:         row.ID,
			CreatedAt:  time.UnixMilli(row.CreatedAt).UTC().Format(time.RFC3339Nano),
			LastUsedAt: time.UnixMilli(row.LastUsedAt).UTC().Format(time.RFC3339Nano),
			UserAgent:  row.UserAgent, IP: row.IP,
			SecondFactor: row.SecondFactor,
			Registered:   row.SignInMethod == authSessionMethodDevice,
		})
	}
	return event.JSON(http.StatusOK, response)
}

func administratorAccountDevicesError(event *core.RequestEvent, err error) error {
	return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeAuthSessionListFailed,
		"Couldn't load the account's devices.", diagnosticOperation(event), "admin-devices", "SESSION_AUTHORITY_READ_FAILED", err)
}

// revokeAdministratorAccountSession ends one of an account's devices at
// once, kept in the account audit log as the other administrator actions.
func revokeAdministratorAccountSession(event *core.RequestEvent) error {
	input := administratorAccountMutationInput{}
	if err := event.BindBody(&input); err != nil {
		return administratorAccountMutationError(event, err, "session-bind")
	}
	sessionID := strings.TrimSpace(event.Request.PathValue("sessionId"))
	if !validAuthSessionID(sessionID) {
		return authSessionNotFoundAPIError(event)
	}
	return applyAdministratorAccountMutation(event, input.RequestID, input.ExpectedRevision, "session-revoke", sessionID,
		func(txApp core.App, targetID string, state storedAdministratorAccountState) (string, error) {
			now := time.Now().UTC().UnixMilli()
			result, err := txApp.DB().NewQuery(`UPDATE {{leafen_auth_sessions}}
				SET [[revoked_at]] = {:now}, [[updated_at]] = {:now}
				WHERE [[id]] = {:id} AND [[owner_id]] = {:owner} AND [[revoked_at]] = 0`).
				Bind(dbx.Params{"id": sessionID, "owner": targetID, "now": now}).Execute()
			if err != nil {
				return "", err
			}
			if changed, err := result.RowsAffected(); err != nil || changed != 1 {
				if err == nil {
					err = errAdministratorAccountSessionNotFound
				}
				return "", err
			}
			recheckCollaborationAccessAfterCommit(txApp)
			endSignInWaits(txApp, targetID)
			return state.Status, nil
		})
}
