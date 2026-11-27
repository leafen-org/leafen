package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database's version, as Notion's view-version gives it (seen 2026-11-13):
// "type":"database", its configuration as JSON text — its name, inline or
// not, its parent, its data sources with their schemas (each property named
// by its collectionProperty:// URL) and its views in its tabs' order, each
// as fetch gives a view — then its rows then, each its url and its values
// as fetch gives a page's, and total_rows; a linked database leaves out the
// data sources and rows that belong to another. Leafen reads it from the
// version of the page holding it, as its history screen previews it: the
// data sources, properties and views the version kept, and the rows there
// at the time with the values they had, under the reader's property access
// rules now.

const mcpVersionRowsPageSize = 100

type notionMCPVersionRow struct {
	URL string `json:"url"`
	// Properties are its values in its table's column order
	// (mcpVersionSource.valuesInOrder).
	Properties any `json:"properties"`
}

// errMCPVersionBeforeDatabase refuses a time before a database was made.
var errMCPVersionBeforeDatabase = mcpVersionBeforeMade("database")

// viewMCPDatabaseVersion reads a database as the page's version at a cursor
// held it, with its rows at a time from offset on.
func viewMCPDatabaseVersion(
	ctx context.Context, app core.App, authority mcpAuthority, target mcpHistoryTarget, versionCursor int, at int64, offset int,
) (notionMCPViewVersionOutput, error) {
	output := notionMCPViewVersionOutput{}
	err := runReadSnapshot(ctx, app, func(txApp core.App) error {
		state, err := readMCPVersionDatabase(txApp, authority, target, versionCursor)
		if err != nil {
			return err
		}
		if len(state.Sources) == 0 && len(state.Views) == 0 {
			return errMCPVersionBeforeDatabase
		}
		configuration, err := state.configuration(txApp, authority, target)
		if err != nil {
			return err
		}
		ids, sources, err := mcpVersionRowIDs(txApp, authority.PrincipalID, state, at)
		if err != nil {
			return err
		}
		reader, err := newMCPVersionRowReader(txApp, authority, target.Page, state)
		if err != nil {
			return err
		}
		start := min(offset, len(ids))
		end := min(start+mcpVersionRowsPageSize, len(ids))
		rows := make([]notionMCPVersionRow, 0, end-start)
		for _, id := range ids[start:end] {
			source, _ := state.source(sources[id])
			values, _, err := reader.values(source, id, at)
			if err != nil {
				return err
			}
			rows = append(rows, notionMCPVersionRow{URL: mcpPageURL(authority, id), Properties: source.valuesInOrder(values)})
		}
		total := len(ids)
		output = notionMCPViewVersionOutput{
			Type: "database", URL: target.Database.URL, Configuration: configuration, Rows: &rows, TotalRows: &total,
		}
		if end < len(ids) {
			output.NextCursor = "rows:" + strconv.Itoa(end)
		}
		return nil
	})
	if errors.Is(err, errMCPToolInvalid) || errors.Is(err, errMCPScopeDenied) {
		return notionMCPViewVersionOutput{}, err
	}
	if err != nil {
		return notionMCPViewVersionOutput{}, failedBecause(errMCPToolFailed, err)
	}
	return output, nil
}

// mcpVersionSource is a data source as a version holds it: its name and the
// properties the reader may see, as they were.
type mcpVersionSource struct {
	ID         string
	Name       string
	Properties []notionMCPDataSourceProperty
	Configs    map[string]string
	IDs        notionPropertyIDs
}

// mcpVersionView is a view as a version holds it.
type mcpVersionView struct {
	notionMCPDataSourceView
	Position string
}

// mcpVersionDatabaseState is a database as a version of its page holds it.
type mcpVersionDatabaseState struct {
	Name    string
	Sources []mcpVersionSource
	Views   []mcpVersionView
}

func (state mcpVersionDatabaseState) source(id string) (mcpVersionSource, bool) {
	index := slices.IndexFunc(state.Sources, func(source mcpVersionSource) bool { return source.ID == id })
	if index < 0 {
		return mcpVersionSource{}, false
	}
	return state.Sources[index], true
}

