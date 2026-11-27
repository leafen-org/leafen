package app

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

type notionMCPGetAsyncTaskInput struct {
	TaskID string `json:"task_id" jsonschema:"The copy's page_id notion-duplicate-page answered, or a task's ID"`
}

type notionMCPAsyncTaskResult struct {
	SourcePageID string `json:"source_page_id"`
	PageID       string `json:"page_id"`
	URL          string `json:"url"`
}

type notionMCPAsyncTaskFailure struct {
	Code string `json:"code"`
}

type notionMCPGetAsyncTaskOutput struct {
	Object           string                          `json:"object"`
	ID               string                          `json:"id"`
	Status           string                          `json:"status"`
	CreatedTime      string                          `json:"created_time"`
	PollAfterSeconds int                             `json:"poll_after_seconds,omitzero"`
	Operation        notionMCPDuplicatePageOperation `json:"operation"`
	Phase            string                          `json:"phase"`
	Result           *notionMCPAsyncTaskResult       `json:"result,omitempty"`
	Error            *notionMCPAsyncTaskFailure      `json:"error,omitempty"`
}

func registerMCPAsyncTaskTool(
	server *mcp.Server,
	app core.App,
	readOnly *mcp.ToolAnnotations,
) {
	mcp.AddTool(server, &mcp.Tool{
		Name: "notion-get-async-task", Title: "Get a Leafen async task",
		Description: "Get the current status and terminal result of a page duplication this connection's principal started, by the copy's page_id notion-duplicate-page answered.",
		Annotations: readOnly,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input notionMCPGetAsyncTaskInput) (*mcp.CallToolResult, notionMCPGetAsyncTaskOutput, error) {
		return getMCPAsyncTask(ctx, app, input)
	})
}

func getMCPAsyncTask(
	ctx context.Context,
	app core.App,
	input notionMCPGetAsyncTaskInput,
) (*mcp.CallToolResult, notionMCPGetAsyncTaskOutput, error) {
	authority, err := mcpAuthorityFromContext(ctx)
	if err != nil {
		return nil, notionMCPGetAsyncTaskOutput{}, err
	}
	taskID := strings.TrimSpace(input.TaskID)
	if taskID == "" || len(taskID) > maxWorkspacePermissionIdentifierLength {
		return nil, notionMCPGetAsyncTaskOutput{}, mcpInvalid("task_id is the ID a tool gave for its task")
	}
	job, err := loadPageDuplicationJobByID(app, taskID, authority.PrincipalID)
	if errors.Is(err, sql.ErrNoRows) {
		job, err = loadPageDuplicationJobByCopy(app, mcpLocalID(app, taskID), authority.PrincipalID)
	}
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || isPermissionDenied(err) {
			return nil, notionMCPGetAsyncTaskOutput{}, mcpDenied("no task of that ID is this account's")
		}
		return nil, notionMCPGetAsyncTaskOutput{}, failedBecause(errMCPToolFailed, err)
	}
	if !strings.HasPrefix(job.RequestID, mcpPageDuplicationRequestPrefix(authority.ConnectionID)) {
		return nil, notionMCPGetAsyncTaskOutput{}, mcpDenied("that task isn't this connection's")
	}
	workspace, err := selectMCPWorkspace(app, authority, mcpWorkspaceInput{
		WorkspaceID: job.WorkspaceID,
	}, permissionWorkspaceWrite)
	if err != nil || workspace.WorkspaceID != job.WorkspaceID {
		return nil, notionMCPGetAsyncTaskOutput{}, mcpRefusedUnlessStorage(errMCPOutsideConnection, err)
	}
	status := "queued"
	if job.Status == durableJobStatusCompleted {
		status = "succeeded"
	} else if job.Status == durableJobStatusFailed || job.Phase == pageDuplicationPhaseFailed {
		status = "failed"
	} else if job.Revision > 1 || job.Phase != pageDuplicationPhaseDiscovering {
		status = "running"
	}
	output := notionMCPGetAsyncTaskOutput{
		Object: "async_task", ID: job.ID, Status: status,
		CreatedTime: mcpTimestamp(job.CreatedAt, authority.clock()),
		Operation: notionMCPDuplicatePageOperation{
			Surface: "mcp", OperationName: "duplicate_page",
		},
		Phase: job.Phase,
	}
	if status == "queued" || status == "running" {
		output.PollAfterSeconds = 2
	}
	if status == "succeeded" {
		output.Result = &notionMCPAsyncTaskResult{
			SourcePageID: job.SourceRootPageID, PageID: job.TargetRootPageID,
			URL: mcpPageAddress(app, authority, job.TargetRootPageID),
		}
	}
	if status == "failed" {
		output.Error = &notionMCPAsyncTaskFailure{Code: job.ErrorCode}
	}
	return nil, output, nil
}

// loadPageDuplicationJobByCopy is the duplication that made a copy, as
// notion-duplicate-page names it by the copy's page (Notion's answer has no
// task of its own); the latest, should a page have been copied into twice.
func loadPageDuplicationJobByCopy(app core.App, pageID, actorID string) (storedPageDuplicationJob, error) {
	jobID := ""
	err := app.DB().NewQuery(`SELECT [[job_id]] FROM {{leafen_page_duplication_jobs}}
		WHERE [[target_root_page_id]]={:pageId} AND [[actor_id]]={:actorId} ORDER BY [[created_at]] DESC LIMIT 1`).
		Bind(dbx.Params{"pageId": pageID, "actorId": actorID}).Row(&jobID)
	if err != nil {
		return storedPageDuplicationJob{}, err
	}
	return loadPageDuplicationJobByID(app, jobID, actorID)
}
