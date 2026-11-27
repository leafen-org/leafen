package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page's databases live in their records. These read one record as the
// page that holds it keeps it; a record on another page is not there.

// databasePageRecords reads one page's database records while a change to
// that page is being prepared.
type databasePageRecords struct {
	app    core.App
	pageID string
}

func loadDatabaseViewRecord(app core.App, pageID, id string) (databaseViewAuthority, bool, error) {
	var view databaseViewAuthority
	var config string
	err := app.DB().NewQuery(`
		SELECT [[data_source_id]], [[container_id]], [[type]], [[name]], [[position]], [[config]]
		FROM {{leafen_database_views}} WHERE [[id]] = {:id} AND [[page_id]] = {:pageId}
	`).Bind(dbx.Params{"id": id, "pageId": pageID}).Row(
		&view.DataSourceID, &view.ContainerID, &view.Type, &view.Name, &view.Position, &config,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return databaseViewAuthority{}, false, nil
	}
	if err != nil {
		return view, false, err
	}
	if view.Config, err = databaseRecordConfig(config); err != nil {
		return view, true, err
	}
	return view, true, nil
}

func loadDatabasePropertyRecord(app core.App, pageID, id string) (databasePropertyAuthority, bool, error) {
	var property databasePropertyAuthority
	var config string
	err := app.DB().NewQuery(`
		SELECT [[data_source_id]], [[type]], [[name]], [[position]], [[config]]
		FROM {{leafen_database_properties}} WHERE [[id]] = {:id} AND [[page_id]] = {:pageId}
	`).Bind(dbx.Params{"id": id, "pageId": pageID}).Row(
		&property.DataSourceID, &property.Type, &property.Name, &property.Position, &config,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return databasePropertyAuthority{}, false, nil
	}
	if err != nil {
		return property, false, err
	}
	if property.Config, err = databaseRecordConfig(config); err != nil {
		return property, true, err
	}
	return property, true, nil
}

// loadDatabaseViewRecords reads every view the page's databases have, by ID.
func loadDatabaseViewRecords(app core.App, pageID string) (map[string]databaseViewAuthority, error) {
	rows := []struct {
		ID           string `db:"id"`
		DataSourceID string `db:"data_source_id"`
		ContainerID  string `db:"container_id"`
		Type         string `db:"type"`
		Name         string `db:"name"`
		Position     string `db:"position"`
		Config       string `db:"config"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[data_source_id]], [[container_id]], [[type]], [[name]], [[position]], [[config]]
		FROM {{leafen_database_views}} WHERE [[page_id]] = {:pageId}
	`).Bind(dbx.Params{"pageId": pageID}).All(&rows); err != nil {
		return nil, err
	}
	views := make(map[string]databaseViewAuthority, len(rows))
	for _, row := range rows {
		config, err := databaseRecordConfig(row.Config)
		if err != nil {
			return nil, err
		}
		views[row.ID] = databaseViewAuthority{
			DataSourceID: row.DataSourceID, ContainerID: row.ContainerID, Type: row.Type, Name: row.Name,
			Position: row.Position, Config: config,
		}
	}
	return views, nil
}

// loadDatabasePropertyRecords reads every property the page's databases
// have, by ID.
func loadDatabasePropertyRecords(app core.App, pageID string) (map[string]databasePropertyAuthority, error) {
	rows := []struct {
		ID           string `db:"id"`
		DataSourceID string `db:"data_source_id"`
		Type         string `db:"type"`
		Name         string `db:"name"`
		Position     string `db:"position"`
		Config       string `db:"config"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[data_source_id]], [[type]], [[name]], [[position]], [[config]]
		FROM {{leafen_database_properties}} WHERE [[page_id]] = {:pageId}
	`).Bind(dbx.Params{"pageId": pageID}).All(&rows); err != nil {
		return nil, err
	}
	properties := make(map[string]databasePropertyAuthority, len(rows))
	for _, row := range rows {
		config, err := databaseRecordConfig(row.Config)
		if err != nil {
			return nil, err
		}
		properties[row.ID] = databasePropertyAuthority{
			DataSourceID: row.DataSourceID, Type: row.Type, Name: row.Name, Position: row.Position, Config: config,
		}
	}
	return properties, nil
}

