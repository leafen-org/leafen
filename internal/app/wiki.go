package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

const (
	wikiMutationJobKind        = "wiki-mutation"
	wikiMutationDefaultRetries = 3
	wikiMutationMaxBodyBytes   = 4096
)

var (
	errWikiInvalid         = errors.New(errorCodeWikiInvalid)
	errWikiNotFound        = errors.New(errorCodeWikiNotFound)
	errWikiRequestIDReused = errors.New(errorCodeWikiRequestIDReused)
	errWikiConflict        = errors.New(errorCodeWikiConflict)
	errWikiRevisionChanged = errors.New(errorCodeWikiRevisionChanged)
)

type storedWikiMutation struct {
	ID                    string `db:"job_id"`
	ActorID               string `db:"actor_id"`
	WorkspaceOwnerID      string `db:"workspace_owner_id"`
	WorkspaceID           string `db:"workspace_id"`
	RootPageID            string `db:"root_page_id"`
	TargetPageID          string `db:"target_page_id"`
	Operation             string `db:"operation"`
	ExpectedRevision      int    `db:"expected_revision"`
	TargetRevision        int    `db:"target_revision"`
	Phase                 string `db:"phase"`
	RequestHash           string `db:"request_hash"`
	OwnerIDsJSON          string `db:"owner_ids_json"`
	VerificationExpiresAt string `db:"verification_expires_at"`
	Status                string `db:"status"`
	Revision              int    `db:"revision"`
	ItemCount             int    `db:"item_count"`
	ProcessedCount        int    `db:"processed_count"`
	ErrorCode             string `db:"error_code"`
	PayloadJSON           string `db:"payload_json"`
	CreatedAt             int64  `db:"created_at"`
	UpdatedAt             int64  `db:"updated_at"`
	CompletedAt           int64  `db:"completed_at"`
}

type wikiMutationAdmission struct {
	Job       storedWikiMutation
	Duplicate bool
}

func normalizeWikiMutationInput(input wikiMutationInput) (wikiMutationInput, error) {
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.Operation = strings.TrimSpace(input.Operation)
	if err := wikiMutationInputRefusal(input); err != nil {
		return wikiMutationInput{}, err
	}
	return input, nil
}

func validWikiOperation(operation string) bool {
	switch operation {
	case wikiOperationConvert, wikiOperationUndo:
		return true
	default:
		return false
	}
}

