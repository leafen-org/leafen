package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// My tasks a window at a time. The list My tasks shows is its first 500
// tasks, sorted, with truncated set past them (database_my_tasks.go); one
// that counts or reads them all — the Daily Briefing's worker, an agent —
// asks with ?limit= (up to maxMyTasks) and goes on with ?cursor= from each
// answer's nextCursor until it has none (2026-11-24: the briefing took a
// task database's first 200 rows and the list's first 500 for all, and
// counted a person's overdue and due tasks short). A window keeps the
// databases' order (by page, then data source) and, in each, its rows' own
// order (by place, then ID), past the last row the window before read, so
// a task added or moved meanwhile neither shifts the rest nor repeats a
// window. It holds the person's own tasks; with ?scope=sources, what the My
// tasks page and Home's widget read — its sources' tasks, none until the
// person picks them, then everyone's — which they read to the end so
// (2026-11-24: the page showed a task database's first 200 rows as all its
// tasks); with ?assignee=me too, only the person's own there, as Home's
// "예정된 이벤트" reads them.

// myTasksWhose is whose tasks a database gives: the person's own, or
// everyone's — the person's first where a list keeps only its first rows,
// or in the rows' own order where a window goes on past its last row.
type myTasksWhose int

const (
	myTasksOwn myTasksWhose = iota
	myTasksEveryoneTheirsFirst
	myTasksEveryone
)

// myTasksCursor is the last row a window read: its database's page and data
// source, its place and its ID. The zero value reads from the first; one
// with itself set reads from that row on, the row too — how a task is
// looked up (findMyTask), never a cursor a window gives.
type myTasksCursor struct {
	Page     string `json:"g"`
	Source   string `json:"s"`
	Position string `json:"p"`
	ID       string `json:"i"`
	itself   bool
}

func (cursor myTasksCursor) String() string {
	encoded, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(encoded)
}

// readMyTasksCursor reads a cursor a window gave back; false for one it
// didn't give.
func readMyTasksCursor(raw string) (myTasksCursor, bool) {
	cursor := myTasksCursor{}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || json.Unmarshal(decoded, &cursor) != nil || cursor.Page == "" || cursor.Source == "" || cursor.ID == "" {
		return myTasksCursor{}, false
	}
	return cursor, true
}

// myTasksAfterSQL keeps of a database's rows ([[row]]) those past after (or
// from it on, after itself set), binding its place in params; nothing when
// after is the start.
func myTasksAfterSQL(after myTasksCursor, params dbx.Params) string {
	if after.ID == "" {
		return ""
	}
	params["afterPosition"], params["afterId"] = after.Position, after.ID
	past := ">"
	if after.itself {
		past = ">="
	}
	return `
			AND ([[row]].[[position]], [[row]].[[id]]) ` + past + ` ({:afterPosition}, {:afterId})`
}

// myTasksAsk is what a read of My tasks asks for: a window (?limit= and
// ?cursor=: its size and where it goes on from) or the list, whether it
// reads My tasks' sources (?scope=sources) and whether only the person's
// own tasks there (?assignee=me).
type myTasksAsk struct {
	windowed bool
	limit    int
	after    myTasksCursor
	sources  bool
	own      bool
}

func myTasksAsked(event *core.RequestEvent) (myTasksAsk, error) {
	query := event.Request.URL.Query()
	ask := myTasksAsk{sources: query.Get("scope") == "sources"}
	switch assignee := strings.TrimSpace(query.Get("assignee")); assignee {
	case "":
	case "me":
		ask.own = true
	default:
		return ask, refuse(errDatabaseToolbarConfigInvalid, "assignee is me or left out, not %q", assignee)
	}
	rawLimit, rawCursor := strings.TrimSpace(query.Get("limit")), strings.TrimSpace(query.Get("cursor"))
	if rawLimit == "" && rawCursor == "" {
		return ask, nil
	}
	ask.windowed, ask.limit = true, maxMyTasks
	if rawLimit != "" {
		value, err := strconv.Atoi(rawLimit)
		if err != nil || value < 1 || value > maxMyTasks {
			return ask, refuse(errDatabaseToolbarConfigInvalid, "limit is a number from 1 to %d, not %q", maxMyTasks, rawLimit)
		}
		ask.limit = value
	}
	if rawCursor != "" {
		read, ok := readMyTasksCursor(rawCursor)
		if !ok {
			return ask, refuse(errDatabaseToolbarConfigInvalid, "cursor isn't one this list gave: leave it out to read from the start")
		}
		ask.after = read
	}
	return ask, nil
}

// myTasksWindowDatabases is how many task databases a window reads at once.
const myTasksWindowDatabases = 50

