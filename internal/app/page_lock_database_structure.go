package app

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// A page's lock stops what is on the page: its blocks, and in the app its
// databases' cells, new rows and views where the page shows them. It leaves
// the databases on it — their properties, views, sources and pages' layout —
// to their own lock alone, an inline database's block's "locked"
// (database_container_lock.go), as Notion's (seen 2026-11-16 in Chrome, two
// tabs each: a locked page's inline database's row page made a property
// with "속성 추가", and its "…" › 테마와 레이아웃 offered 목차 and 레이아웃
// 사용자 지정, while the table's cells took no typing; the same database
// opened alone (⤢) took a typed cell and title, its bar without "잠금"). A
// full-page database's or a wiki's page lock is its database's own, which
// stops both.

// pageLockHoldsDatabaseStructure reports whether a page's lock is the lock
// of the database it is: a full-page database's page, or a wiki's.
func pageLockHoldsDatabaseStructure(page *core.Record) bool {
	return isFullPageDatabase(page) || page != nil && page.GetBool("is_wiki")
}

// pageDatabaseStructureLockedFor reports whether the page's lock stops this
// person's changes to its databases' properties, views and page layout:
// only a full-page database's or a wiki's page lock does, unless they
// unlocked it for themselves.
func pageDatabaseStructureLockedFor(app core.App, page *core.Record, principalID string) (bool, error) {
	if !pageLockHoldsDatabaseStructure(page) {
		return false, nil
	}
	return pageLockedFor(app, page, principalID)
}

// graphOperationsChangePageLockedContent reports whether a content change of
// the page reaches what its lock protects. A full-page database's or a
// wiki's lock protects its database (graphOperationsChangeLockedContent); another
// page's lock protects its blocks, and leaves its databases' sources, views,
// properties and rows to theirs.
func graphOperationsChangePageLockedContent(page *core.Record, operations []graphOperation) bool {
	if pageLockHoldsDatabaseStructure(page) {
		return graphOperationsChangeLockedContent(operations)
	}
	for _, operation := range operations {
		switch operation.Kind {
		case graphOperationKindUpsertBlock, graphOperationKindMoveBlock, graphOperationKindDeleteBlock:
			return true
		}
	}
	return false
}

// databaseRowPageHolderLockedFor reports whether the page whose body holds
// a row's inline database is locked for the reader: the database's
// templates stay as they are there, as its views do, while its properties
// and page layout follow its own lock.
func databaseRowPageHolderLockedFor(app core.App, principalID string, authority databaseRowPageAuthority) (bool, error) {
	container, err := findRecordRemembered(app, pagesCollection, authority.ContainerPageID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil || pageLockHoldsDatabaseStructure(container) {
		return false, err
	}
	return pageLockedFor(app, container, principalID)
}

// refusePageLockedContent refuses a change of the page that reaches what
// its lock protects from this person (graphOperationsChangePageLockedContent),
// all but an inline database's own lock turned on a locked page: its "잠금"
// there unlocks it for everyone, and the page stays locked, as Notion's
// (seen 2026-11-16 in Chrome, two tabs: a locked page's locked inline
// database's "잠금" offered "잠금 해제" and "모든 사용자에 대해 잠금 해제",
// both of which worked). A full-page database's or a wiki's lock is its
// database's: a change of its properties, views or sources it refuses as
// the database's lock (lock_refusal.go), one of its blocks as the page's.
func refusePageLockedContent(app core.App, ownerID string, page *core.Record, principalID string, operations []graphOperation) error {
	if !graphOperationsChangePageLockedContent(page, operations) {
		return nil
	}
	locked, err := pageLockedFor(app, page, principalID)
	if err != nil || !locked {
		return err
	}
	if pageLockHoldsDatabaseStructure(page) {
		if graphOperationsChangeDatabaseStructure(operations) {
			return databaseLockedRefusal(app, principalID, page.Id)
		}
		return pageLockedError()
	}
	rest := make([]graphOperation, 0, len(operations))
	for _, operation := range operations {
		alone, err := inlineDatabaseLockTurnedAlone(app, ownerID, page.Id, operation)
		if err != nil {
			return err
		}
		if !alone {
			rest = append(rest, operation)
		}
	}
	if !graphOperationsChangePageLockedContent(page, rest) {
		return nil
	}
	return pageLockedError()
}

// inlineDatabaseLockTurnedAlone reports whether an operation locks or
// unlocks an inline database of the page and changes nothing else of it
// (page_lock_records.go).
func inlineDatabaseLockTurnedAlone(app core.App, ownerID, pageID string, operation graphOperation) (bool, error) {
	block := operation.Block
	if operation.Kind != graphOperationKindUpsertBlock || block == nil || block.Type != graphBlockTypeInlineDatabase {
		return false, nil
	}
	stored, found, err := loadStoredDatabaseBlock(app, ownerID, pageID, block.ID)
	if err != nil || !found || stored.Type != graphBlockTypeInlineDatabase {
		return false, err
	}
	turned := databaseBlockContentLocked([]byte(stored.Content)) != databaseBlockContentLocked(block.Content)
	return turned && databaseBlockLockAlone(stored, block), nil
}

// graphOperationsChangeDatabaseStructure reports whether a change reaches
// a database's own parts — its sources, views, properties and dependency
// settings — rather than blocks or rows.
func graphOperationsChangeDatabaseStructure(operations []graphOperation) bool {
	for _, operation := range operations {
		switch operation.Kind {
		case graphOperationKindUpsertBlock, graphOperationKindMoveBlock, graphOperationKindDeleteBlock,
			graphOperationKindUpsertRow, graphOperationKindDeleteRow, graphOperationKindRestoreDateCommand,
			graphOperationKindUpsertDependencyEdge, graphOperationKindDeleteDependencyEdge:
			continue
		default:
			return true
		}
	}
	return false
}
