package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// The sending steps of a workspace's move (workspace_move_out.go): making
// its bundle, sending its pages' history, and waiting for the new server to
// bring it in, each through the new server's side of the move.

// awaitBringIn waits for the new server to bring the workspace in, and ends
// the move moved or failed; a server stopping leaves it waiting, for its
// next start to ask again.
func (out *workspaceMoveOut) awaitBringIn(ctx context.Context, workspaceID, actorID string, code serverMoveCode, origin string) {
	app := out.app
	failWith := func(code string, err error) {
		app.Logger().Warn("workspace move: it stopped", "workspaceId", workspaceID, "stage", "bring-in", "error", err)
		_ = setWorkspaceMove(app, workspaceID, actorID, origin, workspaceMoveFailed, "bring-in", code, nil)
	}
	deadline := time.Now().Add(workspaceMoveImportWait)
	for time.Now().Before(deadline) {
		if !waitUnlessStopped(ctx, 2*workspaceMovePoll) {
			return
		}
		response, err := workspaceMoveAsk(ctx, code, origin, http.MethodGet, "/status", nil, "", 30*time.Second)
		if err != nil {
			continue
		}
		if response.StatusCode == http.StatusUnauthorized {
			_ = response.Body.Close()
			failWith(errorCodeWorkspaceMoveFailed, errors.New("the new server no longer holds the move"))
			return
		}
		status := workspaceMoveInStatus{}
		decodeErr := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&status)
		_ = response.Body.Close()
		if decodeErr != nil || response.StatusCode != http.StatusOK {
			continue
		}
		if status.State != workspaceMoveInImporting {
			failWith(errorCodeWorkspaceMoveFailed, errors.New("the new server didn't receive the move"))
			return
		}
		switch {
		// A cancelled import's job says "canceled" (workspace_move_undo.go).
		case workspaceMoveJobFailed(status.JobStatus):
			// What stopped it there is what this side says, a server that has
			// the workspace's pages already among them.
			code := errorCodeWorkspaceMoveFailed
			if status.ErrorCode != "" {
				code = status.ErrorCode
			}
			failWith(code, errors.New(status.ErrorCode))
			return
		case status.JobStatus == workspaceImportStatusCompleted:
			pages, files, comments := countWorkspaceMoveContents(app, workspaceID)
			report := workspaceMoveReport{Here: workspaceMoveCounts{Pages: pages, Files: files, Comments: comments},
				There: workspaceMoveCounts{Pages: status.Pages, Files: status.Files, Comments: status.Comments}}
			_ = setWorkspaceMove(app, workspaceID, actorID, origin, workspaceMoveMoved, "", "", report)
			return
		}
	}
	failWith(errorCodeWorkspaceMoveFailed, errors.New("the new server didn't finish bringing the workspace in"))
}

// sendHistoryTo sends the history of the pages a bundle carries to a path of
// the move and gives its SHA-256; "" from a server that doesn't take
// history, which keeps the history its import writes.
func (out *workspaceMoveOut) sendHistoryTo(ctx context.Context, code serverMoveCode, origin, path, workspaceID, bundlePath string) (string, error) {
	written := bundlePath + ".history.ndjson"
	defer os.Remove(written)
	sum, size, err := writeWorkspaceMoveHistory(out.app, workspaceID, bundlePath, written)
	if err != nil {
		return "", errors.Join(errors.New("couldn't write the pages' history"), err)
	}
	if size == 0 {
		// No page has history to send: the move goes without.
		return "", nil
	}
	file, err := os.Open(written)
	if err != nil {
		return "", err
	}
	defer file.Close()
	response, err := workspaceMoveAsk(ctx, code, origin, http.MethodPut, path, file, "application/x-ndjson", 6*time.Hour)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		out.app.Logger().Warn("workspace move: the new server doesn't take history; it stays here", "workspaceId", workspaceID)
		return "", nil
	}
	sent := struct {
		SHA256 string `json:"sha256"`
		Bytes  int64  `json:"bytes"`
	}{}
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&sent) != nil || sent.SHA256 != sum || sent.Bytes != size {
		return "", workspaceMoveError(response)
	}
	return sum, nil
}

// export makes a workspace's bundle as Settings › Export makes one, as
// actor, and waits for it.
func (out *workspaceMoveOut) export(ctx context.Context, actorID, workspaceID string) (string, error) {
	app := out.app
	options, valid := normalizeWorkspaceExportOptions(workspaceExportFormatNativeArchive, map[string]bool{"includeFiles": true})
	if !valid {
		return "", errors.New("no options for a bundle")
	}
	result := workspaceExportAdmissionResult{}
	err := app.RunInTransaction(func(txApp core.App) error {
		var err error
		result, err = admitWorkspaceExportInTransaction(txApp, workspaceExportAdmissionInput{
			ActorID: actorID, WorkspaceID: workspaceID, Format: workspaceExportFormatNativeArchive,
			Scope: workspaceExportScope{Kind: workspaceExportScopeWorkspace}, Options: options,
			RequestID: func(revision string) string { return "workspace-move:" + revision },
		})
		return err
	})
	if err != nil || result.IdentityReused {
		return "", errors.Join(errors.New("couldn't start the bundle"), err)
	}
	out.jobs.notify()
	deadline := time.Now().Add(workspaceMoveExportWait)
	for time.Now().Before(deadline) {
		job, err := loadStoredJob(app, result.JobID, actorID)
		if err != nil {
			return "", err
		}
		switch job.Status {
		case workspaceExportStatusCompleted:
			return result.JobID, nil
		case workspaceExportStatusFailed, workspaceExportStatusCancelled:
			return "", errors.New("the bundle failed: " + job.ErrorCode)
		}
		if !waitUnlessStopped(ctx, workspaceMovePoll) {
			return "", ctx.Err()
		}
	}
	return "", errors.New("the bundle took too long")
}

// workspaceMoveAsk calls the new server's side of the move.
func workspaceMoveAsk(ctx context.Context, code serverMoveCode, origin, method, path string, payload any, contentType string,
	timeout time.Duration) (*http.Response, error) {
	var reader io.Reader
	var length int64 = -1
	switch body := payload.(type) {
	case nil:
	case io.Reader:
		reader = body
		if sized, ok := body.(interface{ Stat() (os.FileInfo, error) }); ok {
			if info, err := sized.Stat(); err == nil {
				length = info.Size()
			}
		}
	default:
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader, length, contentType = bytes.NewReader(raw), int64(len(raw)), "application/json"
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	request, err := http.NewRequestWithContext(ctx, method, origin+apiRoot+"/workspace-move/in/"+code.ID+path, reader)
	if err != nil {
		cancel()
		return nil, err
	}
	if length >= 0 {
		request.ContentLength = length
	}
	request.SetBasicAuth(code.ID, code.Secret)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		cancel()
		return nil, err
	}
	body := response.Body
	response.Body = struct {
		io.Reader
		io.Closer
	}{body, closerFunc(func() error { defer cancel(); return body.Close() })}
	return response, nil
}
