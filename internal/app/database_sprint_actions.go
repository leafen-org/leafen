package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

const databaseSprintStatusPrefix = "sprint-"

var databaseSprintStatusOptions = []struct{ id, color string }{
	{databaseSprintStatusCurrent, "blue"},
	{databaseSprintStatusNext, "purple"},
	{databaseSprintStatusFuture, "gray"},
	{databaseSprintStatusLast, "green"},
	{databaseSprintStatusPast, "default"},
}

// databaseSprintStatusName is a sprint status's name among the names given.
func databaseSprintStatusName(names generatedSprintNames, status string) string {
	switch status {
	case databaseSprintStatusCurrent:
		return names.Current
	case databaseSprintStatusNext:
		return names.Next
	case databaseSprintStatusFuture:
		return names.Future
	case databaseSprintStatusLast:
		return names.Last
	default:
		return names.Past
	}
}

func runDatabaseSprintAction(
	ctx context.Context,
	app core.App,
	ygo *pocketBaseYgoPersistence,
	actorID string,
	authority databaseToolbarConfigAuthority,
	input databaseSprintActionInput,
) (databaseSprintActionResponse, error) {
	return runDatabaseSprintActionWithRooms(ctx, app, ygo, nil, actorID, authority, input)
}

// runDatabaseSprintActionWithRooms runs a sprint action; turning sprints off
// closes the workspace's collaboration rooms before deleting the sprint
// pages, as a permanent deletion does.
func runDatabaseSprintActionWithRooms(
	ctx context.Context,
	app core.App,
	ygo *pocketBaseYgoPersistence,
	rooms workspaceRoomInvalidator,
	actorID string,
	authority databaseToolbarConfigAuthority,
	input databaseSprintActionInput,
) (databaseSprintActionResponse, error) {
	input.RequestID = strings.TrimSpace(input.RequestID)
	if input.RequestID == "" || len(input.RequestID) > 160 {
		return databaseSprintActionResponse{}, databaseToolbarRefused("requestId is a new id of yours, 1–160 characters, so the same action sent again isn't done twice")
	}
	stored, enabled, err := loadDatabaseSprints(app, authority.DataSourceID)
	if err != nil {
		return databaseSprintActionResponse{}, err
	}
	if enabled && stored.LastRequestID == input.RequestID {
		state, err := readDatabaseSprintState(app, actorID, authority.DataSourceID)
		return databaseSprintActionResponse{State: state, Duplicate: true}, err
	}
	var changes *databaseSprintChangesWire
	switch input.Action {
	case "enable":
		if enabled {
			return databaseSprintActionResponse{}, databaseToolbarStateRefused("sprints are on already")
		}
		err = enableDatabaseSprints(ctx, app, ygo, actorID, authority, input)
	case "update", "complete", "disable", "manageView":
		if !enabled {
			return databaseSprintActionResponse{}, databaseToolbarStateRefused("sprints are off: enable them first")
		}
		if stored.Revision != input.ExpectedRevision {
			return databaseSprintActionResponse{}, errDatabaseSprintConflict
		}
		switch input.Action {
		case "manageView":
			err = manageDatabaseSprintsInView(ctx, app, ygo, actorID, stored, input)
		case "update":
			err = updateDatabaseSprintSettings(ctx, app, ygo, actorID, stored, input)
		case "complete":
			mode := stored.IncompleteTasks
			if input.IncompleteTasks != "" {
				mode = input.IncompleteTasks
			}
			var made databaseSprintChangesWire
			made, err = completeDatabaseSprint(ctx, app, ygo, actorID, stored, databaseSprintCompletion{
				Mode: mode, Start: strings.TrimSpace(input.SprintStart), End: strings.TrimSpace(input.SprintEnd), Undated: input.SprintUndated,
			}, input.RequestID, time.Now().UTC())
			changes = &made
		default:
			err = disableDatabaseSprints(ctx, app, ygo, rooms, actorID, stored, input.RequestID)
		}
	default:
		return databaseSprintActionResponse{}, databaseToolbarRefused("action is enable, update, complete, disable or manageView, not %q", input.Action)
	}
	if err != nil {
		return databaseSprintActionResponse{}, err
	}
	state, err := readDatabaseSprintState(app, actorID, authority.DataSourceID)
	return databaseSprintActionResponse{State: state, Changes: changes}, err
}

