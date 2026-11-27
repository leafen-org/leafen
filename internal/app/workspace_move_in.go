package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// Moving one workspace to a Leafen server that already has people (the agent
// decision of 2026-10-19 09:31, its B), the receiving side; the sending side
// is workspace_move_out.go. The owner of an empty workspace here opens a code
// for an hour. The old server, given it, reads which accounts here its
// people may be paired with — the people of this workspace's organization —
// then sends the workspace's bundle (docs/bundle.md), its pages' history
// (workspace_move_history.go), then its people with the pairs the person
// confirmed. The bundle import fills the workspace as Settings › Import
// does, keeping the bundle's IDs, with the people paired (workspace_import_
// archive_people.go), puts the history in, and the people paired join it.
// Leafen copies: an agent that guides the move never carries the content.

const (
	workspaceMoveCodePrefix  = "leafen-workspace-move-1:"
	workspaceMoveInStoreKey  = "leafenWorkspaceMoveIn"
	workspaceMoveBundleBytes = 64 << 30
	// workspaceMoveAccountsWindow is how many accounts /accounts gives at a
	// time; its nextCursor reads on to the last.
	workspaceMoveAccountsWindow = 1000

	workspaceMoveInWaiting   = "waiting"
	workspaceMoveInReceiving = "receiving"
	workspaceMoveInImporting = "importing"

	errorCodeWorkspaceMoveCodeInvalid = "LEAFEN_WORKSPACE_MOVE_CODE_INVALID"
	errorCodeWorkspaceMoveNotEmpty    = "LEAFEN_WORKSPACE_MOVE_NOT_EMPTY"
	errorCodeWorkspaceMoveFailed      = "LEAFEN_WORKSPACE_MOVE_FAILED"
)

type workspaceMoveIn struct {
	app   core.App
	jobs  *durableJobRunner
	mu    sync.Mutex
	slots map[string]*workspaceMoveInSlot
}

type workspaceMoveInSlot struct {
	code    serverMoveCode
	encoded string
	// secretSHA256 is the code's secret as kept (workspace_move_resume.go).
	secretSHA256 string
	workspaceID  string
	ownerID      string
	state        string
	upload       *workspaceImportArchiveUpload
	history      *workspaceImportArchiveUpload
	jobID        string
	// historyFiles are the files only the history names, by "/<file>"
	// (workspace_move_history_files.go).
	historyFiles map[string]*workspaceImportArchiveUpload
	// made is what the move made here beside the workspace's pages: that a
	// failed one was let go (workspace_move_undo.go).
	made workspaceMoveMade
}

// workspaceMoveInStatus is what the receiving side shows its owner and tells
// the old server.
type workspaceMoveInStatus struct {
	State     string `json:"state"`
	Code      string `json:"code,omitempty"`
	ExpiresAt string `json:"expiresAt,omitempty"`
	JobID     string `json:"jobId,omitempty"`
	JobStatus string `json:"jobStatus,omitempty"`
	ErrorCode string `json:"errorCode,omitempty"`
	Pages     int    `json:"pages"`
	Files     int    `json:"files"`
	Comments  int    `json:"comments"`
	// Moved is, once a moved workspace is in, who to invite and what to turn
	// public again (workspace_move_access.go).
	Moved *workspaceMovedSummary `json:"moved,omitempty"`
}

func workspaceMoveInOf(app core.App, jobs *durableJobRunner) *workspaceMoveIn {
	return app.Store().GetOrSet(workspaceMoveInStoreKey, func() any {
		return &workspaceMoveIn{app: app, jobs: jobs, slots: map[string]*workspaceMoveInSlot{}}
	}).(*workspaceMoveIn)
}

