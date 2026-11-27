package app

import (
	"github.com/pocketbase/pocketbase/core"
)

// The per-person page list is retired: it held a row for each person and
// each page they see, so a workspace's members times its pages, and every
// page or teamspace change rewrote it. The sidebar's pages are read when
// asked, through the person's workspaces and the teamspaces open to them
// (bootstrap_page_access.go); what stays kept per person is their
// workspaces. The triggers that queued its work on pages, teamspaces, their
// members and settings, permission groups and restricted members go with
// it, and two page indexes read a teamspace's pages, and a person's private
// ones, in the order they were made.

// bootstrapPageVisibilityKept says the per-person page list is still kept,
// as before migration 613: earlier migrations' checks look for its parts
// only then.
func bootstrapPageVisibilityKept(app core.App) bool {
	return app.HasTable(bootstrapPageVisibilityTable)
}
