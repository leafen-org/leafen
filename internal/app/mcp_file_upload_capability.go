package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

const (
	mcpFileUploadCapabilityPath           = "/api/leafen/v1/mcp-file-uploads/{token}"
	mcpFileUploadCapabilityVersion        = 1
	mcpFileUploadCapabilityTTL            = 5 * time.Minute
	mcpFileUploadMaximumBytes       int64 = 20 << 20
	mcpFileUploadMultipartAllowance       = 1 << 20
	mcpFileUploadCapabilityTokenMax       = 4096
)

var (
	errMCPFileUploadCapabilityInvalid = errors.New("LEAFEN_MCP_FILE_UPLOAD_CAPABILITY_INVALID")
	errMCPFileUploadCapabilityExpired = errors.New("LEAFEN_MCP_FILE_UPLOAD_CAPABILITY_EXPIRED")
)

type mcpFileUploadCapabilityPayload struct {
	FormatVersion  int    `json:"formatVersion"`
	ConnectionID   string `json:"connectionId"`
	CredentialHash string `json:"credentialHash"`
	PrincipalID    string `json:"principalId"`
	WorkspaceID    string `json:"workspaceId"`
	FileID         string `json:"fileId"`
	RequestID      string `json:"requestId"`
	Filename       string `json:"filename"`
	ContentType    string `json:"contentType"`
	ExpiresAt      int64  `json:"expiresAt"`
}

func sealMCPFileUploadCapability(
	app core.App,
	payload mcpFileUploadCapabilityPayload,
) (string, int64, error) {
	secret, err := administratorSettingsSecret(app)
	if err != nil {
		return "", 0, err
	}
	payload.FormatVersion = mcpFileUploadCapabilityVersion
	payload.ExpiresAt = time.Now().UTC().Add(mcpFileUploadCapabilityTTL).UnixMilli()
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", 0, err
	}
	cipher, err := security.Encrypt(encoded, mcpFileUploadCapabilityCipherKey(secret))
	if err != nil {
		return "", 0, err
	}
	token := base64.RawURLEncoding.EncodeToString([]byte(cipher))
	if len(token) < 64 || len(token) > mcpFileUploadCapabilityTokenMax {
		return "", 0, errMCPFileUploadCapabilityInvalid
	}
	return token, payload.ExpiresAt, nil
}

func openMCPFileUploadCapability(
	app core.App,
	token string,
) (mcpFileUploadCapabilityPayload, error) {
	if len(token) < 64 || len(token) > mcpFileUploadCapabilityTokenMax {
		return mcpFileUploadCapabilityPayload{}, errMCPFileUploadCapabilityInvalid
	}
	secret, err := administratorSettingsSecret(app)
	if err != nil {
		return mcpFileUploadCapabilityPayload{}, err
	}
	cipher, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return mcpFileUploadCapabilityPayload{}, errMCPFileUploadCapabilityInvalid
	}
	plain, err := security.Decrypt(string(cipher), mcpFileUploadCapabilityCipherKey(secret))
	if err != nil {
		return mcpFileUploadCapabilityPayload{}, errMCPFileUploadCapabilityInvalid
	}
	payload := mcpFileUploadCapabilityPayload{}
	if json.Unmarshal(plain, &payload) != nil ||
		payload.FormatVersion != mcpFileUploadCapabilityVersion ||
		payload.ConnectionID == "" || payload.CredentialHash == "" || payload.PrincipalID == "" ||
		payload.WorkspaceID == "" || !fileUploadIDPattern.MatchString(payload.FileID) ||
		!validProductFileUploadRequestID(payload.RequestID) || payload.Filename == "" ||
		payload.ContentType == "" || payload.ExpiresAt <= 0 {
		return mcpFileUploadCapabilityPayload{}, errMCPFileUploadCapabilityInvalid
	}
	if time.Now().UTC().UnixMilli() >= payload.ExpiresAt {
		return mcpFileUploadCapabilityPayload{}, errMCPFileUploadCapabilityExpired
	}
	return payload, nil
}