func registerWorkspaceMoveRoutes(event *core.ServeEvent, jobs *durableJobRunner) {
	in := workspaceMoveInOf(event.App, jobs)
	in.restore()
	event.Router.GET(apiRoot+"/workspaces/{workspaceId}/move-in", in.read)
	event.Router.POST(apiRoot+"/workspaces/{workspaceId}/move-in", in.open)
	event.Router.DELETE(apiRoot+"/workspaces/{workspaceId}/move-in", in.close)
	event.Router.GET(apiRoot+"/workspace-move/in/{id}/accounts", in.accounts)
	event.Router.PUT(apiRoot+"/workspace-move/in/{id}/bundle", in.bundle).Bind(apis.BodyLimit(0))
	event.Router.PUT(apiRoot+"/workspace-move/in/{id}/history", in.history).Bind(apis.BodyLimit(0))
	event.Router.PUT(apiRoot+"/workspace-move/in/{id}/history-files/{file}", in.historyFile).Bind(apis.BodyLimit(0))
	event.Router.POST(apiRoot+"/workspace-move/in/{id}/finish", in.finish).Bind(apis.BodyLimit(4 << 20))
	event.Router.GET(apiRoot+"/workspace-move/in/{id}/status", in.status)
	event.Router.POST(apiRoot+"/workspace-move/in/{id}/public", in.public).Bind(apis.BodyLimit(256 << 10))
	out := workspaceMoveOutOf(event.App, jobs)
	serverBackgroundOf(event.App).run("workspace moves under way", out.resume)
	serverBackgroundOf(event.App).run("moved workspaces' follow-up", out.follow)
	event.Router.GET(apiRoot+"/workspaces/{workspaceId}/move-out", out.read)
	event.Router.POST(apiRoot+"/workspaces/{workspaceId}/move-out/check", out.check).Bind(apis.BodyLimit(64 << 10))
	event.Router.POST(apiRoot+"/workspaces/{workspaceId}/move-out", out.start).Bind(apis.BodyLimit(4 << 20))
}

// current is a workspace's open move, closing one that ran out unused.
func (in *workspaceMoveIn) current(workspaceID string) *workspaceMoveInSlot {
	slot := in.slots[workspaceID]
	if slot != nil && slot.state != workspaceMoveInImporting && time.Now().UnixMilli() > slot.code.Expires {
		in.drop(slot)
		return nil
	}
	return slot
}

func (in *workspaceMoveIn) drop(slot *workspaceMoveInSlot) {
	if slot.state != workspaceMoveInImporting {
		for _, upload := range []*workspaceImportArchiveUpload{slot.upload, slot.history} {
			if upload != nil {
				_ = removeWorkspaceImportSourceFile(in.app.DataDir(), upload.StorageKey)
			}
		}
		for _, upload := range slot.historyFiles {
			_ = removeWorkspaceImportSourceFile(in.app.DataDir(), upload.StorageKey)
		}
	}
	_ = forgetWorkspaceMoveInSlot(in.app, slot.workspaceID)
	delete(in.slots, slot.workspaceID)
}

// owned is the workspace a request names, when its owner asks.
func (in *workspaceMoveIn) owned(event *core.RequestEvent) (workspaceAuthorization, bool) {
	user, ok := authenticatedUser(event)
	if !ok {
		_ = apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
		return workspaceAuthorization{}, false
	}
	authorization, err := authorizeWorkspace(event.App, user.Id, strings.TrimSpace(event.Request.PathValue("workspaceId")), permissionWorkspaceOwner)
	if err != nil {
		_ = apiError(event, http.StatusForbidden, errorCodePermissionDenied, "Only the workspace's owner moves a workspace.")
		return workspaceAuthorization{}, false
	}
	return authorization, true
}

