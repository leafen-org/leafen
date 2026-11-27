package app

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type formRelationSelection struct {
	Property     storedFormProperty
	TargetSource string
	ReciprocalID string
	TargetIDs    []string
}

type storedFormSubmissionRow struct {
	ID              string `db:"id"`
	Owner           string `db:"owner"`
	WorkspaceID     string `db:"workspace_id"`
	PageID          string `db:"page_id"`
	DataSourceID    string `db:"data_source_id"`
	SubitemParentID string `db:"subitem_parent_id"`
	Position        string `db:"position"`
}

type storedFormReciprocalProperty struct {
	ID           string `db:"id"`
	Owner        string `db:"owner"`
	PageID       string `db:"page_id"`
	DataSourceID string `db:"data_source_id"`
	Type         string `db:"type"`
	Config       string `db:"config"`
}

type storedFormReciprocalValue struct {
	RowID      string `db:"row_id"`
	PropertyID string `db:"property_id"`
	Type       string `db:"type"`
	Value      string `db:"value"`
}

func prepareFormSubmissionReferences(
	app core.App,
	principalID string,
	link storedFormLink,
	source formSource,
	rowID string,
	values map[string]graphRowValue,
) ([]*graphDatabaseRow, error) {
	people := []string{}
	relations := []formRelationSelection{}
	for _, property := range source.Properties {
		value, present := values[property.ID]
		if !present {
			continue
		}
		switch property.Type {
		case graphValueTypePerson:
			ids, err := decodeFormStringArray(value.Value)
			if err != nil {
				return nil, formInvalid("answers.%s is a list of user ids", property.ID)
			}
			people = append(people, ids...)
		case graphValueTypeRelation:
			ids, err := decodeFormStringArray(value.Value)
			if err != nil {
				return nil, formInvalid("answers.%s is a list of page ids", property.ID)
			}
			targetSource, reciprocalID, configErr := formRelationConfiguration(property, link.DataSourceID)
			if configErr != nil {
				return nil, configErr
			}
			relations = append(relations, formRelationSelection{
				Property: property, TargetSource: targetSource,
				ReciprocalID: reciprocalID, TargetIDs: ids,
			})
		}
	}
	if err := validateFormSubmissionPeople(app, link, uniqueStrings(people)); err != nil {
		return nil, err
	}
	if len(relations) == 0 {
		return []*graphDatabaseRow{}, nil
	}
	return prepareFormReciprocalRows(app, principalID, link, rowID, relations)
}

func validateFormSubmissionPeople(app core.App, link storedFormLink, userIDs []string) error {
	if len(userIDs) == 0 {
		return nil
	}
	params, placeholders := formSQLValues("person", userIDs)
	params["workspaceId"] = link.WorkspaceID
	rows := []struct {
		ID string `db:"id"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[user]].[[id]]
		FROM {{users}} [[user]]
		JOIN {{leafen_workspaces}} [[workspace]] ON [[workspace]].[[id]] = {:workspaceId}
		JOIN {{leafen_organization_memberships}} [[organization_member]]
			ON [[organization_member]].[[organization]] = [[workspace]].[[organization]]
			AND [[organization_member]].[[user]] = [[user]].[[id]]
			AND [[organization_member]].[[status]] = 'active'
		LEFT JOIN {{leafen_workspace_memberships}} [[workspace_member]]
			ON [[workspace_member]].[[workspace_id]] = [[workspace]].[[id]]
			AND [[workspace_member]].[[user_id]] = [[user]].[[id]]
		WHERE [[user]].[[id]] IN (` + joinSQL(placeholders) + `)
			AND ([[workspace]].[[workspace_owner]] = [[user]].[[id]]
				OR [[workspace_member]].[[user_id]] IS NOT NULL)
		ORDER BY [[user]].[[id]]
	`).Bind(params).All(&rows)
	if err != nil {
		return err
	}
	if len(rows) != len(userIDs) {
		return formPersonNotOffered
	}
	for index, id := range userIDs {
		if rows[index].ID != id {
			return formPersonNotOffered
		}
	}
	return nil
}

