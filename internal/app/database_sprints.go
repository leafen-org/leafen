package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A task database can run sprints as Notion's do: turning them on creates,
// beside the task database, a Sprints database (sprint name, status, dates
// and its tasks) and a Sprint board page whose linked view of the tasks has
// Current sprint, Sprint planning and Backlog tabs, and a Sprint relation on
// the tasks. Completing a sprint moves Current to Last, Last to Past and
// Next to Current, plans a new Next sprint and carries unfinished tasks
// forward, to the backlog, or leaves them; an automatic cadence completes
// sprints when they end.

const (
	databaseSprintStatusCurrent = "current"
	databaseSprintStatusNext    = "next"
	databaseSprintStatusLast    = "last"
	databaseSprintStatusPast    = "past"
	databaseSprintStatusFuture  = "future"
)

// Sprint requests answer as the toolbar configuration they belong to.
var (
	errDatabaseSprintInvalid  = errDatabaseToolbarConfigInvalid
	errDatabaseSprintConflict = errDatabaseToolbarConfigConflict
)

// databaseTaskRoles are a task database's status, assignee and due date
// properties, and whether the status was a select it was made from.
type databaseTaskRoles struct {
	Enabled            bool   `json:"enabled"`
	StatusPropertyID   string `json:"statusPropertyId"`
	AssigneePropertyID string `json:"assigneePropertyId"`
	DueDatePropertyID  string `json:"dueDatePropertyId"`
	StatusWasSelect    bool   `json:"statusWasSelect"`
}

type storedDatabaseSprints struct {
	DataSourceID          string `db:"data_source_id"`
	Owner                 string `db:"owner"`
	WorkspaceID           string `db:"workspace_id"`
	PageID                string `db:"page_id"`
	SprintsPageID         string `db:"sprints_page_id"`
	BoardPageID           string `db:"board_page_id"`
	SprintsDataSourceID   string `db:"sprints_data_source_id"`
	TaskSprintPropertyID  string `db:"task_sprint_property_id"`
	SprintTasksPropertyID string `db:"sprint_tasks_property_id"`
	StatusPropertyID      string `db:"sprint_status_property_id"`
	DatesPropertyID       string `db:"sprint_dates_property_id"`
	ViewsJSON             string `db:"views_json"`
	DurationWeeks         int    `db:"duration_weeks"`
	StartWeekday          int    `db:"start_weekday"`
	IncompleteTasks       string `db:"incomplete_tasks"`
	Automatic             bool   `db:"automatic"`
	TimeZone              string `db:"time_zone"`
	SprintSequence        int    `db:"sprint_sequence"`
	NextRolloverAt        int64  `db:"next_rollover_at"`
	Revision              int    `db:"revision"`
	LastRequestID         string `db:"last_request_id"`
}

type databaseSprintViews struct {
	Current  string `json:"current"`
	Planning string `json:"planning"`
	Backlog  string `json:"backlog"`
	All      string `json:"all"`
	Timeline string `json:"timeline"`
}

func (sprints storedDatabaseSprints) views() databaseSprintViews {
	views := databaseSprintViews{}
	_ = json.Unmarshal([]byte(sprints.ViewsJSON), &views)
	return views
}

func loadDatabaseSprints(app core.App, dataSourceID string) (storedDatabaseSprints, bool, error) {
	stored := storedDatabaseSprints{}
	err := app.DB().NewQuery(`
		SELECT [[data_source_id]], [[owner]], [[workspace_id]], [[page_id]], [[sprints_page_id]], [[board_page_id]],
			[[sprints_data_source_id]], [[task_sprint_property_id]], [[sprint_tasks_property_id]],
			[[sprint_status_property_id]], [[sprint_dates_property_id]], [[views_json]], [[duration_weeks]],
			[[start_weekday]], [[incomplete_tasks]], [[automatic]], [[time_zone]], [[sprint_sequence]],
			[[next_rollover_at]], [[revision]], [[last_request_id]]
		FROM {{leafen_database_sprints}} WHERE [[data_source_id]] = {:dataSourceId}
	`).Bind(dbx.Params{"dataSourceId": dataSourceID}).One(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return stored, false, nil
	}
	return stored, err == nil, err
}

func loadDatabaseTaskRoles(app core.App, dataSourceID string) (databaseTaskRoles, error) {
	roles := databaseTaskRoles{}
	raw := ""
	err := app.DB().NewQuery(`
		SELECT [[tasks_json]] FROM {{leafen_database_task_features}} WHERE [[data_source_id]] = {:dataSourceId}
	`).Bind(dbx.Params{"dataSourceId": dataSourceID}).Row(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return roles, nil
	}
	if err != nil {
		return roles, err
	}
	_ = json.Unmarshal([]byte(raw), &roles)
	return roles, nil
}

