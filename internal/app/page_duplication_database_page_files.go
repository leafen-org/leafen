package app

import (
	"encoding/json"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A database's own pages — its rows and its templates — bring their files
// into another workspace as a page does (the user, 2026-11-26: a page copied
// whole into another workspace showed its rows' attached files from the
// first one; the copy's row bodies still named that workspace's files, a
// person of the other workspace alone couldn't open them, and a row's cover
// was left out). A row's file cells and uploaded icon come with its
// database's page (inventoryPageDuplicationFilesWindow); what its own page
// shows — a file, a picture or a callout's uploaded icon in its body, its
// cover — and a template's body are taken in as the copy maps the database's
// rows and sources, before any of them is written, so they are copied first
// as the copy's other files are, and the bodies then name the copies
// (namePageDuplicationBodyFiles). A page's copy and a block copy from another
// workspace (block_copy_workspaces.go) take them in alike; a copy within its
// workspace keeps naming the same files, as before.

// pageDuplicationFileMappingBatch bounds the files one mapping write names.
const pageDuplicationFileMappingBatch = 200

// databaseOwnPageFileIDs are the files the own pages of a window of a
// copy's database entities show: the bodies and covers of the rows a "row"
// window names, and the bodies of the templates of the sources a
// "data_source" window names.
func databaseOwnPageFileIDs(app core.App, workspaceID, ownerID, kind string, ids []string) ([]string, error) {
	if len(ids) == 0 || kind != "row" && kind != "data_source" {
		return nil, nil
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	pages := string(encoded)
	if kind == "data_source" {
		templates := []string{}
		if err := app.DB().NewQuery(`
			SELECT [[id]] FROM {{leafen_database_templates}}
			WHERE [[owner]]={:ownerId} AND [[workspace_id]]={:workspaceId}
				AND [[data_source_id]] IN (SELECT [[value]] FROM json_each({:ids}))
			ORDER BY [[id]]
		`).Bind(dbx.Params{"ownerId": ownerID, "workspaceId": workspaceID, "ids": pages}).Column(&templates); err != nil {
			return nil, err
		}
		if len(templates) == 0 {
			return nil, nil
		}
		if encoded, err = json.Marshal(templates); err != nil {
			return nil, err
		}
		pages = string(encoded)
	}
	fileIDs := []string{}
	err = app.DB().NewQuery(`
		SELECT DISTINCT [[file_id]] FROM {{leafen_file_references}}
		WHERE [[workspace_id]]={:workspaceId} AND [[page_id]] IN (SELECT [[value]] FROM json_each({:pages}))
			AND [[owner_kind]] IN ({:block},{:cover})
		ORDER BY [[file_id]]
	`).Bind(dbx.Params{
		"workspaceId": workspaceID, "pages": pages,
		"block": productFileReferenceOwnerBlock, "cover": productFileReferenceOwnerPageCover,
	}).Column(&fileIDs)
	return fileIDs, err
}

// takePageDuplicationDatabasePageFiles takes in, for a page's copy into
// another workspace, the files of the own pages of the database entities a
// mapping window just mapped; the copy copies them before it goes on
// (advancePageDuplication).
func takePageDuplicationDatabasePageFiles(
	app core.App, job storedPageDuplicationJob, state storedPageDuplicationEntityState, ids []string,
) error {
	if !job.crossWorkspace() {
		return nil
	}
	fileIDs, err := databaseOwnPageFileIDs(app, job.sourceWorkspace(), job.sourceOwner(), state.Kind, ids)
	if err != nil || len(fileIDs) == 0 {
		return err
	}
	for start := 0; start < len(fileIDs); start += pageDuplicationFileMappingBatch {
		references := []pageDuplicationFileReference{}
		for _, fileID := range fileIDs[start:min(start+pageDuplicationFileMappingBatch, len(fileIDs))] {
			references = append(references, pageDuplicationFileReference{
				SourcePageID: state.SourcePageID, TargetPageID: state.TargetPageID, SourceFileID: fileID,
			})
		}
		if err := storePageDuplicationFileMappings(app, job.ID, references); err != nil {
			return err
		}
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_page_duplication_file_state}} SET [[files_complete]]=0, [[updated_at]]={:now}
		WHERE [[job_id]]={:jobId} AND EXISTS (SELECT 1 FROM {{leafen_page_duplication_files}}
			WHERE [[job_id]]={:jobId} AND [[status]]='pending')
	`).Bind(dbx.Params{"jobId": job.ID, "now": time.Now().UTC().UnixMilli()}).Execute()
	return err
}

// namePageDuplicationBodyFiles points a database page's body copied into
// another workspace at the copy's own files: a file block's file and a
// callout's uploaded icon, which the copy took in as it mapped the page
// (takePageDuplicationDatabasePageFiles, mapBlockCopyDatabaseFiles). One the
// copy has no copy of ends it, as a page's body's does
// (remapPageDuplicationBlockFiles).
func namePageDuplicationBodyFiles(app core.App, jobID string, body []newRowPageBlock) error {
	fileIDs := map[string]struct{}{}
	for _, block := range body {
		if id := pageDuplicationBodyFileID(block); id != "" {
			fileIDs[id] = struct{}{}
		}
	}
	if len(fileIDs) == 0 {
		return nil
	}
	mappings, err := pageDuplicationReferenceMappings(app, jobID, "file", referenceIDs(fileIDs))
	if err != nil {
		return err
	}
	for index := range body {
		block := &body[index]
		id := pageDuplicationBodyFileID(*block)
		if id == "" {
			continue
		}
		target := mappings[id]
		if target == "" {
			return errPageDuplicationConflict
		}
		if graphCalloutIconFileIDOf(block.Type, block.Content) != "" {
			block.Content["icon"] = target
		} else {
			block.Content["fileId"] = target
		}
	}
	return nil
}

// pageDuplicationBodyFileID is the stored file a copied body block shows:
// a file block's, or a callout's uploaded icon.
func pageDuplicationBodyFileID(block newRowPageBlock) string {
	if icon := graphCalloutIconFileIDOf(block.Type, block.Content); icon != "" {
		return icon
	}
	if _, managed := graphManagedFileBlockTypes[block.Type]; !managed {
		return ""
	}
	id, _ := block.Content["fileId"].(string)
	if !workspaceExportFileRecordIDPattern.MatchString(id) {
		return ""
	}
	return id
}
