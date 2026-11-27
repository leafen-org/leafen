package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What People's data source lets every view of it do, wherever it shows —
// People's own page (PeopleView.tsx), a linked view of it on any page, a
// dashboard's widget, a phone's: its rows are its people, made by the
// server as they join and by no one else (database_added_rows.go), so no
// view of it offers a new page; its data source settings are its "속성
// 편집" alone; and its five own properties stay as the server keeps them.
// Notion's linked view of People shows no "새로 만들기" and no "+ 새
// 페이지", and "속성 편집" alone under its data source settings (seen
// 2026-11-19 in Chrome at 1440, its Business-trial owner), as its People
// page does (seen 2026-11-15). A read tells the app which of its sources is
// People and which of its properties are People's own (the web's
// databaseSourceRules.tsx follows them); a write never sets either
// (normalizeGraphDataSource).
//
// Its rows read the same in every view of it: each person's Email as the
// reader may see it (peopleEmailShown) — read from their account, never kept
// in the row — and its five own values none of the reader's to change in the
// row ("readOnlyValues"), but About for the workspace's owner, who writes
// anyone's but a deactivated person's into their profile (the web's
// peopleSourceCells.ts), as People's own page has shown them since
// 2026-11-15.

// markPeopleDatabases marks a read's People data sources, their own five
// properties and their rows as the reader reads them.
func markPeopleDatabases(app core.App, reader workspaceAuthorization, items []graphReadItem) error {
	sources := []string{}
	for _, item := range items {
		if item.Kind != graphReadKindDataSource && item.Kind != graphReadKindRow {
			continue
		}
		key := "id"
		if item.Kind == graphReadKindRow {
			key = "dataSourceId"
		}
		if source, _ := item.Value[key].(string); source != "" && !slices.Contains(sources, source) {
			sources = append(sources, source)
		}
	}
	if len(sources) == 0 {
		return nil
	}
	people, err := loadPeopleDatabasesOf(app, sources)
	if err != nil || len(people) == 0 {
		return err
	}
	// The read's own People rows alone, in one ask whatever its databases
	// (2026-11-20: each window of a People database read every person of the
	// workspace with their account, once for each People database in it).
	rows := []string{}
	for _, item := range items {
		source, _ := item.Value["dataSourceId"].(string)
		if _, found := people[source]; found && item.Kind == graphReadKindRow {
			if id, _ := item.Value["id"].(string); id != "" {
				rows = append(rows, id)
			}
		}
	}
	persons, err := loadPeopleRowAccounts(app, rows)
	if err != nil {
		return err
	}
	for _, item := range items {
		switch item.Kind {
		case graphReadKindDataSource:
			source, _ := item.Value["id"].(string)
			if database, found := people[source]; found {
				item.Value["peopleDatabase"] = true
				item.Value["fixedPropertyIds"] = peopleOwnPropertyIDs(database)
			}
		case graphReadKindRow:
			source, _ := item.Value["dataSourceId"].(string)
			if database, found := people[source]; found {
				id, _ := item.Value["id"].(string)
				person := persons[id]
				if person.WorkspaceID != database.WorkspaceID {
					person = peopleRowAccount{}
				}
				markPeopleRow(reader, database, person, item.Value)
			}
		}
	}
	return nil
}

// peopleRowAccount is the person a People row is, in its workspace, and the
// address their account keeps.
type peopleRowAccount struct{ WorkspaceID, UserID, Email string }

// markPeopleRow gives a People row its person's Email as the reader may see
// it and names the own values the reader doesn't change in it.
func markPeopleRow(reader workspaceAuthorization, database peopleDatabase, person peopleRowAccount, row map[string]any) {
	values, _ := row["values"].(map[string]any)
	if values == nil {
		values = map[string]any{}
		row["values"] = values
	}
	// A guest and a restricted member see no one's address but their own (peopleEmailShown).
	narrow := reader.Restricted || reader.Role != workspaceRoleOwner && reader.Role != workspaceRoleMember
	values[database.EmailPropertyID] = map[string]any{
		"type": graphValueTypeEmail, "value": peopleEmailShown(narrow, reader.PrincipalID, person.UserID, person.Email),
	}
	membership := ""
	if cell, _ := values[database.MembershipPropertyID].(map[string]any); cell != nil {
		membership, _ = cell["value"].(string)
	}
	readOnly, _ := row["readOnlyValues"].([]string)
	for _, id := range peopleOwnPropertyIDs(database) {
		if id == database.AboutPropertyID && reader.Role == workspaceRoleOwner && membership != peopleMembershipKeptDeactivated {
			continue
		}
		if !slices.Contains(readOnly, id) {
			readOnly = append(readOnly, id)
		}
	}
	slices.Sort(readOnly)
	row["readOnlyValues"] = readOnly
}

