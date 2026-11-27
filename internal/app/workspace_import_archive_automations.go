package app

import (
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// archiveAutomationValue is a database automation as an archive carries it
// (workspace_export_automations.go).
type archiveAutomationValue struct {
	ID             string          `json:"id"`
	PageID         string          `json:"pageId"`
	DataSourceID   string          `json:"dataSourceId"`
	Name           string          `json:"name"`
	ScopeType      string          `json:"scopeType"`
	ViewID         string          `json:"viewId"`
	TriggerType    string          `json:"triggerType"`
	Trigger        json.RawMessage `json:"trigger"`
	ActionDocument json.RawMessage `json:"actionDocument"`
	Status         string          `json:"status"`
	PausedReason   string          `json:"pausedReason"`
	CreatedAt      int64           `json:"createdAt"`
}

// workspaceImportStagedAutomation is an archived automation with the
// imported database's, view's and properties' IDs.
type workspaceImportStagedAutomation struct {
	ID             string          `json:"id"`
	PageID         string          `json:"pageId"`
	DataSourceID   string          `json:"dataSourceId"`
	Name           string          `json:"name"`
	ScopeType      string          `json:"scopeType"`
	ViewID         string          `json:"viewId"`
	Trigger        json.RawMessage `json:"trigger"`
	ActionDocument json.RawMessage `json:"actionDocument"`
	Status         string          `json:"status"`
	PausedReason   string          `json:"pausedReason"`
	CreatedAt      int64           `json:"createdAt"`
}

func appendWorkspaceImportArchiveAutomations(w *workspaceImportArchiveChunkWriter, db *sql.DB) error {
	rows, err := db.Query(`SELECT value_json FROM entities WHERE kind = ? ORDER BY sequence`, workspaceExportItemDatabaseAutomation)
	if err != nil {
		return workspaceImportStorageError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var encoded string
		if rows.Scan(&encoded) != nil {
			return workspaceImportInputError()
		}
		item, err := workspaceImportArchiveAutomationItem(db, encoded)
		if err != nil {
			return err
		}
		if err := w.append(item); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return workspaceImportStorageError(err)
	}
	return nil
}

// workspaceImportArchiveAutomationItem brings back an automation of a
// database the archive holds, checked as making one checks it. Its steps
// and trigger name the imported database's properties and rows; what they
// name outside the archive stays as it was, as a button's does.
func workspaceImportArchiveAutomationItem(db *sql.DB, encoded string) (workspaceImportStagedItem, error) {
	value := archiveAutomationValue{}
	if json.Unmarshal([]byte(encoded), &value) != nil || value.CreatedAt < 0 ||
		databaseAutomationNameAndScopeRefusal(value.Name, databaseAutomationScope{Type: value.ScopeType, ViewID: value.ViewID}) != nil ||
		!utf8.ValidString(value.PausedReason) || len(value.PausedReason) > databaseAutomationMaxIdentifierRunes {
		return workspaceImportStagedItem{}, workspaceImportInputError()
	}
	switch value.Status {
	case databaseAutomationStatusActive, databaseAutomationStatusDisabled, databaseAutomationStatusPaused:
	default:
		return workspaceImportStagedItem{}, workspaceImportInputError()
	}
	id, err := workspaceImportArchiveReference(db, value.ID, workspaceExportItemDatabaseAutomation)
	if err != nil {
		return workspaceImportStagedItem{}, err
	}
	pageID, dataSourceID, viewID, people, err := workspaceImportArchiveAutomationSource(db, value)
	if err != nil {
		return workspaceImportStagedItem{}, err
	}
	triggerJSON, err := rewriteWorkspaceImportArchiveActionJSON(db, "trigger", value.Trigger)
	if err != nil {
		return workspaceImportStagedItem{}, err
	}
	actionJSON, err := rewriteWorkspaceImportArchiveActionJSON(db, "actionDocument", value.ActionDocument)
	if err != nil {
		return workspaceImportStagedItem{}, err
	}
	trigger := databaseAutomationTrigger{}
	if json.Unmarshal(triggerJSON, &trigger) != nil {
		return workspaceImportStagedItem{}, workspaceImportInputError()
	}
	normalized, err := normalizeDatabaseAutomationRule(trigger, actionJSON, time.Now().UTC())
	if err != nil || normalized.TriggerType != value.TriggerType ||
		people && validatePeopleAutomationRule(normalized, "people-email", value.DataSourceID) != nil {
		return workspaceImportStagedItem{}, workspaceImportInputError()
	}
	return workspaceImportStagedItem{PageID: pageID, Automation: &workspaceImportStagedAutomation{
		ID: id, PageID: pageID, DataSourceID: dataSourceID, Name: value.Name,
		ScopeType: value.ScopeType, ViewID: viewID,
		Trigger: json.RawMessage(normalized.TriggerJSON), ActionDocument: json.RawMessage(normalized.ActionJSON),
		Status: value.Status, PausedReason: value.PausedReason, CreatedAt: value.CreatedAt,
	}}, nil
}

// workspaceImportArchiveAutomationSource is where an archived automation
// goes: the imported database's page, data source and view, or — for one on
// People, which the archive names by People's names (peopleArchiveAutomation)
// — People itself, named by its prefix alone until the workspace it lands in
// names it (insertWorkspaceImportAutomation), with the view's own suffix.
func workspaceImportArchiveAutomationSource(db *sql.DB, value archiveAutomationValue) (string, string, string, bool, error) {
	if workspaceID, people := strings.CutPrefix(value.PageID, peopleArchiveSourcePrefix); people {
		viewID := ""
		if workspaceID == "" || strings.Contains(workspaceID, ":") || value.DataSourceID != value.PageID {
			return "", "", "", true, workspaceImportInputError()
		}
		if value.ScopeType == databaseAutomationScopeView {
			viewID = strings.TrimPrefix(value.ViewID, value.PageID+":")
			if viewID == value.ViewID || viewID == "" {
				return "", "", "", true, workspaceImportInputError()
			}
			viewID = ":" + viewID
		}
		return peopleArchiveSourcePrefix, peopleArchiveSourcePrefix, viewID, true, nil
	}
	pageID, err := workspaceImportArchiveHostReference(db, value.PageID)
	if err != nil {
		return "", "", "", false, err
	}
	dataSourceID, err := workspaceImportArchiveReference(db, value.DataSourceID, workspaceExportItemDataSource)
	if err != nil {
		return "", "", "", false, err
	}
	viewID := ""
	if value.ScopeType == databaseAutomationScopeView {
		if viewID, err = workspaceImportArchiveReference(db, value.ViewID, workspaceExportItemView); err != nil {
			return "", "", "", false, err
		}
	}
	return pageID, dataSourceID, viewID, false, nil
}

// rewriteWorkspaceImportArchiveActionJSON remaps an automation's trigger or
// steps as a button's steps are remapped.
func rewriteWorkspaceImportArchiveActionJSON(db *sql.DB, field string, raw json.RawMessage) (json.RawMessage, error) {
	value := any(nil)
	if json.Unmarshal(raw, &value) != nil {
		return nil, workspaceImportInputError()
	}
	rewritten, err := rewriteWorkspaceImportArchiveNode(db, field, value, false, true)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(rewritten)
	if err != nil {
		return nil, workspaceImportInputError()
	}
	return encoded, nil
}

func (r *durableJobRunner) applyWorkspaceArchiveAutomations(
	job storedJob,
	payload workspaceImportJobPayload,
	chunk workspaceImportStagedChunk,
	nextOffset int64,
) error {
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, job.Owner)
		if err != nil || current.Status != durableJobStatusQueued || current.Cursor != job.Cursor {
			return err
		}
		currentPayload, err := decodeWorkspaceImportPayload(current)
		if err != nil || currentPayload.ChunkIndex != payload.ChunkIndex || currentPayload.ItemOffset != 0 {
			if err != nil {
				return err
			}
			return workspaceImportStateErrorAt("archive-automation-frontier")
		}
		authorization, err := authorizeWorkspace(txApp, current.Owner, current.WorkspaceID, permissionWorkspaceWrite)
		if err != nil {
			return err
		}
		now := r.clock.now().UTC()
		for _, item := range chunk.Items {
			if item.Automation == nil || item.Page != nil || item.Operation != nil || item.PageID != item.Automation.PageID {
				return workspaceImportStateErrorAt("archive-automation-item")
			}
			if err := insertWorkspaceImportAutomation(txApp, authorization, current.Owner, *item.Automation, now); err != nil {
				return err
			}
		}
		currentPayload.SourceVerified = true
		currentPayload.ItemOffset = 0
		currentPayload.ChunkIndex++
		currentPayload.ReadyToPublish = chunk.Last
		currentPayload.Phase = workspaceImportPhaseApply
		return r.updateWorkspaceImportPayload(
			txApp, current, currentPayload, strconv.FormatInt(nextOffset, 10),
			current.Processed+len(chunk.Items), true,
		)
	})
}

