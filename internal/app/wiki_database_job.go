package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A wiki job changes a wiki whose pages are its database's pages through
// content changes of the wiki's page, a window at a time. Turning a page
// into a wiki first puts the wiki's database on the page, then makes the
// pages under it the database's pages, shallowest first, so that the page
// above each one in the wiki is already the database's. Undoing a wiki, or
// taking back a conversion that failed, makes the database's pages pages
// again, those with no pages under them in the wiki first, then takes the
// database away. The job's fence keeps other changes out of the wiki
// meanwhile.

type wikiConversionItem struct {
	PageID   string `db:"page_id"`
	ParentID string `db:"parent_id"`
}

// wikiJobAuthorization lets a wiki job change the page it fences.
func wikiJobAuthorization(job storedWikiMutation) func(core.App) (pageAuthorization, error) {
	return func(txApp core.App) (pageAuthorization, error) {
		workspace, err := authorizeWorkspace(txApp, job.ActorID, job.WorkspaceID, permissionWorkspaceWrite)
		if err != nil || workspace.OwnerID != job.WorkspaceOwnerID {
			return pageAuthorization{}, refusedUnlessStorage(errWikiConflict, err)
		}
		page, err := txApp.FindRecordById(pagesCollection, job.TargetPageID)
		if err != nil || page.GetString("workspace") != job.WorkspaceID || page.GetString("wiki_active_job_id") != job.ID {
			return pageAuthorization{}, refusedUnlessStorage(errWikiConflict, err)
		}
		return pageAuthorization{Workspace: workspace, Page: page}, nil
	}
}

// advanceWikiDatabaseMutation runs a step of a wiki job that changes a
// wiki's database, and reports whether the step was one.
func (p *pocketBaseYgoPersistence) advanceWikiDatabaseMutation(ctx context.Context, jobID string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return true, err
	}
	job, err := loadWikiMutationByID(p.app, jobID, "")
	if errors.Is(err, sql.ErrNoRows) || (err == nil && job.Status != durableJobStatusQueued) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	switch {
	case job.Operation == wikiOperationConvert && job.Phase == wikiMutationPhaseApplying:
		return true, p.convertWikiDatabaseWindow(ctx, job)
	case job.Operation == wikiOperationConvert && job.Phase == wikiMutationPhaseCleaning,
		job.Operation == wikiOperationUndo &&
			(job.Phase == wikiMutationPhaseApplying || job.Phase == wikiMutationPhaseCleaning):
		wiki, err := loadWikiDatabaseOnPage(p.app, job.WorkspaceOwnerID, job.TargetPageID)
		if err != nil {
			return true, err
		}
		for blockID, sourceID := range wiki.Blocks {
			return true, p.revertWikiDatabaseWindow(ctx, job, sourceID, blockID)
		}
	}
	return false, nil
}

