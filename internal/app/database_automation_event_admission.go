package app

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type plannedDatabaseAutomationEvent struct {
	ID                 string
	DatabaseID         string
	RowID              string
	EventType          string
	ChangedPropertyIDs string
	CapturedFrontier   int
	// JobID is the run that evaluates it: a page added waits in its own
	// (databaseAutomationPageAddedDelay).
	JobID string
}

type databaseAutomationMutationFacts struct {
	Events []plannedDatabaseAutomationEvent
	JobID  string
	// AddedJobID runs the pages the change added, once they have waited.
	AddedJobID string
}

type databaseAutomationFrontierRow struct {
	DatabaseID string `db:"database_id"`
	Frontier   int    `db:"frontier"`
}

type databaseAutomationExistingRow struct {
	ID string `db:"id"`
}

type databaseAutomationSnapshotValue struct {
	RowID      string `db:"row_id"`
	PropertyID string `db:"property_id"`
	Type       string `db:"type"`
	Value      string `db:"value"`
}

// prepareDatabaseAutomationEvents is what a change's rows set off. A
// database automation's own write sets off none — "Database automations
// can't be triggered by other automations", Notion's help says, and a page
// an automation added to another database ran none of that database's (seen
// 2026-11-14) — while a person's, a button's or a form's does.
func prepareDatabaseAutomationEvents(
	app core.App,
	authorization pageAuthorization,
	mutationID string,
	operations []graphOperation,
	automationWrite bool,
) (databaseAutomationMutationFacts, error) {
	if automationWrite {
		return databaseAutomationMutationFacts{}, nil
	}
	rows := map[string]*graphDatabaseRow{}
	databaseIDs := map[string]struct{}{}
	for _, operation := range operations {
		if operation.Kind == graphOperationKindUpsertRow && operation.Row != nil {
			rows[operation.Row.ID] = operation.Row
			databaseIDs[operation.Row.DataSourceID] = struct{}{}
		}
	}
	if len(rows) == 0 {
		return databaseAutomationMutationFacts{}, nil
	}
	frontiers, err := loadDatabaseAutomationAdmissionFrontiers(app, authorization, databaseIDs)
	if err != nil || len(frontiers) == 0 {
		return databaseAutomationMutationFacts{}, err
	}
	existing, err := loadDatabaseAutomationExistingRows(app, authorization, rows)
	if err != nil {
		return databaseAutomationMutationFacts{}, err
	}
	rowIDs := make([]string, 0, len(rows))
	existingIDs := make([]string, 0, len(existing))
	for rowID := range rows {
		rowIDs = append(rowIDs, rowID)
		if _, found := existing[rowID]; found {
			existingIDs = append(existingIDs, rowID)
		}
	}
	sort.Strings(rowIDs)
	// A row's values as they stand before the change applies: a property
	// written again with the value it holds is not edited, as a row's
	// followers hear only of the properties that changed
	// (databaseRowNotificationSameValue).
	stored := map[string]map[string]string{}
	if len(existingIDs) > 0 {
		if stored, _, err = loadDatabaseRowNotificationValues(app, existingIDs); err != nil {
			return databaseAutomationMutationFacts{}, err
		}
	}
	facts := databaseAutomationMutationFacts{
		Events:     []plannedDatabaseAutomationEvent{},
		JobID:      automationStableID("haj", authorization.Workspace.OwnerID, authorization.Page.Id, mutationID),
		AddedJobID: automationStableID("haj", authorization.Workspace.OwnerID, authorization.Page.Id, mutationID, "page-added"),
	}
	for _, rowID := range rowIDs {
		row := rows[rowID]
		frontier, active := frontiers[row.DataSourceID]
		if !active {
			continue
		}
		eventType, jobID := databaseAutomationTriggerPropertyEdited, facts.JobID
		_, found := existing[rowID]
		if !found {
			eventType, jobID = databaseAutomationTriggerRowAdded, facts.AddedJobID
		}
		propertyIDs := make([]string, 0, len(row.Values))
		for propertyID, value := range row.Values {
			if found && databaseRowNotificationSameValue(stored[rowID][propertyID], string(value.Value)) {
				continue
			}
			propertyIDs = append(propertyIDs, propertyID)
		}
		if found && len(propertyIDs) == 0 {
			// Moving a row, or writing the values it holds again (a date
			// picked and its field left at once), edits none of its properties.
			continue
		}
		sort.Strings(propertyIDs)
		facts.Events = append(facts.Events, plannedDatabaseAutomationEvent{
			ID:         automationStableID("hae", authorization.Workspace.OwnerID, authorization.Page.Id, mutationID, rowID),
			DatabaseID: row.DataSourceID, RowID: rowID, EventType: eventType,
			ChangedPropertyIDs: strings.Join(propertyIDs, "\x00"),
			CapturedFrontier:   frontier, JobID: jobID,
		})
	}
	return facts, nil
}