func (in *workspaceMoveIn) project(app core.App, workspaceID string, slot *workspaceMoveInSlot) workspaceMoveInStatus {
	if slot == nil {
		// Whether the workspace is empty, which alone takes a workspace moved here.
		status := workspaceMoveInStatus{State: "none"}
		status.Pages, status.Files, status.Comments = countWorkspaceMoveContents(app, workspaceID)
		if status.Pages > 0 {
			status.Moved = readWorkspaceMovedSummary(app, workspaceID)
		}
		return status
	}
	status := workspaceMoveInStatus{State: slot.state, ExpiresAt: time.UnixMilli(slot.code.Expires).UTC().Format(time.RFC3339), JobID: slot.jobID}
	if slot.state == workspaceMoveInWaiting {
		status.Code = slot.encoded
	}
	if slot.jobID != "" {
		if job, err := loadStoredJob(app, slot.jobID, slot.ownerID); err == nil {
			status.JobStatus, status.ErrorCode = job.Status, job.ErrorCode
			// A move that failed leaves nothing it made.
			if workspaceMoveJobFailed(job.Status) {
				serverBackgroundOf(app).behind("failed workspace move", func() { in.undoFailed(app, slot) })
			}
		}
	}
	status.Pages, status.Files, status.Comments = countWorkspaceMoveContents(app, slot.workspaceID)
	if status.JobStatus == durableJobStatusCompleted {
		status.Moved = readWorkspaceMovedSummary(app, slot.workspaceID)
	}
	return status
}

func (in *workspaceMoveIn) read(event *core.RequestEvent) error {
	authorization, ok := in.owned(event)
	if !ok {
		return nil
	}
	in.mu.Lock()
	slot := in.current(authorization.WorkspaceID)
	in.mu.Unlock()
	return event.JSON(http.StatusOK, in.project(event.App, authorization.WorkspaceID, slot))
}

// open makes the code a workspace is sent to. Only an empty workspace takes
// one: the bundle import fills an empty workspace alone.
func (in *workspaceMoveIn) open(event *core.RequestEvent) error {
	authorization, ok := in.owned(event)
	if !ok {
		return nil
	}
	if fresh, err := workspaceImportArchiveTargetFresh(event.App, authorization.WorkspaceID); err != nil || !fresh {
		return apiError(event, http.StatusConflict, errorCodeWorkspaceMoveNotEmpty,
			"Only an empty workspace takes a workspace moved here; make a new one first.")
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeWorkspaceMoveFailed, "Couldn't make the code.", err)
	}
	code := serverMoveCode{To: serverMoveRequestOrigin(event.Request), ID: security.RandomStringWithAlphabet(20, "abcdefghijklmnopqrstuvwxyz0123456789"),
		Secret: hex.EncodeToString(secret), Release: ReleaseVersion(), Expires: time.Now().Add(serverMoveCodeFor).UnixMilli()}
	if public := requestOrigin(event.Request); public != "" && public != code.To {
		code.Also = public
	}
	if code.To == "" {
		code.To, code.Also = code.Also, ""
	}
	if code.To == "" {
		return apiError(event, http.StatusInternalServerError, errorCodeWorkspaceMoveFailed, "Couldn't tell this server's address.")
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	if slot := in.current(authorization.WorkspaceID); slot != nil {
		// One that failed, what it made taken away, gives way to the next.
		if slot.state != workspaceMoveInWaiting && !slot.made.Undone {
			return apiError(event, http.StatusConflict, errorCodeWorkspaceMoveNotEmpty, "A workspace is moving here now.")
		}
		in.drop(slot)
	}
	slot := &workspaceMoveInSlot{code: code, encoded: encodeMoveCode(workspaceMoveCodePrefix, code), secretSHA256: workspaceMoveSecretSHA256(code.Secret),
		workspaceID: authorization.WorkspaceID, ownerID: authorization.PrincipalID, state: workspaceMoveInWaiting}
	in.slots[authorization.WorkspaceID] = slot
	return event.JSON(http.StatusOK, in.project(event.App, authorization.WorkspaceID, slot))
}

