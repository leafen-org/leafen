package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// What the source deleted, when the same plan runs again (decision "설계 우선
// Notion 이사" 3절 원칙 4; 2026-10-13: a line changed in a bundle's source, or
// a block deleted in Notion, stayed beside what replaced it). A run again
// takes away a block the source no longer has inside a page it read again,
// as deleting it on screen does — to the page's version history and the
// block trash — unless somebody changed it in Leafen since the run that left
// it, or it holds something the source didn't give; those stay and the
// report counts them. A page, row or database template the source no longer
// gives stays and the report names it: that can mean it moved out of the
// connection's reach or the plan left it out, not only that it was deleted.
//
// To tell, the ledger keeps each block as the run left it — at the run's end,
// after the people mapping and the other restructure steps change mentions
// and the like.

const (
	notionMigrationStepRemove = "remove"
	notionMigrationStepLedger = "ledger"
	// A turn reads this many of the ledger's blocks, and records how the run
	// left this many; the steps go on over turns.
	notionMigrationRemoveWindow = 100
	notionMigrationLedgerWindow = 1000
	// The state of a block the run made that is no longer anywhere.
	notionMigrationBlockGone = "gone"
)

// notionMigrationBlockState is a block as it is: its kind, content, parent
// and text, as one digest.
func notionMigrationBlockState(block storedNotionBlock, document *notionYgoDocument) (string, error) {
	encoded, err := json.Marshal(struct {
		Type     string       `json:"type"`
		Content  string       `json:"content"`
		ParentID string       `json:"parentId"`
		Text     []crdt.Delta `json:"text"`
	}{block.Type, block.Content, block.ParentID, document.doc.GetText(ygoBlockTextRootPrefix + block.ID).ToDelta()})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// recordNotionMigrationBlockStates keeps in the ledger what the run made,
// then how it left each block it made or changed, with the block's source as
// it took it (notion_migration_source_change.go), a window a turn, then
// where it left them (notion_migration_block_place.go); true while more
// remain.
func recordNotionMigrationBlockStates(app core.App, job storedJob, payload *notionImportJobPayload) (bool, error) {
	if !payload.LedgerRecorded {
		payload.LedgerRecorded = true
		return true, recordNotionMigrationLedger(app, payload.MigrationID, job.ID)
	}
	entries := []struct {
		SourceID string `db:"source_id"`
		NativeID string `db:"native_id"`
		PageID   string `db:"page_id"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[source_id]],[[native_id]],[[page_id]] FROM {{leafen_notion_migration_ledger}}
		WHERE [[migration_id]]={:migrationId} AND [[kind]]='block' AND [[state]]=''
		ORDER BY [[page_id]] LIMIT {:limit}
	`).Bind(dbx.Params{"migrationId": payload.MigrationID, "limit": notionMigrationLedgerWindow + 1}).All(&entries); err != nil {
		return false, err
	}
	more := len(entries) > notionMigrationLedgerWindow
	if more {
		entries = entries[:notionMigrationLedgerWindow]
	}
	documents, documentsPage := map[string]*notionYgoDocument{}, ""
	for _, entry := range entries {
		// The window comes a page at a time; one page's text is kept at once.
		if entry.PageID != documentsPage {
			documents, documentsPage = map[string]*notionYgoDocument{}, entry.PageID
		}
		state, pageID := notionMigrationBlockGone, entry.PageID
		if err := keepNotionMigrationBlockSource(app, job, payload.MigrationID, entry.SourceID, entry.NativeID); err != nil {
			return false, err
		}
		block, found, err := loadStoredNotionBlock(app, entry.NativeID)
		if err != nil {
			return false, err
		}
		if found {
			document, err := loadNotionYgoDocument(app, block.PageID, documents)
			if err != nil {
				return false, err
			}
			if state, err = notionMigrationBlockState(block, document); err != nil {
				return false, err
			}
			pageID = block.PageID
		}
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_notion_migration_ledger}} SET [[state]]={:state},[[page_id]]={:pageId}
			WHERE [[migration_id]]={:migrationId} AND [[source_id]]={:sourceId} AND [[kind]]='block'
		`).Bind(dbx.Params{
			"state": state, "pageId": pageID, "migrationId": payload.MigrationID, "sourceId": entry.SourceID,
		}).Execute(); err != nil {
			return false, err
		}
	}
	if more {
		return true, nil
	}
	return recordNotionMigrationBlockPlaces(app, job, payload)
}

// notionMigrationLedgerBlock is a block in a migration's ledger, and
// whether this run's source still has it.
type notionMigrationLedgerBlock struct {
	SourceID string `db:"source_id"`
	NativeID string `db:"native_id"`
	State    string `db:"state"`
	Present  bool   `db:"present"`
}

// removeNotionMigrationDeletedBlocks takes away, a window of the ledger a
// turn, the blocks the source deleted; true while more of the ledger remains.
func (r *durableJobRunner) removeNotionMigrationDeletedBlocks(
	ctx context.Context, job storedJob, payload *notionImportJobPayload,
) (bool, error) {
	if r.ygo == nil {
		return false, errNotionMigrationRestructure
	}
	if payload.RemoveCursor == "" {
		// The report counts this run's.
		if err := updateNotionMigrationRunReport(r.app, payload.MigrationID, func(report *notionMigrationRunReport) {
			report.BlocksRemoved, report.BlocksKept = 0, 0
		}); err != nil {
			return false, err
		}
	}
	entries := []notionMigrationLedgerBlock{}
	if err := r.app.DB().NewQuery(`
		SELECT [[ledger]].[[source_id]],[[ledger]].[[native_id]],[[ledger]].[[state]],
			EXISTS(SELECT 1 FROM {{leafen_notion_import_sources}} AS [[source]]
				WHERE [[source]].[[job_id]]={:jobId} AND [[source]].[[source_id]]=[[ledger]].[[source_id]]) AS [[present]]
		FROM {{leafen_notion_migration_ledger}} AS [[ledger]]
		WHERE [[ledger]].[[migration_id]]={:migrationId} AND [[ledger]].[[source_id]]>{:cursor} AND [[ledger]].[[kind]]='block'
		ORDER BY [[ledger]].[[source_id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"jobId": job.ID, "migrationId": payload.MigrationID, "cursor": payload.RemoveCursor,
		"limit": notionMigrationRemoveWindow,
	}).All(&entries); err != nil {
		return false, err
	}
	removed, kept := 0, 0
	for _, entry := range entries {
		payload.RemoveCursor = entry.SourceID
		// A database's block is named after its database, which the source
		// gives as a database.
		if entry.Present || strings.HasSuffix(entry.SourceID, ":block") {
			continue
		}
		block, found, err := loadStoredNotionBlock(r.app, entry.NativeID)
		if err != nil {
			return false, err
		}
		if !found {
			// Gone already, by a person or with a block that held it.
			if err := forgetNotionMigrationBlocks(r.app, job.ID, payload.MigrationID, []string{entry.SourceID}); err != nil {
				return false, err
			}
			continue
		}
		// A block on a page the source didn't give again goes as its page
		// does; the report names the page. Nor does a page whose reading
		// Notion refused in part say what the source deleted.
		reread, err := notionMigrationPageRead(r.app, job.ID, payload.MigrationID, block.PageID)
		if err != nil {
			return false, err
		}
		if !reread {
			continue
		}
		refused, err := notionMigrationPageRefused(r.app, job.ID, payload.MigrationID, block.PageID)
		if err != nil {
			return false, err
		}
		if refused {
			continue
		}
		taken, removable, err := notionMigrationRemovable(r.app, job.ID, payload.MigrationID, block, entry,
			map[string]*notionYgoDocument{})
		if err != nil {
			return false, err
		}
		if !removable {
			kept++
			continue
		}
		_, err = r.ygo.applyContentGraphMutationContext(ctx, job.Owner, block.PageID, graphMutationInput{
			MutationID: "notion-migration-remove-" + job.ID + "-" + block.ID,
			Operations: []graphOperation{{Kind: graphOperationKindDeleteBlock, Deletion: &graphBlockDelete{BlockID: block.ID}}},
		}, contentGraphMutationHooks{
			Authorize: func(txApp core.App) (pageAuthorization, error) {
				return authorizePageBody(txApp, job.Owner, block.PageID, permissionWorkspaceWrite)
			},
			BeforeApplyOperations: captureRecoverableBlockTrash(block.ID, job.Owner),
		})
		if isPermissionDenied(err) {
			// The person can't change the page now; the block stays.
			kept++
			continue
		}
		if err != nil {
			return false, err
		}
		if err := forgetNotionMigrationBlocks(r.app, job.ID, payload.MigrationID, taken); err != nil {
			return false, err
		}
		removed += len(taken)
	}
	if removed > 0 || kept > 0 {
		if err := updateNotionMigrationRunReport(r.app, payload.MigrationID, func(report *notionMigrationRunReport) {
			report.BlocksRemoved += removed
			report.BlocksKept += kept
		}); err != nil {
			return false, err
		}
	}
	return len(entries) == notionMigrationRemoveWindow, nil
}

