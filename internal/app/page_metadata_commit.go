package app

import (
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type pageMetadataCommitResult struct {
	Page     *core.Record
	Cursor   int
	EditedAt int64
}

// workspaceUserState is the record that counts a workspace owner's page
// changes, made at its first use: an owner who never made a page (a member
// a workspace was handed to) has none yet.
func workspaceUserState(app core.App, ownerID string) (*core.Record, error) {
	state, err := app.FindFirstRecordByData(userStateCollection, "owner", ownerID)
	if !errors.Is(err, sql.ErrNoRows) {
		return state, err
	}
	collection, err := app.FindCollectionByNameOrId(userStateCollection)
	if err != nil {
		return nil, err
	}
	state = core.NewRecord(collection)
	state.Set("owner", ownerID)
	state.Set("cursor", 0)
	// It is saved as it is made, and one another write made meanwhile is
	// taken: a new workspace's People database, made after its request, and
	// the owner's first page found none at once, and the one saved second
	// failed on the owner's unique index (2026-11-18).
	if err := app.Save(state); err != nil {
		if made, findErr := app.FindFirstRecordByData(userStateCollection, "owner", ownerID); findErr == nil {
			return made, nil
		}
		return nil, err
	}
	return state, nil
}

// nextWorkspaceUserStateCursor takes the owner's next page cursor, in one
// statement, so two writes at once — a request's transaction and work after
// another request — never take the same one.
func nextWorkspaceUserStateCursor(app core.App, ownerID string) (int, error) {
	state, err := workspaceUserState(app, ownerID)
	if err != nil {
		return 0, err
	}
	var nextCursor int
	if err := app.DB().NewQuery(`UPDATE {{` + userStateCollection + `}}
		SET [[cursor]] = [[cursor]] + 1, [[updated]] = {:now} WHERE [[id]] = {:id} RETURNING [[cursor]]`).Bind(dbx.Params{
		"id": state.Id, "now": time.Now().UTC().Format("2006-01-02 15:04:05.000Z"),
	}).Row(&nextCursor); err != nil {
		return 0, err
	}
	return nextCursor, nil
}

func commitPageMetadataRecord(
	app core.App,
	authorization pageAuthorization,
	actorID string,
	apply func(*core.Record, int64),
) (pageMetadataCommitResult, error) {
	return commitPageMetadata(app, authorization, actorID, apply, markSearchDirty, false)
}

// commitPageMetadataRecordKept commits a page's fields as
// commitPageMetadataRecord does; kept (pageMetadataKeepsLastEdit), the page
// keeps who edited it last and when, and the change is no version of it.
func commitPageMetadataRecordKept(
	app core.App,
	authorization pageAuthorization,
	actorID string,
	kept bool,
	apply func(*core.Record, int64),
) (pageMetadataCommitResult, error) {
	return commitPageMetadata(app, authorization, actorID, apply, markSearchDirty, kept)
}

// commitPageMetadata commits a page's fields, marking its search entry as
// markSearch does: a graph write's title marks the page's fields alone, its
// blocks marked beside them (content_graph_compound_mutation.go).
func commitPageMetadata(
	app core.App,
	authorization pageAuthorization,
	actorID string,
	apply func(*core.Record, int64),
	markSearch func(core.App, *core.Record) error,
	keepLastEdit bool,
) (pageMetadataCommitResult, error) {
	// The page's history starts from what it was before its first change.
	if err := ensurePageHistoryBaseline(app, authorization.Page.Id); err != nil {
		return pageMetadataCommitResult{}, err
	}
	nextCursor, err := nextWorkspaceUserStateCursor(app, authorization.Workspace.OwnerID)
	if err != nil {
		return pageMetadataCommitResult{}, err
	}
	page := authorization.Page
	activityBefore := captureWorkspaceActivityPage(page)
	fieldsBefore := page.FieldsData()
	lastEditedAt, lastEditedBy := page.GetString("updated"), page.GetString("last_edited_by")
	editedAt := time.Now().UTC().UnixMilli()
	apply(page, editedAt)
	lockOnly := pageMetadataLockOnly(fieldsBefore, page.FieldsData())
	// Into the trash or back is the updates' own line (workspace_activity_page.go),
	// not an edit the page's history keeps, as Notion lists neither as an edit.
	trashOnly := pageMetadataTrashOnly(fieldsBefore, page.FieldsData())
	page.Set("revision", page.GetInt("revision")+1)
	page.Set("cursor", nextCursor)
	if !keepLastEdit {
		setPageLastEditor(page, actorID)
	}
	if err := app.Save(page); err != nil {
		return pageMetadataCommitResult{}, err
	}
	if keepLastEdit {
		if err := restorePageLastEdit(app, page, lastEditedAt, lastEditedBy); err != nil {
			return pageMetadataCommitResult{}, err
		}
	}
	// The sub-pages hanging on its title follow its mentions there
	// (mention_anchored_fields.go).
	if err := settleTitleAnchoredPages(app, actorID, fieldsBefore, page); err != nil {
		return pageMetadataCommitResult{}, err
	}
	// Its parent's line of its title is read again once the index takes the
	// new title (searchEntryRetitled).
	if err := markSearch(app, page); err != nil {
		return pageMetadataCommitResult{}, err
	}
	// The version it records, which its new title's edit keeps
	// (workspace_activity_title_change.go).
	version := 0
	if lockOnly {
		if err := recordPageHistoryPageLock(app, page.Id, actorID); err != nil {
			return pageMetadataCommitResult{}, err
		}
	} else if !trashOnly && !keepLastEdit {
		if err := recordPageHistoryMetadataMutation(app, page.Id, actorID); err != nil {
			return pageMetadataCommitResult{}, err
		}
		if version, err = workspaceActivityTitleVersion(app, page.Id); err != nil {
			return pageMetadataCommitResult{}, err
		}
	}
	if err := recordWorkspacePageMetadata(app, page, activityBefore, actorID, editedAt, version); err != nil {
		return pageMetadataCommitResult{}, err
	}
	return pageMetadataCommitResult{Page: page, Cursor: nextCursor, EditedAt: editedAt}, nil
}

// pageMetadataLockOnly reports whether a change only locked or unlocked the
// page for everyone: a version listed as a lock (page_history_page_lock.go),
// which its updates list as the lock alone (page_lock_records.go).
func pageMetadataLockOnly(before, after map[string]any) bool {
	if fmt.Sprint(before["is_locked"]) == fmt.Sprint(after["is_locked"]) {
		return false
	}
	for name, value := range after {
		if name != "is_locked" && name != "locked_by" && !reflect.DeepEqual(before[name], value) {
			return false
		}
	}
	return true
}

// pageMetadataTrashOnly is a change that only moves the page into the trash
// or out of it, its place staying.
func pageMetadataTrashOnly(before, after map[string]any) bool {
	if fmt.Sprint(before["in_trash"]) == fmt.Sprint(after["in_trash"]) {
		return false
	}
	for name, value := range after {
		if name != "in_trash" && name != "trashed_at" && !reflect.DeepEqual(before[name], value) {
			return false
		}
	}
	return true
}
