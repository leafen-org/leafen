package app

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"

	"github.com/pocketbase/pocketbase/core"
)

type workspaceExportStreamDescriptor struct {
	Path    string `json:"path"`
	Records int    `json:"records"`
	Bytes   int64  `json:"bytes"`
	SHA256  string `json:"sha256"`
}

type workspaceExportArchiveDescriptor struct {
	Format        string                                     `json:"format"`
	FormatVersion int                                        `json:"formatVersion"`
	GeneratedAt   string                                     `json:"generatedAt"`
	Scope         workspaceExportArchiveScope                `json:"scope"`
	Source        nativeWorkspaceExportSource                `json:"source"`
	Counts        map[string]int                             `json:"counts"`
	Files         map[string]any                             `json:"files"`
	Streams       map[string]workspaceExportStreamDescriptor `json:"streams"`
	// Losses are what another tool's bundle says it couldn't carry
	// (docs/bundle.md); a migration's report lists them.
	Losses []workspaceBundleLoss `json:"losses,omitempty"`
}

type workspaceBundleLoss struct {
	What  string `json:"what"`
	Why   string `json:"why"`
	Count int    `json:"count"`
}

type workspaceExportArchiveScope struct {
	Kind    string   `json:"kind"`
	RootIDs []string `json:"rootIds"`
}

type nativeWorkspaceExportSource struct {
	WorkspaceID   string `json:"workspaceId"`
	WorkspaceName string `json:"workspaceName,omitempty"`
}

type workspaceExportEntityLine struct {
	Kind  string         `json:"kind"`
	Value map[string]any `json:"value"`
}

type workspaceExportFileLine struct {
	ID          string `json:"id"`
	Offset      int64  `json:"offset"`
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	Scope       string `json:"scope"`
	Bytes       int64  `json:"bytes"`
	SHA256      string `json:"sha256"`
}

type workspaceExportStreamWriter struct {
	writer  io.Writer
	hasher  hashWriter
	bytes   int64
	records int
}

type hashWriter interface {
	Write([]byte) (int, error)
	Sum([]byte) []byte
}

func newWorkspaceExportStreamWriter(writer io.Writer) *workspaceExportStreamWriter {
	return &workspaceExportStreamWriter{writer: writer, hasher: sha256.New()}
}

func (w *workspaceExportStreamWriter) Write(data []byte) (int, error) {
	written, err := io.MultiWriter(w.writer, w.hasher).Write(data)
	w.bytes += int64(written)
	return written, err
}

func (w *workspaceExportStreamWriter) writeJSONLine(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if _, err := w.Write(encoded); err != nil {
		return err
	}
	w.records++
	return nil
}

func (w *workspaceExportStreamWriter) descriptor(path string) workspaceExportStreamDescriptor {
	return workspaceExportStreamDescriptor{
		Path: path, Records: w.records, Bytes: w.bytes,
		SHA256: hex.EncodeToString(w.hasher.Sum(nil)),
	}
}