// applyDatabaseSprintMutation applies one content-graph mutation the sprint
// owner builds; a retried request replays its receipt instead.
func applyDatabaseSprintMutation(
	ctx context.Context,
	ygo *pocketBaseYgoPersistence,
	actorID, pageID, mutationID string,
	appendBlocks bool,
	build func(records databasePageRecords) ([]graphOperation, error),
) error {
	return applyDatabaseSprintMutationWith(ctx, ygo, actorID, pageID, mutationID, appendBlocks, databaseSprintMutationExtras{}, build)
}

// databaseSprintMutationExtras are what turning sprints on adds to its
// mutation, before the database is known as a Sprints database: the ID
// property whose numbers it writes, and the sprints it makes, with their icon.
type databaseSprintMutationExtras struct {
	numberPropertyID string
	newSprintIDs     []string
}

func applyDatabaseSprintMutationWith(
	ctx context.Context,
	ygo *pocketBaseYgoPersistence,
	actorID, pageID, mutationID string,
	appendBlocks bool,
	extras databaseSprintMutationExtras,
	build func(records databasePageRecords) ([]graphOperation, error),
) error {
	digest := sha256.Sum256([]byte("database-sprint\x00" + pageID + "\x00" + mutationID))
	_, err := ygo.applyContentGraphMutationContext(ctx, actorID, pageID, graphMutationInput{MutationID: mutationID},
		contentGraphMutationHooks{
			StructuralAppend: appendBlocks, StepsKeepGraph: true, // its step: the icons of new sprints, rows it names
			ResolveRequestHash: func(core.App, pageAuthorization, *crdt.Doc) (string, error) {
				return hex.EncodeToString(digest[:]), nil
			},
			Prepare: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				operations, err := build(databasePageRecords{app: txApp, pageID: pageID})
				preparation := contentGraphMutationPreparation{Operations: operations}
				if extras.numberPropertyID != "" {
					preparation.RuntimeWritablePropertyIDs = map[string]struct{}{extras.numberPropertyID: {}}
				}
				return preparation, err
			},
			AfterApply: func(txApp core.App, _ pageAuthorization, _ *crdt.Doc) error {
				for _, sprintID := range extras.newSprintIDs {
					if _, err := setRowPageRecordIcon(txApp, sprintID, "", pageID, "", databaseSprintIcon, nativeIconType); err != nil {
						return err
					}
				}
				return nil
			},
		})
	return err
}

func databaseSprintJSON(value any) json.RawMessage {
	encoded, _ := json.Marshal(value)
	return encoded
}

func databaseSprintValue(kind string, value any) graphRowValue {
	return graphRowValue{Type: kind, Value: databaseSprintJSON(value)}
}

func databaseSprintDates(start, end time.Time) map[string]any {
	return map[string]any{"start": start.Format(time.DateOnly), "end": end.Format(time.DateOnly)}
}

func databaseSprintRowPosition(sequence int) string {
	return fmt.Sprintf("s%010d", sequence)
}

// databaseSprintViewFilters are the filters of the task views that follow
// the sprints, as Notion's Sprint board's (seen 2026-11-04): pills for the
// current sprint — fixed, dimmed — and the people tasks are assigned to,
// for the current sprint; a rule group of the current, next or last sprint
// or none, and pills for unfinished tasks and their people, for sprint
// planning; pills for no sprint, unfinished tasks and their people, for the
// backlog. They name the sprints by place, so they follow each completed
// sprint as they are; the people pills wait for a value.
func databaseSprintViewFilters(stored storedDatabaseSprints, roles databaseTaskRoles, unfinished map[string]any) map[string]map[string]any {
	property := stored.TaskSprintPropertyID
	relative := func(places ...string) []any {
		values := []any{}
		for _, place := range places {
			values = append(values, map[string]any{"kind": databaseQueryRelativeSprintKind, "sprint": place})
		}
		return values
	}
	pills := func(first ...map[string]any) []any {
		filters := []any{}
		for _, rule := range first {
			if rule != nil {
				filters = append(filters, rule)
			}
		}
		if roles.AssigneePropertyID != "" {
			filters = append(filters, map[string]any{"propertyId": roles.AssigneePropertyID, "operator": "contains"})
		}
		return filters
	}
	noSprint := map[string]any{"propertyId": property, "operator": "is_empty"}
	return map[string]map[string]any{
		"current": {"quickFilters": pills(map[string]any{
			"propertyId": property, "operator": "contains", "value": relative(databaseSprintStatusCurrent), "locked": true,
		})},
		"planning": {"quickFilters": pills(unfinished), "filterGroup": map[string]any{
			"conjunction": "or", "groups": []any{}, "filters": []any{
				map[string]any{"propertyId": property, "operator": "contains",
					"value": relative(databaseSprintStatusCurrent, databaseSprintStatusNext, databaseSprintStatusLast)},
				noSprint,
			},
		}},
		"backlog": {"quickFilters": pills(noSprint, unfinished)},
	}
}

