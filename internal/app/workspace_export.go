package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

const workspaceExportJobKind = "workspace-export"

type workspaceExportJobPayload struct {
	AuthorityOwnerID string                 `json:"authorityOwnerId"`
	WorkspaceName    string                 `json:"workspaceName"`
	Format           string                 `json:"format"`
	Options          workspaceExportOptions `json:"options"`
	Scope            workspaceExportScope   `json:"scope"`
	SourceRevision   string                 `json:"sourceRevision"`
	// SourceItems is the count the source revision was taken over when the
	// export was asked for: an archive's run counts the same way again to
	// know whether pages came or went (the job's total later counts its parts).
	SourceItems     int                                `json:"sourceItems,omitzero"`
	Phase           string                             `json:"phase"`
	ScopeCursor     string                             `json:"scopeCursor,omitempty"`
	InventoryCursor string                             `json:"inventoryCursor"`
	PagesExported   int                                `json:"pagesExported,omitzero"`
	RowsExported    int                                `json:"rowsExported,omitzero"`
	CreatedAt       string                             `json:"createdAt"`
	NotionAdmin     *notionAdminSpaceExportJobMetadata `json:"notionAdmin,omitempty"`
	// OmittedPages counts the database pages shared on their own that a
	// workspace archive leaves out.
	OmittedPages int `json:"omittedPages,omitzero"`
	// OmittedFiles counts the files named by its pages that were still arriving or gone.
	OmittedFiles int `json:"omittedFiles,omitzero"`
	// Language is the one its person asked in, for the dates it writes.
	Language string `json:"language,omitempty"`
	// LinkedView is the linked database a database export was asked for by
	// (workspace_export_linked_view.go).
	LinkedView *workspaceExportLinkedView `json:"linkedView,omitempty"`
}

type workspaceExportAdmissionInput struct {
	ActorID     string
	WorkspaceID string
	Format      string
	Options     workspaceExportOptions
	// Scope is the one its request names; a linked database's stands for
	// its first source at admission (resolveWorkspaceExportLinkedView).
	Scope       workspaceExportScope
	RequestID   func(sourceRevision string) string
	NotionAdmin *notionAdminSpaceExportJobMetadata
	Language    string
}

type workspaceExportAdmissionResult struct {
	Authorization  workspaceAuthorization
	JobID          string
	Duplicate      bool
	IdentityReused bool
}

type storedWorkspaceExportArtifact struct {
	JobID       string `db:"job_id"`
	WorkspaceID string `db:"workspace_id"`
	Owner       string `db:"owner"`
	Format      string `db:"format"`
	Filename    string `db:"filename"`
	ContentType string `db:"content_type"`
	Bytes       int64  `db:"bytes"`
	SHA256      string `db:"sha256"`
	Created     int64  `db:"created"`
}

func registerWorkspaceExportRoutes(event *core.ServeEvent, jobs *durableJobRunner) {
	event.Router.POST(apiRoot+"/workspaces/{workspaceId}/exports", jobs.createWorkspaceExport)
	event.Router.GET(apiRoot+"/workspaces/{workspaceId}/exports/current", jobs.getCurrentWorkspaceExport)
	event.Router.GET(apiRoot+"/workspaces/{workspaceId}/exports/policy", jobs.getWorkspaceExportPolicy)
	event.Router.GET(apiRoot+"/workspaces/{workspaceId}/exports/{jobId}", jobs.getWorkspaceExport)
	event.Router.POST(apiRoot+"/workspaces/{workspaceId}/exports/{jobId}/retry", jobs.retryWorkspaceExport)
	event.Router.POST(apiRoot+"/workspaces/{workspaceId}/exports/{jobId}/cancel", jobs.cancelWorkspaceExport)
	event.Router.GET(apiRoot+"/workspaces/{workspaceId}/exports/{jobId}/download", jobs.downloadWorkspaceExport)
}

