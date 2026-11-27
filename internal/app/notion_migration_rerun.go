package app

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/reearth/ygo/crdt"
)

// The same plan run again (decision "설계 우선 Notion 이사" 3절 원칙 4): a
// migration keeps a ledger of what each Notion object it moved became, past
// the import job's own records. A run after the first starts from it, so the
// engine passes by what it already made and brings what is new; a row or a
// block Notion changed since the last run started takes Notion's values or
// text again, and a page Notion renamed takes its new title. A block the
// source deleted goes, unless somebody changed it in Leafen since; a page or
// row it no longer gives stays, named in the report
// (notion_migration_removed.go).

// The ledger also keeps each comment a run moved, by its source's ID, so a
// run again doesn't move it twice; comments aren't in the job's mappings.
const notionMigrationLedgerComment = "comment"

// recordNotionMigrationLedger keeps what a run's job made. A block keeps
// what the ledger knew of it; one new to it waits for the run's end to say
// how the run left it.
func recordNotionMigrationLedger(app core.App, migrationID, jobID string) error {
	// A window of mappings at a time (notion_migration_ledger_window.go).
	if err := recordNotionMigrationLedgerMappings(app, migrationID, jobID); err != nil {
		return err
	}
	// Each data source and view it made by its own Notion ID, for links and
	// mentions of it (notion_address_moved_ledger.go).
	if err := recordNotionMigrationLedgerDataSources(app, migrationID, jobID); err != nil {
		return err
	}
	// What the bodies of the templates it read hold, for a run again that
	// leaves them as they are (notion_import_counted.go).
	if err := recordNotionImportTemplateBodies(app, migrationID, jobID); err != nil {
		return err
	}
	// A page, a database and a property keep the name Notion gave them, to tell
	// later whether Notion's changed. Each of these goes a window of the
	// ledger's entries at a time too.
	err := eachNotionMigrationLedgerEntries(app, migrationID, []string{"page", "data_source", "property"}, func(entries string) error {
		_, err := app.DB().NewQuery(`
			UPDATE {{leafen_notion_migration_ledger}} SET [[state]]=COALESCE((
				SELECT [[source]].[[title]] FROM {{leafen_notion_import_sources}} AS [[source]]
				WHERE [[source]].[[job_id]]={:jobId} AND [[source]].[[source_id]]={{leafen_notion_migration_ledger}}.[[source_id]]
					AND [[source]].[[kind]]=CASE {{leafen_notion_migration_ledger}}.[[kind]] WHEN 'data_source' THEN 'database' ELSE {{leafen_notion_migration_ledger}}.[[kind]] END
			), [[state]])
			WHERE [[migration_id]]={:migrationId} AND ` + notionMigrationLedgerEntriesSQL + `
		`).Bind(dbx.Params{"migrationId": migrationID, "jobId": jobID, "entries": entries}).Execute()
		return err
	})
	if err != nil {
		return err
	}
	// A view and a row keep what the run staged of them, to tell later
	// whether the source changed them (notion_import_view_links.go,
	// notion_migration_source_change.go).
	err = eachNotionMigrationLedgerEntries(app, migrationID, []string{"view", "row"}, func(entries string) error {
		_, err := app.DB().NewQuery(`
			UPDATE {{leafen_notion_migration_ledger}} SET [[state]]=COALESCE((
				SELECT [[source]].[[text]] FROM {{leafen_notion_import_sources}} AS [[source]]
				WHERE [[source]].[[job_id]]={:jobId} AND [[source]].[[source_id]]={{leafen_notion_migration_ledger}}.[[source_id]]
					AND [[source]].[[kind]]={{leafen_notion_migration_ledger}}.[[kind]]
			), [[state]])
			WHERE [[migration_id]]={:migrationId} AND ` + notionMigrationLedgerEntriesSQL + `
		`).Bind(dbx.Params{"migrationId": migrationID, "jobId": jobID, "entries": entries}).Execute()
		return err
	})
	if err != nil {
		return err
	}
	return recordNotionMigrationPlanNames(app, migrationID)
}

