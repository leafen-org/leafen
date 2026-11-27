package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

const (
	notionAdminSpaceExportMaxBodyBytes  = 64 << 10
	notionAdminSpaceExportMaxTeamspaces = 200
	notionAdminSpaceExportMaxTextRunes  = 100
)

var (
	errNotionAdminSpaceExportInvalid     = errors.New("invalid Notion Admin space export request")
	errNotionAdminSpaceExportForbidden   = notionAdminInvalid(errors.New("Notion Admin space export authority is forbidden"), notionAdminScopeChangedReason)
	errNotionAdminSpaceExportNotFound    = errors.New("Notion Admin space export was not found")
	errNotionAdminSpaceExportUnavailable = errors.New("Notion Admin space export is unavailable")
)

type notionAdminSpaceExportRequest struct {
	ExportType               string   `json:"export_type"`
	OnBehalfOfUserEmail      string   `json:"on_behalf_of_user_email"`
	CollectionViewExportType *string  `json:"collection_view_export_type,omitempty"`
	FlattenExportFiletree    *bool    `json:"flatten_export_filetree,omitempty"`
	IncludeComments          *bool    `json:"include_comments,omitempty"`
	IncludeContents          *string  `json:"include_contents,omitempty"`
	Locale                   *string  `json:"locale,omitempty"`
	PDFFormat                *string  `json:"pdf_format,omitempty"`
	TeamspaceIDs             []string `json:"teamspace_ids,omitempty"`
	TimeZone                 *string  `json:"time_zone,omitempty"`
}

type notionAdminSpaceExportJobMetadata struct {
	OrganizationID string                        `json:"organizationId"`
	TokenID        string                        `json:"tokenId"`
	ExportFormat   string                        `json:"exportFormat"`
	Request        notionAdminSpaceExportRequest `json:"request"`
}

type notionAdminSpaceExportEnqueueResponse struct {
	ExportJobID string `json:"export_job_id"`
	Status      string `json:"status"`
}

type notionAdminSpaceExportStatusResponse struct {
	CreatedTime    int64    `json:"created_time"`
	DownloadURLs   []string `json:"download_urls,omitempty"`
	ErrorMessage   string   `json:"error_message,omitempty"`
	ErrorType      string   `json:"error_type,omitempty"`
	ExportFormat   string   `json:"export_format"`
	ExportJobID    string   `json:"export_job_id"`
	ExportType     string   `json:"export_type"`
	LastEditedTime int64    `json:"last_edited_time"`
	PagesExported  int      `json:"pages_exported"`
	Status         string   `json:"status"`
	URLExpiresTime int64    `json:"url_expires_time,omitempty"`
}

func serveNotionAdminSpaceExportEnqueue(
	event *core.RequestEvent,
	jobs *durableJobRunner,
	rawWorkspaceID string,
) error {
	secret := bearerToken(event.Request)
	release := acquireNotionAdminTokenLifecycleLock(notionAdminTokenHash(secret))
	defer release()
	identity, authorized := authorizeNotionAdminAPI(
		event, secret, notionAdminCapabilityWorkspaceExport,
	)
	if !authorized {
		return nil
	}
	workspaceID, request, err := parseNotionAdminSpaceExportRequest(event, rawWorkspaceID)
	if err != nil {
		return notionAdminSpaceExportError(event, err)
	}
	if !identity.CanAccessWorkspace(workspaceID) {
		return notionAdminSpaceExportError(event, errNotionAdminSpaceExportNotFound)
	}
	jobID, err := jobs.admitNotionAdminSpaceExport(
		event.Request.Context(), event.App, secret, identity, workspaceID, request,
	)
	if err != nil {
		return notionAdminSpaceExportError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, notionAdminSpaceExportEnqueueResponse{
		ExportJobID: jobID, Status: "in_progress",
	})
}

