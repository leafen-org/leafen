package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// An imported row keeps what Notion recorded of its history: the time Notion
// last changed it and, where the migration's plan gives the people a
// workspace member, who made it and who changed it last. Every write the
// import makes to a row — its values, its body, its links, the plan's property
// changes and people — would move these on, so the import sets them last, a
// window of rows a turn, after all of that. A person the plan doesn't map (an
// integration, or someone with no account here) leaves the person importing,
// as the import's issues say.

type notionImportRowStampSource struct {
	SourceID string `db:"source_id"`
	RowID    string `db:"native_id"`
	Text     string `db:"text"`
}

// notionImportRowStamp is what a staged row carries of its Notion history:
// a row's created time is the one it took among the rows of its minute
// (notion_import_row_minutes.go).
type notionImportRowStamp struct {
	Created      time.Time
	Edited       time.Time
	CreatedBy    string
	LastEditedBy string
}

// notionImportEditedAt is the last edit a page or row record keeps: Notion's,
// or its created time where that came later. Notion gives both to the minute,
// and a row made and changed in one minute takes a created time within it,
// past the minute's start its last edit stands at, to keep the order Notion
// lists its rows in; its last edit takes the same time, so it is never before
// it was made and stays in Notion's minute (2026-11-16: every row of an
// 80-row synthetic import, each made and last changed in one minute, was made
// after its last edit). A run again reads Notion's own time
// (notionMigrationChangedSince).
func notionImportEditedAt(created, edited time.Time) time.Time {
	if created.After(edited) {
		return created
	}
	return edited
}

func notionImportRowStampOf(text string) notionImportRowStamp {
	stamp := notionImportRowStamp{}
	values := map[string]notionDatabaseRowValuePlan{}
	if err := json.Unmarshal([]byte(text), &values); err != nil {
		return stamp
	}
	if edited, _ := values[notionImportRowEditedKey].Value.(string); edited != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, edited); err == nil {
			stamp.Edited = parsed
		}
	}
	stamp.Created = notionImportRowCreated(values)
	if authors, _ := values[notionImportRowAuthorsKey].Value.(map[string]any); authors != nil {
		stamp.CreatedBy, _ = authors["createdBy"].(string)
		stamp.LastEditedBy, _ = authors["lastEditedBy"].(string)
	}
	return stamp
}

