package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

var (
	errDatabaseMutationInvalid         = errors.New(errorCodeDatabaseMutationInvalid)
	errDatabaseMutationNotFound        = errors.New(errorCodeDatabaseMutationNotFound)
	errDatabaseMutationConflict        = errors.New(errorCodeDatabaseMutationConflict)
	errDatabaseMutationRequestIDReused = errors.New(errorCodeDatabaseMutationRequestIDReused)
)

type storedDatabaseMutationDeletion struct {
	ID                 string `db:"id"`
	ActorID            string `db:"actor_id"`
	OwnerID            string `db:"owner_id"`
	WorkspaceID        string `db:"workspace_id"`
	PageID             string `db:"page_id"`
	RequestID          string `db:"request_id"`
	RequestHash        string `db:"request_hash"`
	MutationID         string `db:"mutation_id"`
	TargetKind         string `db:"target_kind"`
	TargetID           string `db:"target_id"`
	Status             string `db:"status"`
	Phase              string `db:"phase"`
	Revision           int    `db:"revision"`
	ProcessedItems     int    `db:"processed_items"`
	CleanupLane        int    `db:"cleanup_lane"`
	NotBefore          int64  `db:"not_before"`
	ReceiptCursor      int    `db:"receipt_cursor"`
	ReceiptCountsJSON  string `db:"receipt_counts_json"`
	ReceiptStateVector string `db:"receipt_state_vector"`
	CreatedAt          int64  `db:"created_at"`
	UpdatedAt          int64  `db:"updated_at"`
	CompletedAt        int64  `db:"completed_at"`
}

type databaseMutationDeletionAdmission struct {
	Job       storedDatabaseMutationDeletion
	Admitted  bool
	Duplicate bool
}

