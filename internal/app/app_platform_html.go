//go:build !leafen_noapps

package app

import (
	"bufio"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

// User HTML (docs/app-platform.md 14): an uploaded HTML file — an
// "HTML" block's or any embed's — opens as a small app with no permissions,
// in a frame with no origin (a CSP sandbox without same-origin), with no
// bridge and none of Leafen's data. Its own scripts and outside images,
// fonts, media and connections run, as it has nothing to send out; forms and
// the top window stay out of reach. A person who may read the file, or a
// public page that shows it, gets a ticket for an hour; an organization that
// turned user HTML off gets none, and its frames stop at their next load.

const htmlFramePrefix = "/html-frame/"

var errHTMLFrameTicket = errors.New("html frame ticket invalid")

func htmlFrameTicket(app core.App, fileID string, expires int64) (string, error) {
	secret, err := administratorSettingsSecret(app)
	if err != nil {
		return "", err
	}
	payload := fileID + "." + strconv.FormatInt(expires, 10)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("leafen-html-frame\n" + payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:18]), nil
}

// readHTMLFrameTicket returns the file a valid, unexpired ticket names.
func readHTMLFrameTicket(app core.App, ticket string, now time.Time) (string, error) {
	encodedPayload, encodedMAC, ok := strings.Cut(ticket, ".")
	payload, payloadErr := base64.RawURLEncoding.DecodeString(encodedPayload)
	given, macErr := base64.RawURLEncoding.DecodeString(encodedMAC)
	if !ok || payloadErr != nil || macErr != nil {
		return "", errHTMLFrameTicket
	}
	fileID, rawExpires, ok := strings.Cut(string(payload), ".")
	expires, err := strconv.ParseInt(rawExpires, 10, 64)
	if !ok || err != nil || fileID == "" || expires <= now.Unix() {
		return "", errHTMLFrameTicket
	}
	secret, err := administratorSettingsSecret(app)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("leafen-html-frame\n" + string(payload)))
	if !hmac.Equal(given, mac.Sum(nil)[:18]) {
		return "", errHTMLFrameTicket
	}
	return fileID, nil
}

// userHTMLAllowed reports whether the organization draws user HTML.
func userHTMLAllowed(app core.App, organizationID string) (bool, error) {
	policy, err := loadEnforcedSharingSecurityPolicy(app, organizationID)
	return err == nil && policy.UserHTML == 1, err
}

func isSandboxedHTML(contentType string) bool {
	base, _, _ := strings.Cut(contentType, ";")
	return strings.EqualFold(strings.TrimSpace(base), sandboxedHTMLContentType)
}

func registerHTMLFrameRoutes(event *core.ServeEvent) {
	event.Router.POST(apiRoot+appPlatformHTMLFramePath, issueHTMLFrameRoute)
	event.Router.GET(apiRoot+appPlatformPublicHTMLFramePath, issuePublicHTMLFrameRoute)
	event.Router.GET(htmlFramePrefix+"{ticket}", serveHTMLFrameRoute)
}

func respondHTMLFrame(event *core.RequestEvent, fileID string) error {
	expires := time.Now().Add(appHTMLFrameSeconds * time.Second)
	ticket, err := htmlFrameTicket(event.App, fileID, expires.Unix())
	if err != nil {
		return appPlatformFailure(event, err)
	}
	return event.JSON(http.StatusOK, appHTMLFrameWire{FrameURL: htmlFramePrefix + ticket, ExpiresAt: expires.UnixMilli()})
}

func htmlOff(event *core.RequestEvent) error {
	return apiError(event, http.StatusForbidden, errorCodeAppHTMLOff, "This organization turned user HTML off.")
}

// issueHTMLFrameRoute gives a person who may read an HTML file a ticket to
// its frame.
func issueHTMLFrameRoute(event *core.RequestEvent) error {
	user, ok := authenticatedUser(event)
	if !ok {
		return apiError(event, http.StatusUnauthorized, errorCodeUnauthorized, "Login required.")
	}
	read, err := authorizeProductFileRead(event.App, user.Id, event.Request.PathValue("id"))
	if err != nil {
		return writeProductFileReadError(event, err, false)
	}
	defer read.Filesystem.Close()
	if !isSandboxedHTML(read.Record.GetString("content_type")) {
		return apiError(event, http.StatusBadRequest, errorCodeAppPlatformInputInvalid, "The file isn't HTML.")
	}
	workspace, err := event.App.FindRecordById(workspacesCollection, read.Record.GetString("workspace"))
	if err != nil {
		return appPlatformFailure(event, err)
	}
	if allowed, err := userHTMLAllowed(event.App, workspace.GetString("organization")); err != nil {
		return appPlatformFailure(event, err)
	} else if !allowed {
		return htmlOff(event)
	}
	return respondHTMLFrame(event, read.Record.Id)
}

