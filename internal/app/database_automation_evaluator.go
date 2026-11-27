package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

type databaseAutomationEvaluationCandidate struct {
	EventID string `db:"event_id"`
	OwnerID string `db:"owner"`
	// ActorID is who set it off — the person a notification to "whoever
	// triggered" reaches, none for an anonymous form's response; RunnerID
	// is whose rights it runs with: whoever last saved or turned it on.
	ActorID            string `db:"actor_id"`
	RunnerID           string `db:"runner_id"`
	WorkspaceID        string `db:"workspace_id"`
	PageID             string `db:"page_id"`
	DatabaseID         string `db:"database_id"`
	RowID              string `db:"row_id"`
	EventType          string `db:"event_type"`
	ChangedPropertyIDs string `db:"changed_property_ids"`
	RowSnapshotJSON    string `db:"row_snapshot_json"`
	// RunSnapshotJSON is a page added's values when its automation runs, a
	// moment after (refreshDatabaseAutomationAddedPages), which its actions
	// read; "" reads RowSnapshotJSON.
	RunSnapshotJSON    string `db:"-"`
	EventCreated       int64  `db:"event_created"`
	CapturedFrontier   int    `db:"captured_frontier"`
	DefinitionID       string `db:"definition_id"`
	DefinitionRevision int    `db:"definition_revision"`
	UpdatedFrontier    int    `db:"updated_frontier"`
	DefinitionStatus   string `db:"definition_status"`
	TriggerType        string `db:"trigger_type"`
	ScopeType          string `db:"scope_type"`
	ViewID             string `db:"view_id"`
	TriggerJSON        string `db:"trigger_json"`
	ActionJSON         string `db:"action_json"`
	// DefinitionName is what the automation, or a button, is called: the
	// name its notifications say they come from.
	DefinitionName string `db:"definition_name"`
}

type databaseAutomationEvaluationState struct {
	authorizations map[string]pageAuthorization
	dlpScanners    map[string]organizationDLPContentScanner
	inactive       map[string]struct{}
	viewContexts   map[string]databaseQueryContext
	// statusProperties are the status properties a condition names a group of (databaseAutomationStatusGroupsAsOptions).
	statusProperties map[string]storedDatabaseQueryProperty
	// graphChange gathers what the chunk's automations changed in its page's graph (database_automation_changed_parts.go).
	graphChange *databaseAutomationGraphChange
}

type databaseAutomationRowHeader struct {
	ID                string `db:"id"`
	Position          string `db:"position"`
	SubitemParentID   string `db:"subitem_parent_id"`
	SubitemChildCount int    `db:"subitem_child_count"`
}

func newDatabaseAutomationEvaluationState(graphChange *databaseAutomationGraphChange) *databaseAutomationEvaluationState {
	return &databaseAutomationEvaluationState{
		authorizations:   map[string]pageAuthorization{},
		dlpScanners:      map[string]organizationDLPContentScanner{},
		inactive:         map[string]struct{}{},
		viewContexts:     map[string]databaseQueryContext{},
		statusProperties: map[string]storedDatabaseQueryProperty{},
		graphChange:      graphChange,
	}
}

func (r *durableJobRunner) applyDatabaseAutomationEventChunk(ctx context.Context, job storedJob) error {
	if r.ygo == nil {
		return errDatabaseAutomationUnavailable
	}
	var graphChange *databaseAutomationGraphChange
	err := r.ygo.mutateRoomDocumentContext(
		ctx,
		job.PageID,
		func(_ context.Context, txApp core.App, doc *crdt.Doc) (bool, error) {
			graphChange = &databaseAutomationGraphChange{}
			return evaluateDatabaseAutomationJobChunk(txApp, doc, job.ID, graphChange)
		},
		// What the automations changed reaches the page's open readers as a
		// person's change does (2026-10-30: it showed only on reload), saying
		// what it changed so they read that alone.
		func(_ context.Context, txApp core.App, update, _ []byte) error {
			return recordDatabaseAutomationGraphChange(txApp, job.Owner, job.PageID,
				"database-automation:"+job.ID+":"+newDiagnosticReference(), update, graphChange)
		},
	)
	if err == nil {
		r.notify()
	}
	return err
}

