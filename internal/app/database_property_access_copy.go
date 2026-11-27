package app

import (
	"fmt"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// copyDatabasePropertyAccess gives each copied data source the property
// access rules of the source it copies, as a duplicated database keeps them
// in Notion (seen 2026-10-12): each rule on the copy's own property, a
// person-property exception on the copy's person property. Copied into
// another workspace, a rule keeps no exception naming that workspace's
// people, groups or connections.
func copyDatabasePropertyAccess(app core.App, jobID, actorID, workspaceID string, sourceIDs []string) error {
	if len(sourceIDs) == 0 {
		return nil
	}
	copies, err := pageDuplicationReferenceMappings(app, jobID, "data_source", sourceIDs)
	if err != nil {
		return err
	}
	params := dbx.Params{}
	placeholders := make([]string, 0, len(sourceIDs))
	for index, sourceID := range sourceIDs {
		key := fmt.Sprintf("accessSource%d", index)
		params[key] = sourceID
		placeholders = append(placeholders, "{:"+key+"}")
	}
	rules := []struct {
		DataSourceID string `db:"data_source_id"`
		PropertyID   string `db:"property_id"`
		WorkspaceID  string `db:"workspace_id"`
		DefaultLevel string `db:"default_level"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[data_source_id]], [[property_id]], [[workspace_id]], [[default_level]]
		FROM {{leafen_database_property_access}} WHERE [[data_source_id]] IN (` + joinSQL(placeholders) + `)
		ORDER BY [[data_source_id]], [[property_id]]`).Bind(params).All(&rules); err != nil {
		return err
	}
	if len(rules) == 0 {
		return nil
	}
	exceptions := []storedDatabasePropertyAccessException{}
	if err := app.DB().NewQuery(`SELECT [[data_source_id]], [[property_id]], [[principal_type]], [[principal_id]], [[level]]
		FROM {{leafen_database_property_access_exceptions}} WHERE [[data_source_id]] IN (` + joinSQL(placeholders) + `)
		ORDER BY [[data_source_id]], [[property_id]], [[principal_type]], [[principal_id]]`).Bind(params).All(&exceptions); err != nil {
		return err
	}
	propertyIDs := []string{}
	for _, rule := range rules {
		propertyIDs = append(propertyIDs, rule.PropertyID)
	}
	for _, exception := range exceptions {
		if exception.PrincipalType == propertyAccessPrincipalProperty {
			propertyIDs = append(propertyIDs, exception.PrincipalID)
		}
	}
	properties, err := pageDuplicationReferenceMappings(app, jobID, "property", propertyIDs)
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	for _, rule := range rules {
		target, property := copies[rule.DataSourceID], properties[rule.PropertyID]
		if target == "" || property == "" {
			continue
		}
		if _, err := app.DB().NewQuery(`INSERT INTO {{leafen_database_property_access}}
				([[data_source_id]], [[property_id]], [[workspace_id]], [[default_level]], [[updated_by]], [[updated_at]])
			VALUES ({:source}, {:property}, {:workspaceId}, {:level}, {:actor}, {:now})
			ON CONFLICT ([[data_source_id]], [[property_id]]) DO NOTHING`).Bind(dbx.Params{
			"source": target, "property": property, "workspaceId": workspaceID, "level": rule.DefaultLevel, "actor": actorID, "now": now,
		}).Execute(); err != nil {
			return err
		}
		for _, exception := range exceptions {
			if exception.DataSourceID != rule.DataSourceID || exception.PropertyID != rule.PropertyID {
				continue
			}
			principal := exception.PrincipalID
			if exception.PrincipalType == propertyAccessPrincipalProperty {
				principal = properties[exception.PrincipalID]
			} else if rule.WorkspaceID != workspaceID {
				continue
			}
			if principal == "" {
				continue
			}
			if _, err := app.DB().NewQuery(`INSERT INTO {{leafen_database_property_access_exceptions}}
					([[data_source_id]], [[property_id]], [[workspace_id]], [[principal_type]], [[principal_id]], [[level]])
				VALUES ({:source}, {:property}, {:workspaceId}, {:type}, {:principal}, {:level})
				ON CONFLICT DO NOTHING`).Bind(dbx.Params{
				"source": target, "property": property, "workspaceId": workspaceID, "type": exception.PrincipalType,
				"principal": principal, "level": exception.Level,
			}).Execute(); err != nil {
				return err
			}
		}
	}
	return nil
}
