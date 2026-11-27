package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	databaseSprintRunnerStoreKey = "leafenDatabaseSprintRunner"
	databaseSprintRunnerInterval = time.Minute
	databaseSprintRunnerBatch    = 16
)

func trashDatabaseSprintPage(app core.App, actorID, pageID, requestID string) error {
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return nil
	}
	if page.GetBool("in_trash") {
		return nil
	}
	input := normalizePageMetadataInput(pageMetadataInput{
		Title: page.GetString("title"), ParentID: page.GetString("parent_id"), InTrash: true,
		MutationID: "sprint-trash-" + databaseSprintDeterministicID("sprints-trash", pageID, requestID),
		BaseCursor: page.GetInt("cursor"),
	})
	_, status, err := applyPageMetadataWithLock(app, actorID, pageID, input, pageMetadataRequestHash(pageID, input), pageLockEnforced)
	if err == nil && status != pageMetadataApplied && status != pageMetadataPageMissing {
		return errDatabaseSprintConflict
	}
	return err
}

func databaseSprintRelationIDs(records databasePageRecords, rowID, propertyID string) []string {
	cell, exists, err := loadDatabaseCellRecord(records.app, records.pageID, rowID, propertyID)
	if err != nil || !exists {
		return []string{}
	}
	ids := []string{}
	if values, ok := cell.Value.([]any); ok {
		for _, value := range values {
			if id, ok := value.(string); ok && id != "" {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// databaseSprintRowUpdate rewrites some of a row's values, keeping its place.
func databaseSprintRowUpdate(records databasePageRecords, rowID string, values map[string]graphRowValue) graphOperation {
	stored, _, _ := loadDatabaseRowRecord(records.app, records.pageID, rowID)
	var parent *string
	if parentID := stored.SubitemParentID; parentID != "" {
		parent = &parentID
	}
	return graphOperation{Kind: graphOperationKindUpsertRow, Row: &graphDatabaseRow{
		ID: rowID, DataSourceID: stored.DataSourceID, SubitemParentID: parent, Position: stored.Position,
		Values: values,
	}}
}

// databaseTaskIsComplete says whether a task is done: its status is one of
// the options of the task status property's Complete group, or, for a
// checkbox status, checked.
func databaseTaskIsComplete(app core.App, roles databaseTaskRoles) (func(rowID string) bool, error) {
	property := struct {
		Type   string `db:"type"`
		Config string `db:"config"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[type]], [[config]] FROM {{leafen_database_properties}} WHERE [[id]] = {:id}
	`).Bind(dbx.Params{"id": roles.StatusPropertyID}).One(&property); err != nil {
		return nil, err
	}
	complete := map[string]bool{}
	config := struct {
		StatusGroups []map[string]any `json:"statusGroups"`
	}{}
	_ = json.Unmarshal([]byte(property.Config), &config)
	for index, group := range config.StatusGroups {
		if notionStatusGroupIndex(config.StatusGroups, index) == 2 {
			for _, option := range mapStrings(group, "optionIds") {
				complete[option] = true
			}
		}
	}
	return func(rowID string) bool {
		raw := ""
		_ = app.DB().NewQuery(`
			SELECT [[value]] FROM {{leafen_database_row_values}} WHERE [[row_id]] = {:rowId} AND [[property_id]] = {:propertyId}
		`).Bind(dbx.Params{"rowId": rowID, "propertyId": roles.StatusPropertyID}).Row(&raw)
		var value any
		// A status that sets none reads its default (databaseValueAsRead).
		_ = json.Unmarshal(databaseValueAsRead(property.Type, property.Config, json.RawMessage(raw), ""), &value)
		if property.Type == graphValueTypeCheckbox {
			checked, _ := value.(bool)
			return checked
		}
		option, _ := value.(string)
		return complete[option]
	}, nil
}

// databaseSprintCompletion is how a sprint ends: unfinished tasks move as
// `Mode` says, and the sprint keeps its planned dates while the next one
// takes those chosen for it, by default from the first start day after the
// ended sprint's end for the sprint length, as Notion's (seen 2026-11-03).
type databaseSprintCompletion struct {
	Mode       string
	Automatic  bool
	Start, End string
	// Undated leaves the next sprint without dates, as clearing them does.
	Undated bool
}

// completeDatabaseSprint ends the current sprint as Notion's does (seen
// 2026-11-03): Current becomes Last, Last becomes Past, Next becomes Current,
// Future becomes Next and a new Future sprint, undated, is planned. It
// tells how many sprints it made and changed, and how many tasks it moved,
// and names the new Future sprint, which its note opens.
func completeDatabaseSprint(
	ctx context.Context,
	app core.App,
	ygo *pocketBaseYgoPersistence,
	actorID string,
	stored storedDatabaseSprints,
	completion databaseSprintCompletion,
	requestID string,
	now time.Time,
) (databaseSprintChangesWire, error) {
	changes := databaseSprintChangesWire{}
	mode, automatic := completion.Mode, completion.Automatic
	// A sprint completing makes the next one, named in the language of the
	// person completing it.
	title := generatedNamesFor(ctx, app, actorID).Sprints.Title
	if mode != "next" && mode != "backlog" && mode != "keep" {
		return changes, databaseToolbarRefused("incompleteTasks is next, backlog or keep, not %q", mode)
	}
	summaries, err := loadDatabaseSprintSummaries(app, stored)
	if err != nil {
		return changes, err
	}
	// The newest sprint of each status, but the first of those planned
	// after the next one.
	byStatus := map[string]databaseSprintSummaryWire{}
	for _, summary := range summaries {
		if _, seen := byStatus[summary.Status]; !seen || summary.Status == databaseSprintStatusFuture {
			byStatus[summary.Status] = summary
		}
	}
	current, hasCurrent := byStatus[databaseSprintStatusCurrent]
	if !hasCurrent {
		return changes, databaseToolbarStateRefused("there is no current sprint to complete")
	}
	next, hasNext := byStatus[databaseSprintStatusNext]
	future, hasFuture := byStatus[databaseSprintStatusFuture]
	last, hasLast := byStatus[databaseSprintStatusLast]
	roles, err := loadDatabaseTaskRoles(app, stored.DataSourceID)
	if err != nil {
		return changes, err
	}
	titlePropertyID := ""
	if err := app.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_database_properties}}
		WHERE [[data_source_id]] = {:sprints} AND [[type]] = 'title' LIMIT 1
	`).Bind(dbx.Params{"sprints": stored.SprintsDataSourceID}).Row(&titlePropertyID); err != nil {
		return changes, err
	}
	id := func(name string) string {
		return databaseSprintDeterministicID("sprint-complete", stored.DataSourceID, requestID, name)
	}
	// A new sprint is a page (page_ids.go).
	page := func(name string) string {
		return derivedPageID("sprint-complete", stored.DataSourceID, requestID, name)
	}
	// An undated current sprint is taken to end today.
	currentEnd, endErr := time.Parse(time.DateOnly, current.End)
	if endErr != nil {
		currentEnd = databaseSprintDay(now, stored.TimeZone)
	}
	// The next sprint starts on the first start day after the current one.
	newStart := currentEnd.AddDate(0, 0, 1)
	for int(newStart.Weekday()) != stored.StartWeekday {
		newStart = newStart.AddDate(0, 0, 1)
	}
	newEnd := databaseSprintEnd(newStart, stored.DurationWeeks)
	if !automatic && (completion.Start != "" || completion.End != "") {
		start, startErr := time.Parse(time.DateOnly, completion.Start)
		end, endErr := time.Parse(time.DateOnly, completion.End)
		if startErr != nil || endErr != nil {
			return changes, databaseToolbarRefused("sprintStart and sprintEnd are dates as YYYY-MM-DD")
		}
		if end.Before(start) || end.Sub(start) > 366*24*time.Hour {
			return changes, databaseToolbarRefused("sprintEnd is on or after sprintStart and within a year of it")
		}
		newStart, newEnd = start, end
	}
	sequence := stored.SprintSequence
	newCurrentID, createCurrent := next.ID, !hasNext
	if createCurrent {
		sequence++
		newCurrentID = page("current")
	}
	currentSequence := sequence
	newNextID, createNext := future.ID, !hasFuture
	if createNext {
		sequence++
		newNextID = page("next")
	}
	nextSequence := sequence
	sequence++
	newFutureID := page("future")

	// Every task in the current sprint, a window at a time.
	taskIDs := []string{}
	for after := ""; ; {
		window := []string{}
		if err := app.DB().NewQuery(`
			SELECT [[cell]].[[row_id]] FROM {{leafen_database_row_values}} AS [[cell]]
			WHERE [[cell]].[[owner]] = {:owner} AND [[cell]].[[property_id]] = {:property}
				AND [[cell]].[[row_id]] > {:after}
				AND EXISTS (SELECT 1 FROM json_each([[cell]].[[value]]) AS [[item]] WHERE [[item]].[[value]] = {:sprint})
			ORDER BY [[cell]].[[row_id]] LIMIT {:limit}
		`).Bind(dbx.Params{
			"owner": stored.Owner, "property": stored.TaskSprintPropertyID, "sprint": current.ID,
			"after": after, "limit": maxGraphMutationOperations,
		}).Column(&window); err != nil {
			return changes, err
		}
		taskIDs = append(taskIDs, window...)
		if len(window) < maxGraphMutationOperations {
			break
		}
		after = window[len(window)-1]
	}
	moved := []string{}
	if mode != "keep" && roles.StatusPropertyID != "" {
		complete, err := databaseTaskIsComplete(app, roles)
		if err != nil {
			return changes, err
		}
		for _, taskID := range taskIDs {
			if !complete(taskID) {
				moved = append(moved, taskID)
			}
		}
	}
	movedSet := map[string]bool{}
	for _, taskID := range moved {
		movedSet[taskID] = true
	}

	// The unfinished tasks move a change at a time, each change named by its
	// first task: a retry moves only those still in the sprint, under new names.
	for start := 0; start < len(moved); start += maxGraphMutationOperations {
		chunk := moved[start:min(start+maxGraphMutationOperations, len(moved))]
		err = applyDatabaseSprintMutation(ctx, ygo, actorID, stored.PageID, "sprint-done-"+id("tasks:"+chunk[0]), false,
			func(records databasePageRecords) ([]graphOperation, error) {
				operations := []graphOperation{}
				for _, taskID := range chunk {
					sprints := []string{}
					for _, sprintID := range databaseSprintRelationIDs(records, taskID, stored.TaskSprintPropertyID) {
						if sprintID != current.ID {
							sprints = append(sprints, sprintID)
						}
					}
					if mode == "next" {
						sprints = append(sprints, newCurrentID)
					}
					operations = append(operations, databaseSprintRowUpdate(records, taskID, map[string]graphRowValue{
						stored.TaskSprintPropertyID: databaseSprintValue(graphValueTypeRelation, sprints),
					}))
				}
				return operations, nil
			})
		if err != nil {
			return changes, err
		}
	}
	err = applyDatabaseSprintMutation(ctx, ygo, actorID, stored.SprintsPageID, "sprint-done-"+id("sprints"), false,
		func(records databasePageRecords) ([]graphOperation, error) {
			status := func(value string) graphRowValue {
				return databaseSprintValue(graphValueTypeStatus, databaseSprintStatusPrefix+value)
			}
			operations := []graphOperation{}
			if hasLast {
				operations = append(operations, databaseSprintRowUpdate(records, last.ID, map[string]graphRowValue{
					stored.StatusPropertyID: status(databaseSprintStatusPast),
				}))
			}
			remaining := []string{}
			for _, taskID := range databaseSprintRelationIDs(records, current.ID, stored.SprintTasksPropertyID) {
				if !movedSet[taskID] {
					remaining = append(remaining, taskID)
				}
			}
			operations = append(operations, databaseSprintRowUpdate(records, current.ID, map[string]graphRowValue{
				stored.StatusPropertyID:      status(databaseSprintStatusLast),
				stored.SprintTasksPropertyID: databaseSprintValue(graphValueTypeRelation, remaining),
			}))
			currentValues := map[string]graphRowValue{
				stored.StatusPropertyID: status(databaseSprintStatusCurrent),
				stored.DatesPropertyID:  databaseSprintValue(graphValueTypeDate, databaseSprintDates(newStart, newEnd)),
			}
			if completion.Undated && !automatic {
				currentValues[stored.DatesPropertyID] = databaseSprintValue(graphValueTypeDate, nil)
			}
			tasks := []string{}
			if !createCurrent {
				tasks = databaseSprintRelationIDs(records, newCurrentID, stored.SprintTasksPropertyID)
			}
			// The moved tasks already name the sprint, so its side may hold
			// them: each once.
			if mode == "next" {
				for _, taskID := range moved {
					if !slices.Contains(tasks, taskID) {
						tasks = append(tasks, taskID)
					}
				}
			}
			currentValues[stored.SprintTasksPropertyID] = databaseSprintValue(graphValueTypeRelation, tasks)
			planned := func(rowID string, sequence int, values map[string]graphRowValue) graphOperation {
				values[titlePropertyID] = databaseSprintValue(graphValueTypeTitle, fmt.Sprintf(title, sequence))
				if _, linked := values[stored.SprintTasksPropertyID]; !linked {
					values[stored.SprintTasksPropertyID] = databaseSprintValue(graphValueTypeRelation, []string{})
				}
				return graphOperation{Kind: graphOperationKindUpsertRow, Row: &graphDatabaseRow{
					ID: rowID, DataSourceID: stored.SprintsDataSourceID,
					Position: databaseSprintRowPosition(sequence), Values: values,
				}}
			}
			if createCurrent {
				operations = append(operations, planned(newCurrentID, currentSequence, currentValues))
			} else {
				operations = append(operations, databaseSprintRowUpdate(records, newCurrentID, currentValues))
			}
			if createNext {
				operations = append(operations, planned(newNextID, nextSequence, map[string]graphRowValue{
					stored.StatusPropertyID: status(databaseSprintStatusNext),
				}))
			} else {
				operations = append(operations, databaseSprintRowUpdate(records, newNextID, map[string]graphRowValue{
					stored.StatusPropertyID: status(databaseSprintStatusNext),
				}))
			}
			operations = append(operations, planned(newFutureID, sequence, map[string]graphRowValue{
				stored.StatusPropertyID: status(databaseSprintStatusFuture),
			}))
			return operations, nil
		})
	if err != nil {
		return changes, err
	}
	// The new Future sprint and the ended one, then the next and future
	// ones moved up or made, and the last one now past.
	changes = databaseSprintChangesWire{CreatedSprints: 1, EditedSprints: 1, EditedTasks: len(moved), CreatedSprintID: newFutureID}
	for _, edited := range []bool{!createCurrent, !createNext} {
		if edited {
			changes.EditedSprints++
		} else {
			changes.CreatedSprints++
		}
	}
	if hasLast {
		changes.EditedSprints++
	}
	rollover := int64(0)
	if stored.Automatic {
		rollover = databaseSprintRolloverAt(newEnd, stored.TimeZone)
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_database_sprints}} SET [[sprint_sequence]] = {:sequence}, [[next_rollover_at]] = {:rollover},
			[[revision]] = [[revision]] + 1, [[last_request_id]] = {:requestId}, [[updated_at]] = {:now}
		WHERE [[data_source_id]] = {:dataSourceId} AND [[revision]] = {:revision}
	`).Bind(dbx.Params{
		"sequence": sequence, "rollover": rollover, "requestId": requestID, "now": now.UnixMilli(),
		"dataSourceId": stored.DataSourceID, "revision": stored.Revision,
	}).Execute()
	return changes, err
}

// databaseSprintRunner completes automatic sprints when their current sprint
// has ended.
type databaseSprintRunner struct {
	app  core.App
	ygo  *pocketBaseYgoPersistence
	mu   sync.Mutex
	stop context.CancelFunc
	done chan struct{}
}

func newDatabaseSprintRunner(app core.App, ygo *pocketBaseYgoPersistence) *databaseSprintRunner {
	return &databaseSprintRunner{app: app, ygo: ygo}
}

func (runner *databaseSprintRunner) Start() {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	if runner.stop != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	runner.stop, runner.done = cancel, make(chan struct{})
	go func(done chan struct{}) {
		defer close(done)
		ticker := time.NewTicker(databaseSprintRunnerInterval)
		defer ticker.Stop()
		for {
			runner.runTurn(ctx, time.Now().UTC())
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}(runner.done)
}

func (runner *databaseSprintRunner) Shutdown(ctx context.Context) error {
	runner.mu.Lock()
	stop, done := runner.stop, runner.done
	runner.stop, runner.done = nil, nil
	runner.mu.Unlock()
	if stop == nil {
		return nil
	}
	stop()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// runTurn completes each due automatic sprint once; a failed rollover is
// retried on the next turn with the same request, so it never doubles.
func (runner *databaseSprintRunner) runTurn(ctx context.Context, now time.Time) {
	due := []string{}
	if err := runner.app.DB().NewQuery(`
		SELECT [[data_source_id]] FROM {{leafen_database_sprints}}
		WHERE [[automatic]] = 1 AND [[next_rollover_at]] > 0 AND [[next_rollover_at]] <= {:now}
		ORDER BY [[next_rollover_at]], [[data_source_id]] LIMIT {:limit}
	`).Bind(dbx.Params{"now": now.UnixMilli(), "limit": databaseSprintRunnerBatch}).Column(&due); err != nil {
		return
	}
	for _, dataSourceID := range due {
		if ctx.Err() != nil {
			return
		}
		stored, enabled, err := loadDatabaseSprints(runner.app, dataSourceID)
		if err != nil || !enabled || !stored.Automatic {
			continue
		}
		requestID := "auto-" + strconv.FormatInt(stored.NextRolloverAt, 10)
		if _, err := completeDatabaseSprint(ctx, runner.app, runner.ygo, stored.Owner, stored,
			databaseSprintCompletion{Mode: stored.IncompleteTasks, Automatic: true}, requestID, now); err != nil {
			slog.Warn("database sprint rollover failed", "dataSourceId", dataSourceID, "error", err)
		}
	}
}
