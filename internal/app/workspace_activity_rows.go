package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type workspaceActivityRowState struct {
	ID       string `db:"id"`
	PageID   string `db:"page_id"`
	SourceID string `db:"data_source_id"`
	Trashed  bool   `db:"trashed"`
	Name     string `db:"name"`
}

func workspaceActivityTitleValue(raw string) string {
	var title string
	if json.Unmarshal([]byte(raw), &title) == nil {
		return strings.TrimSpace(title)
	}
	var spans []graphTextSpan
	if json.Unmarshal([]byte(raw), &spans) != nil {
		return ""
	}
	var text strings.Builder
	for _, span := range spans {
		text.WriteString(span.Text)
	}
	return strings.TrimSpace(text.String())
}

func workspaceActivityPageName(app core.App, page *core.Record) (string, error) {
	if !isDatabaseRowPageRecord(page) {
		return page.GetString("title"), nil
	}
	var raw string
	err := app.DB().NewQuery(`SELECT [[value]] FROM {{leafen_database_row_values}} WHERE [[row_id]]={:id} AND [[type]]='title' LIMIT 1`).Bind(dbx.Params{"id": page.Id}).Row(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return workspaceActivityTitleValue(raw), err
}

func captureWorkspaceActivityRows(app core.App, ids []string) (map[string]workspaceActivityRowState, error) {
	result := map[string]workspaceActivityRowState{}
	if len(ids) == 0 {
		return result, nil
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows := []workspaceActivityRowState{}
	if err = app.DB().NewQuery(`SELECT [[row]].[[id]],[[row]].[[page_id]],[[row]].[[data_source_id]],
		EXISTS(SELECT 1 FROM {{leafen_database_row_trash}} [[trash]] WHERE [[trash]].[[row_id]]=[[row]].[[id]]) AS [[trashed]],
		COALESCE((SELECT [[value]] FROM {{leafen_database_row_values}} [[title]] WHERE [[title]].[[row_id]]=[[row]].[[id]] AND [[title]].[[type]]='title' LIMIT 1),'') AS [[name]]
		FROM {{leafen_database_rows}} [[row]] WHERE [[row]].[[id]] IN (SELECT [[value]] FROM json_each({:ids}))`).Bind(dbx.Params{"ids": string(encoded)}).All(&rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		row.Name = workspaceActivityTitleValue(row.Name)
		result[row.ID] = row
	}
	return result, nil
}

// workspaceActivityRowTitleSpaced is a row change of nothing but spaces at
// its title's ends, which its name trims away: no rename, and no edit either
// (2026-11-13: each space typed between a new row's words listed it as
// edited under the words typed so far).
func workspaceActivityRowTitleSpaced(change pageHistoryRowChange) bool {
	if len(change.Values) == 0 {
		return false
	}
	for _, value := range change.Values {
		if value.Type != graphValueTypeTitle || value.From == value.To || strings.TrimSpace(value.From) != strings.TrimSpace(value.To) {
			return false
		}
	}
	return true
}

func recordWorkspaceRowActivities(app core.App, actor, origin string, at int64, capture *pageHistoryRowCapture, changes []pageHistoryRowChange, jobID string,
	pagesMadeRows []string) (bool, error) {
	if capture == nil {
		return false, nil
	}
	after, err := captureWorkspaceActivityRows(app, capture.ids)
	if err != nil {
		return false, err
	}
	// A row a change wrote is edited, but spaces at its title's ends,
	// though its own change, list nothing (workspaceActivityRowTitleSpaced).
	// A rename that wrote other values too is their edit besides: its
	// record's update lists them (workspace_activity_properties.go).
	edited, valued := map[string]bool{}, map[string]bool{}
	for _, change := range changes {
		edited[change.ID] = edited[change.ID] || !workspaceActivityRowTitleSpaced(change)
		for _, value := range change.Values {
			valued[change.ID] = valued[change.ID] || value.Type != graphValueTypeTitle && value.From != value.To
		}
	}
	// A page turned into its wiki's page is no page made: Notion's updates
	// list no making of the pages of a page turned into a wiki, in the wiki's
	// updates or in theirs (seen 2026-11-17 at 1416).
	madeFromPage := map[string]bool{}
	for _, id := range pagesMadeRows {
		madeFromPage[id] = true
	}
	changed := false
	for _, id := range capture.ids {
		was, existed := capture.activityBefore[id]
		now, exists := after[id]
		if !exists {
			continue
		} // Permanent removal cannot be read through its page.
		action, kind := "", "database_row"
		switch {
		case !existed && madeFromPage[id]:
			changed = true
			continue
		case !existed:
			action = "create"
		case was.Trashed != now.Trashed:
			if now.Trashed {
				action = "delete"
			} else {
				action = "restore"
				kind = "trashRestore"
			}
		case was.PageID != now.PageID || was.SourceID != now.SourceID:
			action = "move"
		case was.Name != now.Name:
			action = "rename"
		case edited[id]:
			action = "edit"
		}
		if action == "" {
			// Its title's spaces are the row's change, not an edit of its page.
			_, wrote := edited[id]
			changed = changed || wrote
			continue
		}
		page, err := app.FindRecordById(pagesCollection, id)
		if err != nil {
			return changed, err
		}
		metadata := workspaceActivityMetadata{OperationJobID: jobID, Name: now.Name, Kind: kind, LocationID: now.PageID, SourceID: now.SourceID}
		if action == "rename" {
			metadata.PreviousName = was.Name
		}
		if err = recordWorkspaceActivity(app, page, actor, origin, "page", id, action, metadata, at); err != nil {
			return changed, err
		}
		if action == "rename" && valued[id] {
			metadata.PreviousName = ""
			if err = recordWorkspaceActivity(app, page, actor, origin, "page", id, "edit", metadata, at); err != nil {
				return changed, err
			}
		}
		changed = true
	}
	return changed, nil
}
