package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/reearth/ygo/crdt"
)

// A ZIP's batch leaves the page Notion's ZIP import leaves (seen 2026-11-07):
// a box counting the pages and attachments it brought, a closed "Debugging
// Information" listing the workspace and import, each name bold and its
// value as code, a closed "Page Mappings (First 20 only)" heading over a table
// of where each folder and document went, a level at a time with a level's
// folders first, and the import's own page last, a
// closed "Unreferenced Attachments" heading over a database of the
// attachments no document uses, and "Output Files" holding the whole mapping
// as a JSON file.

const workspaceImportZipLogMappingRows = 20

type workspaceImportBatchLogItem struct {
	Position       int    `db:"position"`
	ParentPosition int    `db:"parent_position"`
	Kind           string `db:"kind"`
	Name           string `db:"name"`
	Title          string `db:"title"`
	PageID         string `db:"page_id"`
	State          string `db:"state"`
	Message        string `db:"message"`
	FileID         string `db:"file_id"`
}

type workspaceImportZipLogMapping struct {
	Source string `json:"source"`
	PageID string `json:"pageId"`
	title  string
}

// workspaceImportZipLog is what the page says of a ZIP's batch.
type workspaceImportZipLog struct {
	pages        int
	attachments  int
	mappings     []workspaceImportZipLogMapping
	unreferenced []workspaceImportBatchLogItem
}

// readWorkspaceImportZipLog reads where the batch's folders and documents
// went, the import's own page last, and what it brought.
func readWorkspaceImportZipLog(app core.App, jobID, zipName string, items []workspaceImportBatchLogItem) (workspaceImportZipLog, error) {
	log := workspaceImportZipLog{}
	prefix := zipName + "/"
	var root *workspaceImportBatchLogItem
	for index := range items {
		item := items[index]
		switch {
		case item.Kind == workspaceImportBatchItemKindSkipped:
			if item.FileID != "" {
				log.unreferenced = append(log.unreferenced, item)
			}
			continue
		case item.State != workspaceImportBatchItemImported || item.PageID == "":
			continue
		}
		log.pages++
		if item.ParentPosition < 0 && item.Kind == workspaceImportBatchItemKindFolder {
			root = &items[index]
			continue
		}
		source := strings.TrimPrefix(item.Name, prefix)
		// A folder's path ends with its slash, as Notion's table writes it.
		if item.Kind == workspaceImportBatchItemKindFolder && !strings.HasSuffix(source, "/") {
			source += "/"
		}
		log.mappings = append(log.mappings, workspaceImportZipLogMapping{Source: source, PageID: item.PageID, title: item.Title})
	}
	sortWorkspaceImportZipLogMappings(log.mappings)
	if root != nil {
		// The import's own page is named by the ZIP, without ".zip" and its
		// spaces underscores, whatever folders it holds, as Notion's ("가 나.zip"
		// holding one folder "다 라" reads "가_나", seen 2026-11-12).
		source := zipName
		if strings.EqualFold(path.Ext(source), ".zip") {
			source = source[:len(source)-len(".zip")]
		}
		source = strings.Join(strings.Fields(source), "_")
		log.mappings = append(log.mappings, workspaceImportZipLogMapping{Source: source, PageID: root.PageID, title: root.Title})
	}
	// Each file a page shows once, and each attachment no document uses.
	var used int
	if err := app.DB().NewQuery(`
		SELECT COUNT(DISTINCT json_extract([[block]].[[content]], '$.fileId')) FROM {{leafen_blocks}} AS [[block]]
		WHERE [[block]].[[page_id]] IN (
			SELECT [[page_id]] FROM {{leafen_workspace_import_batch_items}} WHERE [[job_id]] = {:jobId} AND [[state]] = 'imported'
		) AND COALESCE(json_extract([[block]].[[content]], '$.fileId'), '') <> ''
	`).Bind(dbx.Params{"jobId": jobID}).Row(&used); err != nil {
		return log, err
	}
	log.attachments = used + len(log.unreferenced)
	return log, nil
}