// databaseSprintUnfinished is a task's status short of its last group —
// Notion's "상태: 선택 값과 동일하지 않음 Complete" — or an unchecked box.
func databaseSprintUnfinished(app core.App, dataSourceID, statusPropertyID string) (map[string]any, error) {
	var status struct {
		Type   string `db:"type"`
		Config string `db:"config"`
	}
	if err := app.DB().NewQuery(`
		SELECT [[type]], [[config]] FROM {{leafen_database_properties}}
		WHERE [[id]] = {:id} AND [[data_source_id]] = {:dataSourceId}
	`).Bind(dbx.Params{"id": statusPropertyID, "dataSourceId": dataSourceID}).One(&status); err != nil {
		return nil, err
	}
	if status.Type == graphValueTypeCheckbox {
		return map[string]any{"propertyId": statusPropertyID, "operator": "equals", "value": false}, nil
	}
	config := databaseQueryParsedPropertyConfig(storedDatabaseQueryProperty{Type: status.Type, Config: status.Config})
	if len(config.StatusGroups) == 0 {
		return nil, nil
	}
	done := config.StatusGroups[len(config.StatusGroups)-1].ID
	return map[string]any{"propertyId": statusPropertyID, "operator": "does_not_equal", "value": []any{done}}, nil
}

// databaseSprintBoardViewConfigs are the Sprint board's three views as
// Notion makes them (seen 2026-11-04): the current sprint's board by status
// showing names, assignees and sub-tasks, its columns without counts;
// sprint planning's table by sprint, empty sprints shown, showing all but
// the sub-item properties; the backlog's table of name,
// status, assignee and sprint. Each has Notion's icon, lists those first
// and the rest by name, and starts with its filter bar closed.
func databaseSprintBoardViewConfigs(app core.App, stored storedDatabaseSprints, roles databaseTaskRoles) (map[string]json.RawMessage, error) {
	unfinished, err := databaseSprintUnfinished(app, stored.DataSourceID, roles.StatusPropertyID)
	if err != nil {
		return nil, err
	}
	filters := databaseSprintViewFilters(stored, roles, unfinished)
	properties := []struct {
		ID     string `db:"id"`
		Name   string `db:"name"`
		Type   string `db:"type"`
		Config string `db:"config"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[name]], [[type]], [[config]] FROM {{leafen_database_properties}}
		WHERE [[data_source_id]] = {:dataSourceId} AND ` + databasePropertyNotTrashedSQL("[[id]]") + `
		ORDER BY [[position]], [[id]] LIMIT {:limit}
	`).Bind(dbx.Params{"dataSourceId": stored.DataSourceID, "limit": maxDatabaseQueryProperties}).All(&properties); err != nil {
		return nil, err
	}
	pairs, err := loadDatabaseSubitemPairs(app, []string{stored.DataSourceID})
	if err != nil {
		return nil, err
	}
	pair := pairs[stored.DataSourceID]
	title := ""
	byName := make([]string, 0, len(properties))
	sort.SliceStable(properties, func(left, right int) bool { return properties[left].Name < properties[right].Name })
	for _, property := range properties {
		if property.Type == graphValueTypeTitle {
			title = property.ID
		}
		byName = append(byName, property.ID)
	}
	ids := func(candidates ...string) []string {
		listed := []string{}
		for _, id := range candidates {
			if id != "" && !slices.Contains(listed, id) {
				listed = append(listed, id)
			}
		}
		return listed
	}
	order := func(first ...string) []string {
		listed := ids(first...)
		for _, id := range byName {
			if !slices.Contains(listed, id) {
				listed = append(listed, id)
			}
		}
		return listed
	}
	except := func(shown []string) []string {
		hidden := []string{}
		for _, id := range byName {
			if !slices.Contains(shown, id) {
				hidden = append(hidden, id)
			}
		}
		return hidden
	}
	return map[string]json.RawMessage{
		// Its board shows no counts, its later groups' neither, as Notion's
		// board, whose count is one for the view (seen 2026-11-04, 2026-11-12).
		"current": databaseSprintJSON(map[string]any{
			"icon": "playback-play-button:gray", "groupBy": roles.StatusPropertyID, "quickFilters": filters["current"]["quickFilters"],
			"filterBarHidden": true, "groupCountsHidden": true,
			"propertyOrder":     order(title, roles.AssigneePropertyID, pair.Children, roles.StatusPropertyID, roles.DueDatePropertyID, stored.TaskSprintPropertyID),
			"visibleProperties": ids(title, roles.AssigneePropertyID, pair.Children),
		}),
		"planning": databaseSprintJSON(map[string]any{
			"icon": "meeting:gray", "groupBy": stored.TaskSprintPropertyID, "filterBarHidden": true, "hideEmptyGroups": false,
			"quickFilters": filters["planning"]["quickFilters"], "filterGroup": filters["planning"]["filterGroup"],
			"propertyOrder":    order(title, roles.StatusPropertyID, roles.AssigneePropertyID),
			"hiddenProperties": ids(pair.Parent, pair.Children),
		}),
		"backlog": databaseSprintJSON(map[string]any{
			"icon": "inbox:gray", "quickFilters": filters["backlog"]["quickFilters"], "filterBarHidden": true,
			"propertyOrder": order(title, roles.StatusPropertyID, roles.AssigneePropertyID, stored.TaskSprintPropertyID),
			// A table lists what it hides, so a property added later shows, as Notion's tables do (seen 2026-11-04).
			"hiddenProperties": except(ids(title, roles.StatusPropertyID, roles.AssigneePropertyID, stored.TaskSprintPropertyID)),
		}),
		// A sprint page's second view: its tasks in a table by assignee,
		// showing their names and status (Notion's "담당자별", seen 2026-11-05).
		"byAssignee": databaseSprintJSON(map[string]any{
			"groupBy": roles.AssigneePropertyID, "filterBarHidden": true,
			"propertyOrder":    order(title, roles.StatusPropertyID),
			"hiddenProperties": except(ids(title, roles.StatusPropertyID)),
		}),
	}, nil
}

