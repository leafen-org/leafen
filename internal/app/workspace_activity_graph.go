package app

import (
	"bytes"
	"database/sql"
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

type workspaceActivityGraphContext struct {
	Databases *pageHistoryDatabaseCapture
	Rows      *pageHistoryRowCapture
	// Maintenance is a change no one made — a table's schema kept, Leafen's
	// own upkeep of People (contentGraphMutationHooks.LeafenOwn): kept in
	// the page's history, and no line in the workspace's updates.
	Maintenance    bool
	OperationJobID string
	// LockOnly is a change that only locked or unlocked inline databases:
	// kept in the page's history, listed as no edit (page_lock_records.go).
	LockOnly bool
	// CommentMarks is a change that only marked a value's words with the
	// discussion left on them: kept in the history, and no edit at all
	// (pageHistoryVersionCommentMarks).
	CommentMarks bool
	// BecameDatabase is the change that makes a page a full-page database,
	// once the change before gave it its database
	// (workspace_activity_database_block.go).
	BecameDatabase bool
	// PagesMadeRows are pages the change made its database's pages as they
	// are (contentGraphMutationHooks.PagesMadeRows), whose rows aren't made.
	PagesMadeRows []string
	// WikiHome is the data source of the wiki whose Home the change made
	// (contentGraphMutationHooks.WikiHomeMade).
	WikiHome string
}

func recordWorkspaceGraphActivities(app core.App, page *core.Record, version storedPageHistoryVersion, update []byte, deltas []pageHistoryGraphDelta,
	activity workspaceActivityGraphContext, databases pageHistoryDatabaseChanges, rows []pageHistoryRowChange) error {
	capture, rowCapture, jobID := activity.Databases, activity.Rows, activity.OperationJobID
	if activity.BecameDatabase {
		if err := retractWorkspaceActivityDatabaseStart(app, page, version.ActorID, version.CreatedAt); err != nil {
			return err
		}
	}
	changed := false
	blocks := map[string]pageHistoryProjectionBlock{}
	for _, delta := range deltas {
		blocks[delta.BlockID] = pageHistoryProjectionBlock{}
	}
	previous, err := loadPageHistoryOpenBlockSpans(app, page.Id, blocks)
	if err != nil {
		return err
	}
	origin := "history:" + version.ID
	changes := make([]workspaceBlockActivity, 0, len(deltas))
	wikiLinked := workspaceActivityWikiLinkedPages(deltas)
	for _, delta := range deltas {
		old, existed := previous[delta.BlockID]
		action, kind := "", delta.Type
		switch {
		case delta.Deleted && existed:
			action = "delete"
			kind = old.Type
		case delta.Deleted:
			continue
		case !existed:
			action = "create"
		case old.ParentID != delta.ParentID || delta.Moved:
			action = "move"
		case old.Type != delta.Type || old.Content != string(delta.Content):
			action = "edit"
		}
		if action == "" {
			continue
		}
		changed = true
		// A full-page database's own block is no edit of its page; its
		// database's lines say what changed (workspace_activity_database_block.go).
		if workspaceActivityDatabaseOwnBlock(page, kind) {
			continue
		}
		// A sub-page's block swapped for its wiki's link to it went with no deletion (workspaceActivityWikiLinkedPages).
		if action == "delete" && kind == graphBlockTypeChildPage && wikiLinked[delta.BlockID] {
			continue
		}
		metadata := workspaceActivityMetadata{OperationJobID: jobID, Kind: kind, BlockID: delta.BlockID, LocationID: page.Id}
		if kind == graphBlockTypeChildPage {
			child, err := app.FindRecordById(pagesCollection, delta.BlockID)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			if err != nil {
				return err
			}
			metadata.Name = child.GetString("title")
			metadata.SubjectPageID = child.Id
		}
		// A sub-page made — a block turned into one too — is only an edit of
		// its page, whose changes list it by its name: Notion's updates give
		// its making no line of its own (seen 2026-11-13, made by "/페이지"
		// and named, and made through the API). Its block deleted or moved is
		// a line of its own, named as the sub-page is named now
		// (visibleWorkspaceActivity). Deleted — a sub-page put in the trash —
		// that line is its one: it joins its page's edit, listed under
		// "삭제됨", only where the person's edit of the page is a line
		// already, as Notion's (seen 2026-11-13: trashed alone from its menu,
		// the block's line alone; with other edits, in their line too).
		made := metadata.SubjectPageID != "" && (action == "create" || action == "edit" && old.Type != delta.Type)
		if !made {
			changes = append(changes, workspaceBlockActivity{BlockID: delta.BlockID, Action: action, Metadata: metadata})
		}
		if deleted := metadata.SubjectPageID != "" && action == "delete"; made || deleted {
			edit := workspaceActivityMetadata{OperationJobID: jobID, Kind: kind, BlockID: delta.BlockID, LocationID: page.Id}
			changes = append(changes, workspaceBlockActivity{BlockID: delta.BlockID, Action: action, Metadata: edit, JoinsEdit: deleted})
		}
	}
	if err := recordWorkspaceBlockActivities(app, page, version.ActorID, origin, changes, version.CreatedAt, version.VersionCursor); err != nil {
		return err
	}
	if capture != nil {
		changed = changed || len(databases) > 0
		if err := recordWorkspaceDatabaseActivities(app, page, version, origin, capture.before, databases, capture.activityTrashed, jobID, activity.WikiHome); err != nil {
			return err
		}
	}
	rowChanged, err := recordWorkspaceRowActivities(app, version.ActorID, origin, version.CreatedAt, rowCapture, rows, jobID, activity.PagesMadeRows)
	if err != nil {
		return err
	}
	changed = changed || rowChanged
	// An empty update — no items, no deletions — changed nothing: a page
	// made a full-page database alone.
	if !changed && len(update) > 0 && !bytes.Equal(update, []byte{0, 0}) {
		name, err := workspaceActivityPageName(app, page)
		if err != nil {
			return err
		}
		return recordWorkspaceActivityOfVersion(app, page, version.ActorID, origin, "page", page.Id, "edit", workspaceActivityMetadata{OperationJobID: jobID, Name: name, LocationID: page.GetString("parent_id")}, version.CreatedAt, version.VersionCursor)
	}
	return nil
}
