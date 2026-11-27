package app

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// My tasks gathers the tasks assigned to the person asking from every task
// database in the workspace they can read: the rows whose assignee property
// names them, with the task's status, whether it is done (its status sits in
// the Complete group, or its checkbox status is checked) and its due date.
// Archived rows and databases in the trash, or on a page there or under
// one, stay out. The My tasks page asks for its sources only
// (?scope=sources, database_my_task_sources.go), there
// everyone's tasks with theirs first, as Notion's page filters by assignee and
// by a task's status group; Home's My tasks the person's own there
// (?assignee=me). Until the person picks a source there is none, and so no
// task, as Notion's My tasks shows "나에게 할당된 모든 작업이 여기에 표시됩니다."
// and its Home widget the card that asks for sources (seen 2026-11-24, two
// tabs, with tasks assigned to the person in a task database). ?limit= and
// ?cursor= read them all a window at a time (database_my_tasks_paged.go).

const (
	maxMyTaskDatabases      = 200
	maxMyTasksPerDatabase   = 200
	maxMyTasks              = 500
	databaseMyTasksNotFound = "LEAFEN_DATABASE_MY_TASKS_NOT_FOUND"
)

type storedMyTaskDatabase struct {
	DataSourceID string `db:"data_source_id"`
	PageID       string `db:"page_id"`
	TasksJSON    string `db:"tasks_json"`
	Title        string `db:"title"`
	Icon         string `db:"icon"`
	IconType     string `db:"icon_type"`
	// Created is when its database was made, which orders the sources'
	// options in My tasks' status (myTaskStatus.wire), oldest first.
	Created int64 `db:"created"`
}

func registerDatabaseMyTasksRoute(event *core.ServeEvent) {
	event.Router.Route(databaseMyTasksMethod, apiRoot+databaseMyTasksPath, func(event *core.RequestEvent) error {
		user, ok, err := databaseToolbarConfigActor(event)
		if !ok {
			return err
		}
		workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
		authorization, err := authorizeWorkspaceReader(event.App, user.Id, workspaceID)
		if isPermissionDenied(err) {
			return apiError(event, http.StatusNotFound, databaseMyTasksNotFound, "Workspace not found.")
		}
		if err != nil {
			return writeDatabaseToolbarConfigError(event, err)
		}
		ask, err := myTasksAsked(event)
		if err != nil {
			return writeDatabaseToolbarConfigError(event, err)
		}
		only := []string(nil)
		if ask.sources {
			if only, err = readMyTaskSourceIDs(event.App, user.Id, authorization.WorkspaceID); err != nil {
				return writeDatabaseToolbarConfigError(event, err)
			}
			if only == nil {
				only = []string{}
			}
		}
		// Picked sources give everyone's tasks, as the My tasks page lists
		// them (loadMyTaskSource reads the same); ?assignee=me keeps the
		// person's own.
		everyone := only != nil && !ask.own
		if ask.windowed {
			response, err := readDatabaseMyTasksWindow(event.Request.Context(), event.App, user.Id, authorization, ask.limit, ask.after, only, everyone)
			if err != nil {
				return writeDatabaseToolbarConfigError(event, err)
			}
			return event.JSON(http.StatusOK, response)
		}
		response, err := readDatabaseMyTasks(event.Request.Context(), event.App, user.Id, authorization, only, everyone)
		if err != nil {
			return writeDatabaseToolbarConfigError(event, err)
		}
		return event.JSON(http.StatusOK, response)
	})
}

// readDatabaseMyTasks reads the person's tasks in every task database they
// can read, or in those in only when it isn't nil — everyone's with everyone.
func readDatabaseMyTasks(ctx context.Context, app core.App, principalID string, workspace workspaceAuthorization, only []string, everyone bool,
) (databaseMyTasksResponse, error) {
	response := databaseMyTasksResponse{Tasks: []databaseMyTaskWire{}}
	if only != nil && len(only) == 0 {
		return response, nil
	}
	databases, err := readMyTaskDatabases(app, workspace)
	if err != nil {
		return response, err
	}
	if only != nil {
		picked := map[string]bool{}
		for _, id := range only {
			picked[id] = true
		}
		kept := databases[:0]
		for _, database := range databases {
			if picked[database.DataSourceID] {
				kept = append(kept, database)
			}
		}
		databases = kept
	}
	if len(databases) > maxMyTaskDatabases {
		databases, response.Truncated = databases[:maxMyTaskDatabases], true
	}
	whose := myTasksOwn
	if everyone {
		whose = myTasksEveryoneTheirsFirst
	}
	reading := newMyTaskReading(ctx, app, principalID, workspace, whose)
	for _, database := range databases {
		tasks, truncated, _, err := reading.tasks(database, myTasksCursor{}, maxMyTasksPerDatabase)
		if err != nil {
			return response, err
		}
		response.Truncated = response.Truncated || truncated
		response.Tasks = append(response.Tasks, tasks...)
	}
	sort.SliceStable(response.Tasks, func(left, right int) bool {
		a, b := response.Tasks[left], response.Tasks[right]
		if a.Done != b.Done {
			return !a.Done
		}
		if (a.DueStart == "") != (b.DueStart == "") {
			return a.DueStart != ""
		}
		if a.DueStart != b.DueStart {
			return a.DueStart < b.DueStart
		}
		return strings.ToLower(a.Title) < strings.ToLower(b.Title)
	})
	if len(response.Tasks) > maxMyTasks {
		response.Tasks, response.Truncated = response.Tasks[:maxMyTasks], true
	}
	return response, nil
}

