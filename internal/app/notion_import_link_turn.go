package app

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The link phase runs in bounded turns after the apply phase, in staging
// order: a database's relation and rollup properties come before its rows,
// so a row's related rows are set once its relation property exists. Views
// come last, in a pass of their own: a view in a row's body may show a database
// the import came to after that row, and may name its relations and rollups.

// notionImportLinkScanItems bounds how many staged sources one link turn
// reads while looking for the next ones to link.
const notionImportLinkScanItems = 512

// notionImportLinkSourceSQL matches the staged sources that may need linking
// before views: relation and rollup properties and rows with related pages.
// A row of a database with a property its migration's plan turns into
// objects (notion_migration_objects.go) is linked too: {:objectDatabases};
// so is a row a run again finds in its migration's ledger with related pages,
// which may hold none now (notion_migration_relation_pairs.go): {:migrationId}.
const notionImportLinkSourceSQL = `(([[kind]]='property' AND (instr([[text]], '"type":"relation"')>0 ` +
	`OR instr([[text]], '"type":"rollup"')>0)) OR ([[kind]]='row' AND (instr([[text]], '"type":"relation"')>0 ` +
	`OR [[parent_source_id]] IN (SELECT [[value]] FROM json_each({:objectDatabases})) ` +
	`OR EXISTS (SELECT 1 FROM {{leafen_notion_migration_ledger}} AS [[before]] WHERE [[before]].[[migration_id]]={:migrationId} ` +
	`AND [[before]].[[source_id]]={{leafen_notion_import_sources}}.[[source_id]] AND [[before]].[[kind]]='row' ` +
	`AND instr([[before]].[[state]], '"type":"relation"')>0))))`

const notionImportLinkViewSQL = `[[kind]]='view'`

// notionImportHasLinks reports whether an import staged anything to link.
func notionImportHasLinks(app core.App, jobID, migrationID string, run *notionMigrationRunPlan) (bool, error) {
	objects, err := run.objectDatabases(app, jobID)
	if err != nil {
		return false, err
	}
	var found int
	err = app.DB().NewQuery(`
		SELECT EXISTS (
			SELECT 1 FROM {{leafen_notion_import_sources}}
			WHERE [[job_id]]={:jobId} AND (` + notionImportLinkSourceSQL + ` OR ` + notionImportLinkViewSQL + `)
		)
	`).Bind(dbx.Params{"jobId": jobID, "objectDatabases": objects, "migrationId": migrationID}).Row(&found)
	return found == 1, err
}

type notionImportLinkWindow struct {
	Sources []storedNotionSource
	Next    int64
	HasMore bool
}

