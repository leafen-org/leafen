package app

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

const (
	notionAdminSpaceExportDownloadVersion   = 1
	notionAdminSpaceExportDownloadPrefix    = "hnased1"
	notionAdminSpaceExportDownloadDomain    = "leafen:notion-admin-space-export-download:v1"
	notionAdminSpaceExportDownloadParameter = "download_token"
	notionAdminSpaceExportDownloadMaxLength = 4096
	notionAdminSpaceExportDownloadLifetime  = 5 * time.Minute
)

type notionAdminSpaceExportDownloadClaim struct {
	Version        int    `json:"v"`
	OrganizationID string `json:"o"`
	TokenID        string `json:"t"`
	WorkspaceID    string `json:"w"`
	ExportJobID    string `json:"j"`
	ExpiresAt      int64  `json:"e"`
}

func notionAdminSpaceExportDownloadURL(
	event *core.RequestEvent,
	secret string,
	identity notionAdminIdentity,
	workspaceID string,
	exportJobID string,
) (string, int64, error) {
	expiresAt := time.Now().UTC().Add(notionAdminSpaceExportDownloadLifetime).UnixMilli()
	claim := notionAdminSpaceExportDownloadClaim{
		Version:        notionAdminSpaceExportDownloadVersion,
		OrganizationID: identity.OrganizationID, TokenID: identity.TokenID,
		WorkspaceID: workspaceID, ExportJobID: exportJobID, ExpiresAt: expiresAt,
	}
	payload, err := json.Marshal(claim)
	if err != nil {
		return "", 0, err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	signature := notionAdminSpaceExportDownloadSignature(secret, encoded)
	token := notionAdminSpaceExportDownloadPrefix + "." + encoded + "." +
		base64.RawURLEncoding.EncodeToString(signature)
	if len(token) > notionAdminSpaceExportDownloadMaxLength {
		return "", 0, errNotionAdminSpaceExportUnavailable
	}
	publicWorkspaceID, err := notionCompatibleWorkspaceID(workspaceID)
	if err != nil {
		return "", 0, err
	}
	location := url.URL{
		Scheme: requestScheme(event.Request), Host: event.Request.Host,
		Path: notionAdminAPISpacesPath + "/" + publicWorkspaceID +
			"/exports/" + exportJobID + "/download",
		RawQuery: url.Values{notionAdminSpaceExportDownloadParameter: []string{token}}.Encode(),
	}
	return location.String(), expiresAt, nil
}

func serveNotionAdminSpaceExportDownload(
	event *core.RequestEvent,
	rawWorkspaceID string,
	exportJobID string,
) error {
	if event.Request.ContentLength > 0 || !workspaceExportIDPattern.MatchString(exportJobID) {
		return notionAdminSpaceExportError(event, notionAdminInvalid(errNotionAdminSpaceExportInvalid, "Downloading takes no body, and the export ID must be one an export gave."))
	}
	query := event.Request.URL.Query()
	values, present := query[notionAdminSpaceExportDownloadParameter]
	if len(query) != 1 || !present || len(values) != 1 {
		return notionAdminSpaceExportError(event, notionAdminInvalid(errNotionAdminSpaceExportInvalid, "The query takes the download parameter once and nothing else."))
	}
	workspaceID, err := resolveNotionCompatibleWorkspaceID(strings.TrimSpace(rawWorkspaceID))
	if err != nil {
		return notionAdminSpaceExportError(event, notionAdminInvalid(errNotionAdminSpaceExportInvalid, "The workspace ID should be a UUID."))
	}
	claim, secret, err := decodeNotionAdminSpaceExportDownloadClaim(
		event.App, values[0], workspaceID, exportJobID, time.Now().UTC(),
	)
	if err != nil {
		return notionAdminSpaceExportError(event, errNotionAdminTokenUnauthorized)
	}
	artifact, err := authorizeNotionAdminSpaceExportDownload(
		event, secret, claim, workspaceID, exportJobID,
	)
	if err != nil {
		return notionAdminSpaceExportError(event, err)
	}
	return serveNotionAdminSpaceExportArtifact(event, artifact)
}

func authorizeNotionAdminSpaceExportDownload(
	event *core.RequestEvent,
	secret string,
	claim notionAdminSpaceExportDownloadClaim,
	workspaceID string,
	exportJobID string,
) (storedWorkspaceExportArtifact, error) {
	release := acquireNotionAdminTokenLifecycleLock(notionAdminTokenHash(secret))
	defer release()
	identity, err := authenticateNotionAdminToken(
		event.Request.Context(), event.App, secret, time.Now().UTC(),
	)
	if err != nil || identity.OrganizationID != claim.OrganizationID ||
		identity.TokenID != claim.TokenID || !identity.HasCapability(notionAdminCapabilityWorkspaceExport) ||
		!identity.CanAccessWorkspace(workspaceID) {
		return storedWorkspaceExportArtifact{}, refusedUnlessStorage(errNotionAdminTokenUnauthorized, err)
	}
	artifact := storedWorkspaceExportArtifact{}
	err = runReadSnapshot(event.Request.Context(), event.App, func(txApp core.App) error {
		job, err := loadStoredJob(txApp, exportJobID, "")
		if err != nil || job.Kind != workspaceExportJobKind ||
			job.Status != durableJobStatusCompleted || job.WorkspaceID != workspaceID {
			return refusedUnlessStorage(errNotionAdminSpaceExportNotFound, err)
		}
		payload := workspaceExportJobPayload{}
		if json.Unmarshal([]byte(job.PayloadJSON), &payload) != nil || payload.NotionAdmin == nil ||
			payload.NotionAdmin.OrganizationID != identity.OrganizationID ||
			payload.NotionAdmin.TokenID != identity.TokenID {
			return errNotionAdminSpaceExportNotFound
		}
		workspace, err := txApp.FindRecordById(workspacesCollection, workspaceID)
		if err != nil || workspace.GetString("organization") != identity.OrganizationID {
			return refusedUnlessStorage(errNotionAdminSpaceExportNotFound, err)
		}
		artifact, err = loadWorkspaceExportArtifact(txApp, job.ID, job.Owner, workspaceID)
		if err != nil {
			return refusedUnlessStorage(errNotionAdminSpaceExportNotFound, err)
		}
		return nil
	})
	return artifact, err
}

func decodeNotionAdminSpaceExportDownloadClaim(
	app core.App,
	token string,
	workspaceID string,
	exportJobID string,
	now time.Time,
) (notionAdminSpaceExportDownloadClaim, string, error) {
	if len(token) == 0 || len(token) > notionAdminSpaceExportDownloadMaxLength {
		return notionAdminSpaceExportDownloadClaim{}, "", errNotionAdminTokenUnauthorized
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != notionAdminSpaceExportDownloadPrefix {
		return notionAdminSpaceExportDownloadClaim{}, "", errNotionAdminTokenUnauthorized
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return notionAdminSpaceExportDownloadClaim{}, "", errNotionAdminTokenUnauthorized
	}
	claim := notionAdminSpaceExportDownloadClaim{}
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&claim) != nil || claim.Version != notionAdminSpaceExportDownloadVersion ||
		claim.OrganizationID == "" || len(claim.TokenID) != 15 ||
		claim.WorkspaceID != workspaceID || claim.ExportJobID != exportJobID ||
		claim.ExpiresAt <= now.UnixMilli() {
		return notionAdminSpaceExportDownloadClaim{}, "", errNotionAdminTokenUnauthorized
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return notionAdminSpaceExportDownloadClaim{}, "", errNotionAdminTokenUnauthorized
	}
	secret, err := notionAdminTokenCredential(app, claim.TokenID)
	if err != nil {
		return notionAdminSpaceExportDownloadClaim{}, "", refusedUnlessStorage(errNotionAdminTokenUnauthorized, err)
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(signature, notionAdminSpaceExportDownloadSignature(secret, parts[1])) {
		return notionAdminSpaceExportDownloadClaim{}, "", errNotionAdminTokenUnauthorized
	}
	return claim, secret, nil
}

func notionAdminSpaceExportDownloadSignature(secret string, payload string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(notionAdminSpaceExportDownloadDomain + "\x00" + payload))
	return mac.Sum(nil)
}
