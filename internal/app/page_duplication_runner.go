package app

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type pageDuplicationChild struct {
	ID string `db:"id"`
}

func advancePageDuplication(ctx context.Context, app core.App, jobID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	job, err := loadPageDuplicationJobByID(app, jobID, "")
	if errors.Is(err, sql.ErrNoRows) ||
		(err == nil && job.Status != durableJobStatusQueued) {
		return nil
	}
	if err != nil {
		return err
	}
	if job.Phase == pageDuplicationPhaseCopying {
		ready, err := pageDuplicationFilesReady(app, job.ID)
		if err != nil {
			return err
		}
		if !ready {
			return advancePageDuplicationFiles(ctx, app, job)
		}
	}
	// The copy's top going first brings its files first, one a turn
	// (page_duplication_top_first.go).
	if job.Phase == pageDuplicationPhaseDiscovering {
		next := func(app core.App) (storedPageDuplicationFile, error) { return nextPageDuplicationTopFile(app, job) }
		if task, err := next(app); err == nil {
			return copyPageDuplicationFile(ctx, app, job, task, pageDuplicationPhaseDiscovering, next)
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	if job.Phase == pageDuplicationPhaseCleaning {
		pagesClean, err := pageDuplicationTargetPagesClean(app, job.ID)
		if err != nil {
			return err
		}
		if pagesClean {
			filesClean, err := pageDuplicationFilesClean(app, job.ID)
			if err != nil {
				return err
			}
			if !filesClean {
				return cleanPageDuplicationFile(ctx, app, job)
			}
		}
	}
	return app.RunInTransaction(func(txApp core.App) error {
		job, err := loadPageDuplicationJobByID(txApp, jobID, "")
		if errors.Is(err, sql.ErrNoRows) ||
			(err == nil && job.Status != durableJobStatusQueued) {
			return nil
		}
		if err != nil {
			return err
		}
		if job.Phase == pageDuplicationPhaseCleaning {
			done, err := cleanPageDuplicationFailureWindow(txApp, job)
			if err != nil {
				return err
			}
			if done {
				filesClean, err := pageDuplicationFilesClean(txApp, job.ID)
				if err != nil {
					return err
				}
				if !filesClean {
					return reschedulePageDuplication(txApp, job)
				}
				return failPageDuplicationInTransaction(txApp, job, job.ErrorCode)
			}
			return reschedulePageDuplication(txApp, job)
		}
		if err := authorizePageDuplicationTarget(
			txApp, job.ActorID, job,
		); err != nil {
			return refusedUnlessStorage(errPageDuplicationConflict, err)
		}
		// A copy into another workspace still reads the page's own, or its site.
		if err := job.sourceReadable(txApp); err != nil {
			return err
		}
		source, err := txApp.FindRecordById(pagesCollection, job.SourceRootPageID)
		if err != nil || source.GetString("duplication_source_job_id") != job.ID {
			if err != nil {
				return err
			}
			return errPageDuplicationConflict
		}

		switch job.Phase {
		case pageDuplicationPhaseDiscovering:
			// As copying below, and for the same waits as the files' turns.
			deadline := time.Now().Add(pageDuplicationTurnBudget)
			for {
				// The copy's top may take its body first (page_duplication_top_first.go).
				worked, err := copyPageDuplicationTopFirstWindow(txApp, job)
				if err != nil {
					return err
				}
				done := false
				if !worked {
					if done, err = discoverPageDuplicationWindow(txApp, job); err != nil {
						return err
					}
				}
				if done {
					// The windows just run counted what can't be copied.
					job, err := loadPageDuplicationJobByID(txApp, jobID, "")
					if err != nil {
						return err
					}
					if job.UnsupportedCount > 0 {
						return failPageDuplicationInTransaction(
							txApp, job, errorCodePageDuplicationUnsupportedContent,
						)
					}
					if err := countPageDuplicationTopFirst(txApp, job); err != nil {
						return err
					}
					return advancePageDuplicationPhase(
						txApp, job, pageDuplicationPhaseCopying,
					)
				}
				if time.Now().After(deadline) {
					break
				}
			}
		case pageDuplicationPhaseCopying:
			// Windows go on in one turn while it stays short: the yield after
			// each window took most of a long page's copy (2026-10-16: 190
			// windows of a 3,000-paragraph page, 56 s, the work itself 6 s).
			deadline := time.Now().Add(pageDuplicationTurnBudget)
			for {
				done, err := copyPageDuplicationWindow(txApp, job)
				if errors.Is(err, errPageDuplicationTurnEnds) {
					break
				}
				if err != nil {
					return err
				}
				// A window that met a workspace emoji to bring along added its
				// picture to the files: the next turn copies it first
				// (page_duplication_custom_emoji.go).
				if ready, err := pageDuplicationFilesReady(txApp, job.ID); err != nil || !ready {
					if err != nil {
						return err
					}
					break
				}
				if done {
					return advancePageDuplicationPhase(
						txApp, job, pageDuplicationPhasePublishing,
					)
				}
				if time.Now().After(deadline) {
					break
				}
			}
		case pageDuplicationPhasePublishing:
			done, err := publishPageDuplicationTargets(txApp, job)
			if err != nil {
				return err
			}
			if done {
				return completePageDuplication(txApp, job)
			}
		case pageDuplicationPhaseCompleted, pageDuplicationPhaseFailed:
			return nil
		default:
			return errPageDuplicationConflict
		}
		return reschedulePageDuplication(txApp, job)
	})
}

// pageDuplicationTurnBudget is how long one turn of a copy may go on with
// further windows before yielding to other work.
const pageDuplicationTurnBudget = 150 * time.Millisecond

func discoverPageDuplicationWindow(
	app core.App,
	job storedPageDuplicationJob,
) (bool, error) {
	item := storedPageDuplicationItem{}
	err := app.DB().NewQuery(`
		SELECT [[job_id]],[[source_page_id]],[[target_page_id]],[[source_parent_id]],
			[[target_parent_id]],[[depth]],[[discovery_cursor]],[[discovered]]
		FROM {{leafen_page_duplication_items}}
		WHERE [[job_id]]={:jobId} AND [[discovered]]=0
		ORDER BY [[depth]],[[source_page_id]] LIMIT 1
	`).Bind(dbx.Params{"jobId": job.ID}).One(&item)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	// A copy leaves out the sub-pages in the trash, as it leaves out rows.
	children := []pageDuplicationChild{}
	// A visitor's copy takes only the pages its site shows.
	publicOnly, publicParams := publicPageDuplicationChildSQL(job)
	publicParams["workspaceId"], publicParams["parentId"] = job.sourceWorkspace(), item.SourcePageID
	publicParams["cursor"], publicParams["limit"] = item.DiscoveryCursor, maxPageDuplicationItemsPerTurn+1
	// A wiki page copied to private pages takes the wiki pages under it too,
	// as sub-pages, as Notion's copy does (seen 2026-10-15): the rows under
	// its row, read along their sub-item index.
	toPrivate, err := pageDuplicationToPrivatePages(app, job.ID)
	if err != nil {
		return false, err
	}
	subitems := ""
	if toPrivate {
		subitems = `
			UNION
			SELECT [[page]].[[id]] FROM {{leafen_database_rows}} AS [[parent]]
			JOIN {{leafen_database_rows}} AS [[row]] INDEXED BY [[idx_leafen_database_rows_subitem_order]]
				ON [[row]].[[owner]] = [[parent]].[[owner]] AND [[row]].[[page_id]] = [[parent]].[[page_id]]
				AND [[row]].[[data_source_id]] = [[parent]].[[data_source_id]] AND [[row]].[[subitem_parent_id]] = [[parent]].[[id]]
			JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[id]] = [[row]].[[id]]
			WHERE [[parent]].[[id]] = {:parentId} AND [[page]].[[workspace]] = {:workspaceId}
				AND [[page]].[[id]] > {:cursor} AND [[page]].[[notion_import_staging]] = 0
				AND [[page]].[[duplication_target_job_id]] = '' AND [[page]].[[in_trash]] = 0`
	}
	pages := `
		SELECT [[id]] FROM {{leafen_pages}} INDEXED BY [[idx_leafen_pages_duplication_children]]
		WHERE [[workspace]]={:workspaceId} AND [[parent_id]]={:parentId}
			AND [[id]]>{:cursor} AND [[notion_import_staging]]=0
			AND [[duplication_target_job_id]]='' AND [[in_trash]]=0` + publicOnly
	if subitems == "" {
		err = app.DB().NewQuery(pages + `
		ORDER BY [[id]] LIMIT {:limit}`).Bind(publicParams).All(&children)
	} else {
		err = app.DB().NewQuery(`SELECT [[id]] FROM (` + pages + subitems + `
		) ORDER BY [[id]] LIMIT {:limit}`).Bind(publicParams).All(&children)
	}
	if err != nil {
		return false, err
	}
	hasMore := len(children) > maxPageDuplicationItemsPerTurn
	if hasMore {
		children = children[:maxPageDuplicationItemsPerTurn]
	}
	now := time.Now().UTC().UnixMilli()
	// A copy takes the sub-pages its maker reads
	// (page_duplication_guest.go); the copies' IDs already chosen (pageDuplicationChosenPages).
	kept, childIDs, err := pageDuplicationReadableChildren(app, job, children)
	if err != nil {
		return false, err
	}
	planned, err := pageDuplicationChosenPages(app, job.ID, childIDs)
	if err != nil {
		return false, err
	}
	for _, child := range kept {
		targetID := newPageID()
		if planned[child.ID] != "" {
			targetID = planned[child.ID]
		}
		// The page the copy goes into is taken as it stands, without what is
		// under it (pageDuplicationStopsAt).
		stops := pageDuplicationStopsAt(job, child.ID)
		_, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_page_duplication_items}}
				([[job_id]],[[source_page_id]],[[target_page_id]],[[source_parent_id]],
				 [[target_parent_id]],[[depth]],[[discovered]],[[blocks_mapped]],[[database_mapped]],
				 [[blocks_copied]],[[created_at]],[[updated_at]])
			VALUES ({:jobId},{:sourcePageId},{:targetPageId},{:sourceParentId},
				{:targetParentId},{:depth},{:stops},{:stops},{:stops},{:stops},{:createdAt},{:updatedAt})
			ON CONFLICT ([[job_id]],[[source_page_id]]) DO NOTHING
		`).Bind(dbx.Params{
			"jobId": job.ID, "sourcePageId": child.ID, "targetPageId": targetID,
			"sourceParentId": item.SourcePageID, "targetParentId": item.TargetPageID,
			"depth": item.Depth + 1, "stops": stops, "createdAt": now, "updatedAt": now,
		}).Execute()
		if err != nil {
			return false, err
		}
		if err := mapPageDuplicationIdentity(
			app, job.ID, "page", child.ID, targetID, child.ID, targetID, now,
		); err != nil {
			return false, err
		}
		if stops {
			continue
		}
		if err := initializePageDuplicationDatabaseStates(
			app, job.ID, job.sourceOwner(), job.sourceWorkspace(), child.ID, targetID, now,
		); err != nil {
			return false, err
		}
	}
	nextCursor := item.DiscoveryCursor
	if len(children) > 0 {
		nextCursor = children[len(children)-1].ID
	}
	unsupported := false
	if !hasMore {
		unsupported, err = pageDuplicationSourceUnsupported(app, job, item.SourcePageID)
		if err != nil {
			return false, err
		}
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_page_duplication_items}}
		SET [[discovery_cursor]]={:cursor}, [[discovered]]={:done}, [[updated_at]]={:updatedAt}
		WHERE [[job_id]]={:jobId} AND [[source_page_id]]={:sourcePageId}
	`).Bind(dbx.Params{
		"jobId": job.ID, "sourcePageId": item.SourcePageID,
		"cursor": nextCursor, "done": !hasMore, "updatedAt": now,
	}).Execute()
	if err != nil {
		return false, err
	}
	return false, updatePageDuplicationProgress(
		app, job.ID, len(kept), !hasMore, unsupported,
	)
}