// loadPeopleDatabasesOf reads the People databases among data sources, by source.
func loadPeopleDatabasesOf(app core.App, sources []string) (map[string]peopleDatabase, error) {
	encoded, err := json.Marshal(sources)
	if err != nil {
		return nil, err
	}
	found := []peopleDatabase{}
	if err := app.DB().NewQuery(`SELECT [[workspace_id]], [[page_id]], [[data_source_id]], [[name_property_id]], [[email_property_id]],
			[[about_property_id]], [[membership_property_id]], [[person_property_id]]
		FROM {{leafen_workspace_people_databases}} WHERE [[data_source_id]] IN (SELECT [[value]] FROM json_each({:sources}))`).
		Bind(dbx.Params{"sources": string(encoded)}).All(&found); err != nil {
		return nil, err
	}
	people := make(map[string]peopleDatabase, len(found))
	for _, database := range found {
		people[database.DataSourceID] = database
	}
	return people, nil
}

// loadPeopleRowAccounts reads the People rows asked for with their persons'
// accounts, by row: each found by its id, so a read reads its own rows
// however many people their workspace has.
func loadPeopleRowAccounts(app core.App, rowIDs []string) (map[string]peopleRowAccount, error) {
	persons := map[string]peopleRowAccount{}
	if len(rowIDs) == 0 {
		return persons, nil
	}
	encoded, err := json.Marshal(rowIDs)
	if err != nil {
		return nil, err
	}
	found := []struct {
		RowID       string `db:"row_id"`
		WorkspaceID string `db:"workspace_id"`
		UserID      string `db:"user_id"`
		Email       string `db:"email"`
	}{}
	// From the rows asked for to each person by its row's key (CROSS JOIN
	// keeps that order), then to their account.
	if err := app.DB().NewQuery(`SELECT [[person]].[[row_id]], [[person]].[[workspace_id]], [[person]].[[user_id]],
			COALESCE([[user]].[[email]], '') AS [[email]]
		FROM json_each({:rows}) [[asked]]
		CROSS JOIN {{leafen_workspace_people_rows}} [[person]] ON [[person]].[[row_id]] = [[asked]].[[value]]
		LEFT JOIN {{users}} [[user]] ON [[user]].[[id]] = [[person]].[[user_id]]`).
		Bind(dbx.Params{"rows": string(encoded)}).All(&found); err != nil {
		return nil, err
	}
	for _, row := range found {
		persons[row.RowID] = peopleRowAccount{WorkspaceID: row.WorkspaceID, UserID: row.UserID, Email: row.Email}
	}
	return persons, nil
}

// peopleOwnPropertyIDs are People's five own properties: Name, Email, About,
// Membership Type and Person.
func peopleOwnPropertyIDs(database peopleDatabase) []string {
	return []string{database.NamePropertyID, database.EmailPropertyID, database.AboutPropertyID,
		database.MembershipPropertyID, database.PersonPropertyID}
}

// A person's write of People's rows — from a cell, a row's page, the Public
// API, MCP, a batch — keeps its own values as the server keeps them: Name,
// Email, Membership Type and Person are its people's, so a write that would
// change one is refused as Notion's API refuses a property it can't write
// (400, the properties named); an
// Email or another of them given as the row has it is no change, and an
// Email, which no row keeps, is not stored. About is the person's own: its
// workspace's owner writes anyone's but a deactivated person's, while People
// shows profiles, into their profile (savePersonAbout) as well as the row,
// so the next bringing up of People keeps it; anyone else's write of it is
// refused the same way. Leafen's own upkeep of People (LeafenOwn) is no
// person's write. Before 2026-11-20 such a write was taken, and the next
// sync put the row back without a word.

// keepPeopleOwnValues applies People's rule to a person's operations: the
// operations to write, or the refusal.
func keepPeopleOwnValues(app core.App, principalID string, operations []graphOperation) ([]graphOperation, error) {
	sources := []string{}
	for _, operation := range operations {
		if operation.Kind == graphOperationKindUpsertRow && operation.Row != nil && !slices.Contains(sources, operation.Row.DataSourceID) {
			sources = append(sources, operation.Row.DataSourceID)
		}
	}
	if len(sources) == 0 {
		return operations, nil
	}
	people, err := loadPeopleDatabasesOf(app, sources)
	if err != nil || len(people) == 0 {
		return operations, err
	}
	kept := make([]graphOperation, 0, len(operations))
	for _, operation := range operations {
		if operation.Kind == graphOperationKindUpsertRow && operation.Row != nil {
			if database, found := people[operation.Row.DataSourceID]; found {
				row, err := keepPeopleRowValues(app, principalID, database, *operation.Row)
				if err != nil {
					return nil, err
				}
				operation.Row = &row
			}
		}
		kept = append(kept, operation)
	}
	return kept, nil
}

