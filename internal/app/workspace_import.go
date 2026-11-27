package app

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/apis"
	"github.com/pocketbase/pocketbase/core"
)

type workspaceImportJobPayload struct {
	SchemaVersion     int      `json:"schemaVersion"`
	Format            string   `json:"format"`
	Title             string   `json:"title"`
	Phase             string   `json:"phase"`
	RootPageID        string   `json:"rootPageId"`
	RootPageIDs       []string `json:"rootPageIds"`
	ParentPageID      string   `json:"parentPageId,omitempty"`
	TeamspaceID       string   `json:"teamspaceId,omitempty"`
	ChunkIndex        int      `json:"chunkIndex"`
	ItemOffset        int      `json:"itemOffset"`
	ReadyToPublish    bool     `json:"readyToPublish"`
	SourceVerified    bool     `json:"sourceVerified"`
	CancelRequested   bool     `json:"cancelRequested"`
	CleanupLane       int      `json:"cleanupLane"`
	TerminalErrorCode string   `json:"terminalErrorCode"`
	// TerminalErrorDetail says where and what a refused import found wrong.
	TerminalErrorDetail      string `json:"terminalErrorDetail,omitempty"`
	ArchiveFileIndex         int    `json:"archiveFileIndex"`
	ArchiveFileCursor        int64  `json:"archiveFileCursor"`
	ArchiveFileChain         string `json:"archiveFileChain"`
	ArchiveFilesReady        bool   `json:"archiveFilesReady"`
	ArchiveCleanupFileIndex  int    `json:"archiveCleanupFileIndex"`
	ArchiveCleanupFileCursor int64  `json:"archiveCleanupFileCursor"`
	ArchiveCleanupFileChain  string `json:"archiveCleanupFileChain"`
	ArchivePrepareUpload     bool   `json:"archivePrepareUpload"`
	ArchivePreparePart       int    `json:"archivePreparePart"`
	ArchivePrepareBytes      int64  `json:"archivePrepareBytes"`
	ArchivePrepareHashState  string `json:"archivePrepareHashState"`
	AppendToExistingPage     bool   `json:"appendToExistingPage"`
	AppendCleanupCursor      string `json:"appendCleanupCursor"`
	ReplaceExistingPage      bool   `json:"replaceExistingPage"`
	ReplaceSnapshotCursor    string `json:"replaceSnapshotCursor"`
	ReplaceCleanupCursor     string `json:"replaceCleanupCursor"`
	ReplaceRestoreCursor     string `json:"replaceRestoreCursor"`
	ReplaceSnapshotItems     int    `json:"replaceSnapshotItems"`
	ReplaceCleanupStage      string `json:"replaceCleanupStage"`
	// ArchiveKindsUnknown marks an archive from before page kinds were kept.
	ArchiveKindsUnknown bool `json:"archiveKindsUnknown,omitzero"`
	// ArchivePages is the pages and database pages the bundle brings, said as
	// its size on the import's card (workspaceImportArchivePages).
	ArchivePages int `json:"archivePages,omitzero"`
	// People are a moved workspace's people and the accounts here they were
	// paired with (workspace_import_archive_people.go).
	People []workspaceImportPerson `json:"people,omitempty"`
	// KeepIDs is a workspace moved from another Leafen server: its import
	// keeps the IDs the bundle gives, which its pages' history names
	// (workspace_move_history.go).
	KeepIDs bool `json:"keepIds,omitzero"`
	// History is the moved workspace's pages' history, put in place of what
	// the import wrote once its pages are in.
	History *workspaceMoveHistorySource `json:"history,omitempty"`
	// MoveAccess is who could open what on the old server (workspace_move_access.go).
	MoveAccess *workspaceMoveAccess `json:"moveAccess,omitempty"`
	// MergeDataSourceID is the database a CSV's rows join: MergeRows of them and MergeNewPropertyIDs.
	MergeDataSourceID   string   `json:"mergeDataSourceId,omitempty"`
	MergeRows           int      `json:"mergeRows,omitzero"`
	MergeNewPropertyIDs []string `json:"mergeNewPropertyIds,omitempty"`
	// PageIDs marks a job admitted once the rows it names from its own ID
	// took page IDs (page_ids.go): a CSV merge's rows
	// (workspaceCSVMergeJobRowID) and a ZIP batch's attachments database's
	// (addWorkspaceImportZipLogParts). One admitted before names them as it
	// staged or began them. A migration connection's bundle has it when the
	// connection's source was made since (workspaceImportArchiveIDSalt).
	PageIDs bool `json:"pageIds,omitzero"`
	// BundleConnectionID is the migration connection whose bundle this
	// archive is: its IDs are made from the connection, not the job.
	BundleConnectionID string `json:"bundleConnectionId,omitempty"`
	// Locale is what a batch's CSV files are read in (workspace_import_batch.go).
	Locale string `json:"locale,omitempty"`
	// BatchBusyTurns is how many turns a batch has waited for a busy disk or
	// database in a row.
	BatchBusyTurns int `json:"batchBusyTurns,omitzero"`
	// HeldSince and HeldSeen are when a batch began waiting for a place an
	// operation holds and last found it held (workspace_import_held.go).
	HeldSince int64 `json:"heldSince,omitzero"`
	HeldSeen  int64 `json:"heldSeen,omitzero"`
	// BatchJobID is the batch a document's import belongs to, which reports it.
	BatchJobID string `json:"batchJobId,omitempty"`
	// ReservedRootPageID is the ID its batch gave this import's new page.
	ReservedRootPageID string `json:"reservedRootPageId,omitempty"`
	// FillsPlace is an import that fills the empty page it was asked to go
	// under, which had PlaceTitle then (workspace_import_empty_place.go).
	FillsPlace bool   `json:"fillsPlace,omitzero"`
	PlaceTitle string `json:"placeTitle,omitempty"`
	// FillEmptyPage is a batch asked to fill the empty page it goes under
	// with its one file (fillEmptyPage, workspace_import_empty_place.go).
	FillEmptyPage bool `json:"fillEmptyPage,omitzero"`
	// DataSourceID is the database whose row the import's page becomes, or,
	// for a batch, its top pages (workspace_import_database_place.go).
	DataSourceID string `json:"dataSourceId,omitempty"`
	// InlineDatabases are the databases the import's page holds inline, left
	// to turn once its text is in (workspace_import_inline_databases.go).
	InlineDatabases []string `json:"inlineDatabases,omitempty"`
}

