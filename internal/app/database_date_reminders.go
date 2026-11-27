package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database date property value is {start, end?, timeZone?, reminder?,
// deadline?} — a deadline is Leafen's own (deadline_values.go).
// The web's "start" and "start/end" forms are read as that object, and a
// reminder is the minutes before the date it fires, with the choices date
// mentions offer. As in Notion, a date property's reminder reminds only the
// person who set it: it is kept in the reminders table under a row key, set
// by whoever changes it, delivered by the reminder runner, and removed when
// the value loses it or the row's projection is deleted
// (deleteDatabaseRowProjection).

const databaseDateReminderKeyPrefix = "row:"

// graphDateText reports one end of a date value that is a date: a day, a
// time with its offset, or a clock read in the value's zone, written with a
// "T" or a space — what every reader takes, and what Notion's API takes
// (2026-10-29: "yesterday", "2026-13-45" and hour 25 were kept as given).
func graphDateText(text string) bool {
	_, ok := graphDateMoment(text)
	return ok
}

func graphDateMoment(text string) (notionMentionMoment, bool) {
	return parseNotionMentionMoment(strings.Replace(strings.TrimSpace(text), " ", "T", 1), "")
}

// graphDateRangeOrdered reports a range whose end is not before its start,
// a day read as its first moment; as Notion's API, which refuses the other
// (seen 2026-10-29).
func graphDateRangeOrdered(start, end string) bool {
	from, ok := graphDateMoment(start)
	to, okEnd := graphDateMoment(end)
	if !ok || !okEnd {
		return true
	}
	instant := func(moment notionMentionMoment) time.Time {
		if moment.timed {
			return moment.at
		}
		day, _ := time.Parse(time.DateOnly, moment.day)
		return day
	}
	return !instant(to).Before(instant(from))
}

// canonicalGraphDateValue reads a date property value into its object form.
func canonicalGraphDateValue(value any) (map[string]any, bool) {
	if text, ok := value.(string); ok {
		start, end, _ := strings.Cut(strings.TrimSpace(text), "/")
		value = map[string]any{"start": strings.TrimSpace(start), "end": strings.TrimSpace(end)}
	}
	object, ok := value.(map[string]any)
	if !ok || !graphDateValue(object) {
		return nil, false
	}
	start := strings.TrimSpace(object["start"].(string))
	if !graphDateText(start) {
		return nil, false
	}
	canonical := map[string]any{"start": start}
	if end, _ := object["end"].(string); strings.TrimSpace(end) != "" {
		if !graphDateText(end) {
			return nil, false
		}
		canonical["end"] = strings.TrimSpace(end)
	}
	for _, key := range []string{"timeZone", "reminder", "deadline"} {
		raw, present := object[key]
		if !present || raw == nil || raw == "" {
			continue
		}
		text, isText := raw.(string)
		if !isText {
			return nil, false
		}
		switch key {
		case "timeZone":
			if _, err := time.LoadLocation(text); err != nil || len(text) > 64 {
				return nil, false
			}
		case "reminder":
			if !validDateMentionReminder(start, text) {
				return nil, false
			}
		case "deadline":
			days, _, valid := canonicalDeadlineDays(text)
			if !valid {
				return nil, false
			}
			text = days
		}
		canonical[key] = text
	}
	return canonical, true
}

func databaseDateReminderKey(rowID, propertyID string, date map[string]any) string {
	zone, _ := date["timeZone"].(string)
	reminder, _ := date["reminder"].(string)
	start, _ := date["start"].(string)
	return databaseDateReminderKeyPrefix + rowID + ":" + propertyID + "|" + start + "|" + zone + "|" + reminder
}

// databaseValueReminderDates are the dates a cell's value reminds of: a date
// value's own, or those of the date mentions in a title or text value.
func databaseValueReminderDates(value graphRowValue) []map[string]any {
	dates := []map[string]any{}
	switch value.Type {
	case graphValueTypeDate:
		date := map[string]any{}
		_ = json.Unmarshal(value.Value, &date)
		if reminder, _ := date["reminder"].(string); reminder != "" {
			dates = append(dates, date)
		}
	case graphValueTypeTitle, graphValueTypeRichText:
		for _, span := range value.Spans {
			if span.Mention == "date" && span.DateReminder != "" {
				dates = append(dates, map[string]any{"start": span.Date, "timeZone": span.DateTimeZone, "reminder": span.DateReminder})
			}
		}
	}
	return dates
}

// reconcileDatabaseDateReminders keeps the reminders of the date values a
// mutation wrote: an unchanged reminder keeps its setter, a new or changed
// one is set by the actor, and a value without one loses its reminder.
func reconcileDatabaseDateReminders(
	ctx context.Context, app core.App,
	workspaceID, pageID, actorID string,
	operations []graphOperation,
	now time.Time,
) error {
	remove := func(prefix string, keep map[string]bool) (map[string]bool, error) {
		kept := map[string]bool{}
		keys := []string{}
		if err := app.DB().NewQuery(`
			SELECT [[reminder_key]] FROM {{leafen_reminders}}
			WHERE [[page_id]] = {:pageId} AND substr([[reminder_key]], 1, length({:prefix})) = {:prefix}
		`).WithContext(ctx).Bind(dbx.Params{"pageId": pageID, "prefix": prefix}).Column(&keys); err != nil {
			return nil, err
		}
		for _, key := range keys {
			if keep[key] {
				kept[key] = true
				continue
			}
			if _, err := app.DB().NewQuery(`
				DELETE FROM {{leafen_reminders}} WHERE [[page_id]] = {:pageId} AND [[reminder_key]] = {:key}
			`).WithContext(ctx).Bind(dbx.Params{"pageId": pageID, "key": key}).Execute(); err != nil {
				return nil, err
			}
		}
		return kept, nil
	}
	inserted := false
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertRow || operation.Row == nil {
			continue
		}
		for propertyID, value := range operation.Row.Values {
			if value.Type != graphValueTypeDate && value.Type != graphValueTypeTitle && value.Type != graphValueTypeRichText {
				continue
			}
			dates := databaseValueReminderDates(value)
			keep := map[string]bool{}
			if actorID != "" {
				for _, date := range dates {
					keep[databaseDateReminderKey(operation.Row.ID, propertyID, date)] = true
				}
			}
			kept, err := remove(databaseDateReminderKeyPrefix+operation.Row.ID+":"+propertyID+"|", keep)
			if err != nil {
				return err
			}
			for _, date := range dates {
				key := databaseDateReminderKey(operation.Row.ID, propertyID, date)
				if !keep[key] || kept[key] {
					continue
				}
				kept[key] = true
				reminder, _ := date["reminder"].(string)
				start, _ := date["start"].(string)
				zone, _ := date["timeZone"].(string)
				if zone == "" {
					// A date without a time zone of its own reminds in its setter's.
					zone = loadAccountTimeZone(app, actorID)
				}
				due, ok := dateMentionReminderDue(start, zone, reminder)
				if !ok {
					continue
				}
				status := "pending"
				if due.Before(now.Add(-reminderPastTolerance)) {
					status = "past"
				}
				recipients, _ := json.Marshal([]string{actorID})
				if _, err := app.DB().NewQuery(`
					INSERT INTO {{leafen_reminders}} (
						[[id]], [[workspace_id]], [[page_id]], [[block_id]], [[reminder_key]], [[creator_id]],
						[[recipient_ids_json]], [[date_value]], [[preview]], [[due_at]], [[status]],
						[[created_at]], [[updated_at]]
					) VALUES (
						{:id}, {:workspaceId}, {:pageId}, {:rowId}, {:key}, {:creatorId},
						{:recipients}, {:dateValue}, '', {:dueAt}, {:status}, {:now}, {:now}
					)
				`).WithContext(ctx).Bind(dbx.Params{
					"id": reminderID(pageID, key), "workspaceId": workspaceID, "pageId": pageID,
					"rowId": operation.Row.ID, "key": key, "creatorId": actorID, "recipients": string(recipients),
					"dateValue": start, "dueAt": max(due.UnixMilli(), 0), "status": status, "now": now.UnixMilli(),
				}).Execute(); err != nil {
					return err
				}
				inserted = inserted || status == "pending"
			}
		}
	}
	if inserted {
		notifyReminderRunnerAfterCommit(app)
	}
	return nil
}