func writeNativeWorkspaceExportV2(
	app core.App,
	file *os.File,
	job storedJob,
	payload workspaceExportJobPayload,
) error {
	archive := zip.NewWriter(file)
	closeWithError := func(err error) error {
		_ = archive.Close()
		return err
	}

	entitiesEntry, err := archive.CreateHeader(workspaceExportZipHeader(workspaceExportEntitiesPath, workspaceExportZipTime(job)))
	if err != nil {
		return closeWithError(err)
	}
	entities := newWorkspaceExportStreamWriter(entitiesEntry)
	counts := emptyWorkspaceExportCounts()
	rootIDs := []string{}
	access := newWorkspaceExportArchiveAccess(app, job.Owner)
	databaseRoot, err := loadWorkspaceExportDatabaseArchiveRoot(app, job, payload)
	if err != nil {
		return closeWithError(err)
	}
	// What the blocks and text documents belong to has to be in it, or it can't
	// be brought back; a removal and a making that cancel out go unseen before this.
	heldPages, namedPages := map[string]struct{}{}, map[string]struct{}{}
	// A page's archive carries the workspace emoji it uses (custom_emoji_carried.go).
	carried := newWorkspaceExportCarriedCustomEmojis(payload)
	err = walkWorkspaceExportChunks(app, job, func(chunk workspaceExportInventoryChunk) error {
		for _, item := range chunk.Items {
			if keep, err := access.keep(item); err != nil {
				return err
			} else if !keep {
				continue
			}
			if err := access.mentionsAsRead(item); err != nil {
				return err
			}
			carried.use(item)
			databaseRoot.rewrite(item)
			switch item.Kind {
			case workspaceExportItemPage:
				if id, _ := item.Value["id"].(string); id != "" {
					heldPages[id] = struct{}{}
				}
			case workspaceExportItemBlock, workspaceExportItemYgo:
				if id, _ := item.Value["pageId"].(string); id != "" {
					namedPages[id] = struct{}{}
				}
			}
			if item.Kind == workspaceExportItemPage {
				parentID, _ := item.Value["parentId"].(string)
				id, _ := item.Value["id"].(string)
				if parentID == "" && id != "" {
					rootIDs = append(rootIDs, id)
				}
			}
			rewriteWorkspaceExportReferencesV2(item.Value, "")
			if _, exists := counts[item.Kind]; !exists {
				return newWorkspaceExportFailure(errorCodeWorkspaceExportArtifactInvalid)
			}
			counts[item.Kind]++
			if err := entities.writeJSONLine(workspaceExportEntityLine{Kind: item.Kind, Value: item.Value}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return closeWithError(err)
	}
	for id := range namedPages {
		if _, held := heldPages[id]; !held {
			return closeWithError(newWorkspaceExportFailure(errorCodeWorkspaceExportSourceChanged))
		}
	}
	if err := carried.write(app, job, entities, counts); err != nil {
		return closeWithError(err)
	}

	filesIndexEntry, err := archive.CreateHeader(workspaceExportZipHeader(workspaceExportFilesIndexPath, workspaceExportZipTime(job)))
	if err != nil {
		return closeWithError(err)
	}
	filesIndex := newWorkspaceExportStreamWriter(filesIndexEntry)
	var fileBytes int64
	fileCount, err := walkWorkspaceExportFileSnapshots(app, job.ID, func(snapshot workspaceExportFileSnapshot) error {
		line := workspaceExportFileLine{
			ID: snapshot.RecordID, Offset: fileBytes, Name: snapshot.Name,
			ContentType: snapshot.ContentType, Scope: snapshot.Scope,
			Bytes: snapshot.Bytes, SHA256: snapshot.SHA256,
		}
		if err := filesIndex.writeJSONLine(line); err != nil {
			return err
		}
		fileBytes += snapshot.Bytes
		return nil
	})
	if err != nil {
		return closeWithError(err)
	}

	filesDataEntry, err := archive.CreateHeader(workspaceExportZipHeader(workspaceExportFilesDataPath, workspaceExportZipTime(job)))
	if err != nil {
		return closeWithError(err)
	}
	filesData := newWorkspaceExportStreamWriter(filesDataEntry)
	writtenFiles, err := writeWorkspaceExportFileData(app, filesData, job.WorkspaceID, job.ID)
	if err != nil {
		return closeWithError(err)
	}
	if writtenFiles != fileCount || filesData.bytes != fileBytes {
		return closeWithError(newWorkspaceExportFailure(errorCodeWorkspaceExportSourceChanged))
	}

	scopeKind := workspaceExportScopeWorkspace
	if payload.Format == workspaceExportFormatPageNativeArchive {
		scopeKind = "subtree"
		// A database's archive's root is the page it comes back on, named
		// by its data source (workspace_export_database_archive.go).
		rootIDs = []string{payload.Scope.TargetID}
	}
	descriptor := workspaceExportArchiveDescriptor{
		Format: workspaceExportArchiveFormat, FormatVersion: workspaceExportArchiveFormatVersion,
		GeneratedAt: payload.CreatedAt,
		Scope:       workspaceExportArchiveScope{Kind: scopeKind, RootIDs: rootIDs},
		Source:      nativeWorkspaceExportSource{WorkspaceID: job.WorkspaceID, WorkspaceName: payload.WorkspaceName},
		Counts:      publicWorkspaceExportCounts(counts),
		Files: map[string]any{
			"included": true, "count": fileCount, "bytes": fileBytes, "strippedReferences": 0,
		},
		Streams: map[string]workspaceExportStreamDescriptor{
			"entities":  entities.descriptor(workspaceExportEntitiesPath),
			"fileIndex": filesIndex.descriptor(workspaceExportFilesIndexPath),
			"fileData":  filesData.descriptor(workspaceExportFilesDataPath),
		},
	}
	descriptorBytes, err := json.Marshal(descriptor)
	if err != nil {
		return closeWithError(err)
	}
	if err := writeWorkspaceExportZipEntry(archive, workspaceExportDescriptorPath, descriptorBytes, workspaceExportZipTime(job)); err != nil {
		return closeWithError(err)
	}
	return archive.Close()
}

func emptyWorkspaceExportCounts() map[string]int {
	return map[string]int{
		workspaceExportItemPage: 0, workspaceExportItemBlock: 0,
		workspaceExportItemDataSource: 0, workspaceExportItemView: 0,
		workspaceExportItemProperty: 0, workspaceExportItemRow: 0,
		workspaceExportItemYgo: 0, workspaceExportItemComment: 0, workspaceExportItemTemplate: 0,
		workspaceExportItemDatabaseContainer: 0, workspaceExportItemBlockTrash: 0,
		workspaceExportItemCustomEmoji: 0, workspaceExportItemRowPageBlock: 0,
		workspaceExportItemRowBlockTrash: 0, workspaceExportItemTeamspace: 0,
		workspaceExportItemDatabaseAutomation: 0,
	}
}

func publicWorkspaceExportCounts(counts map[string]int) map[string]int {
	return map[string]int{
		"pages": counts[workspaceExportItemPage], "blocks": counts[workspaceExportItemBlock],
		"databases": counts[workspaceExportItemDataSource], "dataSources": counts[workspaceExportItemDataSource],
		"dbProperties": counts[workspaceExportItemProperty], "dbViews": counts[workspaceExportItemView],
		"databaseRows": counts[workspaceExportItemRow], "ygoDocuments": counts[workspaceExportItemYgo],
		"dbTemplates": counts[workspaceExportItemTemplate], "comments": counts[workspaceExportItemComment],
		"databaseContainers":      counts[workspaceExportItemDatabaseContainer],
		"blockTrashTombstones":    counts[workspaceExportItemBlockTrash],
		"customEmojis":            counts[workspaceExportItemCustomEmoji],
		"rowPageBlocks":           counts[workspaceExportItemRowPageBlock],
		"rowBlockTrashTombstones": counts[workspaceExportItemRowBlockTrash],
		"teamspaces":              counts[workspaceExportItemTeamspace],
		"dbAutomations":           counts[workspaceExportItemDatabaseAutomation],
	}
}

func rewriteWorkspaceExportReferencesV2(value any, field string) any {
	switch typed := value.(type) {
	case string:
		if field == "icon" || field == "cover" {
			if fileID, managed := productFilePresentationID(typed); managed {
				return "leafen-archive://files/" + fileID
			}
		}
		if field == "fileId" || field == "uploadId" || field == "fileUploadId" || field == "attachmentIds" {
			if workspaceExportFileRecordIDPattern.MatchString(typed) {
				return "leafen-archive://files/" + typed
			}
		}
	case []any:
		for index, child := range typed {
			typed[index] = rewriteWorkspaceExportReferencesV2(child, field)
		}
	case map[string]any:
		for key, child := range typed {
			typed[key] = rewriteWorkspaceExportReferencesV2(child, key)
		}
	}
	return value
}

func walkWorkspaceExportFileSnapshots(
	app core.App,
	jobID string,
	visit func(workspaceExportFileSnapshot) error,
) (int, error) {
	cursor := ""
	count := 0
	for {
		snapshots, err := loadWorkspaceExportFileWindow(app, jobID, cursor, workspaceExportFileWindow)
		if err != nil {
			return 0, err
		}
		if len(snapshots) == 0 {
			return count, nil
		}
		for _, snapshot := range snapshots {
			if !workspaceExportFileRecordIDPattern.MatchString(snapshot.RecordID) || snapshot.RecordID <= cursor ||
				snapshot.Bytes <= 0 || !sha256Pattern.MatchString(snapshot.SHA256) {
				return 0, newWorkspaceExportFailure(errorCodeWorkspaceExportArtifactInvalid)
			}
			if err := visit(snapshot); err != nil {
				return 0, err
			}
			cursor = snapshot.RecordID
			count++
		}
	}
}

func writeWorkspaceExportFileData(
	app core.App,
	destination io.Writer,
	workspaceID string,
	jobID string,
) (int, error) {
	filesystem, err := app.NewFilesystem()
	if err != nil {
		return 0, err
	}
	defer filesystem.Close()
	buffer := make([]byte, workspaceExportReaderWindowBytes)
	return walkWorkspaceExportFileSnapshots(app, jobID, func(snapshot workspaceExportFileSnapshot) error {
		record, err := app.FindRecordById(filesCollection, snapshot.RecordID)
		if err != nil || !workspaceFileRecordBelongsTo(record, workspaceID) ||
			record.BaseFilesPath()+"/"+record.GetString("blob") != snapshot.StorageKey ||
			record.GetString("original_name") != snapshot.Name || record.GetString("content_type") != snapshot.ContentType ||
			int64(record.GetFloat("size")) != snapshot.Bytes || record.GetString("sha256") != snapshot.SHA256 {
			return newWorkspaceExportFailure(errorCodeWorkspaceExportSourceChanged)
		}
		reader, err := filesystem.GetReader(snapshot.StorageKey)
		if err != nil {
			return err
		}
		hasher := sha256.New()
		written, copyErr := io.CopyBuffer(
			io.MultiWriter(destination, hasher),
			io.LimitReader(reader, snapshot.Bytes+1),
			buffer,
		)
		closeErr := reader.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if written != snapshot.Bytes || hex.EncodeToString(hasher.Sum(nil)) != snapshot.SHA256 {
			return newWorkspaceExportFailure(errorCodeWorkspaceExportSourceChanged)
		}
		return nil
	})
}
