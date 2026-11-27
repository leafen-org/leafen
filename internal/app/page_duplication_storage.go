package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	pageDuplicationJobKind        = "page-duplication"
	durableJobRunnerStoreKey      = "leafen.durable-job-runner"
	pageDuplicationDefaultRetries = 3
)

type storedPageDuplicationJob struct {
	ID             string `db:"job_id"`
	RequestID      string `db:"request_id"`
	ActorID        string `db:"actor_id"`
	OwnerID        string `db:"owner_id"`
	OrganizationID string `db:"organization_id"`
	WorkspaceID    string `db:"workspace_id"`
	// SourceOwnerID and SourceWorkspaceID hold the pages a copy into another
	// workspace reads; empty, it reads its own (sourceOwner, sourceWorkspace).
	SourceOwnerID     string `db:"source_owner_id"`
	SourceWorkspaceID string `db:"source_workspace_id"`
	// SourcePublicLinkID is the published site's link a visitor's copy reads
	// its pages through, not being in their workspace (public_page_duplicate.go).
	SourcePublicLinkID  string `db:"source_public_link_id"`
	SourceRootPageID    string `db:"source_root_page_id"`
	DestinationParentID string `db:"destination_parent_id"`
	TargetRootPageID    string `db:"target_root_page_id"`
	Title               string `db:"title"`
	RequestHash         string `db:"request_hash"`
	Status              string `db:"status"`
	Phase               string `db:"phase"`
	Revision            int    `db:"revision"`
	DiscoveredCount     int    `db:"discovered_count"`
	CopiedCount         int    `db:"copied_count"`
	UnsupportedCount    int    `db:"unsupported_count"`
	// HeldPageID is the page being copied this moment, held from changes while
	// it is (see holdPageDuplicationItem): the rest of the source stays editable.
	HeldPageID  string `db:"held_page_id"`
	ErrorCode   string `db:"error_code"`
	CreatedAt   int64  `db:"created_at"`
	UpdatedAt   int64  `db:"updated_at"`
	CompletedAt int64  `db:"completed_at"`
}

type storedPageDuplicationItem struct {
	JobID           string `db:"job_id"`
	SourcePageID    string `db:"source_page_id"`
	TargetPageID    string `db:"target_page_id"`
	SourceParentID  string `db:"source_parent_id"`
	TargetParentID  string `db:"target_parent_id"`
	Depth           int    `db:"depth"`
	DiscoveryCursor string `db:"discovery_cursor"`
	Discovered      bool   `db:"discovered"`
	PageCreated     bool   `db:"page_created"`
	BlockMapCursor  string `db:"block_map_cursor"`
	BlocksMapped    bool   `db:"blocks_mapped"`
	BlockCopyCursor string `db:"block_copy_cursor"`
	BlocksCopied    bool   `db:"blocks_copied"`
	Copied          bool   `db:"copied"`
	Published       bool   `db:"published"`
	CleanupLane     int    `db:"cleanup_lane"`
	Cleaned         bool   `db:"cleaned"`
}

type pageDuplicationAdmission struct {
	Job       storedPageDuplicationJob
	Admitted  bool
	Duplicate bool
}

// admitPageDuplication admits a copy the app asks for.
func admitPageDuplication(
	app core.App,
	actorID, sourcePageID string,
	input pageDuplicationInput,
) (pageDuplicationAdmission, error) {
	return admitPageDuplicationFrom(app, pageDuplicationFromApp, actorID, sourcePageID, input)
}

func admitPageDuplicationFrom(
	app core.App,
	surface pageDuplicationSurface,
	actorID, sourcePageID string,
	input pageDuplicationInput,
) (pageDuplicationAdmission, error) {
	return admitPageDuplicationWith(app, surface, actorID, sourcePageID, input, "", "", "")
}

