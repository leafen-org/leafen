package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

func advanceBlockMoveDatabaseForward(
	app core.App,
	job storedBlockMoveJob,
) (bool, error) {
	return advanceBlockMoveDatabaseForwardKeeping(app, job, nil)
}

// advanceBlockMoveDatabaseForwardKeeping moves every lane except those keep
// reports, which stay on the source page.
func advanceBlockMoveDatabaseForwardKeeping(
	app core.App,
	job storedBlockMoveJob,
	keep func(lane int) bool,
) (bool, error) {
	if err := assertBlockMoveDatabaseAutomationHistoryRetained(); err != nil {
		return false, err
	}
	inventoried, err := inventoryBlockMoveDatabaseWindow(app, job, keep)
	if err != nil {
		return false, err
	}
	if !inventoried {
		return false, touchBlockMoveDatabaseProgress(app, job)
	}
	items, err := loadBlockMoveDatabaseTransferWindow(app, job.ID, false)
	if err != nil {
		return false, err
	}
	if len(items) == 0 {
		return true, nil
	}
	if err := transferBlockMoveDatabaseItems(app, job, items, false); err != nil {
		return false, err
	}
	return false, touchBlockMoveDatabaseProgress(app, job)
}

func advanceBlockMoveDatabaseRollback(
	app core.App,
	job storedBlockMoveJob,
) (bool, error) {
	items, err := loadBlockMoveDatabaseTransferWindow(app, job.ID, true)
	if err != nil {
		return false, err
	}
	if len(items) == 0 {
		return true, nil
	}
	if err := transferBlockMoveDatabaseItems(app, job, items, true); err != nil {
		return false, err
	}
	return false, touchBlockMoveDatabaseProgress(app, job)
}

func loadBlockMoveDatabaseTransferWindow(
	app core.App,
	jobID string,
	rollback bool,
) ([]storedBlockMoveDatabaseItem, error) {
	moved := 0
	if rollback {
		moved = 1
	}
	first := storedBlockMoveDatabaseItem{}
	err := app.DB().NewQuery(`
		SELECT [[job_id]],[[data_source_id]],[[lane]],[[source_rowid]],[[entity_id]],
			[[secondary_id]],[[tertiary_id]],[[payload_bytes]],[[moved]]
		FROM {{leafen_block_move_database_items}}
		WHERE [[job_id]]={:jobId} AND [[moved]]={:moved}
		ORDER BY [[lane]],[[source_rowid]] LIMIT 1
	`).Bind(dbx.Params{"jobId": jobID, "moved": moved}).One(&first)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	items := []storedBlockMoveDatabaseItem{}
	err = app.DB().NewQuery(`
		SELECT [[job_id]],[[data_source_id]],[[lane]],[[source_rowid]],[[entity_id]],
			[[secondary_id]],[[tertiary_id]],[[payload_bytes]],[[moved]]
		FROM {{leafen_block_move_database_items}}
		WHERE [[job_id]]={:jobId} AND [[moved]]={:moved} AND [[lane]]={:lane}
		ORDER BY [[source_rowid]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"jobId": jobID, "moved": moved, "lane": first.Lane,
		"limit": maxBlockMoveItemsPerTurn,
	}).All(&items)
	return items, err
}

func transferBlockMoveDatabaseItems(
	app core.App,
	job storedBlockMoveJob,
	items []storedBlockMoveDatabaseItem,
	rollback bool,
) error {
	if len(items) == 0 || len(items) > maxBlockMoveItemsPerTurn {
		return errBlockMoveConflict
	}
	lane := items[0].Lane
	for _, item := range items {
		if item.Lane != lane {
			return errBlockMoveConflict
		}
	}
	fromPageID, toPageID := job.SourcePageID, job.DestinationPageID
	if rollback {
		fromPageID, toPageID = toPageID, fromPageID
	}
	table, err := blockMoveDatabaseLaneTable(lane)
	if err != nil {
		return err
	}
	rowIDs := make([]int64, len(items))
	for index := range items {
		rowIDs[index] = items[index].SourceRowID
	}
	encodedRows, err := json.Marshal(rowIDs)
	if err != nil {
		return err
	}
	result, err := app.DB().NewQuery(fmt.Sprintf(`
		UPDATE {{%s}} SET [[page_id]]={:toPageId}
		WHERE [[page_id]]={:fromPageId}
			AND [[rowid]] IN (SELECT [[value]] FROM json_each({:rowIds}))
	`, table)).Bind(dbx.Params{
		"toPageId": toPageID, "fromPageId": fromPageID, "rowIds": string(encodedRows),
	}).Execute()
	if err != nil {
		return err
	}
	updated, err := result.RowsAffected()
	if err != nil || updated != int64(len(items)) {
		return errors.Join(errBlockMoveConflict, err)
	}
	// A moved property keeps its type-change records, so the change can still
	// be undone on the page it moved to.
	if lane == blockMoveDatabaseLaneProperty {
		propertyIDs := make([]string, len(items))
		for index := range items {
			propertyIDs[index] = items[index].EntityID
		}
		encodedProperties, err := json.Marshal(propertyIDs)
		if err != nil {
			return err
		}
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_database_property_conversions}} SET [[page_id]]={:toPageId}
			WHERE [[page_id]]={:fromPageId}
				AND [[property_id]] IN (SELECT [[value]] FROM json_each({:propertyIds}))
		`).Bind(dbx.Params{
			"toPageId": toPageID, "fromPageId": fromPageID, "propertyIds": string(encodedProperties),
		}).Execute(); err != nil {
			return err
		}
	}
	if blockMoveDatabaseLaneInGraph(lane) {
		if err := recordBlockMoveDatabaseGraphChanges(app, job, []string{fromPageID, toPageID}, rollback, items); err != nil {
			return err
		}
	}
	now := time.Now().UTC().UnixMilli()
	newMoved := 1
	oldMoved := 0
	if rollback {
		newMoved, oldMoved = 0, 1
	}
	result, err = app.DB().NewQuery(`
		UPDATE {{leafen_block_move_database_items}}
		SET [[moved]]={:newMoved},[[updated_at]]={:now}
		WHERE [[job_id]]={:jobId} AND [[lane]]={:lane} AND [[moved]]={:oldMoved}
			AND [[source_rowid]] IN (SELECT [[value]] FROM json_each({:rowIds}))
	`).Bind(dbx.Params{
		"newMoved": newMoved, "oldMoved": oldMoved, "now": now,
		"jobId": job.ID, "lane": lane, "rowIds": string(encodedRows),
	}).Execute()
	if err != nil {
		return err
	}
	updated, err = result.RowsAffected()
	if err != nil || updated != int64(len(items)) {
		return errors.Join(errBlockMoveConflict, err)
	}
	return nil
}

