package app

import "github.com/pocketbase/pocketbase/core"

// pageDuplicationSurface names who asks for a copy. Notion's app keeps a
// locked page from changing, while its API and MCP change pages regardless
// of a lock, so only the app is refused a copy placed under a locked page.
type pageDuplicationSurface uint8

const (
	pageDuplicationFromApp pageDuplicationSurface = iota
	pageDuplicationFromIntegration
)

// pageDuplicationPlacementFailure is the one rule, for every surface that
// duplicates a page, for what may be copied and where the copy may go. A page
// in the trash, or under a page in the trash, is not copied, and a copy is not
// put in the trash, as no page is made there (page_creation_parent_trash.go):
// both are told as the trash (errPageDuplicationInTrash). A copy is not put
// inside the page it copies, or, from the app, under another locked page,
// which fails as its own error. A locked page may be copied, and
// a page in a locked page copied beside itself, as Notion's (seen
// 2026-10-31); the copy keeps the lock (createPageDuplicationTarget). A sub-page a
// copied body carries (inPlace) may go inside itself: a page's body copied
// to a page under it, or to itself, as Notion's template copy does (seen
// 2026-11-14) — the copy stops at its destination (pageDuplicationStopsAt).
func pageDuplicationPlacementFailure(
	app core.App,
	surface pageDuplicationSurface,
	sourcePageID string,
	destination *core.Record,
	inPlace bool,
) error {
	trashed, err := pageInTrashPath(app, sourcePageID)
	if err != nil || trashed || destination == nil {
		return pageDuplicationInTrashOr(trashed, err)
	}
	if surface == pageDuplicationFromApp && destination.GetBool("is_locked") {
		source, err := app.FindRecordById(pagesCollection, sourcePageID)
		if err != nil {
			return err
		}
		if source.GetString("parent_id") != destination.Id {
			return errPageDuplicationDestinationLocked
		}
	}
	inside, err := pageParentCreatesCycle(app, sourcePageID, destination.Id)
	if err != nil || inside && !inPlace {
		return pageDuplicationConflictOr(inside, err)
	}
	return pageDuplicationInTrashOr(pageInTrashPath(app, destination.Id))
}

func pageDuplicationInTrashOr(trashed bool, err error) error {
	if err == nil && trashed {
		return errPageDuplicationInTrash
	}
	return err
}

func pageDuplicationConflictOr(conflict bool, err error) error {
	if err != nil {
		return err
	}
	if conflict {
		return errPageDuplicationConflict
	}
	return nil
}

// pageDuplicationStopsAt reports whether a copy takes a page as it stands,
// without its body and the pages under it: the page the copy goes into,
// which a copy carried by a copied body meets inside what it copies. Notion's
// copy of a page made under the page it copies holds that new page empty,
// with its name and icon (seen 2026-11-14), as the copy found it.
func pageDuplicationStopsAt(job storedPageDuplicationJob, sourcePageID string) bool {
	return job.DestinationParentID != "" && sourcePageID == job.DestinationParentID
}