func prepareFormReciprocalRows(
	app core.App,
	principalID string,
	link storedFormLink,
	rowID string,
	relations []formRelationSelection,
) ([]*graphDatabaseRow, error) {
	targetIDs := []string{}
	for _, relation := range relations {
		targetIDs = append(targetIDs, relation.TargetIDs...)
	}
	targetIDs = uniqueStrings(targetIDs)
	rows, err := loadFormSubmissionRows(app, targetIDs)
	if err != nil {
		return nil, err
	}
	rowByID := make(map[string]storedFormSubmissionRow, len(rows))
	pageIDs := []string{}
	for _, row := range rows {
		if row.Owner != link.Owner || row.WorkspaceID != link.WorkspaceID {
			return nil, formPageNotOffered
		}
		rowByID[row.ID] = row
		pageIDs = append(pageIDs, row.PageID)
	}
	if len(rowByID) != len(targetIDs) {
		return nil, formPageNotOffered
	}
	// Nor an unlinked item the person has no role in, which the form never
	// offered them (database_row_unlinked_access.go).
	items, err := unlinkedRowAccessFor(app, principalID, targetIDs)
	if err != nil {
		return nil, err
	}
	for _, targetID := range targetIDs {
		if items.hides(targetID) {
			return nil, formPageNotOffered
		}
	}
	pageIDs = uniqueStrings(pageIDs)
	if _, err := authorizePageSet(
		app, principalID, link.WorkspaceID, pageIDs, permissionWorkspaceRead,
	); err != nil {
		if isPermissionDenied(err) {
			return nil, formPageNotOffered
		}
		return nil, err
	}
	for _, pageID := range pageIDs {
		if pageID != link.PageID {
			return nil, errFormConflict
		}
	}
	for _, relation := range relations {
		for _, targetID := range relation.TargetIDs {
			if rowByID[targetID].DataSourceID != relation.TargetSource {
				return nil, formPageNotOffered
			}
		}
	}

	reciprocalIDs := []string{}
	for _, relation := range relations {
		if relation.ReciprocalID != "" {
			reciprocalIDs = append(reciprocalIDs, relation.ReciprocalID)
		}
	}
	reciprocalIDs = uniqueStrings(reciprocalIDs)
	properties, err := loadFormReciprocalProperties(app, reciprocalIDs)
	if err != nil {
		return nil, err
	}
	propertyByID := make(map[string]storedFormReciprocalProperty, len(properties))
	for _, property := range properties {
		propertyByID[property.ID] = property
	}
	for _, relation := range relations {
		if relation.ReciprocalID == "" {
			continue
		}
		property, exists := propertyByID[relation.ReciprocalID]
		if !exists || property.Owner != link.Owner || property.PageID != link.PageID ||
			property.DataSourceID != relation.TargetSource || property.Type != graphValueTypeRelation {
			return nil, errFormConflict
		}
		target, _, configErr := formRelationConfiguration(
			storedFormProperty{Type: property.Type, Config: property.Config}, relation.TargetSource,
		)
		if configErr != nil || target != link.DataSourceID {
			return nil, errFormConflict
		}
	}

	current, err := loadFormReciprocalValues(app, targetIDs, reciprocalIDs)
	if err != nil {
		return nil, err
	}
	valuesByRow := map[string]map[string]graphRowValue{}
	for _, stored := range current {
		if stored.Type != graphValueTypeRelation {
			return nil, errFormConflict
		}
		ids, decodeErr := decodeFormStringArray(json.RawMessage(stored.Value))
		if decodeErr != nil {
			return nil, errFormConflict
		}
		encoded, _ := json.Marshal(ids)
		if valuesByRow[stored.RowID] == nil {
			valuesByRow[stored.RowID] = map[string]graphRowValue{}
		}
		valuesByRow[stored.RowID][stored.PropertyID] = graphRowValue{
			Type: graphValueTypeRelation, Value: encoded,
		}
	}
	for _, relation := range relations {
		if relation.ReciprocalID == "" {
			continue
		}
		for _, targetID := range relation.TargetIDs {
			if valuesByRow[targetID] == nil {
				valuesByRow[targetID] = map[string]graphRowValue{}
			}
			currentIDs := []string{}
			if currentValue, exists := valuesByRow[targetID][relation.ReciprocalID]; exists {
				currentIDs, _ = decodeFormStringArray(currentValue.Value)
			}
			if !containsFormString(currentIDs, rowID) {
				currentIDs = append(currentIDs, rowID)
			}
			encoded, _ := json.Marshal(currentIDs)
			valuesByRow[targetID][relation.ReciprocalID] = graphRowValue{
				Type: graphValueTypeRelation, Value: encoded,
			}
		}
	}
	result := make([]*graphDatabaseRow, 0, len(valuesByRow))
	for _, targetID := range targetIDs {
		values := valuesByRow[targetID]
		if len(values) == 0 {
			continue
		}
		stored := rowByID[targetID]
		var parent *string
		if stored.SubitemParentID != "" {
			value := stored.SubitemParentID
			parent = &value
		}
		result = append(result, &graphDatabaseRow{
			ID: stored.ID, DataSourceID: stored.DataSourceID,
			SubitemParentID: parent, Position: stored.Position, Values: values,
		})
	}
	return result, nil
}

