package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// How a view treats a property it groups or dates by once the property is
// deleted, as Notion's does (seen 2026-11-08, "W29 보기 속성 삭제 시험"):
//
//   - deleting it changes no view: each keeps naming it, so restoring it
//     brings its grouping or date back. Meanwhile a table, list or gallery
//     grouped by it shows ungrouped, and a board grouped by it, or a calendar
//     or timeline dated by it, shows its fallback (the web draws it): a
//     board its default group among the properties left (status, select,
//     person, multi-select, relation — mcpBoardGroupPreference), a calendar
//     or timeline the first date left, the first of a kind in the order the
//     API lists them (notionPublicOrderedProperties);
//   - a board, calendar or timeline shown while the property is deleted takes
//     that fallback for good (databaseViewShownConfig), as Notion's writes it
//     once shown: restored, the property finds the board on the fallback, and
//     a board never shown on its own group again. With no fallback left the
//     view keeps the property and asks for one;
//   - deleting the property for good clears what views still name of it
//     (withDatabaseViewsOffProperties), a board taking its fallback, so no
//     view names a property that is gone.
//
// The editor's, an agent's and the API's deletions all go by this, as they
// all send the property to Deleted properties alike (trashDatabaseProperty).

// databaseViewShownKey is the setting naming the property a view of the type
// shows by while it can fall back to another: a board's group, a calendar's
// date, a timeline's own date or else the calendar's it kept.
func databaseViewShownKey(view databaseViewAuthority) string {
	switch view.Type {
	case graphViewTypeBoard:
		return "groupBy"
	case graphViewTypeCalendar:
		return "calendarBy"
	case graphViewTypeTimeline:
		if named, _ := view.Config["timelineBy"].(string); strings.TrimSpace(named) != "" {
			return "timelineBy"
		}
		return "calendarBy"
	}
	return ""
}

