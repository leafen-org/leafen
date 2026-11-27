package app

import (
	"encoding/json"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A duplicated database row gets its row page's icon and body with it, as a
// duplicated single row does, and stays archived when its source is, so a
// copied database shows the rows its source shows. The copied rows of a
// window are found through the duplication's identity map, as are the
// synced block originals the copy took that their bodies' copies show.
func duplicatePageDuplicationRowPages(
	app core.App,
	job storedPageDuplicationJob,
	operations []graphOperation,
) error {
	jobID := job.ID
	targets := []string{}
	for _, operation := range operations {
		if operation.Kind == graphOperationKindUpsertRow && operation.Row != nil {
			targets = append(targets, operation.Row.ID)
		}
	}
	if len(targets) == 0 {
		return nil
	}
	encoded, err := json.Marshal(targets)
	if err != nil {
		return err
	}
	rows := []pageDuplicationIdentity{}
	if err := app.DB().NewQuery(`
		SELECT [[source_id]],[[target_id]] FROM {{leafen_page_duplication_identity_map}}
		WHERE [[job_id]]={:jobId} AND [[kind]]='row'
			AND [[target_id]] IN (SELECT [[value]] FROM json_each({:targets}))
		ORDER BY [[target_id]]
	`).Bind(dbx.Params{"jobId": jobID, "targets": string(encoded)}).All(&rows); err != nil {
		return err
	}
	if len(rows) != len(targets) {
		return errPageDuplicationConflict
	}
	// A copy into another workspace names its own copies of the files; a
	// cover or icon that is no stored file — an address, a gradient — stays.
	var files func(string) string
	if job.crossWorkspace() {
		files = func(value string) string {
			id, managed := productFilePresentationID(value)
			if !managed {
				return value
			}
			mappings, err := pageDuplicationReferenceMappings(app, jobID, "file", []string{id})
			if err != nil || mappings[id] == "" {
				return ""
			}
			return productFilePresentationWithID(value, mappings[id])
		}
	}
	// A row's synced copy of an original the copy copied, on a page or a
	// database page, shows the original's copy (synced_block_duplication.go).
	syncedCopies := pointRowPageBodySyncedCopies(func(contents []map[string]any) (map[string]copiedSyncedBlock, error) {
		return copiedSyncedOriginals(app, jobID, contents)
	})
	for _, row := range rows {
		if err := duplicateDatabaseRowPagePresentationAndBodyFrom(
			app, row.SourceID, job.sourceWorkspace(), row.TargetID, job.WorkspaceID, job.ActorID, files,
			pageDuplicationBodyCustomEmoji(app, jobID), syncedCopies, pageDuplicationBodyRewrite(app, job),
		); err != nil {
			return err
		}
		if job.crossWorkspace() {
			if err := remapPageDuplicationRowCustomEmojiIcon(app, jobID, row.TargetID); err != nil {
				return err
			}
		}
		if err := copyDatabaseRowPageArchiveStateFrom(
			app, row.SourceID, job.sourceWorkspace(), row.TargetID, job.WorkspaceID,
		); err != nil {
			return err
		}
	}
	return nil
}

// pageDuplicationBodyRewrite is the rewrite a database page's body takes in
// a copy of pages or blocks, as the copy's pages' bodies take theirs: what
// the body names that the copy copied — a database with its views' sources,
// properties and rows, a page, a row, a block — names the copy, as a page's
// body does (remapPageDuplicationBlockContents), its text's mentions too. A
// linked database of a database the copy doesn't take comes, into another
// workspace, as a linked view not yet linked, as on a copied page
// (page_duplication_unlinked_views.go). Before, a linked database of a
// database the copy took went on showing the source's: in a copy into
// another workspace, a database of the workspace it came from.
func pageDuplicationBodyRewrite(app core.App, job storedPageDuplicationJob) func([]newRowPageBlock) error {
	return func(body []newRowPageBlock) error {
		return namePageDuplicationBodyCopies(app, job, body)
	}
}

// namePageDuplicationBodyCopies points a copied body's blocks and their text
// at the copies the job made of what they name, and gives its linked
// databases' views the job's copies of their sources and of what their
// settings name.
func namePageDuplicationBodyCopies(app core.App, job storedPageDuplicationJob, body []newRowPageBlock) error {
	// Into another workspace, its files are the copy's own
	// (page_duplication_database_page_files.go).
	if job.crossWorkspace() {
		if err := namePageDuplicationBodyFiles(app, job.ID, body); err != nil {
			return err
		}
	}
	references := pageDuplicationReferenceSet{
		pageIDs: map[string]struct{}{}, blockIDs: map[string]struct{}{},
		dataSourceIDs: map[string]struct{}{}, viewIDs: map[string]struct{}{},
	}
	mentionedPages, mentionedDatabases := map[string]struct{}{}, map[string]struct{}{}
	unlinks := newPageDuplicationUnlinks(app, job)
	// A linked database of the body's own database stays: the body's copy
	// takes that database as a copy, which it then shows
	// (nameRowPageDatabaseCopy).
	unlinks.keep(rowPageOwnDatabaseSources(body))
	for _, block := range body {
		if block.Type == graphBlockTypeInlineDatabase {
			if _, err := unlinks.content(block.Content); err != nil {
				return err
			}
		}
		collectPageDuplicationReferences(block.Content, false, &references)
		collectPageDuplicationSpanTargets(block.Spans, mentionedPages, mentionedDatabases)
	}
	pages, err := pageDuplicationMentionTargetMappings(app, job.ID, referenceIDs(references.pageIDs))
	if err != nil {
		return err
	}
	blocks, err := pageDuplicationReferenceMappings(app, job.ID, "block", referenceIDs(references.blockIDs))
	if err != nil {
		return err
	}
	dataSources, err := pageDuplicationReferenceMappings(app, job.ID, "data_source", referenceIDs(references.dataSourceIDs))
	if err != nil {
		return err
	}
	views, err := pageDuplicationReferenceMappings(app, job.ID, "view", referenceIDs(references.viewIDs))
	if err != nil {
		return err
	}
	textPages, textDatabases, err := pageDuplicationTextMentionMappings(app, job.ID, mentionedPages, mentionedDatabases)
	if err != nil {
		return err
	}
	for index := range body {
		block := &body[index]
		rewritePageDuplicationReferences(block.Content, false, pages, blocks, dataSources, views)
		pageDuplicationSpanCopy(block.Spans, textPages, textDatabases)
		if linked, _ := block.Content["linkedDatabaseSource"].(bool); block.Type == graphBlockTypeInlineDatabase && linked {
			block.LinkedViews = func(view graphDatabaseView) (graphDatabaseView, error) {
				return pageDuplicationLinkedViewCopy(app, job, unlinks, view)
			}
		}
	}
	return nil
}

// pageDuplicationLinkedViewCopy is a copied body's linked database view as
// the job's copies name it, when the job copied its source, as a copied
// page's linked views are (loadPageDuplicationDatabaseViews); one of a source
// it didn't copy stays as it is, or waits for a source in another workspace
// (page_duplication_unlinked_views.go).
func pageDuplicationLinkedViewCopy(
	app core.App, job storedPageDuplicationJob, unlinks *pageDuplicationUnlinks, view graphDatabaseView,
) (graphDatabaseView, error) {
	sources, err := pageDuplicationReferenceMappings(app, job.ID, "data_source", []string{view.DataSourceID})
	if err != nil || sources[view.DataSourceID] == "" {
		if err == nil {
			view.DataSourceID, err = unlinks.viewSource(view.DataSourceID)
		}
		return view, err
	}
	view.DataSourceID = sources[view.DataSourceID]
	config := view.Config
	// A copy into another workspace has no property it left out.
	if job.crossWorkspace() {
		if config, err = pageDuplicationViewConfigWithout(app, job.ID, config); err != nil {
			return view, err
		}
	}
	configs, err := remapPageDuplicationDatabaseJSON(app, job.ID, []json.RawMessage{config})
	if err != nil {
		return view, err
	}
	view.Config = configs[0]
	return view, nil
}
