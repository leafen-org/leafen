package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	blockMoveJobKind        = "block-move"
	blockMoveDefaultRetries = 3
)

type storedBlockMoveJob struct {
	ID          string `db:"job_id"`
	ActorID     string `db:"actor_id"`
	OwnerID     string `db:"owner_id"`
	WorkspaceID string `db:"workspace_id"`
	// SourceOwnerID and SourceWorkspaceID hold the page a copy from another
	// workspace reads; empty, it reads its own (block_copy_workspaces.go).
	SourceOwnerID     string `db:"source_owner_id"`
	SourceWorkspaceID string `db:"source_workspace_id"`
	SourcePageID      string `db:"source_page_id"`
	DestinationPageID string `db:"destination_page_id"`
	RootBlockID       string `db:"root_block_id"`
	TargetRootBlockID string `db:"target_root_block_id"`
	Operation         string `db:"operation"`
	RequestHash       string `db:"request_hash"`
	Status            string `db:"status"`
	Phase             string `db:"phase"`
	Revision          int    `db:"revision"`
	DiscoveredCount   int    `db:"discovered_count"`
	MovedCount        int    `db:"moved_count"`
	ErrorCode         string `db:"error_code"`
	CreatedAt         int64  `db:"created_at"`
	UpdatedAt         int64  `db:"updated_at"`
	CompletedAt       int64  `db:"completed_at"`
}

type storedBlockMoveItem struct {
	JobID           string `db:"job_id"`
	BlockID         string `db:"block_id"`
	TargetBlockID   string `db:"target_block_id"`
	ParentID        string `db:"parent_id"`
	Position        string `db:"position"`
	Depth           int    `db:"depth"`
	DiscoveryCursor string `db:"discovery_cursor"`
	Discovered      bool   `db:"discovered"`
	Moved           bool   `db:"moved"`
}

type blockMoveAdmission struct {
	Job       storedBlockMoveJob
	Admitted  bool
	Duplicate bool
}

type storedBlockMoveRoot struct {
	ID       string `db:"id"`
	ParentID string `db:"parent_id"`
	Position string `db:"position"`
}

func admitBlockMove(
	app core.App,
	actorID, sourcePageID, blockID string,
	input blockMoveInput,
) (blockMoveAdmission, error) {
	return admitBlockMoveOf(app, actorID, sourcePageID, blockID, input, false, false, false)
}

// admitBlockChildrenMove moves the blocks inside a block, rather than the
// block, to the end of another page's body in their order: a block turned
// into a page takes the blocks inside it into the page.
func admitBlockChildrenMove(
	app core.App,
	actorID, sourcePageID, blockID string,
	input blockMoveInput,
) (blockMoveAdmission, error) {
	input.Operation = blockMoveOperationMove
	return admitBlockMoveOf(app, actorID, sourcePageID, blockID, input, true, false, false)
}

// A conversion's inverse discovers the page body in bounded windows. Its
// virtual root is discovery-only and never becomes a product block.
func admitPageBodyConversionReturn(app core.App, actorID, sourcePageID string, input blockMoveInput) (blockMoveAdmission, error) {
	input.Operation = blockMoveOperationMove
	return admitBlockMoveOf(app, actorID, sourcePageID, sourcePageID, input, true, true, false)
}

// admitPageBodyCopy copies a page's whole body, in its order, to the end of
// another page's, as a page made from it as a template takes it
// (notion_page_template_copy.go): the page is read, not written, and its
// sub-pages and databases come as copies. Its virtual root is
// discovery-only, as a conversion return's.
func admitPageBodyCopy(app core.App, actorID, sourcePageID string, input blockMoveInput) (blockMoveAdmission, error) {
	input.Operation = blockMoveOperationCopy
	return admitBlockMoveOf(app, actorID, sourcePageID, sourcePageID, input, true, true, false)
}