func loadNotionImportLinkWindow(app core.App, jobID, migrationID string, cursor int64, views bool, run *notionMigrationRunPlan) (notionImportLinkWindow, error) {
	window := notionImportLinkWindow{Next: cursor}
	matches := notionImportLinkSourceSQL
	if views {
		matches = notionImportLinkViewSQL
	}
	objects, err := run.objectDatabases(app, jobID)
	if err != nil {
		return window, err
	}
	scanned := struct {
		End   int64 `db:"scanned_end"`
		Count int   `db:"scanned_count"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT COALESCE(MAX([[sequence]]), 0) AS [[scanned_end]], COUNT(*) AS [[scanned_count]] FROM (
			SELECT [[sequence]] FROM {{leafen_notion_import_sources}}
			WHERE [[job_id]]={:jobId} AND [[sequence]]>{:cursor}
			ORDER BY [[sequence]] LIMIT {:scan}
		) AS [[scanned]]
	`).Bind(dbx.Params{"jobId": jobID, "cursor": cursor, "scan": notionImportLinkScanItems}).One(&scanned); err != nil {
		return window, err
	}
	if scanned.Count == 0 {
		return window, nil
	}
	candidates := []storedNotionSource{}
	if err := app.DB().NewQuery(`
		SELECT [[sequence]], [[source_id]], [[kind]], [[parent_source_id]], [[parent_kind]], [[position]], [[title]], [[text]]
		FROM {{leafen_notion_import_sources}}
		WHERE [[job_id]]={:jobId} AND [[sequence]]>{:cursor} AND [[sequence]]<={:end}
			AND ` + matches + `
		ORDER BY [[sequence]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"jobId": jobID, "cursor": cursor, "end": scanned.End, "limit": notionImportApplyItems + 1, "objectDatabases": objects,
		"migrationId": migrationID,
	}).All(&candidates); err != nil {
		return window, err
	}
	window.Next, window.HasMore = scanned.End, scanned.Count == notionImportLinkScanItems
	bytes := 0
	for index, source := range candidates {
		bytes += len(source.Text)
		if index == notionImportApplyItems || index > 0 && bytes > maxGraphChangeBytes {
			window.Next, window.HasMore = candidates[index-1].Sequence, true
			break
		}
		window.Sources = append(window.Sources, source)
	}
	return window, nil
}

func (r *durableJobRunner) linkNotionImportChunk(job storedJob, payload notionImportJobPayload) error {
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, "")
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		if _, err := authorizeNotionImportTurn(txApp, current, payload); err != nil {
			return err
		}
		run, err := loadNotionMigrationRunPlan(txApp, current.Owner, payload)
		if err != nil {
			return err
		}
		window, err := loadNotionImportLinkWindow(txApp, current.ID, payload.MigrationID, payload.LinkCursor, payload.LinkingViews, run)
		if err != nil {
			return err
		}
		linker := &notionImportLinker{
			app: txApp, job: current, payload: payload,
			resolver: &notionImportReferenceResolver{app: txApp, jobID: current.ID, resolved: map[string]string{}},
			mappings: &notionApplyMappings{nativeIDs: map[notionMappingKey]string{}},
			linked:   map[string]string{}, staged: map[string]*notionDatabasePropertyPlan{},
			pages: map[string]string{}, documents: map[string]*structuralMutationBatch{},
			views: map[string]*notionImportViewPlan{}, converters: map[string]*notionImportViewConverter{},
			run: run,
		}
		for _, source := range window.Sources {
			// What the run left out isn't linked (notion_import_left_out.go).
			if slices.Contains(payload.LeftOut, source.SourceID) || slices.Contains(payload.LeftOut, source.ParentSourceID) {
				continue
			}
			switch source.Kind {
			case "property":
				// A split database's goes to the targets of its rows too (notion_migration_split_linked.go).
				if err = linker.property(source); err == nil {
					err = linker.splitLinkedColumns(source)
				}
			case "row":
				err = linker.row(source)
			case "view":
				err = linker.view(source)
			}
			if err != nil {
				return notionImportItemAt(&source, err)
			}
		}
		if err := saveStructuralMutationBatches(txApp, job.Owner, linker.documents); err != nil {
			return err
		}
		if err := linker.mappings.flush(txApp, current.ID); err != nil {
			return err
		}
		payload.LinkCursor = window.Next
		switch {
		case window.HasMore:
		case payload.LinkingViews:
			payload.Linking, payload.Publishing = false, true
		default:
			payload.LinkingViews, payload.LinkCursor = true, 0
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		chunkIndex := current.ChunkCount + 1
		if err := recordNotionImportChunk(
			txApp, current.ID, chunkIndex, len(window.Sources), fmt.Sprint(window.Next),
			notionApplyDigest(window.Sources), now,
		); err != nil {
			return err
		}
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[payload_json]] = {:payload}, [[chunk_count]] = {:chunkCount},
				[[not_before]] = {:notBefore}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[status]] = 'queued'
		`).Bind(dbx.Params{
			"id": current.ID, "payload": string(encoded), "chunkCount": chunkIndex,
			"notBefore": now.Add(durableJobReadyDelay).UnixMilli(), "updated": now.UnixMilli(),
		}).Execute()
		return err
	})
}