func enableDatabaseSprints(
	ctx context.Context,
	app core.App,
	ygo *pocketBaseYgoPersistence,
	actorID string,
	authority databaseToolbarConfigAuthority,
	input databaseSprintActionInput,
) error {
	if input.Settings == nil {
		return errDatabaseSprintSettingsMissing
	}
	settings, err := normalizeDatabaseSprintSettings(*input.Settings)
	if err != nil {
		return err
	}
	roles, err := loadDatabaseTaskRoles(app, authority.DataSourceID)
	if err != nil {
		return err
	}
	if !roles.Enabled || roles.StatusPropertyID == "" {
		return databaseToolbarRefused("the database isn't a task database with a status: turn its task settings on first")
	}
	// What turning sprints on makes is named in the language of the person
	// turning them on.
	names := generatedNamesFor(ctx, app, actorID).Sprints
	statusType := ""
	if err := app.DB().NewQuery(`
		SELECT [[type]] FROM {{leafen_database_properties}}
		WHERE [[id]] = {:id} AND [[data_source_id]] = {:dataSourceId}
	`).Bind(dbx.Params{"id": roles.StatusPropertyID, "dataSourceId": authority.DataSourceID}).Row(&statusType); err != nil ||
		(statusType != graphValueTypeStatus && statusType != graphValueTypeCheckbox) {
		return databaseToolbarRefused("the task settings' status property is gone or isn't a status or checkbox")
	}
	taskContainerID := ""
	if err := app.DB().NewQuery(`
		SELECT [[container_id]] FROM {{leafen_database_container_sources}}
		WHERE [[owner]] = {:owner} AND [[workspace_id]] = {:workspaceId}
			AND [[data_source_id]] = {:dataSourceId} AND [[source_container_id]] = '' LIMIT 1
	`).Bind(dbx.Params{
		"owner": authority.OwnerID, "workspaceId": authority.WorkspaceID, "dataSourceId": authority.DataSourceID,
	}).Row(&taskContainerID); err != nil {
		return databaseToolbarRefused("the data source has no database of its own to hold the sprints")
	}
	id := func(name string) string {
		return databaseSprintDeterministicID("sprints", authority.DataSourceID, input.RequestID, name)
	}
	// Its data source and views are named as every database's are (database_entity_ids.go),
	// its pages and sprints as every page is (page_ids.go).
	entity := func(name string) string {
		return derivedDatabaseEntityID("sprints", authority.DataSourceID, input.RequestID, name)
	}
	page := func(name string) string {
		return derivedPageID("sprints", authority.DataSourceID, input.RequestID, name)
	}
	stored := storedDatabaseSprints{
		DataSourceID: authority.DataSourceID, Owner: authority.OwnerID, WorkspaceID: authority.WorkspaceID,
		PageID: authority.PageID, SprintsPageID: page("page"), BoardPageID: page("board-page"),
		SprintsDataSourceID: entity("source"), TaskSprintPropertyID: id("task-sprint"), SprintTasksPropertyID: id("sprint-tasks"),
		StatusPropertyID: id("sprint-status"), DatesPropertyID: id("sprint-dates"),
		DurationWeeks: settings.DurationWeeks, StartWeekday: settings.StartWeekday,
		IncompleteTasks: settings.IncompleteTasks, Automatic: settings.Automatic, TimeZone: settings.TimeZone,
		SprintSequence: 3, Revision: 1, LastRequestID: input.RequestID,
	}
	views := databaseSprintViews{
		Current: entity("view-current"), Planning: entity("view-planning"), Backlog: entity("view-backlog"),
		All: entity("view-all"), Timeline: entity("view-timeline"),
	}
	stored.ViewsJSON = string(databaseSprintJSON(views))
	properties := databaseSprintPropertyIDs{
		Name: id("sprint-name"), Dates: stored.DatesPropertyID, Number: id("sprint-number"), Tasks: stored.SprintTasksPropertyID,
		Total: id("sprint-total"), Completed: id("sprint-completed"), Status: stored.StatusPropertyID,
	}
	totalRollup, completedRollup, err := databaseSprintRollupConfigs(app, authority.DataSourceID, roles.StatusPropertyID, properties.Tasks)
	if err != nil {
		return err
	}
	currentID, nextID, futureID := page("sprint-1"), page("sprint-2"), page("sprint-3")
	today := databaseSprintDay(time.Now().UTC(), settings.TimeZone)
	currentStart := databaseSprintAlignedStart(today, settings.StartWeekday)
	currentEnd := databaseSprintEnd(currentStart, settings.DurationWeeks)
	if settings.Automatic {
		stored.NextRolloverAt = databaseSprintRolloverAt(currentEnd, settings.TimeZone)
	}

	// The Sprint board and Sprints are full-page databases right after the
	// task database's block, as Notion's (seen 2026-11-03): beside a
	// full-page one, in the page an inline one is in after the top block
	// holding it; the board first, with its runner.
	taskPage, err := app.FindRecordById(pagesCollection, authority.PageID)
	if err != nil {
		return err
	}
	parentID, teamspaceID := taskPage.Id, ""
	after := &pageBodyPlacement{Type: "after", AfterKind: pageBodyItemBlock, AfterID: taskContainerID}
	if isFullPageDatabase(taskPage) {
		parentID = taskPage.GetString("parent_id")
		after = &pageBodyPlacement{Type: "after", AfterKind: pageBodyItemPage, AfterID: taskPage.Id}
		if parentID == "" {
			teamspaceID, after = taskPage.GetString("teamspace_id"), nil
		}
	}
	// Up to the top block however deep; each is climbed once, and a chain
	// that comes back on itself has no top: at the end.
	for climbed := map[string]bool{}; after != nil; {
		if climbed[after.AfterID] {
			after = nil
			break
		}
		climbed[after.AfterID] = true
		var holder struct {
			ParentID string `db:"parent_id"`
		}
		if app.DB().NewQuery(`SELECT [[parent_id]] FROM {{leafen_blocks}} WHERE [[id]] = {:id} AND [[page_id]] = {:pageId}`).
			Bind(dbx.Params{"id": after.AfterID, "pageId": parentID}).One(&holder) != nil {
			after = nil // Its block is gone: at the end.
		} else if holder.ParentID == "" {
			break
		} else {
			after.AfterID = holder.ParentID
		}
	}
	createPage := func(pageID, title, request string) error {
		_, status, err := admitPageCreation(app, actorID, normalizePageCreationAdmissionInput(pageCreationAdmissionInput{
			RequestID: request + "-" + id("request"), WorkspaceID: authority.WorkspaceID, TeamspaceID: teamspaceID,
			ParentID: parentID, Title: title, PreparedPageID: pageID, Kind: pageKindDatabase, BodyPlacement: after,
		}))
		if err == nil && status != pageCreationApplied {
			return errDatabaseSprintConflict
		}
		if after != nil {
			after = &pageBodyPlacement{Type: "after", AfterKind: pageBodyItemPage, AfterID: pageID}
		}
		return err
	}
	if err := createPage(stored.BoardPageID, names.Board, "sprint-board-page"); err != nil {
		return err
	}
	board, err := authorizePage(app, actorID, stored.BoardPageID, permissionWorkspaceWrite)
	if err != nil {
		return err
	}
	if _, err := commitPageMetadataRecord(app, board, actorID, func(page *core.Record, _ int64) {
		page.Set("icon", databaseSprintIcon)
		page.Set("icon_type", nativeIconType)
	}); err != nil {
		return err
	}
	if err := createPage(stored.SprintsPageID, names.Page, "sprints-page"); err != nil {
		return err
	}
	sprintsPage, err := authorizePage(app, actorID, stored.SprintsPageID, permissionWorkspaceWrite)
	if err != nil {
		return err
	}
	if _, err := commitPageMetadataRecord(app, sprintsPage, actorID, func(page *core.Record, _ int64) {
		page.Set("icon", databaseSprintDatabaseIcon)
		page.Set("icon_type", nativeIconType)
	}); err != nil {
		return err
	}
	databaseIcon, databaseIconType := databaseSprintDatabaseIcon, graphDataSourceIconBuiltIn
	statusOptions := []any{}
	for _, option := range databaseSprintStatusOptions {
		statusOptions = append(statusOptions, map[string]any{
			"id": databaseSprintStatusPrefix + option.id, "name": databaseSprintStatusName(names, option.id), "color": option.color,
		})
	}
	statusConfig := map[string]any{"options": statusOptions, "statusGroups": []any{
		map[string]any{"id": "to_do", "name": names.ToDo, "color": "gray", "optionIds": []string{"sprint-next", "sprint-future"}},
		map[string]any{"id": "in_progress", "name": names.InProgress, "color": "blue", "optionIds": []string{"sprint-current"}},
		map[string]any{"id": "complete", "name": names.Complete, "color": "green", "optionIds": []string{"sprint-last", "sprint-past"}},
	}}
	// The current sprint, dated, then the next and future ones, undated
	// until each becomes current, as Notion's (seen 2026-11-03). They are
	// numbered here, as those made together would otherwise number in no order.
	sprintRow := func(rowID string, sequence int, status string, dates map[string]any) graphOperation {
		values := map[string]graphRowValue{
			properties.Name:   databaseSprintValue(graphValueTypeTitle, fmt.Sprintf(names.Title, sequence)),
			properties.Number: databaseSprintValue(graphValueTypeUniqueID, sequence),
			properties.Status: databaseSprintValue(graphValueTypeStatus, databaseSprintStatusPrefix+status),
			properties.Tasks:  databaseSprintValue(graphValueTypeRelation, []string{}),
		}
		if dates != nil {
			values[stored.DatesPropertyID] = databaseSprintValue(graphValueTypeDate, dates)
		}
		return graphOperation{Kind: graphOperationKindUpsertRow, Row: &graphDatabaseRow{
			ID: rowID, DataSourceID: stored.SprintsDataSourceID, Position: databaseSprintRowPosition(sequence), Values: values,
		}}
	}
	err = applyDatabaseSprintMutationWith(ctx, ygo, actorID, stored.SprintsPageID, "sprint-create-"+id("sprints-mutation"), true,
		databaseSprintMutationExtras{numberPropertyID: properties.Number, newSprintIDs: []string{currentID, nextID, futureID}},
		func(databasePageRecords) ([]graphOperation, error) {
			property := func(propertyID, kind, name, position string, config any) graphOperation {
				return graphOperation{Kind: graphOperationKindUpsertProperty, Property: &graphProperty{
					ID: propertyID, DataSourceID: stored.SprintsDataSourceID, Type: kind, Name: name,
					Position: position, Config: databaseSprintJSON(config),
				}}
			}
			return []graphOperation{
				{Kind: graphOperationKindUpsertBlock, Block: &graphBlock{
					ID: id("sprints-block"), Position: "p0000000000", Type: graphBlockTypeInlineDatabase,
					Content: databaseSprintJSON(map[string]any{"dataSourceId": stored.SprintsDataSourceID, "viewId": views.All}),
				}},
				{Kind: graphOperationKindUpsertDataSource, DataSource: &graphDataSource{
					ID: stored.SprintsDataSourceID, Title: names.Page, Icon: &databaseIcon, IconType: &databaseIconType,
				}},
				property(properties.Name, graphValueTypeTitle, names.Name, databaseSprintNamePosition, map[string]any{}),
				property(properties.Dates, graphValueTypeDate, names.Dates, databaseSprintDatesPosition, map[string]any{}),
				// Its number and task count wait behind "속성 2개 더 보기" on its page.
				property(properties.Number, graphValueTypeUniqueID, names.SprintID, databaseSprintNumberPosition, map[string]any{"hideInPagePanel": true}),
				property(properties.Tasks, graphValueTypeRelation, names.Tasks, databaseSprintTasksPosition, map[string]any{
					"relationDatabaseId": authority.DataSourceID, "relatedPropertyId": stored.TaskSprintPropertyID,
				}),
				property(properties.Total, graphValueTypeRollup, names.TotalTasks, databaseSprintTotalPosition, totalRollup),
				property(properties.Completed, graphValueTypeRollup, names.CompletedTasks, databaseSprintCompletedPosition, completedRollup),
				property(properties.Status, graphValueTypeStatus, names.Status, databaseSprintStatusPosition, statusConfig),
				{Kind: graphOperationKindUpsertView, View: &graphDatabaseView{
					ID: views.All, DataSourceID: stored.SprintsDataSourceID, Type: graphViewTypeTable,
					Name: names.AllSprints, Position: "v0", Config: databaseSprintJSON(databaseSprintAllViewConfig(properties)),
				}},
				{Kind: graphOperationKindUpsertView, View: &graphDatabaseView{
					ID: views.Timeline, DataSourceID: stored.SprintsDataSourceID, Type: graphViewTypeTimeline,
					Name: names.Timeline, Position: "v1", Config: databaseSprintJSON(map[string]any{
						"timelineBy": stored.DatesPropertyID, "hiddenProperties": databaseSprintTimelineHidden(properties),
					}),
				}},
				sprintRow(currentID, 1, databaseSprintStatusCurrent, databaseSprintDates(currentStart, currentEnd)),
				sprintRow(nextID, 2, databaseSprintStatusNext, nil),
				sprintRow(futureID, 3, databaseSprintStatusFuture, nil),
			}, nil
		})
	if err != nil {
		return err
	}
	err = applyDatabaseSprintMutation(ctx, ygo, actorID, authority.PageID, "sprint-link-"+id("tasks-mutation"), false,
		func(databasePageRecords) ([]graphOperation, error) {
			return []graphOperation{{Kind: graphOperationKindUpsertProperty, Property: &graphProperty{
				ID: stored.TaskSprintPropertyID, DataSourceID: authority.DataSourceID, Type: graphValueTypeRelation,
				// A task is in one sprint at a time: its relation takes one page, as
				// Notion's (seen 2026-11-07: 제한 1개 페이지, and linking a task in
				// another sprint asks to take it out of that one).
				Name: names.Sprint, Position: "zsprint", Config: databaseSprintJSON(map[string]any{
					"relationDatabaseId": stored.SprintsDataSourceID, "relatedPropertyId": stored.SprintTasksPropertyID,
					"icon": databaseSprintIcon, "limit": 1,
				}),
			}}}, nil
		})
	if err != nil {
		return err
	}
	configs, err := databaseSprintBoardViewConfigs(app, stored, roles)
	if err != nil {
		return err
	}
	boardBlockID := id("board-block")
	err = applyDatabaseSprintMutation(ctx, ygo, actorID, stored.BoardPageID, "sprint-board-"+id("board-mutation"), true,
		func(databasePageRecords) ([]graphOperation, error) {
			// The board's page names it, the task database's title hidden.
			view := func(viewID, kind, name, position, config string) graphOperation {
				fields := map[string]any{}
				_ = json.Unmarshal(configs[config], &fields)
				fields["showDataSourceTitle"] = false
				return graphOperation{Kind: graphOperationKindUpsertView, View: &graphDatabaseView{
					ID: viewID, DataSourceID: authority.DataSourceID, ContainerID: boardBlockID, Type: kind,
					Name: name, Position: position, Config: databaseSprintJSON(fields),
				}}
			}
			return []graphOperation{
				{Kind: graphOperationKindUpsertBlock, Block: &graphBlock{
					ID: boardBlockID, Position: "p0000000000", Type: graphBlockTypeInlineDatabase,
					Content: databaseSprintJSON(map[string]any{
						"dataSourceId": authority.DataSourceID, "viewId": views.Current,
						"linkedDatabaseSource": true, "sourceContainerId": taskContainerID,
					}),
				}},
				view(views.Current, graphViewTypeBoard, names.CurrentSprint, "v1", "current"),
				view(views.Planning, graphViewTypeTable, names.Planning, "v2", "planning"),
				view(views.Backlog, graphViewTypeTable, names.Backlog, "v3", "backlog"),
			}, nil
		})
	if err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	_, err = app.DB().NewQuery(`
		INSERT INTO {{leafen_database_sprints}}
			([[data_source_id]], [[owner]], [[workspace_id]], [[page_id]], [[sprints_page_id]], [[board_page_id]],
			 [[sprints_data_source_id]], [[task_sprint_property_id]], [[sprint_tasks_property_id]],
			 [[sprint_status_property_id]], [[sprint_dates_property_id]], [[views_json]], [[duration_weeks]],
			 [[start_weekday]], [[incomplete_tasks]], [[automatic]], [[time_zone]], [[sprint_sequence]],
			 [[next_rollover_at]], [[revision]], [[last_request_id]], [[created_at]], [[updated_at]])
		VALUES ({:dataSourceId}, {:owner}, {:workspaceId}, {:pageId}, {:sprintsPageId}, {:boardPageId},
			{:sprintsDataSourceId}, {:taskSprint}, {:sprintTasks}, {:status}, {:dates}, {:views}, {:weeks},
			{:weekday}, {:incomplete}, {:automatic}, {:zone}, {:sequence}, {:rollover}, 1, {:requestId}, {:now}, {:now})
		ON CONFLICT ([[data_source_id]]) DO NOTHING
	`).Bind(dbx.Params{
		"dataSourceId": stored.DataSourceID, "owner": stored.Owner, "workspaceId": stored.WorkspaceID,
		"pageId": stored.PageID, "sprintsPageId": stored.SprintsPageID, "boardPageId": stored.BoardPageID,
		"sprintsDataSourceId": stored.SprintsDataSourceID,
		"taskSprint":          stored.TaskSprintPropertyID, "sprintTasks": stored.SprintTasksPropertyID,
		"status": stored.StatusPropertyID, "dates": stored.DatesPropertyID, "views": stored.ViewsJSON,
		"weeks": stored.DurationWeeks, "weekday": stored.StartWeekday, "incomplete": stored.IncompleteTasks,
		"automatic": stored.Automatic, "zone": stored.TimeZone, "sequence": stored.SprintSequence,
		"rollover": stored.NextRolloverAt, "requestId": input.RequestID, "now": now,
	}).Execute()
	if err != nil {
		return err
	}
	return seedDatabaseSprintPageBodies(ctx, app, stored, actorID, []string{currentID, nextID, futureID})
}

