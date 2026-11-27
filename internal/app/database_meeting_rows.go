package app

import (
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// databaseMeetingRowIcon is Notion's gray calendar, which its meeting notes
// database gives each meeting made there by hand ("+ 새 회의", seen
// 2026-11-12: the row's page_icon /icons/calendar_gray.svg, its date today).
// The date is the maker's day, which their screen gives the row
// (web lib/newRowStart.ts); the icon is the database's, given here.
const databaseMeetingRowIcon = "calendar:gray"

// prepareDatabaseRowsMade finds the rows a write makes in databases that
// give their new rows something: a Sprints database's, which also start as a
// future sprint (prepareDatabaseSprintRows), and a meeting notes database's,
// which get the calendar. A form's answer is not a meeting made by hand.
func prepareDatabaseRowsMade(app core.App, pageID string, operations []graphOperation, formResponse bool) ([]string, []string, error) {
	sprints, err := prepareDatabaseSprintRows(app, pageID, operations)
	if err != nil || formResponse {
		return sprints, nil, err
	}
	meetings, err := prepareDatabaseMeetingRows(app, pageID, operations)
	return sprints, meetings, err
}

// prepareDatabaseMeetingRows finds the rows a write makes in a meeting notes
// database: one made from the meeting notes template or as Notion's
// meeting_notes type (notionStoredDatabaseType).
func prepareDatabaseMeetingRows(app core.App, pageID string, operations []graphOperation) ([]string, error) {
	meeting, made := map[string]bool{}, []string(nil)
	for _, operation := range operations {
		if operation.Kind != graphOperationKindUpsertRow || operation.Row == nil || operation.Row.DataSourceID == "" {
			continue
		}
		row := operation.Row
		is, known := meeting[row.DataSourceID]
		if !known {
			count := 0
			if err := app.DB().NewQuery(`
				SELECT COUNT(*) FROM {{leafen_data_sources}}
				WHERE [[id]] = {:id} AND ([[starter_template]] = {:template} OR [[database_type]] = {:type})
			`).Bind(dbx.Params{"id": row.DataSourceID, "template": databaseStarterMeetings, "type": notionDatabaseTypeMeetingNotes}).Row(&count); err != nil {
				return nil, err
			}
			is = count > 0
			meeting[row.DataSourceID] = is
		}
		if !is {
			continue
		}
		if stored, err := databaseRowRecordSource(app, pageID, row.ID); err != nil {
			return nil, err
		} else if stored == row.DataSourceID {
			continue
		}
		made = append(made, row.ID)
	}
	return made, nil
}

// giveDatabaseRowsMadeIcons gives the rows made in a Sprints database the
// runner and those made in a meeting notes database the calendar, each
// unless it came with an icon of its own.
func giveDatabaseRowsMadeIcons(app core.App, sprints, meetings []string) error {
	if err := giveDatabaseSprintRowsIcon(app, sprints); err != nil {
		return err
	}
	for _, rowID := range meetings {
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_pages}} SET [[icon]] = {:icon}, [[icon_type]] = {:iconType}
			WHERE [[id]] = {:id} AND [[data_source_id]] != '' AND [[icon]] = ''
		`).Bind(dbx.Params{"icon": databaseMeetingRowIcon, "iconType": nativeIconType, "id": rowID}).Execute(); err != nil {
			return err
		}
	}
	return nil
}