func loadDatabaseAutomationAdmissionFrontiers(
	app core.App, authorization pageAuthorization, databaseIDs map[string]struct{},
) (map[string]int, error) {
	params := dbx.Params{"owner": authorization.Workspace.OwnerID, "pageId": authorization.Page.Id}
	placeholders := make([]string, 0, len(databaseIDs))
	for databaseID := range databaseIDs {
		key := fmt.Sprintf("database%d", len(placeholders))
		params[key] = databaseID
		placeholders = append(placeholders, "{:"+key+"}")
	}
	rows := []databaseAutomationFrontierRow{}
	err := app.DB().NewQuery(`
		SELECT [[database_id]], [[frontier]] FROM {{leafen_database_automation_frontiers}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[active_count]] > 0
			AND [[database_id]] IN (` + strings.Join(placeholders, ",") + `)
	`).Bind(params).All(&rows)
	result := map[string]int{}
	for _, row := range rows {
		result[row.DatabaseID] = row.Frontier
	}
	return result, err
}

func loadDatabaseAutomationExistingRows(
	app core.App, authorization pageAuthorization, rows map[string]*graphDatabaseRow,
) (map[string]struct{}, error) {
	params := dbx.Params{"owner": authorization.Workspace.OwnerID, "pageId": authorization.Page.Id}
	placeholders := make([]string, 0, len(rows))
	for rowID := range rows {
		key := fmt.Sprintf("row%d", len(placeholders))
		params[key] = rowID
		placeholders = append(placeholders, "{:"+key+"}")
	}
	stored := []databaseAutomationExistingRow{}
	err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_database_rows}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId} AND [[id]] IN (` + strings.Join(placeholders, ",") + `)
	`).Bind(params).All(&stored)
	result := map[string]struct{}{}
	for _, row := range stored {
		result[row.ID] = struct{}{}
	}
	return result, err
}

func admitDatabaseAutomationEvents(
	app core.App,
	authorization pageAuthorization,
	actorID, mutationID, requestHash string,
	facts databaseAutomationMutationFacts,
) (bool, error) {
	if len(facts.Events) == 0 {
		return false, nil
	}
	snapshots, err := loadDatabaseAutomationEventSnapshots(app, facts.Events)
	if err != nil {
		return false, err
	}
	return true, insertDatabaseAutomationEvents(app, authorization, actorID, mutationID, requestHash, facts, snapshots)
}

// insertDatabaseAutomationEvents puts a change's events, each with its row's
// values, and the jobs that evaluate them in the automations' queue: a
// database page's rows (admitDatabaseAutomationEvents) or People's rows its
// sync wrote (admitPeopleRowAutomationEvents).
func insertDatabaseAutomationEvents(
	app core.App,
	authorization pageAuthorization,
	actorID, mutationID, requestHash string,
	facts databaseAutomationMutationFacts,
	snapshots map[string]map[string]graphRowValue,
) error {
	now := time.Now().UTC()
	counts := map[string]int{}
	for _, event := range facts.Events {
		counts[event.JobID]++
		snapshot, err := json.Marshal(snapshots[event.RowID])
		if err != nil {
			return err
		}
		_, err = app.DB().Insert("leafen_database_automation_events", dbx.Params{
			"id": event.ID, "owner": authorization.Workspace.OwnerID,
			"workspace_id": authorization.Workspace.WorkspaceID, "page_id": authorization.Page.Id,
			"database_id": event.DatabaseID, "row_id": event.RowID, "actor_id": actorID,
			"mutation_id": mutationID, "request_hash": requestHash, "event_type": event.EventType,
			"changed_property_ids": event.ChangedPropertyIDs, "row_snapshot_json": string(snapshot),
			"captured_frontier": event.CapturedFrontier, "job_id": event.JobID,
			"status": "queued", "created": now.UnixMilli(), "updated": now.UnixMilli(),
		}).Execute()
		if err != nil {
			return err
		}
	}
	payload, _ := json.Marshal(map[string]string{"mutationId": mutationID})
	requestID := "database-automation:" + authorization.Page.Id + ":" + mutationID
	for _, job := range []struct {
		id, requestID string
		notBefore     time.Time
	}{
		{facts.JobID, requestID, now.Add(durableJobReadyDelay)},
		// A page added runs a moment later, with what was typed in it
		// meanwhile, as Notion's does (databaseAutomationPageAddedDelay).
		{facts.AddedJobID, requestID + ":page-added", now.Add(databaseAutomationPageAddedDelay)},
	} {
		if counts[job.id] == 0 {
			continue
		}
		_, err := app.DB().NewQuery(`
			INSERT INTO {{leafen_jobs}}
				([[id]], [[owner]], [[workspace_id]], [[request_id]], [[request_hash]], [[kind]], [[page_id]],
				 [[status]], [[error_code]], [[not_before]], [[created]], [[updated]], [[payload_json]], [[cursor]],
				 [[processed_items]], [[total_items]], [[attempt]], [[max_attempts]], [[chunk_count]])
			VALUES ({:id}, {:owner}, {:workspaceId}, {:requestId}, {:requestHash}, {:kind}, {:pageId},
				'queued', '', {:notBefore}, {:now}, {:now}, {:payload}, '', 0, {:total}, 0, {:maxAttempts}, 0)
		`).Bind(dbx.Params{
			"id": job.id, "owner": authorization.Workspace.OwnerID,
			"workspaceId": authorization.Workspace.WorkspaceID, "requestId": job.requestID,
			"requestHash": requestHash, "kind": databaseAutomationEventJobKind,
			"pageId": authorization.Page.Id, "notBefore": job.notBefore.UnixMilli(),
			"now": now.UnixMilli(), "payload": string(payload), "total": counts[job.id],
			"maxAttempts": defaultDurableJobMaxAttempts,
		}).Execute()
		if err != nil {
			return err
		}
	}
	return nil
}

func loadDatabaseAutomationEventSnapshots(
	app core.App, events []plannedDatabaseAutomationEvent,
) (map[string]map[string]graphRowValue, error) {
	params := dbx.Params{}
	placeholders := make([]string, 0, len(events))
	seen := map[string]struct{}{}
	for _, event := range events {
		if _, duplicate := seen[event.RowID]; duplicate {
			continue
		}
		seen[event.RowID] = struct{}{}
		key := fmt.Sprintf("snapshotRow%d", len(placeholders))
		params[key] = event.RowID
		placeholders = append(placeholders, "{:"+key+"}")
	}
	rows := []databaseAutomationSnapshotValue{}
	err := app.DB().NewQuery(`
		SELECT [[row_id]], [[property_id]], [[type]], [[value]]
		FROM {{leafen_database_row_values}}
		WHERE [[row_id]] IN (` + strings.Join(placeholders, ",") + `)
		ORDER BY [[row_id]], [[property_id]]
	`).Bind(params).All(&rows)
	if err != nil {
		return nil, err
	}
	result := map[string]map[string]graphRowValue{}
	for rowID := range seen {
		result[rowID] = map[string]graphRowValue{}
	}
	for _, row := range rows {
		result[row.RowID][row.PropertyID] = graphRowValue{Type: row.Type, Value: json.RawMessage(row.Value)}
	}
	// A People row's Email is its person's, kept with their account.
	return result, peopleAutomationSnapshotEmails(app, result)
}