// admitDatabaseTemplateCopy copies a database's block, from a page its
// actor may only read, to another page, as a page made from a database as
// its template becomes a copy of it (notion_page_template_database.go).
func admitDatabaseTemplateCopy(app core.App, actorID, sourcePageID, blockID string, input blockMoveInput) (blockMoveAdmission, error) {
	input.Operation = blockMoveOperationCopy
	return admitBlockMoveOf(app, actorID, sourcePageID, blockID, input, false, false, true)
}

// admitBlockMoveOf admits a move or copy; a copy that only reads its source
// (readSource) needs no write on it, as a body copy needs none.
func admitBlockMoveOf(
	app core.App,
	actorID, sourcePageID, blockID string,
	input blockMoveInput,
	children, body, readSource bool,
) (blockMoveAdmission, error) {
	operation, ok := normalizeBlockMoveOperation(input.Operation)
	if !ok || input.WithoutContent && operation != blockMoveOperationCopy {
		return blockMoveAdmission{}, errBlockMoveInvalid
	}
	input.Operation = operation
	// A page's body copied, rather than moved back (admitPageBodyCopy).
	bodyCopy := body && operation == blockMoveOperationCopy
	requestHash := blockMoveRequestHash(sourcePageID, blockID, input)
	if children {
		requestHash = blockMoveRequestHash(sourcePageID, blockID+"\x00children", input)
	}
	if body {
		requestHash = blockMoveRequestHash(sourcePageID, blockID+"\x00conversion-return", input)
	}
	if bodyCopy {
		requestHash = blockMoveRequestHash(sourcePageID, blockID+"\x00body-copy", input)
	}
	result := blockMoveAdmission{}
	err := app.RunInTransaction(func(txApp core.App) error {
		existing, err := loadStoredJobByRequest(txApp, actorID, input.RequestID)
		if err == nil {
			if existing.Kind != blockMoveJobKind || existing.RequestHash != requestHash {
				return errBlockMoveRequestIDReused
			}
			job, err := loadBlockMoveJobByID(txApp, existing.ID, actorID)
			if err != nil {
				return err
			}
			if _, err := authorizeWorkspace(
				txApp, actorID, job.WorkspaceID, permissionWorkspaceWrite,
			); err != nil {
				return err
			}
			result = blockMoveAdmission{Job: job, Duplicate: true}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		sourceRequirement := permissionWorkspaceWrite
		if bodyCopy || readSource && operation == blockMoveOperationCopy {
			sourceRequirement = permissionWorkspaceRead
		}
		source, err := authorizePageBody(txApp, actorID, sourcePageID, sourceRequirement)
		if err != nil {
			return err
		}
		destination, err := authorizePageBody(
			txApp, actorID, input.DestinationPageID, permissionWorkspaceWrite,
		)
		if err != nil {
			return err
		}
		// A copy may stay on its page, as a duplicate of its blocks, and a
		// block moved to its own page goes last on it (moveBlockToOwnPageEnd);
		// the blocks inside a block or a page's body don't. A full-page
		// database's body is its database alone, which stays: a block moved
		// into one becomes its row instead, as a page and then the row
		// (convertPageToDatabaseRow), which the editor asks for.
		ownPage := sourcePageID == input.DestinationPageID
		// Only a copy that reads its source crosses workspaces: a database
		// of a page's own (blockCopyCrossesWorkspaces).
		crossWorkspace := source.Workspace.WorkspaceID != destination.Workspace.WorkspaceID
		if crossWorkspace && (!readSource || operation != blockMoveOperationCopy || children || body) ||
			ownPage && input.Operation != blockMoveOperationCopy && (children || body) ||
			isFullPageDatabase(destination.Page) ||
			isFullPageDatabase(source.Page) && input.Operation != blockMoveOperationCopy && !body ||
			body && !bodyCopy && source.Page.GetString("parent_id") != input.DestinationPageID ||
			bodyCopy && (ownPage || isFullPageDatabase(source.Page)) {
			return errBlockMoveConflict
		}
		root := storedBlockMoveRoot{}
		if !body {
			if err := txApp.DB().NewQuery(`
			SELECT [[id]],[[parent_id]],[[position]] FROM {{leafen_blocks}}
			WHERE [[owner]]={:ownerId} AND [[workspace_id]]={:workspaceId}
				AND [[page_id]]={:pageId} AND [[id]]={:blockId}
		`).Bind(dbx.Params{
				"ownerId": source.Workspace.OwnerID, "workspaceId": source.Workspace.WorkspaceID,
				"pageId": sourcePageID, "blockId": blockID,
			}).One(&root); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return errBlockMoveNotFound
				}
				return err
			}
		} else {
			root.ID = sourcePageID
		}
		if crossWorkspace {
			crosses, err := blockCopyCrossesWorkspaces(txApp, source.Workspace, sourcePageID, blockID)
			if err != nil {
				return err
			}
			if !crosses {
				return errBlockMoveConflict
			}
			// As a page's copy into another workspace, while its
			// organization lets it leave (page_duplication_storage.go).
			if err := enforceSharingSecurityCrossWorkspaceCopy(txApp, source.Workspace, destination.Workspace); err != nil {
				return err
			}
		}
		// The copy is written in the destination's workspace, and read in
		// the source's when that is another.
		writes, sourceOwnerID, sourceWorkspaceID := source.Workspace, "", ""
		if crossWorkspace {
			writes = destination.Workspace
			sourceOwnerID, sourceWorkspaceID = source.Workspace.OwnerID, source.Workspace.WorkspaceID
		}
		if ownPage && input.Operation == blockMoveOperationMove {
			job, err := moveBlockToOwnPageEnd(
				txApp, actorID, source.Workspace.OwnerID, source.Workspace.WorkspaceID, sourcePageID, blockID, input, requestHash,
			)
			result = blockMoveAdmission{Job: job, Admitted: true}
			return err
		}
		roots := []storedBlockMoveRoot{root}
		copyChildren := children && operation == blockMoveOperationCopy && !body
		if children && !body && !copyChildren {
			roots = nil
			if err := txApp.DB().NewQuery(`
				SELECT [[id]],[[parent_id]],[[position]] FROM {{leafen_blocks}}
				WHERE [[owner]]={:ownerId} AND [[workspace_id]]={:workspaceId}
					AND [[page_id]]={:pageId} AND [[parent_id]]={:blockId}
				ORDER BY [[position]],[[id]]
			`).Bind(dbx.Params{
				"ownerId": source.Workspace.OwnerID, "workspaceId": source.Workspace.WorkspaceID,
				"pageId": sourcePageID, "blockId": blockID,
			}).All(&roots); err != nil {
				return err
			}
			if len(roots) == 0 {
				return errBlockMoveNotFound
			}
		}
		// A wiki's database stays on the wiki's page; a copy may leave.
		if body && !bodyCopy {
			wiki, err := loadWikiDatabaseOnPage(txApp, source.Workspace.OwnerID, sourcePageID)
			if err != nil {
				return err
			}
			if len(wiki.Blocks) > 0 {
				return errBlockMoveConflict
			}
			allowed, err := childPagesMayMoveTo(txApp, actorID, nil, sourcePageID, input.DestinationPageID)
			if err != nil {
				return err
			}
			if !allowed {
				return errBlockMoveConflict
			}
		} else if !body && input.Operation != blockMoveOperationCopy {
			wiki, err := wikiBlocksWithin(txApp, source.Workspace.OwnerID, sourcePageID, blockID)
			if err != nil {
				return err
			}
			if wiki {
				return errBlockMoveConflict
			}
			// Sub-pages go with their blocks, where they may go.
			pages, err := blockSubtreeChildPages(txApp, source.Workspace.OwnerID, sourcePageID, blockID)
			if err != nil {
				return err
			}
			if children {
				// The block whose blocks move stays.
				pages = slices.DeleteFunc(pages, func(id string) bool { return id == blockID })
			}
			if len(pages) > 0 {
				allowed, err := childPagesMayMoveTo(txApp, actorID, pages, sourcePageID, input.DestinationPageID)
				if err != nil {
					return err
				}
				if !allowed {
					return errBlockMoveConflict
				}
			}
		}
		active, err := blockMoveSetFenceActive(
			txApp, []string{sourcePageID, input.DestinationPageID},
		)
		if err != nil {
			return err
		}
		if active {
			return errBlockMoveConflict
		}
		payload := map[string]any{}
		if copyChildren {
			payload["copyChildrenParent"] = blockID
		}
		if bodyCopy {
			payload["bodyCopy"] = sourcePageID
		} else if body {
			payload["conversionReturnParent"] = sourcePageID
		}
		if input.WithoutContent {
			payload["withoutContent"] = true
		}
		if readSource && operation == blockMoveOperationCopy {
			payload["keepName"] = true
		}
		// A paste's copy lands where the paste put it (pastedBlockCopySpot).
		if input.Operation == blockMoveOperationCopy && input.PlaceAfter != "" && !copyChildren && !body {
			payload["copyPlaced"] = true
			payload["copyParent"] = input.PlaceParent
			payload["copyAfter"] = input.PlaceAfter
		}
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			return err
		}

		now := time.Now().UTC().UnixMilli()
		jobID := core.GenerateDefaultRandomId()
		targetRootBlockID := blockID
		if input.Operation == blockMoveOperationCopy {
			targetRootBlockID = uuid.NewString()
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
			"id": jobID, "owner": actorID, "workspaceId": writes.WorkspaceID,
			"requestId": input.RequestID, "requestHash": requestHash,
			"kind": blockMoveJobKind, "pageId": sourcePageID,
			"notBefore": now, "created": now, "updated": now,
			"maxAttempts": blockMoveDefaultRetries, "payloadJSON": string(payloadJSON),
		}).Execute()
		if err != nil {
			return err
		}
		discovered := len(roots)
		if body || copyChildren {
			discovered = 0
		}
		_, err = txApp.DB().Insert(blockMoveJobsTable, dbx.Params{
			"job_id": jobID, "actor_id": actorID, "owner_id": writes.OwnerID,
			"workspace_id": writes.WorkspaceID, "source_owner_id": sourceOwnerID,
			"source_workspace_id": sourceWorkspaceID, "source_page_id": sourcePageID,
			"destination_page_id": input.DestinationPageID, "root_block_id": blockID,
			"target_root_block_id": targetRootBlockID, "operation": input.Operation,
			"request_hash": requestHash, "phase": blockMovePhaseDiscovering,
			"revision": 1, "discovered_count": discovered, "moved_count": 0,
			"error_code": "", "created_at": now, "updated_at": now, "completed_at": 0,
		}).Execute()
		if err != nil {
			return err
		}
		for _, moved := range roots {
			target := moved.ID
			if moved.ID == blockID {
				target = targetRootBlockID
			}
			if body || copyChildren {
				target = ""
			}
			_, err = txApp.DB().Insert(blockMoveItemsTable, dbx.Params{
				"job_id": jobID, "block_id": moved.ID, "target_block_id": target,
				"parent_id": moved.ParentID,
				"position":  moved.Position, "depth": 0, "created_at": now, "updated_at": now,
			}).Execute()
			if err != nil {
				return err
			}
		}
		result = blockMoveAdmission{Job: storedBlockMoveJob{
			ID: jobID, ActorID: actorID, OwnerID: writes.OwnerID, WorkspaceID: writes.WorkspaceID,
			SourceOwnerID: sourceOwnerID, SourceWorkspaceID: sourceWorkspaceID, SourcePageID: sourcePageID,
			DestinationPageID: input.DestinationPageID, RootBlockID: blockID,
			TargetRootBlockID: targetRootBlockID, Operation: input.Operation,
			RequestHash: requestHash, Status: durableJobStatusQueued,
			Phase: blockMovePhaseDiscovering, Revision: 1, DiscoveredCount: discovered,
			CreatedAt: now, UpdatedAt: now,
		}, Admitted: true}
		return nil
	})
	return result, err
}

