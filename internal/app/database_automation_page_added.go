package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// databaseAutomationPageAddedDelay is how long a "페이지 추가" automation
// waits after the page is made before it runs, with the page as it is then:
// what was typed in it meanwhile. Notion's runs three to four and a half
// seconds after, typing or not (measured 2026-11-09 on six pages: a title
// typed 3 or 4.3 seconds after was in, one typed 3.4, 4.1 or 5 seconds
// after was not). The wait is the run's job's not_before, so a restart
// keeps it.
const databaseAutomationPageAddedDelay = 4 * time.Second

// refreshDatabaseAutomationAddedPages gives each page-added candidate the
// page as it is when the automation runs, which it waited for: its actions
// read its values then (RunSnapshotJSON) and run at that time, while which
// properties the adding set, and the view it was added to, are judged as it
// was added — an edit made meanwhile runs its own "속성 수정" once. A page
// gone or in the trash by then runs nothing; its candidates come back in
// gone.
func refreshDatabaseAutomationAddedPages(
	app core.App, candidates []databaseAutomationEvaluationCandidate,
) (gone map[int]bool, err error) {
	events := []plannedDatabaseAutomationEvent{}
	for _, candidate := range candidates {
		if databaseAutomationAddedPageWaits(candidate) {
			events = append(events, plannedDatabaseAutomationEvent{RowID: candidate.RowID})
		}
	}
	gone = map[int]bool{}
	if len(events) == 0 {
		return gone, nil
	}
	snapshots, err := loadDatabaseAutomationEventSnapshots(app, events)
	if err != nil {
		return nil, err
	}
	present, err := loadDatabaseAutomationLiveRows(app, events)
	if err != nil {
		return nil, err
	}
	for index := range candidates {
		candidate := &candidates[index]
		if !databaseAutomationAddedPageWaits(*candidate) {
			continue
		}
		if _, live := present[candidate.RowID]; !live {
			gone[index] = true
			continue
		}
		encoded, err := json.Marshal(snapshots[candidate.RowID])
		if err != nil {
			return nil, err
		}
		candidate.RunSnapshotJSON = string(encoded)
		// It runs, and reads "now", when its wait ends.
		candidate.EventCreated += databaseAutomationPageAddedDelay.Milliseconds()
	}
	return gone, nil
}

func databaseAutomationAddedPageWaits(candidate databaseAutomationEvaluationCandidate) bool {
	return candidate.EventType == databaseAutomationTriggerRowAdded
}

func loadDatabaseAutomationLiveRows(
	app core.App, events []plannedDatabaseAutomationEvent,
) (map[string]struct{}, error) {
	params := dbx.Params{}
	placeholders := make([]string, 0, len(events))
	for _, event := range events {
		key := fmt.Sprintf("liveRow%d", len(placeholders))
		params[key] = event.RowID
		placeholders = append(placeholders, "{:"+key+"}")
	}
	rows := []string{}
	err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_database_rows}}
		WHERE [[id]] IN (` + strings.Join(placeholders, ",") + `)
			AND ` + databaseRowNotTrashedSQL("[[id]]") + `
	`).Bind(params).Column(&rows)
	if err != nil {
		return nil, err
	}
	present := make(map[string]struct{}, len(rows))
	for _, id := range rows {
		present[id] = struct{}{}
	}
	return present, nil
}
