package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A workspace moved to another server stays here reading only, and what it
// made public stays public here (user decision 2026-11-07): its public links
// and published sites keep answering until the same page is made public on
// the new server, and then they close here on their own, so a link never
// goes dark and the same page is not public in two places for long. The old
// server asks, in the direction the move itself went: it keeps the move's
// code once the move is done, and every minute, while anything here is
// still public, it asks the new server which of those pages are public
// there (POST …/workspace-move/in/{code}/public), then takes each of them
// down here as the workspace's owner would. Nothing else of the workspace
// changes: its owner may still turn any of them off by hand in Settings ›
// Public pages, which the move's fence lets through (public_mutation).
//
// When the new server can't be reached, nothing changes here and it is
// asked again later, waiting longer each time (a minute, doubling, at most
// an hour); a server starting again asks at once. When the new server no
// longer holds the move (it answers 401: the move there was closed or its
// workspace deleted), this server forgets the code and asks no more; the
// pages still public here stay so, for their owner to turn off.

const (
	workspaceMoveFollowMaxWait = time.Hour
	workspaceMoveFollowBatch   = 500
)

// workspaceMoveFollowEvery is how often the follow-up looks; tests look sooner.
var workspaceMoveFollowEvery = time.Minute

// workspaceMovePublicInput is the pages the old server asks about.
type workspaceMovePublicInput struct {
	Pages []string `json:"pages"`
}

// workspaceMovePublicAnswer is which of them are public here.
type workspaceMovePublicAnswer struct {
	Public []string `json:"public"`
}

// public tells the old server which of the pages it names the moved
// workspace has made public here: a link anyone with it opens, or a
// published site. It answers only once the workspace is in, and only about
// that workspace's pages.
func (in *workspaceMoveIn) public(event *core.RequestEvent) error {
	in.mu.Lock()
	slot := in.held(event)
	var workspaceID string
	if slot != nil {
		workspaceID = slot.workspaceID
	}
	in.mu.Unlock()
	if slot == nil {
		return workspaceMoveRefused(event)
	}
	if status := in.project(event.App, workspaceID, slot); status.JobStatus != durableJobStatusCompleted {
		return apiError(event, http.StatusConflict, errorCodeWorkspaceMoveFailed, "The workspace isn't in yet.")
	}
	input := workspaceMovePublicInput{}
	if event.BindBody(&input) != nil || len(input.Pages) > workspaceMoveFollowBatch {
		return apiError(event, http.StatusBadRequest, errorCodeWorkspaceMoveFailed, "Name at most 500 pages.")
	}
	answer := workspaceMovePublicAnswer{Public: []string{}}
	if len(input.Pages) == 0 {
		return event.JSON(http.StatusOK, answer)
	}
	params := dbx.Params{"workspaceId": workspaceID}
	names := []string{}
	for index, id := range input.Pages {
		if id = strings.TrimSpace(id); id != "" {
			name := "page" + strconv.Itoa(index)
			params[name] = id
			names = append(names, "{:"+name+"}")
		}
	}
	public := []string{}
	if len(names) == 0 {
		return event.JSON(http.StatusOK, answer)
	}
	if err := event.App.DB().NewQuery(`SELECT DISTINCT [[page_id]] FROM (` + workspaceMovePublicPagesQuery + `)
		WHERE [[page_id]] IN (` + strings.Join(names, ", ") + `)`).Bind(params).Column(&public); err != nil {
		return apiErrorWithCause(event, http.StatusInternalServerError, errorCodeWorkspaceMoveFailed, "Couldn't read what is public here.",
			"workspace_move", "public", errorCodeWorkspaceMoveFailed, err)
	}
	answer.Public = public
	return event.JSON(http.StatusOK, answer)
}

// workspaceMovePublicPagesQuery is a workspace's pages public as a link or
// a site, out of the trash with every page above them (pageOutOfTrashPathSQL).
var workspaceMovePublicPagesQuery = `
	SELECT [[link]].[[root_page_id]] AS [[page_id]] FROM {{leafen_public_share_links}} [[link]]
		JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[link]].[[root_page_id]] AND ` + pageOutOfTrashPathSQL("[[page]].[[id]]") + `
		WHERE [[link]].[[workspace_id]] = {:workspaceId} AND [[link]].[[web_enabled]] = 1
	UNION
	SELECT [[site]].[[root_page_id]] FROM {{leafen_public_sites}} [[site]]
		JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]] = [[site]].[[root_page_id]] AND ` + pageOutOfTrashPathSQL("[[page]].[[id]]") + `
		WHERE [[site]].[[workspace_id]] = {:workspaceId} AND [[site]].[[published]] = 1`

// workspaceMoveFollowing is when each moved workspace is asked about next.
type workspaceMoveFollowing struct {
	mu   sync.Mutex
	next map[string]time.Time
	wait map[string]time.Duration
}

// follow asks, every minute, about each moved workspace with something
// still public here, until the server stops (server_background.go).
func (out *workspaceMoveOut) follow(ctx context.Context) {
	following := &workspaceMoveFollowing{next: map[string]time.Time{}, wait: map[string]time.Duration{}}
	for {
		out.followOnce(ctx, following, time.Now())
		if !waitUnlessStopped(ctx, workspaceMoveFollowEvery) {
			return
		}
	}
}

