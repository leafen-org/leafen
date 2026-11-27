package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	blockMoveDatabaseLaneAllocator = iota
	blockMoveDatabaseLaneSource
	blockMoveDatabaseLaneView
	blockMoveDatabaseLaneProperty
	blockMoveDatabaseLaneRow
	blockMoveDatabaseLaneCell
	blockMoveDatabaseLaneDependencyBinding
	blockMoveDatabaseLaneDependencyEdge
	blockMoveDatabaseLaneRelationItem
	blockMoveDatabaseLaneRelationTarget
	blockMoveDatabaseLaneViewReference
	blockMoveDatabaseLaneFileReference
	blockMoveDatabaseLaneTemplate
	blockMoveDatabaseLaneForm
	blockMoveDatabaseLaneAutomationFrontier
	blockMoveDatabaseLaneAutomationDefinition
	blockMoveDatabaseLaneCount
)

var blockMoveDatabaseAutomationHistoryRetained = map[string]struct{}{
	"leafen_database_automation_events":               {},
	"leafen_database_automation_outcomes":             {},
	"leafen_database_automation_schedule_occurrences": {},
	"leafen_database_automation_deliveries":           {},
	"leafen_database_automation_notifications":        {},
}

type storedBlockMoveDatabaseScope struct {
	JobID           string `db:"job_id"`
	DataSourceID    string `db:"data_source_id"`
	InventoryLane   int    `db:"inventory_lane"`
	InventoryCursor int64  `db:"inventory_cursor"`
	Inventoried     bool   `db:"inventoried"`
}

type storedBlockMoveDatabaseItem struct {
	JobID        string `db:"job_id"`
	DataSourceID string `db:"data_source_id"`
	Lane         int    `db:"lane"`
	SourceRowID  int64  `db:"source_rowid"`
	EntityID     string `db:"entity_id"`
	SecondaryID  string `db:"secondary_id"`
	TertiaryID   string `db:"tertiary_id"`
	PayloadBytes int    `db:"payload_bytes"`
	Moved        bool   `db:"moved"`
}