func (in *workspaceMoveIn) close(event *core.RequestEvent) error {
	authorization, ok := in.owned(event)
	if !ok {
		return nil
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	if slot := in.current(authorization.WorkspaceID); slot != nil {
		in.drop(slot)
	}
	return event.JSON(http.StatusOK, in.project(event.App, authorization.WorkspaceID, nil))
}

// held is the move a request names when it carries the code's secret.
func (in *workspaceMoveIn) held(event *core.RequestEvent) *workspaceMoveInSlot {
	id, secret, ok := event.Request.BasicAuth()
	if !ok || id != event.Request.PathValue("id") {
		return nil
	}
	for _, slot := range in.slots {
		if slot.code.ID == id && slot.holdsSecret(secret) {
			return in.current(slot.workspaceID)
		}
	}
	return nil
}

func workspaceMoveRefused(event *core.RequestEvent) error {
	event.Response.Header().Set("WWW-Authenticate", `Basic realm="leafen-workspace-move"`)
	return apiError(event, http.StatusUnauthorized, errorCodeWorkspaceMoveCodeInvalid, "That move code isn't open here.")
}

// workspaceMoveAccount is an account here a moved workspace's person may be
// paired with.
type workspaceMoveAccount struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// accounts tells the old server this server's version and the people of the
// receiving workspace's organization, to pair its people with, a window at a
// time by id: nextCursor is the last one given while more follow.
func (in *workspaceMoveIn) accounts(event *core.RequestEvent) error {
	in.mu.Lock()
	slot := in.held(event)
	in.mu.Unlock()
	// A code whose move has begun or ended opens no other: the old server is
	// told when it checks the code, not after it starts moving.
	if slot == nil || slot.state != workspaceMoveInWaiting {
		return workspaceMoveRefused(event)
	}
	accounts := []workspaceMoveAccount{}
	if err := event.App.DB().NewQuery(`
		SELECT [[user]].[[id]], [[user]].[[name]], lower([[user]].[[email]]) AS [[email]]
		FROM {{leafen_workspaces}} [[workspace]]
		JOIN {{leafen_organization_memberships}} [[member]]
			ON [[member]].[[organization]] = [[workspace]].[[organization]] AND [[member]].[[status]] = 'active'
		JOIN {{users}} [[user]] ON [[user]].[[id]] = [[member]].[[user]]
		WHERE [[workspace]].[[id]] = {:workspaceId} AND [[user]].[[id]] > {:after}
		ORDER BY [[user]].[[id]] LIMIT {:limit}
	`).Bind(dbx.Params{
		"workspaceId": slot.workspaceID, "after": event.Request.URL.Query().Get("cursor"), "limit": workspaceMoveAccountsWindow + 1,
	}).All(&accounts); err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeWorkspaceMoveFailed, "Couldn't read this server's people.", err)
	}
	next := ""
	if len(accounts) > workspaceMoveAccountsWindow {
		accounts = accounts[:workspaceMoveAccountsWindow]
		next = accounts[len(accounts)-1].ID
	}
	schema := 0
	_ = event.App.DB().NewQuery(`SELECT COALESCE(MAX([[version]]), 0) FROM {{leafen_schema_migrations}}`).Row(&schema)
	return event.JSON(http.StatusOK, map[string]any{
		"release": ReleaseVersion(), "schemaVersion": schema, "state": slot.state, "accounts": accounts, "nextCursor": next,
	})
}

// bundle takes the workspace's bundle, once.
func (in *workspaceMoveIn) bundle(event *core.RequestEvent) error {
	in.mu.Lock()
	slot := in.held(event)
	if slot != nil && slot.state == workspaceMoveInWaiting {
		slot.state = workspaceMoveInReceiving
	} else {
		slot = nil
	}
	in.mu.Unlock()
	if slot == nil {
		return workspaceMoveRefused(event)
	}
	upload, err := stageWorkspaceImportArchiveUpload(event.App.DataDir(), "workspace-move-"+slot.code.ID,
		io.LimitReader(event.Request.Body, workspaceMoveBundleBytes+1))
	in.mu.Lock()
	defer in.mu.Unlock()
	if err != nil || upload.Bytes > workspaceMoveBundleBytes {
		if err == nil {
			_ = removeWorkspaceImportSourceFile(event.App.DataDir(), upload.StorageKey)
		}
		slot.state = workspaceMoveInWaiting
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceMoveFailed, "Couldn't keep the bundle.")
	}
	slot.upload = &upload
	return event.JSON(http.StatusOK, map[string]any{"sha256": upload.SHA256, "bytes": upload.Bytes})
}