func loadDatabaseRowRecord(app core.App, pageID, id string) (databaseRowAuthority, bool, error) {
	var row databaseRowAuthority
	err := app.DB().NewQuery(`
		SELECT [[data_source_id]], [[position]], [[subitem_parent_id]]
		FROM {{leafen_database_rows}} WHERE [[id]] = {:id} AND [[page_id]] = {:pageId}
	`).Bind(dbx.Params{"id": id, "pageId": pageID}).Row(&row.DataSourceID, &row.Position, &row.SubitemParentID)
	if errors.Is(err, sql.ErrNoRows) {
		return databaseRowAuthority{}, false, nil
	}
	return row, err == nil, err
}

// databaseRowRecordOperation is the upsert that writes a row as it stands,
// for a change that adds values to it.
func databaseRowRecordOperation(app core.App, pageID, id string) (graphDatabaseRow, error) {
	stored, exists, err := loadDatabaseRowRecord(app, pageID, id)
	if err != nil {
		return graphDatabaseRow{}, err
	}
	if !exists {
		return graphDatabaseRow{}, graphStructureAuthorityError()
	}
	row := graphDatabaseRow{ID: id, DataSourceID: stored.DataSourceID, Position: stored.Position}
	if stored.SubitemParentID != "" {
		parentID := stored.SubitemParentID
		row.SubitemParentID = &parentID
	}
	return row, nil
}

// databaseRowRecordSource names the source a row on the page belongs to,
// or nothing when the page has no such row.
func databaseRowRecordSource(app core.App, pageID, id string) (string, error) {
	row, _, err := loadDatabaseRowRecord(app, pageID, id)
	return row.DataSourceID, err
}

func loadDatabaseCellRecord(app core.App, pageID, rowID, propertyID string) (databaseCellAuthority, bool, error) {
	var cell databaseCellAuthority
	var value, spans string
	err := app.DB().NewQuery(`
		SELECT [[type]], [[value]], [[spans]] FROM {{leafen_database_row_values}}
		WHERE [[row_id]] = {:rowId} AND [[property_id]] = {:propertyId} AND [[page_id]] = {:pageId}
	`).Bind(dbx.Params{"rowId": rowID, "propertyId": propertyID, "pageId": pageID}).Row(&cell.Type, &value, &spans)
	if errors.Is(err, sql.ErrNoRows) {
		return databaseCellAuthority{}, false, nil
	}
	if err != nil {
		return cell, false, err
	}
	if json.Unmarshal([]byte(value), &cell.Value) != nil || cell.Type == "" {
		return cell, true, graphStructureAuthorityError()
	}
	if stored := databaseStoredSpans(spans); len(stored) > 0 {
		cell.Spans = stored
	}
	return cell, true, nil
}

func databaseRecordConfig(raw string) (map[string]any, error) {
	config := map[string]any{}
	if raw == "" {
		return config, nil
	}
	if json.Unmarshal([]byte(raw), &config) != nil || config == nil {
		return nil, graphStructureAuthorityError()
	}
	return config, nil
}

// Order keys are compared as text in the printable range, as the app's own
// keys are (web lib/graphPosition.ts).
const (
	databasePositionLowest  = 0x21
	databasePositionHighest = 0x7e
)

// databasePositionAfter is the order key that puts a new record after tail,
// the last key among its siblings; no tail starts the order. Records keep the
// keys their writers chose, so a key is compared as text, never counted. The
// key is tail up to its first character that can still grow, grown by one,
// so keys added one after another stay a few characters long for many
// thousands of records.
func databasePositionAfter(tail string) (string, error) {
	if tail == "" {
		return "p0000000000", nil
	}
	if len(tail) == 11 && tail[0] == 'p' {
		if index, err := strconv.ParseUint(tail[1:], 10, 64); err == nil && index < 9999999999 {
			return fmt.Sprintf("p%010d", index+1), nil
		}
	}
	for index := 0; index < len(tail); index++ {
		if tail[index] < databasePositionHighest {
			grown := max(tail[index]+1, databasePositionLowest+1)
			return tail[:index] + string([]byte{grown}), nil
		}
	}
	if len(tail)+1 > maxGraphPositionLength {
		return "", graphStructureAuthorityError()
	}
	return tail + string([]byte{databasePositionLowest + 1}), nil
}

