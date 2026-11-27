package app

import (
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Whether an account's person knows its password (the multi-server decision
// of 2026-10-20: a password is a backup sign-in, chosen in settings). An
// account made with a name only, or by a sign-in provider, or whose
// password the server made for it, has one no one knows: it sets one there
// without a current password, and doing so ends no device's sign-in. Any
// password a person or an administrator gives makes it known.

// markAccountPasswordUnknown records that no one knows the account's password.
func markAccountPasswordUnknown(app core.App, ownerID string) error {
	_, err := app.DB().NewQuery(`INSERT INTO {{leafen_account_unknown_passwords}} ([[owner_id]], [[since]])
		VALUES ({:owner}, {:now}) ON CONFLICT ([[owner_id]]) DO NOTHING`).
		Bind(dbx.Params{"owner": ownerID, "now": time.Now().UnixMilli()}).Execute()
	return err
}

// markAccountPasswordKnown records that the account's password was given.
func markAccountPasswordKnown(app core.App, ownerID string) error {
	_, err := app.DB().NewQuery(`DELETE FROM {{leafen_account_unknown_passwords}} WHERE [[owner_id]] = {:owner}`).
		Bind(dbx.Params{"owner": ownerID}).Execute()
	return err
}

// accountPasswordKnown says whether the account's person set its password.
func accountPasswordKnown(app core.App, ownerID string) (bool, error) {
	unknown := 0
	err := app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_account_unknown_passwords}} WHERE [[owner_id]] = {:owner}`).
		Bind(dbx.Params{"owner": ownerID}).Row(&unknown)
	return unknown == 0, err
}

// setPasswordKeepingSignIns gives the account a password and leaves every
// device signed in: a save would renew the account's token key, which ends
// all its sign-ins, so the hash is written by itself. A first password or a
// forgotten one changes no device's standing; changing a known one does
// (account_password.go). The password comes hashed before the transaction
// (password_outside_transaction.go).
func setPasswordKeepingSignIns(app core.App, user *core.Record, password preparedPassword) error {
	field, ok := user.Collection().Fields.GetByName(core.FieldNamePassword).(*core.PasswordField)
	if !ok {
		return errors.New("the account has no password field")
	}
	password.set(user)
	hash, err := field.DriverValue(user)
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`UPDATE {{users}} SET [[password]] = {:hash} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"hash": hash, "id": user.Id}).Execute()
	return err
}
