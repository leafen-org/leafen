package app

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

func (r *durableJobRunner) downloadWorkspaceExport(event *core.RequestEvent) error {
	user, workspaceID, ok := authorizeWorkspaceExportRequest(event)
	if !ok {
		return nil
	}
	jobID := strings.TrimSpace(event.Request.PathValue("jobId"))
	job, err := r.workspaceExportResponse(jobID, user.Id, workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceExportNotFound, "Export not found.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeWorkspaceExportReadFailed, "Couldn't read the export.", err)
	}
	if job.Status != workspaceExportStatusCompleted {
		return apiError(event, http.StatusConflict, errorCodeWorkspaceExportNotReady, "Only a completed export can be downloaded.")
	}
	if err := enforceSharingSecurityWorkspaceExports(event.App, user.Id, workspaceID); errors.Is(err, errOrganizationSharingSecurityBlocked) {
		return writeSharingSecurityBlockedError(event)
	} else if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeWorkspaceExportReadFailed, "Couldn't read the export.", err)
	}
	artifact, file, err := openWorkspaceExportArtifact(event.App, jobID, user.Id, workspaceID)
	if err != nil {
		return apiError(event, http.StatusConflict, errorCodeWorkspaceExportArtifactInvalid, "Couldn't check the export file.")
	}
	defer file.Close()
	serveWorkspaceExportArtifact(event, artifact, file)
	if event.Status() < http.StatusBadRequest {
		recordExportReads(event.App, user.Id, job)
	}
	return nil
}

func openWorkspaceExportArtifact(
	app core.App,
	jobID string,
	owner string,
	workspaceID string,
) (storedWorkspaceExportArtifact, *os.File, error) {
	artifact, err := loadWorkspaceExportArtifact(app, jobID, owner, workspaceID)
	if err != nil {
		return storedWorkspaceExportArtifact{}, nil, err
	}
	file, err := openWorkspaceExportArtifactFile(app, artifact)
	return artifact, file, err
}

func openWorkspaceExportArtifactFile(
	app core.App,
	artifact storedWorkspaceExportArtifact,
) (*os.File, error) {
	if filepath.Base(artifact.Filename) != artifact.Filename ||
		!strings.HasPrefix(artifact.Filename, artifact.JobID) {
		return nil, errors.New(errorCodeWorkspaceExportArtifactInvalid)
	}
	path := filepath.Join(app.DataDir(), workspaceExportDirectory, artifact.Filename)
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != artifact.Bytes {
		_ = file.Close()
		return nil, errors.New(errorCodeWorkspaceExportArtifactInvalid)
	}
	return file, nil
}

func serveWorkspaceExportArtifact(
	event *core.RequestEvent,
	artifact storedWorkspaceExportArtifact,
	file *os.File,
) {
	event.Response.Header().Set("Content-Type", artifact.ContentType)
	event.Response.Header().Set("Content-Disposition", contentDisposition("attachment", artifact.Filename))
	event.Response.Header().Set("ETag", fmt.Sprintf("\"sha256:%s\"", artifact.SHA256))
	event.Response.Header().Set("Access-Control-Expose-Headers", "Content-Length, Content-Range, ETag")
	event.Response.Header().Set("X-Content-Type-Options", "nosniff")
	event.Response.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(event.Response, event.Request, artifact.Filename, time.UnixMilli(artifact.Created), file)
}
