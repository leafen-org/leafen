package app

import "github.com/pocketbase/pocketbase/core"

// endSignInWaits ends the waits of a person's open screens once one of
// their sign-ins ends — revoked by them or an administrator, by a password
// change, or by a new device taking the oldest one's place — so each screen
// asks again at once and the ended one hears it is signed out then, rather
// than when its wait runs out. After the change commits.
func endSignInWaits(app core.App, ownerID string) {
	if ownerID == "" {
		return
	}
	end := func() {
		workspacePageListWatches.Range(func(_, watch any) bool {
			watch.(*workspacePageListWatch).endUser(ownerID)
			return true
		})
	}
	if transaction := app.TxInfo(); transaction != nil {
		transaction.OnComplete(func(err error) error {
			if err == nil {
				end()
			}
			return nil
		})
		return
	}
	end()
}
