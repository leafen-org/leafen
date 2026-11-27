package app

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
)

// Files only a moved page's history names: a picture deleted since, the file
// an app or an agent replaced. The bundle carries the files its pages name
// now (snapshotWorkspaceExportFiles); the history may name others, which the
// old server keeps for it (schema_baseline.json). The old
// server sends each of them after the history, the new one keeps them, and
// as the history goes in it puts them in with the IDs they had: the history's
// file spans then name them, and their references keep them.

// workspaceMoveHistoryFile is such a file as sent.
type workspaceMoveHistoryFile struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	Bytes       int64  `json:"bytes"`
	SHA256      string `json:"sha256"`
	// StorageKey is where the new server keeps it until the history goes in.
	StorageKey string `json:"storageKey,omitempty"`
}

var workspaceMoveFileIDPattern = regexp.MustCompile(`^[a-z0-9]{15}$`)

// workspaceMoveBundleFiles are the files a bundle carries.
func workspaceMoveBundleFiles(bundlePath string) (map[string]bool, error) {
	archive, err := zip.OpenReader(bundlePath)
	if err != nil {
		return nil, err
	}
	defer archive.Close()
	files := map[string]bool{}
	for _, entry := range archive.File {
		if entry.Name != workspaceExportFilesIndexPath {
			continue
		}
		reader, err := entry.Open()
		if err != nil {
			return nil, err
		}
		defer reader.Close()
		lines := bufio.NewReaderSize(reader, 1<<20)
		for {
			line, err := readWorkspaceMoveBundleLine(lines)
			if err != nil && !errors.Is(err, io.EOF) {
				return nil, err
			}
			if len(bytes.TrimSpace(line)) > 0 {
				file := workspaceExportFileLine{}
				if json.Unmarshal(line, &file) != nil {
					return nil, errors.New("a line of the bundle's files doesn't read")
				}
				files[file.ID] = true
			}
			if err != nil {
				return files, nil
			}
		}
	}
	return files, nil
}

// sendHistoryFiles sends the files only the history of a bundle's pages
// names, to a move path ("" for the workspace, "/desks/<bot>" for a desk).
func (out *workspaceMoveOut) sendHistoryFiles(ctx context.Context, code serverMoveCode, origin, path, workspaceID, bundlePath string) ([]workspaceMoveHistoryFile, error) {
	sent := []workspaceMoveHistoryFile{}
	pages, err := workspaceMoveBundlePages(bundlePath)
	if err != nil || len(pages) == 0 {
		return sent, err
	}
	carried, err := workspaceMoveBundleFiles(bundlePath)
	if err != nil {
		return sent, err
	}
	encoded, err := json.Marshal(pages)
	if err != nil {
		return sent, err
	}
	named := []string{}
	if err := out.app.DB().NewQuery(`SELECT DISTINCT [[file_id]] FROM {{leafen_file_references}}
		WHERE [[workspace_id]] = {:workspace} AND [[owner_kind]] = 'page_history' AND [[page_id]] IN (SELECT [[value]] FROM json_each({:pages}))
		ORDER BY [[file_id]]`).Bind(dbx.Params{"workspace": workspaceID, "pages": string(encoded)}).Column(&named); err != nil {
		return sent, err
	}
	return out.sendFiles(ctx, code, origin, path, workspaceID, named, carried)
}

