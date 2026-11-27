package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// Moving one workspace to another Leafen server, the sending side
// (workspace_move_in.go is the receiving side). Its owner, given the code the
// new workspace's owner made, checks it: the new server answers with its
// version and the accounts there, and the workspace's people are each
// offered the account with the same email. Once the owner confirms the pairs,
// the workspace stops taking changes (workspace_move_fence.go), its bundle is
// made as Settings › Export makes one, sent, and handed over with its people;
// the new server brings it in, and the counts on both sides are compared.
// The workspace stays here, reading only, pointing to its new place; its
// edit history, and pages only another member could read, stay here too, as
// the move says before it starts.

const (
	workspaceMoveOutStoreKey = "leafenWorkspaceMoveOut"
	workspaceMoveExportWait  = 2 * time.Hour
	workspaceMoveImportWait  = 4 * time.Hour
	workspaceMovePoll        = time.Second
)

type workspaceMoveOut struct {
	app  core.App
	jobs *durableJobRunner
	mu   sync.Mutex
	busy map[string]bool
}

func workspaceMoveOutOf(app core.App, jobs *durableJobRunner) *workspaceMoveOut {
	return app.Store().GetOrSet(workspaceMoveOutStoreKey, func() any {
		return &workspaceMoveOut{app: app, jobs: jobs, busy: map[string]bool{}}
	}).(*workspaceMoveOut)
}

// workspaceMovePerson is one of the workspace's people, and the account on
// the new server with the same email, if any.
type workspaceMovePerson struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Email      string `json:"email"`
	Role       string `json:"role"`
	Suggestion string `json:"suggestion,omitempty"`
}

type workspaceMoveCheck struct {
	To       string                 `json:"to"`
	Release  string                 `json:"release"`
	People   []workspaceMovePerson  `json:"people"`
	Accounts []workspaceMoveAccount `json:"accounts"`
	// Kept is what doesn't go: the pages here no one moving reads, and the
	// edit history.
	PagesHere int `json:"pagesHere"`
}

// workspaceMoveReport is what a move says it carried, here and there.
type workspaceMoveReport struct {
	Here  workspaceMoveCounts `json:"here"`
	There workspaceMoveCounts `json:"there"`
}

type workspaceMoveCounts struct {
	Pages    int `json:"pages"`
	Files    int `json:"files"`
	Comments int `json:"comments"`
}

type workspaceMoveOutStatus struct {
	State     string               `json:"state"`
	Step      string               `json:"step,omitempty"`
	MovedTo   string               `json:"movedTo,omitempty"`
	ErrorCode string               `json:"errorCode,omitempty"`
	Report    *workspaceMoveReport `json:"report,omitempty"`
}

func (out *workspaceMoveOut) read(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	status := readWorkspaceMoveOut(event.App, workspaceID)
	if _, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceRead); err != nil {
		// A guest hears it too, and where the pages they were given went; no more.
		if _, guestErr := readGuestWorkspaceChangeSnapshot(event.App, user.Id, workspaceID); guestErr != nil {
			return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
		}
		return event.JSON(http.StatusOK, workspaceMoveOutStatus{State: status.State, MovedTo: status.MovedTo})
	}
	return event.JSON(http.StatusOK, status)
}

func readWorkspaceMoveOut(app core.App, workspaceID string) workspaceMoveOutStatus {
	stored := struct {
		MovedTo   string `db:"moved_to"`
		State     string `db:"state"`
		Step      string `db:"step"`
		ErrorCode string `db:"error_code"`
		Report    string `db:"report_json"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[moved_to]], [[state]], [[step]], [[error_code]], [[report_json]]
		FROM {{leafen_workspace_moves_out}} WHERE [[workspace_id]] = {:id}`).Bind(dbx.Params{"id": workspaceID}).One(&stored); err != nil {
		return workspaceMoveOutStatus{State: "none"}
	}
	status := workspaceMoveOutStatus{State: stored.State, Step: stored.Step, MovedTo: stored.MovedTo, ErrorCode: stored.ErrorCode}
	report := workspaceMoveReport{}
	if json.Unmarshal([]byte(stored.Report), &report) == nil && (report.Here.Pages > 0 || report.There.Pages > 0) {
		status.Report = &report
	}
	return status
}

type workspaceMoveOutInput struct {
	Code string `json:"code"`
	// Pairs are the accounts there the owner confirmed, by person here.
	Pairs map[string]string `json:"pairs"`
}