func loadBlockMoveJobByID(app core.App, jobID, actorID string) (storedBlockMoveJob, error) {
	params := dbx.Params{"jobId": jobID}
	actorClause := ""
	if actorID != "" {
		actorClause = " AND [[detail]].[[actor_id]]={:actorId}"
		params["actorId"] = actorID
	}
	job := storedBlockMoveJob{}
	err := app.DB().NewQuery(`
		SELECT [[detail]].[[job_id]],[[detail]].[[actor_id]],[[detail]].[[owner_id]],
			[[detail]].[[workspace_id]],[[detail]].[[source_owner_id]],
			[[detail]].[[source_workspace_id]],[[detail]].[[source_page_id]],
			[[detail]].[[destination_page_id]],[[detail]].[[root_block_id]],
			[[detail]].[[target_root_block_id]],[[detail]].[[operation]],
			[[detail]].[[request_hash]],[[base]].[[status]],[[detail]].[[phase]],
			[[detail]].[[revision]],[[detail]].[[discovered_count]],[[detail]].[[moved_count]],
			[[detail]].[[error_code]],[[detail]].[[created_at]],[[detail]].[[updated_at]],
			[[detail]].[[completed_at]]
		FROM {{leafen_block_move_jobs}} [[detail]]
		JOIN {{leafen_jobs}} [[base]] ON [[base]].[[id]]=[[detail]].[[job_id]]
		WHERE [[detail]].[[job_id]]={:jobId}` + actorClause + `
	`).Bind(params).One(&job)
	return job, err
}

