package app

import (
	"github.com/pocketbase/pocketbase/core"
)

// A deleted account's deletion line in the server audit log keeps the email
// the account had when its deletion started, so an administrator can tell
// whom they deleted (user decision 2026-11-07): that line alone, read only by
// those who read the audit log, and gone with it when its retention ends.

// deletionAuditEmail is the account's email as its deletion starts, for its
// deletion line; empty when the account has none to keep.
func deletionAuditEmail(app core.App, userID string) string {
	user, err := app.FindRecordById("users", userID)
	if err != nil {
		return ""
	}
	return visibleAccountEmail(user.Email())
}