// insertWorkspaceImportAutomation makes an imported database's automation,
// the importer's, in the state it was archived in: one waiting on rows'
// changes counts on the database's frontier, and a schedule runs next at its
// first time from now.
func insertWorkspaceImportAutomation(
	app core.App,
	authorization workspaceAuthorization,
	actorID string,
	automation workspaceImportStagedAutomation,
	now time.Time,
) error {
	if automation.PageID == peopleArchiveSourcePrefix {
		// People's, on the People database of the workspace it lands in,
		// whose automations a restricted member doesn't reach.
		if automation.DataSourceID != peopleArchiveSourcePrefix || authorization.Restricted {
			return workspaceImportStateErrorAt("archive-automation-people")
		}
		if err := peopleArchiveAutomationIn(app, authorization.WorkspaceID, &automation); err != nil {
			return err
		}
		return insertWorkspaceImportAutomationRecord(app, authorization, actorID, automation, now)
	}
	var sources int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_data_sources}}
		WHERE [[id]] = {:sourceId} AND [[page_id]] = {:pageId}
			AND [[workspace_id]] = {:workspaceId} AND [[owner]] = {:ownerId}
	`).Bind(dbx.Params{
		"sourceId": automation.DataSourceID, "pageId": automation.PageID,
		"workspaceId": authorization.WorkspaceID, "ownerId": authorization.OwnerID,
	}).Row(&sources); err != nil {
		return err
	}
	if sources != 1 {
		return workspaceImportStateErrorAt("archive-automation-source")
	}
	if automation.ScopeType == databaseAutomationScopeView {
		var views int
		if err := app.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_database_views}}
			WHERE [[id]] = {:viewId} AND [[data_source_id]] = {:sourceId} AND [[page_id]] = {:pageId}
				AND [[workspace_id]] = {:workspaceId} AND [[owner]] = {:ownerId}
		`).Bind(dbx.Params{
			"viewId": automation.ViewID, "sourceId": automation.DataSourceID, "pageId": automation.PageID,
			"workspaceId": authorization.WorkspaceID, "ownerId": authorization.OwnerID,
		}).Row(&views); err != nil {
			return err
		}
		if views != 1 {
			return workspaceImportStateErrorAt("archive-automation-view")
		}
	}
	return insertWorkspaceImportAutomationRecord(app, authorization, actorID, automation, now)
}