// databaseViewShownConfig is a view's settings once it is shown, as Notion's
// screen writes them then (seen 2026-11-12, views made through the API: its
// answer read null until each was shown, last edited at the showing): a
// board, calendar or timeline naming no property it groups or dates by
// takes its default, the one the web draws (a board's as a pick from its
// grouping list, so its option groups by name with the empty ones hidden; a
// calendar's or timeline's first date in the order the API lists them); one
// whose property is in Deleted properties takes its fallback the same way.
// A table, list or gallery keeps nothing (seen 2026-11-12). Notion's timeline
// also writes its zoom and centre then; Leafen's keeps those in the viewer's
// browser and leaves the view as it is. Nil when nothing changes,
// as when no property to fall to is left.
func databaseViewShownConfig(
	app core.App, view databaseViewAuthority, reader *databasePropertyAccessReader,
) (map[string]any, error) {
	key := databaseViewShownKey(view)
	if key == "" {
		return nil, nil
	}
	config := maps.Clone(view.Config)
	changed := false
	named, _ := view.Config[key].(string)
	named = strings.TrimSpace(named)
	falls := named == ""
	if !falls {
		var deleted int
		if err := app.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_database_property_trash}} AS [[trash]]
			JOIN {{leafen_database_properties}} AS [[property]] ON [[property]].[[id]] = [[trash]].[[property_id]]
			WHERE [[trash]].[[property_id]] = {:propertyId} AND [[property]].[[data_source_id]] = {:sourceId}
		`).Bind(dbx.Params{"propertyId": named, "sourceId": view.DataSourceID}).Row(&deleted); err != nil {
			return nil, err
		}
		falls = deleted > 0
	}
	if falls {
		left, err := databaseSourceLiveProperties(app, view.DataSourceID, nil, reader)
		if err != nil {
			return nil, err
		}
		if view.Type == graphViewTypeBoard {
			changed = regroupDatabaseBoardWithoutProperty(config, left) || changed
		} else if index := slices.IndexFunc(left, databaseViewDateSource); index >= 0 {
			// A timeline naming no date keeps its own.
			if named == "" && view.Type == graphViewTypeTimeline {
				key = "timelineBy"
			}
			config[key] = left[index].ID
			changed = true
		}
	}
	if !changed {
		return nil, nil
	}
	return config, nil
}

// databaseViewDateSource is whether a calendar or timeline may place its
// pages by the property, as the web's isViewDateSource: a date, or a rollup
// of the related pages' dates.
func databaseViewDateSource(property notionMCPDataSourceProperty) bool {
	if property.Type == graphValueTypeDate {
		return true
	}
	function, _ := property.Config["rollupFunction"].(string)
	return property.Type == graphValueTypeRollup &&
		slices.Contains([]string{"earliest_date", "latest_date", "date_range"}, function)
}

// databaseViewShownChange is the change a view shown makes: its settings
// with its default or fallback (databaseViewShownConfig), or none.
func databaseViewShownChange(app core.App, pageID, viewID, principalID string) ([]graphOperation, error) {
	view, exists, err := loadDatabaseViewRecord(app, pageID, viewID)
	if err != nil || !exists {
		return nil, err
	}
	reader, err := newDatabasePropertyAccessReader(app, principalID, []string{view.DataSourceID})
	if err != nil {
		return nil, err
	}
	config, err := databaseViewShownConfig(app, view, reader)
	if err != nil || config == nil {
		return nil, err
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	return []graphOperation{{Kind: graphOperationKindUpsertView, View: &graphDatabaseView{
		ID: viewID, DataSourceID: view.DataSourceID, ContainerID: view.ContainerID, Type: view.Type,
		Name: view.Name, Position: view.Position, Config: encoded,
	}}}, nil
}

// withDatabaseViewsOffProperties adds to a change's operations the changes
// to the page's views that still name the properties being deleted for good,
// and the other side of a relation pair deleted with one, for what they show.
func withDatabaseViewsOffProperties(
	app core.App, pageID string, propertyIDs []string, operations []graphOperation,
) ([]graphOperation, error) {
	if len(propertyIDs) == 0 {
		return operations, nil
	}
	gone := slices.Clone(propertyIDs)
	for _, propertyID := range propertyIDs {
		partner, err := databaseDeletedRelationPartner(app, pageID, propertyID)
		if err != nil {
			return nil, err
		}
		if partner != "" && !slices.Contains(gone, partner) {
			gone = append(gone, partner)
		}
	}
	encoded, err := json.Marshal(gone)
	if err != nil {
		return nil, err
	}
	// The views whose checked settings name them (graphViewPropertyReferences
	// keeps them indexed), and timelines dated by them, which the index
	// doesn't hold.
	viewIDs := []string{}
	if err := app.DB().NewQuery(`
		SELECT [[view_id]] FROM {{leafen_database_view_property_refs}}
		WHERE [[page_id]] = {:pageId} AND [[property_id]] IN (SELECT [[value]] FROM json_each({:propertyIds}))
		UNION
		SELECT [[id]] FROM {{leafen_database_views}}
		WHERE [[page_id]] = {:pageId} AND [[type]] = {:timeline} AND (
			json_extract([[config]], '$.timelineBy') IN (SELECT [[value]] FROM json_each({:propertyIds}))
			OR json_extract([[config]], '$.timelineEndBy') IN (SELECT [[value]] FROM json_each({:propertyIds})))
		ORDER BY 1
	`).Bind(dbx.Params{
		"pageId": pageID, "propertyIds": string(encoded), "timeline": graphViewTypeTimeline,
	}).Column(&viewIDs); err != nil {
		return nil, err
	}
	left := map[string][]notionMCPDataSourceProperty{}
	for _, viewID := range viewIDs {
		view, exists, err := loadDatabaseViewRecord(app, pageID, viewID)
		if err != nil {
			return nil, err
		}
		if !exists {
			continue
		}
		if _, loaded := left[view.DataSourceID]; !loaded {
			if left[view.DataSourceID], err = databaseSourceLiveProperties(app, view.DataSourceID, gone, nil); err != nil {
				return nil, err
			}
		}
		config, err := json.Marshal(databaseViewConfigWithoutProperties(view, gone, left[view.DataSourceID]))
		if err != nil {
			return nil, err
		}
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertView, View: &graphDatabaseView{
			ID: viewID, DataSourceID: view.DataSourceID, ContainerID: view.ContainerID, Type: view.Type,
			Name: view.Name, Position: view.Position, Config: config,
		}})
	}
	return operations, nil
}

// databaseDeletedRelationPartner names the other side of a two-way relation
// on the page when it is in Deleted properties too, which a deletion for good
// takes along (planDatabaseRelationPairDeletion); "" otherwise.
func databaseDeletedRelationPartner(app core.App, pageID, propertyID string) (string, error) {
	partner := ""
	err := app.DB().NewQuery(`
		SELECT [[pair]].[[id]]
		FROM {{leafen_database_properties}} AS [[property]]
		JOIN {{leafen_database_properties}} AS [[pair]]
			ON [[pair]].[[id]] = json_extract([[property]].[[config]], '$.relatedPropertyId')
			AND [[pair]].[[page_id]] = [[property]].[[page_id]]
		JOIN {{leafen_database_property_trash}} AS [[trash]] ON [[trash]].[[property_id]] = [[pair]].[[id]]
		WHERE [[property]].[[id]] = {:propertyId} AND [[property]].[[page_id]] = {:pageId}
			AND [[property]].[[type]] = {:relation}
		LIMIT 1
	`).Bind(dbx.Params{"propertyId": propertyID, "pageId": pageID, "relation": graphValueTypeRelation}).Row(&partner)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return partner, err
}

// databaseSourceLiveProperties are a source's properties not in Deleted
// properties nor among those named, in the order its API lists them, which a
// view picks its fallback in (notionPublicOrderedProperties); with a reader,
// those it sees.
func databaseSourceLiveProperties(
	app core.App, dataSourceID string, except []string, reader *databasePropertyAccessReader,
) ([]notionMCPDataSourceProperty, error) {
	stored := []struct {
		ID     string `db:"id"`
		Type   string `db:"type"`
		Config string `db:"config"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[type]], [[config]] FROM {{leafen_database_properties}}
		WHERE [[data_source_id]] = {:sourceId} AND ` + databasePropertyNotTrashedSQL("[[id]]") + `
		ORDER BY [[position]], [[id]]
	`).Bind(dbx.Params{"sourceId": dataSourceID}).All(&stored); err != nil {
		return nil, err
	}
	properties := make([]notionMCPDataSourceProperty, 0, len(stored))
	for _, property := range stored {
		if slices.Contains(except, property.ID) {
			continue
		}
		if reader != nil {
			if shown, err := reader.propertyShown(dataSourceID, property.ID); err != nil {
				return nil, err
			} else if !shown {
				continue
			}
		}
		config, err := databaseRecordConfig(property.Config)
		if err != nil {
			return nil, err
		}
		properties = append(properties, notionMCPDataSourceProperty{ID: property.ID, Type: property.Type, Config: config})
	}
	return notionPublicOrderedProperties(app, dataSourceID, properties)
}

// databaseViewConfigWithoutProperties is a view's settings once the
// properties are gone; left are the properties that stay, in their order. A
// board grouped by one takes its default group, if any is left.
func databaseViewConfigWithoutProperties(view databaseViewAuthority, gone []string, left []notionMCPDataSourceProperty) map[string]any {
	config := maps.Clone(view.Config)
	if config == nil {
		config = map[string]any{}
	}
	for _, key := range append(slices.Clone(graphViewPropertySettingKeys), "timelineBy", "timelineEndBy") {
		if named, _ := config[key].(string); !slices.Contains(gone, strings.TrimSpace(named)) {
			continue
		}
		delete(config, key)
		if key == "groupBy" && view.Type == graphViewTypeBoard {
			for _, grouping := range []string{"groupSort", "groupOrder", "groupByMode", "hideEmptyGroups"} {
				delete(config, grouping)
			}
			regroupDatabaseBoardWithoutProperty(config, left)
		}
	}
	return config
}

// databaseBoardOptionGroupTypes are the kinds a board groups by their own
// options, people or box rather than by the values its cards hold (the web's
// isBoardValueGroupProperty).
var databaseBoardOptionGroupTypes = []string{
	graphValueTypeStatus, graphValueTypeSelect, graphValueTypeMultiSelect, graphValueTypePerson, graphValueTypeCheckbox,
}

// regroupDatabaseBoardWithoutProperty groups a board by its default as a pick
// from its grouping list does (the web's regroupedViewConfig): the old
// group's order, sort and hidden groups go, an option property's groups
// start in their own order, and empty groups of options, people or a box are
// hidden. False, with the board unchanged, when no property it may group by
// is left.
func regroupDatabaseBoardWithoutProperty(config map[string]any, left []notionMCPDataSourceProperty) bool {
	for _, kind := range mcpBoardGroupPreference {
		index := slices.IndexFunc(left, func(property notionMCPDataSourceProperty) bool { return property.Type == kind })
		if index < 0 {
			continue
		}
		for _, key := range []string{"groupSort", "groupOrder", "groupByMode", "hideEmptyGroups", "hiddenGroups"} {
			delete(config, key)
		}
		config["groupBy"] = left[index].ID
		if order := mcpBoardGroupOrder(left[index], mcpBoardNoValueGroup); order != nil {
			config["groupOrder"] = order
		}
		if slices.Contains(databaseBoardOptionGroupTypes, kind) {
			config["hideEmptyGroups"] = true
		}
		return true
	}
	return false
}
