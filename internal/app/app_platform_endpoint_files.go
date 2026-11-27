//go:build !leafen_noapps

package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// Large bodies through an app's endpoints (document apps decision of
// 2026-10-10, 8) go through Leafen's file storage, never through the
// worker: a request body past a megabyte is kept as a file in the app's
// workspace for the organization, uploaded as whoever the run acts for, as
// any upload is — the organization's storage limit and data rules apply,
// and a file nothing keeps is cleaned away later — and the worker gets its
// file. A worker answers with a file the run may read, and Leafen sends it
// with ranges. A worker joins files it was given into one, as a resumable or
// multipart upload ends.

var errAppEndpointFile = errors.New("the file isn't one the app may use here")

// appEndpointActor is who an endpoint run's files are uploaded and read
// as: the person when the run acts for one, otherwise the app's account,
// and the app's workspace for the organization they go in.
type appEndpointActor struct {
	UserID     string
	Workspace  storedAppWorkspace
	Workspaces []string
	// Person is set when the run acts for a person, not the app's account.
	Person bool
}

// personID is the person whose files the run keeps, if any.
func (actor appEndpointActor) personID() string {
	if actor.Person {
		return actor.UserID
	}
	return ""
}

func appEndpointActorFor(app core.App, install storedAppInstall, manifest appManifest, tenancy, personID string,
	person *userKeyAuthority) (appEndpointActor, error) {
	actor := appEndpointActor{}
	workspaces, err := loadAppWorkspaces(app, install.ID)
	if err != nil {
		return actor, err
	}
	for _, workspace := range workspaces {
		if workspace.TenancyKey == tenancy {
			actor.Workspace = workspace
		}
	}
	if actor.Workspace.WorkspaceID == "" {
		if personID == "" {
			return actor, errAppEndpointFile
		}
		if actor.Workspace, err = prepareAppForPerson(app, usableApp{Install: install, Manifest: manifest, TenancyKey: tenancy}, personID); err != nil {
			return actor, err
		}
	}
	actor.UserID, actor.Workspaces = actor.Workspace.AccountID, []string{actor.Workspace.WorkspaceID}
	if personID != "" {
		actor.UserID, actor.Person = personID, true
	}
	if person != nil {
		actor.Workspaces = append(actor.Workspaces, person.WorkspaceIDs...)
	}
	return actor, nil
}

// spoolAppEndpointBody writes what's left of a body, after the head
// already read, to a file of the server's own, up to the largest upload.
func spoolAppEndpointBody(app core.App, head []byte, rest io.Reader) (*os.File, int64, error) {
	folder := filepath.Join(app.DataDir(), ".app-endpoint-bodies")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		return nil, 0, err
	}
	// What a stopped server left behind goes after a day.
	if entries, err := os.ReadDir(folder); err == nil {
		for _, entry := range entries {
			if info, err := entry.Info(); err == nil && time.Since(info.ModTime()) > 24*time.Hour {
				os.Remove(filepath.Join(folder, entry.Name()))
			}
		}
	}
	spool, err := os.CreateTemp(folder, "body-*")
	if err != nil {
		return nil, 0, err
	}
	discard := func(err error) (*os.File, int64, error) {
		spool.Close()
		os.Remove(spool.Name())
		return nil, 0, err
	}
	if _, err := spool.Write(head); err != nil {
		return discard(err)
	}
	// A body is kept only while the disk stays above its floor
	// (data_disk_floor.go).
	limit := int64(fileTransferMaxFileBytes)
	room, known := dataDiskRoom(app)
	if known && room < limit {
		limit = room
	}
	copied, err := io.Copy(spool, io.LimitReader(rest, limit-int64(len(head))+1))
	if err != nil {
		return discard(err)
	}
	size := int64(len(head)) + copied
	if size > limit {
		if limit < fileTransferMaxFileBytes {
			return discard(errServerDiskLow)
		}
		return discard(errFileUploadInvalid)
	}
	return spool, size, nil
}