// insertWorkspaceImportAutomationRecord saves an imported automation on the
// source it was checked against.
func insertWorkspaceImportAutomationRecord(
	app core.App,
	authorization workspaceAuthorization,
	actorID string,
	automation workspaceImportStagedAutomation,
	now time.Time,
) error {
	trigger := databaseAutomationTrigger{}
	if json.Unmarshal(automation.Trigger, &trigger) != nil {
		return workspaceImportStateErrorAt("archive-automation-trigger")
	}
	normalized, err := normalizeDatabaseAutomationRule(trigger, automation.ActionDocument, now)
	if err != nil {
		return workspaceImportStateErrorAt("archive-automation-rule")
	}
	active := automation.Status == databaseAutomationStatusActive
	activeDelta, nextRunAt := 0, int64(0)
	if active && normalized.TriggerType == databaseAutomationTriggerEvents {
		activeDelta = 1
	}
	if active && normalized.TriggerType == databaseAutomationTriggerSchedule {
		nextRunAt = normalized.NextRunAt
	}
	frontier, err := advanceDatabaseAutomationFrontierOf(
		app, authorization.OwnerID, authorization.WorkspaceID, automation.PageID, automation.DataSourceID, activeDelta,
	)
	if err != nil {
		return err
	}
	updated := now.UnixMilli()
	created := automation.CreatedAt
	if created <= 0 || created > updated {
		created = updated
	}
	_, err = app.DB().Insert("leafen_database_automation_definitions", dbx.Params{
		"id": automation.ID, "owner": authorization.OwnerID, "workspace_id": authorization.WorkspaceID,
		"page_id": automation.PageID, "database_id": automation.DataSourceID, "name": automation.Name,
		"scope_type": automation.ScopeType, "view_id": automation.ViewID,
		"trigger_type": normalized.TriggerType, "trigger_json": normalized.TriggerJSON,
		"action_json": normalized.ActionJSON, "status": automation.Status, "revision": 1,
		"created_frontier": frontier, "updated_frontier": frontier, "next_run_at": nextRunAt,
		"paused_reason": automation.PausedReason, "created_by": actorID, "updated_by": actorID,
		"created": created, "updated": updated,
	}).Execute()
	if err != nil {
		return workspaceImportFailureAt(err, "archive-automation-insert")
	}
	return nil
}
