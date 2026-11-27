package app

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// What a stopped run left out comes when it is tried again (2026-11-23: a
// Notion move left out a database's unnamed title property in a window it
// passed and stopped in the next; resumed on a Leafen that takes such a
// property, it went on from where it stopped and finished with the
// database lacking its title property, its rows their titles and its view
// that column — the check said "visible columns: Notion 5 · Leafen 4"). Tried
// again, the run takes everything it left out off its list: what it left out
// of the window it stopped in comes as that window is made again, and what it
// left out of the windows it had passed is made first, in a late pass of its
// own over those windows in their order. A property made so is one made
// after rows of its database: the late pass takes those rows again, and each
// takes its cell of it, as a row made by an earlier run of a migration takes
// its cell of a property new in a run again, and a view made by an earlier
// run that shows such a property takes its Notion settings again — what
// links them is made after, in the link phase, so a view made then shows it.

// retryNotionImportLeftOut has a stopped run, tried again while it makes,
// try again all it left out: out of its payload, and its report of it gone.
// What it left out of the windows it had passed is the late pass's.
func retryNotionImportLeftOut(app core.App, jobID string) error {
	job, err := loadStoredJob(app, jobID, "")
	if err != nil {
		return err
	}
	payload := notionImportJobPayload{}
	if job.Status != durableJobStatusFailed || json.Unmarshal([]byte(job.PayloadJSON), &payload) != nil || len(payload.LeftOut) == 0 ||
		payload.Linking || payload.Publishing || payload.Restructuring || payload.StampingRows {
		return nil
	}
	cursor, err := parseNotionApplyCursor(job.Cursor)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(payload.LeftOut)
	if err != nil {
		return err
	}
	left := []struct {
		SourceID string `db:"source_id"`
		Sequence int64  `db:"sequence"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[source_id]], [[sequence]] FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]] = {:jobId} AND [[kind]] <> 'issue' AND [[source_id]] IN (SELECT [[value]] FROM json_each({:leftOut}))
		ORDER BY [[sequence]]
	`).Bind(dbx.Params{"jobId": jobID, "leftOut": string(encoded)}).All(&left); err != nil || len(left) == 0 {
		return err
	}
	retried := map[string]bool{}
	for _, item := range left {
		retried[item.SourceID] = true
		result, err := app.DB().NewQuery(`
			DELETE FROM {{leafen_notion_import_sources}} WHERE [[job_id]] = {:jobId} AND [[kind]] = 'issue' AND [[source_id]] = {:issue}
		`).Bind(dbx.Params{"jobId": jobID, "issue": item.SourceID + "#" + notionImportIssueInvalidBlock}).Execute()
		if err != nil {
			return err
		}
		if removed, _ := result.RowsAffected(); removed > 0 {
			if _, err := app.DB().NewQuery(`UPDATE {{leafen_jobs}} SET [[total_items]] = MAX([[total_items]] - {:removed}, 0) WHERE [[id]] = {:id}`).
				Bind(dbx.Params{"id": jobID, "removed": removed}).Execute(); err != nil {
				return err
			}
		}
		// One of a window the run had passed is the late pass's, which goes
		// over those windows again from their start.
		if item.Sequence <= cursor && !slices.Contains(payload.Late, item.SourceID) {
			payload.Late = append(payload.Late, item.SourceID)
			payload.LateCursor, payload.LateUntil = 0, max(payload.LateUntil, cursor)
		}
	}
	payload.LeftOut = slices.DeleteFunc(payload.LeftOut, func(id string) bool { return retried[id] })
	updated, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`UPDATE {{leafen_jobs}} SET [[payload_json]] = {:payload} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": jobID, "payload": string(updated)}).Execute()
	return err
}

// loadNotionImportWindow loads the window a run makes next: the late pass's
// while it has one, else up to items of what the job read after cursor.
func loadNotionImportWindow(
	app core.App, jobID string, payload notionImportJobPayload, cursor int64, items int,
) (notionApplySourceWindow, bool, error) {
	if len(payload.Late) == 0 {
		window, err := loadNotionApplySourceWindow(app, jobID, cursor, items)
		return window, false, err
	}
	late, err := json.Marshal(payload.Late)
	if err != nil {
		return notionApplySourceWindow{}, true, err
	}
	// The rows of a database one of its properties is late for, which the run
	// made: each takes its cell of it.
	parents := []string{}
	for _, id := range payload.Late {
		if parent, _, found := strings.Cut(id, ":property:"); found {
			parents = append(parents, parent)
		}
	}
	encodedParents, err := json.Marshal(parents)
	if err != nil {
		return notionApplySourceWindow{}, true, err
	}
	stored := []storedNotionSource{}
	if err := app.DB().NewQuery(`
		SELECT [[sequence]], [[source_id]], [[kind]], [[parent_source_id]], [[parent_kind]], [[position]], [[title]], [[text]]
		FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]] = {:jobId} AND [[sequence]] > {:from} AND [[sequence]] <= {:until} AND [[kind]] <> 'issue' AND (
			[[source_id]] IN (SELECT [[value]] FROM json_each({:late}))
			OR [[kind]] = 'row' AND [[parent_source_id]] IN (SELECT [[value]] FROM json_each({:parents}))
				AND [[source_id]] IN (SELECT [[source_id]] FROM {{leafen_notion_import_mappings}}
					WHERE [[job_id]] = {:jobId} AND [[kind]] = 'row'))
		ORDER BY [[sequence]]
		LIMIT {:limit}
	`).Bind(dbx.Params{
		"jobId": jobID, "from": payload.LateCursor, "until": payload.LateUntil,
		"late": string(late), "parents": string(encodedParents), "limit": items + 1,
	}).All(&stored); err != nil {
		return notionApplySourceWindow{}, true, err
	}
	window, bytes := notionApplySourceWindow{CandidateCount: len(stored)}, 0
	for index, source := range stored {
		bytes += len(source.SourceID) + len(source.Kind) + len(source.ParentSourceID) + len(source.ParentKind) + len(source.Title) + len(source.Text)
		if index >= items || index > 0 && bytes > maxGraphChangeBytes {
			break
		}
		window.Sources = append(window.Sources, source)
	}
	return window, true, nil
}

// notionImportLateWindowDone moves the late pass past a window of it, and
// ends it after its last; it says whether the window was the pass's.
func notionImportLateWindowDone(payload *notionImportJobPayload, late bool, all []storedNotionSource, hasMore bool) bool {
	if !late {
		return false
	}
	if len(all) > 0 {
		payload.LateCursor = all[len(all)-1].Sequence
	}
	if !hasMore {
		payload.Late, payload.LateCursor, payload.LateUntil = nil, 0, 0
	}
	return true
}

// notionImportLateProperty notes a property the run just made as one rows of
// its database were made before: one the late pass makes, or one new in a
// migration's run again of a database an earlier run made.
func notionImportLateProperty(app core.App, payload *notionImportJobPayload, late bool, source storedNotionSource) error {
	if slices.Contains(payload.LateProperties, source.SourceID) {
		return nil
	}
	if !late {
		if payload.MigrationRerunSince <= 0 {
			return nil
		}
		var earlier int
		if err := app.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_notion_migration_ledger}}
			WHERE [[migration_id]] = {:migrationId} AND [[source_id]] = {:sourceId} AND [[kind]] = 'data_source'
		`).Bind(dbx.Params{"migrationId": payload.MigrationID, "sourceId": source.ParentSourceID}).Row(&earlier); err != nil || earlier == 0 {
			return err
		}
	}
	payload.LateProperties = append(payload.LateProperties, source.SourceID)
	return nil
}