// databasePositionBetween is an order key after low and before high, or
// after low when high is empty. With no key between them (high is low and
// the lowest character), it is low, and the id breaks the tie.
func databasePositionBetween(low, high string) (string, error) {
	if high == "" {
		return databasePositionAfter(low)
	}
	key := low
	for len(key) < maxGraphPositionLength {
		if !strings.HasPrefix(high, key) {
			// high differs from key before key ends, so key grown is below it.
			return key + "U", nil
		}
		if len(key) == len(high) {
			break
		}
		next := high[len(key)]
		if next > databasePositionLowest {
			// A byte past ASCII sorts after all of it, so the key stays text.
			if middle := databasePositionLowest + (min(next, 0x7f)-databasePositionLowest)/2; middle > databasePositionLowest {
				return key + string([]byte{middle}), nil
			}
			// Only the lowest character is below next: a key never ends in it,
			// or nothing could be put before that key (2026-11-10: a form's
			// ninth answer, each put above the last, found no room before "!").
			return key + string([]byte{databasePositionLowest, 'U'}), nil
		}
		key += string([]byte{next})
	}
	return low, nil
}

// databaseRowTail is the position of the last row of a data source.
func databaseRowTail(app core.App, ownerID, pageID, dataSourceID string) (string, error) {
	tail := ""
	err := app.DB().NewQuery(`SELECT COALESCE(MAX([[position]]), '') FROM {{leafen_database_rows}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[data_source_id]] = {:dataSourceId}`).
		Bind(dbx.Params{"owner": ownerID, "pageId": pageID, "dataSourceId": dataSourceID}).Row(&tail)
	return tail, err
}

// databaseRowPositionAtStart is the order key of a row added before every row
// of its data source.
func databaseRowPositionAtStart(app core.App, ownerID, pageID, dataSourceID string) (string, error) {
	heads := []string{}
	err := app.DB().NewQuery(`SELECT [[position]] FROM {{leafen_database_rows}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[data_source_id]] = {:dataSourceId}
		ORDER BY [[position]] LIMIT 2`).
		Bind(dbx.Params{"owner": ownerID, "pageId": pageID, "dataSourceId": dataSourceID}).Column(&heads)
	if err != nil {
		return "", err
	}
	if len(heads) == 0 {
		return databasePositionBetween("", "")
	}
	// Before a counted key, a counted key one lower, as keys added at the end
	// count up (databasePositionAfter): "o9999999999" before "p0000000000",
	// so a form's answers, each put above the last, keep keys of eleven
	// characters however many come.
	if head := heads[0]; len(head) == 11 && (head[0] == 'p' || head[0] == 'o') {
		if index, err := strconv.ParseUint(head[1:], 10, 64); err == nil {
			if head[0] == 'p' {
				return "o9999999999", nil
			}
			if index > 0 {
				return fmt.Sprintf("o%010d", index-1), nil
			}
		}
	}
	if position, err := databasePositionBetween("", heads[0]); err != nil || position != "" {
		return position, err
	}
	// A first key made of the lowest character alone leaves no room before
	// it (written so before keys never ended in it): the row goes just after.
	next := ""
	if len(heads) > 1 {
		next = heads[1]
	}
	return databasePositionBetween(heads[0], next)
}

// databaseRowPositionAtEnd is the order key of a row added after every row of
// its data source, where the app's own table puts a new row.
func databaseRowPositionAtEnd(app core.App, ownerID, pageID, dataSourceID string) (string, error) {
	tail, err := databaseRowTail(app, ownerID, pageID, dataSourceID)
	if err != nil {
		return "", err
	}
	return databasePositionAfter(tail)
}