// convertWikiDatabaseWindow takes a conversion one step: the wiki's database,
// then a window of the pages under the wiki's page, then on to publishing.
func (p *pocketBaseYgoPersistence) convertWikiDatabaseWindow(ctx context.Context, job storedWikiMutation) error {
	ids := newWikiDatabaseIDs(job.ID)
	var made int
	if err := p.app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_data_sources}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": ids.Source}).Row(&made); err != nil {
		return err
	}
	if made == 0 {
		if _, err := p.createWikiDatabase(ctx, job.ActorID, job.TargetPageID, job.ID, wikiJobAuthorization(job)); err != nil {
			return err
		}
		return p.app.RunInTransaction(func(txApp core.App) error {
			return rescheduleWikiMutation(txApp, job.ID, job.Phase)
		})
	}
	window, err := loadWikiConversionWindow(p.app, job.ID)
	if err != nil {
		return err
	}
	if len(window) == 0 {
		// The wiki's own page, the job's first item, holds the database.
		return p.app.RunInTransaction(func(txApp core.App) error {
			if err := markWikiMutationItemsApplied(txApp, job.ID, []string{job.TargetPageID}, time.Now().UTC().UnixMilli()); err != nil {
				return err
			}
			if err := updateWikiMutationProgress(txApp, job.ID, 1); err != nil {
				return err
			}
			return advanceWikiMutationPhase(txApp, job, wikiMutationPhasePublishing)
		})
	}
	pageIDs := make([]string, len(window))
	for index, item := range window {
		pageIDs[index] = item.PageID
	}
	digest := sha256.Sum256([]byte("wiki-convert\x00" + job.ID + "\x00" + strings.Join(pageIDs, "\x00")))
	_, err = p.applyContentGraphMutationContext(ctx, job.ActorID, job.TargetPageID,
		graphMutationInput{MutationID: "wiki-convert:" + job.ID + ":" + pageIDs[0]},
		contentGraphMutationHooks{
			Authorize:                wikiJobAuthorization(job),
			SuppressAutomationEvents: true,
			SuppressNotifications:    true,
			PagesMadeRows:            pageIDs,
			ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
				return hex.EncodeToString(digest[:]), nil
			},
			Prepare: func(txApp core.App, authorization pageAuthorization, doc *crdt.Doc) (contentGraphMutationPreparation, error) {
				operations, err := wikiConversionRows(
					txApp, wikiPageMaker(job.ActorID), job.WorkspaceID, job.WorkspaceOwnerID, authorization.Page, ids, window,
				)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				// A wiki's pages are its database's; their page blocks become links to them.
				links, err := childPageBlockWikiLinks(doc, pageIDs)
				return contentGraphMutationPreparation{Operations: append(operations, links...)}, err
			},
			BeforeApplyOperations: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc, _ []graphOperation) error {
				for _, pageID := range pageIDs {
					if err := turnPageIntoWikiRow(txApp, job.WorkspaceID, pageID, ids.Source, job.TargetPageID); err != nil {
						return err
					}
				}
				return nil
			},
			AfterApply: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) error {
				if err := settleWikiRows(ctx, txApp, job.WorkspaceID, job.TargetPageID, pageIDs); err != nil {
					return err
				}
				if err := swapChildPageBlocksForWikiLinks(txApp, job.WorkspaceOwnerID, job.WorkspaceID, job.ActorID, pageIDs); err != nil {
					return err
				}
				if err := markWikiMutationItemsApplied(txApp, job.ID, pageIDs, time.Now().UTC().UnixMilli()); err != nil {
					return err
				}
				if err := updateWikiMutationProgress(txApp, job.ID, len(pageIDs)); err != nil {
					return err
				}
				recheckCollaborationAccessAfterCommit(txApp)
				return rescheduleWikiMutation(txApp, job.ID, job.Phase)
			},
		})
	return err
}

// loadWikiConversionWindow reads the next pages a conversion makes the wiki
// database's, of one depth at a time so that the page above each one is
// already the database's.
func loadWikiConversionWindow(app core.App, jobID string) ([]wikiConversionItem, error) {
	items := []struct {
		wikiConversionItem
		Depth int `db:"depth"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[page_id]], [[parent_id]], [[depth]]
		FROM {{leafen_wiki_mutation_items}} INDEXED BY [[idx_leafen_wiki_mutation_apply]]
		WHERE [[job_id]] = {:jobId} AND [[applied]] = 0 AND [[depth]] > 0
		ORDER BY [[depth]], [[page_id]] LIMIT {:limit}
	`).Bind(dbx.Params{"jobId": jobID, "limit": maxWikiMutationItemsPerTurn}).All(&items); err != nil {
		return nil, err
	}
	window := make([]wikiConversionItem, 0, len(items))
	for _, item := range items {
		if item.Depth != items[0].Depth {
			break
		}
		window = append(window, item.wikiConversionItem)
	}
	return window, nil
}