// readMCPVersionDatabase reads a database as the page's version at a cursor
// holds it.
func readMCPVersionDatabase(app core.App, authority mcpAuthority, target mcpHistoryTarget, versionCursor int) (mcpVersionDatabaseState, error) {
	database := target.Database
	materialized, err := materializePageHistoryVersion(app, target.PageID, versionCursor)
	if err != nil {
		return mcpVersionDatabaseState{}, err
	}
	state := mcpVersionDatabaseState{Name: materialized.Page.Title}
	propertyIDs := []string{}
	for _, sourceID := range database.SourceIDs {
		for _, property := range materialized.Databases[sourceID].Properties {
			propertyIDs = append(propertyIDs, property.ID)
		}
	}
	access, err := newPageHistoryPropertyAccess(app, authority.PrincipalID, propertyIDs)
	if err != nil {
		return mcpVersionDatabaseState{}, err
	}
	for _, sourceID := range database.SourceIDs {
		held, found := materialized.Databases[sourceID]
		if !found || held.Source == nil {
			continue
		}
		source, err := mcpVersionSourceOf(app, access, sourceID, held)
		if err != nil {
			return mcpVersionDatabaseState{}, err
		}
		state.Sources = append(state.Sources, source)
		if database.Inline && len(state.Sources) == 1 {
			state.Name = held.Source.Title
		}
	}
	// A linked database is named as the data source it shows.
	if database.Inline && len(database.SourceIDs) == 0 {
		if members, err := loadMCPDatabaseContainerMemberships(app, database.ContainerID); err == nil && len(members) > 0 {
			state.Name = ""
			_ = app.DB().NewQuery(`SELECT [[title]] FROM {{leafen_data_sources}} WHERE [[id]] = {:id}`).
				Bind(dbx.Params{"id": members[0].DataSourceID}).Row(&state.Name)
		}
	}
	order, err := mcpDatabaseTabOrder(app, database.ContainerID)
	if err != nil {
		return mcpVersionDatabaseState{}, err
	}
	for sourceID, held := range materialized.Databases {
		_, own := state.source(sourceID)
		for _, view := range held.Views {
			if view.ContainerID != database.ContainerID && (!own || view.ContainerID != "") {
				continue
			}
			if dashboardWidgetOwner(view.Config) != "" {
				continue
			}
			config := map[string]any{}
			if json.Unmarshal(view.Config, &config) != nil || config == nil {
				config = map[string]any{}
			}
			position := view.Position
			if tab, listed := order[view.ID]; listed {
				position = tab
			}
			state.Views = append(state.Views, mcpVersionView{
				notionMCPDataSourceView: notionMCPDataSourceView{ID: view.ID, Name: view.Name, Type: view.Type, Config: config, DataSourceID: sourceID},
				Position:                position,
			})
		}
	}
	sort.SliceStable(state.Views, func(left, right int) bool {
		if state.Views[left].Position != state.Views[right].Position {
			return state.Views[left].Position < state.Views[right].Position
		}
		return state.Views[left].ID < state.Views[right].ID
	})
	return state, nil
}

func mcpVersionSourceOf(app core.App, access *pageHistoryPropertyAccess, sourceID string, held pageHistoryDatabaseState) (mcpVersionSource, error) {
	ids, err := loadNotionPropertyIDs(app, sourceID)
	if err != nil {
		return mcpVersionSource{}, err
	}
	source := mcpVersionSource{ID: sourceID, Name: held.Source.Title, IDs: ids, Configs: map[string]string{}}
	for _, property := range held.Properties {
		if access != nil {
			if gone, err := access.gone(property.ID); err != nil {
				return mcpVersionSource{}, err
			} else if gone {
				continue
			}
		}
		config := map[string]any{}
		if json.Unmarshal(property.Config, &config) != nil || config == nil {
			config = map[string]any{}
		}
		source.Properties = append(source.Properties, notionMCPDataSourceProperty{ID: property.ID, Name: property.Name, Type: property.Type, Config: config})
		source.Configs[property.ID] = string(property.Config)
	}
	return source, nil
}