// notionMigrationPageRead reports whether this run's source gave again the
// page or row a block is on.
func notionMigrationPageRead(app core.App, jobID, migrationID, pageID string) (bool, error) {
	var read bool
	err := app.DB().NewQuery(`
		SELECT EXISTS(
			SELECT 1 FROM {{leafen_notion_migration_ledger}} AS [[ledger]]
			JOIN {{leafen_notion_import_sources}} AS [[source]]
				ON [[source]].[[job_id]]={:jobId} AND [[source]].[[source_id]]=[[ledger]].[[source_id]]
			WHERE [[ledger]].[[migration_id]]={:migrationId} AND [[ledger]].[[native_id]]={:pageId}
				AND [[ledger]].[[kind]] IN ('page','row'))
	`).Bind(dbx.Params{"jobId": jobID, "migrationId": migrationID, "pageId": pageID}).Row(&read)
	return read, err
}

// notionMigrationPageRefused reports whether this run couldn't read all of the
// page or row a block is on: the page's or a block's children, or a block.
func notionMigrationPageRefused(app core.App, jobID, migrationID, pageID string) (bool, error) {
	var refused bool
	err := app.DB().NewQuery(`
		SELECT EXISTS(
			SELECT 1 FROM {{leafen_notion_migration_ledger}} AS [[ledger]]
			JOIN {{leafen_notion_import_sources}} AS [[issue]]
				ON [[issue]].[[job_id]]={:jobId} AND [[issue]].[[source_id]]=[[ledger]].[[source_id]] || '#' || {:reason}
			WHERE [[ledger]].[[migration_id]]={:migrationId}
				AND (([[ledger]].[[kind]] IN ('page','row') AND [[ledger]].[[native_id]]={:pageId})
					OR ([[ledger]].[[kind]]='block' AND [[ledger]].[[page_id]]={:pageId})))
	`).Bind(dbx.Params{
		"jobId": jobID, "migrationId": migrationID, "pageId": pageID, "reason": notionImportIssueInvalidBlock,
	}).Row(&refused)
	return refused, err
}

