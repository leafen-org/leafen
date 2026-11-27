package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// The move's road: POST /pages/{pageId}/workspace-move asks for one — with
// dryRun, only its plan, which the screen's confirmation and an agent show
// before asking — and GET /page-workspace-moves/{jobId} reads it, waiting
// with leafen_call's wait_seconds while it goes (mcp_call_wait.go). Only
// someone with full access to the page moves it, and only where they may add
// pages in the other workspace: under a page they can edit, at the top of a
// teamspace they can edit in, or at the top of their own private pages.

const pageWorkspaceMoveMaxBodyBytes = 4096

// errPageWorkspaceMoveDryRun lets go of a dry run's transaction.
var errPageWorkspaceMoveDryRun = errors.New("page workspace move dry run")

func startPageWorkspaceMove(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	input := pageWorkspaceMoveInput{}
	if err := event.BindBody(&input); err != nil {
		return pageWorkspaceMoveError(event, errPageWorkspaceMoveInvalid, "bind")
	}
	pageID := strings.TrimSpace(event.Request.PathValue("pageId"))
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.DestinationWorkspaceID = strings.TrimSpace(input.DestinationWorkspaceID)
	input.DestinationTeamspaceID = strings.TrimSpace(input.DestinationTeamspaceID)
	input.DestinationParentID = strings.TrimSpace(input.DestinationParentID)
	if pageID == "" || input.DestinationWorkspaceID == "" || (input.RequestID == "" && !input.DryRun) ||
		len([]rune(input.RequestID)) > maxPageDuplicationRequestIDRunes ||
		len(input.DestinationWorkspaceID) > maxWorkspacePermissionIdentifierLength ||
		len(input.DestinationTeamspaceID) > maxWorkspacePermissionIdentifierLength ||
		len(input.DestinationParentID) > maxWorkspacePermissionIdentifierLength ||
		(input.DestinationParentID != "" && input.DestinationTeamspaceID != "") {
		return pageWorkspaceMoveError(event, errPageWorkspaceMoveInvalid, "validate")
	}
	response, err := admitPageWorkspaceMove(event.App, user.Id, pageID, input)
	if err != nil {
		return pageWorkspaceMoveError(event, err, "admit")
	}
	if runner, ok := event.App.Store().Get(durableJobRunnerStoreKey).(*durableJobRunner); ok && !input.DryRun {
		runner.notify()
	}
	status := http.StatusAccepted
	if response.Status != pageWorkspaceMoveStatusPending {
		status = http.StatusOK
	}
	return event.JSON(status, response)
}

func readPageWorkspaceMove(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	move, err := loadPageWorkspaceMove(event.App, strings.TrimSpace(event.Request.PathValue("jobId")))
	if errors.Is(err, sql.ErrNoRows) || (err == nil && move.ActorID != user.Id) {
		return pageWorkspaceMoveError(event, errPageWorkspaceMoveNotFound, "read")
	}
	if err != nil {
		return pageWorkspaceMoveError(event, err, "read")
	}
	return event.JSON(http.StatusOK, pageWorkspaceMoveResponseOf(move, false))
}

func pageWorkspaceMoveError(event *core.RequestEvent, err error, stage string) error {
	if errors.Is(err, errOrganizationSharingSecurityBlocked) {
		return writeSharingSecurityBlockedError(event)
	}
	status, code, message := http.StatusServiceUnavailable, errorCodePageWorkspaceMoveUnavailable, "Couldn't move the page."
	switch {
	case errors.Is(err, errPageWorkspaceMoveInvalid):
		status, code, message = http.StatusBadRequest, errorCodePageWorkspaceMoveInvalid, "Check the move request."
	case errors.Is(err, errPageWorkspaceMoveNotFound), isPermissionDenied(err):
		status, code, message = http.StatusNotFound, errorCodePageWorkspaceMoveNotFound, "Page or workspace move not found."
	case errors.Is(err, errPageWorkspaceMoveRequestIDReused):
		status, code, message = http.StatusConflict, errorCodePageWorkspaceMoveRequestIDReused, "This request ID was used for a different move."
	case errors.Is(err, errPageWorkspaceMoveConflict):
		status, code, message = http.StatusConflict, errorCodePageWorkspaceMoveConflict, "The page can't be moved there now."
	}
	return writeAPIErrorWithReference(
		event, status, code, message, newDiagnosticReference(), "page_workspace_move", stage,
		code, err, diagnosticSourcePosition(1),
	)
}

