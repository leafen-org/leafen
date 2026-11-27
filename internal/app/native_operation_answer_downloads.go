package app

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/security"
)

// An answer larger than leafen_call gives back (an export's file, a backup's
// download, a long text file or list) is kept as it is made and handed over
// as a download address of its own that works for five minutes without a
// key, as Notion's MCP gives a file as a signed address rather than its
// bytes: an agent connected by OAuth, which has no connection key to send
// the request over HTTP again, downloads it too (2026-11-22: such an answer
// said only its size and how to send the request again with an lmc_ key).
// The address names the answer kept in Leafen's own work folder, the
// connection and person it was made for, and when it expires, sealed with
// the server's secret; it opens only while that connection still holds and
// its person may act. What was kept goes once it has expired, and none is
// kept that would bring the disk under its floor (data_disk_floor.go).

const (
	operationAnswerDownloadVersion  = 1
	operationAnswerDownloadTokenMax = 4096
	operationAnswerFolderName       = "operation-answers"
	// operationAnswerDiskStep is how much is kept between looks at the
	// disk's room.
	operationAnswerDiskStep = 64 << 20
)

var (
	operationAnswerNamePattern = regexp.MustCompile(`^[a-z0-9]{24}$`)

	errOperationAnswerDownloadInvalid = errors.New("the answer's download address can't be used")
)

type operationAnswerDownloadPayload struct {
	FormatVersion int    `json:"formatVersion"`
	ConnectionID  string `json:"connectionId"`
	PrincipalID   string `json:"principalId"`
	Name          string `json:"name"`
	ContentType   string `json:"contentType"`
	Filename      string `json:"filename,omitempty"`
	ExpiresAt     int64  `json:"expiresAt"`
}

// operationAnswerDownloads lets an operation's caller keep a large answer of
// bytes to hand over by address; a call without it keeps none.
type operationAnswerDownloads struct{ app core.App }

type operationAnswerDownloadsKey struct{}

func withOperationAnswerDownloads(ctx context.Context, app core.App) context.Context {
	return context.WithValue(ctx, operationAnswerDownloadsKey{}, &operationAnswerDownloads{app: app})
}

func operationAnswerDownloadsOf(ctx context.Context) *operationAnswerDownloads {
	downloads, _ := ctx.Value(operationAnswerDownloadsKey{}).(*operationAnswerDownloads)
	return downloads
}

func operationAnswerFolder(app core.App) string {
	return filepath.Join(leafenTempDir(app.DataDir()), operationAnswerFolderName)
}

// operationAnswerSpool is a large answer being kept as it is made.
type operationAnswerSpool struct {
	downloads *operationAnswerDownloads
	file      *os.File
	name      string
	bytes     int64
	failed    bool
}

