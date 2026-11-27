package app

import (
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Copy windows write facts in the content transaction, with the same origin
// across retries. The reader admits them only after the durable job succeeds;
// partial copies and a failed job's rollback are not successful user activity.

// A copy holding a database is no line at all in Notion's workspace updates:
// not the copy's making, nor its pages', its databases' or its rows' lines,
// nor its block in the page above — Notion makes such a copy on its server,
// '페이지 복제 중' showing meanwhile (seen 2026-11-17 by workers w1596 and
// w1606 and again by w1613 hours later and in another tab: a page at the top
// with sub-pages and an inline database duplicated from '⋯', a sub-page with
// an inline database from its block's handle and from the sidebar's '⋯' — no
// line for any of them hours after). A copy without one lists its lines as
// any change does: a page at the top's made and edited lines, its
// sub-pages' edited lines, and its block in the page above. The audit log
// keeps every copy's lines.

// A copy at the top lists its made line over its own edit line, as Notion's
// "〈x〉 (1) 페이지를 개인 페이지로 생성했습니다" stands over "〈x〉 (1) 페이지를
// 편집했습니다" (seen 2026-11-17 by worker w1606, and by w1613 in another tab
// later: a page with text alone and one with sub-pages, duplicated from
// '⋯'): it says it was made once the copy is done, after its blocks and its
// sub-pages' lines. A sub-page's copy is an edit of it named, its making's
// line written as it is made (workspace_activity_sub_page.go), under its own
// sub-pages' lines and under the page above's edit, as Notion's (the same
// day: "〈위〉 편집", "〈안〉 편집", "〈x〉 (1) 편집" from the top).

// pageDuplicationTopCopy says whether a copied page is a copy at the top of
// the workspace: the copy's own top, with no page above.
func pageDuplicationTopCopy(job storedPageDuplicationJob, sourcePageID string, target *core.Record) bool {
	return sourcePageID == job.SourceRootPageID && target.GetString("parent_id") == ""
}

// recordPageDuplicationMade records a copied page's making.
func recordPageDuplicationMade(app core.App, job storedPageDuplicationJob, target *core.Record) error {
	return recordWorkspaceActivity(app, target, job.ActorID, "copy:"+job.ID, "page", target.Id, "create",
		workspaceActivityMetadata{Name: target.GetString("title"), LocationID: target.GetString("parent_id"), OperationJobID: job.ID}, time.Now().UTC().UnixMilli())
}

// workspaceActivityQuietCopySQL says whether the job named by job is a copy
// holding a database, whose lines the feed leaves out.
func workspaceActivityQuietCopySQL(job string) string {
	return `EXISTS (SELECT 1 FROM {{leafen_page_duplication_identity_map}}
		WHERE [[job_id]]=` + job + ` AND [[kind]]='data_source')`
}

// pageDuplicationQuietActivity is the activity of a copy's placing in the
// page above: the copy's own operation where the copy holds a database, so
// its edit of that page is no line either (workspaceActivityQuietCopySQL).
func pageDuplicationQuietActivity(app core.App, jobID string) ([]workspaceActivityGraphContext, error) {
	var quiet bool
	if err := app.DB().NewQuery(`SELECT ` + workspaceActivityQuietCopySQL("{:job}")).Bind(dbx.Params{"job": jobID}).Row(&quiet); err != nil {
		return nil, err
	}
	if !quiet {
		return nil, nil
	}
	return []workspaceActivityGraphContext{{OperationJobID: jobID}}, nil
}
func recordWorkspaceDatabaseCopyActivities(app core.App, job storedPageDuplicationJob, pageID string, operations []graphOperation) error {
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return err
	}
	for _, operation := range operations {
		entity, id := "", ""
		metadata := workspaceActivityMetadata{LocationID: pageID, OperationJobID: job.ID}
		subject := page
		switch operation.Kind {
		case graphOperationKindUpsertDataSource:
			if operation.DataSource == nil {
				continue
			}
			entity, id, metadata.Name = "dataSource", operation.DataSource.ID, operation.DataSource.Title
		case graphOperationKindUpsertProperty:
			if operation.Property == nil {
				continue
			}
			p := operation.Property
			entity, id, metadata.Name, metadata.Kind, metadata.SourceID = "property", p.ID, p.Name, p.Type, p.DataSourceID
		case graphOperationKindUpsertView:
			if operation.View == nil {
				continue
			}
			v := operation.View
			entity, id, metadata.Name, metadata.Kind, metadata.SourceID = "view", v.ID, v.Name, v.Type, v.DataSourceID
		case graphOperationKindUpsertRow:
			if operation.Row == nil {
				continue
			}
			entity, id, metadata.Kind, metadata.SourceID = "page", operation.Row.ID, "database_row", operation.Row.DataSourceID
			subject, err = app.FindRecordById(pagesCollection, id)
			if err != nil {
				return err
			}
			// A copy's row values can arrive in later windows. Read its source's
			// title while that source is still fenced by this exact job.
			var sourceID string
			if err = app.DB().NewQuery(`SELECT [[source_id]] FROM {{leafen_page_duplication_identity_map}}
				WHERE [[job_id]]={:job} AND [[kind]]='row' AND [[target_id]]={:id}`).Bind(dbx.Params{"job": job.ID, "id": id}).Row(&sourceID); err != nil {
				return err
			}
			source, findErr := app.FindRecordById(pagesCollection, sourceID)
			if findErr != nil {
				return findErr
			}
			metadata.Name, err = workspaceActivityPageName(app, source)
			if err != nil {
				return err
			}
		default:
			continue
		}
		if err = recordWorkspaceActivity(app, subject, job.ActorID, "copy:"+job.ID, entity, id, "create", metadata, time.Now().UTC().UnixMilli()); err != nil {
			return err
		}
	}
	return nil
}

