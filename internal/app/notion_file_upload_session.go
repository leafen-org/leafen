package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

const (
	notionFileUploadModeSingle   = "single_part"
	notionFileUploadModeMulti    = "multi_part"
	notionFileUploadModeExternal = "external_url"
	notionFileUploadLifetime     = time.Hour
	notionFileUploadMinPartBytes = int64(5 << 20)
	notionFileUploadMaxPartBytes = int64(20 << 20)
	notionFileUploadMaxParts     = 10000
	notionFileUploadMaxNameBytes = 900
)

type notionFileUploadCreateRequest struct {
	Mode          string `json:"mode"`
	Filename      string `json:"filename"`
	ContentType   string `json:"content_type"`
	NumberOfParts int    `json:"number_of_parts"`
	ExternalURL   string `json:"external_url"`
}

func normalizeNotionFileUploadCreate(
	input *notionFileUploadCreateRequest,
) (string, error) {
	input.Mode = strings.TrimSpace(input.Mode)
	if input.Mode == "" {
		input.Mode = notionFileUploadModeSingle
	}
	input.ExternalURL = strings.TrimSpace(input.ExternalURL)
	if len(input.ContentType) > maxFileContentTypeLength {
		return "", notionInvalid(errFileUploadInvalid, "content_type is too long.")
	}
	name, contentType, err := normalizeNotionFileUploadMetadata(
		input.Filename, input.ContentType,
	)
	if err != nil {
		return "", err
	}
	input.Filename, input.ContentType = name, contentType
	if len([]byte(input.Filename)) > notionFileUploadMaxNameBytes {
		return "", notionInvalid(errFileUploadInvalid, fmt.Sprintf("filename must be at most %d bytes.", notionFileUploadMaxNameBytes))
	}
	switch input.Mode {
	case notionFileUploadModeSingle:
		if input.NumberOfParts != 0 || input.ExternalURL != "" {
			return "", notionInvalid(errFileUploadInvalid, "A single_part upload takes no number_of_parts and no external_url.")
		}
		return fileUploadKindNotionSingle, nil
	case notionFileUploadModeMulti:
		if input.Filename == "" || input.NumberOfParts < 1 ||
			input.NumberOfParts > notionFileUploadMaxParts || input.ExternalURL != "" {
			return "", notionInvalid(errFileUploadInvalid, fmt.Sprintf("A multi_part upload needs filename and number_of_parts from 1 to %d, and no external_url.", notionFileUploadMaxParts))
		}
		return fileUploadKindNotionMulti, nil
	case notionFileUploadModeExternal:
		if input.Filename == "" || input.NumberOfParts != 0 || input.ExternalURL == "" {
			return "", notionInvalid(errFileUploadInvalid, "An external_url upload needs filename and external_url, and no number_of_parts.")
		}
		if _, err := validateNotionFileURL(input.ExternalURL); err != nil {
			return "", notionInvalid(errFileUploadInvalid, "external_url must be a public https URL.")
		}
		return fileUploadKindNotionExternal, nil
	default:
		return "", notionInvalid(errFileUploadInvalid, "mode takes single_part, multi_part or external_url.")
	}
}