func mcpFileUploadCapabilityCipherKey(secret string) string {
	digest := sha256.Sum256([]byte("leafen-mcp-file-upload-capability-v1\x00" + secret))
	return string(digest[:])
}

func mcpFileUploadURL(resource, token string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(resource))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" ||
		parsed.User != nil || (parsed.Path != "/mcp" && parsed.Path != nativeMCPPath) || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errMCPFileUploadCapabilityInvalid
	}
	parsed.Path = strings.Replace(mcpFileUploadCapabilityPath, "{token}", url.PathEscape(token), 1)
	return parsed.String(), nil
}

// mcpTimestamp is a time a tool gives, on the clock (person_clock.go).
func mcpTimestamp(milliseconds int64, clock *time.Location) string {
	return personClockMillis(time.UnixMilli(milliseconds), clock)
}

func uploadMCPFileWithCapability(event *core.RequestEvent) error {
	token := strings.TrimSpace(event.Request.PathValue("token"))
	payload, err := openMCPFileUploadCapability(event.App, token)
	if err != nil {
		return writeMCPFileUploadCapabilityError(event, err, "capability-open")
	}
	if err := authorizeMCPFileUploadCapability(event.App, payload); err != nil {
		return writeMCPFileUploadCapabilityError(event, err, "capability-authorize")
	}
	uploads, err := event.FindUploadedFiles("file")
	if event.Request.MultipartForm != nil {
		defer event.Request.MultipartForm.RemoveAll()
	}
	if err != nil || len(uploads) != 1 || uploads[0].Size <= 0 {
		return writeMCPFileUploadCapabilityError(event, mcpFileUploadBodyRefused(event, len(uploads), len(uploads) == 1), "multipart")
	}
	if uploads[0].Size > mcpFileUploadMaximumBytes {
		return writeMCPFileUploadCapabilityError(event, errFileUploadInvalid, "multipart")
	}
	file := uploads[0]
	if err := requireDataDiskRoom(event.App, file.Size); err != nil {
		return writeServerDiskLow(event, "mcp-file-upload", err)
	}
	digest, err := fileDigest(file.Reader)
	if err != nil {
		return writeMCPFileUploadCapabilityError(event, err, "digest")
	}
	input := fileUploadPrepareInput{
		RequestID: payload.RequestID, WorkspaceID: payload.WorkspaceID,
		Name: payload.Filename, Size: file.Size, ContentType: payload.ContentType,
	}
	if err := normalizeProductFileUploadInput(&input); err != nil {
		return writeMCPFileUploadCapabilityError(event, err, "validate")
	}
	release := acquireProductFileOperationLock(
		productFileOperationScopedNamespace(productFileOperationUploadRequest, payload.PrincipalID),
		input.RequestID,
	)
	defer release()
	receipt, created, err := admitDirectProductFileUploadWithID(
		event.App, payload.PrincipalID, input, digest, payload.FileID,
	)
	if err != nil {
		return writeMCPFileUploadCapabilityError(event, err, "admit")
	}
	uploadRelease := acquireProductFileOperationLock(productFileOperationResumable, receipt.UploadID)
	defer uploadRelease()
	var record *core.Record
	if receipt.Status == fileUploadStatusCompleted {
		record, err = completedDirectProductFileUpload(event.App, receipt)
	} else {
		record, err = publishDirectProductFileUpload(event.App, receipt, file, digest)
	}
	if err != nil {
		return writeMCPFileUploadCapabilityError(event, err, "publish")
	}
	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	event.Response.Header().Set("Cache-Control", "no-store")
	return event.JSON(status, mcpFileUploadCapabilityAnswer{"file_upload", record.Id, notionMCPAttachmentRecord(record)})
}