func (r *durableJobRunner) createWorkspaceExport(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	var input workspaceExportInput
	if err := event.BindBody(&input); err != nil {
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the export details.")
	}
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.Format = strings.TrimSpace(input.Format)
	if input.Scope.Kind == "" && input.Scope.TargetID == "" &&
		(input.Format == workspaceExportFormatNativeArchive || input.Format == workspaceExportFormatMarkdownCSV ||
			input.Format == workspaceExportFormatHTML) {
		input.Scope.Kind = workspaceExportScopeWorkspace
	}
	scope, scopeValid := normalizeWorkspaceExportScope(input.Format, input.Scope)
	options, valid := normalizeWorkspaceExportOptions(input.Format, input.Options)
	if input.RequestID == "" || len([]rune(input.RequestID)) > maxWorkspaceExportRequestIDRunes || !scopeValid || !valid {
		apiErrorAbout(event, nil, workspaceExportRefusedFields(input.Format, input.Scope, input.Options)...)
		return apiError(event, http.StatusBadRequest, errorCodeInvalidInput, "Check the export details.")
	}
	if err := requireDataDiskRoom(event.App, 0); err != nil {
		return writeServerDiskLow(event, "workspace-export", err)
	}

	// The dates it writes are in the language its person asks in.
	language, _ := serverTextLanguage(event.App, user.Id, event.Request)
	result := workspaceExportAdmissionResult{}
	var err error
	err = event.App.RunInTransaction(func(txApp core.App) error {
		result, err = admitWorkspaceExportInTransaction(txApp, workspaceExportAdmissionInput{
			ActorID: user.Id, WorkspaceID: workspaceID, Format: input.Format, Scope: scope, Options: options,
			RequestID: func(string) string { return input.RequestID }, Language: language,
		})
		return err
	})
	// A page an operation holds a while is held, not gone: asked again once it's done (page_access_hold.go).
	if pageID, busy := pageAccessHeldOn(err); busy != "" {
		return answerPageHeld(event, pageID, busy)
	}
	if isPermissionDenied(err) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
	}
	if result.IdentityReused {
		return apiError(event, http.StatusConflict, errorCodeWorkspaceExportIdentityReused, "The job request ID was used for a different export.")
	}
	if isDLPPolicyDecisionError(err) {
		return writeDLPPolicyError(event, err, "workspace-export")
	}
	if errors.Is(err, errOrganizationSharingSecurityBlocked) {
		return writeSharingSecurityBlockedError(event)
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeWorkspaceExportCreateFailed, "Couldn't start the export.", err)
	}
	r.notify()
	response, err := r.workspaceExportResponse(result.JobID, user.Id, result.Authorization.WorkspaceID)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeWorkspaceExportReadFailed, "Couldn't read the export.", err)
	}
	status := http.StatusAccepted
	if result.Duplicate {
		status = http.StatusOK
	}
	return event.JSON(status, workspaceExportJobResult{Replayed: result.Duplicate, Job: response})
}

