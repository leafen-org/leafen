package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// A page an operation holds for a while — its blocks being moved, a wiki's
// change, its copy, its workspace moving (pageHeldForMember) — is told to an
// agent as busy, under LEAFEN_PAGE_BUSY in the app's words, which say when it
// takes changes again, as the app's own routes answer it 423: never as a page
// not found, as a permission it lacks, or as a failure to try once more
// (2026-11-19: notion-fetch and the parent of notion-create-pages and
// notion-create-database read a page whose blocks were being moved as gone).
// A page being deleted for good stays gone.

var errMCPPageBusy = errors.New(errorCodePageBusy)

// mcpPageBusy is a call refused while an operation holds its page.
func mcpPageBusy(reason string) error {
	return fmt.Errorf("%w: %s", errMCPPageBusy, pageBusyMessage(reason))
}

// mcpPageHeld is the operation holding a page this connection reaches — in a
// workspace it was given, among its pages when its pages are narrowed — for a
// while, told to a member of the page's workspace (pageHeldForMember); "" for
// any other page.
func mcpPageHeld(app core.App, authority mcpAuthority, pageID string) string {
	page, err := findRecordRemembered(app, pagesCollection, strings.TrimSpace(pageID))
	if err != nil || !mcpAuthorityReachesPage(app, authority, page) {
		return ""
	}
	workspaceID := page.GetString("workspace")
	workspace, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{WorkspaceID: workspaceID}, permissionWorkspaceRead)
	if err != nil || workspace.WorkspaceID != workspaceID {
		return ""
	}
	return pageHeldForMember(app, authority.PrincipalID, page.Id)
}

// mcpPageReadRefusal is why a page wasn't opened for reading
// (authorizeMCPPageRead): busy while an operation holds it, else what the
// account can't read, or a failure.
func mcpPageReadRefusal(app core.App, authority mcpAuthority, pageID string, err error) error {
	if !isPermissionDenied(err) {
		return failedBecause(errMCPToolFailed, err)
	}
	if busy := mcpPageHeld(app, authority, pageID); busy != "" {
		return mcpPageBusy(busy)
	}
	return mcpRefusedUnlessStorage(errMCPCantRead, err)
}

// mcpHeldPageFailure is a call's failure as the page busy, when what failed
// it (failure, its tool's error) is a held page's refusal (pageHeldRefusal)
// and its words are a failure's (mcpStopFailures); any other is as it was.
func mcpHeldPageFailure(failure error, text string) string {
	code, _, _ := strings.Cut(strings.TrimSpace(text), ":")
	if !mcpStopFailures[code] {
		return text
	}
	if reason := pageHeldRefusal(failure); reason != "" {
		return mcpPageBusy(reason).Error()
	}
	return text
}
