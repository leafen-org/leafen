package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An organization may suspend a person whose account it manages: their
// sessions end, their tokens and AI connections stop and no email reaches
// them. The organization module keeps the suspensions; the core asks it who
// is suspended where it decides whether an account may act, and without it
// nobody is.

// organizationSuspendedSQL is an SQL expression, true when an organization
// has suspended the user the given expression names.
func organizationSuspendedSQL(userExpression string) string {
	if suspended := organizationPolicy.SuspendedUserSQL; suspended != nil {
		return "(" + suspended(userExpression) + ")"
	}
	return "0"
}

func organizationUserSuspended(app core.App, userID string) (bool, error) {
	var suspended int
	err := app.DB().NewQuery(`SELECT ` + organizationSuspendedSQL("{:userId}")).
		Bind(dbx.Params{"userId": userID}).Row(&suspended)
	return suspended == 1, err
}

// accountMayAct is true of an account the server's administrator hasn't
// deactivated and no organization has suspended, nor one being deleted or
// deleted: its tokens and AI connections act for it, emails reach it and
// its requests to join a teamspace may be approved.
func accountMayAct(app core.App, userID string) (bool, error) {
	var allowed int
	err := app.DB().NewQuery(`SELECT ` + accountMayActSQL("{:userId}")).
		Bind(dbx.Params{"userId": userID}).Row(&allowed)
	return allowed == 1, err
}

// accountMayActSQL is accountMayAct as an SQL expression over the user the
// given expression names, for a list that keeps to such accounts.
func accountMayActSQL(userExpression string) string {
	return `(EXISTS (SELECT 1 FROM {{leafen_administrator_account_states}} [[acting_account]]
			WHERE [[acting_account]].[[user_id]] = ` + userExpression + ` AND [[acting_account]].[[status]] = 'active')
		AND NOT ` + organizationSuspendedSQL(userExpression) + `)`
}