// history takes the workspace's pages' history, once, after its bundle.
func (in *workspaceMoveIn) history(event *core.RequestEvent) error {
	in.mu.Lock()
	slot := in.held(event)
	if slot == nil || slot.state != workspaceMoveInReceiving || slot.upload == nil || slot.history != nil {
		in.mu.Unlock()
		return workspaceMoveRefused(event)
	}
	in.mu.Unlock()
	directory := filepath.Join(event.App.DataDir(), workspaceImportDirectory)
	key := "workspace-move-" + slot.code.ID + ".history.ndjson"
	sum, size, err := stageWorkspaceImportArchiveBytes(directory, key, io.LimitReader(event.Request.Body, workspaceMoveBundleBytes+1))
	if err != nil || size > workspaceMoveBundleBytes {
		if err == nil {
			_ = removeWorkspaceImportSourceFile(event.App.DataDir(), key)
		}
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceMoveFailed, "Couldn't keep the history.")
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	if in.held(event) != slot || slot.state != workspaceMoveInReceiving {
		_ = removeWorkspaceImportSourceFile(event.App.DataDir(), key)
		return workspaceMoveRefused(event)
	}
	slot.history = &workspaceImportArchiveUpload{StorageKey: key, SHA256: sum, Bytes: size}
	return event.JSON(http.StatusOK, map[string]any{"sha256": sum, "bytes": size})
}

type workspaceMoveFinishInput struct {
	SHA256 string `json:"sha256"`
	// History is the SHA-256 of the pages' history it sent, if it did.
	History string                  `json:"history,omitempty"`
	People  []workspaceImportPerson `json:"people"`
	// Members are the people paired who were members of the workspace.
	Members []string `json:"members"`
	// HistoryFiles are the files only its history names, sent after it.
	HistoryFiles []workspaceMoveHistoryFile `json:"historyFiles,omitempty"`
	// Access is who could open what there (workspace_move_access.go); an
	// older server sends none.
	Access *workspaceMoveAccess `json:"access,omitempty"`
}