// notionImportRowLateCells adds to what a row made before takes its cells
// of the properties made since.
func notionImportRowLateCells(payload notionImportJobPayload, source storedNotionSource, change notionMigrationRowChange) notionMigrationRowChange {
	if change.Changed && change.Cells == nil {
		return change
	}
	for _, property := range payload.LateProperties {
		if parent, _, _ := strings.Cut(property, ":property:"); parent != source.ParentSourceID {
			continue
		}
		if change.Cells == nil {
			change.Cells = map[string]bool{}
		}
		change.Cells[property], change.Changed = true, true
	}
	return change
}

// notionImportViewShowsLate says whether a view an earlier run made shows or
// sorts by a property made since, which it takes its Notion settings again
// for.
func notionImportViewShowsLate(payload notionImportJobPayload, source storedNotionSource) bool {
	if len(payload.LateProperties) == 0 {
		return false
	}
	plan := notionImportViewPlan{}
	if json.Unmarshal([]byte(source.Text), &plan) != nil {
		return false
	}
	key := cmpOr(plan.DataSourceKey, source.ParentSourceID)
	settings := struct {
		Properties []struct {
			PropertyID string `json:"property_id"`
		} `json:"properties"`
	}{}
	_ = json.Unmarshal(plan.Configuration, &settings)
	sorts := []struct {
		Property string `json:"property"`
	}{}
	_ = json.Unmarshal(plan.Sorts, &sorts)
	shown := map[string]bool{}
	for _, property := range settings.Properties {
		shown[notionImportPropertyKey(property.PropertyID)] = true
	}
	for _, sort := range sorts {
		shown[notionImportPropertyKey(sort.Property)] = true
	}
	for _, property := range payload.LateProperties {
		parent, id, _ := strings.Cut(property, ":property:")
		if parent == key && shown[notionImportPropertyKey(id)] {
			return true
		}
	}
	return false
}

// forgetNotionMigrationViewIssues lets a view a run made again, Notion's
// settings taken again, answer for what this run's making of it lost alone:
// the problems earlier runs left standing for it go (notion_migration_issues.go).
func forgetNotionMigrationViewIssues(app core.App, migrationID, viewSourceID string) error {
	if migrationID == "" {
		return nil
	}
	_, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_notion_migration_issues}}
		WHERE [[migration_id]] = {:migrationId} AND [[kind]] = {:viewParent} AND substr([[issue_id]], 1, length({:prefix})) = {:prefix}
	`).Bind(dbx.Params{"migrationId": migrationID, "viewParent": notionImportViewIssueParentKind, "prefix": viewSourceID + "#"}).Execute()
	return err
}