func updatePageDuplicationProgress(
	app core.App,
	jobID string,
	addedItems int,
	discovered bool,
	unsupported bool,
) error {
	discoveredDelta := 0
	if discovered {
		discoveredDelta = 1
	}
	unsupportedDelta := 0
	if unsupported {
		unsupportedDelta = 1
	}
	if _, err := app.DB().NewQuery(`
		UPDATE {{leafen_jobs}}
		SET [[total_items]]=[[total_items]]+{:addedItems}
		WHERE [[id]]={:jobId} AND [[status]]='queued'
	`).Bind(dbx.Params{"jobId": jobID, "addedItems": addedItems}).Execute(); err != nil {
		return err
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_page_duplication_jobs}}
		SET [[discovered_count]]=[[discovered_count]]+{:discovered},
			[[unsupported_count]]=[[unsupported_count]]+{:unsupported},
			[[updated_at]]={:updatedAt}
		WHERE [[job_id]]={:jobId} AND [[phase]]='discovering'
	`).Bind(dbx.Params{
		"jobId": jobID, "discovered": discoveredDelta, "unsupported": unsupportedDelta,
		"updatedAt": time.Now().UTC().UnixMilli(),
	}).Execute()
	return err
}

func advancePageDuplicationPhase(
	app core.App,
	job storedPageDuplicationJob,
	nextPhase string,
) error {
	now := time.Now().UTC().UnixMilli()
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_page_duplication_jobs}}
		SET [[phase]]={:phase}, [[revision]]=[[revision]]+1, [[updated_at]]={:updatedAt}
		WHERE [[job_id]]={:jobId} AND [[phase]]={:currentPhase}
	`).Bind(dbx.Params{
		"jobId": job.ID, "phase": nextPhase,
		"currentPhase": job.Phase, "updatedAt": now,
	}).Execute()
	if err != nil {
		return err
	}
	return reschedulePageDuplicationAt(app, job.ID, nextPhase, now)
}

func reschedulePageDuplication(app core.App, job storedPageDuplicationJob) error {
	return reschedulePageDuplicationAt(
		app, job.ID, job.Phase, time.Now().UTC().UnixMilli(),
	)
}

func reschedulePageDuplicationAt(
	app core.App,
	jobID, cursor string,
	now int64,
) error {
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_jobs}}
		SET [[cursor]]={:cursor}, [[not_before]]={:notBefore},
			[[chunk_count]]=[[chunk_count]]+1, [[updated]]={:updatedAt}
		WHERE [[id]]={:jobId} AND [[status]]='queued'
	`).Bind(dbx.Params{
		"jobId": jobID, "cursor": cursor,
		"notBefore": now + durableJobReadyDelay.Milliseconds(), "updatedAt": now,
	}).Execute()
	return err
}