// mcpDatabaseTabOrder is the order a database shows its views in now.
func mcpDatabaseTabOrder(app core.App, containerID string) (map[string]string, error) {
	stored := []struct {
		ViewID   string `db:"view_id"`
		Position string `db:"position"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[view_id]], [[position]] FROM {{leafen_database_container_views}} WHERE [[container_id]] = {:containerId}
	`).Bind(dbx.Params{"containerId": containerID}).All(&stored); err != nil {
		return nil, err
	}
	order := make(map[string]string, len(stored))
	for _, tab := range stored {
		order[tab.ViewID] = tab.Position
	}
	return order, nil
}

// schema is a data source's schema as the version tools give it, each
// property by its collectionProperty:// URL.
func (source mcpVersionSource) schema(app core.App) (map[string]any, error) {
	schema := map[string]any{}
	_, err := source.eachProperty(app, func(key string, entry map[string]any) {
		schema[key] = entry
	})
	return schema, err
}

// orderedSchema is the schema as view-version writes it: its properties in
// the order Notion's database keeps them once changed — its API's and its
// SQLite table's, the title last (mcpSQLiteTableOrder; seen 2026-11-14, a
// database just made and not changed since keeps the order it was made in)
// — each one's keys in Notion's order (mcpVersionKeyOrder); and the
// properties SQL can't query.
func (source mcpVersionSource) orderedSchema(app core.App) (mcpOrderedObject, []string, error) {
	schema := mcpOrderedObject{}
	unqueryable, err := source.eachProperty(app, func(key string, entry map[string]any) {
		schema = append(schema, mcpOrderedEntry{Key: key, Value: mcpVersionKeyOrder(entry)})
	})
	return schema, unqueryable, err
}

// eachProperty gives each property the reader may see, in its table's
// order, as the version tools write it (a formula's as versionFormula
// does), and names those SQL can't query in that order, as Notion's names
// them in its schema's (seen 2026-11-14).
func (source mcpVersionSource) eachProperty(app core.App, each func(key string, entry map[string]any)) ([]string, error) {
	byID := make(map[string]notionMCPDataSourceProperty, len(source.Properties))
	for _, property := range source.Properties {
		byID[property.ID] = property
	}
	mcpID, publicSource := func(id string) string { return notionMCPPropertyID(source.IDs.publicID(id)) }, mcpDataSourcePublicID(app, source.ID)
	unqueryable := []string{}
	for _, property := range mcpSQLiteTableOrder(mcpSQLiteVisibleProperties(source.Properties), source.IDs) {
		entry, queryable, err := mcpPropertyState(app, publicSource, property, byID, mcpID)
		if err != nil {
			return nil, err
		}
		if property.Type == graphValueTypeFormula {
			source.versionFormula(entry, property)
		}
		if !queryable {
			unqueryable = append(unqueryable, property.Name)
		}
		each("collectionProperty://"+publicSource+"/"+mcpID(property.ID), entry)
	}
	return unqueryable, nil
}

// sourceState is a data source as the version tools give it, its keys in
// Notion's order (seen 2026-11-13: url, name, schema; 2026-11-14: then
// notAvailableInQuerySql, as fetch's state names them).
func (source mcpVersionSource) sourceState(app core.App) (mcpOrderedObject, error) {
	schema, unqueryable, err := source.orderedSchema(app)
	if err != nil {
		return nil, err
	}
	state := mcpOrderedObject{
		{Key: "url", Value: mcpDataSourceURL(app, source.ID)}, {Key: "name", Value: source.Name}, {Key: "schema", Value: schema},
	}
	if len(unqueryable) > 0 {
		state = append(state, mcpOrderedEntry{Key: "notAvailableInQuerySql", Value: unqueryable})
	}
	return state, nil
}

// viewState is a view as the version tools give it: as fetch gives it, its
// data source named without fetch's braces.
func (state mcpVersionDatabaseState) viewState(app core.App, authority mcpAuthority, view mcpVersionView) (map[string]any, error) {
	source, own := state.source(view.DataSourceID)
	properties := source.Properties
	if !own {
		current, err := pageHistoryCurrentDatabaseProperties(app, view.DataSourceID)
		if err != nil {
			return nil, err
		}
		_, properties = mcpVersionPropertiesOf(current)
	}
	config, err := mcpFetchedViewConfig(app, authority.PrincipalID, view.notionMCPDataSourceView, view.DataSourceID, properties)
	if err != nil {
		return nil, err
	}
	config["dataSourceUrl"] = mcpDataSourceURL(app, view.DataSourceID)
	return config, nil
}