// sortWorkspaceImportZipLogMappings puts the mappings in the order Notion's
// table lists them (seen 2026-11-07, two ZIPs): a level of the ZIP at a time,
// the folders inside the ones before them in that order, and in each folder
// its folders first, then its documents, each by name.
func sortWorkspaceImportZipLogMappings(mappings []workspaceImportZipLogMapping) {
	type place struct {
		depth        int
		parent, name string
		folder       bool
	}
	places := make(map[string]place, len(mappings))
	for _, mapping := range mappings {
		trimmed := strings.TrimSuffix(mapping.Source, "/")
		parts := strings.Split(trimmed, "/")
		places[mapping.Source] = place{
			depth: len(parts), parent: strings.Join(parts[:len(parts)-1], "/"), name: parts[len(parts)-1],
			folder: strings.HasSuffix(mapping.Source, "/"),
		}
	}
	// rank is each folder's place in the order so far, so its contents follow
	// the folders before it.
	rank := map[string]int{}
	sort.SliceStable(mappings, func(i, j int) bool {
		return places[mappings[i].Source].depth < places[mappings[j].Source].depth
	})
	for start := 0; start < len(mappings); {
		depth := places[mappings[start].Source].depth
		end := start
		for end < len(mappings) && places[mappings[end].Source].depth == depth {
			end++
		}
		level := mappings[start:end]
		sort.SliceStable(level, func(i, j int) bool {
			a, b := places[level[i].Source], places[level[j].Source]
			rankA, foundA := rank[a.parent]
			rankB, foundB := rank[b.parent]
			switch {
			case foundA != foundB:
				return foundA
			case rankA != rankB:
				return rankA < rankB
			case a.parent != b.parent:
				return a.parent < b.parent
			case a.folder != b.folder:
				return a.folder
			}
			return a.name < b.name
		})
		for index, mapping := range level {
			if places[mapping.Source].folder {
				rank[strings.TrimSuffix(mapping.Source, "/")] = start + index
			}
		}
		start = end
	}
}

// workspaceImportZipLogBody is the page's Markdown; the database of
// unreferenced attachments and the mapping file come after it.
func workspaceImportZipLogBody(text map[string]string, log workspaceImportZipLog, workspaceID, jobID string) string {
	var body strings.Builder
	summary := fmt.Sprintf(text["zipPages"], log.pages)
	if log.attachments > 0 {
		summary = fmt.Sprintf(text["zipPagesAttachments"], log.pages, log.attachments)
	}
	body.WriteString("<callout icon=\"✅\">\n\t" + summary + "\n</callout>\n")
	body.WriteString("<details>\n<summary>" + text["zipDebug"] + "</summary>\n")
	// A list of the two, the name bold and the value code, as Notion's (seen 2026-11-07).
	body.WriteString("\t- **" + text["zipDebugWorkspace"] + "** `" + workspaceID + "`\n")
	body.WriteString("\t- **" + text["zipDebugImport"] + "** `" + jobID + "`\n")
	body.WriteString("</details>\n")
	body.WriteString("### " + text["zipMappings"] + " {toggle=\"true\"}\n")
	body.WriteString("\t<table header-row=\"true\">\n")
	row := func(cells ...string) {
		body.WriteString("\t<tr>\n")
		for _, cell := range cells {
			body.WriteString("\t<td>" + cell + "</td>\n")
		}
		body.WriteString("\t</tr>\n")
	}
	row(text["zipNumber"], text["zipSource"], text["zipPage"])
	for index, mapping := range log.mappings {
		if index == workspaceImportZipLogMappingRows {
			break
		}
		// Named by its title, which a database's row page, outside the
		// sidebar's pages, shows (an export's, archiveImportPlan.ts).
		row(fmt.Sprintf("%d.", index+1), workspaceImportBatchLogEscape(mapping.Source),
			fmt.Sprintf(`<mention-page url="%s">%s</mention-page>`, mapping.PageID, notionMarkdownEscaped.Replace(mapping.title)))
	}
	body.WriteString("\t</table>\n")
	if len(log.unreferenced) > 0 {
		body.WriteString("### " + text["zipUnreferenced"] + " {toggle=\"true\"}\n")
	}
	body.WriteString("### " + text["zipOutput"] + "\n")
	return body.String()
}