// stageAppEndpointFile uploads a spooled file into the actor's app
// workspace in parts, as a resumable upload does, and answers it.
func stageAppEndpointFile(app core.App, actor appEndpointActor, spool *os.File, size int64, name, contentType string) (fileTransferFile, error) {
	input := fileUploadPrepareInput{
		RequestID: "app-endpoint-" + security.RandomString(24), WorkspaceID: actor.Workspace.WorkspaceID,
		Name: name, Size: size, ContentType: contentType,
	}
	if err := normalizeProductFileUploadInput(&input); err != nil {
		return fileTransferFile{}, err
	}
	upload, _, err := admitProductFileUpload(app, actor.UserID, input)
	if err != nil {
		return fileTransferFile{}, err
	}
	release := acquireProductFileOperationLock(productFileOperationResumable, upload.UploadID)
	defer release()
	for part := 0; int64(part)*fileTransferPartBytes < size; part++ {
		offset := int64(part) * fileTransferPartBytes
		length := min(int64(fileTransferPartBytes), size-offset)
		hasher := sha256.New()
		if _, err := io.Copy(hasher, io.NewSectionReader(spool, offset, length)); err != nil {
			return fileTransferFile{}, err
		}
		expected, err := expectedProductFileUploadPartBytes(app, upload, part)
		if err != nil {
			return fileTransferFile{}, err
		}
		if upload, _, err = persistProductFileUploadPart(app, upload, part, expected, hex.EncodeToString(hasher.Sum(nil)),
			io.NewSectionReader(spool, offset, length)); err != nil {
			return fileTransferFile{}, err
		}
	}
	parts, err := exactProductFileUploadParts(app, upload)
	if err != nil {
		return fileTransferFile{}, err
	}
	if _, err := streamAndPublishProductFileUpload(app, upload, parts); err != nil {
		return fileTransferFile{}, err
	}
	upload.Status, upload.FileID = fileUploadStatusSettling, upload.UploadID
	record, err := settleProductFileUploadCompletion(app, upload)
	if err != nil {
		return fileTransferFile{}, err
	}
	notifyFileText(app)
	return fileRecordResponse(record), nil
}

// releaseAppEndpointBody lets a body go at once, not a day later as files
// nothing keeps: the run refused the request or failed, so nothing takes it
// (2026-11-14: refused and hidden bodies counted against the person's
// storage for a day). Leafen's cleanup still looks for a reference first.
func releaseAppEndpointBody(app core.App, fileID string) {
	now := time.Now().UTC().UnixMilli()
	if _, err := app.DB().NewQuery(`UPDATE {{leafen_file_reference_cleanups}} SET [[cleanup_due_at]] = {:now}, [[updated_at]] = {:now}
		WHERE [[file_id]] = {:file} AND [[status]] = {:pending} AND [[cleanup_due_at]] > {:now}`).
		Bind(dbx.Params{"file": fileID, "now": now, "pending": fileReferenceCleanupStatusPending}).Execute(); err != nil {
		app.Logger().Warn("leafen app endpoint body not let go", "error", err)
		return
	}
	notifyProductFileReferenceCleanupAfterCommit(app)
}

// appEndpointBodyName is the stored name of a body: the last part of the
// path it was sent to, or "body".
func appEndpointBodyName(requestPath string) string {
	name := path.Base(strings.TrimRight(requestPath, "/"))
	if name == "" || name == "." || name == "/" {
		return "body"
	}
	return name
}

// authorizeAppEndpointFile reads a file the run may use: in the app's
// workspace or one the person's app connection reaches, and readable by the
// actor.
func authorizeAppEndpointFile(app core.App, actor appEndpointActor, fileID string) (authorizedProductFileRead, error) {
	read, err := authorizeProductFileRead(app, actor.UserID, fileID)
	if err != nil {
		return read, err
	}
	if !slices.Contains(actor.Workspaces, read.Record.GetString("workspace")) {
		read.Filesystem.Close()
		return authorizedProductFileRead{}, errAppEndpointFile
	}
	return read, nil
}