func discoverBlockMoveDatabaseScope(
	app core.App,
	job storedBlockMoveJob,
	blockID string,
) error {
	block := struct {
		Type    string `db:"type"`
		Content string `db:"content"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[type]],[[content]] FROM {{leafen_blocks}}
		WHERE [[owner]]={:ownerId} AND [[page_id]]={:pageId} AND [[id]]={:blockId}
	`).Bind(dbx.Params{
		"ownerId": job.sourceOwner(), "pageId": job.SourcePageID, "blockId": blockID,
	}).One(&block); err != nil {
		return err
	}
	if block.Type != graphBlockTypeInlineDatabase {
		return nil
	}
	content, err := parseDatabaseContainerBlockContent(json.RawMessage(block.Content))
	if err != nil || content.DataSourceID == "" {
		return errBlockMoveConflict
	}
	if content.LinkedDatabaseSource {
		return nil
	}
	now := time.Now().UTC().UnixMilli()
	for _, dataSourceID := range content.dataSourceIDs() {
		var exists int
		if err := app.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_data_sources}}
			WHERE [[owner]]={:ownerId} AND [[workspace_id]]={:workspaceId}
				AND [[page_id]]={:pageId} AND [[id]]={:dataSourceId}
		`).Bind(dbx.Params{
			"ownerId": job.sourceOwner(), "workspaceId": job.sourceWorkspace(),
			"pageId": job.SourcePageID, "dataSourceId": dataSourceID,
		}).Row(&exists); err != nil {
			return err
		}
		if exists != 1 {
			return errBlockMoveConflict
		}
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_block_move_database_scopes}}
				([[job_id]],[[data_source_id]],[[created_at]],[[updated_at]])
			VALUES ({:jobId},{:dataSourceId},{:now},{:now})
			ON CONFLICT ([[job_id]],[[data_source_id]]) DO NOTHING
		`).Bind(dbx.Params{
			"jobId": job.ID, "dataSourceId": dataSourceID, "now": now,
		}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

func inventoryBlockMoveDatabaseWindow(
	app core.App,
	job storedBlockMoveJob,
	keep func(lane int) bool,
) (bool, error) {
	scope := storedBlockMoveDatabaseScope{}
	err := app.DB().NewQuery(`
		SELECT [[job_id]],[[data_source_id]],[[inventory_lane]],[[inventory_cursor]],[[inventoried]]
		FROM {{leafen_block_move_database_scopes}}
		WHERE [[job_id]]={:jobId} AND [[inventoried]]=0
		ORDER BY [[data_source_id]] LIMIT 1
	`).Bind(dbx.Params{"jobId": job.ID}).One(&scope)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	quiescent, err := assertBlockMoveDatabaseScopeQuiescent(app, job, scope.DataSourceID)
	if err != nil {
		return false, err
	}
	if !quiescent {
		return false, deferBlockMoveDatabaseQuiescence(app, job.ID)
	}
	remaining := maxBlockMoveItemsPerTurn
	lane := scope.InventoryLane
	cursor := scope.InventoryCursor
	for remaining > 0 && lane < blockMoveDatabaseLaneCount {
		if keep != nil && keep(lane) {
			lane++
			cursor = 0
			continue
		}
		rows, err := loadBlockMoveDatabaseInventoryRows(app, job, scope.DataSourceID, lane, cursor, remaining+1)
		if err != nil {
			return false, err
		}
		hasMore := len(rows) > remaining
		if hasMore {
			rows = rows[:remaining]
		}
		if err := storeBlockMoveDatabaseInventoryRows(app, job.ID, scope.DataSourceID, lane, rows); err != nil {
			return false, err
		}
		remaining -= len(rows)
		if len(rows) > 0 {
			cursor = rows[len(rows)-1].SourceRowID
		}
		if hasMore {
			break
		}
		lane++
		cursor = 0
	}
	now := time.Now().UTC().UnixMilli()
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_block_move_database_scopes}}
		SET [[inventory_lane]]={:lane},[[inventory_cursor]]={:cursor},
			[[inventoried]]={:inventoried},[[updated_at]]={:now}
		WHERE [[job_id]]={:jobId} AND [[data_source_id]]={:dataSourceId} AND [[inventoried]]=0
	`).Bind(dbx.Params{
		"lane": lane, "cursor": cursor, "inventoried": lane >= blockMoveDatabaseLaneCount,
		"now": now, "jobId": job.ID, "dataSourceId": scope.DataSourceID,
	}).Execute()
	return false, err
}

func assertBlockMoveDatabaseScopeQuiescent(
	app core.App,
	job storedBlockMoveJob,
	dataSourceID string,
) (bool, error) {
	var active int
	if err := app.DB().NewQuery(`
		SELECT
			(SELECT COUNT(*) FROM {{leafen_database_mutation_jobs}} [[mutation]]
			 WHERE [[mutation]].[[owner_id]]={:ownerId} AND [[mutation]].[[page_id]]={:pageId}
				AND [[mutation]].[[status]]='pending' AND (
					([[mutation]].[[target_kind]]='source' AND [[mutation]].[[target_id]]={:dataSourceId})
					OR ([[mutation]].[[target_kind]]='property' AND EXISTS (
						SELECT 1 FROM {{leafen_database_properties}} [[property]]
						WHERE [[property]].[[id]]=[[mutation]].[[target_id]]
							AND [[property]].[[data_source_id]]={:dataSourceId}
					)) OR ([[mutation]].[[target_kind]]='row' AND EXISTS (
						SELECT 1 FROM {{leafen_database_rows}} [[row]]
						WHERE [[row]].[[id]]=[[mutation]].[[target_id]]
							AND [[row]].[[data_source_id]]={:dataSourceId}
					))
				)) +
			(SELECT COUNT(*) FROM {{leafen_database_automation_events}} [[event]]
			 WHERE [[event]].[[owner]]={:ownerId} AND [[event]].[[page_id]]={:pageId}
				AND [[event]].[[database_id]]={:dataSourceId}
				AND [[event]].[[status]]<>'completed') +
			(SELECT COUNT(*) FROM {{leafen_database_automation_deliveries}} [[delivery]]
			 WHERE [[delivery]].[[owner]]={:ownerId} AND [[delivery]].[[page_id]]={:pageId}
				AND [[delivery]].[[database_id]]={:dataSourceId}
				AND [[delivery]].[[state]] IN ('pending','retrying','claimed')) +
			(SELECT COUNT(*) FROM {{leafen_database_property_conversions}} [[conversion]]
			 WHERE [[conversion]].[[owner]]={:ownerId} AND [[conversion]].[[page_id]]={:pageId}
				AND [[conversion]].[[data_source_id]]={:dataSourceId}
				AND [[conversion]].[[status]]='pending')
	`).Bind(dbx.Params{
		"ownerId": job.sourceOwner(), "pageId": job.SourcePageID,
		"dataSourceId": dataSourceID,
	}).Row(&active); err != nil {
		return false, err
	}
	return active == 0, nil
}

func deferBlockMoveDatabaseQuiescence(app core.App, jobID string) error {
	now := time.Now().UTC()
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_jobs}}
		SET [[not_before]]={:notBefore},[[updated]]={:updated}
		WHERE [[id]]={:jobId} AND [[status]]='queued'
	`).Bind(dbx.Params{
		"notBefore": now.Add(durableJobReadyDelay).UnixMilli(),
		"updated":   now.UnixMilli(), "jobId": jobID,
	}).Execute()
	return err
}

