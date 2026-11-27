package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// noteWorkspaceLookChangedAfterCommit marks, once the change commits (at
// once outside a transaction), the waits of everyone with a screen open on
// a workspace whose name, icon, domain or landing page changed, and of its
// owner and members wherever their screens are open, on every server watch:
// their change sockets say so (workspace_change_socket.go), and their
// sidebar heads and workspace menus read the workspaces again within about a
// second, without a reload, as Notion's follow a rename within seconds (seen
// 2026-11-16: another member's open screen took the new name). A guest's
// screen open on another workspace takes it at its next read of the list.
func noteWorkspaceLookChangedAfterCommit(app core.App, workspaceID string) {
	// Who is in it is read now, in the change's own transaction.
	people := map[string]struct{}{}
	ids := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[workspace_owner]] FROM {{leafen_workspaces}} WHERE [[id]] = {:workspaceId}
		UNION
		SELECT [[user_id]] FROM {{leafen_workspace_memberships}} WHERE [[workspace_id]] = {:workspaceId}
	`).Bind(dbx.Params{"workspaceId": workspaceID}).Column(&ids); err == nil {
		for _, id := range ids {
			people[id] = struct{}{}
		}
	}
	note := func() {
		workspacePageListWatches.Range(func(_, watch any) bool {
			w := watch.(*workspacePageListWatch)
			w.mu.Lock()
			for waiter := range w.waiters {
				if _, member := people[waiter.userID]; member || waiter.workspaceID == workspaceID {
					waiter.workspaces = true
				}
			}
			w.mu.Unlock()
			return true
		})
	}
	if transaction := app.TxInfo(); transaction != nil {
		transaction.OnComplete(func(err error) error {
			if err == nil {
				note()
			}
			return nil
		})
		return
	}
	note()
}