func databaseMutationDeletionRequestHash(pageID string, input databaseMutationInput) string {
	encoded, _ := json.Marshal([]string{pageID, input.MutationID, input.TargetKind, input.TargetID})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func admitDatabaseMutationDeletion(
	app core.App,
	actorID, pageID string,
	input databaseMutationInput,
) (databaseMutationDeletionAdmission, error) {
	return admitDatabaseMutationDeletionBy(app, personAccessSubject(actorID), pageID, input)
}

func admitDatabaseMutationDeletionBy(
	app core.App,
	subject pageAccessSubject,
	pageID string,
	input databaseMutationInput,
) (databaseMutationDeletionAdmission, error) {
	result := databaseMutationDeletionAdmission{}
	err := app.RunInTransaction(func(txApp core.App) error {
		var err error
		result, err = admitDatabaseMutationDeletionInTransactionBy(txApp, subject, pageID, input)
		return err
	})
	return result, err
}

func admitDatabaseMutationDeletionInTransaction(
	app core.App,
	actorID, pageID string,
	input databaseMutationInput,
) (databaseMutationDeletionAdmission, error) {
	return admitDatabaseMutationDeletionInTransactionBy(app, personAccessSubject(actorID), pageID, input)
}

// admitDatabaseMutationDeletionInTransactionBy admits a deletion the subject
// asks for: a person, or the system (system_job_access.go), whose deletion
// keeps a request hash of its own so its turns are the system's too.
func admitDatabaseMutationDeletionInTransactionBy(
	app core.App,
	subject pageAccessSubject,
	pageID string,
	input databaseMutationInput,
) (databaseMutationDeletionAdmission, error) {
	result := databaseMutationDeletionAdmission{}
	actorID := subject.actorID()
	requestHash := databaseMutationRequestHashBy(subject, pageID, input)
	err := func() error {
		existing, err := loadDatabaseMutationDeletionByRequest(app, actorID, input.RequestID)
		if err == nil {
			if existing.RequestHash != requestHash {
				return errDatabaseMutationRequestIDReused
			}
			if _, err := subject.authorizePageBody(app, existing.PageID, permissionWorkspaceWrite); err != nil {
				return refusedUnlessStorage(errDatabaseMutationNotFound, err)
			}
			result = databaseMutationDeletionAdmission{Job: existing, Duplicate: true}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}

		authorization, err := subject.authorizePageBody(app, pageID, permissionWorkspaceWrite)
		if isPermissionDenied(err) {
			return errDatabaseMutationNotFound
		}
		if err != nil {
			return err
		}
		sourceID, err := databaseMutationDeletionTargetSource(
			app, authorization.Workspace.OwnerID, pageID, input.TargetKind, input.TargetID,
		)
		if errors.Is(err, sql.ErrNoRows) {
			return errDatabaseMutationNotFound
		}
		if err != nil {
			return err
		}
		if err := validateDatabaseMutationDeletionReferences(
			app, authorization.Workspace.OwnerID, pageID, input.TargetKind, input.TargetID,
		); err != nil {
			return err
		}
		if err := guardWikiDatabaseDeletion(app, input.TargetKind, input.TargetID); err != nil {
			return err
		}
		fenced, err := databaseMutationDeletionTargetFenced(
			app, authorization.Workspace.OwnerID, pageID, sourceID, input.TargetKind, input.TargetID,
		)
		if err != nil {
			return err
		}
		if fenced {
			return errDatabaseMutationConflict
		}

		now := time.Now().UTC().UnixMilli()
		job := storedDatabaseMutationDeletion{
			ID: core.GenerateDefaultRandomId(), ActorID: actorID,
			OwnerID:     authorization.Workspace.OwnerID,
			WorkspaceID: authorization.Workspace.WorkspaceID,
			PageID:      pageID, RequestID: input.RequestID, RequestHash: requestHash,
			MutationID: input.MutationID, TargetKind: input.TargetKind, TargetID: input.TargetID,
			Status: databaseMutationStatusPending, Phase: databaseMutationInitialPhase(input.TargetKind),
			Revision: 1, NotBefore: now, CreatedAt: now, UpdatedAt: now,
		}
		_, err = app.DB().Insert(databaseMutationJobsTable, dbx.Params{
			"id": job.ID, "actor_id": job.ActorID, "owner_id": job.OwnerID,
			"workspace_id": job.WorkspaceID, "page_id": job.PageID,
			"request_id": job.RequestID, "request_hash": job.RequestHash,
			"mutation_id": job.MutationID, "target_kind": job.TargetKind,
			"target_id": job.TargetID, "status": job.Status, "phase": job.Phase,
			"revision": job.Revision, "processed_items": 0, "cleanup_lane": 0, "not_before": now,
			"receipt_cursor": 0, "receipt_counts_json": "", "receipt_state_vector": "",
			"created_at": now, "updated_at": now, "completed_at": 0,
		}).Execute()
		if err != nil {
			return err
		}
		// The other side of a two-way relation follows the deletion.
		pairWork, err := planDatabaseRelationPairDeletion(app, job.OwnerID, job.TargetKind, job.TargetID)
		if err != nil {
			return err
		}
		if err := admitDatabaseRelationPairWork(
			app, job.OwnerID, job.WorkspaceID, actorID, "deletion:"+job.ID, pairWork,
		); err != nil {
			return err
		}
		// So do other pages' relations naming a row deleted for good; its
		// own page's lose it in the job's incoming turns.
		if job.TargetKind == databaseMutationTargetRow {
			if err := admitGoneRelationTargetWork(
				app, job.OwnerID, job.WorkspaceID, actorID, "gone:"+job.ID, []string{job.TargetID}, nil,
			); err != nil {
				return err
			}
		}
		result = databaseMutationDeletionAdmission{Job: job, Admitted: true}
		return nil
	}()
	return result, err
}

func databaseMutationInitialPhase(targetKind string) string {
	switch targetKind {
	case databaseMutationTargetSource:
		return databaseMutationPhaseSourceCells
	case databaseMutationTargetProperty:
		return databaseMutationPhasePropertyCells
	case databaseMutationTargetRow:
		return databaseMutationPhaseRowIncoming
	default:
		return ""
	}
}

func databaseMutationDeletionTargetSource(
	app core.App, owner, pageID, targetKind, targetID string,
) (string, error) {
	sourceID := targetID
	query := `SELECT [[id]] FROM {{leafen_data_sources}}
		WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[id]]={:targetId}`
	if targetKind == databaseMutationTargetProperty {
		query = `SELECT [[data_source_id]] FROM {{leafen_database_properties}}
			WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[id]]={:targetId}`
	} else if targetKind == databaseMutationTargetRow {
		query = `SELECT [[data_source_id]] FROM {{leafen_database_rows}}
			WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[id]]={:targetId}`
	}
	err := app.DB().NewQuery(query).Bind(dbx.Params{
		"owner": owner, "pageId": pageID, "targetId": targetID,
	}).Row(&sourceID)
	return sourceID, err
}

func validateDatabaseMutationDeletionReferences(
	app core.App, owner, pageID, targetKind, targetID string,
) error {
	var blocked int
	query := ""
	workspaceID := ""
	if targetKind == databaseMutationTargetSource {
		page, err := app.FindRecordById(pagesCollection, pageID)
		if err != nil {
			return err
		}
		workspaceID = page.GetString("workspace")
		query = `SELECT COUNT(*) FROM (
			SELECT [[target_source_id]] AS [[reference_id]]
			FROM {{leafen_database_relation_property_targets}}
			INDEXED BY [[idx_leafen_database_relation_property_target]]
			WHERE [[owner]]={:owner} AND [[page_id]]={:pageId}
				AND [[target_source_id]]={:targetId} AND [[source_id]]!={:targetId}
			UNION ALL
			SELECT [[container_id]] AS [[reference_id]]
			FROM {{leafen_database_container_sources}}
			INDEXED BY [[idx_leafen_database_container_sources_source]]
			WHERE [[owner]]={:owner} AND [[workspace_id]]={:workspaceId}
				AND [[data_source_id]]={:targetId}
		)`
	} else if targetKind == databaseMutationTargetProperty {
		query = `SELECT COUNT(*) FROM {{leafen_database_view_property_refs}}
			INDEXED BY [[idx_leafen_database_view_property_ref]]
			WHERE [[owner]]={:owner} AND [[page_id]]={:pageId} AND [[property_id]]={:targetId}`
	} else {
		return nil
	}
	if err := app.DB().NewQuery(query).Bind(dbx.Params{
		"owner": owner, "workspaceId": workspaceID, "pageId": pageID, "targetId": targetID,
	}).Row(&blocked); err != nil {
		return err
	}
	if blocked != 0 {
		return errDatabaseMutationConflict
	}
	return nil
}

func databaseMutationDeletionTargetFenced(
	app core.App, owner, pageID, sourceID, targetKind, targetID string,
) (bool, error) {
	var count int
	err := app.DB().NewQuery(`
		SELECT COUNT(*) FROM {{leafen_database_mutation_jobs}} AS [[jobs]]
		WHERE [[jobs]].[[owner_id]]={:owner} AND [[jobs]].[[page_id]]={:pageId}
			AND [[jobs]].[[status]]='pending' AND (
				([[jobs]].[[target_kind]]={:targetKind} AND [[jobs]].[[target_id]]={:targetId}) OR
				([[jobs]].[[target_kind]]='source' AND [[jobs]].[[target_id]]={:sourceId}) OR
				({:targetKind}='source' AND (
					([[jobs]].[[target_kind]]='property' AND EXISTS (
						SELECT 1 FROM {{leafen_database_properties}} AS [[properties]]
						WHERE [[properties]].[[id]]=[[jobs]].[[target_id]] AND [[properties]].[[data_source_id]]={:sourceId}
					)) OR ([[jobs]].[[target_kind]]='row' AND EXISTS (
						SELECT 1 FROM {{leafen_database_rows}} AS [[rows]]
						WHERE [[rows]].[[id]]=[[jobs]].[[target_id]] AND [[rows]].[[data_source_id]]={:sourceId}
					))
				))
			)
	`).Bind(dbx.Params{
		"owner": owner, "pageId": pageID, "sourceId": sourceID,
		"targetKind": targetKind, "targetId": targetID,
	}).Row(&count)
	return count != 0, err
}

const databaseMutationDeletionSelect = `
	SELECT [[id]],[[actor_id]],[[owner_id]],[[workspace_id]],[[page_id]],
		[[request_id]],[[request_hash]],[[mutation_id]],[[target_kind]],[[target_id]],
		[[status]],[[phase]],[[revision]],[[processed_items]],[[cleanup_lane]],[[not_before]],
		[[receipt_cursor]],[[receipt_counts_json]],[[receipt_state_vector]],
		[[created_at]],[[updated_at]],[[completed_at]]
	FROM {{leafen_database_mutation_jobs}}`

func loadDatabaseMutationDeletionByRequest(app core.App, actorID, requestID string) (storedDatabaseMutationDeletion, error) {
	job := storedDatabaseMutationDeletion{}
	err := app.DB().NewQuery(databaseMutationDeletionSelect + `
		WHERE [[actor_id]]={:actorId} AND [[request_id]]={:requestId}`,
	).Bind(dbx.Params{"actorId": actorID, "requestId": requestID}).One(&job)
	return job, err
}

func loadDatabaseMutationDeletionByID(app core.App, jobID string) (storedDatabaseMutationDeletion, error) {
	job := storedDatabaseMutationDeletion{}
	err := app.DB().NewQuery(databaseMutationDeletionSelect + ` WHERE [[id]]={:jobId}`).Bind(dbx.Params{"jobId": jobID}).One(&job)
	return job, err
}

func databaseMutationDeletionResponse(job storedDatabaseMutationDeletion, duplicate bool) (databaseMutationResponse, error) {
	response := databaseMutationResponse{
		JobID: job.ID, PageID: job.PageID, MutationID: job.MutationID,
		TargetKind: job.TargetKind, TargetID: job.TargetID,
		Status: job.Status, Phase: job.Phase,
		Progress:  databaseMutationProgress{Revision: job.Revision, Processed: job.ProcessedItems},
		Duplicate: duplicate,
	}
	if job.Status == databaseMutationStatusCompleted {
		counts := graphMutationCounts{}
		if err := json.Unmarshal([]byte(job.ReceiptCountsJSON), &counts); err != nil {
			return databaseMutationResponse{}, err
		}
		response.Receipt = &graphMutationReceipt{
			MutationID: job.MutationID, Cursor: job.ReceiptCursor,
			Duplicate: duplicate, Counts: counts, StateVector: job.ReceiptStateVector,
		}
	}
	return response, nil
}
