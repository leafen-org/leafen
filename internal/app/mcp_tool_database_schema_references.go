package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Notion's CREATE TABLE also writes columns that refer to something (its
// MCP's create-database and update-data-source, 2026-10-08):
// FORMULA('expression'), RELATION('ds') — RELATION('ds', DUAL) or
// RELATION('ds', DUAL 'name') for a two-way one — and ROLLUP('relation',
// 'property', 'function'), each by name. Leafen resolves the names to the
// data source and properties they mean, in the caller's workspace and
// reach; a self-relation's DUAL makes a second property on it, and its
// pair spelled ('name' 'id') isn't taken yet.

var mcpDatabaseRollupFunctions = map[string]bool{
	"show_original": true, "show_unique": true, "count": true, "count_values": true, "unique": true, "empty": true,
	"not_empty": true, "percent_empty": true, "percent_not_empty": true, "sum": true, "average": true, "median": true,
	"min": true, "max": true, "range": true, "earliest_date": true, "latest_date": true, "date_range": true,
	"checked": true, "unchecked": true, "percent_checked": true, "percent_unchecked": true,
	"count_per_group": true, "percent_per_group": true,
}

// parseMCPDatabaseSchemaArguments reads a formula's, relation's or
// rollup's arguments up to the closing parenthesis.
func parseMCPDatabaseSchemaArguments(body string, offset int, property *mcpDatabaseSchemaProperty) (int, bool) {
	quoted := func() (string, bool) {
		offset = skipMCPDatabaseSchemaSpace(body, offset)
		text, next, ok := readMCPDatabaseSchemaQuoted(body, offset)
		offset = next
		return text, ok && strings.TrimSpace(text) != ""
	}
	comma := func() bool {
		offset = skipMCPDatabaseSchemaSpace(body, offset)
		if offset < len(body) && body[offset] == ',' {
			offset++
			return true
		}
		return false
	}
	var ok bool
	switch property.Type {
	case graphValueTypeFormula:
		if property.Formula, ok = quoted(); !ok || utf8.RuneCountInString(property.Formula) > 8000 || !notionFormulaParses(property.Formula) {
			return offset, false
		}
	case graphValueTypeRelation:
		if property.Relation, ok = quoted(); !ok {
			return offset, false
		}
		if comma() {
			offset = skipMCPDatabaseSchemaSpace(body, offset)
			if len(body)-offset < 4 || !strings.EqualFold(body[offset:offset+4], "DUAL") {
				return offset, false
			}
			offset += 4
			property.Dual = true
			offset = skipMCPDatabaseSchemaSpace(body, offset)
			if offset < len(body) && body[offset] == '\'' {
				if property.SyncedName, ok = quoted(); !ok || utf8.RuneCountInString(property.SyncedName) > maxGraphIdentifierLength {
					return offset, false
				}
			}
		}
	case graphValueTypeRollup:
		if property.RollupRelation, ok = quoted(); !ok || !comma() {
			return offset, false
		}
		if property.RollupTarget, ok = quoted(); !ok || !comma() {
			return offset, false
		}
		if property.RollupFunction, ok = quoted(); !ok || !mcpDatabaseRollupFunctions[property.RollupFunction] {
			return offset, false
		}
	default:
		return offset, false
	}
	offset = skipMCPDatabaseSchemaSpace(body, offset)
	if offset >= len(body) || body[offset] != ')' {
		return offset, false
	}
	return offset + 1, true
}

type mcpSchemaKnownProperty struct {
	id, valueType, relationSource string
}

