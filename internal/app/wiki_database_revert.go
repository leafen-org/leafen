package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// Undoing a wiki whose pages are its database's pages, or taking back a
// conversion that stopped, makes the database's pages pages again, those with
// no pages under them in the wiki first, and then takes the database away.

// revertWikiDatabaseWindow makes a window of the wiki database's pages pages
// again, those with no pages under them in the wiki first. Once none is left
// it takes the database away; pages being deleted for good leave it first.
func (p *pocketBaseYgoPersistence) revertWikiDatabaseWindow(
	ctx context.Context, job storedWikiMutation, sourceID, blockID string,
) error {
	// The window reads the source's rows in their order from the row index,
	// stopping at its last, and looks up each one's sub-items there too.
	rows := []string{}
	if err := p.app.DB().NewQuery(`
		SELECT [[row]].[[id]] FROM {{leafen_database_rows}} AS [[row]]
		WHERE ` + databaseSourceRowsSQL("[[row]]", "{:sourceId}") + `
			AND NOT EXISTS (SELECT 1 FROM {{leafen_database_rows}} AS [[child]]
				WHERE [[child]].[[owner]] = [[row]].[[owner]] AND [[child]].[[page_id]] = [[row]].[[page_id]]
					AND [[child]].[[data_source_id]] = [[row]].[[data_source_id]] AND [[child]].[[subitem_parent_id]] = [[row]].[[id]])
			AND NOT EXISTS (SELECT 1 FROM {{leafen_database_row_trash}} AS [[deleting]]
				WHERE [[deleting]].[[row_id]] = [[row]].[[id]] AND [[deleting]].[[deleting]] = 1)
		ORDER BY [[row]].[[position]], [[row]].[[id]] LIMIT {:limit}
	`).Bind(dbx.Params{"sourceId": sourceID, "limit": maxWikiMutationItemsPerTurn}).Column(&rows); err != nil {
		return err
	}
	if len(rows) == 0 {
		var remaining int
		if err := p.app.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_database_rows}} AS [[row]]
			WHERE ` + databaseSourceRowsSQL("[[row]]", "{:sourceId}")).
			Bind(dbx.Params{"sourceId": sourceID}).Row(&remaining); err != nil {
			return err
		}
		if remaining > 0 {
			return p.app.RunInTransaction(func(txApp core.App) error {
				return rescheduleWikiMutation(txApp, job.ID, job.Phase)
			})
		}
		return p.removeWikiDatabase(ctx, job, sourceID, blockID)
	}
	digest := sha256.Sum256([]byte("wiki-revert\x00" + job.ID + "\x00" + strings.Join(rows, "\x00")))
	authorities := []databaseRowPageAuthority{}
	pairWork := []databaseRelationPairWork{}
	_, err := p.applyContentGraphMutationContext(ctx, job.ActorID, job.TargetPageID,
		graphMutationInput{MutationID: "wiki-revert:" + job.ID + ":" + rows[0]},
		contentGraphMutationHooks{
			Authorize:                wikiJobAuthorization(job),
			SuppressAutomationEvents: true,
			SuppressNotifications:    true,
			ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
				return hex.EncodeToString(digest[:]), nil
			},
			Prepare: func(txApp core.App, authorization pageAuthorization, doc *crdt.Doc) (contentGraphMutationPreparation, error) {
				authorities, pairWork = authorities[:0], pairWork[:0]
				operations := []graphOperation{}
				for _, rowID := range rows {
					authority, err := loadDatabaseRowPageAuthority(txApp, rowID)
					if err != nil {
						return contentGraphMutationPreparation{}, err
					}
					if authority.DataSourceID != sourceID || authority.ContainerPageID != job.TargetPageID {
						return contentGraphMutationPreparation{}, errWikiConflict
					}
					leave, err := databaseRowLeaveOperations(txApp, authority)
					if err != nil {
						return contentGraphMutationPreparation{}, err
					}
					// A window that would grow too large, or change a row twice —
					// the page two of its pages sit under — leaves the rest for
					// the next.
					if len(authorities) > 0 &&
						(len(operations)+len(leave) > maxGraphMutationOperations || changesRowAgain(operations, leave)) {
						break
					}
					work, err := planDatabaseRelationPairDeletion(txApp, authority.OwnerID, databaseMutationTargetRow, rowID)
					if err != nil {
						return contentGraphMutationPreparation{}, err
					}
					operations = append(operations, leave...)
					authorities = append(authorities, authority)
					pairWork = append(pairWork, work...)
				}
				// The wiki's page's links to them turn back into their blocks where they stand.
				leaving := make([]string, len(authorities))
				for index, authority := range authorities {
					leaving[index] = authority.RowID
				}
				returns, err := wikiPageLinkReturns(txApp, job.WorkspaceOwnerID, doc, leaving)
				return contentGraphMutationPreparation{Operations: append(operations, returns...)}, err
			},
			BeforeApplyOperations: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc, _ []graphOperation) error {
				for _, authority := range authorities {
					if err := turnWikiRowIntoPage(txApp, authority, job.TargetPageID); err != nil {
						return err
					}
				}
				return nil
			},
			AfterApply: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) error {
				for _, authority := range authorities {
					page, err := txApp.FindRecordById(pagesCollection, authority.RowID)
					if err != nil {
						return err
					}
					if err := replaceProductFilePageReferences(
						txApp, authority.WorkspaceID, page.Id, page.GetString("icon"), page.GetString("icon_type"), page.GetString("cover"),
					); err != nil {
						return err
					}
					// An undone wiki's pages are no longer verified.
					if err := cancelWikiVerificationGeneration(txApp, page); err != nil {
						return err
					}
					if err := markSearchDirtyByPageID(txApp, page.Id); err != nil {
						return err
					}
				}
				if err := admitDatabaseRelationPairWork(
					txApp, job.WorkspaceOwnerID, job.WorkspaceID, job.ActorID, "wiki-revert:"+job.ID+":"+rows[0], pairWork,
				); err != nil {
					return err
				}
				// Pages back from the wiki show in their parents' bodies, where
				// their links stood, and their sub-pages in theirs, once each
				// parent is a page again.
				returned := make([]string, len(authorities))
				for index, authority := range authorities {
					returned[index] = authority.RowID
				}
				if err := returnWikiPageLinks(txApp, job.WorkspaceOwnerID, job.WorkspaceID, job.ActorID, job.TargetPageID, returned); err != nil {
					return err
				}
				filled := []string{job.TargetPageID}
				for _, authority := range authorities {
					filled = append(filled, authority.RowID)
				}
				if err := requestChildPageBlockBackfill(txApp, job.WorkspaceID, filled...); err != nil {
					return err
				}
				if err := updateWikiMutationProgress(txApp, job.ID, len(authorities)); err != nil {
					return err
				}
				recheckCollaborationAccessAfterCommit(txApp)
				return rescheduleWikiMutation(txApp, job.ID, job.Phase)
			},
		})
	return err
}

// turnWikiRowIntoPage makes a wiki database's page a page under the page it
// sat under in the wiki, keeping its place there, its archive and the
// comments and notification settings of a page. A page trashed in the wiki
// stays in the trash: the one the person trashed as a trashed page, the
// pages trashed with it under it.
func turnWikiRowIntoPage(app core.App, authority databaseRowPageAuthority, wikiPageID string) error {
	parentID := authority.SubitemParentID
	if parentID == "" {
		parentID = wikiPageID
	}
	inTrash, trashedAt := 0, ""
	if authority.TrashedAt != 0 && authority.TrashRootID == authority.RowID {
		inTrash, trashedAt = 1, time.UnixMilli(authority.TrashedAt).UTC().Format(time.RFC3339Nano)
	}
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_pages}}
		SET [[data_source_id]] = '', [[parent_id]] = {:parentId}, [[in_trash]] = {:inTrash},
			[[trashed_at]] = {:trashedAt}, [[archived_at]] = {:archivedAt}, [[archived_by]] = {:archivedBy},
			[[updated]] = {:now}
		WHERE [[id]] = {:id} AND [[data_source_id]] = {:sourceId}
	`).Bind(dbx.Params{
		"id": authority.RowID, "sourceId": authority.DataSourceID, "parentId": parentID,
		"inTrash": inTrash, "trashedAt": trashedAt,
		"archivedAt": authority.ArchivedAt, "archivedBy": authority.ArchivedBy,
		"now": pageUpdatedNow(),
	}).Execute()
	if err := requireOneAffected(result, err); err != nil {
		return err
	}
	if err := keepPageRecordPublicIdentity(app, authority.WorkspaceID, authority.RowID, "page"); err != nil {
		return err
	}
	return retargetPageRecordKind(app, authority.WorkspaceID, authority.RowID, authority.RowID, true)
}

