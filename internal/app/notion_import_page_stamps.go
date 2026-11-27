package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

// An imported page keeps what Notion recorded of its history, as a row does
// (notion_import_row_stamps.go): when Notion made it and last changed it and,
// where the migration's plan gives the people a workspace member, who did. A
// page's source keeps that beside its presentation; a full-page database's
// page takes its database's. A page's record moves on with every write the
// import makes to it, so the import sets these last, a window of pages a
// turn, once the plan's changes are made and before the rows take their
// people: first the pages, then each row's own page record, whose times
// fetch, search and the page's menus read as a page's.

// notionImportPageHistory is what a page source keeps of its Notion history.
type notionImportPageHistory struct {
	Created      string `json:"created,omitempty"`
	Edited       string `json:"edited,omitempty"`
	CreatedBy    string `json:"createdBy,omitempty"`
	LastEditedBy string `json:"lastEditedBy,omitempty"`
}

// The page stamp walk goes over the pages, then the rows' page records, each
// by native ID; done, it sorts after every cursor.
const (
	notionImportPageStampPages = "page:"
	notionImportPageStampRows  = "row:"
	notionImportPagesStamped   = "~"
)

// notionImportPageSourceText is a page source's text: its presentation with
// its cover, and its Notion history.
func notionImportPageSourceText(object notionProviderObject) string {
	presentation := notionImportObjectPresentation(object, true)
	presentation.History = notionImportObjectHistory(object)
	return encodeNotionImportPresentation(presentation)
}

// notionImportObjectHistory is a Notion page's or database's history, nil
// for none.
func notionImportObjectHistory(object notionProviderObject) *notionImportPageHistory {
	history := notionImportPageHistory{
		CreatedBy: notionProviderUserID(object.CreatedBy), LastEditedBy: notionProviderUserID(object.LastEditedBy),
	}
	if created, err := time.Parse(time.RFC3339, strings.TrimSpace(object.CreatedTime)); err == nil {
		history.Created = created.UTC().Format(time.RFC3339Nano)
	}
	if edited, err := time.Parse(time.RFC3339, strings.TrimSpace(object.LastEditedTime)); err == nil {
		history.Edited = edited.UTC().Format(time.RFC3339Nano)
	}
	if history == (notionImportPageHistory{}) {
		return nil
	}
	return &history
}

// notionImportPageStampOf reads the history a staged page or row carries.
func notionImportPageStampOf(kind, text string) (created time.Time, stamp notionImportRowStamp) {
	if kind == "row" {
		stamp = notionImportRowStampOf(text)
		return stamp.Created, stamp
	}
	stored := notionImportPresentation{}
	if !strings.HasPrefix(text, notionPresentationSourcePrefix) ||
		json.Unmarshal([]byte(strings.TrimPrefix(text, notionPresentationSourcePrefix)), &stored) != nil || stored.History == nil {
		return created, stamp
	}
	created, _ = time.Parse(time.RFC3339Nano, stored.History.Created)
	stamp.Edited, _ = time.Parse(time.RFC3339Nano, stored.History.Edited)
	stamp.CreatedBy, stamp.LastEditedBy = stored.History.CreatedBy, stored.History.LastEditedBy
	return created, stamp
}