func blockMoveResponseForJob(app core.App, job storedBlockMoveJob, duplicate bool) (blockMoveResponse, error) {
	status := blockMoveStatusPending
	if job.Status == durableJobStatusCompleted {
		status = blockMoveStatusCompleted
	} else if job.Status == durableJobStatusFailed || job.Phase == blockMovePhaseFailed {
		status = blockMoveStatusFailed
	}
	if status == blockMoveStatusCompleted && job.Operation == blockMoveOperationCopy {
		var failed, pending int
		children := partialIndexKey(`[[parent_block_copy_job_id]]`, `={:id}`)
		if err := app.DB().NewQuery(`SELECT
   EXISTS(SELECT 1 FROM {{leafen_page_duplication_jobs}} WHERE `+children+` AND [[phase]]='failed'),
   EXISTS(SELECT 1 FROM {{leafen_page_duplication_jobs}} WHERE `+children+` AND [[phase]] NOT IN ('completed','failed'))`).Bind(dbx.Params{"id": job.ID}).Row(&failed, &pending); err != nil {
			return blockMoveResponse{}, err
		}
		if failed != 0 {
			status = blockMoveStatusFailed
		} else if pending != 0 {
			status = blockMoveStatusPending
		}
	}
	return blockMoveResponse{
		JobID: job.ID, SourcePageID: job.SourcePageID,
		DestinationPageID: job.DestinationPageID, BlockID: job.RootBlockID,
		TargetBlockID: job.TargetRootBlockID, Operation: job.Operation,
		Status: status, Phase: job.Phase,
		Progress: blockMoveProgress{
			Revision: job.Revision, Discovered: job.DiscoveredCount, Moved: job.MovedCount,
		},
		ErrorCode: job.ErrorCode, Duplicate: duplicate,
	}, nil
}

