package app

import (
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const databaseMutationJobsTable = "leafen_database_mutation_jobs"

type databaseMutationProjectionRow struct {
	ID          string `db:"id"`
	Owner       string `db:"owner"`
	WorkspaceID string `db:"workspace_id"`
	PageID      string `db:"page_id"`
	Type        string `db:"type"`
	Config      string `db:"config"`
	SourceID    string `db:"data_source_id"`
}

func replaceDatabaseRelationPropertyTarget(app core.App, row databaseMutationProjectionRow) error {
	if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_database_relation_property_targets}}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[property_id]]={:id}`,
	).Bind(dbx.Params{"owner": row.Owner, "pageId": row.PageID, "id": row.ID}).Execute(); err != nil {
		return err
	}
	if row.Type != graphValueTypeRelation {
		return nil
	}
	var config struct {
		RelationDatabaseID string `json:"relationDatabaseId"`
	}
	if err := json.Unmarshal([]byte(row.Config), &config); err != nil || config.RelationDatabaseID == "" {
		if err != nil {
			return err
		}
		return errors.New(errorCodeGraphInvalid)
	}
	_, err := app.DB().Insert("leafen_database_relation_property_targets", dbx.Params{
		"owner": row.Owner, "workspace_id": row.WorkspaceID, "page_id": row.PageID, "property_id": row.ID,
		"source_id": row.SourceID, "target_source_id": config.RelationDatabaseID,
	}).Execute()
	return err
}

func replaceDatabaseViewPropertyReferences(app core.App, row databaseMutationProjectionRow) error {
	if _, err := app.DB().NewQuery(`DELETE FROM {{leafen_database_view_property_refs}}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[view_id]]={:id}`,
	).Bind(dbx.Params{"owner": row.Owner, "pageId": row.PageID, "id": row.ID}).Execute(); err != nil {
		return err
	}
	if !json.Valid([]byte(row.Config)) {
		return errors.New(errorCodeGraphInvalid)
	}
	for _, propertyID := range graphViewPropertyReferences(json.RawMessage(row.Config)) {
		if _, err := app.DB().Insert("leafen_database_view_property_refs", dbx.Params{
			"owner": row.Owner, "workspace_id": row.WorkspaceID, "page_id": row.PageID,
			"view_id": row.ID, "property_id": propertyID,
		}).Execute(); err != nil {
			return err
		}
	}
	return nil
}
