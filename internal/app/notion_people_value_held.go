package app

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A person value written back keeps whom it holds already, whoever writes
// it: a personal access token's bot, which only that token may name new,
// a connection or a token since stopped, someone who left the
// workspace, an invitation since lapsed. Each was refused as naming no one,
// so a value read by one road and written back by another failed whole
// (2026-11-26: a row a token made with its own bot, as Notion's API takes
// an integration's bot (measured again that day), read by notion-fetch and
// written back by update_properties: "The person value user://… names no
// one"). Whoever the value doesn't hold yet is checked as before
// (notionDatabaseRowPageRequestValue).

// notionPeopleValueHeld is whom the row's value of the person property
// holds now, by the IDs it keeps them by; none for a row not made yet.
func notionPeopleValueHeld(app core.App, rowID, propertyID string) (map[string]bool, error) {
	held := map[string]bool{}
	raw, found, err := rowValueHeld(app, rowID, propertyID)
	if err != nil || !found {
		return held, err
	}
	ids := []string{}
	if json.Unmarshal([]byte(raw), &ids) != nil {
		return held, nil
	}
	for _, id := range ids {
		held[id] = true
	}
	return held, nil
}

// rowValueHeld is the value the row holds now for the property, as stored;
// found is false for none, or for a row not made yet ("").
func rowValueHeld(app core.App, rowID, propertyID string) (raw string, found bool, err error) {
	if rowID == "" {
		return "", false, nil
	}
	var value sql.NullString
	err = app.DB().NewQuery(`SELECT [[value]] FROM {{leafen_database_row_values}} WHERE [[row_id]] = {:row} AND [[property_id]] = {:property}`).
		Bind(dbx.Params{"row": rowID, "property": propertyID}).Row(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return value.String, err == nil && value.Valid, err
}
