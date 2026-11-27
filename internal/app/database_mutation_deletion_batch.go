package app

import (
	"encoding/json"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type databaseMutationCellKey struct {
	RowID      string `json:"rowId"`
	PropertyID string `json:"propertyId"`
}

type databaseMutationRelationUpdate struct {
	RowID        string `json:"rowId"`
	PropertyID   string `json:"propertyId"`
	DataSourceID string `json:"dataSourceId"`
	Value        string `json:"value"`
}

type databaseMutationFileReference struct {
	OwnerKind  string `db:"owner_kind" json:"ownerKind"`
	RowID      string `db:"owner_id" json:"rowId"`
	PropertyID string `db:"property_id" json:"propertyId"`
	FileID     string `db:"file_id" json:"fileId"`
}

func databaseMutationCellKeys(cells []databaseMutationCell) []databaseMutationCellKey {
	keys := make([]databaseMutationCellKey, 0, len(cells))
	for _, cell := range cells {
		keys = append(keys, databaseMutationCellKey{
			RowID: cell.RowID, PropertyID: cell.PropertyID,
		})
	}
	return keys
}

func deleteDatabaseMutationCellProjections(
	app core.App, job storedDatabaseMutationDeletion, cells []databaseMutationCell,
) error {
	encoded, err := json.Marshal(databaseMutationCellKeys(cells))
	if err != nil {
		return err
	}
	params := dbx.Params{
		"owner": job.OwnerID, "pageId": job.PageID, "cells": string(encoded),
	}
	if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_database_relation_items}}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND EXISTS (
			SELECT 1 FROM json_each({:cells}) AS [[cell]]
			WHERE json_extract([[cell]].[[value]], '$.rowId')={{leafen_database_relation_items}}.[[row_id]]
				AND json_extract([[cell]].[[value]], '$.propertyId')={{leafen_database_relation_items}}.[[property_id]]
		)`).Bind(params).Execute(); err != nil {
		return err
	}
	result, err := app.DB().NewQuery(`DELETE FROM {{leafen_database_row_values}}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND EXISTS (
			SELECT 1 FROM json_each({:cells}) AS [[cell]]
			WHERE json_extract([[cell]].[[value]], '$.rowId')={{leafen_database_row_values}}.[[row_id]]
				AND json_extract([[cell]].[[value]], '$.propertyId')={{leafen_database_row_values}}.[[property_id]]
		)`).Bind(params).Execute()
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil || count != int64(len(cells)) {
		return graphStructureAuthorityError()
	}
	return nil
}

func rewriteDatabaseMutationIncomingCellBatch(
	app core.App, job storedDatabaseMutationDeletion, cells []databaseMutationCell,
) error {
	updates := make([]databaseMutationRelationUpdate, 0, len(cells))
	for _, cell := range cells {
		stored, exists, err := loadDatabaseCellRecord(app, job.PageID, cell.RowID, cell.PropertyID)
		if err != nil {
			return err
		}
		items, isList := stored.Value.([]any)
		if !exists || stored.Type != graphValueTypeRelation || !isList {
			return graphStructureAuthorityError()
		}
		values, err := databaseRelationStrings(items)
		if err != nil {
			return err
		}
		filtered := make([]any, 0, len(values))
		for _, value := range values {
			if value != job.TargetID {
				filtered = append(filtered, value)
			}
		}
		if len(filtered) == len(values) {
			return graphStructureAuthorityError()
		}
		encoded, err := json.Marshal(filtered)
		if err != nil {
			return err
		}
		updates = append(updates, databaseMutationRelationUpdate{
			RowID: cell.RowID, PropertyID: cell.PropertyID,
			DataSourceID: cell.DataSourceID, Value: string(encoded),
		})
	}
	encoded, err := json.Marshal(updates)
	if err != nil {
		return err
	}
	now := time.Now().UTC().UnixMilli()
	params := dbx.Params{
		"owner": job.OwnerID, "workspaceId": job.WorkspaceID, "pageId": job.PageID,
		"updates": string(encoded), "now": now,
	}
	result, err := app.DB().NewQuery(`WITH [[updates]] AS (
		SELECT json_extract([[entry]].[[value]], '$.rowId') AS [[row_id]],
			json_extract([[entry]].[[value]], '$.propertyId') AS [[property_id]],
			json_extract([[entry]].[[value]], '$.value') AS [[value]]
		FROM json_each({:updates}) AS [[entry]]
	) UPDATE {{leafen_database_row_values}}
		SET [[value]]=(SELECT [[value]] FROM [[updates]]
			WHERE [[row_id]]={{leafen_database_row_values}}.[[row_id]]
				AND [[property_id]]={{leafen_database_row_values}}.[[property_id]]),
			[[updated]]={:now}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND EXISTS (
			SELECT 1 FROM [[updates]] WHERE [[row_id]]={{leafen_database_row_values}}.[[row_id]]
				AND [[property_id]]={{leafen_database_row_values}}.[[property_id]]
		)`).Bind(params).Execute()
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil || count != int64(len(updates)) {
		return graphStructureAuthorityError()
	}
	if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_database_relation_items}}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND EXISTS (
			SELECT 1 FROM json_each({:updates}) AS [[entry]]
			WHERE json_extract([[entry]].[[value]], '$.rowId')={{leafen_database_relation_items}}.[[row_id]]
				AND json_extract([[entry]].[[value]], '$.propertyId')={{leafen_database_relation_items}}.[[property_id]]
		)`).Bind(params).Execute(); err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`INSERT INTO {{leafen_database_relation_items}}
		([[owner]],[[workspace_id]],[[page_id]],[[data_source_id]],[[row_id]],
		 [[property_id]],[[ordinal]],[[target_row_id]],[[created]],[[updated]])
		SELECT {:owner},{:workspaceId},{:pageId},
			json_extract([[entry]].[[value]], '$.dataSourceId'),
			json_extract([[entry]].[[value]], '$.rowId'),
			json_extract([[entry]].[[value]], '$.propertyId'),
			CAST([[item]].[[key]] AS INTEGER),CAST([[item]].[[value]] AS TEXT),{:now},{:now}
		FROM json_each({:updates}) AS [[entry]]
		JOIN json_each(json_extract([[entry]].[[value]], '$.value')) AS [[item]]
		WHERE [[item]].[[type]]='text' AND CAST([[item]].[[value]] AS TEXT)!=''
		ORDER BY json_extract([[entry]].[[value]], '$.rowId'),
			json_extract([[entry]].[[value]], '$.propertyId'),CAST([[item]].[[key]] AS INTEGER)`).Bind(params).Execute()
	return err
}

