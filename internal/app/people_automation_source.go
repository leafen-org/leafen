package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// People's automations are its database's own (people_database.go), as
// Notion's People's ⚡ is its database's (seen 2026-11-14): made, listed and
// run as any database's, on its page and data source, applied to all of it
// or one of its views, whose own filters a view's run reads. Its sync sets
// them off as it writes a person's row (admitPeopleRowAutomationEvents): the
// row added as they first show, their Name, About, Membership Type or
// Person changed; a property its owner added sets them off as its value is
// written, as any database's does. A run reads a row's Email from its
// person's account, which the database does not keep
// (peopleAutomationSnapshotEmails). What People's add to a database's rules
// is an approved difference: no step edits the person's row, or adds or edits People's rows
// — a member's name, About and role are written only where they are kept
// (validatePeopleAutomationRule).

// peopleArchiveSourcePrefix names People in a workspace archive, which
// carries no People database: "people:<workspace>" stands for its page and
// data source, "people-name" and the rest for its five own properties
// (peopleOwnPropertyIDsBefore), and "people:<workspace>:directory" or
// ":cards" for its two first views — the names People's automations had
// before People was a database (migration 799).
const peopleArchiveSourcePrefix = "people:"

// peopleStockViews are People's two first views by the names an archive
// gives them.
var peopleStockViews = []string{"directory", "cards"}

// peopleDatabaseOfPage is the People database whose page this is, if any.
func peopleDatabaseOfPage(app core.App, pageID string) (peopleDatabase, bool, error) {
	return loadPeopleDatabaseBy(app, "page_id", pageID)
}

// validatePeopleAutomationRule refuses what People's automations don't do:
// a condition on its Email, which Notion's offers none of (seen
// 2026-11-14) and no change to People edits, and a step that writes
// People's rows — the person it ran for, or another — whose own values are
// written only where they are kept (their account, their profile, 설정 ›
// 사람), and which only joining the workspace adds.
func validatePeopleAutomationRule(normalized normalizedDatabaseAutomationDefinition, emailPropertyID, sourceID string) error {
	for index, condition := range normalized.Trigger.Conditions {
		if condition.PropertyID != "" && condition.PropertyID == emailPropertyID {
			return databaseAutomationRefused("trigger.conditions[%d] names People's Email, which no change to People sets off", index)
		}
	}
	for index, action := range normalized.ActionDocument.Actions {
		var target struct {
			DatabaseID string `json:"databaseId"`
		}
		_ = json.Unmarshal(action.Target, &target)
		if interactiveAutomationWritesTriggerPage(action) ||
			action.DatabaseID == sourceID || target.DatabaseID == sourceID {
			return databaseAutomationRefused(
				"actionDocument.actions[%d] writes People's rows, which only their people's own settings do: add or edit pages of another database instead", index)
		}
	}
	return nil
}

// peopleAutomationSnapshotEmails gives each People row among a run's rows
// its person's Email, as their account keeps it: the value a condition, a
// formula's prop("Email") and a webhook read.
func peopleAutomationSnapshotEmails(app core.App, snapshots map[string]map[string]graphRowValue) error {
	if len(snapshots) == 0 {
		return nil
	}
	rowIDs := make([]string, 0, len(snapshots))
	for rowID := range snapshots {
		rowIDs = append(rowIDs, rowID)
	}
	encoded, err := json.Marshal(rowIDs)
	if err != nil {
		return err
	}
	found := []struct {
		RowID      string `db:"row_id"`
		PropertyID string `db:"email_property_id"`
		Email      string `db:"email"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[person]].[[row_id]], [[database]].[[email_property_id]], [[user]].[[email]]
		FROM {{leafen_workspace_people_rows}} [[person]]
		JOIN {{leafen_workspace_people_databases}} [[database]] ON [[database]].[[workspace_id]] = [[person]].[[workspace_id]]
		JOIN {{users}} [[user]] ON [[user]].[[id]] = [[person]].[[user_id]]
		WHERE [[person]].[[row_id]] IN (SELECT [[value]] FROM json_each({:rowIds}))
	`).Bind(dbx.Params{"rowIds": string(encoded)}).All(&found); err != nil {
		return err
	}
	for _, row := range found {
		email, _ := json.Marshal(row.Email)
		snapshots[row.RowID][row.PropertyID] = graphRowValue{Type: graphValueTypeEmail, Value: email}
	}
	return nil
}

// peopleWebhookEmail gives a People row's Email in a webhook's page as the
// run read it, which the row's own page, read as the API reads it, leaves
// empty.
func peopleWebhookEmail(
	app core.App, candidate databaseAutomationEvaluationCandidate, selected []notionDatabaseRowPageProjectedProperty,
) error {
	database, people, err := peopleDatabaseOfPage(app, candidate.PageID)
	if err != nil || !people {
		return err
	}
	values := map[string]graphRowValue{}
	if json.Unmarshal([]byte(candidate.runSnapshotJSON()), &values) != nil {
		return errDatabaseAutomationUnavailable
	}
	var email string
	_ = json.Unmarshal(values[database.EmailPropertyID].Value, &email)
	for _, property := range selected {
		if property.ID == database.EmailPropertyID && email != "" && property.PageValue != nil {
			property.PageValue["email"] = email
		}
	}
	return nil
}