// admitPageDuplicationInPlace copies a page whose copy's block takes the
// place of a placeholder block in the destination's body, under the ID the
// blocks around it name when one is planned (planBlockCopyPages).
func admitPageDuplicationInPlace(
	app core.App,
	actorID, sourcePageID string,
	input pageDuplicationInput,
	placeholderID, plannedID string,
) (pageDuplicationAdmission, error) {
	return admitPageDuplicationWith(app, pageDuplicationFromApp, actorID, sourcePageID, input, placeholderID, "", plannedID)
}

// publicLinkID, when set, is the published site's link a visitor copies its
// page through into a workspace of theirs (public_page_duplicate.go).
func admitPageDuplicationWith(
	app core.App,
	surface pageDuplicationSurface,
	actorID, sourcePageID string,
	input pageDuplicationInput,
	placeholderID string,
	publicLinkID string,
	plannedID string,
) (pageDuplicationAdmission, error) {
	requestHash := pageDuplicationRequestHash(sourcePageID, input)
	if placeholderID != "" {
		requestHash = pageDuplicationRequestHash(sourcePageID+"\x00"+placeholderID, input)
	}
	result := pageDuplicationAdmission{}
	err := app.RunInTransaction(func(txApp core.App) error {
		existing, err := loadStoredJobByRequest(txApp, actorID, input.RequestID)
		if err == nil {
			if existing.Kind != pageDuplicationJobKind || existing.RequestHash != requestHash {
				return errPageDuplicationRequestIDReused
			}
			job, err := loadPageDuplicationJobByID(txApp, existing.ID, actorID)
			if err != nil {
				return err
			}
			if err := authorizePageDuplicationTarget(
				txApp, actorID, job,
			); err != nil {
				return err
			}
			result = pageDuplicationAdmission{Job: job, Duplicate: true}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		var source pageAuthorization
		var workspace workspaceAuthorization
		if publicLinkID != "" {
			// A visitor reads the page through its site, into a workspace of theirs.
			if source, workspace, err = publicPageDuplicationSource(txApp, publicLinkID, sourcePageID); err != nil {
				return err
			}
			if input.DestinationWorkspaceID == "" {
				return errPageDuplicationConflict
			}
		} else {
			if source, err = authorizePage(txApp, actorID, sourcePageID, permissionWorkspaceRead); err != nil {
				return err
			}
		}
		wikiFenced, err := wikiMutationFenceActive(txApp, sourcePageID)
		if err != nil {
			return err
		}
		if wikiFenced {
			return errPageDuplicationConflict
		}
		if publicLinkID == "" {
			// A guest copies into a workspace of theirs (page_duplication_guest.go).
			if workspace, err = pageDuplicationSourceWorkspace(txApp, actorID, source, input); err != nil {
				return err
			}
		}
		// A copy moved to another workspace goes there, as the app asks.
		if input.DestinationWorkspaceID != "" && (surface != pageDuplicationFromApp || placeholderID != "") {
			return errPageDuplicationConflict
		}
		target, err := pageDuplicationTargetWorkspace(txApp, actorID, workspace, input)
		if publicLinkID != "" {
			// A visitor's copy goes to a workspace they can write, the site's own included.
			if target, err = authorizeWorkspace(txApp, actorID, input.DestinationWorkspaceID, permissionWorkspaceWrite); isPermissionDenied(err) {
				return destinationWorkspaceRefused(txApp, actorID, input.DestinationWorkspaceID, errorCodePageDuplicationInvalid)
			}
		}
		if err != nil {
			return err
		}
		if err := enforceSharingSecurityCrossWorkspaceCopy(txApp, workspace, target); err != nil {
			return err
		}
		if err := pageDuplicationTargetTeamspaceFailure(txApp, actorID, workspace, target, input, source.Page); err != nil {
			return err
		}
		var destination *core.Record
		if input.DestinationParentID != "" {
			// A database page takes a copy as its body's editors write it
			// (authorizePageBody): a sub-page copied into one stayed a link
			// (2026-11-14).
			authorizeParent := authorizePage
			if row, err := isDatabaseRowPageRoom(txApp, input.DestinationParentID); err != nil {
				return err
			} else if row {
				authorizeParent = authorizePageBody
			}
			parent, err := authorizeParent(
				txApp, actorID, input.DestinationParentID, permissionWorkspaceWrite,
			)
			if refusal := destinationParentRefused(parent, err, target.WorkspaceID, errorCodePageDuplicationInvalid, false); refusal != nil {
				return refusal
			}
			destination = parent.Page
		}
		if err := pageDuplicationPlacementFailure(txApp, surface, sourcePageID, destination, placeholderID != ""); err != nil {
			return err
		}
		var active int
		if err := txApp.DB().NewQuery(`
			SELECT COUNT(*) FROM {{leafen_page_duplication_jobs}}
			WHERE [[owner_id]]={:ownerId} AND [[source_root_page_id]]={:sourcePageId}
				AND [[phase]] NOT IN ('completed','failed')
		`).Bind(dbx.Params{
			"ownerId": workspace.OwnerID, "sourcePageId": sourcePageID,
		}).Row(&active); err != nil {
			return err
		}
		if active != 0 || source.Page.GetString("duplication_source_job_id") != "" {
			return errPageDuplicationConflict
		}

		// A page duplicated in its workspace, from its menu, its block's or a
		// connection (Notion's MCP names it so too, seen 2026-11-05), is named
		// as a copy; one sent to another workspace, or carried by a copy of
		// other blocks, keeps its name.
		title := input.Title
		if title == "" {
			title = source.Page.GetString("title")
			if placeholderID == "" && target.WorkspaceID == workspace.WorkspaceID {
				title, _ = duplicateCopyTitle(title)
			}
		}
		now := time.Now().UTC().UnixMilli()
		jobID := core.GenerateDefaultRandomId()
		targetRootID := newPageID()
		if plannedID != "" {
			targetRootID = plannedID
		}
		_, err = txApp.DB().NewQuery(`
			INSERT INTO {{leafen_jobs}}
				([[id]],[[owner]],[[workspace_id]],[[request_id]],[[request_hash]],[[kind]],[[page_id]],
				 [[status]],[[error_code]],[[diagnostic_reference]],[[not_before]],[[created]],[[updated]],
				 [[payload_json]],[[cursor]],[[processed_items]],[[total_items]],[[attempt]],[[max_attempts]],
				 [[chunk_count]],[[captured_bytes]])
			VALUES
				({:id},{:owner},{:workspaceId},{:requestId},{:requestHash},{:kind},{:pageId},
				 'queued','','',{:notBefore},{:created},{:updated},{:payloadJSON},'',0,1,0,{:maxAttempts},0,0)
		`).Bind(dbx.Params{
			"id": jobID, "owner": actorID, "workspaceId": target.WorkspaceID,
			"requestId": input.RequestID, "requestHash": requestHash,
			"kind": pageDuplicationJobKind, "pageId": sourcePageID,
			"notBefore": now, "created": now, "updated": now,
			"maxAttempts": pageDuplicationDefaultRetries,
			"payloadJSON": pageDuplicationPayload(input.WithoutContent, placeholderID, input.DestinationTeamspaceID, input.ToPrivatePages,
				surface != pageDuplicationFromApp || pageHistoryConnectionOf(app).Name != ""),
		}).Execute()
		if err != nil {
			return err
		}
		sourceOwnerID, sourceWorkspaceID := "", ""
		if target.WorkspaceID != workspace.WorkspaceID {
			sourceOwnerID, sourceWorkspaceID = workspace.OwnerID, workspace.WorkspaceID
		}
		_, err = txApp.DB().Insert(pageDuplicationJobsTable, dbx.Params{
			"job_id": jobID, "actor_id": actorID, "owner_id": target.OwnerID,
			"organization_id": target.Organization.OrganizationID,
			"workspace_id":    target.WorkspaceID, "source_root_page_id": sourcePageID,
			"source_owner_id": sourceOwnerID, "source_workspace_id": sourceWorkspaceID,
			"source_public_link_id": publicLinkID,
			"destination_parent_id": input.DestinationParentID,
			"target_root_page_id":   targetRootID, "title": title,
			"request_hash": requestHash, "phase": pageDuplicationPhaseDiscovering,
			"revision": 1, "discovered_count": 0, "copied_count": 0,
			"unsupported_count": 0,
			"error_code":        "", "created_at": now, "updated_at": now,
			"completed_at": 0,
		}).Execute()
		if err != nil {
			return err
		}
		// A page copied into itself is taken as it stands (pageDuplicationStopsAt).
		stops := sourcePageID == input.DestinationParentID
		_, err = txApp.DB().Insert(pageDuplicationItemsTable, dbx.Params{
			"job_id": jobID, "source_page_id": sourcePageID,
			"target_page_id":   targetRootID,
			"source_parent_id": source.Page.GetString("parent_id"),
			"target_parent_id": input.DestinationParentID, "depth": 0,
			"discovered": stops, "blocks_mapped": stops, "database_mapped": stops, "blocks_copied": stops,
			"created_at": now, "updated_at": now,
		}).Execute()
		if err != nil {
			return err
		}
		if err := mapPageDuplicationIdentity(
			txApp, jobID, "page", sourcePageID, targetRootID, sourcePageID, targetRootID, now,
		); err != nil {
			return err
		}
		if err := initializePageDuplicationFileState(txApp, jobID, now); err != nil {
			return err
		}
		if !stops {
			if err := initializePageDuplicationDatabaseStates(
				txApp, jobID, workspace.OwnerID, workspace.WorkspaceID,
				sourcePageID, targetRootID, now,
			); err != nil {
				return err
			}
		}
		// Marking the source is no edit of it: its last edit time stays, as
		// Notion's (seen 2026-11-13), so a copy sorted by edits lists above it
		// after a reload as it does at once.
		if _, err := txApp.DB().NewQuery(`
			UPDATE {{leafen_pages}} SET [[duplication_source_job_id]]={:jobId}
			WHERE [[id]]={:sourcePageId}
		`).Bind(dbx.Params{"jobId": jobID, "sourcePageId": sourcePageID}).Execute(); err != nil {
			return err
		}
		source.Page.Set("duplication_source_job_id", jobID)
		result = pageDuplicationAdmission{
			Job: storedPageDuplicationJob{
				ID: jobID, RequestID: input.RequestID,
				ActorID: actorID, OwnerID: target.OwnerID,
				OrganizationID: target.Organization.OrganizationID,
				WorkspaceID:    target.WorkspaceID, SourceRootPageID: sourcePageID,
				SourceOwnerID: sourceOwnerID, SourceWorkspaceID: sourceWorkspaceID, SourcePublicLinkID: publicLinkID,
				DestinationParentID: input.DestinationParentID,
				TargetRootPageID:    targetRootID, Title: title,
				RequestHash: requestHash, Status: durableJobStatusQueued,
				Phase: pageDuplicationPhaseDiscovering, Revision: 1,
				DiscoveredCount: 0, CopiedCount: 0,
				UnsupportedCount: 0,
				CreatedAt:        now, UpdatedAt: now,
			},
			Admitted: true,
		}
		// The copy's top page exists at once: it opens while the rest is made.
		if err := createPageDuplicationTarget(txApp, result.Job, storedPageDuplicationItem{
			JobID: jobID, SourcePageID: sourcePageID, TargetPageID: targetRootID,
			TargetParentID: input.DestinationParentID,
		}); err != nil {
			return err
		}
		return nil
	})
	return result, err
}

func loadPageDuplicationJobByID(
	app core.App,
	jobID, actorID string,
) (storedPageDuplicationJob, error) {
	params := dbx.Params{"jobId": jobID}
	actorClause := ""
	if actorID != "" {
		actorClause = " AND [[detail]].[[actor_id]]={:actorId}"
		params["actorId"] = actorID
	}
	job := storedPageDuplicationJob{}
	err := app.DB().NewQuery(`
		SELECT [[detail]].[[job_id]],[[base]].[[request_id]],[[detail]].[[actor_id]],[[detail]].[[owner_id]],
			[[detail]].[[organization_id]],
			[[detail]].[[workspace_id]],[[detail]].[[source_owner_id]],[[detail]].[[source_workspace_id]],
			[[detail]].[[source_public_link_id]],[[detail]].[[source_root_page_id]],
			[[detail]].[[destination_parent_id]],[[detail]].[[target_root_page_id]],
			[[detail]].[[title]],[[detail]].[[request_hash]],[[base]].[[status]],
			[[detail]].[[phase]],[[detail]].[[revision]],[[detail]].[[discovered_count]],
			[[detail]].[[copied_count]],[[detail]].[[unsupported_count]],[[detail]].[[held_page_id]],[[detail]].[[error_code]],
			[[detail]].[[created_at]],[[detail]].[[updated_at]],[[detail]].[[completed_at]]
		FROM {{leafen_page_duplication_jobs}} [[detail]]
		JOIN {{leafen_jobs}} [[base]] ON [[base]].[[id]]=[[detail]].[[job_id]]
		WHERE [[detail]].[[job_id]]={:jobId}` + actorClause + `
	`).Bind(params).One(&job)
	return job, err
}

func pageDuplicationResponseForJob(
	job storedPageDuplicationJob,
	duplicate bool,
) (pageDuplicationResponse, error) {
	progress := pageDuplicationProgress{
		Revision: job.Revision, Discovered: job.DiscoveredCount, Copied: job.CopiedCount,
	}
	status := pageDuplicationStatusPending
	if job.Status == durableJobStatusCompleted {
		status = pageDuplicationStatusCompleted
	} else if job.Status == durableJobStatusFailed || job.Phase == pageDuplicationPhaseFailed {
		status = pageDuplicationStatusFailed
	}
	return pageDuplicationResponse{
		JobID: job.ID, SourceRootPageID: job.SourceRootPageID,
		TargetRootPageID: job.TargetRootPageID, Status: status, Phase: job.Phase,
		Progress: progress, ErrorCode: job.ErrorCode, Duplicate: duplicate,
	}, nil
}

func pageDuplicationRequestHash(sourcePageID string, input pageDuplicationInput) string {
	parts := []string{pageDuplicationJobKind, sourcePageID, input.DestinationParentID, input.Title}
	if input.WithoutContent {
		parts = append(parts, "without-content")
	}
	if input.DestinationWorkspaceID != "" {
		parts = append(parts, "workspace", input.DestinationWorkspaceID)
	}
	if input.DestinationTeamspaceID != "" {
		parts = append(parts, "teamspace", input.DestinationTeamspaceID)
	}
	if input.ToPrivatePages {
		parts = append(parts, "private")
	}
	value := strings.Join(parts, "\x00")
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

// pageDuplicationCopyOpenSQL says a page is the top of a copy under way: it
// shows at once, as Notion's copy does, though nothing under it does yet.
const pageDuplicationCopyOpenSQL = `EXISTS (
	SELECT 1 FROM {{leafen_page_duplication_jobs}} [[open_copy]]
	WHERE [[open_copy]].[[job_id]]=[[page]].[[duplication_target_job_id]]
		AND [[open_copy]].[[target_root_page_id]]=[[page]].[[id]]
		AND [[open_copy]].[[phase]] IN ('discovering','copying','publishing')
)`

func pageDuplicationTargetVisible(app core.App, page *core.Record) (bool, error) {
	jobID := page.GetString("duplication_target_job_id")
	if jobID == "" {
		return true, nil
	}
	var count int
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_page_duplication_jobs}} [[detail]]
		JOIN {{leafen_jobs}} [[base]] ON [[base]].[[id]]=[[detail]].[[job_id]]
		WHERE [[detail]].[[job_id]]={:jobId} AND (
			([[detail]].[[phase]]='completed' AND [[base]].[[status]]='completed')
			OR ([[detail]].[[target_root_page_id]]={:pageId} AND [[detail]].[[phase]] IN ('discovering','copying','publishing'))
		)
	`).Bind(dbx.Params{"jobId": jobID, "pageId": page.Id}).Row(&count)
	return count == 1, err
}

// pageDuplicationCopyTopWholeSQL says a page is the top of a copy under way
// whose own body is done: it takes changes from then on, as Notion's copy does
// (seen 2026-11-05), the pages under it each from when they are done.
const pageDuplicationCopyTopWholeSQL = `EXISTS (
	SELECT 1 FROM {{leafen_page_duplication_jobs}} [[whole_copy]]
	JOIN {{leafen_page_duplication_items}} [[whole_item]]
		ON [[whole_item]].[[job_id]]=[[whole_copy]].[[job_id]]
		AND [[whole_item]].[[source_page_id]]=[[whole_copy]].[[source_root_page_id]]
	WHERE [[whole_copy]].[[job_id]]=[[page]].[[duplication_target_job_id]]
		AND [[whole_copy]].[[target_root_page_id]]=[[page]].[[id]]
		AND [[whole_item]].[[copied]]=1
)`

// pageDuplicationTargetUnderWay says a page is a copy whose own content is
// still being made, which takes no changes until it is whole.
func pageDuplicationTargetUnderWay(app core.App, page *core.Record) (bool, error) {
	if page.GetString("duplication_target_job_id") == "" {
		return false, nil
	}
	var whole int
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_pages}} [[page]]
		WHERE [[page]].[[id]]={:pageId} AND ` + pageDuplicationCopyTopWholeSQL,
	).Bind(dbx.Params{"pageId": page.Id}).Row(&whole)
	return whole == 0, err
}