func deleteDatabaseMutationFileReferences(
	app core.App, job storedDatabaseMutationDeletion, nextPhase string,
) (bool, int, string, bool, error) {
	where := `[[refs]].[[owner_id]]={:targetId}`
	index := "idx_leafen_file_references_database_row_subject"
	if job.TargetKind == databaseMutationTargetSource {
		where = `EXISTS (
			SELECT 1 FROM {{leafen_database_rows}} AS [[row]]
			WHERE [[row]].[[owner]]={:owner} AND [[row]].[[page_id]]={:pageId}
				AND [[row]].[[data_source_id]]={:targetId}
				AND [[refs]].[[owner_id]]=[[row]].[[id]]
		)`
	} else if job.TargetKind == databaseMutationTargetProperty {
		where = "[[refs]].[[owner_kind]]={:rowOwnerKind} AND [[refs]].[[property_id]]={:targetId}"
		index = "idx_leafen_file_references_database_property"
	}
	references := []databaseMutationFileReference{}
	err := app.DB().NewQuery(`SELECT [[refs]].[[owner_kind]],[[refs]].[[owner_id]],
		[[refs]].[[property_id]],[[refs]].[[file_id]]
		FROM {{leafen_file_references}} AS [[refs]] INDEXED BY [[` + index + `]]
		WHERE [[refs]].[[workspace_id]]={:workspaceId} AND [[refs]].[[page_id]]={:pageId}
			AND [[refs]].[[owner_kind]] IN ({:rowOwnerKind},{:iconOwnerKind}) AND ` + where + `
		ORDER BY [[refs]].[[owner_kind]],[[refs]].[[owner_id]],[[refs]].[[property_id]],[[refs]].[[file_id]]
		LIMIT {:limit}`,
	).Bind(dbx.Params{
		"owner": job.OwnerID, "workspaceId": job.WorkspaceID, "pageId": job.PageID,
		"rowOwnerKind":  productFileReferenceOwnerRow,
		"iconOwnerKind": productFileReferenceOwnerDatabaseRowPageIcon,
		"targetId":      job.TargetID,
		"limit":         databaseMutationItemsPerTurn,
	}).All(&references)
	if err != nil {
		return false, 0, "", false, err
	}
	if len(references) == 0 {
		return false, 0, nextPhase, false, nil
	}
	encoded, err := json.Marshal(references)
	if err != nil {
		return false, 0, "", false, err
	}
	result, err := app.DB().NewQuery(`DELETE FROM {{leafen_file_references}}
		WHERE [[workspace_id]]={:workspaceId} AND [[page_id]]={:pageId}
			AND [[owner_kind]] IN ({:rowOwnerKind},{:iconOwnerKind}) AND EXISTS (
				SELECT 1 FROM json_each({:references}) AS [[reference]]
				WHERE json_extract([[reference]].[[value]], '$.ownerKind')={{leafen_file_references}}.[[owner_kind]]
					AND json_extract([[reference]].[[value]], '$.rowId')={{leafen_file_references}}.[[owner_id]]
					AND json_extract([[reference]].[[value]], '$.propertyId')={{leafen_file_references}}.[[property_id]]
					AND json_extract([[reference]].[[value]], '$.fileId')={{leafen_file_references}}.[[file_id]]
			)`).Bind(dbx.Params{
		"workspaceId": job.WorkspaceID, "pageId": job.PageID,
		"rowOwnerKind":  productFileReferenceOwnerRow,
		"iconOwnerKind": productFileReferenceOwnerDatabaseRowPageIcon,
		"references":    string(encoded),
	}).Execute()
	if err != nil {
		return false, 0, "", false, err
	}
	count, err := result.RowsAffected()
	if err != nil || count != int64(len(references)) {
		return false, 0, "", false, graphStructureAuthorityError()
	}
	fileIDs := make([]string, 0, len(references))
	for _, reference := range references {
		fileIDs = append(fileIDs, reference.FileID)
	}
	if err := reconcileProductFileReferenceLifecycle(app, job.WorkspaceID, fileIDs); err != nil {
		return false, 0, "", false, err
	}
	return true, len(references), job.Phase, false, nil
}

