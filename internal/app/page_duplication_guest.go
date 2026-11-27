package app

import (
	"github.com/pocketbase/pocketbase/core"
)

// Every copy carries only what the person making it reads, wherever it goes:
// a member's ⌘D, "복제", "이동"'s copy, a block copy of a sub-page or of a
// database, a database item's duplicate. Below a page they open, a sub-page
// shared only with others — its access cut from the page above — stays
// behind with all under it, its files and rows with it; a database's items
// they may not open stay behind with their files, and their sub-items they
// may open come as items of their own; an item's duplicate leaves its
// sub-items they may not open, and theirs. Before, a copy in the same
// workspace read its pages as their owner and took them all, and the copy,
// open to whoever opens the place it goes, gave them to its maker
// (2026-11-16: a member's copy of a teamspace page held a sub-page only its
// owner opened, and the member opened the copy's).
//
// A guest's "페이지 복제 ›" copies a page shared with them into a workspace
// of their own, as Notion's does (seen 2026-11-15): they write no page of
// the workspace the page is in, so the page's own authorization names that
// workspace, and the copy keeps to the sub-pages and rows the guest may open
// — what is shared with them and what inherits it. Links, mentions, synced
// copies and linked views keep pointing at what they named, read with the
// reader's own rights wherever they show; files are copied as their maker
// reads them (page_duplication_files.go), and comments are not copied.
// A visitor's copy of a published site keeps to what the site shows
// (public_page_duplicate.go).

// A guest who may edit the page above a page shared with them duplicates it
// beside it, as Notion's "복제" does (seen 2026-11-16, a guest editing both:
// "〈page〉 (1)" right after it under the same page, open as that page is,
// without its sub-page shared only with others). Where they only read the
// page above, or the page tops what is shared with them, Notion's menu has
// no "복제", and the server refuses such a copy.

// pageDuplicationSourceWorkspace is the workspace a copy reads its page in:
// one the person writes, or the page's own, which a guest reads by its
// shares: for a copy into another workspace of theirs, or beside the page
// under a page they may edit, which admitting the copy checks.
func pageDuplicationSourceWorkspace(
	app core.App, actorID string, source pageAuthorization, input pageDuplicationInput,
) (workspaceAuthorization, error) {
	workspace, err := authorizeWorkspace(app, actorID, source.Workspace.WorkspaceID, permissionWorkspaceWrite)
	elsewhere := input.DestinationWorkspaceID != "" && input.DestinationWorkspaceID != source.Workspace.WorkspaceID
	beside := !elsewhere && input.DestinationParentID != "" && input.DestinationTeamspaceID == "" && !input.ToPrivatePages
	if !isPermissionDenied(err) || !elsewhere && !beside {
		return workspace, err
	}
	return source.Workspace, nil
}

// authorizePageDuplicationTarget says the copy's maker may still write where
// it goes: its workspace, or, for a guest's copy beside its page, the page
// it goes under.
func authorizePageDuplicationTarget(app core.App, actorID string, job storedPageDuplicationJob) error {
	_, err := authorizeWorkspace(app, actorID, job.WorkspaceID, permissionWorkspaceWrite)
	if !isPermissionDenied(err) || job.crossWorkspace() || job.DestinationParentID == "" {
		return err
	}
	_, err = authorizePage(app, actorID, job.DestinationParentID, permissionWorkspaceWrite)
	return err
}

// pageDuplicationReadsAsActor says whether a copy keeps to what its maker
// reads: every copy but a visitor's copy of a published site.
func pageDuplicationReadsAsActor(job storedPageDuplicationJob) bool {
	return job.SourcePublicLinkID == ""
}

// copyUnreadablePages are the pages among pageIDs — sub-pages, or database
// items, whose pages they are — that the copy's maker may not open. They are
// judged as the pages stand, without the fence a block move or copy puts on
// its pages while it runs: that fence keeps writes off content on its way —
// a block copy fences the very page it copies from — not its readers out.
// One copy's window asks its questions — the teamspace's roles, the pages
// above — once.
func copyUnreadablePages(app core.App, actorID, workspaceID string, pageIDs []string) (map[string]struct{}, error) {
	left := map[string]struct{}{}
	if len(pageIDs) == 0 {
		return left, nil
	}
	reading := &accessMemoApp{App: app, memo: map[any]accessMemoEntry{}}
	fences, err := workspacePageFences(reading, workspaceID)
	if err != nil {
		return nil, err
	}
	fences.Moving = false
	reading.memo[pageFencesMemoKey{workspaceID: workspaceID}] = accessMemoEntry{value: fences}
	for _, pageID := range pageIDs {
		if _, err := authorizePage(reading, actorID, pageID, permissionWorkspaceRead); isPermissionDenied(err) {
			left[pageID] = struct{}{}
		} else if err != nil {
			return nil, err
		}
	}
	return left, nil
}