func admitWikiMutation(
	app core.App,
	actorID, targetPageID string,
	input wikiMutationInput,
) (wikiMutationAdmission, error) {
	input, err := normalizeWikiMutationInput(input)
	if err != nil {
		return wikiMutationAdmission{}, err
	}
	payload := "{}"
	if input.Operation == wikiOperationConvert {
		names, err := completeWikiDatabaseNames(input.Names, generatedNamesIn(accountLanguageEn).Wiki)
		if err != nil {
			return wikiMutationAdmission{}, err
		}
		if payload, err = wikiDatabaseJobPayloadJSON(names); err != nil {
			return wikiMutationAdmission{}, err
		}
	}
	requestHash := wikiMutationRequestHash(targetPageID, input)
	result := wikiMutationAdmission{}
	err = app.RunInTransaction(func(txApp core.App) error {
		existing, readErr := loadStoredJobByRequest(txApp, actorID, input.RequestID)
		if readErr == nil {
			if existing.Kind != wikiMutationJobKind || existing.RequestHash != requestHash {
				return errWikiRequestIDReused
			}
			job, loadErr := loadWikiMutationByID(txApp, existing.ID, actorID)
			if loadErr != nil {
				return loadErr
			}
			if _, authErr := authorizeWorkspace(
				txApp, actorID, job.WorkspaceID, permissionWorkspaceRead,
			); authErr != nil {
				return refusedUnlessStorage(errWikiNotFound, authErr)
			}
			result = wikiMutationAdmission{Job: job, Duplicate: true}
			return nil
		}
		if !errors.Is(readErr, sql.ErrNoRows) {
			return readErr
		}

		authorization, authErr := authorizePage(
			txApp, actorID, targetPageID, permissionWorkspaceRead,
		)
		if authErr != nil {
			if isPermissionDenied(authErr) {
				return errWikiNotFound
			}
			return authErr
		}
		if authorization.AccessRole < pageAccessRoleEdit {
			return errWikiNotFound
		}
		target := authorization.Page
		fenced, fenceErr := wikiMutationFenceActive(txApp, targetPageID)
		if fenceErr != nil {
			return fenceErr
		}
		if fenced {
			return errWikiPageBusy
		}
		duplicationFenced, fenceErr := pageDuplicationSourceFenceActive(txApp, targetPageID)
		if fenceErr != nil {
			return fenceErr
		}
		if duplicationFenced {
			return wikiStateRefused("the page is being copied; try again when the copy finishes")
		}
		leased, leaseErr := workspaceMarkdownReplacePageLeased(txApp, targetPageID, "")
		if leaseErr != nil {
			return leaseErr
		}
		if leased {
			return wikiStateRefused("the page's content is being replaced from Markdown; try again when that finishes")
		}
		var activeWorkspaceMutations int
		if err := txApp.DB().NewQuery(`
			SELECT COUNT(*) FROM (
				SELECT 1 FROM {{leafen_wiki_mutations}}
					INDEXED BY [[idx_leafen_wiki_mutations_workspace_phase]]
				WHERE [[workspace_id]]={:workspaceId}
					AND [[phase]] IN ('discovering','applying','publishing','cleaning') LIMIT 1
			)
		`).Bind(dbx.Params{
			"workspaceId": authorization.Workspace.WorkspaceID,
		}).Row(&activeWorkspaceMutations); err != nil {
			return err
		}
		if activeWorkspaceMutations != 0 {
			return wikiStateRefused("another wiki change in this workspace is still running; try again when it finishes")
		}
		// A wiki whose pages are its database's pages holds its database.
		held, heldErr := loadWikiDatabaseOnPage(txApp, authorization.Workspace.OwnerID, targetPageID)
		if heldErr != nil {
			return heldErr
		}
		rootPageID := targetPageID
		switch input.Operation {
		case wikiOperationConvert:
			// A page under a wiki is the wiki's. A full-page database is a
			// database, not a page to turn.
			switch {
			case target.GetBool("is_wiki") || len(held.Sources) > 0:
				return wikiStateRefused("the page is a wiki already")
			case target.GetString("data_source_id") != "":
				return wikiStateRefused("the page is a database row: a page of its own becomes a wiki")
			case isFullPageDatabase(target):
				return wikiStateRefused("the page is a database, not a page to turn into a wiki")
			}
			within, withinErr := pageWithinWiki(txApp, targetPageID)
			if withinErr != nil {
				return withinErr
			}
			if within {
				return wikiStateRefused("the page is inside a wiki, which holds it already")
			}
		case wikiOperationUndo:
			if !target.GetBool("is_wiki") || len(held.Sources) == 0 {
				return errWikiNotFound
			}
		}
		if target.GetInt("wiki_revision") != input.ExpectedRevision {
			return errWikiRevisionChanged
		}

		phase := wikiMutationPhaseDiscovering
		if input.Operation == wikiOperationUndo {
			phase = wikiMutationPhasePublishing
		}
		now := time.Now().UTC().UnixMilli()
		result.Job = storedWikiMutation{
			ID: core.GenerateDefaultRandomId(), ActorID: actorID, WorkspaceOwnerID: authorization.Workspace.OwnerID,
			WorkspaceID: authorization.Workspace.WorkspaceID, RootPageID: rootPageID,
			TargetPageID: targetPageID, Operation: input.Operation,
			ExpectedRevision: input.ExpectedRevision, TargetRevision: input.ExpectedRevision + 1,
			Phase: phase, RequestHash: requestHash, OwnerIDsJSON: "[]",
			Status: durableJobStatusQueued, PayloadJSON: payload, Revision: 1, ItemCount: 1,
			CreatedAt: now, UpdatedAt: now,
		}
		return insertWikiMutationJob(txApp, result.Job, input.RequestID, target)
	})
	return result, err
}