func assertBlockMoveDatabaseAutomationHistoryRetained() error {
	for lane := 0; lane < blockMoveDatabaseLaneCount; lane++ {
		table, err := blockMoveDatabaseLaneTable(lane)
		if err != nil {
			return err
		}
		if _, history := blockMoveDatabaseAutomationHistoryRetained[table]; history {
			return errBlockMoveConflict
		}
	}
	return nil
}

func storeBlockMoveDatabaseInventoryRows(
	app core.App,
	jobID, dataSourceID string,
	lane int,
	rows []storedBlockMoveDatabaseItem,
) error {
	now := time.Now().UTC().UnixMilli()
	for _, row := range rows {
		if _, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_block_move_database_items}}
				([[job_id]],[[data_source_id]],[[lane]],[[source_rowid]],[[entity_id]],
				 [[secondary_id]],[[tertiary_id]],[[payload_bytes]],[[created_at]],[[updated_at]])
			VALUES ({:jobId},{:dataSourceId},{:lane},{:rowId},{:entityId},
				{:secondaryId},{:tertiaryId},{:payloadBytes},{:now},{:now})
			ON CONFLICT ([[job_id]],[[lane]],[[source_rowid]]) DO NOTHING
		`).Bind(dbx.Params{
			"jobId": jobID, "dataSourceId": dataSourceID, "lane": lane,
			"rowId": row.SourceRowID, "entityId": row.EntityID,
			"secondaryId": row.SecondaryID, "tertiaryId": row.TertiaryID,
			"payloadBytes": row.PayloadBytes, "now": now,
		}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

func loadBlockMoveDatabaseInventoryRows(
	app core.App,
	job storedBlockMoveJob,
	dataSourceID string,
	lane int,
	cursor int64,
	limit int,
) ([]storedBlockMoveDatabaseItem, error) {
	query, err := blockMoveDatabaseInventoryQuery(lane)
	if err != nil {
		return nil, err
	}
	rows := []storedBlockMoveDatabaseItem{}
	err = app.DB().NewQuery(query).Bind(dbx.Params{
		"ownerId": job.sourceOwner(), "workspaceId": job.sourceWorkspace(),
		"pageId": job.SourcePageID, "dataSourceId": dataSourceID,
		"cursor": cursor, "limit": limit,
	}).All(&rows)
	return rows, err
}

func blockMoveDatabaseInventoryQuery(lane int) (string, error) {
	selectRow := func(table, predicate, entity, secondary, tertiary, payload string) string {
		return fmt.Sprintf(`SELECT [[item]].[[rowid]] AS [[source_rowid]], %s AS [[entity_id]],
			%s AS [[secondary_id]], %s AS [[tertiary_id]], %s AS [[payload_bytes]]
			FROM {{%s}} [[item]] WHERE [[item]].[[page_id]]={:pageId}
				AND %s AND [[item]].[[rowid]]>{:cursor}
			ORDER BY [[item]].[[rowid]] LIMIT {:limit}`,
			entity, secondary, tertiary, payload, table, predicate)
	}
	empty := `''`
	switch lane {
	case blockMoveDatabaseLaneAllocator:
		return selectRow("leafen_database_unique_id_allocators", "[[item]].[[data_source_id]]={:dataSourceId}", "[[item]].[[property_id]]", empty, empty, "0"), nil
	case blockMoveDatabaseLaneSource:
		return selectRow("leafen_data_sources", "[[item]].[[id]]={:dataSourceId}", "[[item]].[[id]]", empty, empty, "length([[item]].[[title]])+64"), nil
	case blockMoveDatabaseLaneView:
		// Views of linked databases on this page stay with their own blocks.
		return selectRow("leafen_database_views", "[[item]].[[data_source_id]]={:dataSourceId} AND [[item]].[[container_id]]=''", "[[item]].[[id]]", empty, empty, "length([[item]].[[config]])+length([[item]].[[name]])+256"), nil
	case blockMoveDatabaseLaneProperty:
		return selectRow("leafen_database_properties", "[[item]].[[data_source_id]]={:dataSourceId}", "[[item]].[[id]]", empty, empty, "length([[item]].[[config]])+length([[item]].[[name]])+256"), nil
	case blockMoveDatabaseLaneRow:
		return selectRow("leafen_database_rows", "[[item]].[[data_source_id]]={:dataSourceId}", "[[item]].[[id]]", empty, empty, "256"), nil
	case blockMoveDatabaseLaneCell:
		return `SELECT [[item]].[[rowid]] AS [[source_rowid]],[[item]].[[row_id]] AS [[entity_id]],
			[[item]].[[property_id]] AS [[secondary_id]],'' AS [[tertiary_id]],
			length([[item]].[[value]])+256 AS [[payload_bytes]]
			FROM {{leafen_database_row_values}} [[item]]
			JOIN {{leafen_database_rows}} [[row]] ON [[row]].[[id]]=[[item]].[[row_id]]
			WHERE [[item]].[[page_id]]={:pageId} AND [[row]].[[data_source_id]]={:dataSourceId}
				AND [[item]].[[rowid]]>{:cursor} ORDER BY [[item]].[[rowid]] LIMIT {:limit}`, nil
	case blockMoveDatabaseLaneDependencyBinding:
		return selectRow("leafen_database_dependency_bindings", "[[item]].[[data_source_id]]={:dataSourceId}", "[[item]].[[data_source_id]]", empty, empty, "512"), nil
	case blockMoveDatabaseLaneDependencyEdge:
		return selectRow("leafen_database_dependency_edges", "[[item]].[[data_source_id]]={:dataSourceId}", "[[item]].[[id]]", empty, empty, "512"), nil
	case blockMoveDatabaseLaneRelationItem:
		return selectRow("leafen_database_relation_items", "[[item]].[[data_source_id]]={:dataSourceId}", "[[item]].[[row_id]]", "[[item]].[[property_id]]", "CAST([[item]].[[ordinal]] AS TEXT)", "0"), nil
	case blockMoveDatabaseLaneRelationTarget:
		return `SELECT [[item]].[[rowid]] AS [[source_rowid]],[[item]].[[property_id]] AS [[entity_id]],
			'' AS [[secondary_id]],'' AS [[tertiary_id]],0 AS [[payload_bytes]]
			FROM {{leafen_database_relation_property_targets}} [[item]]
			JOIN {{leafen_database_properties}} [[property]] ON [[property]].[[id]]=[[item]].[[property_id]]
			WHERE [[item]].[[page_id]]={:pageId} AND [[property]].[[data_source_id]]={:dataSourceId}
				AND [[item]].[[rowid]]>{:cursor} ORDER BY [[item]].[[rowid]] LIMIT {:limit}`, nil
	case blockMoveDatabaseLaneViewReference:
		return `SELECT [[item]].[[rowid]] AS [[source_rowid]],[[item]].[[view_id]] AS [[entity_id]],
			[[item]].[[property_id]] AS [[secondary_id]],'' AS [[tertiary_id]],0 AS [[payload_bytes]]
			FROM {{leafen_database_view_property_refs}} [[item]]
			JOIN {{leafen_database_views}} [[view]] ON [[view]].[[id]]=[[item]].[[view_id]]
			WHERE [[item]].[[page_id]]={:pageId} AND [[view]].[[data_source_id]]={:dataSourceId}
				AND [[view]].[[container_id]]=''
				AND [[item]].[[rowid]]>{:cursor} ORDER BY [[item]].[[rowid]] LIMIT {:limit}`, nil
	case blockMoveDatabaseLaneFileReference:
		return `SELECT [[item]].[[rowid]] AS [[source_rowid]],[[item]].[[owner_id]] AS [[entity_id]],
			[[item]].[[property_id]] AS [[secondary_id]],[[item]].[[owner_kind]] AS [[tertiary_id]],
			0 AS [[payload_bytes]]
			FROM {{leafen_file_references}} [[item]]
			WHERE [[item]].[[page_id]]={:pageId} AND [[item]].[[rowid]]>{:cursor} AND (
				([[item]].[[owner_kind]]='database_row_value' AND EXISTS (
					SELECT 1 FROM {{leafen_database_rows}} [[row]]
					WHERE [[row]].[[id]]=[[item]].[[owner_id]] AND [[row]].[[data_source_id]]={:dataSourceId}
				)) OR ([[item]].[[owner_kind]]='database_template' AND EXISTS (
					SELECT 1 FROM {{leafen_database_templates}} [[template]]
					WHERE [[template]].[[id]]=[[item]].[[owner_id]] AND [[template]].[[data_source_id]]={:dataSourceId}
				)) OR ([[item]].[[owner_kind]] IN ('data_source_icon','data_source_cover') AND [[item]].[[owner_id]]={:dataSourceId})
				OR ([[item]].[[owner_kind]]='database_row_page_icon' AND EXISTS (
					SELECT 1 FROM {{leafen_database_rows}} [[row]]
					WHERE [[row]].[[id]]=[[item]].[[owner_id]] AND [[row]].[[data_source_id]]={:dataSourceId}
				))
			) ORDER BY [[item]].[[rowid]] LIMIT {:limit}`, nil
	case blockMoveDatabaseLaneTemplate:
		return selectRow("leafen_database_templates", "[[item]].[[data_source_id]]={:dataSourceId}", "[[item]].[[id]]", empty, empty, "0"), nil
	case blockMoveDatabaseLaneForm:
		return selectRow("leafen_form_links", "[[item]].[[data_source_id]]={:dataSourceId}", "[[item]].[[id]]", empty, empty, "0"), nil
	case blockMoveDatabaseLaneAutomationFrontier:
		return selectRow("leafen_database_automation_frontiers", "[[item]].[[database_id]]={:dataSourceId}", "[[item]].[[database_id]]", empty, empty, "0"), nil
	case blockMoveDatabaseLaneAutomationDefinition:
		return selectRow("leafen_database_automation_definitions", "[[item]].[[database_id]]={:dataSourceId}", "[[item]].[[id]]", empty, empty, "0"), nil
	default:
		return "", errBlockMoveConflict
	}
}