func admitWorkspaceExportInTransaction(
	app core.App,
	input workspaceExportAdmissionInput,
) (workspaceExportAdmissionResult, error) {
	result := workspaceExportAdmissionResult{JobID: "workspace-export-" + security.RandomString(15)}
	scope, linkedView, err := resolveWorkspaceExportLinkedView(app, input.ActorID, input.WorkspaceID, input.Scope)
	if err != nil {
		return result, err
	}
	scopeAuthorization, err := authorizeWorkspaceExportScope(app, input.ActorID, input.WorkspaceID, scope)
	if err != nil {
		return result, err
	}
	authorization := scopeAuthorization.Workspace
	result.Authorization = authorization
	sourceRevision, totalItems, err := workspaceExportSourceRevision(app, authorization)
	if err != nil {
		return result, err
	}
	requestID := strings.TrimSpace(input.RequestID(sourceRevision))
	if requestID == "" || len([]rune(requestID)) > maxWorkspaceExportRequestIDRunes {
		return result, errors.New(errorCodeWorkspaceExportCreateFailed)
	}
	requestHash := workspaceExportRequestHash(input.WorkspaceID, requestID, input.Format, input.Scope, input.Options, "")
	sameRequest := func(existing storedJob) bool {
		return workspaceExportSameRequest(existing, requestHash, func(admittedRevision string) string {
			return workspaceExportRequestHash(input.WorkspaceID, requestID, input.Format, scope, input.Options, admittedRevision)
		})
	}
	existing, loadErr := loadStoredJobByRequest(app, input.ActorID, requestID)
	if loadErr == nil {
		if !sameRequest(existing) {
			result.IdentityReused = true
			return result, nil
		}
		result.JobID, result.Duplicate = existing.ID, true
		return result, nil
	}
	if !errors.Is(loadErr, sql.ErrNoRows) {
		return result, loadErr
	}
	// The same person exporting the same thing again while it is still
	// being made gets that export back rather than a second one queued
	// behind it (2026-10-18: a new request ID made a new job each time).
	if input.NotionAdmin == nil {
		running, found, err := runningWorkspaceExport(app, input, scope, linkedView)
		if err != nil {
			return result, err
		}
		if found {
			result.JobID, result.Duplicate = running, true
			return result, nil
		}
	}
	// The organization's own export through its admin API isn't a member's
	// and answers to its DLP policy alone.
	if err := workspaceExportPolicyRefusal(app, scopeAuthorization, input.NotionAdmin != nil); err != nil {
		return result, err
	}

	now := time.Now().UTC()
	payload, err := json.Marshal(workspaceExportJobPayload{
		AuthorityOwnerID: authorization.OwnerID,
		WorkspaceName:    authorization.Workspace.GetString("name"),
		Format:           input.Format, Scope: scope, Options: input.Options, SourceRevision: sourceRevision,
		SourceItems: totalItems,
		Phase:       workspaceExportPhaseScope, CreatedAt: now.Format(time.RFC3339Nano),
		NotionAdmin: input.NotionAdmin, Language: input.Language, LinkedView: linkedView,
	})
	if err != nil {
		return result, err
	}
	insert, err := app.DB().NewQuery(`
		INSERT INTO {{leafen_jobs}}
			([[id]], [[owner]], [[workspace_id]], [[request_id]], [[request_hash]], [[kind]], [[page_id]],
			 [[status]], [[error_code]], [[not_before]], [[created]], [[updated]], [[payload_json]],
			 [[cursor]], [[processed_items]], [[total_items]], [[attempt]], [[max_attempts]], [[chunk_count]])
		VALUES
			({:id}, {:owner}, {:workspaceId}, {:requestId}, {:requestHash}, {:kind}, {:targetId},
			 'queued', '', {:notBefore}, {:now}, {:now}, {:payload}, '', 0, {:totalItems}, 0, {:maxAttempts}, 0)
		ON CONFLICT ([[owner]], [[request_id]]) DO NOTHING
	`).Bind(dbx.Params{
		"id": result.JobID, "owner": input.ActorID, "workspaceId": input.WorkspaceID,
		"requestId": requestID, "requestHash": requestHash, "kind": workspaceExportJobKind,
		"targetId":  scope.TargetID,
		"notBefore": now.Add(durableJobReadyDelay).UnixMilli(), "now": now.UnixMilli(),
		"payload": string(payload), "totalItems": totalItems,
		"maxAttempts": defaultWorkspaceExportMaxAttempts,
	}).Execute()
	if err != nil {
		return result, err
	}
	inserted, err := insert.RowsAffected()
	if err != nil || inserted == 1 {
		if err == nil && inserted == 1 {
			err = seedWorkspaceExportScope(app, result.JobID, input.Format, scope, scopeAuthorization)
		}
		return result, err
	}
	existing, err = loadStoredJobByRequest(app, input.ActorID, requestID)
	if err != nil {
		return result, err
	}
	if !sameRequest(existing) {
		result.IdentityReused = true
		return result, nil
	}
	result.JobID, result.Duplicate = existing.ID, true
	return result, nil
}

func (r *durableJobRunner) getWorkspaceExport(event *core.RequestEvent) error {
	user, workspaceID, ok := authorizeWorkspaceExportRequest(event)
	if !ok {
		return nil
	}
	response, err := r.workspaceExportResponse(strings.TrimSpace(event.Request.PathValue("jobId")), user.Id, workspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return apiError(event, http.StatusNotFound, errorCodeWorkspaceExportNotFound, "Export not found.")
	}
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeWorkspaceExportReadFailed, "Couldn't read the export.", err)
	}
	return event.JSON(http.StatusOK, workspaceExportJobResult{Job: response})
}