// finish starts the bundle import with the people paired, and has the
// paired members join the workspace.
func (in *workspaceMoveIn) finish(event *core.RequestEvent) error {
	input := workspaceMoveFinishInput{}
	if event.BindBody(&input) != nil {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceMoveFailed, "Check the move's people.")
	}
	in.mu.Lock()
	slot := in.held(event)
	if slot == nil || slot.state != workspaceMoveInReceiving || slot.upload == nil || slot.upload.SHA256 != input.SHA256 ||
		(input.History != "" && (slot.history == nil || slot.history.SHA256 != input.History)) {
		in.mu.Unlock()
		return workspaceMoveRefused(event)
	}
	slot.state = workspaceMoveInImporting
	upload := *slot.upload
	var history *workspaceMoveHistorySource
	if input.History != "" {
		history = &workspaceMoveHistorySource{StorageKey: slot.history.StorageKey, SHA256: slot.history.SHA256, Bytes: slot.history.Bytes}
	}
	in.mu.Unlock()
	if history != nil {
		history.Files = in.keptHistoryFiles(slot, "", input.HistoryFiles)
	}
	fail := func(status int, message string, err error) error {
		in.mu.Lock()
		slot.state = workspaceMoveInReceiving
		in.mu.Unlock()
		return apiErrorWithCause(event, status, errorCodeWorkspaceMoveFailed, message, "workspace_move", "finish", errorCodeWorkspaceMoveFailed, err)
	}
	accounts := map[string]bool{}
	for index, person := range input.People {
		// A bot an older server sends goes in unmatched.
		if person.PairOf != "" {
			input.People[index].TargetID = ""
		} else if person.TargetID != "" {
			accounts[person.TargetID] = true
		}
	}
	// A person is paired only with an account of the workspace's organization.
	authorization, err := authorizeWorkspace(event.App, slot.ownerID, slot.workspaceID, permissionWorkspaceOwner)
	if err != nil {
		return fail(http.StatusForbidden, "The workspace's owner can no longer take the move.", err)
	}
	for account := range accounts {
		var member int
		if err := event.App.DB().NewQuery(`SELECT COUNT(*) FROM {{leafen_organization_memberships}}
			WHERE [[organization]] = {:organization} AND [[user]] = {:user} AND [[status]] = 'active'`).
			Bind(dbx.Params{"organization": authorization.Organization.OrganizationID, "user": account}).Row(&member); err != nil || member == 0 {
			return fail(http.StatusBadRequest, "A person was matched with someone outside this workspace's organization.", err)
		}
	}
	dlpScanner, err := newOrganizationDLPContentScanner(event.App, authorization.Organization.OrganizationID)
	if err != nil {
		return fail(http.StatusInternalServerError, "Couldn't check the organization's rules.", err)
	}
	jobID, _, err := in.jobs.startWorkspaceArchiveImportWithPeople(event.App, slot.ownerID, slot.workspaceID,
		"workspace-move:"+slot.code.ID, "workspace-move-"+slot.code.ID, upload, dlpScanner, false, true, input.People, true, history,
		input.Access)
	if err != nil {
		return fail(http.StatusConflict, "Couldn't start bringing the workspace in.", err)
	}
	now := time.Now().UTC().UnixMilli()
	for _, member := range input.Members {
		if !accounts[member] || member == slot.ownerID {
			continue
		}
		if err := event.App.RunInTransaction(func(txApp core.App) error {
			_, err := addWorkspaceMember(txApp, slot.workspaceID, authorization.Organization.OrganizationID, slot.ownerID, member, now,
				map[string]any{"via": "workspace_move"})
			return err
		}); err != nil {
			event.App.Logger().Warn("workspace move: a matched member couldn't join", "workspaceId", slot.workspaceID, "userId", member, "error", err)
		}
	}
	in.mu.Lock()
	slot.jobID = jobID
	in.mu.Unlock()
	// It answers how bringing it in goes after this server starts again.
	if err := keepWorkspaceMoveInSlot(event.App, slot); err != nil {
		event.App.Logger().Warn("workspace move: couldn't keep the move coming in", "workspaceId", slot.workspaceID, "error", err)
	}
	return event.JSON(http.StatusAccepted, in.project(event.App, slot.workspaceID, slot))
}

// status tells the old server how bringing the workspace in goes.
func (in *workspaceMoveIn) status(event *core.RequestEvent) error {
	in.mu.Lock()
	slot := in.held(event)
	in.mu.Unlock()
	if slot == nil {
		return workspaceMoveRefused(event)
	}
	return event.JSON(http.StatusOK, in.project(event.App, slot.workspaceID, slot))
}

// errWorkspaceMoveCodeRefused is the other server saying the code opens nothing there now.
var errWorkspaceMoveCodeRefused = errors.New("the move code isn't open there")

// workspaceMoveError is a move step's refusal, with what the other server said.
func workspaceMoveError(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	answer := struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{}
	_ = json.Unmarshal(body, &answer)
	refusal := answer.Error
	if refusal.Message == "" {
		refusal.Message = response.Status
	}
	if refusal.Code == errorCodeWorkspaceMoveCodeInvalid {
		return fmt.Errorf("%w: %s", errWorkspaceMoveCodeRefused, refusal.Message)
	}
	return errors.New(strings.TrimSpace(refusal.Code + " " + refusal.Message))
}
