package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

// The order a person dragged their workspaces to in the workspace menu
// follows them to every device, as Notion's does (seen 2026-11-16: a
// workspace dragged in Chrome stood in its new place in iPhone Safari's
// "계정과 워크스페이스" and in Chrome read again). It is kept with the
// account's display preferences (account_display_preferences.go), apart
// from them: the workspaces in the order dragged, those not named after them
// in the menu's own order.

const (
	accountWorkspaceOrderPath      = "/account/workspace-order"
	accountWorkspaceOrderBodyBytes = 32 << 10
	accountWorkspaceOrderMax       = 500
	accountWorkspaceOrderIDBytes   = 128

	errorCodeAccountWorkspaceOrderInvalid = "LEAFEN_ACCOUNT_WORKSPACE_ORDER_INVALID"
	errorCodeAccountWorkspaceOrderFailed  = "LEAFEN_ACCOUNT_WORKSPACE_ORDER_FAILED"
)

type accountWorkspaceOrder struct {
	WorkspaceIDs []string `json:"workspaceIds"`
}

func registerAccountWorkspaceOrderRoutes(event *core.ServeEvent) {
	event.Router.GET(apiRoot+accountWorkspaceOrderPath, readAccountWorkspaceOrder)
	event.Router.PUT(apiRoot+accountWorkspaceOrderPath, saveAccountWorkspaceOrder).
		Bind(apis.BodyLimit(accountWorkspaceOrderBodyBytes))
}

// validAccountWorkspaceOrder holds an order to distinct, bounded IDs.
func validAccountWorkspaceOrder(ids []string) bool {
	if len(ids) > accountWorkspaceOrderMax {
		return false
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if id == "" || len(id) > accountWorkspaceOrderIDBytes {
			return false
		}
		if _, repeated := seen[id]; repeated {
			return false
		}
		seen[id] = struct{}{}
	}
	return true
}

func loadAccountWorkspaceOrder(app core.App, ownerID string) (accountWorkspaceOrder, error) {
	var stored string
	err := app.DB().NewQuery(`
		SELECT [[workspace_order]] FROM {{leafen_account_display_preferences}} WHERE [[owner_id]] = {:ownerId}
	`).Bind(dbx.Params{"ownerId": ownerID}).Row(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return accountWorkspaceOrder{WorkspaceIDs: []string{}}, nil
	}
	if err != nil {
		return accountWorkspaceOrder{}, err
	}
	ids := []string{}
	if json.Unmarshal([]byte(stored), &ids) != nil || !validAccountWorkspaceOrder(ids) {
		ids = []string{}
	}
	return accountWorkspaceOrder{WorkspaceIDs: ids}, nil
}

func readAccountWorkspaceOrder(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	order, err := loadAccountWorkspaceOrder(event.App, user.Id)
	if err != nil {
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeAccountWorkspaceOrderFailed,
			"Couldn't load the workspace order.", "account_workspace_order.read", "storage-read",
			errorCodeAccountWorkspaceOrderFailed, err,
		)
	}
	return event.JSON(http.StatusOK, order)
}

func saveAccountWorkspaceOrder(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := accountWorkspaceOrder{}
	if err := event.BindBody(&input); err != nil || input.WorkspaceIDs == nil || !validAccountWorkspaceOrder(input.WorkspaceIDs) {
		return apiError(event, http.StatusBadRequest, errorCodeAccountWorkspaceOrderInvalid, "Check the workspace order.")
	}
	encoded, err := json.Marshal(input.WorkspaceIDs)
	if err == nil {
		_, err = event.App.DB().NewQuery(`
			INSERT INTO {{leafen_account_display_preferences}} ([[owner_id]], [[workspace_order]], [[updated_at]])
			VALUES ({:ownerId}, {:order}, {:updatedAt})
			ON CONFLICT ([[owner_id]]) DO UPDATE SET
				[[workspace_order]] = excluded.[[workspace_order]], [[updated_at]] = excluded.[[updated_at]]
		`).Bind(dbx.Params{"ownerId": user.Id, "order": string(encoded), "updatedAt": time.Now().UnixMilli()}).Execute()
	}
	if err != nil {
		return apiErrorWithCause(
			event, http.StatusInternalServerError, errorCodeAccountWorkspaceOrderFailed,
			"Couldn't save the workspace order.", "account_workspace_order.save", "storage-write",
			errorCodeAccountWorkspaceOrderFailed, err,
		)
	}
	return event.JSON(http.StatusOK, input)
}
