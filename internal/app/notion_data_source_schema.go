package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type storedNotionDataSourceProperty struct {
	ID       string `db:"id"`
	Type     string `db:"type"`
	Name     string `db:"name"`
	Position string `db:"position"`
	Config   string `db:"config"`
	// PublicID is the property's ID as the Public API gives it
	// (notion_property_public_id.go).
	PublicID string `db:"-"`
}

func loadNotionDataSourceProperties(
	app core.App,
	authorization notionDatabaseRowPageAuthorization,
) (map[string]any, error) {
	rows, err := loadStoredNotionDataSourceProperties(app, authorization.Container)
	if err != nil {
		return nil, err
	}
	formula, err := notionDataSourceFormulaScope(app, authorization.Container, rows)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]storedNotionDataSourceProperty, len(rows))
	for _, row := range rows {
		byID[row.ID] = row
	}
	// A property the connection's account sees in no row leaves the schema.
	reader, err := newDatabasePropertyAccessReader(app, authorization.Identity.PrincipalID, []string{authorization.Container.DataSource})
	if err != nil {
		return nil, err
	}
	properties := make(map[string]any, len(rows))
	for _, row := range rows {
		if !notionDatabaseRowPagePropertyVisible(row.Type) {
			continue
		}
		if hidden, err := notionSchemaPropertyHidden(reader, authorization.Container.DataSource, row.ID); err != nil {
			return nil, err
		} else if hidden {
			continue
		}
		projected, visible, err := projectNotionDataSourceProperty(app, authorization, row, formula, byID)
		if err != nil {
			return nil, err
		}
		if !visible {
			continue
		}
		if _, duplicate := properties[row.Name]; duplicate {
			return nil, errNotionDatabaseRowPageProjectionInvalid
		}
		properties[row.Name] = projected
	}
	return properties, nil
}

func loadStoredNotionDataSourceProperties(
	app core.App,
	container notionDataSourceParentAuthority,
) ([]storedNotionDataSourceProperty, error) {
	rows := []storedNotionDataSourceProperty{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[type]], [[name]], [[position]], [[config]]
		FROM {{leafen_database_properties}}
		WHERE [[owner]]={:owner} AND [[workspace_id]]={:workspaceId}
			AND [[page_id]]={:pageId} AND [[data_source_id]]={:dataSourceId}
			AND ` + databasePropertyNotTrashedSQL("[[id]]") + `
		ORDER BY [[position]], [[id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"owner": container.OwnerID, "workspaceId": container.WorkspaceID,
		"pageId": container.PageID, "dataSourceId": container.DataSource,
		"limit": maxDatabaseQueryProperties + 1,
	}).All(&rows)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 || len(rows) > maxDatabaseQueryProperties {
		return nil, errNotionDatabaseRowPageProjectionInvalid
	}
	ids, err := loadNotionPropertyIDs(app, container.DataSource)
	if err != nil {
		return nil, err
	}
	for index := range rows {
		rows[index].PublicID = ids.publicID(rows[index].ID)
	}
	return rows, nil
}

func projectNotionDataSourceProperty(
	app core.App,
	authorization notionDatabaseRowPageAuthorization,
	stored storedNotionDataSourceProperty,
	formula notionFormulaScope,
	byID map[string]storedNotionDataSourceProperty,
) (map[string]any, bool, error) {
	nativeType := normalizeNotionDatabaseRowPagePropertyType(stored.Type)
	officialType := notionDatabaseRowPageOfficialPropertyType(nativeType)
	config := map[string]any{}
	if json.Unmarshal([]byte(stored.Config), &config) != nil || config == nil ||
		stored.ID == "" || strings.TrimSpace(stored.Name) == "" {
		return nil, false, errNotionDatabaseRowPageProjectionInvalid
	}
	projectedConfig, visible, err := notionDataSourcePropertyConfig(
		app, authorization, stored, nativeType, config, formula, byID,
	)
	if err != nil || !visible {
		return nil, visible, err
	}
	property := map[string]any{
		"id": stored.rowPageProperty().publicID(), "name": stored.Name, "type": officialType,
		officialType: projectedConfig,
	}
	if description, ok := config["description"].(string); ok && description != "" {
		property["description"] = description
	} else {
		property["description"] = nil
	}
	return property, true, nil
}