func formRelationConfiguration(
	property storedFormProperty,
	fallbackSource string,
) (string, string, error) {
	config := struct {
		RelationDatabaseID string `json:"relationDatabaseId"`
		RelatedPropertyID  string `json:"relatedPropertyId"`
	}{}
	if json.Unmarshal([]byte(property.Config), &config) != nil {
		return "", "", errFormConflict
	}
	target := strings.TrimSpace(config.RelationDatabaseID)
	if target == "" {
		target = fallbackSource
	}
	if _, err := normalizeFormIdentifier(target); err != nil {
		return "", "", errFormConflict
	}
	reciprocal := strings.TrimSpace(config.RelatedPropertyID)
	if reciprocal != "" {
		if _, err := normalizeFormIdentifier(reciprocal); err != nil {
			return "", "", errFormConflict
		}
	}
	return target, reciprocal, nil
}

func loadFormSubmissionRows(app core.App, ids []string) ([]storedFormSubmissionRow, error) {
	if len(ids) == 0 {
		return []storedFormSubmissionRow{}, nil
	}
	params, placeholders := formSQLValues("row", ids)
	rows := []storedFormSubmissionRow{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[owner]], [[workspace_id]], [[page_id]], [[data_source_id]],
			[[subitem_parent_id]], [[position]]
		FROM {{leafen_database_rows}} WHERE [[id]] IN (` + joinSQL(placeholders) + `)
		ORDER BY [[id]]
	`).Bind(params).All(&rows)
	return rows, err
}

func loadFormReciprocalProperties(app core.App, ids []string) ([]storedFormReciprocalProperty, error) {
	if len(ids) == 0 {
		return []storedFormReciprocalProperty{}, nil
	}
	params, placeholders := formSQLValues("property", ids)
	rows := []storedFormReciprocalProperty{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[owner]], [[page_id]], [[data_source_id]], [[type]], [[config]]
		FROM {{leafen_database_properties}} WHERE [[id]] IN (` + joinSQL(placeholders) + `)
		ORDER BY [[id]]
	`).Bind(params).All(&rows)
	if err == nil && len(rows) != len(ids) {
		return nil, errFormConflict
	}
	return rows, err
}

func loadFormReciprocalValues(
	app core.App,
	rowIDs, propertyIDs []string,
) ([]storedFormReciprocalValue, error) {
	if len(rowIDs) == 0 || len(propertyIDs) == 0 {
		return []storedFormReciprocalValue{}, nil
	}
	params, rowPlaceholders := formSQLValues("valueRow", rowIDs)
	propertyParams, propertyPlaceholders := formSQLValues("valueProperty", propertyIDs)
	for key, value := range propertyParams {
		params[key] = value
	}
	rows := []storedFormReciprocalValue{}
	err := app.DB().NewQuery(`
		SELECT [[row_id]], [[property_id]], [[type]], [[value]]
		FROM {{leafen_database_row_values}}
		WHERE [[row_id]] IN (` + joinSQL(rowPlaceholders) + `)
			AND [[property_id]] IN (` + joinSQL(propertyPlaceholders) + `)
		ORDER BY [[row_id]], [[property_id]]
	`).Bind(params).All(&rows)
	return rows, err
}

// formPersonNotOffered and formPageNotOffered refuse an answer that
// names someone or a page the question doesn't offer.
var (
	formPersonNotOffered = formInvalid("a person answer names someone who isn't a member of the form's workspace: POST /forms {\"action\": \"options\"} lists whom it offers")
	formPageNotOffered   = formInvalid("a relation answer names a page the question doesn't offer: POST /forms {\"action\": \"options\"} lists them")
)

func decodeFormStringArray(raw json.RawMessage) ([]string, error) {
	values := []string{}
	if json.Unmarshal(raw, &values) != nil {
		return nil, errFormInvalid
	}
	return values, nil
}

func formSQLValues(prefix string, values []string) (dbx.Params, []string) {
	params := dbx.Params{}
	placeholders := make([]string, len(values))
	for index, value := range values {
		key := fmt.Sprintf("%s%d", prefix, index)
		params[key] = value
		placeholders[index] = "{:" + key + "}"
	}
	return params, placeholders
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func containsFormString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