func pageWorkspaceMoveRequestHash(pageID string, input pageWorkspaceMoveInput) string {
	digest := sha256.Sum256([]byte(strings.Join([]string{
		pageWorkspaceMoveJobKind, pageID, input.DestinationWorkspaceID, input.DestinationTeamspaceID, input.DestinationParentID,
	}, "\x00")))
	return hex.EncodeToString(digest[:])
}

// admitPageWorkspaceMove plans the move and, unless it is a dry run, starts
// it: its pages are held from then on.
func admitPageWorkspaceMove(app core.App, actorID, pageID string, input pageWorkspaceMoveInput) (pageWorkspaceMoveResponse, error) {
	response := pageWorkspaceMoveResponse{}
	requestHash := pageWorkspaceMoveRequestHash(pageID, input)
	audience, err := readPageWorkspaceMoveAudienceFor(app, actorID, pageID, input)
	if err != nil {
		return response, err
	}
	err = app.RunInTransaction(func(txApp core.App) error {
		if !input.DryRun {
			existing, err := loadStoredJobByRequest(txApp, actorID, input.RequestID)
			if err == nil {
				if existing.Kind != pageWorkspaceMoveJobKind || existing.RequestHash != requestHash {
					return errPageWorkspaceMoveRequestIDReused
				}
				move, err := loadPageWorkspaceMove(txApp, existing.ID)
				if err != nil {
					return err
				}
				response = pageWorkspaceMoveResponseOf(move, true)
				return nil
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		move, err := preparePageWorkspaceMove(txApp, actorID, pageID, input, requestHash)
		if err != nil {
			return err
		}
		if err := mapPageWorkspaceMoveItems(txApp, move); err != nil {
			return err
		}
		if err := refusePageWorkspaceMoveBusy(txApp, move); err != nil {
			return err
		}
		plan, err := countPageWorkspaceMovePlan(txApp, move, audience)
		if err != nil {
			return err
		}
		if err := storePageWorkspaceMovePlan(txApp, move.JobID, plan); err != nil {
			return err
		}
		if input.DryRun {
			response = pageWorkspaceMoveResponse{
				Status: pageWorkspaceMoveStatusPlanned, RootPageID: pageID,
				SourceWorkspaceID: move.SourceWorkspaceID, TargetWorkspaceID: move.TargetWorkspaceID, Plan: plan,
			}
			return errPageWorkspaceMoveDryRun
		}
		count := plan.Pages + plan.Rows
		now := time.Now().UTC().UnixMilli()
		if _, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_page_workspace_moves}} SET [[page_count]] = {:count} WHERE [[job_id]] = {:jobId}
		`).Bind(dbx.Params{"count": count, "jobId": move.JobID}).Execute(); err != nil {
			return err
		}
		if _, err := txApp.DB().NewQuery(`
			INSERT INTO {{leafen_jobs}}
				([[id]],[[owner]],[[workspace_id]],[[request_id]],[[request_hash]],[[kind]],[[page_id]],
				 [[status]],[[error_code]],[[diagnostic_reference]],[[not_before]],[[created]],[[updated]],
				 [[payload_json]],[[cursor]],[[processed_items]],[[total_items]],[[attempt]],[[max_attempts]],
				 [[chunk_count]],[[captured_bytes]])
			VALUES
				({:id},{:owner},{:workspaceId},{:requestId},{:requestHash},{:kind},{:pageId},
				 'queued','','',{:now},{:now},{:now},'{}','',0,{:count},0,{:maxAttempts},0,0)
		`).Bind(dbx.Params{
			"id": move.JobID, "owner": actorID, "workspaceId": move.TargetWorkspaceID,
			"requestId": input.RequestID, "requestHash": requestHash, "kind": pageWorkspaceMoveJobKind,
			"pageId": pageID, "now": now, "count": count, "maxAttempts": pageWorkspaceMoveMaxAttempts,
		}).Execute(); err != nil {
			return err
		}
		// Sockets open on its pages hear they are held.
		recheckCollaborationAccessAfterCommit(txApp)
		stored, err := loadPageWorkspaceMove(txApp, move.JobID)
		if err != nil {
			return err
		}
		response = pageWorkspaceMoveResponseOf(stored, false)
		return nil
	})
	if errors.Is(err, errPageWorkspaceMoveDryRun) {
		err = nil
	}
	return response, err
}

// readPageWorkspaceMoveAudienceFor reads, before the move's write, who opens
// the page now and who would lose it (readPageWorkspaceMoveAudience); none
// for a request asked before, which answers as it was, or a page the move's
// own checks refuse.
func readPageWorkspaceMoveAudienceFor(app core.App, actorID, pageID string, input pageWorkspaceMoveInput) (pageWorkspaceMoveAudience, error) {
	if !input.DryRun {
		if _, err := loadStoredJobByRequest(app, actorID, input.RequestID); err == nil {
			return pageWorkspaceMoveAudience{}, nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return pageWorkspaceMoveAudience{}, err
		}
	}
	page, err := app.FindRecordById(pagesCollection, pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return pageWorkspaceMoveAudience{}, nil
	}
	if err != nil {
		return pageWorkspaceMoveAudience{}, err
	}
	return readPageWorkspaceMoveAudience(app, storedPageWorkspaceMove{
		ActorID: actorID, RootPageID: pageID,
		SourceWorkspaceID: page.GetString("workspace"), SourceOwnerID: page.GetString("owner"),
		TargetWorkspaceID: input.DestinationWorkspaceID, DestinationParentID: input.DestinationParentID,
		DestinationTeamspaceID: input.DestinationTeamspaceID,
	})
}

// preparePageWorkspaceMove checks the move may go and writes its job: the
// page, with full access, not a database's page nor in the trash, to another
// workspace the mover may add pages to, where they asked.
func preparePageWorkspaceMove(
	app core.App, actorID, pageID string, input pageWorkspaceMoveInput, requestHash string,
) (storedPageWorkspaceMove, error) {
	// A database's item goes over with its database (other_workspace_refusal.go).
	if row, err := authorizeDatabaseRowPage(app, actorID, pageID, permissionWorkspaceRead); err == nil && row.WorkspaceID != input.DestinationWorkspaceID {
		return storedPageWorkspaceMove{}, otherWorkspaceRefused(app, "pageId", workspaceCrossing{
			subject: "this page, an item of a database,", from: row.WorkspaceID, place: "where it would go",
			to: input.DestinationWorkspaceID, whole: row.ContainerPageID,
		})
	}
	authorization, err := authorizePageAccessRole(app, actorID, pageID, pageAccessRoleFullAccess)
	if pageAccessHeld(err) != "" {
		return storedPageWorkspaceMove{}, errPageWorkspaceMoveConflict
	}
	if err != nil {
		return storedPageWorkspaceMove{}, err
	}
	page := authorization.Page
	source := authorization.Workspace
	if isDatabaseRowPageRecord(page) {
		return storedPageWorkspaceMove{}, errPageWorkspaceMoveConflict
	}
	if page.GetBool("in_trash") {
		return storedPageWorkspaceMove{}, refusedFor(http.StatusConflict, errorCodePageInTrash, "A page in Trash moves after it's restored.")
	}
	if page.GetBool("notion_import_staging") {
		return storedPageWorkspaceMove{}, errPageWorkspaceMoveConflict
	}
	if input.DestinationWorkspaceID == source.WorkspaceID {
		return storedPageWorkspaceMove{}, errPageWorkspaceMoveInvalid
	}
	target, err := authorizeWorkspace(app, actorID, input.DestinationWorkspaceID, permissionWorkspaceWrite)
	if isPermissionDenied(err) {
		return storedPageWorkspaceMove{}, destinationWorkspaceRefused(app, actorID, input.DestinationWorkspaceID, errorCodePageWorkspaceMoveInvalid)
	}
	if err != nil {
		return storedPageWorkspaceMove{}, err
	}
	if err := enforceSharingSecurityCrossWorkspaceCopy(app, source, target); err != nil {
		return storedPageWorkspaceMove{}, err
	}
	now := time.Now().UTC().UnixMilli()
	move := storedPageWorkspaceMove{
		JobID: core.GenerateDefaultRandomId(), ActorID: actorID, RequestHash: requestHash,
		RootPageID: pageID, RootParentID: page.GetString("parent_id"),
		SourceWorkspaceID: source.WorkspaceID, SourceOwnerID: source.OwnerID,
		TargetWorkspaceID: target.WorkspaceID, TargetOwnerID: target.OwnerID,
		DestinationParentID: input.DestinationParentID, DestinationTeamspaceID: input.DestinationTeamspaceID,
		Phase: pageWorkspaceMovePhaseMoving, PlanJSON: "{}", CutJSON: "{}", CreatedAt: now, UpdatedAt: now,
	}
	if err := pageWorkspaceMoveDestinationWritable(app, move, target); err != nil {
		return storedPageWorkspaceMove{}, err
	}
	_, err = app.DB().Insert(pageWorkspaceMovesTable, dbx.Params{
		"job_id": move.JobID, "actor_id": move.ActorID, "request_hash": move.RequestHash,
		"root_page_id": move.RootPageID, "root_parent_id": move.RootParentID,
		"source_workspace_id": move.SourceWorkspaceID, "source_owner_id": move.SourceOwnerID,
		"target_workspace_id": move.TargetWorkspaceID, "target_owner_id": move.TargetOwnerID,
		"destination_parent_id": move.DestinationParentID, "destination_teamspace_id": move.DestinationTeamspaceID,
		"phase": move.Phase, "created_at": now, "updated_at": now,
	}).Execute()
	return move, err
}

// pageWorkspaceMoveDestinationWritable checks the mover may add pages where
// the move goes in the other workspace.
func pageWorkspaceMoveDestinationWritable(app core.App, move storedPageWorkspaceMove, target workspaceAuthorization) error {
	switch {
	case move.DestinationParentID != "":
		parent, err := authorizePageBody(app, move.ActorID, move.DestinationParentID, permissionWorkspaceWrite)
		return destinationParentRefused(parent, err, target.WorkspaceID, errorCodePageWorkspaceMoveInvalid, true)
	case move.DestinationTeamspaceID != "":
		return destinationTeamspaceRefused(app, move.ActorID, target.WorkspaceID, move.DestinationTeamspaceID, errorCodePageWorkspaceMoveInvalid)
	default:
		refused, err := restrictedPrivatePagesRefused(app, target)
		if err == nil && refused {
			return destinationPrivatePagesRefused("destinationTeamspaceId")
		}
		return err
	}
}

// refusePageWorkspaceMoveBusy refuses a move whose pages another operation
// holds or is about to — a permanent deletion, a wiki's change, a copy, a
// block move, an import's replacement, another move — or that would take
// the workspace's People database.
func refusePageWorkspaceMoveBusy(app core.App, move storedPageWorkspaceMove) error {
	pages := pageWorkspaceMoveInBundle(pageWorkspaceMoveItemPage)
	var busy int
	if err := app.DB().NewQuery(`
		SELECT EXISTS(SELECT 1 FROM {{leafen_pages}} WHERE [[id]] IN ` + pages + ` AND (
				[[deletion_job_id]] != '' OR [[wiki_active_job_id]] != '' OR [[duplication_source_job_id]] != ''
				OR [[duplication_target_job_id]] != '' OR [[notion_import_staging]] = TRUE))
			OR EXISTS(SELECT 1 FROM {{leafen_block_move_jobs}} WHERE [[phase]] NOT IN ('completed','failed')
				AND ([[source_page_id]] IN ` + pages + ` OR [[destination_page_id]] IN ` + pages + `))
			OR EXISTS(SELECT 1 FROM {{leafen_workspace_import_replace_leases}} WHERE [[page_id]] IN ` + pages + `)
			OR EXISTS(SELECT 1 FROM {{leafen_workspace_people_databases}} WHERE [[page_id]] IN ` + pages + `)
			OR EXISTS(SELECT 1 FROM {{leafen_page_workspace_move_items}} AS [[other]]
				JOIN {{leafen_page_workspace_moves}} AS [[move]] ON [[move]].[[job_id]] = [[other]].[[job_id]]
				WHERE [[other]].[[job_id]] != {:jobId} AND [[other]].[[kind]] = 'page'
					AND [[move]].[[phase]] IN ('moving','rolling_back') AND [[other]].[[item_id]] IN ` + pages + `)
	`).Bind(dbx.Params{"jobId": move.JobID}).Row(&busy); err != nil {
		return err
	}
	if busy == 1 {
		return errPageWorkspaceMoveConflict
	}
	return nil
}