// removeWikiDatabase takes an undone wiki's database, with no pages left, off
// the wiki's page: its block goes to the trash of deleted blocks as a
// deleted database's does, no longer the database of a wiki.
func (p *pocketBaseYgoPersistence) removeWikiDatabase(
	ctx context.Context, job storedWikiMutation, sourceID, blockID string,
) error {
	digest := sha256.Sum256([]byte("wiki-remove\x00" + job.ID + "\x00" + blockID))
	_, err := p.applyContentGraphMutationContext(ctx, job.ActorID, job.TargetPageID,
		graphMutationInput{MutationID: "wiki-remove:" + job.ID},
		contentGraphMutationHooks{
			Authorize:  wikiJobAuthorization(job),
			RemoveWiki: true,
			ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
				return hex.EncodeToString(digest[:]), nil
			},
			Prepare: func(core.App, pageAuthorization, *crdt.Doc) (contentGraphMutationPreparation, error) {
				return contentGraphMutationPreparation{Operations: []graphOperation{{
					Kind: graphOperationKindDeleteBlock, Deletion: &graphBlockDelete{BlockID: blockID},
				}}}, nil
			},
			BeforeApplyOperations: func(
				txApp core.App, authorization pageAuthorization, doc *crdt.Doc, operations []graphOperation,
			) error {
				if _, err := txApp.DB().NewQuery(`
					UPDATE {{leafen_data_sources}}
					SET [[wiki]] = 0, [[wiki_owner_property_id]] = '', [[wiki_verification_property_id]] = ''
					WHERE [[id]] = {:id}
				`).Bind(dbx.Params{"id": sourceID}).Execute(); err != nil {
					return err
				}
				return captureRecoverableBlockTrash(blockID, job.ActorID)(txApp, authorization, doc, operations)
			},
			AfterApply: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) error {
				// The page is no wiki's now, so its body takes the blocks of
				// sub-pages it lacks, which it couldn't while it held the wiki.
				if err := requestChildPageBlockBackfill(txApp, job.WorkspaceID, job.TargetPageID); err != nil {
					return err
				}
				return rescheduleWikiMutation(txApp, job.ID, job.Phase)
			},
		})
	return err
}

// changesRowAgain reports whether more operations save or delete a row that
// operations already do, which one change can't hold.
func changesRowAgain(operations, more []graphOperation) bool {
	rowOf := func(operation graphOperation) string {
		switch {
		case operation.Kind == graphOperationKindUpsertRow && operation.Row != nil:
			return operation.Row.ID
		case operation.Kind == graphOperationKindDeleteRow && operation.RowDeletion != nil:
			return operation.RowDeletion.ID
		}
		return ""
	}
	changed := map[string]bool{}
	for _, operation := range operations {
		if id := rowOf(operation); id != "" {
			changed[id] = true
		}
	}
	for _, operation := range more {
		if id := rowOf(operation); id != "" && changed[id] {
			return true
		}
	}
	return false
}