// issuePublicHTMLFrameRoute gives a public page's reader a ticket to the
// frame of an HTML file the page shows.
func issuePublicHTMLFrameRoute(event *core.RequestEvent) error {
	authorization, fileID, dlpActions, ok, failure := authorizePublicContentFile(event)
	if !ok {
		return failure
	}
	workspace := authorization.Workspace
	if workspace == nil || workspace.GetString("deletion_job_id") != "" || workspace.GetString("organization") == "" {
		return publicContentFileNotFound(event)
	}
	for _, action := range dlpActions {
		if err := enforceOrganizationDLPOutbound(event.App, workspace.GetString("organization"), action); err != nil {
			return publicContentFileNotFound(event)
		}
	}
	files, err := loadGraphFileAuthorityRecords(event.App, workspace.Id, []string{fileID})
	if err != nil || !isSandboxedHTML(files[fileID].ContentType) {
		return publicContentFileNotFound(event)
	}
	if allowed, err := userHTMLAllowed(event.App, workspace.GetString("organization")); err != nil {
		return appPlatformFailure(event, err)
	} else if !allowed {
		return htmlOff(event)
	}
	return respondHTMLFrame(event, fileID)
}

// serveHTMLFrameRoute draws the file its ticket names in a document with no
// origin, while the file, its workspace and its organization's switch allow.
func serveHTMLFrameRoute(event *core.RequestEvent) error {
	fileID, err := readHTMLFrameTicket(event.App, event.Request.PathValue("ticket"), time.Now())
	if err != nil {
		return event.NoContent(http.StatusNotFound)
	}
	record, err := event.App.FindRecordById(filesCollection, fileID)
	if err != nil || !isSandboxedHTML(record.GetString("content_type")) {
		return event.NoContent(http.StatusNotFound)
	}
	if pending, err := productFileDeletionPending(event.App, fileID); err != nil || pending {
		return event.NoContent(http.StatusNotFound)
	}
	workspace, err := event.App.FindRecordById(workspacesCollection, record.GetString("workspace"))
	if err != nil || workspace.GetString("deletion_job_id") != "" {
		return event.NoContent(http.StatusNotFound)
	}
	if allowed, err := userHTMLAllowed(event.App, workspace.GetString("organization")); err != nil || !allowed {
		return event.NoContent(http.StatusForbidden)
	}
	fsys, err := event.App.NewFilesystem()
	if err != nil {
		return event.NoContent(failedStatus(event, http.StatusInternalServerError, errorCodeAppPlatformFailed, err))
	}
	defer fsys.Close()
	if err := verifyProductFileStorageIdentity(event.App, fsys, record); err != nil {
		return event.NoContent(http.StatusNotFound)
	}
	reader, err := fsys.GetReader(record.BaseFilesPath() + "/" + record.GetString("blob"))
	if err != nil {
		return event.NoContent(http.StatusNotFound)
	}
	defer reader.Close()
	origins, err := framingOrigins(event.Request)
	if err != nil {
		return event.NoContent(failedStatus(event, http.StatusInternalServerError, errorCodeAppPlatformFailed, err))
	}
	// A file that doesn't say its encoding reads as UTF-8, not as the browser's
	// default for its language (a Korean page came out garbled, 2026-11-04).
	body := bufio.NewReader(reader)
	head, _ := body.Peek(htmlFrameCharsetWindow)
	contentType := sandboxedHTMLContentType
	if !htmlFrameDeclaresCharset.Match(head) {
		contentType += "; charset=utf-8"
	}
	header := event.Response.Header()
	header.Set("Content-Type", contentType)
	header.Set("Content-Security-Policy", htmlFramePolicy(origins))
	header.Del("X-Frame-Options")
	header.Set("X-Content-Type-Options", "nosniff")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("Cache-Control", "private, max-age=300")
	event.Response.WriteHeader(http.StatusOK)
	_, err = io.Copy(event.Response, body)
	return err
}

// Browsers look for a page's encoding in its first kilobyte.
const htmlFrameCharsetWindow = 1024

var htmlFrameDeclaresCharset = regexp.MustCompile(`(?i)<meta[^>]*charset`)

// htmlFramePolicy keeps user HTML without an origin and away from forms and
// the top window, and lets it reach outside images, fonts, media, scripts
// and connections: it holds none of Leafen's data to send.
func htmlFramePolicy(origins []string) string {
	return strings.Join([]string{
		"sandbox allow-scripts",
		"default-src 'none'",
		"script-src * data: blob: 'unsafe-inline' 'unsafe-eval'",
		"style-src * data: 'unsafe-inline'",
		"img-src * data: blob:",
		"font-src * data:",
		"media-src * data: blob:",
		"connect-src *",
		"frame-src https:",
		"worker-src * data: blob:",
		"manifest-src 'none'",
		"object-src 'none'",
		"form-action 'none'",
		"base-uri 'none'",
		frameAncestorsDirective(origins),
	}, "; ")
}
