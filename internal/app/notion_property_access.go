package app

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

// The Notion-compatible API reads a database under its property access
// rules (database_property_access.go) for the connection's account, as
// Notion's API answers an integration (seen 2026-10-12): a property shown
// to nobody in its place leaves the data source's schema and the page's
// properties, and one shown without its values reads as empty. Filtering or
// sorting by it is refused, as an unknown property is, and so is changing
// what the account may not change.

type notionRowPropertyAccess struct {
	reader       *databasePropertyAccessReader
	dataSourceID string
	row          *databasePropertyAccessRow
}

// newNotionRowPropertyAccess reads the rules for one row; nil where its
// database has none.
func newNotionRowPropertyAccess(app core.App, readerID string, authority databaseRowPageAuthority,
	stored []storedNotionDatabaseRowPageProperty) (*notionRowPropertyAccess, error) {
	reader, err := newDatabasePropertyAccessReader(app, readerID, []string{authority.DataSourceID})
	if err != nil || reader == nil {
		return nil, err
	}
	values := map[string][]byte{}
	for _, property := range stored {
		if property.Value.Valid {
			values[property.ID] = []byte(property.Value.String)
		}
	}
	return &notionRowPropertyAccess{reader: reader, dataSourceID: authority.DataSourceID, row: &databasePropertyAccessRow{
		ID: authority.RowID, CreatedBy: authority.CreatedBy,
		People: func(propertyID string) []string { return peopleInValue(values[propertyID]) },
	}}, nil
}

// shows is whether the property shows in the row, and whether its value does.
func (access *notionRowPropertyAccess) shows(propertyID string) (bool, bool, error) {
	if access == nil || !access.reader.restricted(access.dataSourceID, propertyID) {
		return true, true, nil
	}
	level, err := access.reader.level(access.dataSourceID, propertyID, access.row)
	if err != nil || propertyAccessShowsValues(level) {
		return err == nil, err == nil, err
	}
	shown, err := access.reader.propertyShown(access.dataSourceID, propertyID)
	return shown, false, err
}

// redact empties the values the reader may not see, reporting the
// properties that leave the answer and those that read as empty.
func (access *notionRowPropertyAccess) redact(rows []storedNotionDatabaseRowPageProperty,
	computed map[string]databaseComputedValue) (map[string]bool, map[string]bool, error) {
	gone, empty := map[string]bool{}, map[string]bool{}
	if access == nil {
		return gone, empty, nil
	}
	for index := range rows {
		shown, values, err := access.shows(rows[index].ID)
		if err != nil {
			return nil, nil, err
		}
		if !shown {
			gone[rows[index].ID] = true
		}
		if !values {
			rows[index].ValueType, rows[index].Value, rows[index].Spans = sql.NullString{}, sql.NullString{}, sql.NullString{}
			// A formula keeps the kind of its result for its empty value.
			if normalizeNotionDatabaseRowPagePropertyType(rows[index].Type) != graphValueTypeFormula {
				delete(computed, rows[index].ID)
			}
			empty[rows[index].ID] = true
		}
	}
	return gone, empty, nil
}

// projectNotionRowProperty projects a property's value, or, for a formula or
// rollup whose value the reader may not see, its kind with no value.
func projectNotionRowProperty(app core.App, authorization notionDatabaseRowPageAuthorization, stored storedNotionDatabaseRowPageProperty,
	computed map[string]databaseComputedValue, computedUnsupported bool, fileIssuer *notionBlockFileCapabilityIssuer,
	empty bool) (notionDatabaseRowPageProjectedProperty, error) {
	propertyType := normalizeNotionDatabaseRowPagePropertyType(stored.Type)
	if empty && (propertyType == graphValueTypeFormula || propertyType == graphValueTypeRollup) {
		officialType := notionDatabaseRowPageOfficialPropertyType(propertyType)
		var value any
		// A formula reads as its result's kind with no value, as Notion's
		// (seen 2026-10-12: "formula": {"type": "number", "number": null}).
		if result, found := computed[stored.ID]; found && propertyType == graphValueTypeFormula {
			if projected, err := notionDatabaseRowPageFormulaValue(stored, result, computedUnsupported); err == nil {
				if kind, ok := projected["type"].(string); ok {
					value = map[string]any{"type": kind, kind: nil}
				}
			}
		}
		return notionDatabaseRowPageProjectedProperty{ID: stored.ID, PublicID: stored.publicID(), Name: stored.Name, Type: officialType,
			PageValue: map[string]any{"id": stored.publicID(), "type": officialType, officialType: value}}, nil
	}
	return projectNotionDatabaseRowPageProperty(app, authorization, stored, computed, computedUnsupported, fileIssuer)
}

// notionSchemaPropertyHidden is whether a data source's property leaves its
// schema for the reader: no row shows it to them.
func notionSchemaPropertyHidden(reader *databasePropertyAccessReader, dataSourceID, propertyID string) (bool, error) {
	if !reader.restricted(dataSourceID, propertyID) {
		return false, nil
	}
	shown, err := reader.propertyShown(dataSourceID, propertyID)
	return !shown, err
}

// withoutWithheld leaves out of a view's scope the properties the reader sees
// in no row, as Notion leaves them out of a view's configuration for an
// integration that can't see them (seen 2026-10-12).
func (scope notionViewFilterScope) withoutWithheld(app core.App, readerID, dataSourceID string) (notionViewFilterScope, error) {
	reader, err := newDatabasePropertyAccessReader(app, readerID, []string{dataSourceID})
	if err != nil {
		return scope, err
	}
	kept, withheld := make([]notionViewProperty, 0, len(scope.ordered)), map[string]struct{}{}
	for _, property := range scope.ordered {
		hidden, err := notionSchemaPropertyHidden(reader, dataSourceID, property.ID)
		if err != nil {
			return scope, err
		}
		if hidden {
			withheld[property.ID] = struct{}{}
		} else {
			kept = append(kept, property)
		}
	}
	if len(withheld) == 0 {
		return scope, nil
	}
	narrowed := newNotionViewFilterScope(kept)
	narrowed.timestamps, narrowed.sortsTimestamps = scope.timestamps, scope.sortsTimestamps
	narrowed.withheld, narrowed.ids = withheld, scope.ids
	narrowed.userToNative, narrowed.rowToNative = scope.userToNative, scope.rowToNative
	narrowed.userToPublic, narrowed.rowToPublic = scope.userToPublic, scope.rowToPublic
	narrowed.wholePropertyLists, narrowed.refusesUnknownOptions = scope.wholePropertyLists, scope.refusesUnknownOptions
	return narrowed, nil
}

// notionPropertyAccessError answers a refusal under the rules: a filter or
// sort by a hidden property, or a change the account may not make.
func notionPropertyAccessError(event *core.RequestEvent, err error) (error, bool) {
	if err == nil {
		return nil, false
	}
	if strings.Contains(err.Error(), errorCodeDatabaseQueryPropertyHidden) {
		return notionPageValidationError(event, "A filter or sort names a property this connection can't read."), true
	}
	requestError := &graphRequestError{}
	if errors.As(err, &requestError) && requestError.code == errorCodeGraphPropertyAccessDenied {
		return notionCompatibilityError(event, http.StatusForbidden, notionErrorRestrictedResource,
			"This connection can't change the property."), true
	}
	return nil, false
}