// addWorkspaceImportZipLogParts puts the database of unreferenced
// attachments inside its heading and the mapping file at the page's end, once
// for the batch. Its parts' IDs come from the batch, the same each time; a
// row is a page, whose ID is a page's for a batch admitted since rows took
// page IDs (pageIDs).
func (r *durableJobRunner) addWorkspaceImportZipLogParts(
	job storedJob, pageIDs bool, pageID, zipName string, text map[string]string, log workspaceImportZipLog,
) error {
	identity := func(kind string, index int) string {
		if kind == "row" && pageIDs {
			return derivedPageID("workspace-import-zip-log", job.ID, kind, strconv.Itoa(index))
		}
		digest := sha256.Sum256([]byte(fmt.Sprintf("leafen:workspace-import-zip-log:v1\x00%s\x00%s\x00%d", job.ID, kind, index)))
		return hex.EncodeToString(digest[:])[:15]
	}
	mapping, err := json.MarshalIndent(map[string]any{"pages": log.mappings}, "", "  ")
	if err != nil {
		return err
	}
	file, err := storeWorkspaceImportZipLogFile(r.app, job.Owner, job.WorkspaceID, job.ID, text["zipMappingFile"], mapping)
	if err != nil {
		return err
	}
	files := map[string]*core.Record{}
	for _, item := range log.unreferenced {
		record, err := r.app.FindRecordById(filesCollection, item.FileID)
		if err != nil || !workspaceFileRecordBelongsTo(record, job.WorkspaceID) {
			continue
		}
		files[item.FileID] = record
	}
	_, err = r.ygo.applyContentGraphMutationContext(context.Background(), job.Owner, pageID,
		graphMutationInput{MutationID: "workspace-import-log-parts:" + job.ID},
		contentGraphMutationHooks{
			Authorize: func(tx core.App) (pageAuthorization, error) {
				return authorizePageBody(tx, job.Owner, pageID, permissionWorkspaceWrite)
			},
			Prepare: func(tx core.App, _ pageAuthorization, _ *crdt.Doc) (contentGraphMutationPreparation, error) {
				// The body just written is in its stored blocks.
				order := []string{}
				if err := tx.DB().NewQuery(`
					SELECT [[id]] FROM {{leafen_blocks}} WHERE [[page_id]] = {:pageId} AND [[parent_id]] = '' ORDER BY [[position]]
				`).Bind(dbx.Params{"pageId": pageID}).Column(&order); err != nil {
					return contentGraphMutationPreparation{}, err
				}
				operations := []graphOperation{}
				// The box, the debugging toggle, the mappings heading, then this.
				if len(log.unreferenced) > 0 && len(order) >= 4 {
					operations = append(operations, workspaceImportZipLogAttachmentOperations(
						identity, order[3], text, zipName, log.unreferenced, files)...)
				}
				if len(order) > 0 {
					tail := order[len(order)-1]
					// Without its size, as Notion's NotionPageMapping.json (seen 2026-11-07).
					mappingFile := canonicalGraphFileContent(file)
					mappingFile.SizeHidden = true
					content, err := json.Marshal(mappingFile)
					if err != nil {
						return contentGraphMutationPreparation{}, err
					}
					operations = append(operations, graphOperation{Kind: graphOperationKindUpsertBlock, Block: &graphBlock{
						ID: identity("file", 0), AfterID: &tail, Position: "p0000000000", Type: graphBlockTypeFile, Content: content,
					}})
				}
				return contentGraphMutationPreparation{Operations: operations}, nil
			},
		})
	// The log says which part of the change the graph refused.
	var refused *graphRequestError
	if errors.As(err, &refused) {
		return fmt.Errorf("%w: %s", err, refused.message)
	}
	return err
}

