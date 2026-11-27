// The built-in Daily Briefing app (docs/app-platform.md 16): what is on a
// person's plate today — their open tasks overdue, due today and in the
// week ahead, and their unread mentions — gathered as the person, from every
// workspace of theirs in the organization. The app's screen, its side panel,
// its block and agents ask for it; each morning, at the hour the person
// chose, a notice says how much there is.
package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"leafen.org/leafen/sdk/app/go/leafen"
)

type task struct {
	PageID    string `json:"pageId"`
	Title     string `json:"title"`
	Database  string `json:"database"`
	Due       string `json:"due"`
	Status    string `json:"status"`
	Workspace string `json:"workspace"`
}

type mention struct {
	PageID    string `json:"pageId"`
	Title     string `json:"title"`
	Preview   string `json:"preview"`
	At        string `json:"at"`
	Workspace string `json:"workspace"`
}

type briefing struct {
	Date       string    `json:"date"`
	Overdue    []task    `json:"overdue"`
	Today      []task    `json:"today"`
	Soon       []task    `json:"soon"`
	Undated    int       `json:"undated"`
	Mentions   []mention `json:"mentions"`
	Unread     int       `json:"unread"`
	Workspaces int       `json:"workspaces"`
}

// settings are the person's own, kept in their store for the app.
type settings struct {
	Enabled  bool `json:"enabled"`
	Hour     int  `json:"hour"`
	Weekdays bool `json:"weekdays"`
	// The person's offset from UTC in minutes, as their browser had it.
	Offset int `json:"offset"`
}

func api(tenancy, method, path, query string, body any, answer any) error {
	reply, err := leafen.API(leafen.APIRequest{Tenancy: tenancy, Method: method, Path: path, Query: query, Body: body})
	if err != nil {
		return err
	}
	if reply.Status >= 300 {
		return fmt.Errorf("%s %s answered %d: %s", method, path, reply.Status, string(reply.Body))
	}
	return json.Unmarshal(reply.Body, answer)
}

// windowQuery asks for a window of limit items, past cursor when there is one.
func windowQuery(limit int, cursor string) string {
	query := fmt.Sprintf("limit=%d", limit)
	if cursor != "" {
		query += "&cursor=" + url.QueryEscape(cursor)
	}
	return query
}

func readSettings(tenancy string) settings {
	stored := struct {
		Value *settings `json:"value"`
	}{}
	if err := api(tenancy, "POST", "/app-storage", "", map[string]any{"command": "get", "key": "settings"}, &stored); err != nil || stored.Value == nil {
		return settings{Enabled: true, Hour: 8, Weekdays: true, Offset: 0}
	}
	return *stored.Value
}

