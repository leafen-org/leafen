package app

import (
	"context"
	"encoding/json"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// A database page's history is a page's: its body's changes, and its
// property values with its page state, each change of them a version.

// beginDatabaseRowValueHistory reports whether an upsert changes an existing
// database page's values, first keeping the page's history from before it.
func beginDatabaseRowValueHistory(app core.App, row *graphDatabaseRow, knownNew bool) (bool, error) {
	if knownNew || len(row.Values) == 0 {
		return false, nil
	}
	var pages int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_pages}} WHERE [[id]]={:rowId} AND [[data_source_id]]!=''
	`).Bind(dbx.Params{"rowId": row.ID}).Row(&pages); err != nil || pages == 0 {
		return false, err
	}
	changed, err := graphRowValuesChange(app, row)
	if err != nil || !changed {
		return false, err
	}
	return true, ensurePageHistoryBaseline(app, row.ID)
}

// pageHistoryVersionRowMade is the version where a page became a database's
// page — made in one, as a wiki's sidebar "+" makes its pages, or turned into
// one, as a wiki takes in the pages under it — with the values it got then
// (a wiki's owner among them): its making, which Notion's updates list as no
// change of its values (seen 2026-11-17 at 1416, two tabs: a wiki's new page
// lists its title alone, a page the wiki took in no line of the conversion).
// Its event is the page's state, replayed as any other; it makes no line in
// the workspace's updates, and a person's edit of the page's values is read
// from it on (workspace_activity_properties.go, the web's pageUpdates.ts).
const pageHistoryVersionRowMade = "row_made"

// finishDatabaseRowValueHistory records a database page's changed values as
// a version of it, by versionsBy when one is named (Leafen's own upkeep,
// content_graph_mutation_leafen_own.go) and else its writer; values whose
// words a discussion only marked, as no edit; a page's first values, as its
// making (pageHistoryVersionRowMade).
func finishDatabaseRowValueHistory(app core.App, rowID, actorID, versionsBy string, changed, commentMarks, made bool) error {
	if !changed {
		return nil
	}
	if versionsBy != "" {
		actorID = versionsBy
	}
	if made {
		return recordPageHistoryRowMadeState(app, rowID, actorID)
	}
	if commentMarks {
		return recordPageHistoryCommentMarksState(app, rowID, actorID)
	}
	return recordPageHistoryMetadataMutation(app, rowID, actorID)
}

// recordPageHistoryRowMadeState records the page that just became a
// database's page, with its values, as its making (pageHistoryVersionRowMade).
func recordPageHistoryRowMadeState(app core.App, rowID, actorID string) error {
	page, err := app.FindRecordById(pagesCollection, rowID)
	if err != nil {
		return err
	}
	state, err := pageHistoryPageStateFor(app, page)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, _, err = recordPageHistoryEventKind(app, rowID, actorID, pageHistoryVersionRowMade, pageHistoryEventMetadata, payload, true)
	return err
}

// restoreDatabaseRowPageHistoryState restores a database page's icon and
// cover to a version's; its title is one of its property values.
func restoreDatabaseRowPageHistoryState(app core.App, rowID, actorID string, state pageHistoryPageState) error {
	authority, err := loadDatabaseRowPageAuthority(app, rowID)
	if err != nil {
		return err
	}
	iconType := state.IconType
	if iconType == "" {
		iconType = "none"
	}
	if state.Icon != authority.Icon || iconType != authority.IconType {
		if err := updateDatabaseRowPagePresentation(
			app, rowID, authority.WorkspaceID, authority.ContainerPageID, actorID, state.Icon, iconType,
		); err != nil {
			return err
		}
	}
	if state.Cover != authority.Cover || state.CoverPosition != authority.CoverPosition {
		position := state.CoverPosition
		if _, err := setRowPageRecordCover(app, rowID, authority.WorkspaceID, actorID, state.Cover, &position); err != nil {
			return err
		}
		if err := replaceProductFilePageReferences(app, authority.WorkspaceID, rowID, "", "none", state.Cover); err != nil {
			return err
		}
	}
	if err := touchDatabaseRowPageAuthority(app, rowID, time.Now().UTC().UnixMilli()); err != nil {
		return err
	}
	if err := announceDatabaseRowPageChange(app, rowID); err != nil {
		return err
	}
	return markDatabaseRowSearchDirty(app, rowID)
}

// restoreDatabaseRowPageHistoryValues sets a database page's property values
// to a version's, as its values route writes them: the version's value for
// each property it has now, and none where the version had none. A version
// from before database pages kept their values leaves them as they are.
func (p *pocketBaseYgoPersistence) restoreDatabaseRowPageHistoryValues(
	ctx context.Context, actorID, rowID, versionID, requestID string,
) error {
	if rowPage, err := isDatabaseRowPageRoom(p.app, rowID); err != nil || !rowPage {
		return err
	}
	version, err := loadPageHistoryVersion(p.app, rowID, versionID)
	if err != nil {
		return err
	}
	state, err := readPageHistorySnapshotPageState(p.app, rowID, version.VersionCursor)
	if err != nil || state.Values == nil {
		return err
	}
	authority, _, err := databaseRowPageMutationAuthorization(p.app, actorID, rowID, permissionWorkspaceWrite)
	if err != nil {
		return err
	}
	_, err = p.applyContentGraphMutationContext(ctx, actorID, authority.ContainerPageID,
		graphMutationInput{MutationID: "page-history-restore-values:" + rowID + ":" + requestID},
		contentGraphMutationHooks{
			EnforcePageLock: true,
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				current, page, err := databaseRowPageMutationAuthorization(txApp, actorID, rowID, permissionWorkspaceWrite)
				if err == nil && current.ContainerPageID != authority.ContainerPageID {
					err = errDatabaseRowPageConflict
				}
				return page, err
			},
			Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				current, _, err := databaseRowPageMutationAuthorization(txApp, actorID, rowID, permissionWorkspaceWrite)
				if err != nil {
					return contentGraphMutationPreparation{}, err
				}
				values, err := databaseRowPageHistoryValues(txApp, current, state.Values)
				if err != nil || len(values) == 0 {
					return contentGraphMutationPreparation{Deferred: err == nil}, err
				}
				return contentGraphMutationPreparation{Operations: []graphOperation{{
					Kind: graphOperationKindUpsertRow,
					Row: &graphDatabaseRow{
						ID: current.RowID, DataSourceID: current.DataSourceID,
						SubitemParentID: optionalDatabaseRowParent(current.SubitemParentID),
						Position:        current.Position, Values: values,
					},
				}}}, nil
			},
		},
	)
	return err
}

// databaseRowPageHistoryValues plans a version's values for the row's
// properties now, as its values route takes them.
func databaseRowPageHistoryValues(
	app core.App, authority databaseRowPageAuthority, stored map[string]pageHistoryRowValue,
) (map[string]graphRowValue, error) {
	properties, err := loadDatabaseRowPageProperties(app, authority, authority.ContainerPageID, authority.DataSourceID)
	if err != nil {
		return nil, err
	}
	values := map[string]graphRowValue{}
	for _, property := range properties {
		if databaseRowPageComputedValueTypes[property.Type] {
			continue
		}
		cell, found := stored[property.ID]
		if !found || cell.Type != property.Type {
			values[property.ID] = graphRowValue{Type: property.Type, Value: graphRowValueCleared}
			continue
		}
		value := graphRowValue{Type: cell.Type, Value: cell.Value}
		if len(cell.Spans) > 0 && json.Unmarshal(cell.Spans, &value.Spans) != nil {
			return nil, errDatabaseRowPageValuesInvalid
		}
		values[property.ID] = value
	}
	return values, nil
}

// recordDatabaseRowPageHistoryBaseline starts a database page's history
// anew from its state now, after changes its history didn't record: its
// history is read and restored from its latest baseline on.
func recordDatabaseRowPageHistoryBaseline(app core.App, rowID string) error {
	frontier, err := pageHistorySnapshotFrontier(app, rowID)
	if err != nil {
		return err
	}
	page, err := app.FindRecordById(pagesCollection, rowID)
	if err != nil {
		return err
	}
	actorID := page.GetString("last_edited_by")
	if actorID == "" {
		actorID = page.GetString("created_by")
	}
	cursor := frontier + 1
	if _, err := app.DB().Insert("leafen_page_history_versions", dbx.Params{
		"id": pageHistoryVersionID(rowID, cursor), "workspace_id": page.GetString("workspace"),
		"page_id": rowID, "version_cursor": cursor, "actor_id": actorID,
		"source": pageHistoryVersionBaseline, "created_at": pageHistoryMoment(app),
	}).Execute(); err != nil {
		return err
	}
	return capturePageHistoryCheckpointAndProject(app, rowID, cursor, nil)
}

// refreshDatabaseRowPageHistoryBaseline starts a database page's history
// anew when it has one, after a write its history doesn't record.
func refreshDatabaseRowPageHistoryBaseline(app core.App, rowID string) error {
	var versions int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_page_history_versions}} WHERE [[page_id]]={:rowId}
	`).Bind(dbx.Params{"rowId": rowID}).Row(&versions); err != nil || versions == 0 {
		return err
	}
	return recordDatabaseRowPageHistoryBaseline(app, rowID)
}

// pageHistoryStartCursor is where a page's history starts: its latest
// baseline.
func pageHistoryStartCursor(app core.App, pageID string) (int, error) {
	var start int
	err := app.DB().NewQuery(`
		SELECT COALESCE(MAX([[version_cursor]]),0) FROM {{leafen_page_history_versions}}
		WHERE [[page_id]]={:pageId} AND [[source]]={:baseline}
	`).Bind(dbx.Params{"pageId": pageID, "baseline": pageHistoryVersionBaseline}).Row(&start)
	return start, err
}