func normalizeDatabaseSprintSettings(settings databaseSprintSettingsWire) (databaseSprintSettingsWire, error) {
	settings.IncompleteTasks = strings.TrimSpace(settings.IncompleteTasks)
	settings.TimeZone = strings.TrimSpace(settings.TimeZone)
	if settings.TimeZone == "" {
		settings.TimeZone = "UTC"
	}
	if _, err := time.LoadLocation(settings.TimeZone); err != nil || len(settings.TimeZone) > 160 {
		return settings, databaseToolbarRefused("settings.timeZone is left out, or an IANA time zone such as Asia/Seoul, not %q", settings.TimeZone)
	}
	switch settings.IncompleteTasks {
	case "next", "backlog", "keep":
	default:
		return settings, databaseToolbarRefused("settings.incompleteTasks is next, backlog or keep, not %q", settings.IncompleteTasks)
	}
	switch {
	case settings.DurationWeeks < 1 || settings.DurationWeeks > 8:
		return settings, databaseToolbarRefused("settings.durationWeeks is 1 to 8 weeks, not %d", settings.DurationWeeks)
	case settings.StartWeekday < 0 || settings.StartWeekday > 6:
		return settings, databaseToolbarRefused("settings.startWeekday is 0 (Sunday) to 6 (Saturday), not %d", settings.StartWeekday)
	}
	return settings, nil
}

// databaseSprintDeterministicID derives a stable record id from the request,
// so a retried sprint action recreates exactly what it already made.
func databaseSprintDeterministicID(parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	alphabet := core.DefaultIdAlphabet
	result := make([]byte, core.DefaultIdLength)
	for index := range result {
		result[index] = alphabet[int(digest[index])%len(alphabet)]
	}
	return string(result)
}

// databaseSprintDay is a calendar day in the sprint time zone.
func databaseSprintDay(at time.Time, zone string) time.Time {
	location, err := time.LoadLocation(zone)
	if err != nil {
		location = time.UTC
	}
	local := at.In(location)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
}

// databaseSprintAlignedStart is the latest start weekday on or before day.
func databaseSprintAlignedStart(day time.Time, weekday int) time.Time {
	return day.AddDate(0, 0, -((int(day.Weekday()) - weekday + 7) % 7))
}

func databaseSprintEnd(start time.Time, weeks int) time.Time {
	return start.AddDate(0, 0, weeks*7-1)
}

// databaseSprintRolloverAt is the instant a sprint ending on `end` is done:
// midnight after its last day in the sprint time zone.
func databaseSprintRolloverAt(end time.Time, zone string) int64 {
	location, err := time.LoadLocation(zone)
	if err != nil {
		location = time.UTC
	}
	next := end.AddDate(0, 0, 1)
	return time.Date(next.Year(), next.Month(), next.Day(), 0, 0, 0, 0, location).UnixMilli()
}

func registerDatabaseSprintRoutes(event *core.ServeEvent, ygo *pocketBaseYgoPersistence, rooms workspaceRoomInvalidator) {
	event.Router.Route(databaseSprintReadMethod, apiRoot+databaseSprintReadPath, func(event *core.RequestEvent) error {
		user, ok, err := databaseToolbarConfigActor(event)
		if !ok {
			return err
		}
		pageID, dataSourceID, _ := databaseToolbarConfigIdentity(event)
		if _, err := authorizeDatabaseToolbarConfig(event.App, user.Id, pageID, dataSourceID, permissionWorkspaceRead); err != nil {
			return writeDatabaseToolbarConfigError(event, err)
		}
		state, err := readDatabaseSprintState(event.App, user.Id, dataSourceID)
		if err != nil {
			return writeDatabaseToolbarConfigError(event, err)
		}
		return event.JSON(http.StatusOK, state)
	})
	event.Router.Route(databaseSprintActionMethod, apiRoot+databaseSprintActionPath, func(event *core.RequestEvent) error {
		user, ok, err := databaseToolbarConfigActor(event)
		if !ok {
			return err
		}
		pageID, dataSourceID, _ := databaseToolbarConfigIdentity(event)
		input := databaseSprintActionInput{}
		if err := event.BindBody(&input); err != nil {
			return writeDatabaseToolbarConfigError(event, bodyRefusal(errDatabaseSprintInvalid, "sprint action", err))
		}
		authority, err := authorizeDatabaseToolbarConfig(event.App, user.Id, pageID, dataSourceID, permissionWorkspaceWrite)
		if err != nil {
			return writeDatabaseToolbarConfigError(event, err)
		}
		// What the action makes is named in the language the request prefers,
		// for an account following the system.
		ctx := withRequestLanguage(event.Request.Context(), event.Request)
		response, err := runDatabaseSprintActionWithRooms(ctx, event.App, ygo, rooms, user.Id, authority, input)
		if err != nil {
			return writeDatabaseToolbarConfigError(event, err)
		}
		return event.JSON(http.StatusOK, response)
	})
}