// resolveMCPDatabaseSchemaReferences ties new columns' relations and
// rollups to what they name: a relation's data source, readable in the
// same workspace, and a rollup's relation (on this data source, already
// there or earlier among the new columns) and the property it reads on the
// related one. A data source keeps one unique ID.
func resolveMCPDatabaseSchemaReferences(app core.App, principalID, workspaceID, dataSourceID string,
	properties []mcpDatabaseSchemaProperty, propertyIDs []string, existing []storedMCPDataSourceProperty) error {
	var translate func(string) string
	for index := range properties {
		if properties[index].Type == graphValueTypeStatus && len(properties[index].Options) == 0 {
			if translate == nil {
				translate = newStatusWords(app, principalID)
			}
			properties[index].translate = translate
		}
	}
	known := map[string]mcpSchemaKnownProperty{}
	uniques := 0
	for _, property := range existing {
		config := map[string]any{}
		_ = json.Unmarshal([]byte(property.Config), &config)
		source, _ := config["relationDatabaseId"].(string)
		known[property.Name] = mcpSchemaKnownProperty{id: property.ID, valueType: property.Type, relationSource: source}
		if property.Type == graphValueTypeUniqueID {
			uniques++
		}
	}
	for index := range properties {
		property := &properties[index]
		switch property.Type {
		case graphValueTypeRelation:
			source, ok := mcpLocalDataSourceID(app, property.Relation)
			if !ok {
				return graphInputError(errorCodeGraphInvalid, "RELATION('…') names a data source by the ID notion-fetch gives (collection://…); "+
					"a database being made has none yet, so for a relation to itself make the database first, then add the relation with notion-update-data-source.")
			}
			if source != dataSourceID {
				location, err := loadMCPDataSourceLocation(app, source)
				if err == nil && location.WorkspaceID != workspaceID {
					return mcpLinkedAcross(app, "a relation", property.Relation, location.WorkspaceID, workspaceID)
				}
				if err != nil {
					return graphInputError(errorCodeGraphReferenceInvalid, "The relation's data source isn't one in this workspace: RELATION('…') names it by the ID "+
						"notion-fetch gives (collection://…). A database being made has none yet, so for a relation to itself make it first, then add the relation with notion-update-data-source.")
				}
				if _, err := authorizePageBody(app, principalID, location.PageID, permissionWorkspaceRead); err != nil {
					return graphInputError(errorCodeGraphReferenceInvalid, "The relation's data source isn't in this workspace.")
				}
			}
			// DUAL makes the matching property, on its own data source a
			// second one kept in step, as Notion's API makes it (seen 2026-10-11).
			if property.Dual {
				property.pairID = mcpEntityID(propertyIDs[index] + "\x00relation-pair")
			}
			property.relationSource = source
		case graphValueTypeRollup:
			relation, ok := known[property.RollupRelation]
			if !ok || relation.valueType != graphValueTypeRelation || relation.relationSource == "" {
				return graphInputError(errorCodeGraphReferenceInvalid, "The rollup's relation isn't a relation property here.")
			}
			var targetID, targetType string
			err := app.DB().NewQuery(`SELECT [[id]], [[type]] FROM {{leafen_database_properties}}
				WHERE [[data_source_id]] = {:source} AND [[name]] = {:name} AND `+databasePropertyNotTrashedSQL("[[id]]")+` LIMIT 1`).
				Bind(dbx.Params{"source": relation.relationSource, "name": property.RollupTarget}).Row(&targetID, &targetType)
			if errors.Is(err, sql.ErrNoRows) {
				return graphInputError(errorCodeGraphReferenceInvalid, "The rollup's property isn't on the related data source.")
			}
			if err != nil {
				return err
			}
			// A rollup of a rollup is refused as Notion's API refuses it
			// (resolveNotionRollupRequests), as the app's list leaves rollups out.
			if targetType == graphValueTypeRollup {
				return graphInputError(errorCodeGraphReferenceInvalid,
					"Rollup property "+property.Name+" can't roll up a rollup of the related database.")
			}
			property.rollupRelationID, property.rollupTargetID = relation.id, targetID
		case graphValueTypeUniqueID:
			if uniques++; uniques > 1 {
				return graphInputError(errorCodeGraphInvalid, "A data source has at most one unique ID property.")
			}
		}
		known[property.Name] = mcpSchemaKnownProperty{id: propertyIDs[index], valueType: property.Type, relationSource: property.relationSource}
	}
	return nil
}

// mcpDatabaseSchemaHasSettings says a column carries more than its type.
func mcpDatabaseSchemaHasSettings(column mcpDatabaseSchemaProperty) bool {
	return len(column.Options) > 0 || column.NumberFormat != "" || column.Description != "" || column.Formula != "" ||
		column.Relation != "" || column.RollupRelation != "" || column.IDPrefix != "" || column.Type == graphValueTypeUniqueID
}
