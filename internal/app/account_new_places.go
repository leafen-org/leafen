package app

// An account being deleted, or deleted, is given no new place in a
// workspace: no teamspace takes it, added by hand, by an approved request or
// as a default teamspace stops being one; and no people picker offers it.
// The cleanup takes what was only the person's, but teamspace memberships
// stay while their workspace
// lives (account_deletion_workspace_inventory.go), so a place given in the
// time before the cleanup reaches the person's workspace memberships stayed
// after the account was gone (2026-11-17). An account its administrator
// deactivated, or an organization suspended, keeps its places and is given
// new ones as before: it may come back, and finds them all again.

// accountTakesNewPlacesSQL is that rule as an SQL expression over the user
// the given expression names.
func accountTakesNewPlacesSQL(userExpression string) string {
	return `NOT EXISTS (SELECT 1 FROM {{leafen_administrator_account_states}} [[leaving_account]]
			WHERE [[leaving_account]].[[user_id]] = ` + userExpression + `
				AND [[leaving_account]].[[status]] IN ('deleting', 'deleted'))`
}
