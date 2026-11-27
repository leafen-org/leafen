package app

import "github.com/pocketbase/pocketbase/core"

// A workspace's imports (GET /workspaces/{workspaceId}/imports/{jobId}) are
// its files' and archives' imports; an import from Notion is a job of its
// own, read at /workspaces/{workspaceId}/notion-import/jobs/{jobId}. Given a
// Notion import's ID, the files' import read names that address instead of
// saying the import isn't found (2026-11-16: an agent read the same job as
// missing on one address and there on the other).

const workspaceImportIsNotionImport = "This is an import from Notion: GET /workspaces/{workspaceId}/notion-import/jobs/{jobId} reads it."

// workspaceImportFromNotion says whether the ID names the person's import
// from Notion into the workspace.
func workspaceImportFromNotion(app core.App, jobID, ownerID, workspaceID string) bool {
	job, err := loadStoredJob(app, jobID, ownerID)
	return err == nil && job.Kind == notionImportJobKind && job.WorkspaceID == workspaceID
}