func (out *workspaceMoveOut) followOnce(ctx context.Context, following *workspaceMoveFollowing, now time.Time) {
	app := out.app
	rows := []struct {
		WorkspaceID string `db:"workspace_id"`
		Code        string `db:"code_json"`
		Origin      string `db:"origin"`
	}{}
	if err := app.DB().NewQuery(`SELECT [[move]].[[workspace_id]], [[code]].[[code_json]], [[code]].[[origin]]
		FROM {{leafen_workspace_moves_out}} [[move]]
		JOIN {{leafen_workspace_move_out_codes}} [[code]] ON [[code]].[[workspace_id]] = [[move]].[[workspace_id]]
		WHERE [[move]].[[state]] = {:moved}`).Bind(dbx.Params{"moved": workspaceMoveMoved}).All(&rows); err != nil {
		return
	}
	for _, row := range rows {
		following.mu.Lock()
		due := !now.Before(following.next[row.WorkspaceID])
		following.mu.Unlock()
		if !due {
			continue
		}
		code := serverMoveCode{}
		if json.Unmarshal([]byte(row.Code), &code) != nil {
			continue
		}
		err := out.followWorkspace(ctx, row.WorkspaceID, code, row.Origin)
		following.mu.Lock()
		if err == nil {
			delete(following.wait, row.WorkspaceID)
			delete(following.next, row.WorkspaceID)
		} else {
			wait := min(max(following.wait[row.WorkspaceID]*2, workspaceMoveFollowEvery), workspaceMoveFollowMaxWait)
			following.wait[row.WorkspaceID] = wait
			following.next[row.WorkspaceID] = now.Add(wait)
			app.Logger().Debug("workspace move: couldn't ask the new server what is public", "workspaceId", row.WorkspaceID, "error", err)
		}
		following.mu.Unlock()
	}
}

// followWorkspace asks the new server about the pages still public here and
// takes down each one public there.
func (out *workspaceMoveOut) followWorkspace(ctx context.Context, workspaceID string, code serverMoveCode, origin string) error {
	app := out.app
	pages := []string{}
	if err := app.DB().NewQuery(workspaceMovePublicPagesQuery).Bind(dbx.Params{"workspaceId": workspaceID}).Column(&pages); err != nil {
		return err
	}
	for start := 0; start < len(pages); start += workspaceMoveFollowBatch {
		batch := pages[start:min(start+workspaceMoveFollowBatch, len(pages))]
		response, err := workspaceMoveAsk(ctx, code, origin, http.MethodPost, "/public", workspaceMovePublicInput{Pages: batch}, "", 30*time.Second)
		if err != nil {
			return err
		}
		answer := workspaceMovePublicAnswer{}
		decodeErr := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&answer)
		_ = response.Body.Close()
		if response.StatusCode == http.StatusUnauthorized {
			// The new server holds the move no more: nothing is asked again.
			_ = forgetWorkspaceMoveOutCode(app, workspaceID)
			return nil
		}
		if response.StatusCode != http.StatusOK || decodeErr != nil {
			return workspaceMoveError(response)
		}
		asked := map[string]bool{}
		for _, id := range batch {
			asked[id] = true
		}
		for _, pageID := range answer.Public {
			if !asked[pageID] {
				continue
			}
			if err := turnOffMovedPublic(ctx, app, workspaceID, pageID); err != nil {
				app.Logger().Warn("workspace move: couldn't take down what is public there now", "workspaceId", workspaceID,
					"pageId", pageID, "error", err)
			}
		}
	}
	return nil
}

// turnOffMovedPublic takes a moved workspace's page's site down and turns
// its link off, as the workspace's owner would in the page's share menu.
func turnOffMovedPublic(ctx context.Context, app core.App, workspaceID, pageID string) error {
	var ownerID string
	// The person who owns the workspace, as the server's own actions on it take (administrator_workspace_content.go).
	if err := app.DB().NewQuery(`SELECT COALESCE(NULLIF([[workspace_owner]], ''), [[owner]]) FROM {{leafen_workspaces}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": workspaceID}).Row(&ownerID); err != nil {
		return err
	}
	var site, link int
	if err := app.DB().NewQuery(`SELECT
			(SELECT COUNT(*) FROM {{leafen_public_sites}} WHERE [[workspace_id]] = {:workspaceId} AND [[root_page_id]] = {:pageId} AND [[published]] = 1),
			(SELECT COUNT(*) FROM {{leafen_public_share_links}} WHERE [[workspace_id]] = {:workspaceId} AND [[root_page_id]] = {:pageId} AND [[web_enabled]] = 1)`).
		Bind(dbx.Params{"workspaceId": workspaceID, "pageId": pageID}).Row(&site, &link); err != nil {
		return err
	}
	off := false
	actions := []publicMutationInput{}
	if site > 0 {
		actions = append(actions, publicMutationInput{Action: publicMutationActionUnpublishSite})
	}
	if link > 0 {
		actions = append(actions, publicMutationInput{Action: publicMutationActionSetWebSharing, Enabled: &off})
	}
	for _, input := range actions {
		stored, err := loadStoredPublicMutationState(app, pageID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		input.RequestID, input.ExpectedRevision = "workspace-move-public:"+newDiagnosticReference(), stored.Revision
		if _, _, err := executePublicMutation(ctx, app, ownerID, pageID, input); err != nil {
			return err
		}
	}
	return nil
}