func (r *durableJobRunner) getCurrentWorkspaceExport(event *core.RequestEvent) error {
	user, workspaceID, ok := authorizeWorkspaceExportRequest(event)
	if !ok {
		return nil
	}
	rows := []readyJob{}
	err := event.App.DB().NewQuery(`
		SELECT [[id]] FROM {{leafen_jobs}}
		WHERE [[owner]] = {:owner} AND [[workspace_id]] = {:workspaceId} AND [[kind]] = {:kind}
			AND [[page_id]] = ''
		ORDER BY [[updated]] DESC, [[id]] DESC LIMIT 2
	`).Bind(dbx.Params{"owner": user.Id, "workspaceId": workspaceID, "kind": workspaceExportJobKind}).All(&rows)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeWorkspaceExportReadFailed, "Couldn't read the export.", err)
	}
	if len(rows) == 0 {
		return event.JSON(http.StatusOK, workspaceExportCurrentJobResult{})
	}
	response, err := r.workspaceExportResponse(rows[0].ID, user.Id, workspaceID)
	if err != nil {
		return apiErrorFrom(event, http.StatusInternalServerError, errorCodeWorkspaceExportReadFailed, "Couldn't read the export.", err)
	}
	return event.JSON(http.StatusOK, workspaceExportCurrentJobResult{
		Job: &response, Finished: r.keptWorkspaceExports(event.App, user.Id, workspaceID, response),
	})
}