func deleteDatabaseMutationEntityBatch(
	app core.App, job storedDatabaseMutationDeletion, table string, items []databaseMutationEntity,
) error {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ID)
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	params := dbx.Params{
		"owner": job.OwnerID, "pageId": job.PageID, "ids": string(encoded),
	}
	if table == "leafen_database_views" {
		if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_database_view_property_refs}}
			WHERE [[owner]]={:owner} AND [[page_id]]={:pageId}
				AND [[view_id]] IN (SELECT [[value]] FROM json_each({:ids}))`).Bind(params).Execute(); err != nil {
			return err
		}
	}
	if table == "leafen_database_properties" {
		if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_database_relation_property_targets}}
			WHERE [[owner]]={:owner} AND [[page_id]]={:pageId}
				AND [[property_id]] IN (SELECT [[value]] FROM json_each({:ids}))`).Bind(params).Execute(); err != nil {
			return err
		}
	}
	// A deleted source's rows leave other pages' relations as they go.
	if table == "leafen_database_rows" {
		if err := admitGoneRelationTargetWork(
			app, job.OwnerID, job.WorkspaceID, job.ActorID, "gone:"+job.ID+":"+ids[0], ids, nil,
		); err != nil {
			return err
		}
	}
	params["targetId"] = job.TargetID
	// By the batch's IDs, the source only checked: by the source, each batch
	// went through all of a big database's rows left.
	result, err := app.DB().NewQuery(`DELETE FROM {{` + table + `}}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId}
			AND [[id]] IN (SELECT [[value]] FROM json_each({:ids})) AND +[[data_source_id]]={:targetId}`).Bind(params).Execute()
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil || count != int64(len(items)) {
		return graphStructureAuthorityError()
	}
	return nil
}
