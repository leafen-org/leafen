package app

import (
	"cmp"
	"database/sql"
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// What a page's state lets anyone have on it, whoever asks: a person's
// decision (decidePageAccessRole) and the system's (system_job_access.go)
// both pass these before either decides a role. A page being imported, a
// workspace moving away, blocks being moved, a copy being made, a permanent
// deletion or a wiki's change under way refuse everyone alike.

// loadAccessPage reads the page a decision is about, refusing one an import
// still stages or whose workspace is moving away, with the fences its
// workspace has under way.
func loadAccessPage(app core.App, pageID string, minimum pageAccessRole) (*core.Record, pageFences, error) {
	page, err := findRecordRemembered(app, pagesCollection, pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, pageFences{}, permissionDenied()
	}
	if err != nil {
		return nil, pageFences{}, err
	}
	if page.GetBool("notion_import_staging") {
		return nil, pageFences{}, permissionDenied()
	}
	if err := workspaceMoveFence(app, page.GetString("workspace"), minimum); err != nil {
		return nil, pageFences{}, err
	}
	// The fences on a page or one above — blocks being moved, a permanent
	// deletion, a wiki's change, a page being copied — are looked for only
	// where the workspace has one under way.
	fences, err := workspacePageFences(app, page.GetString("workspace"))
	if err != nil {
		return nil, pageFences{}, err
	}
	return page, fences, nil
}

// pageStateAdmits refuses a page, other than a row's, that a copy is still
// making or holds, that a permanent deletion or (for editing) a wiki's change
// fences, or whose workspace is being deleted or keeps none of it; else it
// gives the page's workspace.
func pageStateAdmits(app core.App, page *core.Record, minimum pageAccessRole, fences pageFences) (*core.Record, error) {
	workspace, held, err := pageStateAdmitsHeld(app, page, minimum, fences)
	if err == nil && held != "" {
		return nil, permissionDenied()
	}
	return workspace, err
}

// pageStateAdmitsHeld is pageStateAdmits, telling a page an operation holds
// for a while — a copy making it or copying it, a wiki's change, for
// editing — by the operation (page_access_hold.go) rather than refusing it.
// A permanent deletion ends with the page gone, so it stays refused.
func pageStateAdmitsHeld(app core.App, page *core.Record, minimum pageAccessRole, fences pageFences) (*core.Record, string, error) {
	visible, err := pageDuplicationTargetVisible(app, page)
	if err != nil {
		return nil, "", err
	}
	if !visible {
		return nil, "", permissionDenied()
	}
	held := ""
	if minimum > pageAccessRoleView {
		underWay, err := pageDuplicationTargetUnderWay(app, page)
		if err != nil {
			return nil, "", err
		}
		if underWay {
			held = pageBusyCopying
		}
	}
	fenced := func(active bool, fence func(core.App, string) (bool, error)) (bool, error) {
		if !active {
			return false, nil
		}
		return fence(app, page.Id)
	}
	if deleting, err := fenced(fences.Deleting, pagePermanentDeleteFenceActive); err != nil || deleting {
		return nil, "", cmp.Or(err, permissionDenied())
	}
	if minimum >= pageAccessRoleEdit {
		for _, check := range []struct {
			active bool
			fence  func(core.App, string) (bool, error)
			busy   string
		}{
			{fences.Wiki, wikiMutationFenceActive, pageBusyWiki},
			{fences.Duplicating, pageDuplicationHeldFenceActive, pageBusyCopying},
		} {
			if on, err := fenced(check.active, check.fence); err != nil {
				return nil, "", err
			} else if on && held == "" {
				held = check.busy
			}
		}
	}
	workspace, err := findRecordRemembered(app, workspacesCollection, page.GetString("workspace"))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", permissionDenied()
	}
	if err != nil {
		return nil, "", err
	}
	if workspace.GetString("deletion_job_id") != "" ||
		page.GetString("owner") != workspace.GetString("owner") {
		return nil, "", permissionDenied()
	}
	return workspace, held, nil
}