// insertWikiMutationJob records an admitted wiki job, with the page a
// conversion starts from and the payload it starts with, and fences the page.
func insertWikiMutationJob(txApp core.App, job storedWikiMutation, requestID string, target *core.Record) error {
	if job.PayloadJSON == "" {
		job.PayloadJSON = "{}"
	}
	_, err := txApp.DB().NewQuery(`
		INSERT INTO {{leafen_jobs}}
			([[id]],[[owner]],[[workspace_id]],[[request_id]],[[request_hash]],[[kind]],[[page_id]],
			 [[status]],[[error_code]],[[diagnostic_reference]],[[not_before]],[[created]],[[updated]],
			 [[payload_json]],[[cursor]],[[processed_items]],[[total_items]],[[attempt]],[[max_attempts]],
			 [[chunk_count]],[[captured_bytes]])
		VALUES ({:id},{:owner},{:workspaceId},{:requestId},{:requestHash},{:kind},{:pageId},
			'queued','','',{:now},{:now},{:now},{:payload},'',0,1,0,{:maxAttempts},0,0)
	`).Bind(dbx.Params{
		"id": job.ID, "owner": job.ActorID, "workspaceId": job.WorkspaceID,
		"requestId": requestID, "requestHash": job.RequestHash,
		"kind": wikiMutationJobKind, "pageId": job.TargetPageID,
		"now": job.CreatedAt, "maxAttempts": wikiMutationDefaultRetries, "payload": job.PayloadJSON,
	}).Execute()
	if err != nil {
		return err
	}
	_, err = txApp.DB().Insert(wikiMutationsTable, dbx.Params{
		"job_id": job.ID, "actor_id": job.ActorID,
		"workspace_owner_id": job.WorkspaceOwnerID,
		"workspace_id":       job.WorkspaceID,
		"root_page_id":       job.RootPageID, "target_page_id": job.TargetPageID,
		"operation": job.Operation, "expected_revision": job.ExpectedRevision,
		"target_revision": job.TargetRevision, "phase": job.Phase,
		"request_hash": job.RequestHash, "owner_ids_json": job.OwnerIDsJSON,
		"verification_expires_at": job.VerificationExpiresAt, "revision": job.Revision,
		"item_count": job.ItemCount, "processed_count": 0, "error_code": "",
		"created_at": job.CreatedAt, "updated_at": job.UpdatedAt, "completed_at": 0,
	}).Execute()
	if err != nil {
		return err
	}
	if job.Operation == wikiOperationConvert {
		_, err = txApp.DB().Insert(wikiMutationItemsTable, dbx.Params{
			"job_id": job.ID, "page_id": job.TargetPageID,
			"parent_id": target.GetString("parent_id"), "depth": 0,
			"created_at": job.CreatedAt, "updated_at": job.CreatedAt,
		}).Execute()
		if err != nil {
			return err
		}
	}
	target.Set("wiki_active_job_id", job.ID)
	return txApp.Save(target)
}

func loadWikiMutationByID(app core.App, jobID, actorID string) (storedWikiMutation, error) {
	params := dbx.Params{"jobId": jobID}
	actorClause := ""
	if actorID != "" {
		actorClause = " AND [[detail]].[[actor_id]]={:actorId}"
		params["actorId"] = actorID
	}
	job := storedWikiMutation{}
	err := app.DB().NewQuery(`
		SELECT [[detail]].[[job_id]],[[detail]].[[actor_id]],[[detail]].[[workspace_owner_id]],
			[[detail]].[[workspace_id]],[[detail]].[[root_page_id]],[[detail]].[[target_page_id]],
			[[detail]].[[operation]],[[detail]].[[expected_revision]],[[detail]].[[target_revision]],
			[[detail]].[[phase]],[[detail]].[[request_hash]],[[detail]].[[owner_ids_json]],
			[[detail]].[[verification_expires_at]],
			[[base]].[[status]],[[base]].[[payload_json]],[[detail]].[[revision]],[[detail]].[[item_count]],
			[[detail]].[[processed_count]],[[detail]].[[error_code]],[[detail]].[[created_at]],
			[[detail]].[[updated_at]],[[detail]].[[completed_at]]
		FROM {{leafen_wiki_mutations}} [[detail]]
		JOIN {{leafen_jobs}} [[base]] ON [[base]].[[id]]=[[detail]].[[job_id]]
		WHERE [[detail]].[[job_id]]={:jobId}` + actorClause + `
	`).Bind(params).One(&job)
	return job, err
}