// recordNotionMigrationPlanNames keeps, for a page or database the plan
// names, the plan's name as the one the run gave it, so a later plan's new
// name is told from one a person gave in Leafen (2026-11-06: an agent's
// second rename of a database was taken for the person's and left out).
func recordNotionMigrationPlanNames(app core.App, migrationID string) error {
	raw := ""
	if err := app.DB().NewQuery(`SELECT [[plan_json]] FROM {{leafen_notion_migrations}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": migrationID}).Row(&raw); err != nil || strings.TrimSpace(raw) == "" {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	plan := notionMigrationPlan{}
	if json.Unmarshal([]byte(raw), &plan) != nil {
		return nil
	}
	titles := map[string]string{}
	for _, page := range plan.Pages {
		if title := strings.TrimSpace(page.Title); title != "" {
			titles[notionMigrationSourceKey(page.SourceID)] = title
		}
	}
	for _, database := range plan.Databases {
		if title := strings.TrimSpace(database.Title); title != "" {
			titles[notionMigrationSourceKey(database.SourceID)] = title
		}
	}
	if len(titles) == 0 {
		return nil
	}
	rows := []struct {
		SourceID string `db:"source_id"`
		Kind     string `db:"kind"`
	}{}
	if err := app.DB().NewQuery(`
		SELECT [[source_id]],[[kind]] FROM {{leafen_notion_migration_ledger}}
		WHERE [[migration_id]]={:migrationId} AND [[kind]] IN ('page','data_source')
	`).Bind(dbx.Params{"migrationId": migrationID}).All(&rows); err != nil {
		return err
	}
	for _, row := range rows {
		title, named := titles[notionMigrationSourceKey(strings.TrimPrefix(row.SourceID, notionDatabaseRootPageSourceID("")))]
		if !named {
			continue
		}
		if _, err := app.DB().NewQuery(`
			UPDATE {{leafen_notion_migration_ledger}} SET [[state]]={:state}
			WHERE [[migration_id]]={:migrationId} AND [[source_id]]={:sourceId} AND [[kind]]={:kind}
		`).Bind(dbx.Params{"state": title, "migrationId": migrationID, "sourceId": row.SourceID, "kind": row.Kind}).Execute(); err != nil {
			return err
		}
	}
	return nil
}

// preloadNotionMigrationLedger starts a run's job from what the migration's
// earlier runs made, a window of the ledger at a time
// (notion_migration_ledger_window.go).
func preloadNotionMigrationLedger(app core.App, migrationID, jobID string) error {
	now := time.Now().UTC().UnixMilli()
	return eachNotionMigrationLedgerWindow(app, migrationID, func(after, last string) error {
		_, err := app.DB().NewQuery(`
			INSERT OR IGNORE INTO {{leafen_notion_import_mappings}} ([[job_id]],[[source_id]],[[native_id]],[[kind]],[[created]])
			SELECT {:jobId}, [[source_id]], [[native_id]], [[kind]], {:now}
			FROM {{leafen_notion_migration_ledger}} WHERE [[migration_id]]={:migrationId}
				AND [[source_id]]>{:after} AND [[source_id]]<={:last}
				AND [[kind]] NOT IN ({:comment},{:template},{:blockSource},{:asked},{:dataSourceById},{:viewById},{:options})
			ORDER BY [[source_id]], [[kind]]
		`).Bind(dbx.Params{
			"jobId": jobID, "migrationId": migrationID, "comment": notionMigrationLedgerComment,
			"template": notionMigrationLedgerTemplate, "blockSource": notionMigrationLedgerBlockSource,
			"asked": notionMigrationLedgerCommentsAsked, "dataSourceById": notionMigrationLedgerDataSourceByID,
			"viewById": notionMigrationLedgerViewByID, "options": notionMigrationLedgerOptions, "after": after, "last": last, "now": now,
		}).Execute()
		return err
	})
}

// notionMigrationChangedSince reports whether Notion changed something after
// the run before started; only a run again asks.
func notionMigrationChangedSince(payload notionImportJobPayload, edited string) bool {
	if payload.MigrationRerunSince <= 0 || strings.TrimSpace(edited) == "" {
		return false
	}
	at, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(edited))
	return err == nil && notionMigrationEditedSince(at, payload.MigrationRerunSince)
}

// notionMigrationEditedSince says whether a source last changed at edited
// may have changed after a run that began at since, in milliseconds. Notion
// gives its times to the minute — a change at 19:40:45 reads 19:40:00 — so a
// time in the minute the run began counts as after it; else a run again
// would pass by a change made in that minute after the run began, and every
// run after it too, each beginning later (2026-11-17: a template changed at
// 19:40:45, after a run began at 19:40:06, stayed as it was run after run).
// What changed in that minute before the run began is read again once more.
func notionMigrationEditedSince(edited time.Time, since int64) bool {
	return since > 0 && edited.UnixMilli() >= since-since%time.Minute.Milliseconds()
}

// notionMigrationRowEdited reads the time Notion last changed a staged row.
func notionMigrationRowEdited(source storedNotionSource) string {
	values := map[string]notionDatabaseRowValuePlan{}
	if json.Unmarshal([]byte(source.Text), &values) != nil {
		return ""
	}
	edited, _ := values[notionImportRowEditedKey].Value.(string)
	return edited
}

// syncNotionMigrationPageTitle gives a page made before Notion's new title,
// unless the plan names its own or the person changed it in Leafen; true when
// it took it. The title comes as the first run brings it
// (notionImportedPageTitleSpans): its formatting and its mentions, those of
// what this run brings naming their Notion objects until it is published
// (resolveNotionImportTitleSpans), so a page made since from its "@" hangs on
// it (notionImportFieldAnchor) and a mention of one the run brings is that
// page's, not its words.
func syncNotionMigrationPageTitle(
	app core.App, job storedJob, payload notionImportJobPayload, pageID string, source storedNotionSource, title string,
) (bool, error) {
	if payload.MigrationRerunSince <= 0 {
		return false, nil
	}
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil {
		return false, nil
	}
	spans := []graphTextSpan{}
	if !strings.HasPrefix(source.SourceID, notionDatabaseRootPageSourceID("")) {
		if kept := decodeStoredTextSpans(notionImportedPageTitleSpans(source.Text, title)); len(kept) > 0 {
			spans = kept
		}
	}
	if err := pointNotionMigrationMentionsAtMade(app, job.ID, spans); err != nil {
		return false, err
	}
	// A data source's mention named spells it anew, as the first run's
	// publication did (resolveNotionImportTitleSpans).
	if len(spans) > 0 {
		title = spelledName(spans)
	}
	follows, err := notionMigrationNameFollows(app, payload, source.SourceID, "page", page.GetString("title"), title)
	if err != nil || !follows {
		return false, err
	}
	return true, renameNotionMigrationPage(app, job, pageID, page.GetString("title"), title, &spans)
}

// pointNotionMigrationMentionsAtMade points the page and database mentions of
// a title an earlier run brought what it made at it before the title is
// written, so the pages hanging on the title stay with it
// (settleTitleAnchoredPages) — written naming their Notion pages, the title
// would mention none of them and leave them hanging there unmentioned. Those
// this run makes are pointed at once it is published.
func pointNotionMigrationMentionsAtMade(app core.App, jobID string, spans []graphTextSpan) error {
	resolver := &notionImportReferenceResolver{app: app, jobID: jobID, resolved: map[string]string{}}
	for index := range spans {
		span := &spans[index]
		attribute, value := "pageId", span.PageID
		if span.Mention == "database" {
			attribute, value = "databaseId", span.DatabaseID
		}
		if span.BlockID != "" || !notionImportPendingReference(attribute, value) {
			continue
		}
		nativeID, err := resolver.resolve(attribute, value)
		if err != nil || nativeID == "" {
			if err != nil {
				return err
			}
			continue
		}
		if attribute == "pageId" {
			span.PageID = nativeID
		} else {
			span.DatabaseID = nativeID
		}
		if words, named := notionImportSourceMentionWords(app, attribute, nativeID); named {
			span.Text = words
		}
	}
	return nil
}

// renameNotionMigrationPage gives a page a new title, as long as it still has
// the one it was known by and isn't in the trash: with the spans given, or
// keeping those of the words it keeps when none are.
func renameNotionMigrationPage(app core.App, job storedJob, pageID, current, title string, spans *[]graphTextSpan) error {
	page, err := app.FindRecordById(pagesCollection, pageID)
	if err != nil || page.GetBool("in_trash") || strings.TrimSpace(page.GetString("title")) != strings.TrimSpace(current) {
		return nil
	}
	input := normalizePageMetadataInput(pageMetadataInput{
		Title: title, TitleSpans: spans, TitleEdited: true, ParentID: page.GetString("parent_id"), InTrash: false,
		MutationID: fmt.Sprintf("notion-migration-title-%s-%s", job.ID, pageID), BaseCursor: page.GetInt("cursor"),
	})
	if !validPageMetadataMutationInput(pageID, input) {
		return nil
	}
	// A page hanging on the title it no longer mentions stays there
	// unmentioned, as Notion's is when an integration took the mention out;
	// it is found again as such (notion_import_unmentioned_pages.go).
	defer keepingFieldAnchors(app)()
	_, status, err := applyPageMetadataTransaction(app, job.Owner, pageID, input, pageMetadataRequestHash(pageID, input))
	if err != nil {
		return err
	}
	if status != pageMetadataApplied {
		return fmt.Errorf("%w: page title %v", errNotionMigrationRestructure, status)
	}
	return nil
}

// rewriteNotionMigrationBlock gives a block made before the content and
// text the source changed since (notion_migration_source_change.go); true
// when the source had changed it.
func rewriteNotionMigrationBlock(
	app core.App, job storedJob, payload notionImportJobPayload, source storedNotionSource, blockID string,
	documents map[string]*notionYgoDocument, structuralDocuments map[string]*structuralMutationBatch,
) (bool, error) {
	plan, err := decodeNotionParagraphSourceText(source.Text)
	if err != nil {
		return false, err
	}
	if changed, err := notionMigrationBlockChanged(app, payload, source.SourceID, plan); err != nil || !changed {
		return false, err
	}
	stored, found, err := loadStoredNotionBlock(app, blockID)
	if err != nil || !found {
		return false, err
	}
	// A block turned into another kind since takes that kind with its content.
	blockType := notionStagedBlockType(source, &plan)
	if _, supported := graphBlockTypes[blockType]; !supported || stored.Type == graphBlockTypeTemplate {
		blockType = stored.Type
	}
	keepNotionImportBlockIcon(stored.Content, plan.Content)
	content := json.RawMessage(`{}`)
	if len(plan.Content) > 0 {
		if content, err = json.Marshal(plan.Content); err != nil {
			return false, err
		}
	}
	if stored.Type != graphBlockTypeTemplate {
		if err := writeNotionMigrationBlock(app, job, payload, stored, blockType, content, structuralDocuments); err != nil {
			return false, err
		}
	}
	if err := keepNotionMigrationBlockRewritten(app, payload.MigrationID, source, blockID, stored.PageID); err != nil {
		return false, err
	}
	if _, text := graphTextBlockTypes[blockType]; !text && blockType != graphBlockTypeTemplate {
		return true, nil
	}
	// Its new text is searched for once the turn's save keeps it
	// (saveNotionYgoDocuments).
	document, err := loadNotionYgoDocument(app, stored.PageID, documents)
	if err != nil {
		return false, err
	}
	if length := document.doc.GetText(ygoBlockTextRootPrefix + blockID).Len(); length > 0 {
		document.doc.Transact(func(txn *crdt.Transaction) {
			txn.GetText(ygoBlockTextRootPrefix+blockID).Delete(txn, 0, length)
		})
	}
	return true, applyNotionSourceText(app, stored.PageID, blockID, source.Text, documents)
}

// updateNotionMigrationRow gives a row made before the values the source
// changed since — the cells the change names, or all when it names none —
// cell by cell, as editing the row does; a cell the source emptied is
// emptied (notion_migration_rerun_emptied.go).
func updateNotionMigrationRow(
	app core.App, job storedJob, payload notionImportJobPayload, source storedNotionSource, rowID string,
	change notionMigrationRowChange, mappings *notionApplyMappings, structuralDocuments map[string]*structuralMutationBatch, place *notionRowPlace,
) error {
	dataSourceID, found := mappings.nativeID(source.ParentSourceID, "data_source")
	if !found {
		return nil
	}
	// A split's row is where the first run put it, with its columns there
	// (notion_migration_split.go).
	var placed string
	if err := app.DB().NewQuery(`SELECT [[data_source_id]] FROM {{leafen_database_rows}} WHERE [[id]]={:id}`).
		Bind(dbx.Params{"id": rowID}).Row(&placed); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	split := placed != "" && placed != dataSourceID
	if split {
		dataSourceID = placed
	}
	pageID, err := notionMigrationDataSourcePage(app, dataSourceID)
	if err != nil {
		return err
	}
	row, err := databaseRowPageMoveRow(app, pageID, rowID)
	if err != nil || row == nil {
		return err
	}
	values := map[string]notionDatabaseRowValuePlan{}
	if err := json.Unmarshal([]byte(source.Text), &values); err != nil {
		return err
	}
	properties, err := loadDatabasePropertyRecords(app, pageID)
	if err != nil {
		return err
	}
	for propertySource, value := range notionMigrationRowTaken(values, change.Emptied) {
		// Relations link once every database is in place; a number Notion
		// gave a row stays the one it had.
		if notionImportRowSpecialKey(propertySource) || value.Type == graphValueTypeRelation || value.Type == graphValueTypeUniqueID ||
			(change.Cells != nil && !change.Cells[propertySource]) {
			continue
		}
		propertyID, found := mappings.nativeID(propertySource, "property")
		if split {
			if propertyID, err = notionImportMappedID(app, job.ID, notionSplitColumnSource(propertySource, dataSourceID), "property"); err != nil {
				return err
			}
			found = propertyID != ""
		}
		// A property since changed, by the plan or a person, keeps its cells;
		// an option goes into another option type it stands for.
		property, known := properties[propertyID]
		if !found || !known {
			continue
		}
		if property.Type != value.Type {
			converted, fits := notionOptionValueAs(value, property.Type)
			if !fits {
				continue
			}
			value = converted
		}
		if _, emptied := change.Emptied[propertySource]; emptied {
			if err := emptyNotionMigrationCell(app, job.ID, propertySource, propertyID, property.Type, row.Values); err != nil {
				return err
			}
			continue
		}
		config, err := json.Marshal(property.Config)
		if err != nil {
			return err
		}
		if place != nil && place.Value != nil {
			if value, err = place.Value(propertySource, value); err != nil {
				return err
			}
		}
		if value, err = notionMergedOptionValue(app, place.run(), job.ID, propertySource, string(config), value); err != nil {
			return err
		}
		resolved, _, err := notionImportResolvedRowValue(app, job, value)
		if err != nil || resolved == nil {
			if err != nil {
				return err
			}
			continue
		}
		encoded, err := json.Marshal(resolved)
		if err != nil {
			return err
		}
		row.Values[propertyID] = graphRowValue{Type: value.Type, Value: encoded, Spans: value.Spans}
	}
	if len(row.Values) == 0 {
		return nil
	}
	batch, err := loadStructuralMutationBatch(app, pageID, structuralDocuments)
	if err != nil {
		return err
	}
	return applyNotionGraphOperations(app, batch, payload.AuthorityOwnerID, job.WorkspaceID, pageID, job.Owner,
		[]graphOperation{{Kind: graphOperationKindUpsertRow, Row: row}})
}
