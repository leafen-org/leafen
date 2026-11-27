package app

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
	"github.com/pocketbase/pocketbase/tools/security"
)

const fileDownloadCapabilityFormatVersion = 1

var (
	errFileDownloadCapabilityInvalid = errors.New(errorCodeFileDownloadCapabilityInvalid)
	errFileDownloadCapabilityExpired = errors.New(errorCodeFileDownloadCapabilityExpired)
)

type fileDownloadCapabilityPayload struct {
	FormatVersion int    `json:"formatVersion"`
	UserID        string `json:"userId"`
	FileID        string `json:"fileId"`
	ExpiresAt     int64  `json:"expiresAt"`
}

type authorizedProductFileRead struct {
	Record     *core.Record
	Filesystem *filesystem.System
}

func issueFileDownloadCapability(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	fileID := strings.TrimSpace(event.Request.PathValue("id"))
	read, err := authorizeProductFileRead(event.App, user.Id, fileID)
	if err != nil {
		return writeProductFileReadError(event, err, false)
	}
	read.Filesystem.Close()

	secret, err := fileDownloadCapabilitySecret(event.App)
	if err != nil {
		return apiErrorWithCause(
			event, http.StatusServiceUnavailable, errorCodeFileDownloadCapabilityUnavailable,
			"Couldn't create the file download URL.", "file-capability", "secret",
			errorCodeFileDownloadCapabilityUnavailable, err,
		)
	}
	token, expiresAt, err := sealFileDownloadCapabilityToken(secret, user.Id, read.Record.Id, time.Now().UTC())
	if err != nil {
		return apiErrorWithCause(
			event, http.StatusServiceUnavailable, errorCodeFileDownloadCapabilityUnavailable,
			"Couldn't create the file download URL.", "file-capability", "seal",
			errorCodeFileDownloadCapabilityUnavailable, err,
		)
	}
	path := strings.Replace(fileTransferDownloadCapabilityPath, "{token}", url.PathEscape(token), 1)
	return event.JSON(http.StatusOK, fileDownloadCapabilityResponse{
		URL: apiRoot + path, ExpiresAt: expiresAt,
	})
}

func downloadFileWithCapability(event *core.RequestEvent) error {
	token := strings.TrimSpace(event.Request.PathValue("token"))
	secret, err := fileDownloadCapabilitySecret(event.App)
	if err != nil {
		return apiErrorWithCause(
			event, http.StatusServiceUnavailable, errorCodeFileDownloadCapabilityUnavailable,
			"Couldn't check the file download URL.", "file-capability", "secret",
			errorCodeFileDownloadCapabilityUnavailable, err,
		)
	}
	payload, err := openFileDownloadCapabilityToken(secret, token, time.Now().UTC())
	if errors.Is(err, errFileDownloadCapabilityExpired) {
		return apiErrorWithCause(
			event, http.StatusUnauthorized, errorCodeFileDownloadCapabilityExpired,
			"The file download URL expired.", "file-capability", "expiry",
			errorCodeFileDownloadCapabilityExpired, err,
		)
	}
	if err != nil {
		return apiErrorWithCause(
			event, http.StatusNotFound, errorCodeFileDownloadCapabilityInvalid,
			"The file download URL can't be used.", "file-capability", "open",
			errorCodeFileDownloadCapabilityInvalid, err,
		)
	}
	return serveAuthorizedProductFile(event, payload.UserID, payload.FileID, true)
}