// stampNotionImportRowsChunk sets a window of imported rows' Notion history,
// and ends the import once every row has it and the templates it made have
// their copies of their bodies' databases (notion_import_template_copies_wait.go).
func (r *durableJobRunner) stampNotionImportRowsChunk(job storedJob, payload notionImportJobPayload) error {
	terminal, reportBehind, copying := false, false, false
	err := r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, "")
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		if _, err := authorizeNotionImportTurn(txApp, current, payload); err != nil {
			return err
		}
		runPlan, err := loadNotionMigrationRunPlan(txApp, current.Owner, payload)
		if err != nil {
			return err
		}
		rows := make([]notionImportRowStampSource, 0, notionImportApplyItems+1)
		if err := txApp.DB().NewQuery(`
			SELECT [[mapping]].[[source_id]], [[mapping]].[[native_id]], [[source]].[[text]]
			FROM {{leafen_notion_import_mappings}} [[mapping]]
			JOIN {{leafen_notion_import_sources}} [[source]]
				ON [[source]].[[job_id]] = [[mapping]].[[job_id]] AND [[source]].[[source_id]] = [[mapping]].[[source_id]]
				AND [[source]].[[kind]] = 'row'
			WHERE [[mapping]].[[job_id]] = {:jobId} AND [[mapping]].[[kind]] = 'row' AND [[mapping]].[[native_id]] > {:cursor}
			ORDER BY [[mapping]].[[native_id]]
			LIMIT {:limit}
		`).Bind(dbx.Params{
			"jobId": current.ID, "cursor": payload.StampCursor, "limit": notionImportApplyItems + 1,
		}).All(&rows); err != nil {
			return err
		}
		hasMore := len(rows) > notionImportApplyItems
		if hasMore {
			rows = rows[:notionImportApplyItems]
		}
		passed := 0
		for _, row := range rows {
			if template, standsFor := notionImportRowTemplateOf(row.Text); standsFor {
				named, err := makeNotionImportTemplate(txApp, current, payload, row.SourceID, row.RowID, template)
				if errors.Is(err, errNotionImportPassedBy) {
					passed++
				} else if err != nil {
					return notionImportAt("row", row.RowID, err)
				}
				payload.ReportNamedTemplateRow = payload.ReportNamedTemplateRow || named
				continue
			}
			stamp := notionImportRowStampOf(row.Text)
			// A run again keeps what has happened here since to the rows Notion
			// didn't change (notion_migration_run_again.go).
			if kept, err := notionMigrationKeepsStamp(txApp, payload, row.RowID, stamp.Edited); err != nil || kept {
				if err != nil {
					return notionImportAt("row", row.RowID, err)
				}
				continue
			}
			if err := stampNotionImportRow(txApp, current.WorkspaceID, row.RowID, current.Owner, stamp, runPlan); err != nil {
				return notionImportAt("row", row.RowID, err)
			}
		}
		if err := countNotionMigrationChangedMeanwhile(txApp, payload.MigrationID, passed); err != nil {
			return err
		}
		if len(rows) > 0 {
			payload.StampCursor = rows[len(rows)-1].RowID
		}
		terminal = !hasMore
		if terminal {
			if copying, err = notionImportTemplateCopiesPending(txApp, current.ID); err != nil {
				return err
			}
			terminal = !copying
		}
		if terminal {
			if reportBehind, err = finishNotionMigrationRun(txApp, payload, current.ID); err != nil {
				return err
			}
			// A row it named is its template now (notion_migration_issues.go).
			reportBehind = reportBehind || payload.ReportNamedTemplateRow
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		chunkIndex := current.ChunkCount
		// A look at the copies alone is no turn of the step's own.
		if len(rows) > 0 || !copying {
			chunkIndex++
			turn := sha256.Sum256([]byte("notion-row-stamps:" + payload.StampCursor))
			if err := recordNotionImportChunk(txApp, current.ID, chunkIndex, len(rows), payload.StampCursor, hex.EncodeToString(turn[:]), now); err != nil {
				return err
			}
		}
		status, notBefore := durableJobStatusCompleted, now.UnixMilli()
		if copying {
			status, notBefore = durableJobStatusQueued, now.Add(notionImportTemplateCopiesWait).UnixMilli()
		} else if !terminal {
			status, notBefore = durableJobStatusQueued, now.Add(durableJobReadyDelay).UnixMilli()
		}
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[status]] = {:status}, [[payload_json]] = {:payload}, [[chunk_count]] = {:chunkCount},
				[[not_before]] = {:notBefore}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[status]] = 'queued'
		`).Bind(dbx.Params{
			"id": current.ID, "status": status, "payload": string(encoded), "chunkCount": chunkIndex,
			"notBefore": notBefore, "updated": now.UnixMilli(),
		}).Execute()
		return err
	})
	if err == nil && terminal {
		notifyNotionImportRetentionAfterCommit(r.app)
	}
	// A report written before the last step says what it did too, no
	// longer says the run stopped, and names the template a row it named
	// became; the run has ended either way.
	if err == nil && reportBehind {
		if reportErr := r.writeNotionMigrationReport(context.Background(), r.app, job, payload); reportErr != nil {
			logDurableJobDiagnostic(r.app, errorCodeNotionMigrationFailed, newDiagnosticReference(), job.ID, "notion-migration.report", "after-finish", reportErr)
		}
	}
	return err
}

// stampNotionImportRow gives one row its Notion edit time (never before it
// was made, notionImportEditedAt) and the members the plan gives its Notion
// people; one it gives none was last changed by the person importing, as the
// row the import made — the import's writes to a row there before leave who
// last changed it (applyNotionGraphOperationsWith).
func stampNotionImportRow(app core.App, workspaceID, rowID, importer string, stamp notionImportRowStamp, run *notionMigrationRunPlan) error {
	if !stamp.Edited.IsZero() {
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_database_row_pages}} SET [[updated]] = {:edited}
			WHERE [[row_id]] = {:rowId} AND [[workspace_id]] = {:workspaceId}
		`).Bind(dbx.Params{
			"edited": notionImportEditedAt(stamp.Created, stamp.Edited).UnixMilli(), "rowId": rowID, "workspaceId": workspaceID,
		}).Execute(); err != nil {
			return err
		}
	}
	set, params := []string{}, dbx.Params{"rowId": rowID, "workspaceId": workspaceID}
	if member := run.member(stamp.CreatedBy); member != "" {
		set, params["createdBy"] = append(set, "[[created_by]] = {:createdBy}"), member
	}
	if member := cmpOr(run.member(stamp.LastEditedBy), importer); member != "" {
		set, params["lastEditedBy"] = append(set, "[[last_edited_by]] = {:lastEditedBy}"), member
	}
	if len(set) == 0 {
		return nil
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_pages}} SET ` + strings.Join(set, ", ") + `
		WHERE [[id]] = {:rowId} AND [[workspace]] = {:workspaceId}
	`).Bind(params).Execute()
	return err
}

// notionImportRowsAuthoredByImporter counts the pages and rows a run read
// (notionImportAuthoredSQL) that a Notion person or integration the plan
// doesn't map made or last changed, which keep the person importing (a
// page's are its source's history, notion_import_page_stamps.go).
func notionImportRowsAuthoredByImporter(app core.App, jobID, migrationID string, people []notionMigrationPlanPerson) (int, error) {
	mapped := make(map[string]bool, len(people))
	for _, person := range people {
		if strings.TrimSpace(person.MemberID) != "" {
			mapped[strings.TrimSpace(person.NotionUserID)] = true
		}
	}
	authors := []struct {
		CreatedBy    string `db:"created_by"`
		LastEditedBy string `db:"last_edited_by"`
	}{}
	if err := app.DB().NewQuery(notionImportAuthoredSQL).
		Bind(notionImportCountedParams(dbx.Params{"jobId": jobID, "migrationId": migrationID})).All(&authors); err != nil {
		return 0, err
	}
	count := 0
	for _, row := range authors {
		if row.CreatedBy != "" && !mapped[row.CreatedBy] || row.LastEditedBy != "" && !mapped[row.LastEditedBy] {
			count++
		}
	}
	return count, nil
}