// evaluateDatabaseAutomationJobChunk evaluates a chunk of a run's events
// against the automations waiting on them, in the transaction of the
// source's page room (doc), and reports whether it changed the page, what it
// changed gathered in graphChange.
func evaluateDatabaseAutomationJobChunk(txApp core.App, doc *crdt.Doc, jobID string, graphChange *databaseAutomationGraphChange) (bool, error) {
	current, err := loadStoredJob(txApp, jobID, "")
	if errors.Is(err, sql.ErrNoRows) || err == nil && current.Status != durableJobStatusQueued {
		return false, nil
	}
	if err != nil || current.Kind != databaseAutomationEventJobKind {
		return false, err
	}
	fenced, err := blockMovePageFenceActive(txApp, current.PageID)
	if err != nil {
		return false, err
	}
	if fenced {
		now := time.Now().UTC()
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}} SET [[not_before]]={:notBefore},[[updated]]={:updated}
			WHERE [[id]]={:id} AND [[status]]='queued'
		`).Bind(dbx.Params{
			"notBefore": now.Add(durableJobReadyDelay).UnixMilli(),
			"updated":   now.UnixMilli(), "id": current.ID,
		}).Execute()
		return false, err
	}
	var sourceTrashed int
	if err := txApp.DB().NewQuery(`
		SELECT EXISTS (
			SELECT 1 FROM {{leafen_database_automation_events}} AS [[event]]
			WHERE [[event]].[[job_id]] = {:jobId}
				AND NOT (` + databaseSourceNotTrashedSQL("[[event]].[[database_id]]") + `)
		)
	`).Bind(dbx.Params{"jobId": current.ID}).Row(&sourceTrashed); err != nil {
		return false, err
	}
	if sourceTrashed == 1 {
		now := time.Now().UTC()
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}} SET [[not_before]]={:notBefore},[[updated]]={:updated}
			WHERE [[id]]={:id} AND [[status]]='queued'
		`).Bind(dbx.Params{
			"notBefore": now.Add(durableJobReadyDelay).UnixMilli(),
			"updated":   now.UnixMilli(), "id": current.ID,
		}).Execute()
		return false, err
	}
	candidates, err := loadDatabaseAutomationEvaluationCandidates(txApp, current.ID)
	if err != nil {
		return false, err
	}
	gone, err := refreshDatabaseAutomationAddedPages(txApp, candidates)
	if err != nil {
		return false, err
	}
	changed := false
	state := newDatabaseAutomationEvaluationState(graphChange)
	for index, candidate := range candidates {
		if gone[index] {
			if err := insertDatabaseAutomationOutcome(txApp, candidate, databaseAutomationOutcomeStale, "", "{}"); err != nil {
				return false, err
			}
			continue
		}
		applied, err := evaluateDatabaseAutomationCandidate(txApp, doc, state, candidate)
		if err != nil {
			return false, err
		}
		changed = changed || applied
	}
	remaining, err := hasDatabaseAutomationEvaluationCandidates(txApp, current.ID)
	if err != nil {
		return false, err
	}
	now := time.Now().UTC().UnixMilli()
	status := durableJobStatusCompleted
	notBefore := now
	processed := current.Processed
	if remaining {
		status = durableJobStatusQueued
		notBefore = time.UnixMilli(now).Add(durableJobReadyDelay).UnixMilli()
	} else {
		processed = current.Total
	}
	cursor := current.Cursor
	if len(candidates) > 0 {
		last := candidates[len(candidates)-1]
		cursor = last.EventID + "\x00" + last.DefinitionID
	}
	_, err = txApp.DB().NewQuery(`
		UPDATE {{leafen_jobs}} SET [[status]] = {:status}, [[not_before]] = {:notBefore},
			[[cursor]] = {:cursor}, [[processed_items]] = {:processed},
			[[chunk_count]] = [[chunk_count]] + 1, [[updated]] = {:updated}
		WHERE [[id]] = {:id} AND [[status]] = 'queued'
	`).Bind(dbx.Params{
		"status": status, "notBefore": notBefore, "cursor": cursor,
		"processed": processed, "updated": now, "id": current.ID,
	}).Execute()
	if err != nil {
		return false, err
	}
	if !remaining {
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_database_automation_events}} SET [[status]] = 'completed', [[updated]] = {:updated}
			WHERE [[job_id]] = {:jobId};
			UPDATE {{leafen_database_automation_schedule_occurrences}}
			SET [[status]] = 'completed', [[updated]] = {:updated}
			WHERE [[job_id]] = {:jobId}
		`).Bind(dbx.Params{"updated": now, "jobId": current.ID}).Execute()
	}
	return changed, err
}

func evaluateDatabaseAutomationCandidate(
	app core.App, doc *crdt.Doc, state *databaseAutomationEvaluationState,
	candidate databaseAutomationEvaluationCandidate,
) (bool, error) {
	status := databaseAutomationOutcomeStale
	resultJSON := "{}"
	if _, inactive := state.inactive[candidate.DefinitionID]; inactive {
		return false, insertDatabaseAutomationOutcome(app, candidate, status, "", resultJSON)
	}
	if candidate.DefinitionStatus == databaseAutomationStatusActive &&
		candidate.UpdatedFrontier <= candidate.CapturedFrontier {
		trigger := databaseAutomationTrigger{}
		if json.Unmarshal([]byte(candidate.TriggerJSON), &trigger) != nil {
			return false, errDatabaseAutomationUnavailable
		}
		// A status's whole group is the options in it as it runs.
		databaseAutomationStatusGroupsAsOptions(app, state, candidate, &trigger)
		// The row as the edit left it, for a trigger that names what it holds.
		values := map[string]graphRowValue{}
		_ = json.Unmarshal([]byte(candidate.RowSnapshotJSON), &values)
		if databaseAutomationTriggerMatches(trigger, candidate.EventType, candidate.ChangedPropertyIDs, values) {
			// What stops the automation is the step that fails, which its
			// panel names. It runs with the rights of whoever last saved or
			// turned it on, as Notion's runs for anyone's change — a person
			// who may edit only the row, or read only the database, sets it
			// off without stopping it for everyone (seen 2026-10-15).
			reason := databaseAutomationPausedReasonPermission
			authorization, err := state.authorize(app, candidate)
			if err == nil {
				reason = databaseAutomationPausedReasonView
				matchesView, viewErr := true, error(nil)
				if candidate.EventType != databaseAutomationScheduleEvent {
					matchesView, viewErr = databaseAutomationViewMatches(app, state, authorization, candidate)
				}
				if viewErr == nil && !matchesView {
					return false, insertDatabaseAutomationOutcome(app, candidate, status, "", resultJSON)
				}
				if viewErr == nil {
					reason = databaseAutomationPausedReasonAction
					normalized, normalizeErr := normalizeDatabaseAutomationDocument(json.RawMessage(candidate.ActionJSON))
					if normalizeErr != nil {
						viewErr = normalizeErr
					}
					if normalizeErr == nil {
						sourceKind := "database_event"
						if candidate.EventType == databaseAutomationScheduleEvent {
							sourceKind = "database_schedule"
						}
						source := interactiveAutomationSource{
							Kind: sourceKind, ID: candidate.DefinitionID,
							Document: json.RawMessage(candidate.ActionJSON), TriggerDatabaseID: candidate.DatabaseID,
							TriggerRowID: candidate.RowID, TriggerSnapshotJSON: candidate.runSnapshotJSON(),
							ActorID: candidate.ActorID,
						}
						outcomeID := automationStableID("hao", candidate.EventID, candidate.DefinitionID)
						if databaseAutomationHasStructuredFilter(normalized.Document) {
							changed, pending, result, err := advanceDatabaseAutomationFilteredActions(app, doc, state, authorization, candidate, source, normalized, outcomeID)
							if err == nil {
								if pending {
									return changed, nil
								}
								return changed, insertDatabaseAutomationOutcome(app, candidate, databaseAutomationOutcomeCompleted, "", result)
							}
							var mutationError *databaseAutomationFilterMutationError
							if errors.As(err, &mutationError) {
								return false, err
							}
							return pauseDatabaseAutomationFilteredCandidate(app, state, candidate, databaseAutomationFailureReason(err))
						}
						plan, planErr := planInteractiveAutomationActions(
							app, authorization, source, normalized.Document, outcomeID,
							time.UnixMilli(candidate.EventCreated).UTC(),
						)
						if planErr == nil {
							dlpScanner, dlpErr := state.dlpScanner(app, authorization)
							if dlpErr != nil {
								reason = databaseAutomationPausedReasonDlp
							} else {
								// What it changes reads as Leafen's, as Notion writes it as its system
								// user (seen 2026-11-05); the person's authority ran it.
								plan.SystemAuthored = true
								changed, applyErr := state.applyPlan(app, doc, authorization, plan, dlpScanner)
								if applyErr == nil {
									applyErr = queueInteractiveAutomationOtherPageWrites(app, authorization, plan, outcomeID)
								}
								if applyErr == nil {
									applyErr = queueInteractiveAutomationTemplateRows(app, authorization, plan)
								}
								if applyErr == nil {
									deliveryIDs, deliveryErr := admitDatabaseAutomationDeliveries(
										app, candidate, plan.Deliveries, outcomeID,
										time.UnixMilli(candidate.EventCreated).UTC(),
									)
									if deliveryErr != nil {
										reason = databaseAutomationPausedReasonDelivery
									} else {
										status = databaseAutomationOutcomeCompleted
										payload, _ := json.Marshal(map[string]any{
											"effects": plan.Effects, "deliveries": deliveryIDs,
										})
										if len(payload) <= databaseAutomationMaxResultBytes {
											resultJSON = string(payload)
											return changed, insertDatabaseAutomationOutcome(app, candidate, status, "", resultJSON)
										}
									}
								} else {
									reason = databaseAutomationFailureReason(applyErr)
								}
							}
						}
					}
				}
			}
			status = databaseAutomationOutcomePaused
			paused, err := pauseDatabaseAutomationDefinition(app, candidate, reason)
			if err != nil {
				return false, err
			}
			if !paused {
				return false, insertDatabaseAutomationOutcome(
					app, candidate, databaseAutomationOutcomeStale, "", resultJSON,
				)
			}
			state.inactive[candidate.DefinitionID] = struct{}{}
			return false, insertDatabaseAutomationOutcome(
				app, candidate, status, errorCodeDatabaseAutomationUnavailable, resultJSON,
			)
		}
	}
	return false, insertDatabaseAutomationOutcome(app, candidate, status, "", resultJSON)
}

func (candidate databaseAutomationEvaluationCandidate) runSnapshotJSON() string {
	if candidate.RunSnapshotJSON != "" {
		return candidate.RunSnapshotJSON
	}
	return candidate.RowSnapshotJSON
}

func (state *databaseAutomationEvaluationState) authorize(
	app core.App, candidate databaseAutomationEvaluationCandidate,
) (pageAuthorization, error) {
	key := candidate.RunnerID + "\x00" + candidate.PageID
	if authorization, ok := state.authorizations[key]; ok {
		return authorization, nil
	}
	authorization, err := authorizeDatabaseAutomationSource(app, candidate.RunnerID, candidate.PageID, permissionWorkspaceWrite)
	if err != nil {
		return pageAuthorization{}, err
	}
	if authorization.Workspace.OwnerID != candidate.OwnerID ||
		authorization.Workspace.WorkspaceID != candidate.WorkspaceID {
		return pageAuthorization{}, errDatabaseAutomationUnavailable
	}
	state.authorizations[key] = authorization
	return authorization, nil
}

func (state *databaseAutomationEvaluationState) dlpScanner(
	app core.App, authorization pageAuthorization,
) (organizationDLPContentScanner, error) {
	organizationID := authorization.Workspace.Organization.OrganizationID
	if scanner, ok := state.dlpScanners[organizationID]; ok {
		return scanner, nil
	}
	scanner, err := newOrganizationDLPContentScanner(app, organizationID)
	if err != nil {
		return organizationDLPContentScanner{}, err
	}
	state.dlpScanners[organizationID] = scanner
	return scanner, nil
}

func databaseAutomationViewMatches(
	app core.App, state *databaseAutomationEvaluationState, authorization pageAuthorization,
	candidate databaseAutomationEvaluationCandidate,
) (bool, error) {
	if candidate.ScopeType == databaseAutomationScopeDatabase {
		return true, nil
	}
	if candidate.ScopeType != databaseAutomationScopeView || candidate.ViewID == "" {
		return false, errDatabaseAutomationUnavailable
	}
	contextKey := candidate.RunnerID + "\x00" + candidate.OwnerID + "\x00" + candidate.PageID + "\x00" +
		candidate.DatabaseID + "\x00" + candidate.ViewID
	queryContext, ok := state.viewContexts[contextKey]
	if !ok {
		queryContext = databaseQueryContext{}
		loaded, err := loadDatabaseQueryContext(app, storedDatabaseQueryJob{
			PrincipalID: candidate.RunnerID, ContentOwnerID: authorization.Workspace.OwnerID,
			WorkspaceID: candidate.WorkspaceID, PageID: candidate.PageID,
			DataSourceID: candidate.DatabaseID, ViewID: candidate.ViewID,
		})
		if err != nil {
			return false, err
		}
		queryContext = loaded
		state.viewContexts[contextKey] = queryContext
	}
	referenceTime := time.UnixMilli(candidate.EventCreated).UTC().Format(time.RFC3339Nano)
	viewID := candidate.ViewID
	// The view's days ("today", "this week") are those of the person it runs
	// as, as its execution date's (executionDay), not UTC's.
	plan, err := compileDatabaseQueryPlan(databaseQueryRequest{
		DataSourceID: candidate.DatabaseID, ViewID: &viewID,
		TimeZone: personLocation(app, candidate.RunnerID).String(), ReferenceTime: &referenceTime, Limit: 1,
	}, queryContext)
	if err != nil {
		return false, err
	}
	header := databaseAutomationRowHeader{}
	err = app.DB().NewQuery(`
		SELECT [[id]], [[position]], [[subitem_parent_id]], [[subitem_child_count]]
		FROM {{leafen_database_rows}}
		WHERE [[owner]] = {:owner} AND [[page_id]] = {:pageId}
			AND [[data_source_id]] = {:databaseId} AND [[id]] = {:rowId}
			AND ` + databaseRowNotTrashedSQL("[[id]]") + `
	`).Bind(dbx.Params{
		"owner": candidate.OwnerID, "pageId": candidate.PageID,
		"databaseId": candidate.DatabaseID, "rowId": candidate.RowID,
	}).One(&header)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	values := map[string]graphRowValue{}
	if json.Unmarshal([]byte(candidate.RowSnapshotJSON), &values) != nil {
		return false, errDatabaseAutomationUnavailable
	}
	row := storedDatabaseQueryRow{
		ID: header.ID, Position: header.Position,
		SubitemParentID: header.SubitemParentID, SubitemChildCount: header.SubitemChildCount,
		Values: values, Computed: map[string]databaseComputedValue{},
	}
	boundedRows, dependencyIDs, err := boundedDatabaseQueryRows([]storedDatabaseQueryRow{row}, queryContext)
	if err != nil || len(boundedRows) != 1 {
		if err != nil {
			return false, err
		}
		return false, failedBecause(errDatabaseAutomationUnavailable, err)
	}
	dependencies, err := loadDatabaseQueryComputedRowsByID(
		app, candidate.OwnerID, queryContext.readablePages(candidate.PageID), dependencyIDs,
	)
	if err != nil {
		return false, err
	}
	_, matched, err := evaluateDatabaseQueryRow(
		row, dependencies, nil, queryContext, plan, candidate.ActorID, candidate.PageID,
	)
	return matched, err
}