// readDatabaseMyTasksWindow reads the person's tasks past after, at most
// limit of them, and where the next window goes on from when it is full;
// with only, the tasks of those task databases — everyone's with everyone.
func readDatabaseMyTasksWindow(ctx context.Context, app core.App, principalID string, workspace workspaceAuthorization, limit int, after myTasksCursor,
	only []string, everyone bool,
) (databaseMyTasksResponse, error) {
	response := databaseMyTasksResponse{Tasks: []databaseMyTaskWire{}}
	if only != nil && len(only) == 0 {
		return response, nil
	}
	picked, whose := map[string]bool{}, myTasksOwn
	if everyone {
		whose = myTasksEveryone
	}
	for _, id := range only {
		picked[id] = true
	}
	reading := newMyTaskReading(ctx, app, principalID, workspace, whose)
	// The cursor's own database goes on past its row; the databases after
	// it from their first.
	fromPage, fromSource, past := after.Page, after.Source, ">="
	for {
		databases := []storedMyTaskDatabase{}
		if err := app.DB().NewQuery(`
			SELECT [[feature]].[[data_source_id]], [[source]].[[page_id]], [[feature]].[[tasks_json]], [[source]].[[title]],
				COALESCE([[source]].[[icon]], '') AS [[icon]], COALESCE(NULLIF([[source]].[[icon_type]], ''), 'none') AS [[icon_type]]
			FROM {{leafen_database_task_features}} AS [[feature]]
			JOIN {{leafen_data_sources}} AS [[source]] ON [[source]].[[id]] = [[feature]].[[data_source_id]]
			WHERE [[feature]].[[workspace_id]] = {:workspaceId} AND [[feature]].[[owner]] = {:owner}
				AND json_extract([[feature]].[[tasks_json]], '$.enabled') = 1
				AND ` + databaseSourceNotTrashedSQL("[[source]].[[id]]") + `
				AND ([[source]].[[page_id]], [[feature]].[[data_source_id]]) ` + past + ` ({:fromPage}, {:fromSource})
			ORDER BY [[source]].[[page_id]], [[feature]].[[data_source_id]] LIMIT {:limit}
		`).Bind(dbx.Params{
			"workspaceId": workspace.WorkspaceID, "owner": workspace.OwnerID, "fromPage": fromPage, "fromSource": fromSource,
			"limit": myTasksWindowDatabases,
		}).All(&databases); err != nil {
			return response, err
		}
		for _, database := range databases {
			if only != nil && !picked[database.DataSourceID] {
				continue
			}
			from := myTasksCursor{}
			if database.PageID == after.Page && database.DataSourceID == after.Source {
				from = after
			}
			for {
				tasks, more, last, err := reading.tasks(database, from, limit-len(response.Tasks))
				if err != nil {
					return response, err
				}
				response.Tasks = append(response.Tasks, tasks...)
				if len(response.Tasks) >= limit {
					// Whether anything follows is the next window's to find.
					response.NextCursor = last.String()
					return response, nil
				}
				if !more {
					break
				}
				from = last
			}
		}
		if len(databases) < myTasksWindowDatabases {
			return response, nil
		}
		fromPage, fromSource, past = databases[len(databases)-1].PageID, databases[len(databases)-1].DataSourceID, ">"
	}
}

// myTaskReading reads task databases' tasks for one person, asking once a
// database whether they may read it and change its tasks.
type myTaskReading struct {
	// ctx is the reader's own request, whose language names untitled tasks.
	ctx                context.Context
	app                core.App
	principalID        string
	workspace          workspaceAuthorization
	whose              myTasksWhose
	readable, writable map[string]bool
}

func newMyTaskReading(ctx context.Context, app core.App, principalID string, workspace workspaceAuthorization, whose myTasksWhose) *myTaskReading {
	return &myTaskReading{ctx: ctx, app: app, principalID: principalID, workspace: workspace, whose: whose,
		readable: map[string]bool{}, writable: map[string]bool{}}
}

// tasks reads at most limit rows of a task database past after, as
// loadMyTasksInDatabase does; none when the person can't read the database,
// its page is in the trash or under a page there, or it names no assignee.
func (reading *myTaskReading) tasks(database storedMyTaskDatabase, after myTasksCursor, limit int,
) ([]databaseMyTaskWire, bool, myTasksCursor, error) {
	allowed, checked := reading.readable[database.PageID]
	if !checked {
		var err error
		if allowed, err = myTaskDatabaseListed(reading.app, reading.principalID, database.PageID); err != nil {
			return nil, false, after, err
		}
		reading.readable[database.PageID] = allowed
	}
	roles := databaseTaskRoles{}
	if !allowed || json.Unmarshal([]byte(database.TasksJSON), &roles) != nil || !roles.Enabled || roles.AssigneePropertyID == "" {
		return nil, false, after, nil
	}
	tasks, more, last, err := loadMyTasksInDatabase(reading.ctx, reading.app, reading.principalID, reading.workspace.OwnerID, database, roles,
		reading.whose, after, limit)
	if err != nil {
		return nil, false, after, err
	}
	// A task the reader may only view says so, so the screen offers no
	// check, move or new task there (each would only be refused).
	canEdit, checked := reading.writable[database.PageID]
	if !checked {
		if canEdit, err = myTaskDatabaseWritable(reading.app, reading.principalID, database.PageID); err != nil {
			return nil, false, after, err
		}
		reading.writable[database.PageID] = canEdit
	}
	for index := range tasks {
		tasks[index].CanEdit = canEdit
	}
	return tasks, more, last, nil
}