func serveAuthorizedProductFile(
	event *core.RequestEvent,
	principalID string,
	fileID string,
	capability bool,
) error {
	read, err := authorizeProductFileRead(event.App, principalID, fileID)
	if err != nil {
		return writeProductFileReadError(event, err, capability)
	}
	defer read.Filesystem.Close()
	record := read.Record
	event.Response.Header().Set("Content-Type", record.GetString("content_type"))
	event.Response.Header().Set("X-Content-Type-Options", "nosniff")
	sandboxStoredFileDownload(event.Response.Header(), record.GetString("content_type"))
	if capability {
		event.Response.Header().Set("Cache-Control", "private, no-store")
		event.Response.Header().Set("Referrer-Policy", "no-referrer")
	}
	fileKey := record.BaseFilesPath() + "/" + record.GetString("blob")
	fsys := read.Filesystem
	// A video's still is a preview of it, read as no open of it.
	if productFilePosterAsked(event.Request.URL.Query()) {
		return serveProductFilePoster(event, fsys, record)
	}
	preview, outcome := productFilePreviewKey(event.Request.Context(), fsys, record, event.Request.URL.Query().Get("w"))
	switch {
	case preview != "":
		fileKey = preview
	case outcome == productFilePreviewBusy:
		event.Response.Header().Set("Cache-Control", "private, no-store")
	case outcome == productFilePreviewRefused:
		return refuseProductFilePreview(event)
	}
	showProductTextFileInline(event.Request, event.Response.Header(), record.GetString("content_type"))
	if err := serveNamedFile(
		fsys, event.Response, event.Request, fileKey, productFileResponseName(event.Request, record.GetString("original_name")),
	); err != nil {
		return writeProductFileReadError(event, err, capability)
	}
	// A file given out is read for the read log (read_log.go): in
	// the app, or through a link the API or an agent's connection handed out.
	via := contentReadViaApp
	if capability {
		via = contentReadViaFileLink
	}
	recordContentReads(event.App, record.GetString("workspace"), contentReader{UserID: principalID, Via: via},
		[]contentRead{{Kind: "file", ID: record.Id, How: contentReadOpen}})
	return nil
}

// showProductTextFileInline shows a plain text file in the tab its address
// opens in, as Notion's file opens in a new tab with its text shown (seen
// 2026-11-15 twice: a .txt in a files property); the store gives pictures,
// sounds, films and PDFs so already, and the rest to save. Asked with
// ?download=1 it is saved too. It stays in its sandbox, read as text alone.
func showProductTextFileInline(request *http.Request, header http.Header, contentType string) {
	if download, _ := strconv.ParseBool(request.URL.Query().Get("download")); download {
		return
	}
	if base, _, _ := strings.Cut(contentType, ";"); strings.EqualFold(strings.TrimSpace(base), "text/plain") {
		header.Set("Content-Disposition", "inline")
	}
}

// A block or property can name the same stored file differently. The
// download name is response presentation; storage identity and ACL do not
// depend on it. Keep the existing upload name bound and reject controls.
func productFileResponseName(request *http.Request, original string) string {
	name := strings.TrimSpace(request.URL.Query().Get("filename"))
	if name == "" || !utf8.ValidString(name) || len([]rune(name)) > 2000 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return original
	}
	return name
}

func authorizeProductFileRead(
	app core.App,
	principalID string,
	fileID string,
) (authorizedProductFileRead, error) {
	record, authorization, err := authorizeWorkspaceFile(
		app, principalID, strings.TrimSpace(fileID), permissionWorkspaceRead,
	)
	// A workspace's icon reads outside it too (held_icon_files.go).
	if isPermissionDenied(err) {
		record, authorization, err = authorizeWorkspaceIconFileRead(app, principalID, strings.TrimSpace(fileID))
	}
	if err != nil {
		return authorizedProductFileRead{}, err
	}
	if authorization.Organization.OrganizationID != "" {
		if err := enforceOrganizationDLPOutbound(
			app, authorization.Organization.OrganizationID, dlpOutboundFileDownloads,
		); err != nil {
			return authorizedProductFileRead{}, err
		}
	}
	fsys, err := app.NewFilesystem()
	if err != nil {
		return authorizedProductFileRead{}, err
	}
	if err := verifyProductFileStorageIdentity(app, fsys, record); err != nil {
		fsys.Close()
		return authorizedProductFileRead{}, err
	}
	return authorizedProductFileRead{Record: record, Filesystem: fsys}, nil
}