// pageDuplicationReadableChildren are the sub-pages found under a page that
// the copy takes, with their IDs: those its maker may open.
func pageDuplicationReadableChildren(
	app core.App, job storedPageDuplicationJob, children []pageDuplicationChild,
) ([]pageDuplicationChild, []string, error) {
	left := map[string]struct{}{}
	if pageDuplicationReadsAsActor(job) {
		ids := make([]string, len(children))
		for index, child := range children {
			ids[index] = child.ID
		}
		var err error
		if left, err = copyUnreadablePages(app, job.ActorID, job.sourceWorkspace(), ids); err != nil {
			return nil, nil, err
		}
	}
	kept := make([]pageDuplicationChild, 0, len(children))
	ids := make([]string, 0, len(children))
	for _, child := range children {
		if _, unreadable := left[child.ID]; !unreadable {
			kept = append(kept, child)
			ids = append(ids, child.ID)
		}
	}
	return kept, ids, nil
}

// pageDuplicationUnreadableRows are the rows of a database a copy leaves out:
// those whose pages its maker may not open.
func pageDuplicationUnreadableRows(
	app core.App, job storedPageDuplicationJob, rows []pageDuplicationDatabaseSourceID,
) (map[string]struct{}, error) {
	if !pageDuplicationReadsAsActor(job) {
		return map[string]struct{}{}, nil
	}
	ids := make([]string, len(rows))
	for index, row := range rows {
		ids[index] = row.ID
	}
	return copyUnreadablePages(app, job.ActorID, job.sourceWorkspace(), ids)
}

// pageDuplicationParentLeftOut says whether a sub-item's item stayed behind,
// its copy's maker unable to open it: the sub-item then comes as an item of
// its own, as one whose item is in the trash does.
func pageDuplicationParentLeftOut(app core.App, job storedPageDuplicationJob, parentID string) (bool, error) {
	if !pageDuplicationReadsAsActor(job) {
		return false, nil
	}
	left, err := copyUnreadablePages(app, job.ActorID, job.sourceWorkspace(), []string{parentID})
	_, unreadable := left[parentID]
	return unreadable, err
}

// storePageDuplicationReadableFiles takes in the files a copy carries: those
// of a database item its maker may not open — its file cells', its page
// icon's — stay behind with the item.
func storePageDuplicationReadableFiles(
	app core.App, job storedPageDuplicationJob, references []pageDuplicationFileReference,
) error {
	rowIDs := []string{}
	if pageDuplicationReadsAsActor(job) {
		for _, reference := range references {
			if reference.OwnerKind == productFileReferenceOwnerRow ||
				reference.OwnerKind == productFileReferenceOwnerDatabaseRowPageIcon {
				rowIDs = append(rowIDs, reference.OwnerID)
			}
		}
	}
	left, err := copyUnreadablePages(app, job.ActorID, job.sourceWorkspace(), rowIDs)
	if err != nil {
		return err
	}
	kept := make([]pageDuplicationFileReference, 0, len(references))
	for _, reference := range references {
		if _, unreadable := left[reference.OwnerID]; !unreadable || reference.OwnerKind != productFileReferenceOwnerRow &&
			reference.OwnerKind != productFileReferenceOwnerDatabaseRowPageIcon {
			kept = append(kept, reference)
		}
	}
	return storePageDuplicationFileMappings(app, job.ID, kept)
}

// readableRowDuplicateTree is an item's tree its duplicate takes: without
// the sub-items its maker may not open and those under them. The item
// itself, at depth 0, they write.
func readableRowDuplicateTree(
	app core.App, actorID, workspaceID string, rows []databaseRowTreeRow,
) ([]databaseRowTreeRow, error) {
	ids := make([]string, 0, len(rows))
	parents := make(map[string]string, len(rows))
	for _, row := range rows {
		parents[row.ID] = row.ParentID
		if row.Depth > 0 {
			ids = append(ids, row.ID)
		}
	}
	left, err := copyUnreadablePages(app, actorID, workspaceID, ids)
	if err != nil || len(left) == 0 {
		return rows, err
	}
	kept := make([]databaseRowTreeRow, 0, len(rows))
	for _, row := range rows {
		readable := true
		for id, depth := row.ID, row.Depth; depth > 0 && readable; depth-- {
			_, unreadable := left[id]
			readable, id = !unreadable, parents[id]
		}
		if readable {
			kept = append(kept, row)
		}
	}
	return kept, nil
}
