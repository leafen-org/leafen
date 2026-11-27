package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A read decides who may see what the same way many times over: a page's
// content graph asks whether the reader may read it, edit it and suggest
// in it, each a whole permission check, and a search asks for every page it
// found, under one workspace and a few teamspaces (2026-10-15 load
// measurement: with 3,000 people, permission checks were most of the
// server's time). Within a read scope the answers are kept and asked once:
// a read snapshot sees the database at one moment, and a search's batch is
// one moment's read. Writes decide afresh: no memo outlives a scope, and a
// write never runs in one.

type accessMemoHolder interface {
	accessMemo() map[any]accessMemoEntry
}

type accessMemoEntry struct {
	value any
	err   error
}

// accessMemoApp is a read scope's app that keeps its permission decisions.
type accessMemoApp struct {
	core.App
	memo map[any]accessMemoEntry
}

func (app *accessMemoApp) accessMemo() map[any]accessMemoEntry { return app.memo }

func (app *readSnapshotApp) accessMemo() map[any]accessMemoEntry {
	if app.memo == nil {
		app.memo = map[any]accessMemoEntry{}
	}
	return app.memo
}

// withAccessMemo is the app for a read scope keeping its permission
// decisions; one that keeps them already is itself.
func withAccessMemo(app core.App) core.App {
	if _, ok := app.(accessMemoHolder); ok {
		return app
	}
	return &accessMemoApp{App: app, memo: map[any]accessMemoEntry{}}
}

// remembered answers what compute does, once a read scope: a decision, or
// a denial, is kept; any other error is not.
func remembered[V any](app core.App, key any, compute func() (V, error)) (V, error) {
	holder, ok := app.(accessMemoHolder)
	if !ok {
		return compute()
	}
	memo := holder.accessMemo()
	if entry, found := memo[key]; found {
		value, _ := entry.value.(V)
		return value, entry.err
	}
	value, err := compute()
	if err == nil || isPermissionDenied(err) {
		memo[key] = accessMemoEntry{value: value, err: err}
	}
	return value, err
}

type pageAccessMemoKey struct {
	principalID, pageID, ownedCopyJobID string
	minimum                             pageAccessRole
}

type workspaceAccessMemoKey struct {
	principalID, workspaceID, allowedDeletionJobID string
	requirement                                    permissionRequirement
}

type teamspaceAccessMemoKey struct {
	principalID, workspaceID, teamspaceID string
	requireOwner, invited                 bool
}

type teamspaceAccessMemoValue struct {
	authority teamspaceAuthority
	teamspace storedTeamspace
	decision  teamspaceAccessDecisionWire
}

// pageFences says which fences on a page or one above a workspace has under
// way: blocks being moved, a page being permanently deleted, a wiki's
// change, a page being copied. A page's fences are its own and its
// ancestors', in its workspace (a move's pages are both its workspace's),
// so where the workspace has none no page needs to look (2026-10-15: these
// checks were a quarter of each permission check). A finished move whose
// copies of sub-pages still run counts while any such copy runs.
type pageFences struct {
	Moving      bool `db:"moving"`
	Deleting    bool `db:"deleting"`
	Wiki        bool `db:"wiki"`
	Duplicating bool `db:"duplicating"`
	// WorkspaceMoving is a page move out of or into the workspace under way
	// (page_workspace_move.go).
	WorkspaceMoving bool `db:"workspace_moving"`
}

type pageFencesMemoKey struct{ workspaceID string }

func workspacePageFences(app core.App, workspaceID string) (pageFences, error) {
	return remembered(app, pageFencesMemoKey{workspaceID: workspaceID}, func() (pageFences, error) {
		fences := pageFences{}
		err := app.DB().NewQuery(`
			SELECT
				EXISTS(SELECT 1 FROM {{leafen_block_move_jobs}} WHERE [[workspace_id]] = {:workspaceId} AND [[phase]] NOT IN ('completed','failed'))
					OR EXISTS(SELECT 1 FROM {{leafen_page_duplication_jobs}} WHERE [[parent_block_copy_job_id]] != '' AND [[phase]] NOT IN ('completed','failed')) AS [[moving]],
				EXISTS(SELECT 1 FROM {{leafen_pages}} WHERE [[deletion_job_id]] != '' AND [[workspace]] = {:workspaceId}) AS [[deleting]],
				EXISTS(SELECT 1 FROM {{leafen_pages}} WHERE [[wiki_active_job_id]] != '' AND [[workspace]] = {:workspaceId}) AS [[wiki]],
				EXISTS(SELECT 1 FROM {{leafen_pages}} WHERE [[duplication_source_job_id]] != '' AND [[workspace]] = {:workspaceId}) AS [[duplicating]],
				EXISTS(SELECT 1 FROM {{leafen_page_workspace_moves}} INDEXED BY [[idx_leafen_page_workspace_moves_source]]
					WHERE [[source_workspace_id]] = {:workspaceId} AND [[phase]] IN ('moving','rolling_back'))
					OR EXISTS(SELECT 1 FROM {{leafen_page_workspace_moves}} INDEXED BY [[idx_leafen_page_workspace_moves_target]]
					WHERE [[target_workspace_id]] = {:workspaceId} AND [[phase]] IN ('moving','rolling_back')) AS [[workspace_moving]]
		`).Bind(dbx.Params{"workspaceId": workspaceID}).One(&fences)
		return fences, err
	})
}

type recordMemoKey struct{ collection, id string }

// findRecordRemembered is a record by its ID, read once a read scope: a
// page's and its workspace's records were most of what a permission check
// read (2026-10-15).
func findRecordRemembered(app core.App, collection, id string) (*core.Record, error) {
	return remembered(app, recordMemoKey{collection: collection, id: id}, func() (*core.Record, error) {
		return app.FindRecordById(collection, id)
	})
}

// primeRecordsRemembered reads records by their IDs at once into a read
// scope's memo, where findRecordRemembered then finds them: a search judged
// each page it found by reading its record alone (2026-10-17: 138 page reads
// in one search). Outside a read scope it reads nothing.
func primeRecordsRemembered(app core.App, collection string, ids []string) error {
	holder, ok := app.(accessMemoHolder)
	if !ok {
		return nil
	}
	memo := holder.accessMemo()
	wanted := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, known := memo[recordMemoKey{collection: collection, id: id}]; !known && id != "" {
			wanted = append(wanted, id)
		}
	}
	for start := 0; start < len(wanted); start += 500 {
		records, err := app.FindRecordsByIds(collection, wanted[start:min(start+500, len(wanted))])
		if err != nil {
			return err
		}
		for _, record := range records {
			memo[recordMemoKey{collection: collection, id: record.Id}] = accessMemoEntry{value: record}
		}
	}
	return nil
}

type teamspaceRolesMemoKey struct{ workspaceID, teamspaceID string }