// appEndpointJoinRequest is what a worker's leafen_files_join sends: the
// files in order, and the joined file's name and type.
type appEndpointJoinRequest struct {
	Files       []string `json:"files"`
	Name        string   `json:"name"`
	ContentType string   `json:"contentType"`
	Tenancy     string   `json:"tenancy"`
	// WorkspaceID puts the new file in another workspace the run may write,
	// where a page's file block can take it; joining one file copies it.
	WorkspaceID string `json:"workspaceId"`
}

// joinAppEndpointFiles writes the files one after another into a new file
// in the app's workspace, or the one the request names, and answers it.
func joinAppEndpointFiles(app core.App, actor, target appEndpointActor, request appEndpointJoinRequest) (fileTransferFile, error) {
	if len(request.Files) == 0 || len(request.Files) > fileTransferMaxParts {
		return fileTransferFile{}, errors.New("leafen_files_join takes 1 to 640 files")
	}
	spool, size, err := spoolAppEndpointBody(app, nil, strings.NewReader(""))
	if err != nil {
		return fileTransferFile{}, err
	}
	defer func() {
		spool.Close()
		os.Remove(spool.Name())
	}()
	for _, fileID := range request.Files {
		read, err := authorizeAppEndpointFile(app, actor, fileID)
		if err != nil {
			return fileTransferFile{}, err
		}
		reader, err := read.Filesystem.GetReader(read.Record.BaseFilesPath() + "/" + read.Record.GetString("blob"))
		if err == nil {
			var copied int64
			copied, err = io.Copy(spool, io.LimitReader(reader, fileTransferMaxFileBytes-size+1))
			size += copied
			reader.Close()
		}
		read.Filesystem.Close()
		if err != nil {
			return fileTransferFile{}, err
		}
		if size > fileTransferMaxFileBytes {
			return fileTransferFile{}, errFileUploadInvalid
		}
	}
	if size == 0 {
		return fileTransferFile{}, errors.New("the joined file would be empty")
	}
	joined, err := stageAppEndpointFile(app, target, spool, size, request.Name, request.ContentType)
	if err != nil {
		return joined, err
	}
	// A copy of one file keeps the time its sender gave it.
	if len(request.Files) == 1 {
		if modified := fileClientModified(app, request.Files[0]); modified > 0 {
			if err := setFileClientModified(app, joined.ID, modified); err != nil {
				return fileTransferFile{}, err
			}
		}
	}
	return joined, nil
}

func (host *appWorkerHost) joinFiles(payload []byte) ([]byte, error) {
	request := appEndpointJoinRequest{}
	if err := json.Unmarshal(payload, &request); err != nil {
		return nil, errors.New("leafen_files_join takes files, a name and a content type")
	}
	tenancy := host.run.Tenancy
	if tenancy == "" && appWorkspaceTenancyKnown(host.app, host.install.ID, request.Tenancy) {
		tenancy = request.Tenancy
	}
	personID := ""
	if host.run.Person != nil {
		personID = host.run.Person.OwnerID
	}
	actor, err := appEndpointActorFor(host.app, host.install, host.manifest, tenancy, personID, host.run.Person)
	if err != nil {
		return json.Marshal(map[string]string{"error": "the app has no workspace for that organization"})
	}
	limits := loadAppEndpointLimits(host.app, host.install.ID)
	if left, err := appEndpointStorageLeft(host.app, host.install.ID, actor.personID(), limits, 0); err != nil || !left {
		return json.Marshal(map[string]string{"error": "the person keeps as much through this app as its administrator allows"})
	}
	// Reading stays with the run's workspaces; the file goes where asked.
	target := actor
	if request.WorkspaceID != "" {
		if !slices.Contains(actor.Workspaces, request.WorkspaceID) {
			return json.Marshal(map[string]string{"error": "the run can't keep files in that workspace"})
		}
		target.Workspace.WorkspaceID = request.WorkspaceID
	}
	joined, err := joinAppEndpointFiles(host.app, actor, target, request)
	if err != nil {
		return json.Marshal(map[string]string{"error": err.Error()})
	}
	recordAppEndpointFile(host.app, host.install.ID, actor.personID(), joined.ID)
	return json.Marshal(map[string]any{"file": joined})
}