// open starts keeping an answer, after taking away those expired.
func (downloads *operationAnswerDownloads) open() *operationAnswerSpool {
	folder := operationAnswerFolder(downloads.app)
	if requireDataDiskRoom(downloads.app, operationAnswerDiskStep) != nil || os.MkdirAll(folder, 0o700) != nil {
		return nil
	}
	pruneOperationAnswers(folder, time.Now())
	name := security.RandomStringWithAlphabet(24, "abcdefghijklmnopqrstuvwxyz0123456789")
	file, err := os.OpenFile(filepath.Join(folder, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil
	}
	return &operationAnswerSpool{downloads: downloads, file: file, name: name}
}

// pruneOperationAnswers takes away the answers whose addresses expired.
func pruneOperationAnswers(folder string, now time.Time) {
	entries, err := os.ReadDir(folder)
	if err != nil {
		return
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err == nil && operationAnswerNamePattern.MatchString(entry.Name()) &&
			now.Sub(info.ModTime()) > time.Duration(operationAnswerDownloadTTLSeconds)*time.Second+time.Minute {
			_ = os.Remove(filepath.Join(folder, entry.Name()))
		}
	}
}

func (spool *operationAnswerSpool) write(data []byte) {
	if spool.failed {
		return
	}
	next := spool.bytes + int64(len(data))
	if next > operationAnswerDownloadMaxBytes ||
		next/operationAnswerDiskStep > spool.bytes/operationAnswerDiskStep && requireDataDiskRoom(spool.downloads.app, operationAnswerDiskStep) != nil {
		spool.failed = true
		return
	}
	written, err := spool.file.Write(data)
	spool.bytes += int64(written)
	if err != nil {
		spool.failed = true
	}
}

// discard takes the kept answer away.
func (spool *operationAnswerSpool) discard() {
	_ = spool.file.Close()
	_ = os.Remove(filepath.Join(operationAnswerFolder(spool.downloads.app), spool.name))
}

// offer hands an answer larger than comes back over by a download address,
// for the connection and person the call was made for; any other answer, or
// one not kept whole, keeps nothing. Words and JSON say a list can be read in
// parts too.
func (spool *operationAnswerSpool) offer(result *nativeOperationResult, key userKeyAuthority, total int) {
	if spool.failed || spool.bytes != int64(total) || result.BodyBase64 != "" || result.Bytes == 0 ||
		key.ConnectionID == "" || key.OwnerID == "" {
		spool.discard()
		return
	}
	if err := spool.file.Close(); err != nil {
		spool.discard()
		return
	}
	token, expiresAt, err := sealOperationAnswerDownload(spool.downloads.app, operationAnswerDownloadPayload{
		ConnectionID: key.ConnectionID, PrincipalID: key.OwnerID, Name: spool.name,
		ContentType: result.ContentType, Filename: result.Filename,
	})
	if err != nil {
		_ = os.Remove(filepath.Join(operationAnswerFolder(spool.downloads.app), spool.name))
		return
	}
	result.DownloadURL = apiRoot + strings.Replace(fileTransferOperationAnswerDownloadPath, "{token}", url.PathEscape(token), 1)
	// On the agent's person's clock, a key's in UTC (person_clock.go).
	clock := time.UTC
	if key.MCP != nil {
		clock = key.MCP.Authority.clock()
	}
	result.DownloadExpiresAt = mcpTimestamp(expiresAt, clock)
	result.Note = fmt.Sprintf("This answer is %d bytes, more than the %d bytes an answer gives back: download it instead from download_url, "+
		"which works without a key until download_expires_at; after that, run the call again for a new one.",
		total, mcpAttachmentInlineMaximumBytes)
	if nativeOperationAnswerJSON(result.ContentType) || nativeOperationAnswerWords(result.ContentType) {
		result.Note += " A list can be read in parts instead, a few at a time with its limit."
	}
}

func operationAnswerDownloadCipherKey(secret string) string {
	digest := sha256.Sum256([]byte("leafen-operation-answer-download-v1\x00" + secret))
	return string(digest[:])
}

func sealOperationAnswerDownload(app core.App, payload operationAnswerDownloadPayload) (string, int64, error) {
	secret, err := administratorSettingsSecret(app)
	if err != nil {
		return "", 0, err
	}
	payload.FormatVersion = operationAnswerDownloadVersion
	payload.ExpiresAt = time.Now().UTC().Add(time.Duration(operationAnswerDownloadTTLSeconds) * time.Second).UnixMilli()
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", 0, err
	}
	cipher, err := security.Encrypt(encoded, operationAnswerDownloadCipherKey(secret))
	if err != nil {
		return "", 0, err
	}
	token := base64.RawURLEncoding.EncodeToString([]byte(cipher))
	if len(token) > operationAnswerDownloadTokenMax {
		return "", 0, errOperationAnswerDownloadInvalid
	}
	return token, payload.ExpiresAt, nil
}