// pageCopyPercent is how far the copy this page is the top of has come, or nil
// when none is being made. A reload or another tab asks it of the page.
func pageCopyPercent(app core.App, page *core.Record) (*int, error) {
	jobID := page.GetString("duplication_target_job_id")
	if jobID == "" {
		return nil, nil
	}
	progress := struct {
		Discovered int `db:"discovered_count"`
		Copied     int `db:"copied_count"`
	}{}
	err := app.DB().NewQuery(`
		SELECT [[discovered_count]],[[copied_count]] FROM {{leafen_page_duplication_jobs}}
		WHERE [[job_id]]={:jobId} AND [[target_root_page_id]]={:pageId}
			AND [[phase]] IN ('discovering','copying','publishing')
	`).Bind(dbx.Params{"jobId": jobID, "pageId": page.Id}).One(&progress)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	percent := 0
	if progress.Discovered > 0 {
		percent = min(99, progress.Copied*100/progress.Discovered)
	}
	return &percent, nil
}

func pageDuplicationSourceFenceActive(app core.App, pageID string) (bool, error) {
	var count int
	err := app.DB().NewQuery(`
		WITH RECURSIVE [[duplication_ancestors]] ([[id]],[[parent_id]],[[job_id]]) AS (
			SELECT [[page]].[[id]],` + pageAncestryParentSQL("[[page]]") + `,[[page]].[[duplication_source_job_id]]
			FROM {{leafen_pages}} [[page]] WHERE [[page]].[[id]]={:pageId}
			UNION
			SELECT [[page]].[[id]],` + pageAncestryParentSQL("[[page]]") + `,[[page]].[[duplication_source_job_id]]
			FROM {{leafen_pages}} [[page]]
			JOIN [[duplication_ancestors]] [[ancestor]] ON [[page]].[[id]]=[[ancestor]].[[parent_id]]
			WHERE [[ancestor]].[[parent_id]]!=''
		)
		SELECT COUNT(*) FROM [[duplication_ancestors]] [[ancestor]]
		WHERE [[ancestor]].[[job_id]]!='' AND EXISTS (
			SELECT 1 FROM {{leafen_page_duplication_jobs}} [[job]]
			WHERE [[job]].[[job_id]]=[[ancestor]].[[job_id]]
				AND [[job]].[[phase]] NOT IN ('completed','failed')
		)
	`).Bind(dbx.Params{"pageId": pageID}).Row(&count)
	return count > 0, err
}