func notionDataSourcePropertyConfig(
	app core.App,
	authorization notionDatabaseRowPageAuthorization,
	stored storedNotionDataSourceProperty,
	nativeType string,
	config map[string]any,
	formula notionFormulaScope,
	byID map[string]storedNotionDataSourceProperty,
) (any, bool, error) {
	switch nativeType {
	case graphValueTypeTitle, graphValueTypeRichText, graphValueTypeCheckbox,
		graphValueTypeDate, graphValueTypePerson, graphValueTypeFiles,
		graphValueTypeURL, graphValueTypeEmail, graphValueTypePhone,
		graphValueTypeCreatedTime, graphValueTypeLastEditedTime,
		graphValueTypeCreatedBy, graphValueTypeLastEditedBy,
		graphValueTypeButton, graphValueTypePlace, graphValueTypeVerification,
		graphValueTypeLastVisitedTime:
		return map[string]any{}, true, nil
	case graphValueTypeNumber:
		return map[string]any{"format": notionNumberFormatOf(config)}, true, nil
	case graphValueTypeSelect, graphValueTypeMultiSelect:
		options, err := notionDatabaseRowPageOptions(stored.rowPageProperty())
		if err != nil {
			return nil, false, err
		}
		return map[string]any{"options": notionDataSourceOptions(options)}, true, nil
	case graphValueTypeStatus:
		return notionDataSourceStatusConfig(stored, config)
	case graphValueTypeUniqueID:
		prefix, _ := config["idPrefix"].(string)
		if prefix == "" {
			return map[string]any{"prefix": nil}, true, nil
		}
		return map[string]any{"prefix": prefix}, true, nil
	case graphValueTypeFormula:
		expression, _ := config["formula"].(string)
		if strings.TrimSpace(expression) == "" {
			expression, _ = config["expression"].(string)
		}
		// A formula not written yet is an empty expression, as Notion's (seen 2026-10-04).
		if strings.TrimSpace(expression) == "" {
			return map[string]any{"expression": ""}, true, nil
		}
		byID, _ := config[notionFormulaByIDKey].(bool)
		return map[string]any{"expression": notionFormulaExpression(expression, formula, byID)}, true, nil
	case graphValueTypeRelation:
		target, visible, err := notionDataSourceRelationTarget(app, authorization, config)
		if err != nil || !visible {
			return nil, false, err
		}
		targetID := target.DataSource
		publicID, err := ensureNotionObjectIdentity(
			app, notionObjectIdentityDataSource, target.WorkspaceID, targetID,
		)
		if err != nil {
			return nil, false, err
		}
		// Notion names the database holding the related data source too (seen 2026-10-04, 2026-10-08).
		publicDatabaseID, err := ensureNotionObjectIdentity(
			app, notionObjectIdentityDatabase, target.WorkspaceID, target.ContainerID,
		)
		if err != nil {
			return nil, false, err
		}
		// A relation held to one page says so; one without a limit says null,
		// as Notion's schema does (seen 2026-11-09).
		var limit any
		switch value := config["limit"].(type) {
		case float64:
			if value == 1 {
				limit = 1
			}
		case int:
			if value == 1 {
				limit = 1
			}
		}
		// A two-way relation names the matching property on the target —
		// on its own data source, a second property; one that pairs with
		// itself is a single property, as Notion reads it.
		if pairID, _ := config["relatedPropertyId"].(string); pairID != "" && pairID != stored.ID {
			pair, pairConfig, exists, err := loadDatabaseRelationPairTargetProperty(app, authorization.Container.OwnerID, pairID)
			if err != nil {
				return nil, false, err
			}
			// The pair is named by its ID on its own data source, encoded as
			// a schema's (seen 2026-11-09: "%7B%7DSJ").
			targetIDs, err := loadNotionPropertyIDs(app, targetID)
			if err != nil {
				return nil, false, err
			}
			if exists && pair.DataSourceID == targetID && pairConfig["relatedPropertyId"] == stored.ID {
				return map[string]any{
					"database_id": publicDatabaseID, "data_source_id": publicID, "limit": limit, "type": "dual_property",
					"dual_property": map[string]any{"synced_property_id": targetIDs.publicID(pair.ID), "synced_property_name": pair.Name},
				}, true, nil
			}
			// A pair asked for and not made yet — the server makes it right
			// after the change — reads as it will be, a name taken there
			// numbered, as Notion answers the request that asks for it.
			if !exists {
				name, _ := config["reciprocalName"].(string)
				if strings.TrimSpace(name) == "" {
					if name, err = databaseRelationPairDefaultName(app, authorization.Container.OwnerID, authorization.Container.DataSource); err != nil {
						return nil, false, err
					}
				}
				if name, err = databaseRelationPairNameAsMade(app, authorization.Container.OwnerID, targetID, pairID, name); err != nil {
					return nil, false, err
				}
				return map[string]any{
					"database_id": publicDatabaseID, "data_source_id": publicID, "limit": limit, "type": "dual_property",
					"dual_property": map[string]any{"synced_property_id": targetIDs.publicID(pairID), "synced_property_name": name},
				}, true, nil
			}
		}
		return map[string]any{
			"database_id": publicDatabaseID, "data_source_id": publicID, "limit": limit, "type": "single_property",
			"single_property": map[string]any{},
		}, true, nil
	case graphValueTypeRollup:
		return notionDataSourceRollupConfig(app, authorization, stored, byID)
	default:
		return nil, false, &notionDatabaseRowPagePropertyTypeError{Type: nativeType}
	}
}

