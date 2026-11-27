package app

import (
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

func replaceDatabaseRelationProjection(
	app core.App,
	owner, workspaceID, pageID, dataSourceID, rowID, propertyID, value string,
	updated int64,
) error {
	if !json.Valid([]byte(value)) {
		return errors.New(errorCodeGraphInvalid)
	}
	if _, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_database_relation_items}}
		WHERE [[row_id]] = {:rowId} AND [[property_id]] = {:propertyId}
	`).Bind(dbx.Params{"rowId": rowID, "propertyId": propertyID}).Execute(); err != nil {
		return err
	}
	_, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_database_relation_items}}
			([[owner]], [[workspace_id]], [[page_id]], [[data_source_id]], [[row_id]],
			 [[property_id]], [[ordinal]], [[target_row_id]], [[created]], [[updated]])
		SELECT {:owner}, {:workspaceId}, {:pageId}, {:dataSourceId}, {:rowId},
			{:propertyId}, CAST([[entry]].[[key]] AS INTEGER), CAST([[entry]].[[value]] AS TEXT),
			{:updated}, {:updated}
		FROM json_each({:value}) AS [[entry]]
		WHERE [[entry]].[[type]] = 'text' AND CAST([[entry]].[[value]] AS TEXT) != ''
		ORDER BY CAST([[entry]].[[key]] AS INTEGER)
	`).Bind(dbx.Params{
		"owner": owner, "workspaceId": workspaceID, "pageId": pageID,
		"dataSourceId": dataSourceID, "rowId": rowID, "propertyId": propertyID,
		"value": value, "updated": updated,
	}).Execute()
	return err
}

func deleteDatabaseRelationProjection(
	app core.App,
	rowID, propertyID string,
) error {
	_, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_database_relation_items}}
		WHERE [[row_id]] = {:rowId} AND [[property_id]] = {:propertyId}
	`).Bind(dbx.Params{"rowId": rowID, "propertyId": propertyID}).Execute()
	return err
}