// blockMoveDatabaseLaneInGraph reports the lanes a page's content graph
// shows, whose moves both pages record as graph changes.
func blockMoveDatabaseLaneInGraph(lane int) bool {
	switch lane {
	case blockMoveDatabaseLaneSource, blockMoveDatabaseLaneView, blockMoveDatabaseLaneProperty,
		blockMoveDatabaseLaneRow, blockMoveDatabaseLaneCell, blockMoveDatabaseLaneDependencyBinding,
		blockMoveDatabaseLaneDependencyEdge:
		return true
	default:
		return false
	}
}

func blockMoveDatabaseLaneTable(lane int) (string, error) {
	tables := map[int]string{
		blockMoveDatabaseLaneAllocator:            "leafen_database_unique_id_allocators",
		blockMoveDatabaseLaneSource:               "leafen_data_sources",
		blockMoveDatabaseLaneView:                 "leafen_database_views",
		blockMoveDatabaseLaneProperty:             "leafen_database_properties",
		blockMoveDatabaseLaneRow:                  "leafen_database_rows",
		blockMoveDatabaseLaneCell:                 "leafen_database_row_values",
		blockMoveDatabaseLaneDependencyBinding:    "leafen_database_dependency_bindings",
		blockMoveDatabaseLaneDependencyEdge:       "leafen_database_dependency_edges",
		blockMoveDatabaseLaneRelationItem:         "leafen_database_relation_items",
		blockMoveDatabaseLaneRelationTarget:       "leafen_database_relation_property_targets",
		blockMoveDatabaseLaneViewReference:        "leafen_database_view_property_refs",
		blockMoveDatabaseLaneFileReference:        "leafen_file_references",
		blockMoveDatabaseLaneTemplate:             "leafen_database_templates",
		blockMoveDatabaseLaneForm:                 "leafen_form_links",
		blockMoveDatabaseLaneAutomationFrontier:   "leafen_database_automation_frontiers",
		blockMoveDatabaseLaneAutomationDefinition: "leafen_database_automation_definitions",
	}
	table := tables[lane]
	if table == "" {
		return "", errBlockMoveConflict
	}
	return table, nil
}

func touchBlockMoveDatabaseProgress(app core.App, job storedBlockMoveJob) error {
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_block_move_jobs}}
		SET [[revision]]=[[revision]]+1,[[updated_at]]={:now}
		WHERE [[job_id]]={:jobId} AND [[phase]] IN ('moving','rolling_back')
	`).Bind(dbx.Params{"now": time.Now().UTC().UnixMilli(), "jobId": job.ID}).Execute()
	return err
}

// recordBlockMoveDatabaseGraphChanges records the move on both pages'
// content graphs. The database records moved; the page documents, which
// keep only the body, did not change.
func recordBlockMoveDatabaseGraphChanges(
	app core.App,
	job storedBlockMoveJob,
	pageIDs []string,
	rollback bool,
	items []storedBlockMoveDatabaseItem,
) error {
	counts := blockMoveDatabaseMutationCounts(items)
	unchanged := crdt.EncodeStateAsUpdateV1(crdt.New(), nil)
	sorted := slices.Clone(pageIDs)
	slices.Sort(sorted)
	for _, pageID := range slices.Compact(sorted) {
		if err := recordBlockMoveGraphChange(
			app, job, pageID, unchanged, rollback, "database", counts,
		); err != nil {
			return err
		}
		if err := markSearchDirtyByPageID(app, pageID); err != nil {
			return err
		}
	}
	return nil
}

func blockMoveDatabaseMutationCounts(items []storedBlockMoveDatabaseItem) graphMutationCounts {
	counts := graphMutationCounts{}
	for _, item := range items {
		switch item.Lane {
		case blockMoveDatabaseLaneSource:
			counts.DataSources++
		case blockMoveDatabaseLaneView:
			counts.Views++
		case blockMoveDatabaseLaneProperty:
			counts.Properties++
		case blockMoveDatabaseLaneRow:
			counts.Rows++
		case blockMoveDatabaseLaneCell:
			counts.RowValues++
		case blockMoveDatabaseLaneDependencyBinding:
			counts.DependencyBindings++
		case blockMoveDatabaseLaneDependencyEdge:
			counts.DependencyEdges++
		}
	}
	return counts
}
