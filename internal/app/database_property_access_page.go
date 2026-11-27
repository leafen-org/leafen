package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// DatabasePagePropertyAccess is a database page's "속성 사용 권한" in its
// share window's "고급", as Notion's (seen 2026-10-12): the properties of its
// own data sources that take a rule, and the rules they have.
type DatabasePagePropertyAccess struct {
	Properties []DatabasePagePropertyAccessProperty `json:"properties"`
	Rules      []DatabasePagePropertyAccessRule     `json:"rules"`
}

type DatabasePagePropertyAccessProperty struct {
	DataSourceID string `json:"dataSourceId" db:"data_source_id"`
	PropertyID   string `json:"propertyId" db:"id"`
	Name         string `json:"name" db:"name"`
	Type         string `json:"type" db:"type"`
	Icon         string `json:"icon,omitempty" db:"icon"`
}

type DatabasePagePropertyAccessRule struct {
	DataSourceID string `json:"dataSourceId"`
	PropertyID   string `json:"propertyId"`
	DefaultLevel string `json:"defaultLevel"`
}

// loadDatabasePagePropertyAccess lists them for someone with full access to
// the page; a wiki's sources take no rule.
func loadDatabasePagePropertyAccess(app core.App, actorID, pageID string) (DatabasePagePropertyAccess, error) {
	access := DatabasePagePropertyAccess{Properties: []DatabasePagePropertyAccessProperty{}, Rules: []DatabasePagePropertyAccessRule{}}
	if _, err := authorizePageAccessRole(app, actorID, pageID, pageAccessRoleFullAccess); isPermissionDenied(err) {
		return access, errDatabasePropertyAccessNotFound
	} else if err != nil {
		return access, err
	}
	properties := []DatabasePagePropertyAccessProperty{}
	if err := app.DB().NewQuery(`
		SELECT [[property]].[[data_source_id]], [[property]].[[id]], [[property]].[[name]], [[property]].[[type]],
			CASE WHEN json_valid([[property]].[[config]]) AND json_type([[property]].[[config]], '$.icon') = 'text'
				THEN json_extract([[property]].[[config]], '$.icon') ELSE '' END AS [[icon]]
		FROM {{leafen_database_properties}} [[property]]
		JOIN {{leafen_data_sources}} [[source]] ON [[source]].[[id]] = [[property]].[[data_source_id]]
			AND [[source]].[[page_id]] = {:pageId} AND [[source]].[[wiki]] = 0
		WHERE [[property]].[[page_id]] = {:pageId} AND ` + databasePropertyNotTrashedSQL("[[property]].[[id]]") + `
		ORDER BY [[source]].[[id]], [[property]].[[position]], [[property]].[[id]]
	`).Bind(dbx.Params{"pageId": pageID}).All(&properties); err != nil {
		return access, err
	}
	sources := []string{}
	for _, property := range properties {
		if databasePropertyAccessUnrestrictable(property.Type, property.PropertyID) {
			continue
		}
		access.Properties = append(access.Properties, property)
		if len(sources) == 0 || sources[len(sources)-1] != property.DataSourceID {
			sources = append(sources, property.DataSourceID)
		}
	}
	rules, err := loadDatabasePropertyAccessRules(app, sources)
	if err != nil {
		return access, err
	}
	for _, property := range access.Properties {
		if rule := rules[property.DataSourceID][property.PropertyID]; rule != nil {
			access.Rules = append(access.Rules, DatabasePagePropertyAccessRule{
				DataSourceID: property.DataSourceID, PropertyID: property.PropertyID, DefaultLevel: rule.DefaultLevel,
			})
		}
	}
	return access, nil
}