type workspaceImportAdmission struct {
	Input          workspaceImportInput
	TargetPageID   string
	Append         bool
	Replace        bool
	TargetRevision int
	TargetCursor   int
	// MergeDataSourceID is the database a CSV's rows join, its columns filling
	// the properties MergeColumns names, MergeNewProperties among them.
	MergeDataSourceID  string
	MergeColumns       []string
	MergeNewProperties []workspaceCSVMergeNewProperty
	// BatchJobID is the batch this document belongs to, and
	// ReservedRootPageID the ID the batch gave its new page.
	BatchJobID         string
	ReservedRootPageID string
	// ReservedRows are the pages an export brought as this CSV's rows
	// (workspace_import_csv.go).
	ReservedRows []workspaceImportReservedRow
	// FillsPlace adds to the empty page the import was asked to go under,
	// its target, and names it after the file (workspace_import_empty_place.go).
	FillsPlace bool
}

// workspaceImportReservedRow is an exported database's row page, by its
// title, with the ID its batch gave it.
type workspaceImportReservedRow struct {
	Title  string `db:"title"`
	PageID string `db:"page_id"`
}

func workspaceImportPrimaryRootPageID(payload workspaceImportJobPayload) string {
	if len(payload.RootPageIDs) > 0 {
		return payload.RootPageIDs[0]
	}
	return payload.RootPageID
}

