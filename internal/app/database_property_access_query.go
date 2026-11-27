package app

import (
	"errors"
	"sort"

	"github.com/pocketbase/pocketbase/core"
)

// A database query under the source's property access rules
// (database_property_access.go), as Notion's views keep them: a view that
// filters or sorts by a property whose values the person may not see
// doesn't open for them, a search reads only the values they may see in
// each row, and the rows it answers leave out what they may not see.

// refuseHiddenQueryProperties refuses a query that filters or sorts by a
// property whose values the person may not see.
func (context databaseQueryContext) refuseHiddenQueryProperties(plan databaseQueryPlan) error {
	if context.Access == nil {
		return nil
	}
	properties := []string{}
	for _, sort := range plan.Sorts {
		properties = append(properties, sort.PropertyID)
	}
	var walk func(group *databaseQueryFilterGroup)
	walk = func(group *databaseQueryFilterGroup) {
		if group == nil {
			return
		}
		for _, filter := range group.Filters {
			properties = append(properties, filter.PropertyID)
		}
		for index := range group.Groups {
			walk(&group.Groups[index])
		}
	}
	walk(plan.FilterGroup)
	for _, propertyID := range properties {
		if !context.Access.restricted(context.DataSourceID, propertyID) {
			continue
		}
		level, err := context.Access.level(context.DataSourceID, propertyID, nil)
		if err != nil {
			return err
		}
		if !propertyAccessShowsValues(level) {
			return errors.New(errorCodeDatabaseQueryPropertyHidden)
		}
	}
	return nil
}

// hidesQueryValue is whether the person may not see the row's value.
func (context databaseQueryContext) hidesQueryValue(row databaseQueryRow, propertyID string) bool {
	if context.Access == nil || !context.Access.restricted(context.DataSourceID, propertyID) {
		return false
	}
	level, err := context.Access.level(context.DataSourceID, propertyID, databaseQueryAccessRow(row))
	return err != nil || !propertyAccessShowsValues(level)
}

func databaseQueryAccessRow(row databaseQueryRow) *databasePropertyAccessRow {
	return &databasePropertyAccessRow{ID: row.ID, CreatedBy: row.CreatedBy, People: func(propertyID string) []string {
		value, ok := row.Values[propertyID]
		if !ok {
			return nil
		}
		return peopleInValue(value.Value)
	}}
}

// redactDatabaseQueryRows takes from the query's rows what the person may
// not see, naming what they may see but not change, and the unlinked items
// they may not change (database_row_unlinked_access.go).
func redactDatabaseQueryRows(app core.App, readerID, dataSourceID string, rows []databaseQueryRow) error {
	if len(rows) == 0 {
		return nil
	}
	items, err := loadDatabaseRowAccess(app, readerID, []string{dataSourceID})
	if err != nil {
		return err
	}
	for index := range rows {
		rows[index].ReadOnlyForMe = items.readOnly(rows[index].ID)
	}
	reader, err := newDatabasePropertyAccessReader(app, readerID, []string{dataSourceID})
	if err != nil || reader == nil {
		return err
	}
	for index := range rows {
		row := &rows[index]
		accessRow := databaseQueryAccessRow(*row)
		decisions := map[string]string{}
		for _, propertyID := range reader.restrictedProperties(dataSourceID) {
			if decisions[propertyID], err = reader.level(dataSourceID, propertyID, accessRow); err != nil {
				return err
			}
		}
		for propertyID, level := range decisions {
			if !propertyAccessShowsValues(level) {
				delete(row.Values, propertyID)
				delete(row.Computed, propertyID)
				// A formula or rollup reading a hidden value shows empty, not as hidden.
				if own, err := reader.ownLevel(dataSourceID, propertyID, accessRow); err != nil {
					return err
				} else if propertyAccessShowsValues(own) {
					continue
				}
				if shown, err := reader.propertyShown(dataSourceID, propertyID); err != nil {
					return err
				} else if shown {
					row.HiddenValues = append(row.HiddenValues, propertyID)
				}
			} else if !propertyAccessEditsValues(level) {
				row.ReadOnlyValues = append(row.ReadOnlyValues, propertyID)
			}
		}
		sort.Strings(row.HiddenValues)
		sort.Strings(row.ReadOnlyValues)
	}
	return nil
}
