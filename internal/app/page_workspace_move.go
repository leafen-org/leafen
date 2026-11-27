package app

import (
	"encoding/json"
	"errors"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// A page moved to another workspace on the same server moves there, as the
// user decided on 2026-11-23 ("진짜 옮기기로"), beyond Notion, which copies it
// (its help "Move & duplicate content between workspaces", read
// 2026-11-23): the page, the pages under it, their databases, rows, data
// sources, views, templates, comments, history, favorites and files keep
// their IDs and change only their workspace, so every address and API ID
// they had still opens them. What can't come along is cut, as a copy there
// leaves it out: a relation to a database left behind, both of its sides,
// with the rollups through it (into Deleted properties, where a move back
// finds them); the old workspace's shares, teamspace roles, access requests
// and public links; other people's favorites where they aren't members of
// the new workspace. A linked view of a database on the other side of the
// move, either way, waits there for its source as a copy's does
// (page_duplication_unlinked_views.go); unlike a copy's, it keeps naming that
// database, so it shows it again once the page or the database moves back. The person sees what will be cut, and
// those linked views, as counts, before they confirm (a dry run, or the
// plan in the first answer).
//
// The move holds its pages while it goes (pageBusyWorkspaceMoving): they
// answer as being moved to whoever may open them, and take no edits. It
// changes their rows a window at a time, table by table
// (page_workspace_move_tables.go), each window a short write, so other
// pages' edits never wait long; it goes on after a restart where it stood,
// and a failure takes every row back the way it came before anything is cut.
// Its last step cuts what is cut and places the page where it was asked to
// go, at once. A move to another server stays a copy
// (page_duplication_workspaces.go), as does a database's page moved alone.

const (
	pageWorkspaceMoveJobKind = "page-workspace-move"

	pageWorkspaceMoveStartPath = "/pages/{pageId}/workspace-move"
	pageWorkspaceMoveReadPath  = "/page-workspace-moves/{jobId}"

	pageWorkspaceMovePhaseMoving      = "moving"
	pageWorkspaceMovePhaseRollingBack = "rolling_back"
	pageWorkspaceMovePhaseCompleted   = "completed"
	pageWorkspaceMovePhaseFailed      = "failed"

	pageWorkspaceMoveStatusPlanned   = "planned"
	pageWorkspaceMoveStatusPending   = "pending"
	pageWorkspaceMoveStatusCompleted = "completed"
	pageWorkspaceMoveStatusFailed    = "failed"

	errorCodePageWorkspaceMoveInvalid         = "LEAFEN_PAGE_WORKSPACE_MOVE_INVALID"
	errorCodePageWorkspaceMoveNotFound        = "LEAFEN_PAGE_WORKSPACE_MOVE_NOT_FOUND"
	errorCodePageWorkspaceMoveConflict        = "LEAFEN_PAGE_WORKSPACE_MOVE_CONFLICT"
	errorCodePageWorkspaceMoveRequestIDReused = "LEAFEN_PAGE_WORKSPACE_MOVE_REQUEST_ID_REUSED"
	errorCodePageWorkspaceMoveUnavailable     = "LEAFEN_PAGE_WORKSPACE_MOVE_UNAVAILABLE"
	// errorCodePageWorkspaceMoveAccessLost ends a move whose mover can no
	// longer write where it goes, or no longer has full access to its page.
	errorCodePageWorkspaceMoveAccessLost = "LEAFEN_PAGE_WORKSPACE_MOVE_ACCESS_LOST"

	pageWorkspaceMoveMaxAttempts = 8
)

var (
	errPageWorkspaceMoveInvalid         = errors.New(errorCodePageWorkspaceMoveInvalid)
	errPageWorkspaceMoveNotFound        = errors.New(errorCodePageWorkspaceMoveNotFound)
	errPageWorkspaceMoveConflict        = errors.New(errorCodePageWorkspaceMoveConflict)
	errPageWorkspaceMoveRequestIDReused = errors.New(errorCodePageWorkspaceMoveRequestIDReused)
	errPageWorkspaceMoveUnavailable     = errors.New(errorCodePageWorkspaceMoveUnavailable)
)

type pageWorkspaceMoveInput struct {
	RequestID              string `json:"requestId"`
	DestinationWorkspaceID string `json:"destinationWorkspaceId"`
	DestinationTeamspaceID string `json:"destinationTeamspaceId,omitempty"`
	DestinationParentID    string `json:"destinationParentId,omitempty"`
	DryRun                 bool   `json:"dryRun,omitempty"`
}

// pageWorkspaceMovePlan is what a move takes and what it cuts, counted
// before it starts and kept with it.
type pageWorkspaceMovePlan struct {
	Pages     int                   `json:"pages"`
	Databases int                   `json:"databases"`
	Rows      int                   `json:"rows"`
	Comments  int                   `json:"comments"`
	Files     int                   `json:"files"`
	Cut       pageWorkspaceMoveCuts `json:"cut"`
	Across    pageWorkspaceMoveGaps `json:"across"`
}

// pageWorkspaceMoveCuts is what the move ends.
type pageWorkspaceMoveCuts struct {
	// Relations to databases left behind, counted on both sides, and the
	// values they held.
	RelationProperties int `json:"relationProperties"`
	RelationValues     int `json:"relationValues"`
	Rollups            int `json:"rollups"`
	// People who open the page in the old workspace now and won't through it.
	// PeopleLosingAccessAtLeast says the count stopped at the people the plan
	// asks about (pageWorkspaceMovePeopleAsked): at least that many lose it.
	PeopleLosingAccess        int  `json:"peopleLosingAccess"`
	PeopleLosingAccessAtLeast bool `json:"peopleLosingAccessAtLeast,omitempty"`
	Shares                    int  `json:"shares"`
	PublicLinks               int  `json:"publicLinks"`
	GuestLinks                int  `json:"guestLinks"`
	Favorites                 int  `json:"favorites"`
}

// pageWorkspaceMoveGaps is what keeps pointing across the two workspaces:
// mentions and files, which whoever can't open the other workspace doesn't
// see, and linked views of a database on the other side, which no one sees
// through: each waits for its source ("데이터 소스 연결").
type pageWorkspaceMoveGaps struct {
	MentionsOut    int `json:"mentionsOut"`
	MentionsIn     int `json:"mentionsIn"`
	LinkedViewsOut int `json:"linkedViewsOut"`
	LinkedViewsIn  int `json:"linkedViewsIn"`
	SharedFiles    int `json:"sharedFiles"`
}

type storedPageWorkspaceMove struct {
	JobID                  string `db:"job_id"`
	ActorID                string `db:"actor_id"`
	RequestHash            string `db:"request_hash"`
	RootPageID             string `db:"root_page_id"`
	RootParentID           string `db:"root_parent_id"`
	SourceWorkspaceID      string `db:"source_workspace_id"`
	SourceOwnerID          string `db:"source_owner_id"`
	TargetWorkspaceID      string `db:"target_workspace_id"`
	TargetOwnerID          string `db:"target_owner_id"`
	DestinationParentID    string `db:"destination_parent_id"`
	DestinationTeamspaceID string `db:"destination_teamspace_id"`
	Phase                  string `db:"phase"`
	Lane                   int    `db:"lane"`
	Cursor                 string `db:"cursor"`
	Revision               int    `db:"revision"`
	PageCount              int    `db:"page_count"`
	MovedRows              int    `db:"moved_rows"`
	PlanJSON               string `db:"plan_json"`
	CutJSON                string `db:"cut_json"`
	ErrorCode              string `db:"error_code"`
	CreatedAt              int64  `db:"created_at"`
	UpdatedAt              int64  `db:"updated_at"`
	CompletedAt            int64  `db:"completed_at"`
}

func loadPageWorkspaceMove(app core.App, jobID string) (storedPageWorkspaceMove, error) {
	move := storedPageWorkspaceMove{}
	err := app.DB().NewQuery(`SELECT * FROM {{leafen_page_workspace_moves}} WHERE [[job_id]]={:jobId}`).
		Bind(dbx.Params{"jobId": jobID}).One(&move)
	return move, err
}

func (move storedPageWorkspaceMove) active() bool {
	return move.Phase == pageWorkspaceMovePhaseMoving || move.Phase == pageWorkspaceMovePhaseRollingBack
}

func (move storedPageWorkspaceMove) ownersDiffer() bool {
	return move.SourceOwnerID != move.TargetOwnerID
}

type pageWorkspaceMoveResponse struct {
	JobID             string                 `json:"jobId,omitempty"`
	Status            string                 `json:"status"`
	Phase             string                 `json:"phase,omitempty"`
	RootPageID        string                 `json:"rootPageId"`
	SourceWorkspaceID string                 `json:"sourceWorkspaceId"`
	TargetWorkspaceID string                 `json:"targetWorkspaceId"`
	Plan              pageWorkspaceMovePlan  `json:"plan"`
	Cut               *pageWorkspaceMoveCuts `json:"cut,omitempty"`
	ErrorCode         string                 `json:"errorCode,omitempty"`
	Duplicate         bool                   `json:"duplicate,omitempty"`
}

func pageWorkspaceMoveResponseOf(move storedPageWorkspaceMove, duplicate bool) pageWorkspaceMoveResponse {
	response := pageWorkspaceMoveResponse{
		JobID: move.JobID, Phase: move.Phase, RootPageID: move.RootPageID,
		SourceWorkspaceID: move.SourceWorkspaceID, TargetWorkspaceID: move.TargetWorkspaceID,
		ErrorCode: move.ErrorCode, Duplicate: duplicate,
	}
	_ = json.Unmarshal([]byte(move.PlanJSON), &response.Plan)
	switch move.Phase {
	case pageWorkspaceMovePhaseCompleted:
		response.Status = pageWorkspaceMoveStatusCompleted
		cut := pageWorkspaceMoveCuts{}
		if json.Unmarshal([]byte(move.CutJSON), &cut) == nil {
			response.Cut = &cut
		}
	case pageWorkspaceMovePhaseFailed:
		response.Status = pageWorkspaceMoveStatusFailed
	default:
		response.Status = pageWorkspaceMoveStatusPending
	}
	return response
}

// pageWorkspaceMoveHeldPage reports whether a move under way holds the
// page; the fences look for it only where a workspace has one going out or
// in (workspacePageFences).
func pageWorkspaceMoveHeldPage(app core.App, pageID string) (bool, error) {
	var held int
	err := app.DB().NewQuery(`
		SELECT EXISTS(SELECT 1 FROM {{leafen_page_workspace_move_items}} AS [[item]]
			JOIN {{leafen_page_workspace_moves}} AS [[move]] ON [[move]].[[job_id]] = [[item]].[[job_id]]
			WHERE [[item]].[[item_id]] = {:pageId} AND [[item]].[[kind]] = 'page'
				AND [[move]].[[phase]] IN ('moving','rolling_back'))
	`).Bind(dbx.Params{"pageId": pageID}).Row(&held)
	return held == 1, err
}