// peopleNoticeTargetOf is where a notice an automation sent about People
// opens: the profile of the person whose row it ran for, or People itself
// for one about its database (a schedule's run); false for any other page.
func peopleNoticeTargetOf(app core.App, pageID, personID string) (string, bool, error) {
	if personID != "" {
		return "/people/" + url.PathEscape(personID), true, nil
	}
	_, people, err := peopleDatabaseOfPage(app, pageID)
	if err != nil || !people {
		return "", false, err
	}
	return "/people", true, nil
}

// peopleStockViewID is the ID a workspace's People database gives one of
// its two first views: the one it made, or — where People kept views before
// it was a database (people_database_moves.go) — the one moved from People's
// own, made or still waiting to be.
func peopleStockViewID(app core.App, workspaceID, stock string) (string, error) {
	former := peopleArchiveSourcePrefix + workspaceID + ":" + stock
	moved := derivedPageID("people-database", workspaceID, "view", former)
	var found bool
	if err := app.DB().NewQuery(`
		SELECT EXISTS (SELECT 1 FROM {{leafen_database_views}} WHERE [[id]] = {:moved})
			OR EXISTS (SELECT 1 FROM {{leafen_people_database_moves}} [[move]], json_each([[move]].[[views]]) [[view]]
				WHERE [[move]].[[workspace_id]] = {:workspaceId} AND json_extract([[view]].[[value]], '$.id') = {:former})
	`).Bind(dbx.Params{"moved": moved, "workspaceId": workspaceID, "former": former}).Row(&found); err != nil {
		return "", err
	}
	if found {
		return moved, nil
	}
	return derivedPageID("people-database", workspaceID, stock), nil
}

// peopleOwnIDsText reads a setting's People own property IDs the other way:
// an archive's as the database's (in), or the database's as an archive's.
func peopleOwnIDsText(database peopleDatabase, raw json.RawMessage, in bool) json.RawMessage {
	text := string(raw)
	for archived, own := range peopleOwnPropertyIDsBefore(database) {
		from, to := own, archived
		if in {
			from, to = archived, own
		}
		text = strings.ReplaceAll(text, `"`+from+`"`, `"`+to+`"`)
		text = strings.ReplaceAll(text, `\"`+from+`\"`, `\"`+to+`\"`)
	}
	return json.RawMessage(text)
}

// peopleArchiveAutomation writes one of People's automations as an archive
// carries it (peopleArchiveSourcePrefix); a view other than its first two
// goes by its own ID.
func peopleArchiveAutomation(app core.App, database peopleDatabase, payload string) (string, error) {
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(payload), &fields); err != nil {
		return "", err
	}
	source := peopleArchiveSourcePrefix + database.WorkspaceID
	fields["pageId"], _ = json.Marshal(source)
	fields["dataSourceId"] = fields["pageId"]
	var viewID string
	_ = json.Unmarshal(fields["viewId"], &viewID)
	if viewID != "" {
		named := viewID
		for _, stock := range peopleStockViews {
			id, err := peopleStockViewID(app, database.WorkspaceID, stock)
			if err != nil {
				return "", err
			}
			if id == viewID {
				named = stock
			}
		}
		fields["viewId"], _ = json.Marshal(source + ":" + named)
	}
	for _, key := range []string{"trigger", "actionDocument"} {
		fields[key] = peopleOwnIDsText(database, fields[key], false)
	}
	encoded, err := json.Marshal(fields)
	return string(encoded), err
}

// peopleArchiveAutomationIn places an archive's People automation on the
// People database of the workspace it lands in, named as that database
// names its page, data source, own properties and first two views.
func peopleArchiveAutomationIn(app core.App, workspaceID string, automation *workspaceImportStagedAutomation) error {
	database, has, err := loadPeopleDatabase(app, workspaceID)
	if err != nil {
		return err
	}
	if !has {
		database = newPeopleDatabase(workspaceID)
	}
	automation.PageID, automation.DataSourceID = database.PageID, database.DataSourceID
	if automation.ScopeType == databaseAutomationScopeView {
		named := strings.TrimPrefix(automation.ViewID, ":")
		automation.ViewID = named
		for _, stock := range peopleStockViews {
			if named == stock {
				if automation.ViewID, err = peopleStockViewID(app, workspaceID, stock); err != nil {
					return err
				}
			}
		}
	}
	automation.Trigger = peopleOwnIDsText(database, automation.Trigger, true)
	automation.ActionDocument = peopleOwnIDsText(database, automation.ActionDocument, true)
	return nil
}

// loadPeopleDatabaseBy reads the People database a column of its line
// names: its workspace or its page.
func loadPeopleDatabaseBy(app core.App, column, value string) (peopleDatabase, bool, error) {
	database := peopleDatabase{}
	err := app.DB().NewQuery(`SELECT [[workspace_id]], [[page_id]], [[data_source_id]], [[name_property_id]], [[email_property_id]],
			[[about_property_id]], [[membership_property_id]], [[person_property_id]]
		FROM {{leafen_workspace_people_databases}} WHERE [[` + column + `]] = {:value}`).
		Bind(dbx.Params{"value": value}).One(&database)
	if errors.Is(err, sql.ErrNoRows) {
		return peopleDatabase{}, false, nil
	}
	return database, err == nil, err
}