// owner is the workspace a request names and its decoded code, when its
// owner asks with a code that is open.
func (out *workspaceMoveOut) owner(event *core.RequestEvent, input *workspaceMoveOutInput) (workspaceAuthorization, serverMoveCode, bool) {
	user, ok := authenticatedUser(event)
	if !ok {
		_ = apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
		return workspaceAuthorization{}, serverMoveCode{}, false
	}
	if event.BindBody(input) != nil {
		_ = apiError(event, http.StatusBadRequest, errorCodeWorkspaceMoveCodeInvalid, "That isn't a workspace move code.")
		return workspaceAuthorization{}, serverMoveCode{}, false
	}
	authorization, err := authorizeWorkspace(event.App, user.Id, strings.TrimSpace(event.Request.PathValue("workspaceId")), permissionWorkspaceOwner)
	if err != nil {
		_ = apiError(event, http.StatusForbidden, errorCodePermissionDenied, "Only the workspace's owner moves a workspace.")
		return workspaceAuthorization{}, serverMoveCode{}, false
	}
	if strings.TrimSpace(input.Code) == "" {
		_ = refuseMissingInput(event, errorCodeWorkspaceMoveCodeInvalid, "code", "the workspace move code the workspace it moves to shows.")
		return workspaceAuthorization{}, serverMoveCode{}, false
	}
	code, err := decodeMoveCode(workspaceMoveCodePrefix, input.Code, time.Now())
	if err != nil {
		_ = apiError(event, http.StatusBadRequest, errorCodeWorkspaceMoveCodeInvalid,
			"That isn't a workspace move code, or it ran out; make a new one in the workspace it moves to.")
		return workspaceAuthorization{}, serverMoveCode{}, false
	}
	if workspaceMoveInOf(event.App, out.jobs).madeHere(code) {
		_ = apiError(event, http.StatusBadRequest, errorCodeWorkspaceMoveSameServer,
			"That code is from this server, where this workspace already is; a workspace moves only to another server.")
		return workspaceAuthorization{}, serverMoveCode{}, false
	}
	return authorization, code, true
}

// check reads the new server and offers each person here an account there.
func (out *workspaceMoveOut) check(event *core.RequestEvent) error {
	input := workspaceMoveOutInput{}
	authorization, code, ok := out.owner(event, &input)
	if !ok {
		return nil
	}
	check, _, err := workspaceMoveCheckOf(event.Request.Context(), event.App, authorization, code)
	if errors.Is(err, errWorkspaceMoveCodeRefused) {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceMoveCodeInvalid, "That move code isn't open any more; make a new one there.")
	}
	if err != nil {
		return apiErrorWithCause(event, http.StatusBadGateway, errorCodeWorkspaceMoveFailed,
			"The server the code is from didn't answer; check its address is reachable from here.", "workspace_move", "check", errorCodeWorkspaceMoveFailed, err)
	}
	return event.JSON(http.StatusOK, check)
}

// workspaceMoveCheckOf asks the new server, at the address that answers.
func workspaceMoveCheckOf(ctx context.Context, app core.App, authorization workspaceAuthorization, code serverMoveCode) (workspaceMoveCheck, string, error) {
	answer := workspaceMoveAccountsAnswer{}
	reached := ""
	var lastErr error
	for _, origin := range []string{code.To, code.Also} {
		if origin == "" {
			continue
		}
		if answer, lastErr = workspaceMoveAccountsAt(ctx, code, origin); lastErr == nil {
			reached = origin
			break
		}
	}
	if reached == "" {
		return workspaceMoveCheck{}, "", errors.Join(errors.New("no answer"), lastErr)
	}
	schema := 0
	_ = app.DB().NewQuery(`SELECT COALESCE(MAX([[version]]), 0) FROM {{leafen_schema_migrations}}`).Row(&schema)
	if answer.SchemaVersion < schema {
		return workspaceMoveCheck{}, "", errors.New("the new server is older than this one; update it first")
	}
	people, err := workspaceMovePeopleOf(app, authorization)
	if err != nil {
		return workspaceMoveCheck{}, "", err
	}
	byEmail := map[string]string{}
	for _, account := range answer.Accounts {
		if account.Email != "" {
			byEmail[strings.ToLower(account.Email)] = account.ID
		}
	}
	for index := range people {
		people[index].Suggestion = byEmail[strings.ToLower(people[index].Email)]
	}
	pages, _, _ := countWorkspaceMoveContents(app, authorization.WorkspaceID)
	return workspaceMoveCheck{To: reached, Release: answer.Release, People: people, Accounts: answer.Accounts, PagesHere: pages}, reached, nil
}

type workspaceMoveAccountsAnswer struct {
	Release       string                 `json:"release"`
	SchemaVersion int                    `json:"schemaVersion"`
	State         string                 `json:"state"`
	Accounts      []workspaceMoveAccount `json:"accounts"`
	NextCursor    string                 `json:"nextCursor"`
}

