package app

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// An administrator gives a member a one-time code that signs one device in
// to the member's account (the multi-server decision of 2026-10-20: a member
// who lost every device asks an administrator), as a code from a signed-in
// device does. It is done signed in, not with a key, and the server audit
// log keeps it.

const accountAuditDeviceCode = "device-code"

func registerAdministratorDeviceCodeRoutes(event *core.ServeEvent) {
	event.Router.POST(apiRoot+"/admin/accounts/{accountId}/device-codes", createAdministratorDeviceCode)
}

func createAdministratorDeviceCode(event *core.RequestEvent) error {
	if !authorizeAdministratorRequest(event) {
		return nil
	}
	administrator, ok, err := signedInPerson(event)
	if !ok {
		return err
	}
	targetID := event.Request.PathValue("accountId")
	target, err := event.App.FindRecordById("users", targetID)
	if errors.Is(err, sql.ErrNoRows) {
		return apiError(event, http.StatusNotFound, errorCodeAdministratorAccountNotFound, "Account not found.")
	}
	if err != nil {
		return deviceCodeAPIError(event, err)
	}
	state, err := loadAdministratorAccountState(event.App, target.Id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return deviceCodeAPIError(event, err)
	}
	if state.Status == administratorAccountStatusDisabled || state.Status == administratorAccountStatusDeleting ||
		state.Status == administratorAccountStatusDeleted {
		return apiError(event, http.StatusConflict, errorCodeDeviceCodeFailed, "A turned-off or deleted account can't join a device.")
	}
	if err := authorizeDeviceSignIn(event.App, target.Id); err != nil {
		return deviceCodeAPIError(event, err)
	}
	origin, err := outwardServerOrigin(event.Request)
	if err != nil {
		return deviceCodeAPIError(event, err)
	}
	var code accountDeviceCodeResponse
	err = event.App.RunInTransaction(func(txApp core.App) error {
		issued, err := issueAccountDeviceCode(txApp, target.Id, administrator.Id, "", origin)
		if err != nil {
			return err
		}
		code = issued
		return recordAdministratorAccountAudit(txApp, administrator.Id, target.Id, accountAuditDeviceCode,
			state.Status, state.Status, state.Revision, "device-code-"+core.GenerateDefaultRandomId(), time.Now().UTC().UnixMilli())
	})
	if err != nil {
		return deviceCodeAPIError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusCreated, code)
}