// pageDuplicationHeldFenceActive says whether a copy is reading this page or
// one above it right now. The rest of a source stays editable through a long
// copy, as in Notion (seen 2026-11-05): only the page under the copy's hand
// waits, for that page's windows — the copy's top too, when it goes first
// (page_duplication_top_first.go); the tree as a whole is
// pageDuplicationSourceFenceActive, for what a running copy can't survive.
func pageDuplicationHeldFenceActive(app core.App, pageID string) (bool, error) {
	return pageDuplicationSetHeldFenceActive(app, []string{pageID})
}

func pageDuplicationSetHeldFenceActive(app core.App, pageIDs []string) (bool, error) {
	var running int
	if err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_page_duplication_jobs}} INDEXED BY [[idx_leafen_page_duplication_held]]
		WHERE [[held_page_id]]!=''
	`).Row(&running); err != nil || running == 0 {
		return false, err
	}
	params, placeholders := pageDuplicationIDParams("heldPage", pageIDs)
	var count int
	err := app.DB().NewQuery(`
		WITH RECURSIVE [[held_ancestors]] ([[id]],[[parent_id]]) AS (
			SELECT [[page]].[[id]],` + pageAncestryParentSQL("[[page]]") + `
			FROM {{leafen_pages}} [[page]] WHERE [[page]].[[id]] IN (` + joinSQL(placeholders) + `)
			UNION
			SELECT [[page]].[[id]],` + pageAncestryParentSQL("[[page]]") + `
			FROM {{leafen_pages}} [[page]]
			JOIN [[held_ancestors]] [[ancestor]] ON [[page]].[[id]]=[[ancestor]].[[parent_id]]
			WHERE [[ancestor]].[[parent_id]]!=''
		)
		SELECT COUNT(*) FROM {{leafen_page_duplication_jobs}} [[job]]
		WHERE [[job]].[[held_page_id]]!='' AND [[job]].[[phase]] IN ('discovering','copying')
			AND [[job]].[[held_page_id]] IN (SELECT [[id]] FROM [[held_ancestors]])
	`).Bind(params).Row(&count)
	return count > 0, err
}

func pageDuplicationSetTargetHidden(app core.App, pageIDs []string, reading bool) (bool, error) {
	params, placeholders := pageDuplicationIDParams("targetPage", pageIDs)
	open := " AND NOT " + pageDuplicationCopyTopWholeSQL
	if reading {
		open = " AND NOT " + pageDuplicationCopyOpenSQL
	}
	var count int
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_pages}} [[page]]
		WHERE [[page]].[[id]] IN (` + joinSQL(placeholders) + `)
			AND [[page]].[[duplication_target_job_id]]!=''` + open + `
			AND NOT EXISTS (
				SELECT 1 FROM {{leafen_page_duplication_jobs}} [[detail]]
				JOIN {{leafen_jobs}} [[base]] ON [[base]].[[id]]=[[detail]].[[job_id]]
				WHERE [[detail]].[[job_id]]=[[page]].[[duplication_target_job_id]]
					AND [[detail]].[[phase]]='completed' AND [[base]].[[status]]='completed'
			)
	`).Bind(params).Row(&count)
	return count > 0, err
}