func registerWorkspaceImportRoutes(event *core.ServeEvent, jobs *durableJobRunner) {
	event.Router.POST(
		apiRoot+workspaceImportCreatePath,
		jobs.createWorkspaceImport,
	).Bind(apis.BodyLimit(workspaceImportRequestBytes))
	event.Router.PUT(
		apiRoot+workspaceMarkdownReplaceCreatePath,
		jobs.createWorkspaceMarkdownReplace,
	).Bind(apis.BodyLimit(workspaceImportRequestBytes))
	event.Router.POST(
		apiRoot+workspaceMarkdownAppendCreatePath,
		jobs.createWorkspaceMarkdownAppend,
	).Bind(apis.BodyLimit(workspaceImportRequestBytes))
	registerWorkspaceCSVMergeRoutes(event, jobs)
	event.Router.POST(apiRoot+workspaceImportArchiveCreatePath, jobs.createWorkspaceArchiveImport)
	registerWorkspaceMoveRoutes(event, jobs)
	event.Router.POST(
		apiRoot+workspaceArchiveUploadPreparePath,
		jobs.prepareWorkspaceArchiveImportUpload,
	).Bind(apis.BodyLimit(fileUploadJSONBodyBytes))
	event.Router.PUT(
		apiRoot+workspaceArchiveUploadPartPath,
		jobs.putWorkspaceArchiveImportUploadPart,
	).Bind(apis.BodyLimit(fileTransferPartBytes + fileUploadJSONBodyBytes))
	event.Router.POST(
		apiRoot+workspaceArchiveUploadCompletePath,
		jobs.completeWorkspaceArchiveImportUpload,
	).Bind(apis.BodyLimit(fileUploadJSONBodyBytes))
	event.Router.POST(
		apiRoot+workspaceArchiveUploadCancelPath,
		jobs.cancelWorkspaceArchiveImportUpload,
	).Bind(apis.BodyLimit(fileUploadJSONBodyBytes))
	registerWorkspaceImportBatchRoutes(event, jobs)
	event.Router.GET(apiRoot+workspaceImportListPath, jobs.listWorkspaceImports)
	event.Router.GET(apiRoot+workspaceImportReadPath, jobs.getWorkspaceImport)
	event.Router.POST(apiRoot+workspaceImportCancelPath, jobs.cancelWorkspaceImport)
}

func (r *durableJobRunner) createWorkspaceImport(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	workspaceID := strings.TrimSpace(event.Request.PathValue("workspaceId"))
	_, err := authorizeWorkspace(event.App, user.Id, workspaceID, permissionWorkspaceWrite)
	if isPermissionDenied(err) {
		return apiError(event, http.StatusForbidden, errorCodePermissionDenied, "You don't have permission to import.")
	}
	if err != nil {
		return workspaceImportAPIError(event, err, "authorize")
	}
	input := workspaceImportInput{}
	if err := event.BindBody(&input); err != nil {
		return workspaceImportBodyRefused(event, "import request", err)
	}
	input.RequestID = strings.TrimSpace(input.RequestID)
	input.Format = strings.TrimSpace(input.Format)
	input.Locale = strings.TrimSpace(input.Locale)
	input.Title = strings.TrimSpace(input.Title)
	input.ParentPageID = strings.TrimSpace(input.ParentPageID)
	input.TeamspaceID = strings.TrimSpace(input.TeamspaceID)
	input.DataSourceID = strings.TrimSpace(input.DataSourceID)
	trimWorkspaceImportCSVColumnTypes(input.CSVColumnTypes)
	if problem := firstNonempty(durableRequestIDProblem(input.RequestID), workspaceImportInputProblem(input)); problem != "" {
		return workspaceImportRefused(event, problem)
	}
	if err := requireDataDiskRoom(event.App, 0); err != nil {
		return writeServerDiskLow(event, "workspace-import", err)
	}
	admission := workspaceImportAdmission{Input: input}
	if input.Format == workspaceImportFormatMarkdown && input.FillEmptyPage {
		empty, err := workspaceImportPlaceIsEmpty(event.App, input.ParentPageID)
		if err != nil {
			return workspaceImportAPIError(event, err, "place")
		}
		if empty {
			admission.TargetPageID, admission.Append, admission.FillsPlace = input.ParentPageID, true, true
		}
	}
	return r.admitWorkspaceImport(event, user.Id, workspaceID, admission)
}

// authorizeWorkspaceImportAdmission authorizes an import into the workspace,
// or into its target page: an append adds to the page's body, as a database
// page's body takes it.
func authorizeWorkspaceImportAdmission(
	app core.App,
	ownerID string,
	workspaceID string,
	targetPageID string,
	appendBody bool,
) (workspaceAuthorization, error) {
	if targetPageID == "" {
		return authorizeWorkspace(app, ownerID, workspaceID, permissionWorkspaceWrite)
	}
	authorize := authorizePage
	if appendBody {
		authorize = authorizePageBody
	}
	authorization, err := authorize(app, ownerID, targetPageID, permissionWorkspaceWrite)
	if isPermissionDenied(err) {
		// Held for a while, it is refused as held, not as a page the importer may not change.
		if busy := pageHeldForMember(app, ownerID, targetPageID); busy != "" {
			return workspaceAuthorization{}, workspaceImportParentHeld(busy)
		}
	}
	if err != nil || authorization.Workspace.WorkspaceID != workspaceID {
		if err == nil {
			err = permissionDenied()
		}
		return workspaceAuthorization{}, err
	}
	return authorization.Workspace, nil
}

