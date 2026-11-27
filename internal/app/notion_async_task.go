package app

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Markdown page writes may ask for an async task with allow_async. Leafen's
// Markdown writes finish within the request, so the task is created already
// succeeded, holding the response the synchronous write returned, and the
// same connection can read it until it expires. A refused write is answered
// as it is, since allow_async changes neither validation nor permissions.

const (
	notionAPIAsyncTasksPath          = notionAPIRoot + "/async_tasks"
	notionAsyncTaskLifetime          = 24 * time.Hour
	notionAsyncOperationPageCreate   = "POST /v1/pages"
	notionAsyncOperationPageMarkdown = "PATCH /v1/pages/:page_id/markdown"
)

type storedNotionAsyncTask struct {
	ID         string `db:"id"`
	Operation  string `db:"operation"`
	Status     string `db:"status"`
	ResultJSON string `db:"result_json"`
	CreatedAt  int64  `db:"created_at"`
}

// notionAsyncConnectionID names the credential a task or a view query
// belongs to: an OAuth grant or a personal access token.
func notionAsyncConnectionID(identity notionOAuthBearerIdentity) string {
	return identity.ClientID + ":" + identity.GrantID
}

type notionAsyncResponseCapture struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (capture *notionAsyncResponseCapture) Header() http.Header { return capture.header }

func (capture *notionAsyncResponseCapture) WriteHeader(status int) {
	if capture.status == 0 {
		capture.status = status
	}
}

func (capture *notionAsyncResponseCapture) Write(body []byte) (int, error) {
	capture.WriteHeader(http.StatusOK)
	return capture.body.Write(body)
}

// serveNotionAsyncWrite runs a write that asked for an async task and answers
// 202 with the task it records, or the write's own refusal.
func serveNotionAsyncWrite(
	event *core.RequestEvent, identity notionOAuthBearerIdentity, operation string, write func() error,
) error {
	original := event.Response
	capture := &notionAsyncResponseCapture{header: http.Header{}}
	event.Response = capture
	err := write()
	event.Response = original
	if err != nil {
		return err
	}
	if capture.status != http.StatusOK {
		for key, values := range capture.header {
			original.Header()[key] = values
		}
		original.WriteHeader(capture.status)
		_, err := original.Write(capture.body.Bytes())
		return err
	}
	result := map[string]any{}
	if json.Unmarshal(capture.body.Bytes(), &result) != nil {
		return notionRequestNotCompleted(event, nil)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	task := storedNotionAsyncTask{
		ID: uuid.NewString(), Operation: operation, Status: "succeeded",
		ResultJSON: string(encoded), CreatedAt: now.UnixMilli(),
	}
	err = event.App.RunInTransaction(func(txApp core.App) error {
		if _, err := txApp.DB().NewQuery(`DELETE FROM {{leafen_notion_async_tasks}} WHERE [[expires_at]] <= {:now}`).
			Bind(dbx.Params{"now": task.CreatedAt}).Execute(); err != nil {
			return err
		}
		_, err := txApp.DB().Insert("leafen_notion_async_tasks", dbx.Params{
			"id": task.ID, "workspace_id": identity.WorkspaceID, "principal_id": identity.PrincipalID,
			"connection_id": notionAsyncConnectionID(identity), "operation": task.Operation, "status": task.Status,
			"result_json": task.ResultJSON, "created_at": task.CreatedAt,
			"expires_at": now.Add(notionAsyncTaskLifetime).UnixMilli(),
		}).Execute()
		return err
	})
	if err != nil {
		return notionRequestNotCompleted(event, err)
	}
	return writeNotionAsyncTask(event, http.StatusAccepted, task)
}

func serveNotionAsyncTaskRetrieve(event *core.RequestEvent, taskID string) error {
	workspace, ok, err := authorizeNotionWorkspaceOperation(event, "pages:read", permissionWorkspaceRead)
	if err != nil || !ok {
		return err
	}
	task := storedNotionAsyncTask{}
	err = event.App.DB().NewQuery(`
		SELECT [[id]], [[operation]], [[status]], [[result_json]], [[created_at]]
		FROM {{leafen_notion_async_tasks}}
		WHERE [[id]] = {:id} AND [[workspace_id]] = {:workspaceId} AND [[connection_id]] = {:connectionId}
			AND [[expires_at]] > {:now}
	`).Bind(dbx.Params{
		"id": strings.TrimSpace(taskID), "workspaceId": workspace.Identity.WorkspaceID,
		"connectionId": notionAsyncConnectionID(workspace.Identity), "now": time.Now().UTC().UnixMilli(),
	}).One(&task)
	if errors.Is(err, sql.ErrNoRows) {
		return notionCompatibilityError(event, http.StatusNotFound, notionErrorObjectNotFound,
			"There's no async task with the requested ID.")
	}
	if err != nil {
		return notionRequestNotCompleted(event, err)
	}
	return writeNotionAsyncTask(event, http.StatusOK, task)
}

func writeNotionAsyncTask(event *core.RequestEvent, status int, task storedNotionAsyncTask) error {
	origin, err := publicServerOrigin(event.Request)
	if err != nil {
		return notionRequestNotCompleted(event, err)
	}
	return writeNotionJSON(event, status, map[string]any{
		"object": "async_task", "id": task.ID, "status": task.Status,
		"status_url":   origin + notionAPIAsyncTasksPath + "/" + task.ID,
		"created_time": time.UnixMilli(task.CreatedAt).UTC().Format(time.RFC3339Nano),
		"operation":    map[string]any{"surface": "rest", "name": task.Operation},
		"result":       json.RawMessage(task.ResultJSON),
	})
}