// notionDataSourceRelationTarget is the data source a relation reaches, when
// the caller may read it and it is in the same workspace.
func notionDataSourceRelationTarget(
	app core.App,
	authorization notionDatabaseRowPageAuthorization,
	config map[string]any,
) (notionDataSourceParentAuthority, bool, error) {
	targetID, _ := config["relationDatabaseId"].(string)
	target, err := authorizeNotionDataSourceParentCurrent(
		app, authorization.Identity, targetID, permissionWorkspaceRead,
	)
	if errors.Is(err, errDatabaseRowPageNotFound) || isPermissionDenied(err) {
		return notionDataSourceParentAuthority{}, false, nil
	}
	if err != nil {
		return notionDataSourceParentAuthority{}, false, err
	}
	if target.Container.WorkspaceID != authorization.Container.WorkspaceID {
		return notionDataSourceParentAuthority{}, false, nil
	}
	return target.Container, true, nil
}

// notionDataSourceRollupConfig names the relation a rollup follows and the
// property it rolls up, as Notion does. Notion leaves out a rollup whose
// relation is gone (seen 2026-10-04), and its API can't describe one not set
// up yet, so a rollup without a relation the caller sees, a property on the
// related data source or a calculation is left out too.
func notionDataSourceRollupConfig(
	app core.App,
	authorization notionDatabaseRowPageAuthorization,
	stored storedNotionDataSourceProperty,
	byID map[string]storedNotionDataSourceProperty,
) (any, bool, error) {
	config, err := decodeDatabaseComputedConfig(stored.Config)
	if err != nil {
		return nil, false, errNotionDatabaseRowPageProjectionInvalid
	}
	relationID, targetID, function := config.rollupFields()
	relation, ok := byID[relationID]
	if !ok || normalizeNotionDatabaseRowPagePropertyType(relation.Type) != graphValueTypeRelation ||
		targetID == "" || function == "" {
		return nil, false, nil
	}
	relationConfig := map[string]any{}
	if json.Unmarshal([]byte(relation.Config), &relationConfig) != nil || relationConfig == nil {
		return nil, false, errNotionDatabaseRowPageProjectionInvalid
	}
	target, visible, err := notionDataSourceRelationTarget(app, authorization, relationConfig)
	if err != nil || !visible {
		return nil, false, err
	}
	var targetName string
	err = app.DB().NewQuery(`
		SELECT [[name]] FROM {{leafen_database_properties}}
		WHERE [[id]]={:id} AND [[owner]]={:owner} AND [[data_source_id]]={:dataSourceId}
			AND ` + databasePropertyNotTrashedSQL("[[id]]") + `
	`).Bind(dbx.Params{
		"id": targetID, "owner": target.OwnerID, "dataSourceId": target.DataSource,
	}).Row(&targetName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	// A rollup names both by their IDs as they are, not encoded (seen
	// 2026-10-04 and 2026-11-09: a schema's "y%7C%3B%5C" is a rollup's "y|;\").
	targetIDs, err := loadNotionPropertyIDs(app, target.DataSource)
	if err != nil {
		return nil, false, err
	}
	return map[string]any{
		"relation_property_name": relation.Name, "relation_property_id": notionPropertyRawID(relation.rowPageProperty().publicID()),
		"rollup_property_name": targetName, "rollup_property_id": notionPropertyRawID(targetIDs.publicID(targetID)),
		"function": function,
	}, true, nil
}

func (stored storedNotionDataSourceProperty) rowPageProperty() storedNotionDatabaseRowPageProperty {
	return storedNotionDatabaseRowPageProperty{
		ID: stored.ID, Type: stored.Type, Name: stored.Name, Config: stored.Config, PublicID: stored.PublicID,
	}
}

func notionDataSourceOptions(options []notionDatabaseRowPageOption) []map[string]any {
	result := make([]map[string]any, len(options))
	for index, option := range options {
		result[index] = option.response()
		// Notion's schema gives each option its description, null with none
		// (measured 2026-11-09: "about it", or null).
		if option.Description != nil {
			result[index]["description"] = *option.Description
		} else {
			result[index]["description"] = nil
		}
	}
	return result
}

// notionTitlePropertyID is a title property's ID in Notion's API, whichever
// database it is in.
const notionTitlePropertyID = "title"

func notionDataSourceStatusConfig(
	stored storedNotionDataSourceProperty,
	config map[string]any,
) (any, bool, error) {
	options, err := notionDatabaseRowPageOptions(stored.rowPageProperty())
	if err != nil {
		return nil, false, err
	}
	groups, ok := config["statusGroups"].([]any)
	if !ok {
		groups = []any{}
	}
	groupMaps, err := notionStatusConfigMaps(groups)
	if err != nil {
		return nil, false, err
	}
	projectedGroups := make([]map[string]any, 0, len(groups))
	for groupIndex, value := range groups {
		group, ok := value.(map[string]any)
		if !ok {
			return nil, false, errNotionDatabaseRowPageProjectionInvalid
		}
		id, _ := group["id"].(string)
		name, _ := group["name"].(string)
		color, _ := group["color"].(string)
		optionValues, _ := group["optionIds"].([]any)
		optionIDs := make([]string, 0, len(optionValues))
		for _, optionValue := range optionValues {
			optionID, ok := optionValue.(string)
			if !ok || optionID == "" {
				return nil, false, errNotionDatabaseRowPageProjectionInvalid
			}
			optionIDs = append(optionIDs, optionID)
		}
		if id == "" || name == "" {
			return nil, false, errNotionDatabaseRowPageProjectionInvalid
		}
		if position := notionStatusGroupIndex(groupMaps, groupIndex); position >= 0 {
			name = notionStatusGroupNames[position]
		}
		projectedGroups = append(projectedGroups, map[string]any{
			"id": id, "name": name, "color": color, "option_ids": optionIDs,
		})
	}
	// Its default by name, which its rows that set none read; null for none,
	// as Notion's API gives a status made from a select (seen 2026-11-08).
	defaultOption := any(nil)
	if fallback := databaseStatusDefaultOptionID(stored.Config); fallback != "" {
		if option, found := notionDatabaseRowPageOptionByID(options, fallback); found {
			defaultOption = option.Name
		}
	}
	return map[string]any{
		"options": notionDataSourceOptions(options), "groups": projectedGroups, "default_option": defaultOption,
	}, true, nil
}

// notionDataSourceFormulaScope is what the data source's formulas read, as
// the Public API names it.
func notionDataSourceFormulaScope(
	app core.App,
	container notionDataSourceParentAuthority,
	rows []storedNotionDataSourceProperty,
) (notionFormulaScope, error) {
	dataSourceID, err := ensureNotionObjectIdentity(
		app, notionObjectIdentityDataSource, container.WorkspaceID, container.DataSource,
	)
	if err != nil {
		return notionFormulaScope{}, err
	}
	workspaceUUID, err := notionCompatibleWorkspaceID(container.WorkspaceID)
	if err != nil {
		return notionFormulaScope{}, err
	}
	properties := make([]databaseFormulaReferenceProperty, 0, len(rows))
	for _, row := range rows {
		properties = append(properties, databaseFormulaReferenceProperty{
			ID: row.ID, Type: row.Type, Name: row.Name, Position: row.Position,
		})
	}
	ids, err := loadNotionPropertyIDs(app, container.DataSource)
	if err != nil {
		return notionFormulaScope{}, err
	}
	return notionFormulaScope{
		DataSourceID: dataSourceID, WorkspaceID: container.WorkspaceID, WorkspaceUUID: workspaceUUID,
		Names: newDatabaseFormulaReferenceNames(properties, nil), IDs: ids,
	}, nil
}

// notionNumberFormatOf is a number property's format as Notion's API names
// it in number.format: "number" when none was chosen, and Leafen's "comma"
// as "number_with_commas".
func notionNumberFormatOf(config map[string]any) string {
	if format, chosen := notionChosenNumberFormat(config); chosen {
		return format
	}
	return "number"
}

// notionChosenNumberFormat is the format someone chose for a number property,
// as Notion names it, and whether one was. Notion's MCP names only a chosen
// one (number_format), "number" too when picked; a property added without
// one — a bare NUMBER, the API's {"number":{}}, the "+" menu — has none
// (seen 2026-11-14 in its fetch, update-data-source and view-version).
func notionChosenNumberFormat(config map[string]any) (string, bool) {
	format, _ := config["numberFormat"].(string)
	switch format {
	case "":
		return "", false
	case "comma":
		return "number_with_commas", true
	}
	return format, true
}