// notionMigrationRemovable reports whether a block the source deleted can go
// with all inside it — each a block the run moved that the source deleted
// too and nobody changed in Leafen since — and the source IDs of them all.
func notionMigrationRemovable(
	app core.App, jobID, migrationID string, block storedNotionBlock, entry notionMigrationLedgerBlock,
	documents map[string]*notionYgoDocument,
) ([]string, bool, error) {
	// A block whose earlier state the ledger doesn't know stays.
	if entry.Present || entry.State == "" || entry.State == notionMigrationBlockGone {
		return nil, false, nil
	}
	document, err := loadNotionYgoDocument(app, block.PageID, documents)
	if err != nil {
		return nil, false, err
	}
	state, err := notionMigrationBlockState(block, document)
	if err != nil || state != entry.State {
		return nil, false, err
	}
	taken := []string{entry.SourceID}
	children := []storedNotionBlock{}
	if err := app.DB().NewQuery(`
		SELECT [[id]],[[owner]],[[workspace_id]],[[page_id]],[[parent_id]],[[position]],
			[[type]],[[content]],[[created_by]],[[last_edited_by]],[[created]],[[updated]]
		FROM {{leafen_blocks}} WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[parent_id]]={:id}
	`).Bind(dbx.Params{"owner": block.Owner, "pageId": block.PageID, "id": block.ID}).All(&children); err != nil {
		return nil, false, err
	}
	for _, child := range children {
		childEntry := notionMigrationLedgerBlock{}
		err := app.DB().NewQuery(`
			SELECT [[ledger]].[[source_id]],[[ledger]].[[native_id]],[[ledger]].[[state]],
				EXISTS(SELECT 1 FROM {{leafen_notion_import_sources}} AS [[source]]
					WHERE [[source]].[[job_id]]={:jobId} AND [[source]].[[source_id]]=[[ledger]].[[source_id]]) AS [[present]]
			FROM {{leafen_notion_migration_ledger}} AS [[ledger]]
			WHERE [[ledger]].[[migration_id]]={:migrationId} AND [[ledger]].[[native_id]]={:id} AND [[ledger]].[[kind]]='block'
		`).Bind(dbx.Params{"jobId": jobID, "migrationId": migrationID, "id": child.ID}).One(&childEntry)
		if errors.Is(err, sql.ErrNoRows) {
			// Something the run didn't move as a block: added in Leafen, or a
			// page.
			return nil, false, nil
		}
		if err != nil {
			return nil, false, err
		}
		inside, removable, err := notionMigrationRemovable(app, jobID, migrationID, child, childEntry, documents)
		if err != nil || !removable {
			return nil, false, err
		}
		taken = append(taken, inside...)
	}
	return taken, true, nil
}