// wikiConversionRows writes the rows a window of pages becomes: each page's
// title, its owner (ownerOf), and the page above it in the wiki, in the
// order the pages have beside one another. A page that changed since the
// job found it stops the conversion.
func wikiConversionRows(
	app core.App, ownerOf func(page *core.Record) string, workspaceID, contentOwnerID string, wikiPage *core.Record, ids wikiDatabaseIDs,
	window []wikiConversionItem,
) ([]graphOperation, error) {
	pageIDs := make([]string, len(window))
	for index, item := range window {
		pageIDs[index] = item.PageID
	}
	pages, err := loadWikiMutationPageRecords(app, pageIDs)
	if err != nil {
		return nil, err
	}
	operations := make([]graphOperation, 0, len(window))
	for _, item := range window {
		page := pages[item.PageID]
		if page == nil {
			return nil, errWikiConflict
		}
		if page.GetString("workspace") != workspaceID || page.GetString("owner") != contentOwnerID ||
			page.GetString("parent_id") != item.ParentID || page.GetString("data_source_id") != "" ||
			page.GetBool("is_wiki") || page.GetBool("in_trash") || page.GetBool("notion_import_staging") ||
			page.GetString("teamspace_id") != wikiPage.GetString("teamspace_id") ||
			page.GetString(privatePageOwnerField) != wikiPage.GetString(privatePageOwnerField) {
			return nil, errWikiConflict
		}
		title, err := json.Marshal(page.GetString("title"))
		if err != nil {
			return nil, err
		}
		owner, err := json.Marshal([]string{ownerOf(page)})
		if err != nil {
			return nil, err
		}
		values := map[string]graphRowValue{
			ids.Title: {Type: graphValueTypeTitle, Value: title},
			ids.Owner: {Type: graphValueTypePerson, Value: owner},
		}
		row := &graphDatabaseRow{ID: item.PageID, DataSourceID: ids.Source, Position: wikiRowPosition(page), Values: values}
		if item.ParentID != wikiPage.Id {
			parentID := item.ParentID
			row.SubitemParentID = &parentID
		}
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertRow, Row: row})
	}
	return operations, nil
}

// wikiPageMaker owns a page a page's turning into a wiki takes in: whoever
// made the page, as Notion's wiki names each converted page's maker its
// owner, not the person converting it (seen 2026-11-17 at 1416, twice: pages
// an integration made were owned by the integration) — a page whose maker
// is not known, or Leafen made on its own (leafenSystemAuthorID), by the
// person turning it into a wiki: a person value holds people.
func wikiPageMaker(actorID string) func(page *core.Record) string {
	return func(page *core.Record) string {
		if maker := page.GetString("created_by"); maker != "" && maker != leafenSystemAuthorID {
			return maker
		}
		return actorID
	}
}

// settleWikiRows finishes pages that became a wiki database's pages: the
// icon becomes the database page's while the cover stays the page's, their
// sub-pages take their teamspace, and search reads them again.
func settleWikiRows(ctx context.Context, app core.App, workspaceID, wikiPageID string, pageIDs []string) error {
	for _, pageID := range pageIDs {
		page, err := app.FindRecordById(pagesCollection, pageID)
		if err != nil {
			return err
		}
		if err := replaceProductFilePageReferences(app, workspaceID, pageID, "", "", page.GetString("cover")); err != nil {
			return err
		}
		if err := replaceProductFileDatabaseRowPageIconReference(
			app, workspaceID, wikiPageID, pageID, page.GetString("icon"), page.GetString("icon_type"),
		); err != nil {
			return err
		}
		if err := markSearchDirtyByPageID(app, pageID); err != nil {
			return err
		}
	}
	if err := syncDatabaseRowSubPageTeamspaces(app, pageIDs); err != nil {
		return err
	}
	return markDatabaseRowsSearchDirty(ctx, app, workspaceID, pageIDs)
}

// wikiRowPosition orders a wiki's page among the pages beside it in the
// wiki's views as the page tree orders them: by its place under the page
// above it, then its ID. Order keys are compared as text.
func wikiRowPosition(page *core.Record) string {
	place := page.GetFloat("position")
	if math.IsNaN(place) || place < 0 || place >= 1e14 {
		place = 0
	}
	return fmt.Sprintf("w%021.6f~%s", place, page.Id)
}

// turnPageIntoWikiRow makes a page of a wiki its wiki database's page, with
// the public identity, comments and notification settings of one. It keeps
// its place in the body of the page it sits under in the wiki and its place
// among the pages there.
func turnPageIntoWikiRow(app core.App, workspaceID, pageID, sourceID, wikiPageID string) error {
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_pages}} SET [[data_source_id]] = {:sourceId}, [[parent_id]] = {:sourceId}, [[updated]] = {:now}
		WHERE [[id]] = {:id} AND [[data_source_id]] = ''
	`).Bind(dbx.Params{
		"id": pageID, "sourceId": sourceID, "now": pageUpdatedNow(),
	}).Execute()
	if err := requireOneAffected(result, err); err != nil {
		return err
	}
	if err := keepPageRecordPublicIdentity(app, workspaceID, pageID, notionObjectIdentityRowPage); err != nil {
		return err
	}
	return retargetPageRecordKind(app, workspaceID, pageID, wikiPageID, false)
}