// myTaskDatabaseListed tells whether My tasks lists a task database's tasks
// and offers it as a source: the person may read its page, and the page is
// out of the trash with every page above it — a database under a trashed
// page went to the trash with it (loadPageTrashEffects).
func myTaskDatabaseListed(app core.App, principalID, pageID string) (bool, error) {
	_, err := authorizePage(app, principalID, pageID, permissionWorkspaceRead)
	if isPermissionDenied(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	trashed, err := pageInTrashPath(app, pageID)
	return !trashed, err
}

// myTaskDatabaseWritable tells whether a person may change a task
// database's tasks: make one in it, check one off, move one away.
func myTaskDatabaseWritable(app core.App, principalID, pageID string) (bool, error) {
	_, err := authorizePage(app, principalID, pageID, permissionWorkspaceWrite)
	if err != nil && !isPermissionDenied(err) {
		return false, err
	}
	return err == nil, nil
}

// readMyTaskDatabases reads the workspace's task databases, in the trash or
// not, without asking who may read them.
func readMyTaskDatabases(app core.App, workspace workspaceAuthorization) ([]storedMyTaskDatabase, error) {
	databases := []storedMyTaskDatabase{}
	if err := app.DB().NewQuery(`
		SELECT [[feature]].[[data_source_id]], [[source]].[[page_id]], [[feature]].[[tasks_json]], [[source]].[[title]],
			COALESCE([[source]].[[icon]], '') AS [[icon]], COALESCE(NULLIF([[source]].[[icon_type]], ''), 'none') AS [[icon_type]],
			[[source]].[[created]]
		FROM {{leafen_database_task_features}} AS [[feature]]
		JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[id]] = [[feature]].[[data_source_id]]
		WHERE [[feature]].[[workspace_id]] = {:workspaceId} AND [[feature]].[[owner]] = {:owner}
			AND json_extract([[feature]].[[tasks_json]], '$.enabled') = 1
			AND ` + databaseSourceNotTrashedSQL("[[source]].[[id]]") + `
		ORDER BY [[source]].[[page_id]], [[feature]].[[data_source_id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"workspaceId": workspace.WorkspaceID, "owner": workspace.OwnerID, "limit": maxMyTaskDatabases + 1,
	}).All(&databases); err != nil {
		return nil, err
	}
	return databases, nil
}

// loadMyTasksInDatabase reads at most limit of a task database's rows that
// are whose says past after, a row of the database's own order
// (myTasksCursor), and says whether more follow and the last row it read.
// Each task says when it was made, its page's time as a row's createdAt
// does: a view places the tasks it never placed newest first, as Notion's
// "내 작업" does (myTasksViewOrder.ts).
func loadMyTasksInDatabase(
	ctx context.Context, app core.App, principalID, owner string, database storedMyTaskDatabase, roles databaseTaskRoles, whose myTasksWhose,
	after myTasksCursor, limit int,
) ([]databaseMyTaskWire, bool, myTasksCursor, error) {
	rows := []struct {
		ID       string `db:"id"`
		Position string `db:"position"`
		Icon     string `db:"icon"`
		IconType string `db:"icon_type"`
		Created  int64  `db:"created"`
	}{}
	// Only a list that keeps its first rows puts the person's first; a
	// window goes on in the rows' own order past its last row.
	theirsFirst := ""
	if whose == myTasksEveryoneTheirsFirst {
		theirsFirst = `COALESCE(json_valid([[assignee]].[[value]])
				AND EXISTS (SELECT 1 FROM json_each([[assignee]].[[value]]) WHERE [[value]] = {:principal}), 0) DESC,
			`
	}
	params := dbx.Params{
		"owner": owner, "pageId": database.PageID, "dataSourceId": database.DataSourceID, "everyone": whose != myTasksOwn,
		"assigneeId": roles.AssigneePropertyID, "principal": principalID, "limit": limit + 1,
	}
	if err := app.DB().NewQuery(`
		SELECT [[row]].[[id]], [[row]].[[position]], COALESCE([[page]].[[icon]], '') AS [[icon]],
			COALESCE(NULLIF([[page]].[[icon_type]], ''), 'none') AS [[icon_type]],
			COALESCE([[made]].[[created]], [[row]].[[created]]) AS [[created]]
		FROM {{leafen_database_rows}} AS [[row]]
		LEFT JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[id]] = [[row]].[[id]]
		LEFT JOIN {{leafen_database_row_pages}} AS [[made]] ON [[made]].[[row_id]] = [[row]].[[id]]
		LEFT JOIN {{leafen_database_row_values}} AS [[assignee]]
			ON [[assignee]].[[row_id]] = [[row]].[[id]] AND [[assignee]].[[property_id]] = {:assigneeId}
				AND [[assignee]].[[owner]] = {:owner} AND [[assignee]].[[page_id]] = {:pageId}
		WHERE [[row]].[[owner]] = {:owner} AND [[row]].[[page_id]] = {:pageId} AND [[row]].[[data_source_id]] = {:dataSourceId}
			AND ({:everyone} OR COALESCE(json_valid([[assignee]].[[value]])
				AND EXISTS (SELECT 1 FROM json_each([[assignee]].[[value]]) WHERE [[value]] = {:principal}), 0))
			AND COALESCE([[page]].[[archived_at]], '') = ''
			AND ` + databaseRowNotTrashedSQL("[[row]].[[id]]") + myTasksAfterSQL(after, params) + `
		ORDER BY ` + theirsFirst + `[[row]].[[position]], [[row]].[[id]] LIMIT {:limit}
	`).Bind(params).All(&rows); err != nil {
		return nil, false, after, err
	}
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	if len(rows) == 0 {
		return nil, false, after, nil
	}
	last := myTasksCursor{Page: database.PageID, Source: database.DataSourceID, Position: rows[len(rows)-1].Position, ID: rows[len(rows)-1].ID}
	rowIDs := make([]string, len(rows))
	for index, row := range rows {
		rowIDs[index] = row.ID
	}
	values, err := loadGraphRowValues(app, owner, database.PageID, rowIDs)
	if err != nil {
		return nil, false, after, err
	}
	byRow := map[string]map[string]any{}
	rawByRow := map[string]map[string][]byte{}
	for _, value := range values {
		decoded := any(nil)
		_ = json.Unmarshal([]byte(value.Value), &decoded)
		if byRow[value.RowID] == nil {
			byRow[value.RowID], rawByRow[value.RowID] = map[string]any{}, map[string][]byte{}
		}
		byRow[value.RowID][value.PropertyID] = decoded
		rawByRow[value.RowID][value.PropertyID] = []byte(value.Value)
	}
	titleID, status, err := loadMyTaskProperties(app, database.DataSourceID, roles.StatusPropertyID)
	if err != nil {
		return nil, false, after, err
	}
	// A task shows what its database's property access rules let the person
	// see: none whose assignee they may not see, and no status or due date
	// they may not see.
	reader, err := newDatabasePropertyAccessReader(app, principalID, []string{database.DataSourceID})
	if err != nil {
		return nil, false, after, err
	}
	// Nor an unlinked item they have no role in (database_row_unlinked_access.go).
	items, err := loadDatabaseRowAccess(app, principalID, []string{database.DataSourceID})
	if err != nil {
		return nil, false, after, err
	}
	// A task without a title is called after its database's default template.
	untitled, err := databaseUntitledRowNameFor(ctx, app, principalID, database.DataSourceID)
	if err != nil {
		return nil, false, after, err
	}
	tasks := make([]databaseMyTaskWire, 0, len(rows))
	for _, row := range rows {
		if items.hides(row.ID) {
			continue
		}
		values := status.withDefault(byRow[row.ID], roles.StatusPropertyID)
		if reader != nil {
			raw := rawByRow[row.ID]
			accessRow := &databasePropertyAccessRow{ID: row.ID, People: func(propertyID string) []string { return peopleInValue(raw[propertyID]) }}
			hidden := func(propertyID string) (bool, error) {
				if propertyID == "" || !reader.restricted(database.DataSourceID, propertyID) {
					return false, nil
				}
				level, err := reader.level(database.DataSourceID, propertyID, accessRow)
				return !propertyAccessShowsValues(level), err
			}
			if gone, err := hidden(roles.AssigneePropertyID); err != nil {
				return nil, false, after, err
			} else if gone {
				continue
			}
			visible := map[string]any{}
			for propertyID, value := range values {
				if hides, err := hidden(propertyID); err != nil {
					return nil, false, after, err
				} else if !hides {
					visible[propertyID] = value
				}
			}
			values = visible
		}
		task := databaseMyTaskWire{
			RowID: row.ID, PageID: database.PageID, DataSourceID: database.DataSourceID,
			DatabaseTitle: database.Title, Icon: row.Icon, IconType: row.IconType,
			DatabaseIcon: database.Icon, DatabaseIconType: database.IconType, TitlePropertyID: titleID, DueDatePropertyID: roles.DueDatePropertyID,
			UntitledName: untitled, Created: row.Created,
		}
		task.Assignees = peopleInValue(rawByRow[row.ID][roles.AssigneePropertyID])
		task.Title, _ = values[titleID].(string)
		task.StatusName, task.StatusColor, task.StatusGroup, task.Done = status.describe(values[roles.StatusPropertyID])
		if due, ok := values[roles.DueDatePropertyID].(map[string]any); ok && roles.DueDatePropertyID != "" {
			task.DueStart, task.DueEnd = mapString(due, "start"), mapString(due, "end")
		}
		tasks = append(tasks, task)
	}
	return tasks, truncated, last, nil
}

// The status groups a task's status sits in, by Notion's order.
var myTaskStatusGroups = [3]string{"todo", "inProgress", "complete"}

// myTaskStatus names a task's status value, its group and whether it is
// done; first is each group's first option, which marking a task done or
// not done, or making one, sets, and inGroup each group's options in order.
type myTaskStatus struct {
	checkbox bool
	options  map[string][2]string
	groups   map[string]int
	first    [3]string
	inGroup  [3][]string
	// fallback is the status's default, which a task that sets none reads
	// (databaseStatusDefaultOptionID).
	fallback string
}

// withDefault gives a task's values its status's default while it sets
// none, before what the person may not see is taken out.
func (status myTaskStatus) withDefault(values map[string]any, statusPropertyID string) map[string]any {
	if status.fallback == "" || statusPropertyID == "" {
		return values
	}
	// One the status no longer has sets none (databaseStatusValueOrDefault).
	if id, _ := values[statusPropertyID].(string); id != "" {
		if _, known := status.options[id]; known {
			return values
		}
	}
	read := make(map[string]any, len(values)+1)
	for propertyID, value := range values {
		read[propertyID] = value
	}
	read[statusPropertyID] = status.fallback
	return read
}

func (status myTaskStatus) describe(value any) (string, string, string, bool) {
	if status.checkbox {
		checked, _ := value.(bool)
		if checked {
			return "", "", myTaskStatusGroups[2], true
		}
		return "", "", myTaskStatusGroups[0], false
	}
	id, _ := value.(string)
	option := status.options[id]
	group, known := status.groups[id]
	if !known {
		return option[0], option[1], "", false
	}
	return option[0], option[1], myTaskStatusGroups[group], group == 2
}

// optionIn is the option of a group a task takes: the one named so in it
// (the status a task had, put back by My tasks' ⌘Z, or one chosen there),
// else the group's first, as a group alone asks.
func (status myTaskStatus) optionIn(group int, name string) string {
	if name != "" {
		for _, id := range status.inGroup[group] {
			if status.options[id][0] == name {
				return id
			}
		}
	}
	return status.first[group]
}

func loadMyTaskProperties(app core.App, dataSourceID, statusPropertyID string) (string, myTaskStatus, error) {
	properties := []struct {
		ID     string `db:"id"`
		Type   string `db:"type"`
		Config string `db:"config"`
	}{}
	status := myTaskStatus{options: map[string][2]string{}, groups: map[string]int{}}
	if err := app.DB().NewQuery(`
		SELECT [[id]], [[type]], [[config]] FROM {{leafen_database_properties}}
		WHERE [[data_source_id]] = {:dataSourceId} AND ([[type]] = 'title' OR [[id]] = {:statusId})
	`).Bind(dbx.Params{"dataSourceId": dataSourceID, "statusId": statusPropertyID}).All(&properties); err != nil {
		return "", status, err
	}
	titleID := ""
	for _, property := range properties {
		if property.Type == graphValueTypeTitle {
			titleID = property.ID
		}
		if property.ID != statusPropertyID {
			continue
		}
		status.checkbox = property.Type == graphValueTypeCheckbox
		if property.Type == graphValueTypeStatus {
			status.fallback = databaseStatusDefaultOptionID(property.Config)
		}
		config := struct {
			Options      []map[string]any `json:"options"`
			StatusGroups []map[string]any `json:"statusGroups"`
		}{}
		_ = json.Unmarshal([]byte(property.Config), &config)
		for _, option := range config.Options {
			status.options[mapString(option, "id")] = [2]string{mapString(option, "name"), mapString(option, "color")}
		}
		for index, group := range config.StatusGroups {
			position := notionStatusGroupIndex(config.StatusGroups, index)
			if position < 0 {
				continue
			}
			for _, option := range mapStrings(group, "optionIds") {
				status.groups[option] = position
				status.inGroup[position] = append(status.inGroup[position], option)
				if status.first[position] == "" {
					status.first[position] = option
				}
			}
		}
	}
	return titleID, status, nil
}