// stampNotionImportPagesChunk sets a window of imported pages' Notion
// history, or of rows' page records' times once the pages are done.
func (r *durableJobRunner) stampNotionImportPagesChunk(job storedJob, payload notionImportJobPayload) error {
	return r.app.RunInTransaction(func(txApp core.App) error {
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
		// The kept people's names come before the first page names them.
		if payload.PageStampCursor == "" {
			if err := keepNotionMigrationPeopleNames(txApp, current, payload, runPlan); err != nil {
				return err
			}
		}
		kind, cursor := "page", strings.TrimPrefix(payload.PageStampCursor, notionImportPageStampPages)
		if after, rows := strings.CutPrefix(payload.PageStampCursor, notionImportPageStampRows); rows {
			kind, cursor = "row", after
		}
		items := make([]notionImportRowStampSource, 0, notionImportApplyItems+1)
		if err := txApp.DB().NewQuery(`
			SELECT [[mapping]].[[source_id]], [[mapping]].[[native_id]], [[source]].[[text]]
			FROM {{leafen_notion_import_mappings}} [[mapping]]
			JOIN {{leafen_notion_import_sources}} [[source]]
				ON [[source]].[[job_id]] = [[mapping]].[[job_id]] AND [[source]].[[source_id]] = [[mapping]].[[source_id]]
				AND [[source]].[[kind]] = {:kind}
			WHERE [[mapping]].[[job_id]] = {:jobId} AND [[mapping]].[[kind]] = {:kind} AND [[mapping]].[[native_id]] > {:cursor}
			ORDER BY [[mapping]].[[native_id]]
			LIMIT {:limit}
		`).Bind(dbx.Params{
			"jobId": current.ID, "kind": kind, "cursor": cursor, "limit": notionImportApplyItems + 1,
		}).All(&items); err != nil {
			return err
		}
		hasMore := len(items) > notionImportApplyItems
		if hasMore {
			items = items[:notionImportApplyItems]
		}
		for _, item := range items {
			// A row that stands for a template becomes one with the rows' stamps.
			if kind == "row" {
				if _, template := notionImportRowTemplateOf(item.Text); template {
					continue
				}
			}
			created, stamp := notionImportPageStampOf(kind, item.Text)
			// A run again keeps what has happened here since to the pages
			// Notion didn't change (notion_migration_run_again.go).
			if kept, err := notionMigrationKeepsStamp(txApp, payload, item.RowID, stamp.Edited); err != nil || kept {
				if err != nil {
					return notionImportAt(kind, item.RowID, err)
				}
				continue
			}
			if err := stampNotionImportPage(txApp, current.WorkspaceID, item.RowID, kind, created, stamp, runPlan); err != nil {
				return notionImportAt(kind, item.RowID, err)
			}
		}
		switch {
		case hasMore && kind == "page":
			payload.PageStampCursor = notionImportPageStampPages + items[len(items)-1].RowID
		case hasMore:
			payload.PageStampCursor = notionImportPageStampRows + items[len(items)-1].RowID
		case kind == "page":
			payload.PageStampCursor = notionImportPageStampRows
		default:
			payload.PageStampCursor = notionImportPagesStamped
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		chunkIndex := current.ChunkCount + 1
		turn := sha256.Sum256([]byte("notion-page-stamps:" + payload.PageStampCursor))
		if err := recordNotionImportChunk(txApp, current.ID, chunkIndex, len(items), payload.PageStampCursor, hex.EncodeToString(turn[:]), now); err != nil {
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

// stampNotionImportPage gives one page record the times Notion made and last
// changed it (never before it was made, notionImportEditedAt), and a page the members the plan gives its Notion people (a
// row's take theirs with its stamps); a kept Notion person or integration
// stands beside them (notion_imported_authors.go).
func stampNotionImportPage(
	app core.App, workspaceID, pageID, kind string, created time.Time, stamp notionImportRowStamp, run *notionMigrationRunPlan,
) error {
	set, params := []string{}, dbx.Params{"pageId": pageID, "workspaceId": workspaceID}
	if !created.IsZero() {
		set, params["created"] = append(set, "[[created]] = {:created}"), created.UTC().Format(types.DefaultDateLayout)
	}
	if !stamp.Edited.IsZero() {
		edited := notionImportEditedAt(created, stamp.Edited)
		set, params["edited"] = append(set, "[[updated]] = {:edited}"), edited.UTC().Format(types.DefaultDateLayout)
	}
	if kind == "page" {
		if member := run.member(stamp.CreatedBy); member != "" {
			set, params["createdBy"] = append(set, "[[created_by]] = {:createdBy}"), member
		}
		if member := run.member(stamp.LastEditedBy); member != "" {
			set, params["lastEditedBy"] = append(set, "[[last_edited_by]] = {:lastEditedBy}"), member
		}
	}
	if err := stampNotionImportedAuthors(app, workspaceID, pageID, stamp, run); err != nil || len(set) == 0 {
		return err
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_pages}} SET ` + strings.Join(set, ", ") + `
		WHERE [[id]] = {:pageId} AND [[workspace]] = {:workspaceId}
	`).Bind(params).Execute()
	return err
}
