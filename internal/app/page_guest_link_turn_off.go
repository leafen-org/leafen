package app

import "github.com/pocketbase/pocketbase/core"

// Who may turn a guest invite link off: the person who made it, or an owner
// of its workspace. The revoke route lets them and no one else, and each
// link a list answers says whether its reader may (revocable), which the
// share window follows: it offered the button on the reader's own links
// alone, and a workspace owner couldn't turn off another person's link there
// though the server let them (2026-11-23).

// pageGuestLinkTurnOff judges for one reader, reading their role in a
// workspace once.
type pageGuestLinkTurnOff struct {
	app      core.App
	viewerID string
	owner    map[string]bool
}

func newPageGuestLinkTurnOff(app core.App, viewerID string) *pageGuestLinkTurnOff {
	return &pageGuestLinkTurnOff{app: app, viewerID: viewerID, owner: map[string]bool{}}
}

// allows tells whether the reader may turn the link off.
func (rule *pageGuestLinkTurnOff) allows(link storedPageGuestLink) (bool, error) {
	if link.CreatedBy == rule.viewerID {
		return true, nil
	}
	owner, known := rule.owner[link.WorkspaceID]
	if !known {
		role, err := workspaceRoleOf(rule.app, rule.viewerID, link.WorkspaceID)
		if err != nil {
			return false, err
		}
		owner = role == workspaceRoleOwner
		rule.owner[link.WorkspaceID] = owner
	}
	return owner, nil
}