func serveNotionAdminSpaceExportStatus(
	event *core.RequestEvent,
	rawWorkspaceID string,
	exportJobID string,
) error {
	secret := bearerToken(event.Request)
	release := acquireNotionAdminTokenLifecycleLock(notionAdminTokenHash(secret))
	defer release()
	identity, authorized := authorizeNotionAdminAPI(
		event, secret, notionAdminCapabilityWorkspaceExport,
	)
	if !authorized {
		return nil
	}
	if len(event.Request.URL.Query()) != 0 || event.Request.ContentLength > 0 ||
		!workspaceExportIDPattern.MatchString(exportJobID) {
		return notionAdminSpaceExportError(event, notionAdminInvalid(errNotionAdminSpaceExportInvalid, "This request takes no query parameters and no body, and the export ID must be one an export gave."))
	}
	workspaceID, err := resolveNotionCompatibleWorkspaceID(strings.TrimSpace(rawWorkspaceID))
	if err != nil {
		return notionAdminSpaceExportError(event, notionAdminInvalid(errNotionAdminSpaceExportInvalid, "The workspace ID should be a UUID."))
	}
	if !identity.CanAccessWorkspace(workspaceID) {
		return notionAdminSpaceExportError(event, errNotionAdminSpaceExportNotFound)
	}
	response, err := notionAdminSpaceExportStatus(
		event.Request.Context(), event, secret, identity, workspaceID, exportJobID,
	)
	if err != nil {
		return notionAdminSpaceExportError(event, err)
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(http.StatusOK, response)
}

func parseNotionAdminSpaceExportRequest(
	event *core.RequestEvent,
	rawWorkspaceID string,
) (string, notionAdminSpaceExportRequest, error) {
	if len(event.Request.URL.Query()) != 0 || event.Request.ContentLength == 0 {
		return "", notionAdminSpaceExportRequest{}, notionAdminInvalid(errNotionAdminSpaceExportInvalid, "Exporting takes a JSON body and no query parameters.")
	}
	contentType, _, err := mime.ParseMediaType(event.Request.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		return "", notionAdminSpaceExportRequest{}, notionAdminInvalid(errNotionAdminSpaceExportInvalid, "The body must be application/json.")
	}
	workspaceID, err := resolveNotionCompatibleWorkspaceID(strings.TrimSpace(rawWorkspaceID))
	if err != nil {
		return "", notionAdminSpaceExportRequest{}, notionAdminInvalid(errNotionAdminSpaceExportInvalid, "The workspace ID should be a UUID.")
	}
	event.Request.Body = http.MaxBytesReader(
		event.Response, event.Request.Body, notionAdminSpaceExportMaxBodyBytes,
	)
	decoder := json.NewDecoder(event.Request.Body)
	decoder.DisallowUnknownFields()
	request := notionAdminSpaceExportRequest{}
	if decoder.Decode(&request) != nil {
		return "", notionAdminSpaceExportRequest{}, notionAdminInvalid(errNotionAdminSpaceExportInvalid, "The body isn't an export request: check its fields.")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return "", notionAdminSpaceExportRequest{}, notionAdminInvalid(errNotionAdminSpaceExportInvalid, "The body holds more than one JSON value.")
	}
	normalized, err := normalizeNotionAdminSpaceExportRequest(request)
	return workspaceID, normalized, err
}

func normalizeNotionAdminSpaceExportRequest(
	request notionAdminSpaceExportRequest,
) (notionAdminSpaceExportRequest, error) {
	if request.ExportType != "html" && request.ExportType != "markdown" && request.ExportType != "pdf" {
		return notionAdminSpaceExportRequest{}, notionAdminInvalid(errNotionAdminSpaceExportInvalid, "export_type takes html, markdown or pdf.")
	}
	email, valid := normalizeEmailIdentity(request.OnBehalfOfUserEmail)
	if !valid {
		return notionAdminSpaceExportRequest{}, notionAdminInvalid(errNotionAdminSpaceExportInvalid, "on_behalf_of_user_email isn't an email address.")
	}
	request.OnBehalfOfUserEmail = email
	if !validNotionAdminSpaceExportOptionalText(request.CollectionViewExportType, "all", "currentView") ||
		!validNotionAdminSpaceExportOptionalText(request.IncludeContents, "everything", "no_files") ||
		!validNotionAdminSpaceExportOptionalText(request.PDFFormat, "A3", "A4", "Legal", "Letter", "Tabloid") ||
		!validNotionAdminSpaceExportText(request.Locale) || !validNotionAdminSpaceExportText(request.TimeZone) ||
		len(request.TeamspaceIDs) > notionAdminSpaceExportMaxTeamspaces {
		return notionAdminSpaceExportRequest{}, notionAdminInvalid(errNotionAdminSpaceExportInvalid, fmt.Sprintf("include_contents takes everything or no_files, pdf_format A3, A4, Legal, Letter or Tabloid; locale and time_zone are short text; at most %d teamspace_ids.", notionAdminSpaceExportMaxTeamspaces))
	}
	seen := map[string]struct{}{}
	teamspaces := make([]string, 0, len(request.TeamspaceIDs))
	for _, raw := range request.TeamspaceIDs {
		value := strings.TrimSpace(raw)
		if value == "" || value != raw || len([]rune(value)) > 200 {
			return notionAdminSpaceExportRequest{}, notionAdminInvalid(errNotionAdminSpaceExportInvalid, "Each teamspace ID must be non-empty, without spaces around it, at most 200 characters.")
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		teamspaces = append(teamspaces, value)
	}
	sort.Strings(teamspaces)
	request.TeamspaceIDs = teamspaces
	return request, nil
}

func validNotionAdminSpaceExportOptionalText(value *string, allowed ...string) bool {
	if value == nil {
		return true
	}
	for _, candidate := range allowed {
		if *value == candidate {
			return true
		}
	}
	return false
}

func validNotionAdminSpaceExportText(value *string) bool {
	return value == nil || *value != "" && strings.TrimSpace(*value) == *value &&
		len([]rune(*value)) <= notionAdminSpaceExportMaxTextRunes
}

// admitNotionAdminSpaceExport writes through app, the request's
// (request_write_stop.go).
func (r *durableJobRunner) admitNotionAdminSpaceExport(
	ctx context.Context,
	app core.App,
	secret string,
	expected notionAdminIdentity,
	workspaceID string,
	request notionAdminSpaceExportRequest,
) (string, error) {
	jobID := ""
	duplicate := false
	err := app.RunInTransaction(func(txApp core.App) error {
		current, err := authenticateNotionAdminToken(ctx, txApp, secret, time.Now().UTC())
		if err != nil {
			return err
		}
		if current.OrganizationID != expected.OrganizationID || current.TokenID != expected.TokenID ||
			current.CreatedBy != expected.CreatedBy || !current.HasCapability(notionAdminCapabilityWorkspaceExport) ||
			!current.CanAccessWorkspace(workspaceID) {
			return errNotionAdminSpaceExportForbidden
		}
		actorID, err := resolveNotionAdminManagedUser(ctx, txApp, current.OrganizationID,
			normalizedNotionAdminManagedUserSelector{Email: request.OnBehalfOfUserEmail})
		if err != nil {
			if errors.Is(err, errNotionAdminManagedUserSessionNotFound) {
				return errNotionAdminSpaceExportNotFound
			}
			return errors.Join(errNotionAdminSpaceExportUnavailable, err)
		}
		includeFiles := request.IncludeContents == nil || *request.IncludeContents != "no_files"
		createFolders := request.FlattenExportFiletree == nil || !*request.FlattenExportFiletree
		metadata := &notionAdminSpaceExportJobMetadata{
			OrganizationID: current.OrganizationID, TokenID: current.TokenID,
			ExportFormat: request.ExportType, Request: request,
		}
		encoded, err := json.Marshal(metadata)
		if err != nil {
			return err
		}
		result, err := admitWorkspaceExportInTransaction(txApp, workspaceExportAdmissionInput{
			ActorID: actorID, WorkspaceID: workspaceID,
			Format:      workspaceExportFormatMarkdownCSV,
			Scope:       workspaceExportScope{Kind: workspaceExportScopeWorkspace},
			Options:     workspaceExportOptions{IncludeFiles: includeFiles, CreateFolders: createFolders},
			NotionAdmin: metadata,
			RequestID: func(sourceRevision string) string {
				digest := sha256.Sum256([]byte(current.TokenID + "\x00" + workspaceID + "\x00" +
					actorID + "\x00" + string(encoded) + "\x00" + sourceRevision))
				return "notion-admin-export-" + hex.EncodeToString(digest[:])
			},
		})
		if err != nil {
			return err
		}
		if result.IdentityReused || result.Authorization.Organization.OrganizationID != current.OrganizationID {
			return notionAdminInvalid(errNotionAdminSpaceExportForbidden, "The workspace isn't in this token's organization, or this request ID was used for another export.")
		}
		jobID, duplicate = result.JobID, result.Duplicate
		if duplicate {
			return nil
		}
		return recordOrganizationAuditEvent(txApp, organizationAuditEvent{
			ID: core.GenerateDefaultRandomId(), OrganizationID: current.OrganizationID,
			ActorID: current.CreatedBy, Action: "notion_admin.workspace_export.enqueue",
			TargetType: "workspace_export", TargetID: jobID,
			Metadata: map[string]any{
				"admin_token_id": current.TokenID, "workspace_id": workspaceID,
				"requesting_user_id": actorID, "export_type": request.ExportType,
			},
		})
	})
	if isPermissionDenied(err) {
		return "", errNotionAdminSpaceExportNotFound
	}
	if errors.Is(err, errDLPPolicyBlocked) {
		return "", notionAdminInvalid(errNotionAdminSpaceExportForbidden, dlpPolicyBlockedReason)
	}
	if isDLPPolicyDecisionError(err) {
		return "", err
	}
	if err != nil {
		return "", err
	}
	r.notify()
	return jobID, nil
}

func notionAdminSpaceExportStatus(
	ctx context.Context,
	event *core.RequestEvent,
	secret string,
	identity notionAdminIdentity,
	workspaceID string,
	exportJobID string,
) (notionAdminSpaceExportStatusResponse, error) {
	job := storedJob{}
	payload := workspaceExportJobPayload{}
	err := runReadSnapshot(event.Request.Context(), event.App, func(txApp core.App) error {
		current, err := loadStoredJob(txApp, exportJobID, "")
		if errors.Is(err, sql.ErrNoRows) {
			return errNotionAdminSpaceExportNotFound
		}
		if err != nil {
			return errors.Join(errNotionAdminSpaceExportUnavailable, err)
		}
		currentPayload := workspaceExportJobPayload{}
		if json.Unmarshal([]byte(current.PayloadJSON), &currentPayload) != nil ||
			current.Kind != workspaceExportJobKind || current.WorkspaceID != workspaceID ||
			currentPayload.NotionAdmin == nil ||
			currentPayload.NotionAdmin.OrganizationID != identity.OrganizationID ||
			currentPayload.NotionAdmin.TokenID != identity.TokenID {
			return errNotionAdminSpaceExportNotFound
		}
		workspace, err := txApp.FindRecordById(workspacesCollection, workspaceID)
		if err != nil || workspace.GetString("organization") != identity.OrganizationID {
			return refusedUnlessStorage(errNotionAdminSpaceExportNotFound, err)
		}
		job, payload = current, currentPayload
		return nil
	})
	if err != nil {
		return notionAdminSpaceExportStatusResponse{}, err
	}
	if err := ctx.Err(); err != nil {
		return notionAdminSpaceExportStatusResponse{}, errors.Join(errNotionAdminSpaceExportUnavailable, err)
	}
	response := notionAdminSpaceExportStatusResponse{
		CreatedTime: job.Created, ExportFormat: payload.NotionAdmin.ExportFormat,
		ExportJobID: job.ID, ExportType: "space", LastEditedTime: job.Updated,
		PagesExported: payload.PagesExported, Status: "in_progress",
	}
	switch job.Status {
	case durableJobStatusQueued:
	case durableJobStatusCompleted:
		response.Status = "completed"
		downloadURL, expiresAt, err := notionAdminSpaceExportDownloadURL(
			event, secret, identity, workspaceID, job.ID,
		)
		if err != nil {
			response.Status = "url_generation_failed"
			response.ErrorType = notionAdminErrorInternal
			response.ErrorMessage = notionAdminInternalErrorMessage
			break
		}
		response.DownloadURLs = []string{downloadURL}
		response.URLExpiresTime = expiresAt
	case durableJobStatusFailed, durableJobStatusCanceled:
		response.Status = "failed"
		response.ErrorType = job.ErrorCode
		if response.ErrorType == "" {
			response.ErrorType = errorCodeWorkspaceExportCreateFailed
		}
		response.ErrorMessage = "The workspace export failed."
	default:
		return notionAdminSpaceExportStatusResponse{}, errNotionAdminSpaceExportUnavailable
	}
	return response, nil
}

func serveNotionAdminSpaceExportArtifact(
	event *core.RequestEvent,
	artifact storedWorkspaceExportArtifact,
) error {
	file, err := openWorkspaceExportArtifactFile(event.App, artifact)
	if err != nil {
		return notionAdminSpaceExportError(event, errNotionAdminSpaceExportNotFound)
	}
	defer file.Close()
	serveWorkspaceExportArtifact(event, artifact, file)
	return nil
}

func notionAdminSpaceExportError(event *core.RequestEvent, err error) error {
	switch {
	case errors.Is(err, errNotionAdminSpaceExportInvalid):
		return notionAdminAPIError(event, http.StatusBadRequest, notionAdminErrorValidation, notionAdminValidationReason(err), err)
	case errors.Is(err, errNotionAdminTokenUnauthorized):
		return notionAdminAPIError(event, http.StatusUnauthorized, notionAdminErrorUnauthorized, notionAdminUnauthorizedMessage, err)
	case errors.Is(err, errNotionAdminSpaceExportForbidden):
		return notionAdminAPIError(event, http.StatusForbidden, notionAdminErrorForbidden, notionAdminRefusalReason(err, notionAdminForbiddenMessage), err)
	case errors.Is(err, errNotionAdminSpaceExportNotFound):
		return notionAdminAPIError(event, http.StatusNotFound, notionAdminErrorNotFound, notionAdminNotFoundMessage, err)
	case errors.Is(err, errDLPPolicyUnavailable):
		return notionAdminAPIError(event, http.StatusServiceUnavailable, "service_unavailable", dlpPolicyUnavailableReason, err)
	default:
		return notionAdminAPIError(event, http.StatusInternalServerError, notionAdminErrorInternal, notionAdminInternalErrorMessage, err)
	}
}
