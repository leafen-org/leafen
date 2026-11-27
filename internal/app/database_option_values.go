package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An agent's or the API's value of a select, multi-select or status names
// options its column has (2026-11-26: POST /database-rows/{rowId}/page/values
// took an ID the column didn't have, saved it, and the row read empty). One
// it doesn't have is refused, naming the column and the ID, in the one step
// that checks a change's options (database_option_removal.go): the native
// row values, many rows at once, a content change and notion-update-data-
// source alike; the Notion-compatible API and MCP's page tools name options
// by name or ID and refuse one they don't find already
// (notionDatabaseRowPageRequestOption). An option the same change adds to
// the column is one it has. The screen's own writes are not refused: one
// naming an option someone took away meanwhile reads empty, as before.

const errorCodeDatabaseOptionUnknown = "LEAFEN_DATABASE_OPTION_UNKNOWN"

// refuseUnknownDatabaseOptionValues refuses a change whose rows' values of a
// select, multi-select or status name an option their column doesn't have
// once the change is made.
func refuseUnknownDatabaseOptionValues(app core.App, operations []graphOperation) error {
	written := map[string]*graphProperty{}
	for _, operation := range operations {
		if operation.Kind == graphOperationKindUpsertProperty && operation.Property != nil {
			written[operation.Property.ID] = operation.Property
		}
	}
	held := map[string]map[string]bool{}
	names := map[string]string{}
	optionsOf := func(propertyID string) (map[string]bool, error) {
		if options, read := held[propertyID]; read {
			return options, nil
		}
		config := ""
		if property := written[propertyID]; property != nil {
			config, names[propertyID] = string(property.Config), property.Name
		} else {
			stored := struct {
				Name   string `db:"name"`
				Config string `db:"config"`
			}{}
			err := app.DB().NewQuery(`SELECT [[name]], [[config]] FROM {{leafen_database_properties}} WHERE [[id]]={:id}`).
				Bind(dbx.Params{"id": propertyID}).One(&stored)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
			config, names[propertyID] = stored.Config, stored.Name
		}
		parsed := struct {
			Options []struct {
				ID string `json:"id"`
			} `json:"options"`
		}{}
		_ = json.Unmarshal([]byte(config), &parsed)
		options := map[string]bool{}
		for _, option := range parsed.Options {
			options[strings.TrimSpace(option.ID)] = true
		}
		held[propertyID] = options
		return options, nil
	}
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertRow || operation.Row == nil {
			continue
		}
		for propertyID, value := range operation.Row.Values {
			if _, options := databasePropertyConversionOptionTypes[value.Type]; !options || graphRowValueIsCleared(value.Value) {
				continue
			}
			ids := []string{}
			if value.Type == graphValueTypeMultiSelect {
				_ = json.Unmarshal(value.Value, &ids)
			} else {
				id := ""
				_ = json.Unmarshal(value.Value, &id)
				ids = append(ids, id)
			}
			options, err := optionsOf(propertyID)
			if err != nil {
				return err
			}
			for _, id := range ids {
				if id = strings.TrimSpace(id); id != "" && !options[id] {
					return refusedFor(http.StatusBadRequest, errorCodeDatabaseOptionUnknown, fmt.Sprintf(
						"Check values.%s: column %s of row %s has no option with the id %s, so nothing was changed. "+
							"Give one of its options' ids (GET /pages/{id}/content-graph names them), add the option with the value (options), "+
							"or name it through MCP's or the compatible API's page values.",
						propertyID, strconvQuoteJS(names[propertyID]), strconvQuoteJS(operation.Row.ID), strconvQuoteJS(id)))
				}
			}
		}
	}
	return nil
}