// forgetNotionMigrationBlocks takes blocks out of a migration's ledger, and
// out of the run's job, which started from the ledger and gives it back what
// it holds at the run's end.
func forgetNotionMigrationBlocks(app core.App, jobID, migrationID string, sourceIDs []string) error {
	encoded, err := json.Marshal(sourceIDs)
	if err != nil {
		return err
	}
	params := dbx.Params{"jobId": jobID, "migrationId": migrationID, "sourceIds": string(encoded)}
	if _, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_notion_import_mappings}}
		WHERE [[job_id]]={:jobId} AND [[kind]]='block'
			AND [[source_id]] IN (SELECT [[value]] FROM json_each({:sourceIds}))
	`).Bind(params).Execute(); err != nil {
		return err
	}
	_, err = app.DB().NewQuery(`
		DELETE FROM {{leafen_notion_migration_ledger}}
		WHERE [[migration_id]]={:migrationId} AND [[kind]]='block'
			AND [[source_id]] IN (SELECT [[value]] FROM json_each({:sourceIds}))
	`).Bind(params).Execute()
	return err
}

// notionMigrationNotGiven is a page, row or database template an earlier run
// moved that a run's source didn't give. A template is named with its
// database: a mention of a template's page shows no name.
type notionMigrationNotGiven struct {
	PageID   string `db:"page_id"`
	Template bool   `db:"template"`
	Name     string `db:"name"`
	Database string `db:"database"`
}

var notionMigrationNotGivenTemplateText = map[string]string{
	accountLanguageKo: "%s — %s의 템플릿",
	accountLanguageEn: "%s — a template of %s",
}

// mention names it in a report in the person's language.
func (item notionMigrationNotGiven) mention(language string) string {
	if !item.Template {
		return fmt.Sprintf(`<mention-page url="%s"/>`, item.PageID)
	}
	words, known := notionMigrationNotGivenTemplateText[language]
	if !known {
		words, language = notionMigrationNotGivenTemplateText[accountLanguageEn], accountLanguageEn
	}
	name := strings.TrimSpace(item.Name)
	if name == "" {
		name = notionMigrationFormatsReportText[language]["untitled"]
	}
	if item.Database == "" {
		return name
	}
	return fmt.Sprintf(words, name, fmt.Sprintf(`<mention-page url="%s"/>`, item.Database))
}

// notionMigrationNotGivenGroupText says many of them at once: the pages and
// rows, and the templates.
var notionMigrationNotGivenGroupText = map[string]map[string]string{
	accountLanguageKo: {"pages": "페이지·행 %d개", "templates": "데이터베이스 템플릿 %d개"},
	accountLanguageEn: {"pages": "%d pages and rows", "templates": "%d database templates"},
}

// notionMigrationNotGivenGroups are them as the report names them: the pages
// and rows, then the templates, each kind folded under its count.
func notionMigrationNotGivenGroups(language string, notGiven []notionMigrationNotGiven) []notionMigrationReportGroup {
	text := notionMigrationNotGivenGroupText[language]
	if text == nil {
		text = notionMigrationNotGivenGroupText[accountLanguageEn]
	}
	grouping := newNotionMigrationReportGrouping()
	for _, item := range notGiven {
		grouping.add(map[bool]string{true: "templates", false: "pages"}[item.Template], item.mention(language), 1)
	}
	return grouping.list([]string{"pages", "templates"}, func(group *notionMigrationReportGroup) {
		group.Say = fmt.Sprintf(text[group.Key], group.Count)
		if group.Total == 1 {
			group.One = group.Items[0]
		}
	})
}

// notionMigrationPagesNotGiven lists the pages, rows and database templates
// an earlier run moved that this run's source didn't give, still in Leafen
// and out of the trash. A template Notion still gives is one the run read
// again or left as it was (notion_import_counted.go).
func notionMigrationPagesNotGiven(app core.App, jobID, migrationID string) ([]notionMigrationNotGiven, error) {
	pages := []notionMigrationNotGiven{}
	err := app.DB().NewQuery(`
		SELECT [[page]].[[id]] AS [[page_id]], [[ledger]].[[kind]]={:template} AS [[template]],
			COALESCE([[made]].[[name]], [[page]].[[title]]) AS [[name]], COALESCE([[made]].[[page_id]], '') AS [[database]]
		FROM {{leafen_notion_migration_ledger}} AS [[ledger]]
		JOIN {{leafen_pages}} AS [[page]] ON [[page]].[[id]]=[[ledger]].[[native_id]]
		LEFT JOIN {{leafen_database_templates}} AS [[made]] ON [[ledger]].[[kind]]={:template} AND [[made]].[[id]]=[[ledger]].[[native_id]]
		WHERE [[ledger]].[[migration_id]]={:migrationId}
			AND ([[ledger]].[[kind]] IN ('page','row') OR ([[ledger]].[[kind]]={:template} AND [[ledger]].[[state]]!={:jobId}
				AND NOT EXISTS(SELECT 1 FROM {{leafen_database_toolbar_template_state}} AS [[deleted]]
					WHERE [[deleted]].[[template_id]]=[[ledger]].[[native_id]] AND [[deleted]].[[deleted_at]]!='')))
			AND [[page]].[[in_trash]]=0 AND [[page]].[[notion_import_staging]]=0
			AND NOT EXISTS(SELECT 1 FROM {{leafen_notion_import_sources}} AS [[source]]
				WHERE [[source]].[[job_id]]={:jobId} AND [[source]].[[source_id]]=[[ledger]].[[source_id]])
		ORDER BY [[page]].[[created]], [[page]].[[id]]
	`).Bind(dbx.Params{"jobId": jobID, "migrationId": migrationID, "template": notionMigrationLedgerTemplate}).All(&pages)
	return pages, err
}