func mcpVersionPropertiesOf(stored []pageHistoryDatabaseProperty) (map[string]string, []notionMCPDataSourceProperty) {
	configs := map[string]string{}
	properties := make([]notionMCPDataSourceProperty, 0, len(stored))
	for _, property := range stored {
		config := map[string]any{}
		if json.Unmarshal(property.Config, &config) != nil || config == nil {
			config = map[string]any{}
		}
		configs[property.ID] = string(property.Config)
		properties = append(properties, notionMCPDataSourceProperty{ID: property.ID, Name: property.Name, Type: property.Type, Config: config})
	}
	return configs, properties
}

// configuration is the database's configuration as view-version writes it.
func (state mcpVersionDatabaseState) configuration(app core.App, authority mcpAuthority, target mcpHistoryTarget) (string, error) {
	sources := mcpOrderedObject{}
	for _, source := range state.Sources {
		written, err := source.sourceState(app)
		if err != nil {
			return "", err
		}
		sources = append(sources, mcpOrderedEntry{Key: mcpDataSourceURL(app, source.ID), Value: written})
	}
	views := mcpOrderedObject{}
	for _, view := range state.Views {
		written, err := state.viewState(app, authority, view)
		if err != nil {
			return "", err
		}
		views = append(views, mcpOrderedEntry{Key: mcpViewURLPrefix + view.ID, Value: mcpVersionViewOrder(written)})
	}
	parent := map[string]any{"type": "workspace"}
	if target.Database.Inline {
		parent = map[string]any{"type": "page", "url": mcpPageURL(authority, target.PageID)}
	} else if parentID := target.Page.GetString("parent_id"); parentID != "" {
		parent = map[string]any{"type": "page", "url": mcpPageURL(authority, parentID)}
	}
	return mcpFetchJSON(mcpOrderedObject{
		{Key: "name", Value: mcpDatabaseShownTitle(app, authority.PrincipalID, state.Name)},
		{Key: "inline", Value: target.Database.Inline},
		{Key: "parent", Value: parent},
		{Key: "dataSources", Value: sources},
		{Key: "views", Value: views},
	})
}

// mcpVersionRowIDs are the rows a database's own data sources held at a
// time that the reader may see, in their order: read in that order from the
// row index, their creation time after (+), as a version's preview reads
// them (page_history_database_preview.go).
func mcpVersionRowIDs(app core.App, principalID string, state mcpVersionDatabaseState, at int64) ([]string, map[string]string, error) {
	ids, sources := []string{}, map[string]string{}
	for _, source := range state.Sources {
		listed := []string{}
		if err := app.DB().NewQuery(`
			SELECT [[row]].[[id]] FROM {{leafen_database_rows}} AS [[row]]
			LEFT JOIN {{leafen_database_row_trash}} AS [[trash]] ON [[trash]].[[row_id]] = [[row]].[[id]]
			WHERE ` + databaseSourceRowsSQL("[[row]]", "{:sourceId}") + ` AND +[[row]].[[created]] <= {:at}
				AND ([[trash]].[[row_id]] IS NULL OR [[trash]].[[trashed_at]] > {:at})
				AND NOT ` + databaseRowAwayAtSQL("[[row]].[[id]]") + `
			ORDER BY [[row]].[[position]], [[row]].[[id]]
		`).Bind(dbx.Params{"sourceId": source.ID, "at": at}).Column(&listed); err != nil {
			return nil, nil, err
		}
		for _, id := range listed {
			sources[id] = source.ID
		}
		ids = append(ids, listed...)
	}
	hidden, err := unlinkedRowAccessFor(app, principalID, ids)
	if err != nil || hidden == nil {
		return ids, sources, err
	}
	return slices.DeleteFunc(ids, hidden.hides), sources, nil
}