// sendFiles sends files of a workspace the bundle doesn't carry, with their
// IDs, to a move path's files; none when the new server doesn't take them.
func (out *workspaceMoveOut) sendFiles(ctx context.Context, code serverMoveCode, origin, path, workspaceID string, named []string,
	carried map[string]bool) ([]workspaceMoveHistoryFile, error) {
	sent := []workspaceMoveHistoryFile{}
	storage, err := out.app.NewFilesystem()
	if err != nil {
		return sent, err
	}
	defer storage.Close()
	for _, id := range named {
		if carried[id] || !workspaceMoveFileIDPattern.MatchString(id) {
			continue
		}
		record, err := out.app.FindRecordById(filesCollection, id)
		if err != nil || !workspaceFileRecordBelongsTo(record, workspaceID) || record.GetFloat("size") <= 0 {
			// Gone here too, or another workspace's: the history names it, and nothing comes.
			continue
		}
		file := workspaceMoveHistoryFile{ID: id, Name: record.GetString("original_name"), ContentType: record.GetString("content_type"),
			Bytes: int64(record.GetFloat("size")), SHA256: record.GetString("sha256")}
		reader, err := storage.GetReader(record.BaseFilesPath() + "/" + record.GetString("blob"))
		if err != nil {
			return sent, err
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodPut, origin+apiRoot+"/workspace-move/in/"+code.ID+path+"/history-files/"+id, reader)
		if err != nil {
			_ = reader.Close()
			return sent, err
		}
		request.ContentLength = file.Bytes
		request.SetBasicAuth(code.ID, code.Secret)
		request.Header.Set("Content-Type", "application/octet-stream")
		request.Header.Set("X-Leafen-File-Name", url.QueryEscape(file.Name))
		request.Header.Set("X-Leafen-File-Type", file.ContentType)
		request.Header.Set("X-Leafen-File-Sha256", file.SHA256)
		client := &http.Client{Timeout: 2 * time.Hour, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		response, err := client.Do(request)
		_ = reader.Close()
		if err != nil {
			return sent, err
		}
		if response.StatusCode == http.StatusNotFound {
			// A server that doesn't take them: the history keeps naming them.
			_ = response.Body.Close()
			return []workspaceMoveHistoryFile{}, nil
		}
		if response.StatusCode != http.StatusOK {
			err := workspaceMoveError(response)
			_ = response.Body.Close()
			return sent, err
		}
		_ = response.Body.Close()
		sent = append(sent, file)
	}
	return sent, nil
}

// historyFile keeps a file only the workspace's history names, once, while
// the move is received.
func (in *workspaceMoveIn) historyFile(event *core.RequestEvent) error {
	id := event.Request.PathValue("file")
	desk := ""
	in.mu.Lock()
	slot := in.held(event)
	ok := slot != nil && slot.state == workspaceMoveInReceiving && slot.upload != nil && workspaceMoveFileIDPattern.MatchString(id) &&
		slot.historyFiles[desk+"/"+id] == nil
	in.mu.Unlock()
	if !ok {
		return workspaceMoveRefused(event)
	}
	key := "workspace-move-" + slot.code.ID + "-file-" + firstNonEmpty(desk, "workspace") + "-" + id
	sum, size, err := stageWorkspaceImportArchiveBytes(filepath.Join(event.App.DataDir(), workspaceImportDirectory), key,
		io.LimitReader(event.Request.Body, workspaceMoveBundleBytes+1))
	if err != nil || size > workspaceMoveBundleBytes || size <= 0 || sum != strings.TrimSpace(event.Request.Header.Get("X-Leafen-File-Sha256")) {
		if err == nil {
			_ = removeWorkspaceImportSourceFile(event.App.DataDir(), key)
		}
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceMoveFailed, "Couldn't keep a file the history names.")
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.held(event) != slot || slot.state != workspaceMoveInReceiving || slot.historyFiles[desk+"/"+id] != nil {
		_ = removeWorkspaceImportSourceFile(event.App.DataDir(), key)
		return workspaceMoveRefused(event)
	}
	if slot.historyFiles == nil {
		slot.historyFiles = map[string]*workspaceImportArchiveUpload{}
	}
	slot.historyFiles[desk+"/"+id] = &workspaceImportArchiveUpload{StorageKey: key, SHA256: sum, Bytes: size}
	return event.JSON(http.StatusOK, map[string]any{"sha256": sum, "bytes": size})
}

// keptHistoryFiles are the files a finished move sent that the new server
// kept, for its history to put in: the workspace's ("") or a desk's.
func (in *workspaceMoveIn) keptHistoryFiles(slot *workspaceMoveInSlot, desk string, sent []workspaceMoveHistoryFile) []workspaceMoveHistoryFile {
	in.mu.Lock()
	defer in.mu.Unlock()
	kept := []workspaceMoveHistoryFile{}
	for _, file := range sent {
		staged := slot.historyFiles[desk+"/"+file.ID]
		if staged == nil || staged.SHA256 != file.SHA256 || staged.Bytes != file.Bytes {
			continue
		}
		file.StorageKey = staged.StorageKey
		kept = append(kept, file)
	}
	return kept
}

// publishWorkspaceMoveHistoryFiles puts in the files only a moved
// workspace's history names, with their IDs, before the history goes in. A
// file that can't be put in leaves its old versions without it, not the move.
func (r *durableJobRunner) publishWorkspaceMoveHistoryFiles(job storedJob, history *workspaceMoveHistorySource) {
	publishWorkspaceMoveFiles(r.app, job.WorkspaceID, job.Owner, "workspace-move-file-"+job.ID, history.Files)
}

// publishWorkspaceMoveFiles puts files a move kept into a workspace here
// with their IDs, as actor; one that can't be put in is left out.
func publishWorkspaceMoveFiles(app core.App, workspaceID, actorID, requestPrefix string, files []workspaceMoveHistoryFile) {
	for _, file := range files {
		input := fileUploadPrepareInput{RequestID: requestPrefix + "-" + file.ID, WorkspaceID: workspaceID,
			Name: file.Name, Size: file.Bytes, ContentType: file.ContentType}
		if err := normalizeProductFileUploadInput(&input); err != nil {
			app.Logger().Warn("workspace move: a file it sent doesn't read", "fileId", file.ID, "error", err)
			continue
		}
		upload, _, err := admitDirectProductFileUploadWithID(app, actorID, input, file.SHA256, file.ID)
		if err == nil && upload.Status != fileUploadStatusCompleted {
			var stored *filesystem.File
			stored, err = filesystem.NewFileFromPath(filepath.Join(app.DataDir(), workspaceImportDirectory, file.StorageKey))
			if err == nil {
				stored.Name, stored.OriginalName = "payload.bin", file.Name
				_, err = publishDirectProductFileUpload(app, upload, stored, file.SHA256)
			}
		}
		if err != nil {
			app.Logger().Warn("workspace move: a file it sent couldn't be put in", "fileId", file.ID, "error", err)
		}
	}
}
