package app

import (
	"cmp"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type notionPublicationPage struct {
	PageID string `db:"native_id"`
}

func (r *durableJobRunner) publishNotionImportChunk(job storedJob, payload notionImportJobPayload) error {
	return r.app.RunInTransaction(func(txApp core.App) error {
		current, err := loadStoredJob(txApp, job.ID, "")
		if err != nil || current.Status != durableJobStatusQueued {
			return err
		}
		if _, err := authorizeNotionImportTurn(txApp, current, payload); err != nil {
			return err
		}
		pages := make([]notionPublicationPage, 0, notionImportApplyItems+1)
		err = txApp.DB().NewQuery(`
			SELECT [[native_id]]
			FROM {{leafen_notion_import_mappings}}
			WHERE [[job_id]] = {:jobId} AND [[kind]] IN ('page', 'row') AND [[native_id]] > {:cursor}
			ORDER BY [[native_id]]
			LIMIT {:limit}
		`).Bind(dbx.Params{
			"jobId": current.ID, "cursor": payload.PublicationCursor, "limit": notionImportApplyItems + 1,
		}).All(&pages)
		if err != nil {
			return err
		}
		hasMore := len(pages) > notionImportApplyItems
		if hasMore {
			pages = pages[:notionImportApplyItems]
		}
		pageRecords, err := loadNotionPublicationPages(txApp, pages)
		if err != nil {
			return err
		}
		dirtyPages := make([]*core.Record, 0, len(pages))
		published := map[string][]string{}
		resolver := &notionImportReferenceResolver{app: txApp, jobID: current.ID, resolved: map[string]string{}}
		for _, item := range pages {
			page, found := pageRecords[item.PageID]
			if !found {
				// One an earlier run made that is gone for good since — a
				// person deleted it, or it went with what held it — stays
				// gone, as a template deleted for good does
				// (notionMigrationTemplateGone); one this run made is here.
				if earlier, err := notionMigrationLedgerNamesPage(txApp, payload.MigrationID, item.PageID); err != nil || !earlier {
					return cmp.Or(err, sql.ErrNoRows)
				}
				continue
			}
			if err := resolveNotionImportPageReferences(txApp, current, payload, item.PageID, resolver); err != nil {
				return err
			}
			// A column whose content didn't come goes, as Notion has none
			// (notion_import_column_tidy.go).
			if err := tidyNotionImportColumns(txApp, current, payload, item.PageID); err != nil {
				return err
			}
			spans := page.GetString("title_spans")
			if err := resolveNotionImportTitleSpans(page, resolver); err != nil {
				return err
			}
			// A page an earlier run published is shown already: a run again
			// leaves it as it is, its times and place too, unless its title
			// took what this run brought (notion_migration_run_again.go). What
			// the run wrote to it and its rows was marked for search as each
			// turn kept it (server_save_search_marks.go).
			if !page.GetBool("notion_import_staging") && page.GetString("title_spans") == spans {
				continue
			}
			page.Set("notion_import_staging", false)
			if err := txApp.Save(page); err != nil {
				return err
			}
			dirtyPages = append(dirtyPages, page)
			// A database row belongs to its data source, not a child-page block.
			if isDatabaseRowPageRecord(page) {
				if err := refreshCreatedPageHistoryBaseline(txApp, page.Id); err != nil {
					return err
				}
				continue
			}
			// The page shows in its parent's body, and its sub-pages in its own.
			if err := requestChildPageBlockBackfill(
				txApp, page.GetString("workspace"), page.GetString("parent_id"), page.Id,
			); err != nil {
				return err
			}
			if parentID := page.GetString("parent_id"); parentID != "" {
				published[parentID] = append(published[parentID], page.Id)
			}
		}
		if err := alignNotionImportSubPages(txApp, published); err != nil {
			return err
		}
		if err := markSearchDirtyRecords(txApp, dirtyPages); err != nil {
			return err
		}
		if len(pages) > 0 {
			payload.PublicationCursor = pages[len(pages)-1].PageID
		}
		if !hasMore {
			// A migration's run then makes its plan's property changes.
			changes, err := notionMigrationRestructures(txApp, current.Owner, payload)
			if err != nil {
				return err
			}
			payload.Restructuring = len(changes) > 0
			payload.StampingRows = !payload.Restructuring
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		chunkIndex := current.ChunkCount + 1
		if err := recordNotionImportChunk(txApp, current.ID, chunkIndex, len(pages), payload.PublicationCursor, notionPublicationDigest(pages), now); err != nil {
			return err
		}
		// The rows' Notion history is set last (notion_import_row_stamps.go),
		// which ends the import.
		status := durableJobStatusQueued
		notBefore := now.Add(durableJobReadyDelay).UnixMilli()
		_, err = txApp.DB().NewQuery(`
			UPDATE {{leafen_jobs}}
			SET [[status]] = {:status}, [[payload_json]] = {:payload},
				[[chunk_count]] = {:chunkCount}, [[not_before]] = {:notBefore}, [[updated]] = {:updated}
			WHERE [[id]] = {:id} AND [[status]] = 'queued'
		`).Bind(dbx.Params{
			"id": current.ID, "status": status, "payload": string(encoded),
			"chunkCount": chunkIndex, "notBefore": notBefore, "updated": now.UnixMilli(),
		}).Execute()
		return err
	})
}

func loadNotionPublicationPages(app core.App, pages []notionPublicationPage) (map[string]*core.Record, error) {
	if len(pages) == 0 {
		return map[string]*core.Record{}, nil
	}
	pageIDs := make([]string, len(pages))
	for index, page := range pages {
		pageIDs[index] = page.PageID
	}
	records, err := app.FindRecordsByIds(pagesCollection, pageIDs)
	if err != nil {
		return nil, err
	}
	recordsByID := make(map[string]*core.Record, len(records))
	for _, record := range records {
		recordsByID[record.Id] = record
	}
	return recordsByID, nil
}

// notionMigrationLedgerNamesPage says whether a migration's earlier run made
// the page or row, as its ledger keeps it; never for an import no migration
// runs.
func notionMigrationLedgerNamesPage(app core.App, migrationID, pageID string) (bool, error) {
	if migrationID == "" {
		return false, nil
	}
	var named bool
	err := app.DB().NewQuery(`
		SELECT EXISTS(SELECT 1 FROM {{leafen_notion_migration_ledger}}
			WHERE [[migration_id]]={:migrationId} AND [[native_id]]={:pageId} AND [[kind]] IN ('page','row'))
	`).Bind(dbx.Params{"migrationId": migrationID, "pageId": pageID}).Row(&named)
	return named, err
}

func notionPublicationDigest(pages []notionPublicationPage) string {
	hasher := sha256.New()
	for _, page := range pages {
		_, _ = fmt.Fprintln(hasher, page.PageID)
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

// alignNotionImportSubPages puts the sub-pages just published in their
// parents' sidebar in body order, as a person's move does
// (child_page_sidebar.go): those hanging on a line, a table row or a caption
// where it stands, those on the title first, and those with blocks inside
// other blocks where the blocks stand. Staged pages took their place among
// the blocks of their own container, or none, and the sidebar reads only
// published ones; each chunk lines up those published so far.
func alignNotionImportSubPages(app core.App, published map[string][]string) error {
	parents := make([]string, 0, len(published))
	for parentID := range published {
		parents = append(parents, parentID)
	}
	sort.Strings(parents)
	for _, parentID := range parents {
		if err := alignMentionAnchoredPages(app, parentID, published[parentID]); err != nil {
			return err
		}
	}
	return nil
}
