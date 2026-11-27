package app

import (
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The app's own server lets the person who owns its device back in when the
// window lost its sign-in (the multi-server decision of 2026-10-20: "the
// desktop gets a new code by the system's owner check"): the shell asks the
// system — Touch ID, Face ID or the device's password — and only then makes a
// one-time device code here, in the server's own process. No HTTP route makes
// one, so a page can't skip the check. The organization's rules come first
// (device_sign_in_policy.go), and the server audit log keeps it.

const (
	accountAuditDeviceCodeOwner = "device-code-owner"
	// The app's own window signed the person who set its server up back in
	// by itself (SetupOwnerSignInBackCode).
	accountAuditDeviceCodeOwnWindow = "device-code-own-window"
)

// maxDeviceOwnerAccounts is how many accounts the owner check offers; an
// app's own server has its owner's, or a few.
const maxDeviceOwnerAccounts = 50

// DeviceOwnerAccount is an account the owner check can sign back in to.
type DeviceOwnerAccount struct {
	ID   string `json:"id" db:"id"`
	Name string `json:"name" db:"name"`
}

var errDeviceOwnerAccountClosed = errors.New("the account is turned off or being deleted")

// DeviceOwnerAccounts lists the accounts on the server that may sign in,
// oldest first.
func DeviceOwnerAccounts(app core.App) ([]DeviceOwnerAccount, error) {
	accounts := []DeviceOwnerAccount{}
	err := app.DB().NewQuery(`SELECT [[user]].[[id]], [[user]].[[name]] FROM {{users}} [[user]]
		LEFT JOIN {{leafen_administrator_account_states}} [[state]] ON [[state]].[[user_id]] = [[user]].[[id]]
		WHERE COALESCE([[state]].[[status]], 'active') = 'active'
		ORDER BY [[user]].[[created]], [[user]].[[id]] LIMIT {:limit}`).
		Bind(dbx.Params{"limit": maxDeviceOwnerAccounts}).All(&accounts)
	return accounts, err
}

// IssueDeviceOwnerCode makes a one-time device code for the account once the
// shell has checked the device's owner. It fails with ErrSAMLRequired or
// ErrDeviceSignInRestricted where the account's organization keeps it from
// device sign-ins.
func IssueDeviceOwnerCode(app core.App, accountID string) (string, error) {
	code, err := issueServerOwnDeviceCode(app, accountID, accountAuditDeviceCodeOwner)
	return code.Code, err
}

// SetupOwnerSignInBackCode makes a one-time device code for the person who
// set the app's own server up, with no system check: the app's window,
// landing on its sign-in page without that person having signed out there,
// signs them back in by itself (user decision 2026-11-19 "넣어": a first
// administrator made with a name alone, who has no password, is never kept
// out by a sign-in the window lost). The shell asks it, and only for the
// server it runs itself; a sign-out the person asked for leaves the owner
// check as the way back (IssueDeviceOwnerCode). It answers "" where the
// server isn't set up or the person who set it up can't sign in now.
func SetupOwnerSignInBackCode(app core.App) (string, error) {
	owner := ""
	if err := app.DB().NewQuery(`SELECT [[setup_admin_id]] FROM {{leafen_server_settings}} WHERE [[id]] = 'default'`).
		Row(&owner); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if owner == "" {
		return "", nil
	}
	code, err := issueServerOwnDeviceCode(app, owner, accountAuditDeviceCodeOwnWindow)
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, errDeviceOwnerAccountClosed) {
		return "", nil
	}
	return code.Code, err
}

// issueServerOwnDeviceCode makes a one-time device code for an active
// account, inside the server's own process, with no address to open it at,
// and keeps it in the server audit log as action. It fails with
// ErrSAMLRequired or ErrDeviceSignInRestricted where the account's
// organization keeps it from device sign-ins.
func issueServerOwnDeviceCode(app core.App, accountID, action string) (accountDeviceCodeResponse, error) {
	user, err := app.FindRecordById("users", accountID)
	if err != nil {
		return accountDeviceCodeResponse{}, err
	}
	state, err := loadAdministratorAccountState(app, user.Id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return accountDeviceCodeResponse{}, err
	}
	if state.Status != "" && state.Status != administratorAccountStatusActive {
		return accountDeviceCodeResponse{}, errDeviceOwnerAccountClosed
	}
	if err := authorizeDeviceSignIn(app, user.Id); err != nil {
		return accountDeviceCodeResponse{}, err
	}
	var code accountDeviceCodeResponse
	err = app.RunInTransaction(func(txApp core.App) error {
		issued, err := issueAccountDeviceCode(txApp, user.Id, "", "", "")
		if err != nil {
			return err
		}
		code = issued
		return recordAdministratorAccountAudit(txApp, "", user.Id, action,
			state.Status, state.Status, state.Revision, action+"-"+core.GenerateDefaultRandomId(), time.Now().UTC().UnixMilli())
	})
	return code, err
}