// mcpFileUploadCapabilityAnswer is the upload's own answer, naming it as
// Notion's file upload does, before what notion-create-attachment answers.
type mcpFileUploadCapabilityAnswer struct {
	Object string `json:"object"`
	ID     string `json:"id"`
	notionMCPAttachmentOutput
}

func authorizeMCPFileUploadCapability(
	app core.App,
	payload mcpFileUploadCapabilityPayload,
) error {
	connection := storedMCPConnection{}
	err := app.DB().NewQuery(`
		SELECT [[id]], [[owner_id]], [[client_name]], [[token_hash]], [[token_expires_at]],
			[[oauth_client_id]], [[oauth_resource]], [[oauth_scope]], [[revoked_at]]
		FROM {{leafen_mcp_connections}}
		WHERE [[id]] = {:connectionId}
		LIMIT 1
	`).Bind(dbx.Params{"connectionId": payload.ConnectionID}).One(&connection)
	// The connection works as its person.
	if errors.Is(err, sql.ErrNoRows) || err == nil && (connection.OwnerID != payload.PrincipalID ||
		connection.TokenHash != payload.CredentialHash || connection.RevokedAt != 0 ||
		connection.TokenExpiresAt != 0 && connection.TokenExpiresAt <= time.Now().UnixMilli()) {
		return errMCPFileUploadCapabilityInvalid
	}
	if err != nil {
		return err
	}
	// The connection, and any that made it, still connected.
	if holds, err := connectionLineageHolds(app, payload.ConnectionID); err != nil {
		return err
	} else if !holds {
		return errMCPFileUploadCapabilityInvalid
	}
	var scoped int
	if err := app.DB().NewQuery(`
		SELECT EXISTS(
			SELECT 1 FROM {{leafen_mcp_connection_workspaces}}
			WHERE [[connection_id]] = {:connectionId} AND [[workspace_id]] = {:workspaceId}
		)
	`).Bind(dbx.Params{
		"connectionId": payload.ConnectionID, "workspaceId": payload.WorkspaceID,
	}).Row(&scoped); err != nil {
		return err
	}
	if scoped != 1 {
		return errMCPFileUploadCapabilityInvalid
	}
	if _, err := authorizeWorkspace(app, payload.PrincipalID, payload.WorkspaceID, permissionWorkspaceWrite); err != nil {
		return mcpRefusedUnlessStorage(errMCPFileUploadCapabilityInvalid, err)
	}
	workspaces, err := loadMCPGovernanceWorkspaces(app, []string{payload.WorkspaceID})
	if err != nil || len(workspaces) != 1 {
		return errors.Join(errMCPFileUploadCapabilityInvalid, err)
	}
	clientID := connection.OAuthClientID
	if clientID == "" {
		clientID = mcpGovernanceManualClientID
	}
	approved, err := filterMCPClientApprovedWorkspaces(app, workspaces, clientID, connection.OwnerID)
	if err != nil || len(approved) != 1 || approved[0].ID != payload.WorkspaceID {
		return errors.Join(errMCPFileUploadCapabilityInvalid, err)
	}
	return nil
}

func writeMCPFileUploadCapabilityError(event *core.RequestEvent, err error, stage string) error {
	switch {
	case errors.Is(err, errMCPFileUploadCapabilityExpired):
		return apiErrorWithCause(
			event, http.StatusGone, "LEAFEN_MCP_FILE_UPLOAD_EXPIRED",
			"The file upload URL expired.", "mcp-file-upload", stage,
			"LEAFEN_MCP_FILE_UPLOAD_EXPIRED", err,
		)
	case errors.Is(err, errMCPFileUploadCapabilityInvalid):
		return apiErrorWithCause(
			event, http.StatusNotFound, "LEAFEN_MCP_FILE_UPLOAD_INVALID",
			"The file upload URL can't be used.", "mcp-file-upload", stage,
			"LEAFEN_MCP_FILE_UPLOAD_INVALID", err,
		)
	default:
		return directFileUploadError(event, err, "mcp-"+stage)
	}
}