// openOperationAnswerDownload reads a download address's token; expired says
// it was one that has expired.
func openOperationAnswerDownload(app core.App, token string) (payload operationAnswerDownloadPayload, expired bool, err error) {
	if len(token) < 64 || len(token) > operationAnswerDownloadTokenMax {
		return payload, false, errOperationAnswerDownloadInvalid
	}
	secret, err := administratorSettingsSecret(app)
	if err != nil {
		return payload, false, err
	}
	cipher, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return payload, false, errOperationAnswerDownloadInvalid
	}
	plain, err := security.Decrypt(string(cipher), operationAnswerDownloadCipherKey(secret))
	if err != nil || json.Unmarshal(plain, &payload) != nil || payload.FormatVersion != operationAnswerDownloadVersion ||
		payload.ConnectionID == "" || payload.PrincipalID == "" || !operationAnswerNamePattern.MatchString(payload.Name) ||
		payload.ExpiresAt <= 0 {
		return operationAnswerDownloadPayload{}, false, errOperationAnswerDownloadInvalid
	}
	if time.Now().UTC().UnixMilli() >= payload.ExpiresAt {
		return payload, true, errOperationAnswerDownloadInvalid
	}
	return payload, false, nil
}

// operationAnswerConnectionHolds tells whether the connection an answer was
// made for still holds for its person: not taken back, and any connection
// that made it neither, its person still able to act.
func operationAnswerConnectionHolds(app core.App, payload operationAnswerDownloadPayload) (bool, error) {
	connection := struct {
		OwnerID   string `db:"owner_id"`
		RevokedAt int64  `db:"revoked_at"`
	}{}
	err := app.DB().NewQuery(`SELECT [[owner_id]], [[revoked_at]] FROM {{leafen_mcp_connections}} WHERE [[id]] = {:id}`).
		Bind(dbx.Params{"id": payload.ConnectionID}).One(&connection)
	if err != nil || connection.OwnerID != payload.PrincipalID || connection.RevokedAt != 0 {
		return false, nil
	}
	if holds, err := connectionLineageHolds(app, payload.ConnectionID); err != nil || !holds {
		return false, err
	}
	return accountMayAct(app, payload.PrincipalID)
}

func registerOperationAnswerDownloadRoute(event *core.ServeEvent) {
	event.Router.Route(fileTransferOperationAnswerDownloadMethod, apiRoot+fileTransferOperationAnswerDownloadPath, downloadOperationAnswer)
}

// downloadOperationAnswer serves a kept answer to whoever holds its address.
func downloadOperationAnswer(event *core.RequestEvent) error {
	payload, expired, err := openOperationAnswerDownload(event.App, strings.TrimSpace(event.Request.PathValue("token")))
	if expired {
		return apiError(event, http.StatusUnauthorized, errorCodeFileDownloadCapabilityExpired,
			"The answer's download address expired: run the call again for a new one.")
	}
	if err != nil && !errors.Is(err, errOperationAnswerDownloadInvalid) {
		return apiErrorWithCause(event, http.StatusServiceUnavailable, errorCodeFileDownloadCapabilityUnavailable,
			"Couldn't check the answer's download address.", "operation-answer", "secret", errorCodeFileDownloadCapabilityUnavailable, err)
	}
	refused := func() error {
		return apiError(event, http.StatusNotFound, errorCodeFileDownloadCapabilityInvalid, "The answer's download address can't be used.")
	}
	if err != nil {
		return refused()
	}
	if holds, err := operationAnswerConnectionHolds(event.App, payload); err != nil {
		return apiErrorWithCause(event, http.StatusServiceUnavailable, errorCodeFileDownloadCapabilityUnavailable,
			"Couldn't check the answer's download address.", "operation-answer", "connection", errorCodeFileDownloadCapabilityUnavailable, err)
	} else if !holds {
		return refused()
	}
	file, err := os.Open(filepath.Join(operationAnswerFolder(event.App), payload.Name))
	if err != nil {
		return refused()
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return refused()
	}
	header := event.Response.Header()
	contentType := payload.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	header.Set("Content-Type", contentType)
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Cache-Control", "private, no-store")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("Content-Security-Policy", "sandbox")
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": payload.Filename})
	if payload.Filename == "" || disposition == "" {
		disposition = "attachment"
	}
	header.Set("Content-Disposition", disposition)
	http.ServeContent(event.Response, event.Request, "", info.ModTime(), file)
	return nil
}