func pageDuplicationSetSourceFenceActive(app core.App, pageIDs []string) (bool, error) {
	params, placeholders := pageDuplicationIDParams("sourcePage", pageIDs)
	var count int
	err := app.DB().NewQuery(`
		WITH RECURSIVE [[duplication_ancestors]] ([[id]],[[parent_id]],[[job_id]]) AS (
			SELECT [[page]].[[id]],` + pageAncestryParentSQL("[[page]]") + `,[[page]].[[duplication_source_job_id]]
			FROM {{leafen_pages}} [[page]] WHERE [[page]].[[id]] IN (` + joinSQL(placeholders) + `)
			UNION
			SELECT [[page]].[[id]],` + pageAncestryParentSQL("[[page]]") + `,[[page]].[[duplication_source_job_id]]
			FROM {{leafen_pages}} [[page]]
			JOIN [[duplication_ancestors]] [[ancestor]] ON [[page]].[[id]]=[[ancestor]].[[parent_id]]
			WHERE [[ancestor]].[[parent_id]]!=''
		)
		SELECT COUNT(*) FROM [[duplication_ancestors]] [[ancestor]]
		WHERE [[ancestor]].[[job_id]]!='' AND EXISTS (
			SELECT 1 FROM {{leafen_page_duplication_jobs}} [[job]]
			WHERE [[job]].[[job_id]]=[[ancestor]].[[job_id]]
				AND [[job]].[[phase]] NOT IN ('completed','failed')
		)
	`).Bind(params).Row(&count)
	return count > 0, err
}