// workspaceImportZipLogAttachmentOperations make the database of the
// attachments no document uses inside its heading: # · File Path · Attachment
// Link, as Notion's.
func workspaceImportZipLogAttachmentOperations(
	identity func(string, int) string, headingID string, text map[string]string, zipName string,
	attachments []workspaceImportBatchLogItem, files map[string]*core.Record,
) []graphOperation {
	blockID, dataSourceID, viewID := identity("block", 0), identity("source", 0), identity("view", 0)
	titleID, pathID, linkID := identity("property", 0), identity("property", 1), identity("property", 2)
	content, _ := json.Marshal(map[string]string{"dataSourceId": dataSourceID, "viewId": viewID})
	// The table places the rows in their numbers' order (importViewBase) and
	// shows its columns in that order: a view that names none would show the
	// title first and the rest by name.
	rowIDs := make([]string, len(attachments))
	for index := range attachments {
		rowIDs[index] = identity("row", index)
	}
	settings := importViewBase(rowIDs)
	settings["propertyOrder"] = []string{titleID, pathID, linkID}
	config, _ := json.Marshal(settings)
	parent := headingID
	operations := []graphOperation{
		{Kind: graphOperationKindUpsertBlock, Block: &graphBlock{
			ID: blockID, ParentID: &parent, Position: "p0000000000", Type: graphBlockTypeInlineDatabase, Content: content,
		}},
		{Kind: graphOperationKindUpsertDataSource, DataSource: &graphDataSource{ID: dataSourceID, Title: text["zipUnreferenced"]}},
		{Kind: graphOperationKindUpsertView, View: &graphDatabaseView{
			ID: viewID, DataSourceID: dataSourceID, Type: graphViewTypeTable,
			Name: workspaceImportZipViewName, Position: "p0000000000", Config: config,
		}},
	}
	for index, property := range []struct{ id, kind, name string }{
		{titleID, graphValueTypeTitle, "#"}, {pathID, graphValueTypeRichText, text["zipFilePath"]}, {linkID, graphValueTypeFiles, text["zipAttachmentLink"]},
	} {
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertProperty, Property: &graphProperty{
			ID: property.id, DataSourceID: dataSourceID, Type: property.kind, Name: property.name,
			Position: fmt.Sprintf("p%010d", index), Config: json.RawMessage(`{}`),
		}})
	}
	// Numbered by their paths in the ZIP, a character's code at a time, as
	// Notion's (seen 2026-11-12: "1.png", "B.png", "Z.png", "_x.png", "a.png",
	// "a10.png", "a9.png", "é.png", "가.png"; "sub/a.png" between "img9.png"
	// and "z.png"), whatever order the ZIP holds them in.
	sort.SliceStable(attachments, func(i, j int) bool {
		return strings.TrimPrefix(attachments[i].Name, zipName+"/") < strings.TrimPrefix(attachments[j].Name, zipName+"/")
	})
	for index, item := range attachments {
		number, _ := json.Marshal(fmt.Sprint(index + 1))
		path, _ := json.Marshal(strings.TrimPrefix(item.Name, zipName+"/"))
		values := map[string]graphRowValue{
			titleID: {Type: graphValueTypeTitle, Value: number},
			pathID:  {Type: graphValueTypeRichText, Value: path},
		}
		if record := files[item.FileID]; record != nil {
			link, _ := json.Marshal([]graphFileValueItem{graphFileValueItemFromManaged(canonicalGraphFileContent(record))})
			values[linkID] = graphRowValue{Type: graphValueTypeFiles, Value: link}
		}
		operations = append(operations, graphOperation{Kind: graphOperationKindUpsertRow, Row: &graphDatabaseRow{
			ID: rowIDs[index], DataSourceID: dataSourceID, Position: fmt.Sprintf("p%010d", index), Values: values,
		}})
	}
	return operations
}

// storeWorkspaceImportZipLogFile keeps the mapping as the person's upload,
// once for the batch.
func storeWorkspaceImportZipLogFile(app core.App, userID, workspaceID, jobID, name string, data []byte) (*core.Record, error) {
	file, err := filesystem.NewFileFromBytes(data, name)
	if err != nil {
		return nil, err
	}
	digest, err := fileDigest(file.Reader)
	if err != nil {
		return nil, err
	}
	seed := sha256.Sum256([]byte("leafen:workspace-import-zip-log-file:v1\x00" + userID + "\x00" + jobID))
	input := fileUploadPrepareInput{
		RequestID: "import-log-" + hex.EncodeToString(seed[:16]), WorkspaceID: workspaceID,
		Name: name, Size: int64(len(data)), ContentType: "application/json",
	}
	if err := normalizeProductFileUploadInput(&input); err != nil {
		return nil, err
	}
	release := acquireProductFileOperationLock(productFileOperationScopedNamespace(productFileOperationUploadRequest, userID), input.RequestID)
	defer release()
	upload, _, err := admitDirectProductFileUploadWithID(app, userID, input, digest, hex.EncodeToString(seed[16:])[:15])
	if err != nil {
		return nil, err
	}
	uploadRelease := acquireProductFileOperationLock(productFileOperationResumable, upload.UploadID)
	defer uploadRelease()
	if upload.Status == fileUploadStatusCompleted {
		return completedDirectProductFileUpload(app, upload)
	}
	return publishDirectProductFileUpload(app, upload, file, digest)
}
