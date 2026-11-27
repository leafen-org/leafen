package app

import (
	"encoding/json"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

type interactiveAutomationPageTarget struct {
	Type       string          `json:"type"`
	DatabaseID string          `json:"databaseId"`
	Filter     json.RawMessage `json:"filter"`
	Limit      int             `json:"limit,omitempty"`
	TimeZone   string          `json:"timeZone,omitempty"`
}

// interactiveAutomationTargetThisPage aims a step at the row its button is on,
// Notion's "이 페이지 편집" (seen 2026-11-02), or at the row that set off a
// database's automation, Notion's "보관 [↗ 페이지 실행]" (seen 2026-11-15).
const interactiveAutomationTargetThisPage = "this_page"

// interactiveAutomationAimsAtPages: a step that edits or archives pages names
// them alike, a database's by filter or this_page (Notion's "페이지 편집
// 위치" and "페이지 보관하기…", seen 2026-11-15).
func interactiveAutomationAimsAtPages(action interactiveAutomationAction) bool {
	return action.Type == interactiveAutomationActionEditPages || action.Type == interactiveAutomationActionArchivePages
}

func interactiveAutomationTargetsThisPage(action interactiveAutomationAction) bool {
	var target interactiveAutomationPageTarget
	return interactiveAutomationAimsAtPages(action) && json.Unmarshal(action.Target, &target) == nil &&
		target.Type == interactiveAutomationTargetThisPage
}

func interactiveAutomationUsesFilter(action interactiveAutomationAction) bool {
	if !interactiveAutomationAimsAtPages(action) {
		return false
	}
	var target interactiveAutomationPageTarget
	var filter map[string]json.RawMessage
	if json.Unmarshal(action.Target, &target) != nil || target.Type == interactiveAutomationTargetThisPage || json.Unmarshal(target.Filter, &filter) != nil {
		return false
	}
	_, legacy := filter["rowIds"]
	return !legacy
}

func interactiveAutomationFilterRequest(action interactiveAutomationAction, reference time.Time) (databaseQueryRequest, error) {
	var target interactiveAutomationPageTarget
	if json.Unmarshal(action.Target, &target) != nil {
		return databaseQueryRequest{}, errInteractiveAutomationInvalid
	}
	group := databaseQueryFilterGroup{Conjunction: databaseQueryConjunctionAnd, Filters: []databaseQueryFilter{}}
	if json.Unmarshal(target.Filter, &group) != nil {
		return databaseQueryRequest{}, errInteractiveAutomationInvalid
	}
	if group.Conjunction == "" {
		group.Conjunction = databaseQueryConjunctionAnd
	}
	stamp := reference.UTC().Format(time.RFC3339Nano)
	return databaseQueryRequest{DataSourceID: target.DatabaseID, FilterGroup: &group, TimeZone: target.TimeZone,
		ReferenceTime: &stamp, Limit: maxDatabaseQueryResultRows}, nil
}

// The database query owner freezes a complete selection in bounded scan and
// computed-dependency turns. Ready result windows then drive bounded writes;
// no total row cap or separate filter evaluator is introduced here.
func prepareInteractiveAutomationFilterTurn(app core.App, auth pageAuthorization, action interactiveAutomationAction,
	progress *interactiveAutomationProgress, reference time.Time) (*interactiveAutomationAction, bool, error) {
	request, err := interactiveAutomationFilterRequest(action, reference)
	if err != nil {
		return nil, false, err
	}
	// A target naming no zone (one saved through the API) reads its days on the
	// clock of the person it runs as, as the screen saves its maker's zone.
	if request.TimeZone == "" {
		request.TimeZone = personLocation(app, auth.Workspace.PrincipalID).String()
	}
	request.Cursor = &progress.FilterCursor
	// The database is read where it is, which may be another page.
	pageID, err := interactiveAutomationDatabasePage(app, auth, request.DataSourceID)
	if err != nil {
		return nil, false, err
	}
	canonical, cursor, hash, err := prepareDatabaseQueryRequest(auth.Workspace.PrincipalID, pageID, request)
	if err != nil {
		return nil, false, err
	}
	turn, err := advanceDatabaseQueryTurnTransaction(app, auth.Workspace.PrincipalID, pageID, canonical, cursor, hash)
	if err != nil {
		switch err.Error() {
		case errorCodeDatabaseQueryCursorInvalid:
			return nil, false, refusedUnlessStorage(errInteractiveAutomationConflict, err)
		case errorCodeDatabaseQueryInvalid:
			return nil, false, refusedUnlessStorage(errInteractiveAutomationInvalid, err)
		case errorCodeDatabaseQueryNotFound:
			return nil, false, refusedUnlessStorage(errInteractiveAutomationNotFound, err)
		}
		return nil, false, err
	}
	progress.FilterCursor = ""
	if turn.Response.NextCursor != nil {
		progress.FilterCursor = *turn.Response.NextCursor
	}
	more := turn.Response.State == databaseQueryStatePending || turn.Response.HasMore
	if len(turn.Response.Rows) == 0 {
		return nil, more, nil
	}
	ids := make([]string, 0, len(turn.Response.Rows))
	for _, row := range turn.Response.Rows {
		ids = append(ids, row.ID)
	}
	selected := action
	selected.Target, err = json.Marshal(map[string]any{"type": "database", "databaseId": request.DataSourceID,
		"filter": map[string]any{"rowIds": ids}, "limit": maxDatabaseQueryResultRows})
	return &selected, more, err
}

// One local action phase, shared by buttons and durable event/schedule runs.
// Confirmations and native copies have their interactive owners outside it.
func planInteractiveAutomationActionTurn(app core.App, auth pageAuthorization, source interactiveAutomationSource,
	document interactiveAutomationDocument, executionID string, reference time.Time,
	progress interactiveAutomationProgress) (interactiveAutomationPlan, []interactiveAutomationAction, error) {
	begin, end := progress.NextAction, progress.NextAction
	if begin < 0 || begin > len(document.Actions) {
		return interactiveAutomationPlan{}, nil, errInteractiveAutomationUnavailable
	}
	more := false
	actions := []interactiveAutomationAction{}
	if begin < len(document.Actions) && interactiveAutomationUsesFilter(document.Actions[begin]) {
		selected, pending, err := prepareInteractiveAutomationFilterTurn(app, auth, document.Actions[begin], &progress, reference)
		if err != nil {
			return interactiveAutomationPlan{}, nil, err
		}
		more = pending
		if selected != nil {
			actions = append(actions, *selected)
		}
		if !more {
			end++
		}
	} else {
		for end < len(document.Actions) && document.Actions[end].Type != interactiveAutomationActionShowConfirmation &&
			document.Actions[end].SourceBlockID == "" && !interactiveAutomationUsesFilter(document.Actions[end]) {
			end++
		}
		actions = document.Actions[begin:end]
	}
	phase := document
	phase.Actions = actions
	plan, err := planInteractiveAutomationActions(app, auth, source, phase, executionID, reference, progress)
	if err != nil {
		return interactiveAutomationPlan{}, nil, err
	}
	plan.progress.NextAction = end
	plan.progress.Done = !more && end == len(document.Actions)
	return plan, plan.Deliveries, nil
}