func authorizeWorkspaceExportRequest(event *core.RequestEvent) (*core.Record, string, bool) {
	user, ok := authenticatedUser(event)
	if !ok {
		_ = apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
		return nil, "", false
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	if _, err := authorizeWorkspaceToExport(event.App, user.Id, workspaceID, permissionWorkspaceRead); isPermissionDenied(err) {
		_ = apiError(event, http.StatusNotFound, errorCodeWorkspaceNotFound, "Workspace not found.")
		return nil, "", false
	} else if err != nil {
		_ = apiErrorFrom(event, http.StatusInternalServerError, errorCodeWorkspaceExportReadFailed, "Couldn't read the export.", err)
		return nil, "", false
	}
	return user, workspaceID, true
}

// workspaceExportRequestHash is a request's identity: its workspace, request
// ID, format, scope as asked and options, never what the workspace holds —
// the same request sent again after an edit anywhere in it (People's own
// sync among them) is still that request and gets its job back (2026-11-18:
// it was refused as another export). admittedRevision is empty but for the
// jobs admitted before then, which hashed in the scope they resolved to and
// the workspace's revision at their admission (workspaceExportSameRequest).
func workspaceExportRequestHash(workspaceID, requestID, format string, scope workspaceExportScope, options workspaceExportOptions, admittedRevision string) string {
	encodedScope, _ := json.Marshal(scope)
	// Options added after the first release are omitted while false so a
	// replayed request keeps the hash it was admitted with.
	encoded, _ := json.Marshal(struct {
		IncludeFiles      bool `json:"includeFiles"`
		CreateFolders     bool `json:"createFolders"`
		IncludeTranscript bool `json:"includeTranscript"`
		IncludeSubpages   bool `json:"includeSubpages,omitempty"`
		IncludeComments   bool `json:"includeComments,omitempty"`
		// DefaultDatabaseViews shows databases through their first view
		// instead of the one their block shows.
		DefaultDatabaseViews bool `json:"defaultDatabaseViews,omitempty"`
		// LeavePrivatePagesOut keeps a workspace export to its teamspaces.
		LeavePrivatePagesOut bool `json:"leavePrivatePagesOut,omitempty"`
	}(options))
	digest := sha256.Sum256([]byte(workspaceID + "\x00" + requestID + "\x00" + format + "\x00" + string(encodedScope) + "\x00" + string(encoded) + "\x00" + admittedRevision))
	return hex.EncodeToString(digest[:])
}

// workspaceExportSameRequest says whether a stored job is the export a
// request asks for again. One admitted before 2026-11-18 is when its hash,
// taken again with the revision its payload keeps, is its own.
func workspaceExportSameRequest(existing storedJob, requestHash string, admittedHash func(admittedRevision string) string) bool {
	if existing.Kind != workspaceExportJobKind {
		return false
	}
	if existing.RequestHash == requestHash {
		return true
	}
	payload := workspaceExportJobPayload{}
	if json.Unmarshal([]byte(existing.PayloadJSON), &payload) != nil || payload.SourceRevision == "" {
		return false
	}
	return existing.RequestHash == admittedHash(payload.SourceRevision)
}

func workspaceExportSourceRevision(app core.App, authorization workspaceAuthorization) (string, int, error) {
	revision, total, err := workspaceExportRevision(app, authorization.WorkspaceID)
	if err != nil {
		return "", 0, err
	}
	digest := sha256.Sum256([]byte(
		authorization.WorkspaceID + "\x00" + authorization.OwnerID + "\x00" +
			authorization.Workspace.GetString("name") + "\x00" + authorization.Workspace.GetString("updated") + "\x00" +
			jsonNumber64(revision) + "\x00" + jsonNumber(total),
	))
	return hex.EncodeToString(digest[:]), total, nil
}

func jsonNumber(value int) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func jsonNumber64(value int64) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

func (r *durableJobRunner) workspaceExportResponse(id, owner, workspaceID string) (workspaceExportJob, error) {
	job, err := loadStoredJob(r.app, id, owner)
	if err != nil || job.Kind != workspaceExportJobKind || job.WorkspaceID != workspaceID {
		if err == nil {
			err = sql.ErrNoRows
		}
		return workspaceExportJob{}, err
	}
	payload := workspaceExportJobPayload{}
	if err := json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		return workspaceExportJob{}, err
	}
	normalizeStoredWorkspaceExportPayload(&payload)
	artifact, artifactErr := loadWorkspaceExportArtifact(r.app, job.ID, owner, workspaceID)
	if artifactErr != nil && !errors.Is(artifactErr, sql.ErrNoRows) {
		return workspaceExportJob{}, artifactErr
	}
	status := workspaceExportStatusPending
	switch job.Status {
	case durableJobStatusQueued:
		if job.ChunkCount > 0 || payload.Phase != workspaceExportPhaseInventory {
			status = workspaceExportStatusRunning
		}
	case durableJobStatusCompleted:
		status = workspaceExportStatusCompleted
	case durableJobStatusFailed:
		status = workspaceExportStatusFailed
	case durableJobStatusCanceled:
		status = workspaceExportStatusCancelled
	default:
		return workspaceExportJob{}, errors.New(errorCodeWorkspaceExportReadFailed)
	}
	createdAt := time.UnixMilli(job.Created).UTC().Format(time.RFC3339Nano)
	updatedAt := time.UnixMilli(job.Updated).UTC().Format(time.RFC3339Nano)
	var completedAt *string
	if artifactErr == nil {
		value := time.UnixMilli(artifact.Created).UTC().Format(time.RFC3339Nano)
		completedAt = &value
	}
	return workspaceExportJob{
		ID: job.ID, WorkspaceID: job.WorkspaceID, ActorID: job.Owner, RequestID: job.RequestID,
		Format: payload.Format, Scope: payload.Scope, Options: payload.Options, SourceRevision: payload.SourceRevision,
		Status: status, Phase: payload.Phase, Cursor: job.Cursor, PartCount: job.ChunkCount,
		// Its size is its pages and database pages, as its import's card says
		// the bundle's (workspaceImportArchivePages).
		ItemCount: job.Total, PageCount: payload.PagesExported + payload.RowsExported,
		OmittedPageCount: payload.OmittedPages, OmittedFileCount: payload.OmittedFiles,
		ArtifactBytes: artifact.Bytes, ArtifactSHA256: artifact.SHA256,
		ErrorCode: job.ErrorCode, DiagnosticReference: job.DiagnosticReference,
		Attempts: job.Attempt, MaxAttempts: job.MaxAttempts,
		// Running it again changes nothing for a link that leaves the export.
		CanRetry: job.Status == durableJobStatusFailed && job.ErrorCode != errorCodeWorkspaceExportSourceChanged &&
			job.ErrorCode != errorCodeWorkspaceExportLinkOutside,
		CanCancel: job.Status == durableJobStatusQueued,
		CreatedAt: createdAt, UpdatedAt: updatedAt, CompletedAt: completedAt,
	}, nil
}

func loadWorkspaceExportArtifact(app core.App, jobID, owner, workspaceID string) (storedWorkspaceExportArtifact, error) {
	var artifact storedWorkspaceExportArtifact
	err := app.DB().NewQuery(`
		SELECT [[job_id]], [[workspace_id]], [[owner]], [[format]], [[filename]], [[content_type]], [[bytes]], [[sha256]], [[created]]
		FROM {{leafen_workspace_export_artifacts}}
		WHERE [[job_id]] = {:jobId} AND [[owner]] = {:owner} AND [[workspace_id]] = {:workspaceId}
	`).Bind(dbx.Params{"jobId": jobID, "owner": owner, "workspaceId": workspaceID}).One(&artifact)
	return artifact, err
}