// workspaceImportRequestsKeptFor is how long a finished import past the
// history's last ones still answers its request: picked again after an
// interruption, a batch goes on where it stopped (2026-11-03: a batch of 150
// files, resumed while an import of 600 ran beside it, put in again the 149
// whose records the history had let go). The browser keeps a batch's
// requests as long (DirectFileBatchImport).
const workspaceImportRequestsKeptFor = 30 * 24 * time.Hour

func cleanupWorkspaceImportTerminalHistory(app core.App, workspaceID string) error {
	_, err := app.DB().NewQuery(`
		DELETE FROM {{leafen_jobs}} WHERE [[id]] IN (
			SELECT [[id]] FROM (
				SELECT [[id]], [[updated]] FROM {{leafen_jobs}}
				WHERE [[kind]] = {:kind} AND [[workspace_id]] = {:workspaceId}
					AND [[status]] IN ('completed', 'failed', 'canceled')
				ORDER BY [[updated]] DESC, [[id]] DESC
				LIMIT -1 OFFSET {:retain}
			) WHERE [[updated]] < {:before}
			LIMIT {:limit}
		)
	`).Bind(dbx.Params{
		"kind": workspaceImportJobKind, "workspaceId": workspaceID,
		"limit": workspaceImportCleanupTurnItems, "retain": workspaceImportTerminalRetentionPerWorkspace,
		"before": time.Now().Add(-workspaceImportRequestsKeptFor).UnixMilli(),
	}).Execute()
	return err
}

func workspaceImportRequestHash(workspaceID string, input workspaceImportInput) string {
	parts := []string{workspaceID, input.Format, input.Title, input.Markdown}
	if input.Format != workspaceImportFormatMarkdown || input.Locale != "" || input.CSV != "" {
		parts = append(parts, input.Locale, input.CSV)
	}
	if input.ParentPageID != "" {
		parts = append(parts, "parent", input.ParentPageID)
	}
	if input.FillEmptyPage {
		parts = append(parts, "fill")
	}
	if input.TeamspaceID != "" {
		parts = append(parts, "teamspace", input.TeamspaceID)
	}
	if input.DataSourceID != "" {
		parts = append(parts, "data-source", input.DataSourceID)
	}
	parts = append(parts, workspaceImportCSVHashParts(input)...)
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(digest[:])
}

// parseWorkspaceImportInput reads an import's text; a CSV's data source
// takes dataSourceID when it is given, else one of its own.
func parseWorkspaceImportInput(
	input workspaceImportInput, reserved []workspaceImportReservedRow, dataSourceID string,
) (workspaceImportItemSource, error) {
	if input.Format == workspaceImportFormatMarkdown {
		return workspaceImportTitledMarkdownSource(input.Markdown, input.Title, input.Locale), nil
	}
	locale := input.Locale
	if locale == "" {
		locale = workspaceImportLocaleEnglish
	}
	return workspaceImportHeadedCSV(input.CSV, func(value string) (workspaceImportItemSource, error) {
		return parseWorkspaceImportCSV(value, input.Title, locale, input.CSVColumnTypes, input.CSVKeepFirstRow, reserved, dataSourceID)
	})
}

func workspaceImportDLPContent(input workspaceImportInput) map[string]any {
	return map[string]any{
		"title": input.Title, "format": input.Format,
		"markdown": input.Markdown, "csv": input.CSV,
	}
}

func workspaceImportAPIError(event *core.RequestEvent, err error, stage string) error {
	code := stableWorkspaceImportFailureCode(err)
	status := http.StatusInternalServerError
	message := "Couldn't process the import."
	if code == errorCodeWorkspaceImportInvalid {
		status, message = http.StatusBadRequest, refusalMessage("Check the import details.", workspaceImportFailureDetail(err))
	} else if code == errorCodeWorkspaceImportTargetNotFresh {
		status, message = http.StatusConflict, "The page changed, so the import can't continue."
	} else if words, held := workspaceImportParentHeldWords(err); held {
		// The page to import into is held for a while (workspace_import_parent.go).
		status, message = http.StatusConflict, words
	} else if code == errorCodeWorkspaceImportDatabaseBusy || code == errorCodeWorkspaceImportStorageUnavailable {
		status, message = http.StatusServiceUnavailable, "The import store is unavailable."
	} else if refusal, gone := workspaceImportTargetGoneRefusal(err); gone {
		// The page to import into is in the trash or deleted for good
		// (workspace_import_target_gone.go).
		status, message = refusal.status, refusal.message
	}
	return apiErrorWithCause(event, status, code, message, "workspace-import", stage, code, err)
}
