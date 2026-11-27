package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"sort"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A version's inline database shows its table then, as Notion's version
// preview does (seen 2026-10-01): the properties the version kept, and the
// first rows it had with the values they had at the version's time; a row
// taken away by then is not among them. Rows keep their own history; a row
// whose history begins after the version reads the earliest values it knows.

// fillPageHistoryDatabaseTable sets an inline database's properties and
// first rows as they were at a version's time.
func fillPageHistoryDatabaseTable(
	app core.App, database *pageHistoryDatabaseWire, sourceID string, kept pageHistoryDatabaseState, at int64,
) error {
	return fillPageHistoryDatabaseTableRows(app, database, sourceID, kept, at, pageHistoryDatabasePreviewRows)
}

// fillPageHistoryDatabaseTableRows reads at most limit rows, telling with
// HasMoreRows whether there are more.
func fillPageHistoryDatabaseTableRows(
	app core.App, database *pageHistoryDatabaseWire, sourceID string, kept pageHistoryDatabaseState, at int64, limit int,
) error {
	properties := kept.Properties
	if len(properties) == 0 {
		var err error
		if properties, err = pageHistoryCurrentDatabaseProperties(app, sourceID); err != nil {
			return err
		}
	}
	sort.SliceStable(properties, func(i, j int) bool {
		if (properties[i].Type == graphValueTypeTitle) != (properties[j].Type == graphValueTypeTitle) {
			return properties[i].Type == graphValueTypeTitle
		}
		if properties[i].Position != properties[j].Position {
			return properties[i].Position < properties[j].Position
		}
		return properties[i].ID < properties[j].ID
	})
	database.Properties = make([]pageHistoryDatabasePropertyWire, 0, len(properties))
	for _, property := range properties {
		config := property.Config
		if len(config) == 0 {
			config = json.RawMessage(`{}`)
		}
		database.Properties = append(database.Properties, pageHistoryDatabasePropertyWire{
			ID: property.ID, Name: property.Name, Type: property.Type, Config: config,
		})
	}
	// Each row's page names it with its icon as a table does: its own, or
	// a page's, with lines when it holds anything (now: the history keeps a
	// row's icon, not when its body filled). They are read in their order from
	// the row index up to the limit; their creation time is read after (+),
	// or SQLite, which keeps no statistics, takes the index by creation time
	// and sorts every row of the source.
	rows := []struct {
		ID         string `db:"id"`
		HasContent bool   `db:"has_content"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[row]].[[id]], COALESCE(` + pageHasContentSQL + `, 0) AS [[has_content]]
		FROM {{leafen_database_rows}} AS [[row]]
		LEFT JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[id]] = [[row]].[[id]]
		LEFT JOIN {{leafen_database_row_trash}} AS [[trash]] ON [[trash]].[[row_id]] = [[row]].[[id]]
		WHERE ` + databaseSourceRowsSQL("[[row]]", "{:sourceId}") + ` AND +[[row]].[[created]] <= {:at}
			AND ([[trash]].[[row_id]] IS NULL OR [[trash]].[[trashed_at]] > {:at})
			AND NOT ` + databaseRowAwayAtSQL("[[row]].[[id]]") + `
		ORDER BY [[row]].[[position]], [[row]].[[id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"sourceId": sourceID, "at": at, "limit": limit + 1,
	}).All(&rows); err != nil {
		return err
	}
	if len(rows) > limit {
		rows, database.HasMoreRows = rows[:limit], true
	}
	database.Rows = make([]pageHistoryDatabaseRowWire, 0, len(rows))
	types := make(map[string]string, len(properties))
	for _, property := range properties {
		types[property.ID] = property.Type
	}
	for _, row := range rows {
		state, err := pageHistoryRowStateAt(app, row.ID, at)
		if err != nil {
			return err
		}
		// Each row's automatic properties as they were then (page_history_automatic_values.go).
		if err := fillPageHistoryAutomaticValues(app, row.ID, -1, at, types, state.Values); err != nil {
			return err
		}
		database.Rows = append(database.Rows, pageHistoryDatabaseRowWire{
			ID: row.ID, Values: state.Values, Icon: state.Icon, IconType: state.IconType, HasContent: row.HasContent,
		})
	}
	return nil
}

func pageHistoryCurrentDatabaseProperties(app core.App, sourceID string) ([]pageHistoryDatabaseProperty, error) {
	stored := []struct {
		ID       string `db:"id"`
		Type     string `db:"type"`
		Name     string `db:"name"`
		Position string `db:"position"`
		Config   string `db:"config"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[property]].[[id]],[[property]].[[type]],[[property]].[[name]],[[property]].[[position]],[[property]].[[config]]
		FROM {{leafen_database_properties}} AS [[property]]
		WHERE [[property]].[[data_source_id]]={:sourceId} AND ` + databasePropertyNotTrashedSQL("[[property]].[[id]]") + `
	`).Bind(dbx.Params{"sourceId": sourceID}).All(&stored); err != nil {
		return nil, err
	}
	properties := make([]pageHistoryDatabaseProperty, 0, len(stored))
	for _, property := range stored {
		properties = append(properties, pageHistoryDatabaseProperty{
			ID: property.ID, Type: property.Type, Name: property.Name,
			Position: property.Position, Config: pageHistoryDatabaseJSON(property.Config),
		})
	}
	return properties, nil
}

// pageHistoryRowValuesAt reads a database page's values at a time from its
// history: its latest page state then, else the earliest it knows, else its
// values now.
func pageHistoryRowValuesAt(app core.App, rowID string, at int64) (map[string]pageHistoryRowValue, error) {
	state, err := pageHistoryRowStateAt(app, rowID, at)
	return state.Values, err
}

// pageHistoryRowStateAt reads a database page's state at a time as
// pageHistoryRowValuesAt does, its values never nil.
func pageHistoryRowStateAt(app core.App, rowID string, at int64) (pageHistoryPageState, error) {
	latest := struct {
		Cursor int    `db:"version_cursor"`
		State  string `db:"state"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[version_cursor]], [[state]] FROM (
			SELECT [[version_cursor]], [[payload]] AS [[state]] FROM {{leafen_page_history_events}}
			WHERE [[page_id]]={:rowId} AND [[kind]] IN ('page','metadata') AND [[created_at]]<={:at}
			UNION ALL
			SELECT [[version_cursor]], [[page_json]] AS [[state]] FROM {{leafen_page_history_checkpoints}}
			WHERE [[page_id]]={:rowId} AND [[created_at]]<={:at}
		) ORDER BY [[version_cursor]] DESC LIMIT 1
	`).Bind(dbx.Params{"rowId": rowID, "at": at}).One(&latest)
	if errors.Is(err, sql.ErrNoRows) {
		err = app.DB().NewQuery(`
			SELECT [[version_cursor]], [[page_json]] AS [[state]] FROM {{leafen_page_history_checkpoints}}
			WHERE [[page_id]]={:rowId} ORDER BY [[version_cursor]] LIMIT 1
		`).Bind(dbx.Params{"rowId": rowID}).One(&latest)
	}
	if errors.Is(err, sql.ErrNoRows) {
		page, findErr := app.FindRecordById(pagesCollection, rowID)
		if findErr != nil {
			return pageHistoryPageState{Values: map[string]pageHistoryRowValue{}}, nil
		}
		state, stateErr := pageHistoryPageStateFor(app, page)
		if state.Values == nil {
			state.Values = map[string]pageHistoryRowValue{}
		}
		return state, stateErr
	}
	if err == nil {
		latest.State, err = unpackPageHistoryText(latest.State)
	}
	if err != nil {
		return pageHistoryPageState{}, err
	}
	state := pageHistoryPageState{}
	if err := json.Unmarshal([]byte(latest.State), &state); err != nil {
		return pageHistoryPageState{}, errors.Join(errors.New(errorCodePageHistoryFailed), err)
	}
	if state.Values == nil {
		state.Values = map[string]pageHistoryRowValue{}
	}
	return state, nil
}