// mcpVersionRowsCursor reads view-version's cursor: where its rows go on.
func mcpVersionRowsCursor(raw string) (int, bool) {
	if raw == "" {
		return 0, true
	}
	offset, err := strconv.Atoi(strings.TrimPrefix(raw, "rows:"))
	return offset, err == nil && offset > 0 && strings.HasPrefix(raw, "rows:")
}

// mcpVersionRowReader writes a row's values at a time as fetch writes a
// page's, by column.
type mcpVersionRowReader struct {
	app         core.App
	authority   mcpAuthority
	workspaceID string
	access      *pageHistoryPropertyAccess
	closed      searchMentionCloser
	relations   func(json.RawMessage) (json.RawMessage, bool)
	mentions    notionMarkdownMentions
}

func newMCPVersionRowReader(app core.App, authority mcpAuthority, page *core.Record, state mcpVersionDatabaseState) (mcpVersionRowReader, error) {
	propertyIDs := []string{}
	for _, source := range state.Sources {
		for _, property := range source.Properties {
			propertyIDs = append(propertyIDs, property.ID)
		}
	}
	access, err := newPageHistoryPropertyAccess(app, authority.PrincipalID, propertyIDs)
	workspaceID := page.GetString("workspace")
	return mcpVersionRowReader{
		app: app, authority: authority, workspaceID: workspaceID, access: access, closed: mcpReaderCloses(app, authority),
		relations: mcpRelationsAsRead(app, authority, workspaceID), mentions: mcpPropertyMentions(app, authority, workspaceID),
	}, err
}

// values reads a row's values at a time, and its title as a change names a page's (mcpVersionTitleShown).
func (reader mcpVersionRowReader) values(source mcpVersionSource, rowID string, at int64) (map[string]any, string, error) {
	held, err := pageHistoryRowStateAt(reader.app, rowID, at)
	if err != nil {
		return nil, "", err
	}
	if reader.access != nil {
		if err := reader.access.redactValues(rowID, held.Values); err != nil {
			return nil, "", err
		}
	}
	fact, err := reader.factsAt(source, rowID, at)
	if err != nil {
		return nil, "", err
	}
	values, title := map[string]any{}, ""
	for _, property := range mcpSQLiteVisibleProperties(source.Properties) {
		stored := storedNotionDatabaseRowPageProperty{
			ID: property.ID, Type: property.Type, Name: property.Name, Config: source.Configs[property.ID], PublicID: source.IDs.publicID(property.ID),
		}
		if value, found := held.Values[property.ID]; found && len(value.Value) > 0 && string(value.Value) != "null" {
			stored.ValueType = sql.NullString{String: value.Type, Valid: true}
			stored.Value = sql.NullString{String: string(value.Value), Valid: true}
			if len(value.Spans) > 0 && string(value.Spans) != "null" {
				stored.Spans = sql.NullString{String: string(value.Spans), Valid: true}
			}
		}
		stored = mcpCellAsRead(reader.closed, stored)
		if property.Type == graphValueTypeRelation && stored.Value.Valid {
			if read, changed := reader.relations(json.RawMessage(stored.Value.String)); changed {
				stored.Value.String = string(read)
			}
		}
		for part, value := range mcpFetchedCellParts(stored, mcpDatabasePropertyParts(reader.authority, stored, fact)) {
			values[mcpDatabaseColumnName(stored, part)] = value
		}
		if property.Type == graphValueTypeUniqueID {
			if number, held := mcpVersionUniqueIDValue(stored); held {
				values[mcpDatabaseColumnName(stored, "")] = number
			}
		}
		if markdown, written := mcpTextCellMarkdown(stored, reader.mentions); written {
			values[mcpDatabaseColumnName(stored, "")] = markdown
		}
		if property.Type == graphValueTypeTitle {
			title = mcpVersionTitleShown(reader.app, reader.authority, reader.workspaceID, mcpSQLTextWords(stored), databaseStoredSpans(stored.Spans.String))
		}
	}
	for column, value := range values {
		values[column] = mcpVersionColumnValue(value)
	}
	return values, title, nil
}