// gather reads the person's tasks and mentions in each of their workspaces,
// on their own calendar day.
func gather(tenancy string, offset int) (briefing, error) {
	now := time.Now().UTC().Add(time.Duration(offset) * time.Minute)
	today := now.Format("2006-01-02")
	weekAhead := now.AddDate(0, 0, 7).Format("2006-01-02")
	out := briefing{Date: today, Overdue: []task{}, Today: []task{}, Soon: []task{}, Mentions: []mention{}}
	reach := struct {
		Workspaces []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
			App  bool   `json:"app"`
		} `json:"workspaces"`
	}{}
	if err := api(tenancy, "GET", "/app-workspaces", "", nil, &reach); err != nil {
		return out, err
	}
	for _, workspace := range reach.Workspaces {
		if workspace.App {
			continue
		}
		out.Workspaces++
		// Every task and unread mention, a window at a time to the last: the
		// first window taken for all counted a person's tasks past a task
		// database's 200th row or the list's 500th, and mentions past the
		// newest 50 notifications, as none.
		for cursor := ""; ; {
			tasks := struct {
				Tasks []struct {
					PageID   string `json:"pageId"`
					Database string `json:"databaseTitle"`
					Title    string `json:"title"`
					Status   string `json:"statusName"`
					Done     bool   `json:"done"`
					DueStart string `json:"dueStart"`
				} `json:"tasks"`
				NextCursor string `json:"nextCursor"`
			}{}
			if err := api(tenancy, "GET", "/workspaces/"+url.PathEscape(workspace.ID)+"/my-tasks", windowQuery(500, cursor), nil, &tasks); err != nil {
				leafen.Log("tasks of " + workspace.ID + ": " + err.Error())
				break
			}
			for _, item := range tasks.Tasks {
				if item.Done {
					continue
				}
				entry := task{PageID: item.PageID, Title: item.Title, Database: item.Database, Due: item.DueStart, Status: item.Status, Workspace: workspace.Name}
				due := item.DueStart
				if len(due) > 10 {
					due = due[:10]
				}
				switch {
				case due == "":
					out.Undated++
				case due < today:
					out.Overdue = append(out.Overdue, entry)
				case due == today:
					out.Today = append(out.Today, entry)
				case due <= weekAhead:
					out.Soon = append(out.Soon, entry)
				}
			}
			if tasks.NextCursor == "" || tasks.NextCursor == cursor {
				break
			}
			cursor = tasks.NextCursor
		}
		for cursor := ""; ; {
			inbox := struct {
				Items []struct {
					Kind    string `json:"kind"`
					PageID  string `json:"pageId"`
					Title   string `json:"title"`
					Preview string `json:"preview"`
					At      string `json:"occurredAt"`
				} `json:"items"`
				NextCursor string `json:"nextCursor"`
			}{}
			if err := api(tenancy, "GET", "/workspaces/"+url.PathEscape(workspace.ID)+"/notifications", "state=unread&"+windowQuery(200, cursor), nil, &inbox); err != nil {
				leafen.Log("inbox of " + workspace.ID + ": " + err.Error())
				break
			}
			for _, item := range inbox.Items {
				if item.Kind == "mention" {
					out.Unread++
					out.Mentions = append(out.Mentions, mention{PageID: item.PageID, Title: item.Title, Preview: item.Preview, At: item.At, Workspace: workspace.Name})
				}
			}
			if inbox.NextCursor == "" || inbox.NextCursor == cursor {
				break
			}
			cursor = inbox.NextCursor
		}
	}
	byDue := func(list []task) {
		sort.SliceStable(list, func(i, j int) bool { return list[i].Due < list[j].Due })
	}
	byDue(out.Overdue)
	byDue(out.Today)
	byDue(out.Soon)
	sort.SliceStable(out.Mentions, func(i, j int) bool { return out.Mentions[i].At > out.Mentions[j].At })
	if len(out.Mentions) > 10 {
		out.Mentions = out.Mentions[:10]
	}
	return out, nil
}

// today answers the screen, the panel, the block and agents.
//
//go:wasmexport today
func today() int32 {
	input := struct {
		Offset *int `json:"offset"`
	}{}
	call, err := leafen.Action(&input)
	if err != nil {
		return leafen.Fail("the input isn't JSON")
	}
	offset := 0
	if input.Offset != nil {
		offset = *input.Offset
	} else {
		offset = readSettings(call.Tenancy).Offset
	}
	found, err := gather(call.Tenancy, offset)
	if err != nil {
		return leafen.Fail(err.Error())
	}
	_ = leafen.OutputJSON(found)
	return 0
}

// morning runs each hour for each person and tells them, at the hour they
// chose, how much is on their plate.
//
//go:wasmexport morning
func morning() int32 {
	call := leafen.ScheduleCall{}
	if err := leafen.InputJSON(&call); err != nil {
		return leafen.Fail("the input isn't JSON")
	}
	if call.Person == "" {
		return 0
	}
	chosen := readSettings(call.Tenancy)
	local := time.Now().UTC().Add(time.Duration(chosen.Offset) * time.Minute)
	if !chosen.Enabled || local.Hour() != chosen.Hour || chosen.Weekdays && (local.Weekday() == time.Saturday || local.Weekday() == time.Sunday) {
		return 0
	}
	found, err := gather(call.Tenancy, chosen.Offset)
	if err != nil {
		return leafen.Fail(err.Error())
	}
	parts := []string{}
	if count := len(found.Today); count > 0 {
		parts = append(parts, fmt.Sprintf("오늘 할 일 %d개", count))
	}
	if count := len(found.Overdue); count > 0 {
		parts = append(parts, fmt.Sprintf("기한 지난 일 %d개", count))
	}
	if found.Unread > 0 {
		parts = append(parts, fmt.Sprintf("읽지 않은 멘션 %d개", found.Unread))
	}
	if len(parts) == 0 {
		return 0
	}
	if err := leafen.Notify(leafen.NoticeRequest{Person: call.Person, Tenancy: call.Tenancy, Text: "하루 브리핑 · " + strings.Join(parts, " · ")}); err != nil {
		leafen.Log("notice not sent: " + err.Error())
	}
	return 0
}

func main() {}
