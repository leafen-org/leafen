package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What People kept of a workspace before its database (schema_baseline.json):
// its views, its owner's properties and each person's values of them, and
// each row's number, joining and last edit. The workspace's database is made
// with them — the properties with their IDs, the views with People's own
// five properties' IDs read as the database's, each row with its values and
// its joining and last edit — and the line goes once its rows are made.

type peopleDatabaseMove struct {
	Views      []json.RawMessage
	Properties []json.RawMessage
	Rows       map[string]peopleMovedRow
}

type peopleMovedRow struct {
	UserID   string                   `json:"userId"`
	Values   map[string]graphRowValue `json:"values"`
	JoinedAt int64                    `json:"joinedAt"`
	EditedAt int64                    `json:"editedAt"`
	EditedBy string                   `json:"editedBy"`
}

// peopleOwnPropertyIDsBefore are People's five own properties' IDs as People kept them before.
func peopleOwnPropertyIDsBefore(database peopleDatabase) map[string]string {
	return map[string]string{
		"people-name": database.NamePropertyID, "people-email": database.EmailPropertyID, "people-about": database.AboutPropertyID,
		"people-membership": database.MembershipPropertyID, "people-person": database.PersonPropertyID,
	}
}

func loadPeopleDatabaseMove(app core.App, workspaceID string) (peopleDatabaseMove, bool, error) {
	stored := struct {
		Views      string `db:"views"`
		Properties string `db:"properties"`
		Rows       string `db:"rows"`
	}{}
	err := app.DB().NewQuery(`SELECT [[views]], [[properties]], [[rows]] FROM {{leafen_people_database_moves}} WHERE [[workspace_id]] = {:workspaceId}`).
		Bind(dbx.Params{"workspaceId": workspaceID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return peopleDatabaseMove{}, false, nil
	}
	if err != nil {
		return peopleDatabaseMove{}, false, err
	}
	move, rows := peopleDatabaseMove{Rows: map[string]peopleMovedRow{}}, []peopleMovedRow{}
	if err := errors.Join(json.Unmarshal([]byte(stored.Views), &move.Views), json.Unmarshal([]byte(stored.Properties), &move.Properties),
		json.Unmarshal([]byte(stored.Rows), &rows)); err != nil {
		return peopleDatabaseMove{}, false, err
	}
	for _, row := range rows {
		move.Rows[row.UserID] = row
	}
	return move, true, nil
}

// peopleMovedText reads a kept setting with People's own five properties' IDs as the database's.
func peopleMovedText(database peopleDatabase, raw json.RawMessage) json.RawMessage {
	text := string(raw)
	for before, after := range peopleOwnPropertyIDsBefore(database) {
		text = strings.ReplaceAll(text, `"`+before+`"`, `"`+after+`"`)
	}
	return json.RawMessage(text)
}

// peopleMovedOperations are the database's properties and views of what
// People kept, and whether it kept views of its own (else it takes its two).
func peopleMovedOperations(database peopleDatabase, move peopleDatabaseMove) ([]graphOperation, string, error) {
	operations, firstView := []graphOperation{}, ""
	for _, raw := range move.Properties {
		property := graphProperty{}
		if err := json.Unmarshal(peopleMovedText(database, raw), &property); err != nil {
			return nil, "", err
		}
		if len(property.Config) == 0 {
			property.Config = json.RawMessage(`{}`)
		}
		property.DataSourceID = database.DataSourceID
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertProperty, Property: &property})
	}
	for _, raw := range move.Views {
		view := graphDatabaseView{}
		if err := json.Unmarshal(peopleMovedText(database, raw), &view); err != nil {
			return nil, "", err
		}
		view.ID = derivedPageID("people-database", database.WorkspaceID, "view", view.ID)
		view.DataSourceID, view.ContainerID = database.DataSourceID, ""
		if len(view.Config) == 0 {
			view.Config = json.RawMessage(`{}`)
		}
		if firstView == "" {
			firstView = view.ID
		}
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertView, View: &view})
	}
	return operations, firstView, nil
}

// peopleMovedRowValues are the values a person's row held of the owner's
// properties, of those the database has.
func peopleMovedRowValues(move peopleDatabaseMove, userID string, kept map[string]bool, into map[string]graphRowValue) {
	for id, value := range move.Rows[userID].Values {
		if kept[id] {
			into[id] = value
		}
	}
}

// finishPeopleDatabaseMove lets go of what People kept once its rows hold it.
func finishPeopleDatabaseMove(app core.App, workspaceID string) error {
	_, err := app.DB().NewQuery(`DELETE FROM {{leafen_people_database_moves}} WHERE [[workspace_id]] = {:workspaceId}`).
		Bind(dbx.Params{"workspaceId": workspaceID}).Execute()
	return err
}

// movePeopleDatabasesSoon makes, as the server starts, the People database
// of each workspace whose People kept something before.
func movePeopleDatabasesSoon(app core.App) {
	workspaces := []string{}
	if err := app.DB().NewQuery(`SELECT [[workspace_id]] FROM {{leafen_people_database_moves}} ORDER BY [[workspace_id]]`).Column(&workspaces); err != nil {
		app.Logger().Warn("leafen people databases wait", "error", err)
		return
	}
	for _, workspaceID := range workspaces {
		syncPeopleDatabaseSoon(app, workspaceID)
	}
}

// peopleMovedPropertyID is a kept property's ID.
func peopleMovedPropertyID(raw json.RawMessage) (string, error) {
	property := struct {
		ID string `json:"id"`
	}{}
	err := json.Unmarshal(raw, &property)
	return property.ID, err
}