// updateDatabaseSprintSettings changes the cadence for the sprints that
// follow.
func updateDatabaseSprintSettings(
	ctx context.Context,
	app core.App,
	ygo *pocketBaseYgoPersistence,
	actorID string,
	stored storedDatabaseSprints,
	input databaseSprintActionInput,
) error {
	if input.Settings == nil {
		return errDatabaseSprintSettingsMissing
	}
	settings, err := normalizeDatabaseSprintSettings(*input.Settings)
	if err != nil {
		return err
	}
	summaries, err := loadDatabaseSprintSummaries(app, stored)
	if err != nil {
		return err
	}
	// The sprints keep their dates, as Notion's do (seen 2026-11-03): a new
	// length or start day shapes the sprints that follow. An automatic
	// rollover follows the current sprint's end.
	rollover := int64(0)
	for _, summary := range summaries {
		if summary.Status != databaseSprintStatusCurrent {
			continue
		}
		if end, err := time.Parse(time.DateOnly, summary.End); err == nil && settings.Automatic {
			rollover = databaseSprintRolloverAt(end, settings.TimeZone)
		}
		break
	}
	result, err := app.DB().NewQuery(`
		UPDATE {{leafen_database_sprints}} SET [[duration_weeks]] = {:weeks}, [[start_weekday]] = {:weekday},
			[[incomplete_tasks]] = {:incomplete}, [[automatic]] = {:automatic}, [[time_zone]] = {:zone},
			[[next_rollover_at]] = {:rollover}, [[revision]] = [[revision]] + 1, [[last_request_id]] = {:requestId},
			[[updated_at]] = {:now}
		WHERE [[data_source_id]] = {:dataSourceId} AND [[revision]] = {:revision}
	`).Bind(dbx.Params{
		"weeks": settings.DurationWeeks, "weekday": settings.StartWeekday, "incomplete": settings.IncompleteTasks,
		"automatic": settings.Automatic, "zone": settings.TimeZone, "rollover": rollover, "requestId": input.RequestID,
		"now": time.Now().UnixMilli(), "dataSourceId": stored.DataSourceID, "revision": stored.Revision,
	}).Execute()
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return errDatabaseSprintConflict
	}
	return nil
}

// disableDatabaseSprints moves the Sprint board and Sprints pages to the
// trash, where they can still be restored, and removes the Sprint relation
// from the tasks; turning sprints on again starts afresh.