func writeProductFileReadError(event *core.RequestEvent, err error, capability bool) error {
	if isDLPPolicyDecisionError(err) {
		return writeDLPPolicyError(event, err, "file-download")
	}
	if isPermissionDenied(err) {
		code := errorCodeFileNotFound
		message := "File not found."
		if capability {
			code = errorCodeFileDownloadCapabilityInvalid
			message = "The file download URL can't be used."
		}
		return apiErrorWithCause(
			event, http.StatusNotFound, code, message, "file-download", "authorize", code, err,
		)
	}
	if isProductFileIntegrityError(err) {
		return apiErrorWithCause(
			event, http.StatusConflict, errorCodeFileIntegrityFailed,
			"Couldn't verify the stored file's integrity.", "file-download", "integrity",
			errorCodeFileIntegrityFailed, err,
		)
	}
	code := errorCodeFileDownloadFailed
	if capability {
		code = errorCodeFileDownloadCapabilityUnavailable
	}
	return apiErrorWithCause(
		event, http.StatusServiceUnavailable, code, "Couldn't read the file.",
		"file-download", "storage", code, err,
	)
}

func fileDownloadCapabilitySecret(app core.App) (string, error) {
	return administratorSettingsSecret(app)
}

func fileDownloadCapabilityCipherKey(secret string) string {
	digest := sha256.Sum256([]byte("leafen-file-download-capability-v1\x00" + secret))
	return string(digest[:])
}

func sealFileDownloadCapabilityToken(
	secret string,
	userID string,
	fileID string,
	now time.Time,
) (string, int64, error) {
	expiresAt := now.Add(time.Duration(fileDownloadCapabilityTTLSeconds) * time.Second).UnixMilli()
	payload, err := json.Marshal(fileDownloadCapabilityPayload{
		FormatVersion: fileDownloadCapabilityFormatVersion,
		UserID:        userID, FileID: fileID, ExpiresAt: expiresAt,
	})
	if err != nil {
		return "", 0, err
	}
	cipher, err := security.Encrypt(payload, fileDownloadCapabilityCipherKey(secret))
	if err != nil {
		return "", 0, err
	}
	token := base64.RawURLEncoding.EncodeToString([]byte(cipher))
	if len(token) < 64 || len(token) > fileDownloadCapabilityTokenChars {
		return "", 0, errFileDownloadCapabilityInvalid
	}
	return token, expiresAt, nil
}

func openFileDownloadCapabilityToken(
	secret string,
	token string,
	now time.Time,
) (fileDownloadCapabilityPayload, error) {
	if len(token) < 64 || len(token) > fileDownloadCapabilityTokenChars {
		return fileDownloadCapabilityPayload{}, errFileDownloadCapabilityInvalid
	}
	cipher, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return fileDownloadCapabilityPayload{}, errFileDownloadCapabilityInvalid
	}
	plain, err := security.Decrypt(string(cipher), fileDownloadCapabilityCipherKey(secret))
	if err != nil {
		return fileDownloadCapabilityPayload{}, errFileDownloadCapabilityInvalid
	}
	payload := fileDownloadCapabilityPayload{}
	if json.Unmarshal(plain, &payload) != nil ||
		payload.FormatVersion != fileDownloadCapabilityFormatVersion ||
		payload.UserID == "" || len(payload.UserID) > maxWorkspacePermissionIdentifierLength ||
		payload.FileID == "" || len(payload.FileID) > maxWorkspacePermissionIdentifierLength ||
		payload.ExpiresAt <= 0 {
		return fileDownloadCapabilityPayload{}, errFileDownloadCapabilityInvalid
	}
	if now.UnixMilli() >= payload.ExpiresAt {
		return fileDownloadCapabilityPayload{}, errFileDownloadCapabilityExpired
	}
	return payload, nil
}