// workspaceMoveAccountsAt reads the new server's accounts at one address, a
// window at a time to the last.
func workspaceMoveAccountsAt(ctx context.Context, code serverMoveCode, origin string) (workspaceMoveAccountsAnswer, error) {
	all := workspaceMoveAccountsAnswer{}
	for cursor, first := "", true; first || cursor != ""; first = false {
		path := "/accounts"
		if cursor != "" {
			path += "?cursor=" + url.QueryEscape(cursor)
		}
		response, err := workspaceMoveAsk(ctx, code, origin, http.MethodGet, path, nil, "", 30*time.Second)
		if err != nil {
			return all, err
		}
		if response.StatusCode != http.StatusOK {
			err = workspaceMoveError(response)
			_ = response.Body.Close()
			return all, err
		}
		answer := workspaceMoveAccountsAnswer{}
		err = json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&answer)
		_ = response.Body.Close()
		if err != nil {
			return all, err
		}
		accounts := append(all.Accounts, answer.Accounts...)
		all, all.Accounts = answer, accounts
		// A cursor that doesn't move on ends the list rather than ask again.
		if answer.NextCursor == cursor {
			break
		}
		cursor = answer.NextCursor
	}
	all.NextCursor = ""
	return all, nil
}

// workspaceMovePeopleOf is the workspace's people: its owner and members.
func workspaceMovePeopleOf(app core.App, authorization workspaceAuthorization) ([]workspaceMovePerson, error) {
	people := []workspaceMovePerson{}
	err := app.DB().NewQuery(`
		SELECT [[user]].[[id]], [[user]].[[name]], lower([[user]].[[email]]) AS [[email]], 'owner' AS [[role]]
		FROM {{users}} [[user]] WHERE [[user]].[[id]] = {:owner}
		UNION
		SELECT [[user]].[[id]], [[user]].[[name]], lower([[user]].[[email]]) AS [[email]], [[member]].[[role]]
		FROM {{leafen_workspace_memberships}} [[member]] JOIN {{users}} [[user]] ON [[user]].[[id]] = [[member]].[[user_id]]
		WHERE [[member]].[[workspace_id]] = {:workspaceId} AND [[member]].[[user_id]] != {:owner}
	`).Bind(dbx.Params{"owner": authorization.WorkspaceOwnerID, "workspaceId": authorization.WorkspaceID}).All(&people)
	// The owner first, then by name.
	sort.SliceStable(people, func(i, j int) bool {
		if (people[i].Role == "owner") != (people[j].Role == "owner") {
			return people[i].Role == "owner"
		}
		return people[i].Name < people[j].Name
	})
	return people, err
}

// start checks the code again, stops the workspace's changes and moves it.
func (out *workspaceMoveOut) start(event *core.RequestEvent) error {
	input := workspaceMoveOutInput{}
	authorization, code, ok := out.owner(event, &input)
	if !ok {
		return nil
	}
	check, reached, err := workspaceMoveCheckOf(event.Request.Context(), event.App, authorization, code)
	if errors.Is(err, errWorkspaceMoveCodeRefused) {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceMoveCodeInvalid, "That move code isn't open any more; make a new one there.")
	}
	if err != nil {
		return apiErrorWithCause(event, http.StatusBadGateway, errorCodeWorkspaceMoveFailed,
			"The server the code is from didn't answer; check its address is reachable from here.", "workspace_move", "check", errorCodeWorkspaceMoveFailed, err)
	}
	accounts := map[string]bool{}
	for _, account := range check.Accounts {
		accounts[account.ID] = true
	}
	people := []workspaceImportPerson{}
	members := []string{}
	for _, person := range check.People {
		target := input.Pairs[person.ID]
		if target != "" && !accounts[target] {
			return apiError(event, http.StatusBadRequest, errorCodeWorkspaceMoveFailed, "A person was matched with an account the new server didn't offer.")
		}
		people = append(people, workspaceImportPerson{SourceID: person.ID, TargetID: target, Name: person.Name, Email: person.Email})
		if target != "" {
			members = append(members, target)
		}
	}
	// A key's call asks its person once the new server answered, every
	// person's account is one it offered and the workspace isn't moving
	// already (user_key_acting_ask.go).
	if !out.claim(authorization.WorkspaceID, true) {
		return apiError(event, http.StatusConflict, errorCodeWorkspaceMoveFailed, "This workspace is moving now.")
	}
	if asked, err := askBeforeActing(event); asked {
		return err
	}
	if !out.claim(authorization.WorkspaceID, false) {
		return apiError(event, http.StatusConflict, errorCodeWorkspaceMoveFailed, "This workspace is moving now.")
	}
	if err := setWorkspaceMove(event.App, authorization.WorkspaceID, authorization.PrincipalID, reached, workspaceMoveSending, "export", "", nil); err != nil {
		out.done(authorization.WorkspaceID)
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeWorkspaceMoveFailed, "Couldn't start the move.", err)
	}
	if !serverBackgroundOf(event.App).run("workspace move", func(ctx context.Context) { out.move(ctx, authorization, code, reached, people, members) }) {
		out.done(authorization.WorkspaceID)
	}
	return event.JSON(http.StatusAccepted, readWorkspaceMoveOut(event.App, authorization.WorkspaceID))
}