func blockMoveRequestHash(sourcePageID, blockID string, input blockMoveInput) string {
	parts := []string{blockMoveJobKind, sourcePageID, blockID, input.DestinationPageID, input.Operation}
	if input.WithoutContent {
		parts = append(parts, "without-content")
	}
	value := strings.Join(parts, "\x00")
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func normalizeBlockMoveOperation(operation string) (string, bool) {
	switch strings.TrimSpace(operation) {
	case "", blockMoveOperationMove:
		return blockMoveOperationMove, true
	case blockMoveOperationCopy:
		return blockMoveOperationCopy, true
	default:
		return "", false
	}
}

func blockMovePageFenceActive(app core.App, pageID string) (bool, error) {
	return blockMoveSetFenceActive(app, []string{pageID})
}

// The parent copy stays fenced until its child page copies finish as well.
func blockMoveActiveFenceSQL(alias string) string {
	return `(` + alias + `.[[phase]] NOT IN ('completed','failed') OR (` + alias + `.[[phase]]='completed' AND EXISTS(SELECT 1 FROM {{leafen_page_duplication_jobs}} [[child]] WHERE ` + partialIndexKey(`[[child]].[[parent_block_copy_job_id]]`, `=`+alias+`.[[job_id]]`) + ` AND [[child]].[[phase]] NOT IN ('completed','failed'))))`
}
func blockMovePageUnfencedSQL(pageExpression string) string {
	return `NOT EXISTS (SELECT 1 FROM {{leafen_block_move_jobs}} [[move]] WHERE ` + blockMoveActiveFenceSQL("[[move]]") + ` AND ([[move]].[[source_page_id]]=` + pageExpression + ` OR [[move]].[[destination_page_id]]=` + pageExpression + `))`
}
func blockMoveSetFenceActive(app core.App, pageIDs []string) (bool, error) {
	if len(pageIDs) == 0 {
		return false, nil
	}
	encoded, err := json.Marshal(pageIDs)
	if err != nil {
		return false, err
	}
	var active int
	err = app.DB().NewQuery(`SELECT EXISTS(SELECT 1 FROM {{leafen_block_move_jobs}} [[move]] WHERE ` + blockMoveActiveFenceSQL("[[move]]") + ` AND ([[move]].[[source_page_id]] IN (SELECT [[value]] FROM json_each({:pageIds})) OR [[move]].[[destination_page_id]] IN (SELECT [[value]] FROM json_each({:pageIds}))))`).Bind(dbx.Params{"pageIds": string(encoded)}).Row(&active)
	return active != 0, err
}

func blockMovePagesCurrent(app core.App, job storedBlockMoveJob) (bool, error) {
	var count int
	// The source in its own workspace, which a copy from another reads.
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_pages}}
		WHERE [[notion_import_staging]]=0 AND (
			([[id]]={:sourcePageId} AND [[owner]]={:sourceOwnerId} AND [[workspace]]={:sourceWorkspaceId})
			OR ([[id]]={:destinationPageId} AND [[owner]]={:ownerId} AND [[workspace]]={:workspaceId}))
	`).Bind(dbx.Params{
		"ownerId": job.OwnerID, "workspaceId": job.WorkspaceID,
		"sourceOwnerId": job.sourceOwner(), "sourceWorkspaceId": job.sourceWorkspace(),
		"sourcePageId": job.SourcePageID, "destinationPageId": job.DestinationPageID,
	}).Row(&count)
	// A copy may stay on its page, which then counts once.
	expected := 2
	if job.SourcePageID == job.DestinationPageID {
		expected = 1
	}
	return count == expected, err
}

func blockMoveRollbackAfterID(
	app core.App,
	job storedBlockMoveJob,
	item storedBlockMoveItem,
) (*string, error) {
	var blockID string
	err := app.DB().NewQuery(`
		SELECT [[candidate]].[[block_id]]
		FROM {{leafen_block_move_items}} [[candidate]]
		JOIN {{leafen_blocks}} [[block]] ON [[block]].[[id]]=[[candidate]].[[block_id]]
		WHERE [[candidate]].[[job_id]]={:jobId} AND [[candidate]].[[parent_id]]={:parentId}
			AND [[candidate]].[[moved]]=0 AND [[block]].[[page_id]]={:sourcePageId}
			AND ([[candidate]].[[position]]<{:position}
				OR ([[candidate]].[[position]]={:position} AND [[candidate]].[[block_id]]<{:blockId}))
		ORDER BY [[candidate]].[[position]] DESC,[[candidate]].[[block_id]] DESC LIMIT 1
	`).Bind(dbx.Params{
		"jobId": job.ID, "parentId": item.ParentID, "sourcePageId": job.SourcePageID,
		"position": item.Position, "blockId": item.BlockID,
	}).Row(&blockID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &blockID, nil
}