func wikiMutationResponseForJob(job storedWikiMutation, duplicate bool) (wikiMutationResponse, error) {
	status := wikiMutationStatusPending
	var result *wikiMutationResult
	if job.Status == durableJobStatusCompleted {
		status = wikiMutationStatusCompleted
		var err error
		result, err = decodeWikiMutationResult(job)
		if err != nil {
			return wikiMutationResponse{}, err
		}
	} else if job.Status == durableJobStatusFailed || job.Phase == wikiMutationPhaseFailed {
		status = wikiMutationStatusFailed
	}
	return wikiMutationResponse{
		JobID: job.ID, RootPageID: job.RootPageID, Operation: job.Operation,
		Status: status, Phase: job.Phase,
		Progress: wikiMutationProgress{
			Revision: job.Revision, Processed: job.ProcessedCount, Total: job.ItemCount,
		},
		ErrorCode: job.ErrorCode, Duplicate: duplicate, Result: result,
	}, nil
}

func wikiMutationRequestHash(pageID string, input wikiMutationInput) string {
	value := strings.Join([]string{
		wikiMutationJobKind, pageID, input.Operation, strconv.Itoa(input.ExpectedRevision),
		// Owners and a verification's end once had places here; requests
		// made before keep their hash.
		"", "",
	}, "\x00")
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

// pageWithinWiki reports whether a page sits in a wiki: under a wiki's page
// or a page of one.
func pageWithinWiki(app core.App, pageID string) (bool, error) {
	var found int
	err := app.DB().NewQuery(`
		WITH RECURSIVE [[above]] ([[id]]) AS (
			SELECT ` + pageAncestryParentSQL("[[page]]") + ` FROM {{leafen_pages}} [[page]] WHERE [[page]].[[id]]={:pageId}
			UNION
			SELECT ` + pageAncestryParentSQL("[[page]]") + `
			FROM {{leafen_pages}} [[page]] JOIN [[above]] ON [[page]].[[id]]=[[above]].[[id]]
			WHERE [[above]].[[id]]!=''
		)
		SELECT COUNT(*) FROM [[above]] JOIN {{leafen_pages}} [[page]] ON [[page]].[[id]]=[[above]].[[id]]
		WHERE [[page]].[[is_wiki]]=1
	`).Bind(dbx.Params{"pageId": pageID}).Row(&found)
	return found > 0, err
}

func wikiMutationFenceActive(app core.App, pageID string) (bool, error) {
	if !tableKnown(app, wikiMutationsTable) {
		return false, nil
	}
	// Every page above, however deep, each once.
	var active int
	err := app.DB().NewQuery(`
		WITH RECURSIVE [[ancestors]] ([[id]],[[parent_id]],[[job_id]]) AS (
			SELECT [[page]].[[id]],` + pageAncestryParentSQL("[[page]]") + `,[[page]].[[wiki_active_job_id]]
			FROM {{leafen_pages}} [[page]] WHERE [[page]].[[id]]={:pageId}
			UNION
			SELECT [[page]].[[id]],` + pageAncestryParentSQL("[[page]]") + `,[[page]].[[wiki_active_job_id]]
			FROM {{leafen_pages}} [[page]] JOIN [[ancestors]] ON [[page]].[[id]]=[[ancestors]].[[parent_id]]
			WHERE [[ancestors]].[[parent_id]]!=''
		)
		SELECT
			COALESCE(MAX(CASE WHEN [[job_id]]!='' AND EXISTS (
				SELECT 1 FROM {{leafen_wiki_mutations}} [[mutation]]
				JOIN {{leafen_jobs}} [[job]] ON [[job]].[[id]]=[[mutation]].[[job_id]]
				WHERE [[mutation]].[[job_id]]=[[ancestors]].[[job_id]]
					AND [[job]].[[status]]='queued'
			) THEN 1 ELSE 0 END),0)
		FROM [[ancestors]]
	`).Bind(dbx.Params{"pageId": pageID}).Row(&active)
	return active > 0, err
}