func pageDuplicationIDParams(prefix string, pageIDs []string) (dbx.Params, []string) {
	params := dbx.Params{}
	placeholders := make([]string, 0, len(pageIDs))
	for index, pageID := range pageIDs {
		key := fmt.Sprintf("%s%d", prefix, index)
		params[key] = pageID
		placeholders = append(placeholders, "{:"+key+"}")
	}
	return params, placeholders
}

func schedulePageDuplicationFailure(
	app core.App,
	jobID, reference string,
	applyErr error,
) (bool, error) {
	job, err := loadStoredJob(app, jobID, "")
	if errors.Is(err, sql.ErrNoRows) || (err == nil && job.Kind != pageDuplicationJobKind) {
		return false, nil
	}
	if err != nil {
		return true, err
	}
	detail, err := loadPageDuplicationJobByID(app, jobID, "")
	if err != nil {
		return true, err
	}
	diagnosticCode := errorCodePageDuplicationUnavailable
	if selection, ok := persistentWriteStorageErrorSelection(
		applyErr, persistentWritePageDuplication,
	); ok {
		diagnosticCode = selection.Code
	}
	now := time.Now().UTC().UnixMilli()
	if job.Attempt+1 >= job.MaxAttempts {
		resultErr := app.RunInTransaction(func(txApp core.App) error {
			if err := beginPageDuplicationCleanupInTransaction(
				txApp, detail, diagnosticCode,
			); err != nil {
				return err
			}
			_, err := txApp.DB().NewQuery(`
				UPDATE {{leafen_jobs}} SET [[diagnostic_reference]]={:reference}
				WHERE [[id]]={:jobId} AND [[status]]='queued'
			`).Bind(dbx.Params{"jobId": jobID, "reference": reference}).Execute()
			return err
		})
		if resultErr == nil {
			logDurableJobDiagnostic(
				app, diagnosticCode, reference, jobID,
				"page-duplication.apply", "cleanup-scheduled", applyErr,
			)
		}
		return true, resultErr
	}
	_, err = app.DB().NewQuery(`
		UPDATE {{leafen_jobs}}
		SET [[attempt]]=[[attempt]]+1, [[diagnostic_reference]]={:reference},
			[[not_before]]={:notBefore}, [[updated]]={:updatedAt}
		WHERE [[id]]={:jobId} AND [[status]]='queued'
	`).Bind(dbx.Params{
		"jobId": jobID, "reference": reference,
		"notBefore": now + durableJobReadyDelay.Milliseconds(), "updatedAt": now,
	}).Execute()
	if err == nil {
		logDurableJobDiagnostic(
			app, diagnosticCode, reference, jobID,
			"page-duplication.apply", "retry-scheduled", applyErr,
		)
	}
	return true, err
}