func admitNotionFileUpload(
	app core.App,
	identity notionOAuthBearerIdentity,
	input notionFileUploadCreateRequest,
	kind string,
) (storedProductFileUpload, error) {
	encoded, _ := json.Marshal([]any{
		"notion-file-upload:v1", input.Mode, input.Filename,
		input.ContentType, input.NumberOfParts, input.ExternalURL,
	})
	digest := sha256.Sum256(encoded)
	now := time.Now().UTC().UnixMilli()
	upload := storedProductFileUpload{
		ActorID: identity.PrincipalID, RequestID: "notion-file-upload:" + newDiagnosticReference(),
		RequestHash: hex.EncodeToString(digest[:]), UploadID: core.GenerateDefaultRandomId(),
		Kind: kind, WorkspaceID: identity.WorkspaceID, OriginalName: input.Filename,
		ContentType: input.ContentType, Status: fileUploadStatusPending,
		CreatedAt: now, UpdatedAt: now, CleanupDueAt: now + notionFileUploadLifetime.Milliseconds(),
		NotionMode: input.Mode, NotionPartsTotal: input.NumberOfParts,
	}
	if input.Mode == notionFileUploadModeExternal {
		key, err := notionImportEncryptionKey(app)
		if err != nil {
			return storedProductFileUpload{}, err
		}
		cipher, err := security.Encrypt([]byte(input.ExternalURL), key)
		if err != nil {
			return storedProductFileUpload{}, err
		}
		upload.NotionExternalURLCipher = cipher
		upload.NotionImportNextAt = now
	}
	err := app.RunInTransaction(func(txApp core.App) error {
		authorization, err := authorizeProductFileUpload(
			txApp, identity.PrincipalID, identity.WorkspaceID, productFileUploadScope{},
		)
		if err != nil {
			return err
		}
		stored, _ := notionFileUploadStoredType(input.Filename, input.ContentType)
		if err := authorizeProductFileUploadMetadata(
			txApp, authorization, input.Filename, stored,
		); err != nil {
			return err
		}
		if _, err := txApp.DB().Insert(productFileUploadsTable, dbx.Params{
			"actor_id": upload.ActorID, "request_id": upload.RequestID,
			"request_hash": upload.RequestHash, "upload_id": upload.UploadID,
			"kind": upload.Kind, "workspace_id": upload.WorkspaceID,
			"original_name": upload.OriginalName, "content_type": upload.ContentType,
			"total_size": 0, "status": upload.Status, "received_bytes": 0,
			"next_part": 0, "file_id": "", "sha256": "",
			"created_at": now, "updated_at": now, "completed_at": 0,
			"cleanup_due_at": upload.CleanupDueAt, "cleanup_attempts": 0,
			"cleanup_error_code": "", "notion_mode": upload.NotionMode,
			"notion_parts_total":         upload.NotionPartsTotal,
			"notion_external_url_cipher": upload.NotionExternalURLCipher,
			"notion_import_attempts":     0, "notion_import_next_at": upload.NotionImportNextAt,
			"notion_attached_at": 0, "notion_failure_type": "",
			"notion_failure_code": "", "notion_failure_message": "",
			"notion_failure_parameter": "", "notion_failure_status": 0,
		}).Execute(); err != nil {
			return err
		}
		if _, err := ensureNotionObjectIdentity(
			txApp, notionObjectIdentityFileUpload, upload.WorkspaceID, upload.UploadID,
		); err != nil {
			return err
		}
		recordFileUploadAdmittedWebhookEvents(txApp, upload, identity)
		return nil
	})
	if err != nil {
		return storedProductFileUpload{}, err
	}
	notifyProductFileUploadCleanupAfterCommit(app)
	if input.Mode == notionFileUploadModeExternal {
		notifyNotionFileUploadImportAfterCommit(app)
	}
	return upload, nil
}

func loadAuthorizedNotionFileUpload(
	app core.App,
	identity notionOAuthBearerIdentity,
	publicID string,
) (storedProductFileUpload, error) {
	resolved, err := resolveNotionObjectIdentity(
		app, notionObjectIdentityFileUpload, strings.TrimSpace(publicID),
	)
	if err != nil || resolved.Workspace != identity.WorkspaceID {
		return storedProductFileUpload{}, errFileUploadNotFound
	}
	upload, err := loadProductFileUploadByID(app, identity.PrincipalID, resolved.InternalID)
	if errors.Is(err, sql.ErrNoRows) || err == nil &&
		(upload.WorkspaceID != identity.WorkspaceID || upload.NotionMode == "") {
		return storedProductFileUpload{}, errFileUploadNotFound
	}
	return upload, err
}

func notionFileUploadExpiresAt(upload storedProductFileUpload) int64 {
	if upload.NotionMode == "" || upload.NotionAttachedAt > 0 || upload.CreatedAt <= 0 {
		return 0
	}
	return upload.CreatedAt + notionFileUploadLifetime.Milliseconds()
}

func notionFileUploadPublicStatus(upload storedProductFileUpload, now int64) string {
	switch upload.Status {
	case fileUploadStatusFailed:
		return "failed"
	case fileUploadStatusExpired, fileUploadStatusCleanupPending:
		return "expired"
	case fileUploadStatusCompleted:
		if expiresAt := notionFileUploadExpiresAt(upload); expiresAt > 0 && expiresAt <= now {
			return "expired"
		}
		return "uploaded"
	default:
		if expiresAt := notionFileUploadExpiresAt(upload); expiresAt > 0 && expiresAt <= now {
			return "expired"
		}
		return "pending"
	}
}

func markNotionFileUploadAttached(app core.App, fileID string, now int64) error {
	if strings.TrimSpace(fileID) == "" || now <= 0 {
		return nil
	}
	_, err := app.DB().NewQuery(`
		UPDATE {{leafen_file_uploads}}
		SET [[notion_attached_at]]=CASE WHEN [[notion_attached_at]]=0
			THEN {:now} ELSE [[notion_attached_at]] END, [[updated_at]]={:now}
		WHERE [[file_id]]={:fileId} AND [[notion_mode]]!=''
			AND [[status]]={:completed}
	`).Bind(dbx.Params{
		"now": now, "fileId": fileID, "completed": fileUploadStatusCompleted,
	}).Execute()
	return err
}