// keepPeopleRowValues is a People row's write as its rule lets the person
// make it, its About saved into its person's profile.
func keepPeopleRowValues(app core.App, principalID string, database peopleDatabase, row graphDatabaseRow) (graphDatabaseRow, error) {
	own := peopleOwnPropertyIDs(database)
	if !slices.ContainsFunc(own, func(id string) bool { _, has := row.Values[id]; return has }) {
		return row, nil
	}
	stored := []struct {
		PropertyID string `db:"property_id"`
		Value      string `db:"value"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[property_id]], [[value]] FROM {{leafen_database_row_values}} WHERE [[row_id]] = {:rowId}`).
		Bind(dbx.Params{"rowId": row.ID}).All(&stored); err != nil {
		return row, err
	}
	before := map[string]string{}
	for _, cell := range stored {
		before[cell.PropertyID] = cell.Value
	}
	person := struct {
		UserID string `db:"user_id"`
		Email  string `db:"email"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[person]].[[user_id]], COALESCE([[user]].[[email]], '') AS [[email]]
		FROM {{leafen_workspace_people_rows}} [[person]] LEFT JOIN {{users}} [[user]] ON [[user]].[[id]] = [[person]].[[user_id]]
		WHERE [[person]].[[row_id]] = {:rowId}`).Bind(dbx.Params{"rowId": row.ID}).One(&person); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return row, err
	}
	values := maps.Clone(row.Values)
	refused := []string{}
	names := map[string]string{database.NamePropertyID: "Name", database.EmailPropertyID: "Email", database.AboutPropertyID: "About",
		database.MembershipPropertyID: "Membership Type", database.PersonPropertyID: "Person"}
	for _, id := range own {
		value, has := values[id]
		if !has {
			continue
		}
		switch id {
		case database.EmailPropertyID:
			// No row keeps an Email: one given as the person's account has it, or none, is no change.
			var email string
			if graphRowValueIsCleared(value.Value) || json.Unmarshal(value.Value, &email) == nil && email == person.Email {
				delete(values, id)
				continue
			}
		case database.AboutPropertyID:
			if peopleValueUnchanged(before, id, value) {
				continue
			}
			about, written, err := writePeopleAbout(app, principalID, database, person.UserID, before[database.MembershipPropertyID], value)
			if err != nil {
				return row, err
			}
			if written {
				values[id] = graphRowValue{Type: graphValueTypeRichText, Value: about}
				continue
			}
		default:
			if peopleValueUnchanged(before, id, value) {
				continue
			}
		}
		refused = append(refused, names[id])
	}
	if len(refused) > 0 {
		return row, graphInputError(errorCodeGraphInvalid,
			"No property that can be changed has these IDs or names: "+strings.Join(refused, ","))
	}
	row.Values = values
	return row, nil
}

// peopleValueUnchanged is whether a write gives a value as its row keeps it.
func peopleValueUnchanged(before map[string]string, id string, value graphRowValue) bool {
	stored, had := before[id]
	if !had {
		return graphRowValueIsCleared(value.Value)
	}
	return jsonEquivalent([]byte(stored), value.Value)
}

// writePeopleAbout saves an About written in a People row into its person's
// profile, as its workspace's owner writes it there (updatePersonProfile):
// the value the row keeps, and whether the writer may write it.
func writePeopleAbout(app core.App, principalID string, database peopleDatabase, userID, membership string, value graphRowValue) (json.RawMessage, bool, error) {
	var about string
	if !graphRowValueIsCleared(value.Value) && json.Unmarshal(value.Value, &about) != nil {
		return nil, false, nil
	}
	about = strings.TrimSpace(about)
	var kept string
	_ = json.Unmarshal([]byte(membership), &kept)
	if userID == "" || kept == peopleMembershipKeptDeactivated || principalID != peopleWorkspaceOwner(app, database.WorkspaceID) ||
		!utf8.ValidString(about) || utf8.RuneCountInString(about) > personAboutMaxRunes {
		return nil, false, nil
	}
	settings, err := loadPeopleProfileSettings(app, database.WorkspaceID)
	if err != nil || !settings.Directory {
		return nil, false, err
	}
	if err := savePersonAbout(app, database.WorkspaceID, userID, principalID, about); err != nil {
		return nil, false, err
	}
	encoded, err := json.Marshal(about)
	return encoded, err == nil, err
}