func readDatabaseSprintState(app core.App, readerID, dataSourceID string) (databaseSprintStateWire, error) {
	stored, enabled, err := loadDatabaseSprints(app, dataSourceID)
	if err != nil || !enabled {
		return databaseSprintStateWire{Sprints: []databaseSprintSummaryWire{}}, err
	}
	summaries, err := loadDatabaseSprintSummaries(app, stored)
	if err != nil {
		return databaseSprintStateWire{}, err
	}
	// A sprint's name, a row title, is read by its reader: a page it
	// mentions that they can't open is closed in marks (page_name_mentions.go).
	titles := make(map[string]*string, len(summaries))
	for index := range summaries {
		titles[summaries[index].ID] = &summaries[index].Title
	}
	pageTitlesAsRead(app, readerID, titles, true)
	views := stored.views()
	state := databaseSprintStateWire{
		Enabled: true, Revision: stored.Revision,
		Settings: databaseSprintSettingsWire{
			DurationWeeks: stored.DurationWeeks, StartWeekday: stored.StartWeekday,
			IncompleteTasks: stored.IncompleteTasks, Automatic: stored.Automatic, TimeZone: stored.TimeZone,
		},
		SprintsPageID: stored.SprintsPageID, BoardPageID: stored.BoardPageID,
		SprintsDataSourceID:  stored.SprintsDataSourceID,
		TaskSprintPropertyID: stored.TaskSprintPropertyID, CurrentViewID: views.Current,
		PlanningViewID: views.Planning, BacklogViewID: views.Backlog, Sprints: summaries,
	}
	if stored.Automatic && stored.NextRolloverAt > 0 {
		state.NextRolloverAt = time.UnixMilli(stored.NextRolloverAt).UTC().Format(time.RFC3339)
	}
	return state, nil
}

// loadDatabaseSprintSummaries lists the sprints with a status, newest first:
// every one, read a window at a time.
func loadDatabaseSprintSummaries(app core.App, stored storedDatabaseSprints) ([]databaseSprintSummaryWire, error) {
	type sprintRow struct {
		ID       string `db:"id"`
		Position string `db:"position"`
		Title    string `db:"title"`
		Status   string `db:"status"`
		Dates    string `db:"dates"`
	}
	summaries := []databaseSprintSummaryWire{}
	afterPosition, afterID, first := "", "", true
	for {
		rows := []sprintRow{}
		err := app.DB().NewQuery(`
			SELECT [[rows]].[[id]], [[rows]].[[position]],
				COALESCE((SELECT json_extract([[value]], '$') FROM {{leafen_database_row_values}} [[title]]
					JOIN {{leafen_database_properties}} [[property]] ON [[property]].[[id]] = [[title]].[[property_id]]
					WHERE [[title]].[[row_id]] = [[rows]].[[id]] AND [[property]].[[type]] = 'title' LIMIT 1), '') AS [[title]],
				COALESCE((SELECT json_extract([[value]], '$') FROM {{leafen_database_row_values}}
					WHERE [[row_id]] = [[rows]].[[id]] AND [[property_id]] = {:statusId}), '') AS [[status]],
				COALESCE((SELECT [[value]] FROM {{leafen_database_row_values}}
					WHERE [[row_id]] = [[rows]].[[id]] AND [[property_id]] = {:datesId}), '') AS [[dates]]
			FROM {{leafen_database_rows}} AS [[rows]]
			WHERE ` + databaseSourceRowsSQL("[[rows]]", "{:sprintsId}") + `
				AND ({:first} OR [[rows]].[[position]] < {:afterPosition}
					OR ([[rows]].[[position]] = {:afterPosition} AND [[rows]].[[id]] < {:afterId}))
			ORDER BY [[rows]].[[position]] DESC, [[rows]].[[id]] DESC LIMIT {:limit}
		`).Bind(dbx.Params{
			"statusId": stored.StatusPropertyID, "datesId": stored.DatesPropertyID,
			"sprintsId": stored.SprintsDataSourceID, "first": first,
			"afterPosition": afterPosition, "afterId": afterID, "limit": databaseSprintSummaryWindow,
		}).All(&rows)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			status := strings.TrimPrefix(row.Status, "sprint-")
			if status == "" {
				continue
			}
			dates := map[string]any{}
			_ = json.Unmarshal([]byte(row.Dates), &dates)
			start, _ := dates["start"].(string)
			end, _ := dates["end"].(string)
			summaries = append(summaries, databaseSprintSummaryWire{
				ID: row.ID, Title: row.Title, Status: status, Start: start, End: end,
			})
		}
		if len(rows) < databaseSprintSummaryWindow {
			return summaries, nil
		}
		afterPosition, afterID, first = rows[len(rows)-1].Position, rows[len(rows)-1].ID, false
	}
}

// databaseSprintSummaryWindow is how many sprints one read takes.
const databaseSprintSummaryWindow = 500