// recordWorkspaceCopiedBlock records a block a copy put in a page, an edit
// of it. A page's copy lists its sub-pages among its edit's changes, as
// Notion's updates do, never a line of their own (seen 2026-11-17 by worker
// w1625: no "〈sub〉 블록을 생성했습니다" for a copy at the top with
// sub-pages; workspace_activity_copy_changes.go); a copy of blocks into
// another page — a template's body — keeps its sub-pages' block lines
// (subPageLines).
func recordWorkspaceCopiedBlock(app core.App, actor, jobID, pageID, blockID, kind string, at int64, subPageLines bool) error {
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return err
	}
	// A full-page database's copy is its database's lines
	// (workspace_activity_database_block.go).
	if workspaceActivityDatabaseOwnBlock(page, kind) {
		return nil
	}
	metadata := workspaceActivityMetadata{BlockID: blockID, Kind: kind, LocationID: pageID, OperationJobID: jobID}
	// Blocks copied into a page are the person's edit of it, the copy held
	// in it (workspace_activity_copy_merge.go).
	if subPageLines && kind != graphBlockTypeChildPage {
		metadata.OperationJobID, metadata.CopyJobs = "", []string{jobID}
	}
	if kind == graphBlockTypeChildPage && subPageLines {
		metadata.SubjectPageID = blockID
		child, err := app.FindRecordById(pagesCollection, blockID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if child != nil {
			metadata.Name = child.GetString("title")
		}
	}
	return recordWorkspaceActivity(app, page, actor, "copy:"+jobID, "block", blockID, "create", metadata, at)
}

func recordWorkspaceMovedBlock(app core.App, job storedBlockMoveJob, blockID, kind string, at int64) error {
	page, err := app.FindRecordById(pagesCollection, job.DestinationPageID)
	if err != nil {
		return err
	}
	metadata := workspaceActivityMetadata{BlockID: blockID, Kind: kind, LocationID: page.Id, OperationJobID: job.ID}
	if kind == graphBlockTypeChildPage {
		child, err := app.FindRecordById(pagesCollection, blockID)
		if err != nil {
			return err
		}
		metadata.Name, metadata.SubjectPageID = child.GetString("title"), child.Id
	}
	if err = recordWorkspaceActivity(app, page, job.ActorID, "move:"+job.ID, "block", blockID, "move", metadata, at); err != nil {
		return err
	}
	views := []struct {
		ID       string `db:"id"`
		Name     string `db:"name"`
		Kind     string `db:"type"`
		SourceID string `db:"data_source_id"`
	}{}
	if err = app.DB().NewQuery(`SELECT [[id]],[[name]],[[type]],[[data_source_id]] FROM {{leafen_database_views}} WHERE [[page_id]]={:page} AND [[container_id]]={:block}`).Bind(dbx.Params{"page": page.Id, "block": blockID}).All(&views); err != nil {
		return err
	}
	for _, view := range views {
		if err = recordWorkspaceActivity(app, page, job.ActorID, "move:"+job.ID, "view", view.ID, "move", workspaceActivityMetadata{Name: view.Name, Kind: view.Kind, SourceID: view.SourceID, LocationID: page.Id, OperationJobID: job.ID}, at); err != nil {
			return err
		}
	}
	return nil
}