// claim marks the workspace as moving, false when it moves already; only
// looking, it marks nothing.
func (out *workspaceMoveOut) claim(workspaceID string, onlyLooking bool) bool {
	out.mu.Lock()
	defer out.mu.Unlock()
	if out.busy[workspaceID] {
		return false
	}
	if !onlyLooking {
		out.busy[workspaceID] = true
	}
	return true
}

func (out *workspaceMoveOut) done(workspaceID string) {
	out.mu.Lock()
	delete(out.busy, workspaceID)
	out.mu.Unlock()
}

// move makes the bundle, sends it with the people, and waits for the new
// server to bring it in. A failure lifts the fence: the workspace is as it was.
// A server stopping (ctx) is no failure: the move stays as it stands, for the
// server's next start to go on with (workspace_move_resume.go).
func (out *workspaceMoveOut) move(ctx context.Context, authorization workspaceAuthorization, code serverMoveCode, origin string,
	people []workspaceImportPerson, members []string) {
	app := out.app
	workspaceID, actorID := authorization.WorkspaceID, authorization.PrincipalID
	defer out.done(workspaceID)
	step := func(name string) {
		_ = setWorkspaceMove(app, workspaceID, actorID, origin, workspaceMoveSending, name, "", nil)
	}
	failWith := func(stage, code string, err error) {
		if ctx.Err() != nil {
			return
		}
		app.Logger().Warn("workspace move: it stopped", "workspaceId", workspaceID, "stage", stage, "error", err)
		_ = setWorkspaceMove(app, workspaceID, actorID, origin, workspaceMoveFailed, stage, code, nil)
	}
	fail := func(stage string, err error) { failWith(stage, errorCodeWorkspaceMoveFailed, err) }
	// Changes on their way when the fence went up land before the bundle.
	waitUnlessStopped(ctx, serverMoveSettle)
	jobID, err := out.export(ctx, actorID, workspaceID)
	if err != nil {
		fail("export", err)
		return
	}
	step("send")
	artifact, file, err := openWorkspaceExportArtifact(app, jobID, actorID, workspaceID)
	if err != nil {
		fail("export", err)
		return
	}
	bundlePath := file.Name()
	response, err := workspaceMoveAsk(ctx, code, origin, http.MethodPut, "/bundle", file, "application/vnd.leafen.archive+zip", 6*time.Hour)
	_ = file.Close()
	if err != nil {
		fail("send", err)
		return
	}
	sent := struct {
		SHA256 string `json:"sha256"`
		Bytes  int64  `json:"bytes"`
	}{}
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&sent) != nil ||
		sent.SHA256 != artifact.SHA256 || sent.Bytes != artifact.Bytes {
		err := workspaceMoveError(response)
		_ = response.Body.Close()
		fail("send", err)
		return
	}
	_ = response.Body.Close()
	history, err := out.sendHistoryTo(ctx, code, origin, "/history", workspaceID, bundlePath)
	if err != nil {
		fail("send", err)
		return
	}
	historyFiles := []workspaceMoveHistoryFile{}
	if history != "" {
		if historyFiles, err = out.sendHistoryFiles(ctx, code, origin, "", workspaceID, bundlePath); err != nil {
			fail("send", err)
			return
		}
	}
	// Handed over, it goes on when this server starts again (workspace_move_resume.go).
	if err := keepWorkspaceMoveOutCode(app, workspaceID, code, origin); err != nil {
		fail("bring-in", err)
		return
	}
	step("bring-in")
	access, err := collectWorkspaceMoveAccess(app, workspaceID)
	if err != nil {
		fail("bring-in", err)
		return
	}
	response, err = workspaceMoveAsk(ctx, code, origin, http.MethodPost, "/finish",
		workspaceMoveFinishInput{SHA256: sent.SHA256, History: history, People: people, Members: members,
			HistoryFiles: historyFiles, Access: &access}, "", time.Minute)
	if err != nil {
		fail("bring-in", err)
		return
	}
	if response.StatusCode != http.StatusAccepted {
		err := workspaceMoveError(response)
		_ = response.Body.Close()
		fail("bring-in", err)
		return
	}
	_ = response.Body.Close()
	out.awaitBringIn(ctx, workspaceID, actorID, code, origin)
}
