package app

import (
	"strconv"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A server administrator makes another active account a server
// administrator or removes one, so the server never depends on the account
// that set it up. An administrator cannot change their own role, so the
// server always keeps the administrator who acts. Each change is kept in
// the account audit log.
func updateAdministratorAccountRole(event *core.RequestEvent) error {
	input := administratorAccountRoleInput{}
	if err := event.BindBody(&input); err != nil {
		return administratorAccountMutationError(event, err, "role-bind")
	}
	action := "administrator-revoke"
	if input.Administrator {
		action = "administrator-grant"
	}
	return applyAdministratorAccountMutation(event, input.RequestID, input.ExpectedRevision, action, strconv.FormatBool(input.Administrator),
		func(txApp core.App, targetID string, state storedAdministratorAccountState) (string, error) {
			if input.Administrator {
				if state.Status != administratorAccountStatusActive {
					return "", errAdministratorAccountDisabled
				}
				_, err := txApp.DB().NewQuery(`
					INSERT INTO {{leafen_administrators}} ([[user_id]], [[created_at]]) VALUES ({:userId}, {:now})
					ON CONFLICT ([[user_id]]) DO NOTHING
				`).Bind(dbx.Params{"userId": targetID, "now": time.Now().UTC().UnixMilli()}).Execute()
				return state.Status, err
			}
			_, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_administrators}} WHERE [[user_id]] = {:userId}`).
				Bind(dbx.Params{"userId": targetID}).Execute()
			return state.Status, err
		})
}
